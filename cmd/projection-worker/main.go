package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/FootGrid/footgrid/internal/platform/config"
	"github.com/FootGrid/footgrid/internal/platform/database"
	"github.com/FootGrid/footgrid/internal/projection"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// The deployment subscribes this Lambda to an SQS queue fed by EventBridge.
// The worker will claim an inbox record before materializing a projection.
func main() {
	ctx := context.Background()
	config, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	pool, err := database.Open(ctx, config.DatabaseURL)
	if err != nil {
		slog.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	projector := projection.NewProjector(pool)

	lambda.Start(func(ctx context.Context, event events.SQSEvent) error {
		for _, record := range event.Records {
			payload, err := projection.DecodeEvent([]byte(record.Body))
			if err != nil {
				return err
			}
			if err := projector.Process(ctx, payload); err != nil {
				return err
			}
			slog.InfoContext(ctx, "projected match event", "message_id", record.MessageId, "source_event_id", payload.SourceID, "event_type", payload.EventType)
		}
		return nil
	})
}
