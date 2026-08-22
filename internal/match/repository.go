package match

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Repository methods that change the ledger must run in one database
// transaction. Append locks match_data.match_live_state before checking sequence.
type Repository interface {
	Create(ctx context.Context, input CreateInput, idempotency Idempotency) (Match, error)
	GetSnapshot(ctx context.Context, matchID string) (Snapshot, error)
	ReplaceRoster(ctx context.Context, matchID string, roster Roster, idempotency Idempotency) (Roster, error)
	SetInitialLineups(ctx context.Context, matchID string, homeStarterIDs, awayStarterIDs []string, idempotency Idempotency) (Roster, error)
	MarkReady(ctx context.Context, matchID string, idempotency Idempotency) (Match, error)
	Append(ctx context.Context, matchID string, command AppendEventCommand) (Event, Snapshot, error)
	Reverse(ctx context.Context, matchID, eventID, clientEventID string, expectedSequence int, reason string) (Event, Snapshot, error)
}

// SetupRepository is the narrow dependency for draft setup commands.
type SetupRepository interface {
	ReplaceRoster(ctx context.Context, matchID string, roster Roster, idempotency Idempotency) (Roster, error)
	SetInitialLineups(ctx context.Context, matchID string, homeStarterIDs, awayStarterIDs []string, idempotency Idempotency) (Roster, error)
	MarkReady(ctx context.Context, matchID string, idempotency Idempotency) (Match, error)
}

// DraftCreator is the narrow dependency used by the create-match HTTP handler.
type DraftCreator interface {
	Create(ctx context.Context, input CreateInput, idempotency Idempotency) (Match, error)
}

type Idempotency struct {
	Key         string
	RequestHash []byte
}

func (idempotency Idempotency) Validate() error {
	if len(idempotency.Key) < 16 || len(idempotency.Key) > 128 {
		return invalidMatchInput("Idempotency-Key must be between 16 and 128 characters")
	}
	if strings.TrimSpace(idempotency.Key) == "" {
		return invalidMatchInput("Idempotency-Key must not be blank")
	}
	if len(idempotency.RequestHash) == 0 {
		return invalidMatchInput("request hash is required")
	}
	return nil
}

type MatchSide struct {
	TeamID      *string `json:"team_id"`
	DisplayName string  `json:"display_name"`
}

// Match is the API representation returned when a draft is created. The
// aggregate stays deliberately small until roster and lineup slices are added.
type Match struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	Status         Status    `json:"status"`
	EventSequence  int       `json:"event_sequence"`
	Home           MatchSide `json:"home"`
	Away           MatchSide `json:"away"`
}

type CreateInput struct {
	OrganizationID       string
	VenueName            string
	FormatCode           string
	PlayersPerSide       int
	PeriodCount          int
	TotalDurationSeconds int
	HomeTeamID           string
	HomeDisplayName      string
	AwayTeamID           string
	AwayDisplayName      string
}

func (input CreateInput) Validate() error {
	if !isUUID(input.OrganizationID) {
		return invalidMatchInput("organization_id must be a UUID")
	}
	if input.HomeTeamID != "" && !isUUID(input.HomeTeamID) {
		return invalidMatchInput("home.team_id must be a UUID")
	}
	if input.AwayTeamID != "" && !isUUID(input.AwayTeamID) {
		return invalidMatchInput("away.team_id must be a UUID")
	}
	if len(input.VenueName) > 120 {
		return invalidMatchInput("venue_name must not exceed 120 characters")
	}
	if strings.TrimSpace(input.HomeDisplayName) == "" || strings.TrimSpace(input.AwayDisplayName) == "" {
		return invalidMatchInput("home and away display names are required")
	}
	if len(input.HomeDisplayName) > 100 || len(input.AwayDisplayName) > 100 {
		return invalidMatchInput("team display names must not exceed 100 characters")
	}
	if input.PlayersPerSide < 2 || input.PlayersPerSide > 11 || input.PeriodCount < 1 || input.PeriodCount > 4 {
		return invalidMatchInput("players_per_side or period_count is outside its allowed range")
	}
	if input.TotalDurationSeconds < 600 || input.TotalDurationSeconds > 10800 {
		return invalidMatchInput("total_duration_seconds is outside its allowed range")
	}
	if input.FormatCode == "CUSTOM" {
		return nil
	}
	expectedPlayers, ok := playersPerSideForFormat(input.FormatCode)
	if !ok {
		return invalidMatchInput("format_code is not supported")
	}
	if input.PlayersPerSide != expectedPlayers {
		return invalidMatchInput(fmt.Sprintf("format_code %s requires %d players_per_side", input.FormatCode, expectedPlayers))
	}
	return nil
}

var ErrInvalidMatchInput = invalidInputError{}
var ErrIdempotencyConflict = errors.New("idempotency key was already used for a different request")

type invalidInputError struct{}

func (invalidInputError) Error() string { return "invalid match setup input" }

func invalidMatchInput(detail string) error {
	return fmt.Errorf("%w: %s", ErrInvalidMatchInput, detail)
}

func playersPerSideForFormat(formatCode string) (int, bool) {
	switch formatCode {
	case "5V5":
		return 5, true
	case "6V6":
		return 6, true
	case "8V8":
		return 8, true
	case "11V11":
		return 11, true
	default:
		return 0, false
	}
}
