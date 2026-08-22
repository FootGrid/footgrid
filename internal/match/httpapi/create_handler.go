// Package matchhttp contains the HTTP boundary for match commands.
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
	platformauth "github.com/FootGrid/footgrid/internal/platform/auth"
	platformhttpapi "github.com/FootGrid/footgrid/internal/platform/httpapi"
)

const maxCreateMatchBodyBytes = 1 << 20

type createMatchRequest struct {
	OrganizationID       string          `json:"organization_id"`
	VenueName            string          `json:"venue_name"`
	FormatCode           string          `json:"format_code"`
	PlayersPerSide       int             `json:"players_per_side"`
	PeriodCount          int             `json:"period_count"`
	TotalDurationSeconds int             `json:"total_duration_seconds"`
	Home                 createMatchSide `json:"home"`
	Away                 createMatchSide `json:"away"`
}

type createMatchSide struct {
	TeamID      *string `json:"team_id"`
	DisplayName string  `json:"display_name"`
}

func CreateHandler(creator match.DraftCreator) http.HandlerFunc {
	return CreateHandlerWithAuthorization(creator, nil)
}

func CreateHandlerWithAuthorization(creator match.DraftCreator, authorizer platformauth.OrganizationAuthorizer) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		idempotencyKey := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
		if len(idempotencyKey) < 16 || len(idempotencyKey) > 128 {
			platformhttpapi.WriteProblem(writer, http.StatusUnprocessableEntity, "validation-error", "Idempotency-Key must be between 16 and 128 characters", request)
			return
		}

		body, err := readCreateRequestBody(writer, request)
		if err != nil {
			platformhttpapi.WriteProblem(writer, http.StatusBadRequest, "invalid-request", err.Error(), request)
			return
		}
		var payload createMatchRequest
		if err := decodeCreateRequest(body, &payload); err != nil {
			platformhttpapi.WriteProblem(writer, http.StatusBadRequest, "invalid-request", err.Error(), request)
			return
		}
		input := payload.toCreateInput()
		if err := input.Validate(); err != nil {
			platformhttpapi.WriteProblem(writer, http.StatusUnprocessableEntity, "validation-error", err.Error(), request)
			return
		}
		if authorizer != nil {
			principal, ok := platformauth.FromContext(request.Context())
			if !ok {
				platformhttpapi.WriteProblem(writer, http.StatusUnauthorized, "unauthorized", "authenticated principal is required", request)
				return
			}
			if err := authorizer.AuthorizeOrganization(request.Context(), principal.Subject, input.OrganizationID, "OWNER", "ADMIN", "ORGANIZER", "TEAM_MANAGER"); err != nil {
				if errors.Is(err, platformauth.ErrForbidden) {
					platformhttpapi.WriteProblem(writer, http.StatusForbidden, "forbidden", "organization membership does not permit match creation", request)
				} else {
					platformhttpapi.WriteProblem(writer, http.StatusInternalServerError, "internal-error", "unable to authorize organization access", request)
				}
				return
			}
		}
		hash := sha256.Sum256(body)
		created, err := creator.Create(request.Context(), input, match.Idempotency{Key: idempotencyKey, RequestHash: hash[:]})
		if err != nil {
			switch {
			case errors.Is(err, match.ErrInvalidMatchInput):
				platformhttpapi.WriteProblem(writer, http.StatusUnprocessableEntity, "validation-error", err.Error(), request)
			case errors.Is(err, match.ErrIdempotencyConflict):
				platformhttpapi.WriteProblem(writer, http.StatusConflict, "idempotency-conflict", err.Error(), request)
			default:
				platformhttpapi.WriteProblem(writer, http.StatusInternalServerError, "internal-error", "unable to create draft match", request)
			}
			return
		}
		platformhttpapi.WriteJSON(writer, http.StatusCreated, created)
	}
}

func (request createMatchRequest) toCreateInput() match.CreateInput {
	return match.CreateInput{
		OrganizationID:       request.OrganizationID,
		VenueName:            strings.TrimSpace(request.VenueName),
		FormatCode:           request.FormatCode,
		PlayersPerSide:       request.PlayersPerSide,
		PeriodCount:          request.PeriodCount,
		TotalDurationSeconds: request.TotalDurationSeconds,
		HomeTeamID:           optionalString(request.Home.TeamID),
		HomeDisplayName:      strings.TrimSpace(request.Home.DisplayName),
		AwayTeamID:           optionalString(request.Away.TeamID),
		AwayDisplayName:      strings.TrimSpace(request.Away.DisplayName),
	}
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func readCreateRequestBody(writer http.ResponseWriter, request *http.Request) ([]byte, error) {
	defer request.Body.Close()
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, maxCreateMatchBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	return body, nil
}

func decodeCreateRequest(body []byte, payload *createMatchRequest) error {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(payload); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request body must contain one JSON object")
	}
	return nil
}
