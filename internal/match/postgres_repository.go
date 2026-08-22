package match

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	if err := idempotency.Validate(); err != nil {
		return Match{}, err
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

func (repository *PostgresRepository) ReplaceRoster(ctx context.Context, matchID string, roster Roster, idempotency Idempotency) (Roster, error) {
	if !isUUID(matchID) {
		return Roster{}, invalidMatchInput("match_id must be a UUID")
	}
	if err := idempotency.Validate(); err != nil {
		return Roster{}, err
	}
	var playersPerSide int
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Roster{}, fmt.Errorf("begin roster transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tx.QueryRow(ctx, `SELECT players_per_side FROM match_data.matches WHERE id = $1::uuid AND status = 'DRAFT' FOR UPDATE`, matchID).Scan(&playersPerSide); err != nil {
		return Roster{}, fmt.Errorf("lock draft for roster: %w", err)
	}
	if err := roster.Validate(playersPerSide); err != nil {
		return Roster{}, invalidMatchInput(err.Error())
	}
	if _, err := tx.Exec(ctx, `DELETE FROM match_data.match_participants WHERE match_id = $1::uuid`, matchID); err != nil {
		return Roster{}, fmt.Errorf("clear roster: %w", err)
	}
	for _, participant := range append(append([]Participant{}, roster.Home...), roster.Away...) {
		if _, err := tx.Exec(ctx, `INSERT INTO match_data.match_participants (id, match_id, match_side_id, shirt_number, display_name_snapshot, position_code, participation_status) SELECT $1::uuid, $2::uuid, id, $3, $4, NULLIF($5, ''), 'NOT_SELECTED' FROM match_data.match_sides WHERE match_id = $2::uuid AND side = $6`, participant.ID, matchID, participant.ShirtNumber, strings.TrimSpace(participant.DisplayName), strings.TrimSpace(participant.PositionCode), participant.Side); err != nil {
			return Roster{}, fmt.Errorf("insert roster participant: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Roster{}, fmt.Errorf("commit roster: %w", err)
	}
	return roster, nil
}

func (repository *PostgresRepository) SetInitialLineups(ctx context.Context, matchID string, homeStarterIDs, awayStarterIDs []string, idempotency Idempotency) (Roster, error) {
	if !isUUID(matchID) {
		return Roster{}, invalidMatchInput("match_id must be a UUID")
	}
	if err := idempotency.Validate(); err != nil {
		return Roster{}, err
	}
	if len(homeStarterIDs) == 0 || len(awayStarterIDs) == 0 || hasDuplicates(homeStarterIDs) || hasDuplicates(awayStarterIDs) {
		return Roster{}, invalidMatchInput("both starter lists must be non-empty and unique")
	}
	for _, starterID := range append(append([]string{}, homeStarterIDs...), awayStarterIDs...) {
		if !isUUID(starterID) {
			return Roster{}, invalidMatchInput("starter IDs must be UUIDs")
		}
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Roster{}, fmt.Errorf("begin lineup transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var playersPerSide int
	if err := tx.QueryRow(ctx, `SELECT players_per_side FROM match_data.matches WHERE id = $1::uuid AND status = 'DRAFT' FOR UPDATE`, matchID).Scan(&playersPerSide); err != nil {
		return Roster{}, fmt.Errorf("lock draft for lineups: %w", err)
	}
	if len(homeStarterIDs) != playersPerSide || len(awayStarterIDs) != playersPerSide {
		return Roster{}, invalidMatchInput(fmt.Sprintf("each lineup requires %d starters", playersPerSide))
	}
	if err := setLineupSide(ctx, tx, matchID, Home, homeStarterIDs); err != nil {
		return Roster{}, err
	}
	if err := setLineupSide(ctx, tx, matchID, Away, awayStarterIDs); err != nil {
		return Roster{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Roster{}, fmt.Errorf("commit lineups: %w", err)
	}
	return Roster{}, nil
}

func setLineupSide(ctx context.Context, tx pgx.Tx, matchID string, side Side, starterIDs []string) error {
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM match_data.match_participants p JOIN match_data.match_sides s ON s.id = p.match_side_id WHERE p.match_id = $1::uuid AND s.side = $2 AND p.id = ANY($3::uuid[])`, matchID, side, starterIDs).Scan(&count); err != nil {
		return fmt.Errorf("validate %s lineup: %w", strings.ToLower(string(side)), err)
	}
	if count != len(starterIDs) {
		return invalidMatchInput(fmt.Sprintf("all %s starters must belong to the roster", strings.ToLower(string(side))))
	}
	_, err := tx.Exec(ctx, `UPDATE match_data.match_participants p SET participation_status = CASE WHEN p.id = ANY($2::uuid[]) THEN 'STARTER' ELSE 'BENCH' END FROM match_data.match_sides s WHERE p.match_id = $1::uuid AND p.match_side_id = s.id AND s.side = $3`, matchID, starterIDs, side)
	return err
}

func hasDuplicates(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

func (repository *PostgresRepository) MarkReady(ctx context.Context, matchID string, idempotency Idempotency) (Match, error) {
	if !isUUID(matchID) {
		return Match{}, invalidMatchInput("match_id must be a UUID")
	}
	if err := idempotency.Validate(); err != nil {
		return Match{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Match{}, fmt.Errorf("begin ready transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var playersPerSide int
	if err := tx.QueryRow(ctx, `SELECT players_per_side FROM match_data.matches WHERE id = $1::uuid AND status = 'DRAFT' FOR UPDATE`, matchID).Scan(&playersPerSide); err != nil {
		return Match{}, fmt.Errorf("lock draft for ready: %w", err)
	}
	var starterCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM match_data.match_participants WHERE match_id = $1::uuid AND participation_status = 'STARTER'`, matchID).Scan(&starterCount); err != nil {
		return Match{}, err
	}
	if starterCount != playersPerSide*2 {
		return Match{}, invalidMatchInput("both initial lineups are required")
	}
	if _, err := tx.Exec(ctx, `UPDATE match_data.matches SET status = 'READY', status_version = status_version + 1 WHERE id = $1::uuid`, matchID); err != nil {
		return Match{}, fmt.Errorf("mark match ready: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Match{}, fmt.Errorf("commit ready transition: %w", err)
	}
	return Match{ID: matchID, Status: Ready}, nil
}

func (repository *PostgresRepository) StartLiveSession(ctx context.Context, matchID string, idempotency Idempotency) (Snapshot, error) {
	if !isUUID(matchID) {
		return Snapshot{}, invalidMatchInput("match_id must be a UUID")
	}
	if err := idempotency.Validate(); err != nil {
		return Snapshot{}, err
	}
	scope := "matches:live-session:" + matchID
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Snapshot{}, fmt.Errorf("begin live-session transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM platform.idempotency_records WHERE scope = $1 AND idempotency_key = $2 AND expires_at <= clock_timestamp()`, scope, idempotency.Key); err != nil {
		return Snapshot{}, fmt.Errorf("remove expired live-session idempotency record: %w", err)
	}
	reserved, err := reserveIdempotencyKey(ctx, tx, scope, idempotency)
	if err != nil {
		return Snapshot{}, err
	}
	if !reserved {
		var requestHash, response []byte
		var responseStatus int
		if err := tx.QueryRow(ctx, `SELECT request_hash, response_status, response_body FROM platform.idempotency_records WHERE scope = $1 AND idempotency_key = $2`, scope, idempotency.Key).Scan(&requestHash, &responseStatus, &response); err != nil {
			return Snapshot{}, fmt.Errorf("read live-session idempotency record: %w", err)
		}
		if !bytes.Equal(requestHash, idempotency.RequestHash) {
			return Snapshot{}, ErrIdempotencyConflict
		}
		if responseStatus != 201 {
			return Snapshot{}, fmt.Errorf("stored live-session response has unexpected status %d", responseStatus)
		}
		var snapshot Snapshot
		if err := json.Unmarshal(response, &snapshot); err != nil {
			return Snapshot{}, fmt.Errorf("decode stored live-session response: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Snapshot{}, fmt.Errorf("commit idempotent live-session read: %w", err)
		}
		return snapshot, nil
	}
	var snapshot Snapshot
	var status string
	var playersPerSide int
	if err := tx.QueryRow(ctx, `SELECT m.status::text, m.players_per_side, l.current_sequence, l.home_score, l.away_score FROM match_data.matches m JOIN match_data.match_live_state l ON l.match_id = m.id WHERE m.id = $1::uuid FOR UPDATE OF m, l`, matchID).Scan(&status, &playersPerSide, &snapshot.EventSequence, &snapshot.HomeScore, &snapshot.AwayScore); err != nil {
		return Snapshot{}, fmt.Errorf("lock match for live session: %w", err)
	}
	snapshot.MatchID, snapshot.Status = matchID, Status(status)
	if _, err := StartLiveSession(snapshot); err != nil {
		return Snapshot{}, err
	}
	var homeStarters, awayStarters int
	if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE s.side = 'HOME'), count(*) FILTER (WHERE s.side = 'AWAY') FROM match_data.match_participants p JOIN match_data.match_sides s ON s.id = p.match_side_id WHERE p.match_id = $1::uuid AND p.participation_status = 'STARTER'`, matchID).Scan(&homeStarters, &awayStarters); err != nil {
		return Snapshot{}, fmt.Errorf("count starting lineup: %w", err)
	}
	if homeStarters != playersPerSide || awayStarters != playersPerSide {
		return Snapshot{}, invalidMatchInput("both complete initial lineups are required")
	}
	if _, err := tx.Exec(ctx, `UPDATE match_data.matches SET status = 'LIVE', status_version = status_version + 1 WHERE id = $1::uuid`, matchID); err != nil {
		return Snapshot{}, fmt.Errorf("start live match: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE match_data.match_live_state SET current_period_number = 1, clock_state = 'RUNNING', updated_at = clock_timestamp() WHERE match_id = $1::uuid`, matchID); err != nil {
		return Snapshot{}, fmt.Errorf("start live clock: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO match_data.live_sessions (match_id, status, period_number) VALUES ($1::uuid, 'RUNNING', 1)`, matchID); err != nil {
		return Snapshot{}, fmt.Errorf("create live session: %w", err)
	}
	response, err := json.Marshal(snapshot)
	if err != nil {
		return Snapshot{}, fmt.Errorf("encode live-session response: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO platform.outbox_events (aggregate_type, aggregate_id, aggregate_sequence, event_type, payload) VALUES ('match', $1::uuid, $2, 'match.status-changed.v1', $3::jsonb)`, matchID, snapshot.EventSequence, response); err != nil {
		return Snapshot{}, fmt.Errorf("enqueue live-session event: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE platform.idempotency_records SET response_status = 201, response_body = $3::jsonb WHERE scope = $1 AND idempotency_key = $2`, scope, idempotency.Key, response); err != nil {
		return Snapshot{}, fmt.Errorf("save live-session response: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Snapshot{}, fmt.Errorf("commit live session: %w", err)
	}
	return snapshot, nil
}
