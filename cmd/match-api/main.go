package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/FootGrid/footgrid/internal/match"
	matchhttp "github.com/FootGrid/footgrid/internal/match/httpapi"
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
	mux.Handle("GET /health", httpapi.HealthHandler("match-api", pool.Ping))
	drafts := match.NewPostgresRepository(pool)
	var organizationAuthorizer auth.OrganizationAuthorizer
	var matchAuthorizer auth.MatchAuthorizer
	if !config.AuthDisabled {
		authorizer := auth.NewDatabaseAuthorizer(pool)
		organizationAuthorizer = authorizer
		matchAuthorizer = authorizer
	}
	commandHandler := func(handler http.Handler, roles ...string) http.Handler {
		if matchAuthorizer == nil {
			return handler
		}
		return auth.RequireMatch(matchAuthorizer, roles, handler)
	}
	mux.Handle("POST /v1/matches", matchhttp.CreateHandlerWithAuthorization(drafts, organizationAuthorizer))
	rosterHandler, lineupHandler, readyHandler := matchhttp.SetupHandlers(drafts)
	mux.Handle("PUT /v1/matches/{matchId}/roster", commandHandler(rosterHandler, "OWNER", "ADMIN", "ORGANIZER", "TEAM_MANAGER"))
	mux.Handle("PUT /v1/matches/{matchId}/lineups", commandHandler(lineupHandler, "OWNER", "ADMIN", "ORGANIZER", "TEAM_MANAGER"))
	mux.Handle("POST /v1/matches/{matchId}/ready", commandHandler(readyHandler, "OWNER", "ADMIN", "ORGANIZER", "TEAM_MANAGER"))
	mux.Handle("POST /v1/matches/{matchId}/live-session", commandHandler(matchhttp.LiveSessionHandler(drafts), "OWNER", "ADMIN", "ORGANIZER", "TEAM_MANAGER", "SCORER", "REFEREE"))
	mux.Handle("POST /v1/matches/{matchId}/events", commandHandler(matchhttp.AppendEventHandler(drafts), "OWNER", "ADMIN", "ORGANIZER", "TEAM_MANAGER", "SCORER", "REFEREE"))
	mux.Handle("POST /v1/matches/{matchId}/events/{eventId}/reverse", commandHandler(matchhttp.ReverseEventHandler(drafts), "OWNER", "ADMIN", "ORGANIZER", "TEAM_MANAGER", "SCORER", "REFEREE"))
	mux.Handle("GET /v1/matches/{matchId}/snapshot", matchhttp.SnapshotHandler(drafts))
	mux.Handle("GET /v1/matches/{matchId}/events", matchhttp.ListEventsHandler(drafts))
	mux.Handle("GET /v1/matches/{matchId}", matchhttp.MatchHandler(drafts))
	// Match command handlers delegate domain and persistence work to internal/match.
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
	slog.Info("match api listening", "address", config.HTTPAddress)
	if err := http.ListenAndServe(config.HTTPAddress, handler); err != nil {
		slog.Error("match api stopped", "error", err)
	}
}
