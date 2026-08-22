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

type appendEventResponse struct {
	Event    eventResponse  `json:"event"`
	Snapshot match.Snapshot `json:"snapshot"`
}

type eventResponse struct {
	ID         string          `json:"id"`
	Sequence   int             `json:"sequence"`
	ActionCode string          `json:"action_code"`
	Side       match.Side      `json:"side"`
	Subjects   []match.Subject `json:"subjects"`
	Qualifiers map[string]any  `json:"qualifiers,omitempty"`
}

func AppendEventHandler(repository match.EventAppender) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(io.LimitReader(request.Body, maxCreateMatchBodyBytes+1))
		if err != nil || len(body) > maxCreateMatchBodyBytes {
			platformhttpapi.WriteProblem(writer, http.StatusBadRequest, "invalid-request", "unable to read request body", request)
			return
		}
		defer request.Body.Close()
		var payload match.AppendEventCommand
		decoder := json.NewDecoder(strings.NewReader(string(body)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&payload); err != nil {
			platformhttpapi.WriteProblem(writer, http.StatusBadRequest, "invalid-request", fmt.Sprintf("decode JSON: %v", err), request)
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			platformhttpapi.WriteProblem(writer, http.StatusBadRequest, "invalid-request", "request body must contain one JSON object", request)
			return
		}
		key := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
		if len(key) < 16 || len(key) > 128 {
			platformhttpapi.WriteProblem(writer, http.StatusUnprocessableEntity, "validation-error", "Idempotency-Key must be between 16 and 128 characters", request)
			return
		}
		hash := sha256.Sum256(body)
		event, snapshot, err := repository.Append(request.Context(), request.PathValue("matchId"), payload, match.Idempotency{Key: key, RequestHash: hash[:]})
		if err != nil {
			status, title := http.StatusInternalServerError, "internal-error"
			switch {
			case errors.Is(err, match.ErrInvalidMatchInput):
				status, title = http.StatusUnprocessableEntity, "validation-error"
			case errors.Is(err, match.ErrMatchNotFound):
				status, title = http.StatusNotFound, "not-found"
			case errors.Is(err, match.ErrSequenceConflict), errors.Is(err, match.ErrMatchNotLive), errors.Is(err, match.ErrIdempotencyConflict):
			case errors.Is(err, match.ErrSequenceConflict), errors.Is(err, match.ErrMatchNotLive), errors.Is(err, match.ErrEventAlreadyExists), errors.Is(err, match.ErrIdempotencyConflict):
				status, title = http.StatusConflict, "state-conflict"
			}
			platformhttpapi.WriteProblem(writer, status, title, err.Error(), request)
			return
		}
		platformhttpapi.WriteJSON(writer, http.StatusCreated, appendEventResponse{Event: eventResponse{ID: event.ID, Sequence: event.Sequence, ActionCode: event.Command.ActionCode, Side: event.Command.Side, Subjects: event.Command.Subjects, Qualifiers: event.Command.Qualifiers}, Snapshot: snapshot})
	})
}

