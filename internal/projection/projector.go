package projection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/FootGrid/footgrid/internal/match"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const consumerName = "match-snapshot-v1"

type Event struct {
	SourceID    string
	EventType   string
	AggregateID string
	Payload     json.RawMessage
}

type snapshotEnvelope struct {
	Event    match.Event    `json:"event"`
	Snapshot match.Snapshot `json:"snapshot"`
}

type Projector struct{ pool *pgxpool.Pool }

func NewProjector(pool *pgxpool.Pool) *Projector { return &Projector{pool: pool} }

func (projector *Projector) Process(ctx context.Context, event Event) error {
	if !isUUID(event.SourceID) || !isUUID(event.AggregateID) {
		return errors.New("projection event source and aggregate IDs must be UUIDs")
	}
	if len(event.Payload) == 0 {
		return errors.New("projection event payload is required")
	}
	tx, err := projector.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin projection transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	inserted, err := tx.Exec(ctx, `INSERT INTO read_model.consumer_inbox (consumer_name, source_event_id) VALUES ($1, $2::uuid) ON CONFLICT DO NOTHING`, consumerName, event.SourceID)
	if err != nil {
		return fmt.Errorf("claim projection event: %w", err)
	}
	if inserted.RowsAffected() == 0 {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit duplicate projection acknowledgement: %w", err)
		}
		return nil
	}
	switch event.EventType {
	case "match.created.v1":
		var created match.Match
		if err := json.Unmarshal(event.Payload, &created); err != nil {
			return fmt.Errorf("decode match-created event: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO read_model.match_snapshots (match_id, organization_id, status, last_event_sequence) VALUES ($1::uuid, $2::uuid, $3, $4) ON CONFLICT (match_id) DO UPDATE SET status = EXCLUDED.status, last_event_sequence = EXCLUDED.last_event_sequence, generated_at = clock_timestamp()`, event.AggregateID, created.OrganizationID, created.Status, created.EventSequence); err != nil {
			return fmt.Errorf("project match-created event: %w", err)
		}
	case "match.status-changed.v1", "match.event-recorded.v1", "match.event-reversed.v1":
		var envelope snapshotEnvelope
		if err := json.Unmarshal(event.Payload, &envelope); err != nil {
			return fmt.Errorf("decode match projection event: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE read_model.match_snapshots SET status = $2, home_score = $3, away_score = $4, last_event_sequence = $5, generated_at = clock_timestamp() WHERE match_id = $1::uuid AND (last_event_sequence < $5 OR (last_event_sequence = $5 AND CASE status WHEN 'DRAFT' THEN 0 WHEN 'READY' THEN 1 WHEN 'LIVE' THEN 2 WHEN 'PAUSED' THEN 3 WHEN 'COMPLETED' THEN 4 WHEN 'FINALIZED' THEN 5 WHEN 'ABANDONED' THEN 6 ELSE -1 END < CASE $2 WHEN 'DRAFT' THEN 0 WHEN 'READY' THEN 1 WHEN 'LIVE' THEN 2 WHEN 'PAUSED' THEN 3 WHEN 'COMPLETED' THEN 4 WHEN 'FINALIZED' THEN 5 WHEN 'ABANDONED' THEN 6 ELSE -1 END))`, event.AggregateID, envelope.Snapshot.Status, envelope.Snapshot.HomeScore, envelope.Snapshot.AwayScore, envelope.Snapshot.EventSequence); err != nil {
			return fmt.Errorf("project match snapshot: %w", err)
		}
	default:
		return fmt.Errorf("unsupported projection event type %q", event.EventType)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit projection transaction: %w", err)
	}
	return nil
}

// RebuildMatch reconstructs the replaceable snapshot from command-side facts.
// It is intentionally independent of inbox state so operators can replay a match.
func (projector *Projector) RebuildMatch(ctx context.Context, matchID string) error {
	if !isUUID(matchID) {
		return errors.New("match ID must be a UUID")
	}
	tx, err := projector.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin projection rebuild: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var organizationID, status string
	var sequence, homeScore, awayScore int
	err = tx.QueryRow(ctx, `SELECT m.organization_id::text, m.status::text, COALESCE(MAX(e.sequence), 0), COALESCE(SUM(CASE WHEN e.side = 'HOME' AND NOT EXISTS (SELECT 1 FROM match_data.event_reversals r WHERE r.reversed_event_id = e.id) THEN CASE e.action_code WHEN 'GOAL' THEN 1 WHEN 'SCORE_ADJUSTMENT' THEN COALESCE((e.qualifiers->>'score_delta')::integer, 0) ELSE 0 END ELSE 0 END), 0), COALESCE(SUM(CASE WHEN e.side = 'AWAY' AND NOT EXISTS (SELECT 1 FROM match_data.event_reversals r WHERE r.reversed_event_id = e.id) THEN CASE e.action_code WHEN 'GOAL' THEN 1 WHEN 'SCORE_ADJUSTMENT' THEN COALESCE((e.qualifiers->>'score_delta')::integer, 0) ELSE 0 END ELSE 0 END), 0) FROM match_data.matches m LEFT JOIN match_data.match_events e ON e.match_id = m.id WHERE m.id = $1::uuid GROUP BY m.id, m.organization_id, m.status`, matchID).Scan(&organizationID, &status, &sequence, &homeScore, &awayScore)
	if errors.Is(err, pgx.ErrNoRows) {
		return match.ErrMatchNotFound
	}
	if err != nil {
		return fmt.Errorf("read match ledger for rebuild: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO read_model.match_snapshots (match_id, organization_id, status, home_score, away_score, last_event_sequence, generated_at) VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, clock_timestamp()) ON CONFLICT (match_id) DO UPDATE SET organization_id = EXCLUDED.organization_id, status = EXCLUDED.status, home_score = EXCLUDED.home_score, away_score = EXCLUDED.away_score, last_event_sequence = EXCLUDED.last_event_sequence, generated_at = EXCLUDED.generated_at`, matchID, organizationID, status, homeScore, awayScore, sequence); err != nil {
		return fmt.Errorf("write rebuilt match snapshot: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit projection rebuild: %w", err)
	}
	return nil
}

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}

func DecodeEvent(body []byte) (Event, error) {
	var message struct {
		ID          string          `json:"id"`
		EventType   string          `json:"event_type"`
		DetailType  string          `json:"detail-type"`
		AggregateID string          `json:"aggregate_id"`
		Payload     json.RawMessage `json:"payload"`
		Detail      json.RawMessage `json:"detail"`
	}
	if err := json.Unmarshal(body, &message); err != nil {
		return Event{}, fmt.Errorf("decode projection message: %w", err)
	}
	if len(message.Detail) > 0 && string(message.Detail) != "null" {
		var detail struct {
			AggregateID string          `json:"aggregate_id"`
			EventType   string          `json:"event_type"`
			Payload     json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(message.Detail, &detail); err != nil {
			return Event{}, fmt.Errorf("decode projection detail: %w", err)
		}
		message.AggregateID, message.EventType, message.Payload = detail.AggregateID, detail.EventType, detail.Payload
	}
	if message.EventType == "" {
		message.EventType = message.DetailType
	}
	if message.ID == "" || message.AggregateID == "" || message.EventType == "" || len(message.Payload) == 0 {
		return Event{}, errors.New("projection message requires id, event type, aggregate ID, and payload")
	}
	return Event{SourceID: message.ID, EventType: message.EventType, AggregateID: message.AggregateID, Payload: message.Payload}, nil
}
