package matchhttp

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/FootGrid/footgrid/internal/match"
	platformhttpapi "github.com/FootGrid/footgrid/internal/platform/httpapi"
)

type setupHandlers struct{ repository match.SetupRepository }

func SetupHandlers(repository match.SetupRepository) (http.Handler, http.Handler, http.Handler) {
	handlers := setupHandlers{repository: repository}
	return http.HandlerFunc(handlers.replaceRoster), http.HandlerFunc(handlers.setLineups), http.HandlerFunc(handlers.markReady)
}

func (handlers setupHandlers) replaceRoster(writer http.ResponseWriter, request *http.Request) {
	var roster match.Roster
	if err := decodeJSON(request, &roster); err != nil {
		platformhttpapi.WriteProblem(writer, http.StatusBadRequest, "invalid-request", err.Error(), request)
		return
	}
	key, hash, err := setupIdempotency(request, roster)
	if err != nil {
		platformhttpapi.WriteProblem(writer, http.StatusUnprocessableEntity, "validation-error", err.Error(), request)
		return
	}
	result, err := handlers.repository.ReplaceRoster(request.Context(), request.PathValue("matchId"), roster, match.Idempotency{Key: key, RequestHash: hash[:]})
	if err != nil {
		writeSetupError(writer, request, err)
		return
	}
	platformhttpapi.WriteJSON(writer, http.StatusOK, result)
}

func (handlers setupHandlers) setLineups(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		HomeStarterIDs []string `json:"home_starter_ids"`
		AwayStarterIDs []string `json:"away_starter_ids"`
	}
	if err := decodeJSON(request, &payload); err != nil {
		platformhttpapi.WriteProblem(writer, http.StatusBadRequest, "invalid-request", err.Error(), request)
		return
	}
	key, hash, err := setupIdempotency(request, payload)
	if err != nil {
		platformhttpapi.WriteProblem(writer, http.StatusUnprocessableEntity, "validation-error", err.Error(), request)
		return
	}
	result, err := handlers.repository.SetInitialLineups(request.Context(), request.PathValue("matchId"), payload.HomeStarterIDs, payload.AwayStarterIDs, match.Idempotency{Key: key, RequestHash: hash[:]})
	if err != nil {
		writeSetupError(writer, request, err)
		return
	}
	platformhttpapi.WriteJSON(writer, http.StatusOK, result)
}

func (handlers setupHandlers) markReady(writer http.ResponseWriter, request *http.Request) {
	key, hash, err := setupIdempotency(request, struct{}{})
	if err != nil {
		platformhttpapi.WriteProblem(writer, http.StatusUnprocessableEntity, "validation-error", err.Error(), request)
		return
	}
	result, err := handlers.repository.MarkReady(request.Context(), request.PathValue("matchId"), match.Idempotency{Key: key, RequestHash: hash[:]})
	if err != nil {
		writeSetupError(writer, request, err)
		return
	}
	platformhttpapi.WriteJSON(writer, http.StatusCreated, result)
}

func setupIdempotency(request *http.Request, payload any) (string, [32]byte, error) {
	key := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	if len(key) < 16 || len(key) > 128 {
		return "", [32]byte{}, fmt.Errorf("Idempotency-Key must be between 16 and 128 characters")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", [32]byte{}, fmt.Errorf("encode request: %w", err)
	}
	return key, sha256.Sum256(body), nil
}

func decodeJSON(request *http.Request, target any) error {
	defer request.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(request.Body, maxCreateMatchBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request body must contain one JSON object")
	}
	return nil
}

func writeSetupError(writer http.ResponseWriter, request *http.Request, err error) {
	status := http.StatusInternalServerError
	title := "internal-error"
	if errors.Is(err, match.ErrInvalidMatchInput) {
		status, title = http.StatusUnprocessableEntity, "validation-error"
	}
	platformhttpapi.WriteProblem(writer, status, title, err.Error(), request)
}
