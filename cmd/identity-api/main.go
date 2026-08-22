package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/FootGrid/footgrid/internal/identity"
	identityhttp "github.com/FootGrid/footgrid/internal/identity/httpapi"
	"github.com/FootGrid/footgrid/internal/platform/auth"
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
	mux.Handle("GET /health", httpapi.HealthHandler("identity-api", pool.Ping))
	mux.Handle("GET /v1/me", identityhttp.MeHandler(identity.NewPostgresRepository(pool)))
	handler := httpapi.WithMiddleware(mux)
	if !config.AuthDisabled {
		verifier, err := auth.NewJWTVerifier(config.CognitoIssuerURL, config.CognitoAudience)
		if err != nil {
			slog.Error("invalid authentication configuration", "error", err)
			os.Exit(1)
		}
		handler = auth.Middleware(verifier, handler)
	}
	if os.Getenv("AWS_LAMBDA_RUNTIME_API") != "" {
		httpapi.StartLambda(handler)
		return
	}
	if err := http.ListenAndServe(config.HTTPAddress, handler); err != nil {
		slog.Error("identity api stopped", "error", err)
	}
}
