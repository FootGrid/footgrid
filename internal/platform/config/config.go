package config

import (
	"fmt"
	"os"
	"time"
)

// Config contains only process configuration. Tenant and user context belongs in
// the request, never in environment variables.
type Config struct {
	Environment        string
	HTTPAddress        string
	DatabaseURL        string
	AuthDisabled       bool
	CognitoIssuerURL   string
	CognitoAudience    string
	OutboxPollInterval time.Duration
}

func Load() (Config, error) {
	pollInterval, err := time.ParseDuration(value("OUTBOX_POLL_INTERVAL", "1s"))
	if err != nil {
		return Config{}, fmt.Errorf("parse OUTBOX_POLL_INTERVAL: %w", err)
	}

	config := Config{
		Environment:        value("APP_ENV", "local"),
		HTTPAddress:        value("HTTP_ADDR", ":8080"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		AuthDisabled:       value("AUTH_DISABLED", "false") == "true",
		CognitoIssuerURL:   os.Getenv("COGNITO_ISSUER_URL"),
		CognitoAudience:    os.Getenv("COGNITO_AUDIENCE"),
		OutboxPollInterval: pollInterval,
	}
	if config.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if !config.AuthDisabled && (config.CognitoIssuerURL == "" || config.CognitoAudience == "") {
		return Config{}, fmt.Errorf("COGNITO_ISSUER_URL and COGNITO_AUDIENCE are required when AUTH_DISABLED=false")
	}
	return config, nil
}

func value(key, fallback string) string {
	if current := os.Getenv(key); current != "" {
		return current
	}
	return fallback
}
