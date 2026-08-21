package integration_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"testing"

	"github.com/FootGrid/footgrid/internal/match"
	"github.com/FootGrid/footgrid/internal/platform/database"
)

func TestPostgresRepositoryCreateIsIdempotent(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") != "1" {
		t.Skip("set RUN_INTEGRATION_TESTS=1 to run PostgreSQL integration tests")
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required when RUN_INTEGRATION_TESTS=1")
	}

	ctx := context.Background()
	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := match.NewPostgresRepository(pool)
	input := match.CreateInput{
		OrganizationID:       "11111111-1111-4111-8111-111111111111",
		VenueName:            "Integration Turf",
		FormatCode:           "6V6",
		PlayersPerSide:       6,
		PeriodCount:          2,
		TotalDurationSeconds: 2400,
		HomeDisplayName:      "Home integration team",
		AwayDisplayName:      "Away integration team",
	}
	key := "integration-create-draft-001"
	hash := sha256.Sum256([]byte("first request"))
	scope := "matches:create:" + input.OrganizationID

	created, err := repository.Create(ctx, input, match.Idempotency{Key: key, RequestHash: hash[:]})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM platform.outbox_events WHERE aggregate_id = $1::uuid`, created.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM match_data.matches WHERE id = $1::uuid`, created.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM platform.idempotency_records WHERE scope = $1 AND idempotency_key = $2`, scope, key)
	}()

	retried, err := repository.Create(ctx, input, match.Idempotency{Key: key, RequestHash: hash[:]})
	if err != nil {
		t.Fatal(err)
	}
	if retried.ID != created.ID || retried.Status != match.Draft || retried.EventSequence != 0 {
		t.Fatalf("expected original draft response, got %#v", retried)
	}

	changedHash := sha256.Sum256([]byte("changed request"))
	_, err = repository.Create(ctx, input, match.Idempotency{Key: key, RequestHash: changedHash[:]})
	if !errors.Is(err, match.ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}

	var matches int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM match_data.matches WHERE id = $1::uuid`, created.ID).Scan(&matches); err != nil {
		t.Fatal(err)
	}
	if matches != 1 {
		t.Fatalf("expected one persisted draft, got %d", matches)
	}
}
