package match

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const createIdempotencyTTL = "24 hours"

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Create writes the DRAFT aggregate, its two sides and the initial live state
// in one transaction. It reserves the idempotency key in that same transaction,
// so a retry either receives the original result or a conflict for a changed
// request body.
func (repository *PostgresRepository) Create(ctx context.Context, input CreateInput, idempotency Idempotency) (Match, error) {
	if err := input.Validate(); err != nil {
		return Match{}, err
	}
	if len(idempotency.RequestHash) == 0 {
		return Match{}, fmt.Errorf("%w: request hash is required", ErrIdempotencyConflict)
	}

	scope := "matches:create:" + input.OrganizationID
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Match{}, fmt.Errorf("begin create draft transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		DELETE FROM platform.idempotency_records
		WHERE scope = $1 AND idempotency_key = $2 AND expires_at <= clock_timestamp()`, scope, idempotency.Key); err != nil {
		return Match{}, fmt.Errorf("remove expired idempotency record: %w", err)
	}

	reserved, err := reserveIdempotencyKey(ctx, tx, scope, idempotency)
	if err != nil {
		return Match{}, err
	}
	if !reserved {
		match, err := idempotentMatch(ctx, tx, scope, idempotency)
		if err != nil {
			return Match{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Match{}, fmt.Errorf("commit idempotent draft read: %w", err)
		}
		return match, nil
	}

	match, err := insertDraft(ctx, tx, input)
	if err != nil {
		return Match{}, err
	}
	response, err := json.Marshal(match)
	if err != nil {
		return Match{}, fmt.Errorf("encode draft response: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform.outbox_events
			(aggregate_type, aggregate_id, aggregate_sequence, event_type, payload)
		VALUES ('match', $1::uuid, 0, 'match.created.v1', $2::jsonb)`, match.ID, response); err != nil {
		return Match{}, fmt.Errorf("enqueue draft-created event: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE platform.idempotency_records
		SET response_status = 201, response_body = $3::jsonb
		WHERE scope = $1 AND idempotency_key = $2`, scope, idempotency.Key, response); err != nil {
		return Match{}, fmt.Errorf("save idempotent draft response: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Match{}, fmt.Errorf("commit draft creation: %w", err)
	}
	return match, nil
}

func reserveIdempotencyKey(ctx context.Context, tx pgx.Tx, scope string, idempotency Idempotency) (bool, error) {
	commandTag, err := tx.Exec(ctx, `
		INSERT INTO platform.idempotency_records
			(scope, idempotency_key, request_hash, response_status, response_body, expires_at)
		VALUES ($1, $2, $3, 201, '{}'::jsonb, clock_timestamp() + $4::interval)
		ON CONFLICT (scope, idempotency_key) DO NOTHING`, scope, idempotency.Key, idempotency.RequestHash, createIdempotencyTTL)
	if err != nil {
		return false, fmt.Errorf("reserve idempotency key: %w", err)
	}
	return commandTag.RowsAffected() == 1, nil
}

func idempotentMatch(ctx context.Context, tx pgx.Tx, scope string, idempotency Idempotency) (Match, error) {
	var requestHash, response []byte
	var responseStatus int
	err := tx.QueryRow(ctx, `
		SELECT request_hash, response_status, response_body
		FROM platform.idempotency_records
		WHERE scope = $1 AND idempotency_key = $2`, scope, idempotency.Key).Scan(&requestHash, &responseStatus, &response)
	if err != nil {
		return Match{}, fmt.Errorf("read idempotency record: %w", err)
	}
	if !bytes.Equal(requestHash, idempotency.RequestHash) {
		return Match{}, ErrIdempotencyConflict
	}
	if responseStatus != 201 {
		return Match{}, fmt.Errorf("stored idempotency response has unexpected status %d", responseStatus)
	}
	var match Match
	if err := json.Unmarshal(response, &match); err != nil {
		return Match{}, fmt.Errorf("decode stored draft response: %w", err)
	}
	return match, nil
}

func insertDraft(ctx context.Context, tx pgx.Tx, input CreateInput) (Match, error) {
	var matchID string
	err := tx.QueryRow(ctx, `
		INSERT INTO match_data.matches
			(organization_id, venue_name_snapshot, format_code, players_per_side, period_count, total_duration_seconds)
		VALUES ($1::uuid, NULLIF($2, ''), $3, $4, $5, $6)
		RETURNING id`, input.OrganizationID, input.VenueName, input.FormatCode, input.PlayersPerSide, input.PeriodCount, input.TotalDurationSeconds).Scan(&matchID)
	if err != nil {
		return Match{}, fmt.Errorf("insert draft match: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO match_data.match_sides (match_id, side, team_id, display_name)
		VALUES
			($1::uuid, 'HOME', NULLIF($2, '')::uuid, $3),
			($1::uuid, 'AWAY', NULLIF($4, '')::uuid, $5)`,
		matchID, input.HomeTeamID, input.HomeDisplayName, input.AwayTeamID, input.AwayDisplayName); err != nil {
		return Match{}, fmt.Errorf("insert draft match sides: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO match_data.match_live_state (match_id) VALUES ($1::uuid)`, matchID); err != nil {
		return Match{}, fmt.Errorf("insert draft live state: %w", err)
	}
	return Match{
		ID:             matchID,
		OrganizationID: input.OrganizationID,
		Status:         Draft,
		EventSequence:  0,
		Home:           matchSideFromInput(input.HomeTeamID, input.HomeDisplayName),
		Away:           matchSideFromInput(input.AwayTeamID, input.AwayDisplayName),
	}, nil
}

func matchSideFromInput(teamID, displayName string) MatchSide {
	if teamID == "" {
		return MatchSide{DisplayName: displayName}
	}
	return MatchSide{TeamID: &teamID, DisplayName: displayName}
}
