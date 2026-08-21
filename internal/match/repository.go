package match

import "context"

// Repository methods that change the ledger must run in one database
// transaction. Append locks match_data.match_live_state before checking sequence.
type Repository interface {
	Create(ctx context.Context, input CreateInput) (Snapshot, error)
	GetSnapshot(ctx context.Context, matchID string) (Snapshot, error)
	ReplaceRoster(ctx context.Context, matchID string, roster Roster) error
	SetInitialLineups(ctx context.Context, matchID string, homeStarterIDs, awayStarterIDs []string) error
	Append(ctx context.Context, matchID string, command AppendEventCommand) (Event, Snapshot, error)
	Reverse(ctx context.Context, matchID, eventID, clientEventID string, expectedSequence int, reason string) (Event, Snapshot, error)
}

type CreateInput struct {
	OrganizationID        string
	VenueName             string
	FormatCode            string
	PlayersPerSide        int
	PeriodCount           int
	TotalDurationSeconds  int
	HomeDisplayName       string
	AwayDisplayName       string
}

func (input CreateInput) Validate() error {
	if input.OrganizationID == "" || input.HomeDisplayName == "" || input.AwayDisplayName == "" {
		return ErrInvalidMatchInput
	}
	if input.PlayersPerSide < 2 || input.PlayersPerSide > 11 || input.PeriodCount < 1 || input.PeriodCount > 4 {
		return ErrInvalidMatchInput
	}
	if input.TotalDurationSeconds < 600 || input.TotalDurationSeconds > 10800 {
		return ErrInvalidMatchInput
	}
	return nil
}

var ErrInvalidMatchInput = invalidInputError{}

type invalidInputError struct{}
func (invalidInputError) Error() string { return "invalid match setup input" }
