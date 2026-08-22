package matchhttp

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/FootGrid/footgrid/internal/match"
	platformhttpapi "github.com/FootGrid/footgrid/internal/platform/httpapi"
)

func MatchHandler(repository match.MatchReader) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		result, err := repository.GetMatch(request.Context(), request.PathValue("matchId"))
		if err != nil {
			writeReadError(writer, request, err)
			return
		}
		platformhttpapi.WriteJSON(writer, http.StatusOK, result)
	})
}

func SnapshotHandler(repository match.ReadRepository) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		snapshot, err := repository.GetSnapshot(request.Context(), request.PathValue("matchId"))
		if err != nil {
			writeReadError(writer, request, err)
			return
		}
		platformhttpapi.WriteJSON(writer, http.StatusOK, snapshot)
	})
}

func ListEventsHandler(repository match.ReadRepository) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		afterSequence := 0
		if value := request.URL.Query().Get("after_sequence"); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 0 {
				platformhttpapi.WriteProblem(writer, http.StatusUnprocessableEntity, "validation-error", "after_sequence must be a non-negative integer", request)
				return
			}
			afterSequence = parsed
		}
		events, err := repository.ListEvents(request.Context(), request.PathValue("matchId"), afterSequence)
		if err != nil {
			writeReadError(writer, request, err)
			return
		}
		platformhttpapi.WriteJSON(writer, http.StatusOK, events)
	})
}

func writeReadError(writer http.ResponseWriter, request *http.Request, err error) {
	status, title := http.StatusInternalServerError, "internal-error"
	switch {
	case errors.Is(err, match.ErrInvalidMatchInput):
		status, title = http.StatusUnprocessableEntity, "validation-error"
	case errors.Is(err, match.ErrMatchNotFound):
		status, title = http.StatusNotFound, "not-found"
	}
	platformhttpapi.WriteProblem(writer, status, title, err.Error(), request)
}
