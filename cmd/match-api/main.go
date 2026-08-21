package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/FootGrid/footgrid/internal/platform/config"
	"github.com/FootGrid/footgrid/internal/platform/database"
	"github.com/FootGrid/footgrid/internal/platform/httpapi"
)

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

	mux := http.NewServeMux()
	mux.Handle("GET /health", httpapi.HealthHandler("match-api", pool.Ping))
	// Match routes are registered here as repository methods are implemented.
	// Do not put domain logic in this package; delegate it to internal/match.
	handler := httpapi.WithMiddleware(mux)
	if os.Getenv("AWS_LAMBDA_RUNTIME_API") != "" {
		httpapi.StartLambda(handler)
		return
	}
	slog.Info("match api listening", "address", config.HTTPAddress)
	if err := http.ListenAndServe(config.HTTPAddress, handler); err != nil {
		slog.Error("match api stopped", "error", err)
	}
}
