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

func ReverseEventHandler(repository match.EventReverser) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(io.LimitReader(request.Body, maxCreateMatchBodyBytes+1))
		if err != nil || len(body) > maxCreateMatchBodyBytes {
			platformhttpapi.WriteProblem(writer, http.StatusBadRequest, "invalid-request", "unable to read request body", request)
			return
		}
		defer request.Body.Close()
		var payload struct {
			ClientEventID    string `json:"client_event_id"`
			ExpectedSequence int    `json:"expected_sequence"`
			Reason           string `json:"reason"`
		}
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
		event, snapshot, err := repository.Reverse(request.Context(), request.PathValue("matchId"), request.PathValue("eventId"), payload.ClientEventID, payload.ExpectedSequence, payload.Reason, match.Idempotency{Key: key, RequestHash: hash[:]})
		if err != nil {
			status, title := http.StatusInternalServerError, "internal-error"
			switch {
			case errors.Is(err, match.ErrInvalidMatchInput):
				status, title = http.StatusUnprocessableEntity, "validation-error"
			case errors.Is(err, match.ErrMatchNotFound), errors.Is(err, match.ErrEventNotFound):
				status, title = http.StatusNotFound, "not-found"
			case errors.Is(err, match.ErrSequenceConflict), errors.Is(err, match.ErrMatchNotLive), errors.Is(err, match.ErrEventAlreadyReversed), errors.Is(err, match.ErrEventNotReversible), errors.Is(err, match.ErrIdempotencyConflict):
				status, title = http.StatusConflict, "state-conflict"
			}
			platformhttpapi.WriteProblem(writer, status, title, err.Error(), request)
			return
		}
		platformhttpapi.WriteJSON(writer, http.StatusCreated, appendEventResponse{Event: eventResponse{ID: event.ID, Sequence: event.Sequence, ActionCode: event.Command.ActionCode, Side: event.Command.Side, Subjects: event.Command.Subjects, Qualifiers: event.Command.Qualifiers}, Snapshot: snapshot})
	})
}
