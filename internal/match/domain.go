// Package match contains the domain rules for the live match write model.
// Transport, database, EventBridge and Lambda concerns must stay outside it.
package match

import (
	"errors"
	"fmt"
)

type Side string

const (
	Home Side = "HOME"
	Away Side = "AWAY"
)

type Status string

const (
	Draft     Status = "DRAFT"
	Ready     Status = "READY"
	Live      Status = "LIVE"
	Paused    Status = "PAUSED"
	Completed Status = "COMPLETED"
	Finalized Status = "FINALIZED"
)

type Participant struct {
	ID                  string
	Side                Side
	ShirtNumber         int
	DisplayName         string
	PositionCode        string
	ParticipationStatus string
	PitchSlot           string
}

type Roster struct {
	Home []Participant
	Away []Participant
}

func (r Roster) Validate(playersPerSide int) error {
	for _, side := range [][]Participant{r.Home, r.Away} {
		if len(side) < playersPerSide {
			return fmt.Errorf("each side requires at least %d participants", playersPerSide)
		}
		shirts := make(map[int]struct{}, len(side))
		for _, participant := range side {
			if participant.ID == "" || participant.DisplayName == "" {
				return errors.New("each participant requires id and display name")
			}
			if participant.ShirtNumber < 1 || participant.ShirtNumber > 99 {
				return errors.New("shirt number must be between 1 and 99")
			}
			if _, exists := shirts[participant.ShirtNumber]; exists {
				return fmt.Errorf("duplicate shirt number %d", participant.ShirtNumber)
			}
			shirts[participant.ShirtNumber] = struct{}{}
		}
	}
	return nil
}

type Subject struct {
	Role          string
	ParticipantID string
}

type AppendEventCommand struct {
	ClientEventID   string
	ExpectedSequence int
	ActionCode      string
	Side            Side
	Subjects        []Subject
	Qualifiers      map[string]any
}

func (command AppendEventCommand) Validate() error {
	if command.ClientEventID == "" {
		return errors.New("client_event_id is required")
	}
	if command.ExpectedSequence < 0 {
		return errors.New("expected_sequence must not be negative")
	}
	if command.Side != Home && command.Side != Away {
		return errors.New("side must be HOME or AWAY")
	}
	if len(command.Subjects) == 0 {
		return errors.New("at least one event subject is required")
	}
	roles := map[string]bool{}
	for _, subject := range command.Subjects {
		if subject.ParticipantID == "" || subject.Role == "" {
			return errors.New("event subject role and participant_id are required")
		}
		roles[subject.Role] = true
	}
	switch command.ActionCode {
	case "GOAL":
		if !roles["SCORER"] {
			return errors.New("GOAL requires SCORER")
		}
	case "SUBSTITUTION":
		if !roles["PLAYER_ON"] || !roles["PLAYER_OFF"] {
			return errors.New("SUBSTITUTION requires PLAYER_ON and PLAYER_OFF")
		}
	case "SCORE_ADJUSTMENT":
		if _, ok := command.Qualifiers["reason"].(string); !ok {
			return errors.New("SCORE_ADJUSTMENT requires a reason")
		}
	}
	return nil
}

type Event struct {
	ID       string
	Sequence int
	Command  AppendEventCommand
}

type Snapshot struct {
	MatchID       string
	Status        Status
	EventSequence int
	HomeScore     int
	AwayScore     int
}

var ErrSequenceConflict = errors.New("match event sequence conflict")
var ErrMatchNotLive = errors.New("match is not live")

// ApplyEvent is deterministic and has no I/O. The persistence adapter invokes
// it while holding the match_live_state row lock.
func ApplyEvent(snapshot Snapshot, command AppendEventCommand, eventID string) (Event, Snapshot, error) {
	if err := command.Validate(); err != nil {
		return Event{}, Snapshot{}, err
	}
	if snapshot.Status != Live {
		return Event{}, Snapshot{}, ErrMatchNotLive
	}
	if snapshot.EventSequence != command.ExpectedSequence {
		return Event{}, Snapshot{}, ErrSequenceConflict
	}

	snapshot.EventSequence++
	if command.ActionCode == "GOAL" {
		if command.Side == Home {
			snapshot.HomeScore++
		} else {
			snapshot.AwayScore++
		}
	}
	return Event{ID: eventID, Sequence: snapshot.EventSequence, Command: command}, snapshot, nil
}
