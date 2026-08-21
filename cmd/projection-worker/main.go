package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"

	"github.com/FootGrid/footgrid/internal/platform/config"
	"github.com/FootGrid/footgrid/internal/platform/database"
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

	lambda.Start(func(ctx context.Context, event events.SQSEvent) error {
		for _, record := range event.Records {
			var payload map[string]any
			if err := json.Unmarshal([]byte(record.Body), &payload); err != nil {
				return err
			}
			slog.InfoContext(ctx, "received projection event", "message_id", record.MessageId, "payload", payload)
		}
		return nil
	})
}
