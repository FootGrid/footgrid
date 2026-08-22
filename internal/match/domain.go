// Package match contains the domain rules for the live match write model.
// Transport, database, EventBridge and Lambda concerns must stay outside it.
package match

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	ID                  string `json:"id"`
	Side                Side   `json:"side"`
	ShirtNumber         int    `json:"shirt_number"`
	DisplayName         string `json:"display_name"`
	PositionCode        string `json:"position_code,omitempty"`
	ParticipationStatus string `json:"participation_status"`
	PitchSlot           string `json:"pitch_slot,omitempty"`
}

type Roster struct {
	Home []Participant `json:"home"`
	Away []Participant `json:"away"`
}

func (r Roster) Validate(playersPerSide int) error {
	if playersPerSide < 2 || playersPerSide > 11 {
		return fmt.Errorf("players per side must be between 2 and 11")
	}
	participantIDs := make(map[string]struct{}, len(r.Home)+len(r.Away))
	for _, rosterSide := range []struct {
		side         Side
		participants []Participant
	}{
		{side: Home, participants: r.Home},
		{side: Away, participants: r.Away},
	} {
		side := rosterSide.side
		participants := rosterSide.participants
		if len(participants) < playersPerSide {
			return fmt.Errorf("%s requires at least %d participants", side, playersPerSide)
		}
		shirts := make(map[int]struct{}, len(participants))
		for _, participant := range participants {
			if !isUUID(participant.ID) || strings.TrimSpace(participant.DisplayName) == "" {
				return errors.New("each participant requires a UUID id and display name")
			}
			if participant.Side != side {
				return fmt.Errorf("participant %s must belong to %s", participant.ID, side)
			}
			if _, exists := participantIDs[participant.ID]; exists {
				return fmt.Errorf("duplicate participant id %s", participant.ID)
			}
			participantIDs[participant.ID] = struct{}{}
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

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		switch index {
		case 8, 13, 18, 23:
			if character != '-' {
				return false
			}
		default:
			if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
				return false
			}
		}
	}
	return true
}

type Subject struct {
	Role          string `json:"role"`
	ParticipantID string `json:"participant_id"`
}

type AppendEventCommand struct {
	ClientEventID    string         `json:"client_event_id"`
	ExpectedSequence int            `json:"expected_sequence"`
	ActionCode       string         `json:"action_code"`
	Side             Side           `json:"side"`
	Subjects         []Subject      `json:"subjects"`
	Qualifiers       map[string]any `json:"qualifiers,omitempty"`
}

func (command AppendEventCommand) Validate() error {
	if strings.TrimSpace(command.ClientEventID) == "" {
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
		if strings.TrimSpace(subject.ParticipantID) == "" || strings.TrimSpace(subject.Role) == "" {
			return errors.New("event subject role and participant_id are required")
		}
		roles[subject.Role] = true
	}
	if command.ActionCode == "" {
		return errors.New("action_code is required")
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
		reason, ok := command.Qualifiers["reason"].(string)
		if !ok || strings.TrimSpace(reason) == "" {
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

func (event Event) ScoreDelta() int {
	if event.Command.ActionCode == "GOAL" {
		return 1
	}
	if event.Command.ActionCode == "SCORE_ADJUSTMENT" {
		if delta, ok := event.Command.Qualifiers["score_delta"].(float64); ok {
			return int(delta)
		}
	}
	return 0
}

func (event Event) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID         string         `json:"id"`
		Sequence   int            `json:"sequence"`
		ActionCode string         `json:"action_code"`
		Side       Side           `json:"side"`
		Subjects   []Subject      `json:"subjects"`
		Qualifiers map[string]any `json:"qualifiers,omitempty"`
	}{event.ID, event.Sequence, event.Command.ActionCode, event.Command.Side, event.Command.Subjects, event.Command.Qualifiers})
}

func (event *Event) UnmarshalJSON(data []byte) error {
	var value struct {
		ID         string         `json:"id"`
		Sequence   int            `json:"sequence"`
		ActionCode string         `json:"action_code"`
		Side       Side           `json:"side"`
		Subjects   []Subject      `json:"subjects"`
		Qualifiers map[string]any `json:"qualifiers,omitempty"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	event.ID, event.Sequence = value.ID, value.Sequence
	event.Command = AppendEventCommand{ActionCode: value.ActionCode, Side: value.Side, Subjects: value.Subjects, Qualifiers: value.Qualifiers}
	return nil
}

type Snapshot struct {
	MatchID       string `json:"match_id"`
	Status        Status `json:"status"`
	EventSequence int    `json:"event_sequence"`
	HomeScore     int    `json:"home_score"`
	AwayScore     int    `json:"away_score"`
}

var ErrSequenceConflict = errors.New("match event sequence conflict")
var ErrMatchNotLive = errors.New("match is not live")
var ErrMatchNotReady = errors.New("match is not ready")
var ErrEventNotReversible = errors.New("event is not reversible")
var ErrEventAlreadyReversed = errors.New("event has already been reversed")

// StartLiveSession performs the only legal transition into live scoring.
func StartLiveSession(snapshot Snapshot) (Snapshot, error) {
	if snapshot.Status != Ready {
		return Snapshot{}, ErrMatchNotReady
	}
	snapshot.Status = Live
	return snapshot, nil
}

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

// ReverseEvent creates a compensating event without mutating the original.
func ReverseEvent(snapshot Snapshot, original Event, clientEventID, reason, reversalID string) (Event, Snapshot, error) {
	if snapshot.Status != Live {
		return Event{}, Snapshot{}, ErrMatchNotLive
	}
	if strings.TrimSpace(reason) == "" {
		return Event{}, Snapshot{}, errors.New("reversal reason is required")
	}
	delta := original.ScoreDelta()
	if delta == 0 {
		return Event{}, Snapshot{}, ErrEventNotReversible
	}
	if original.Command.Side == Home && snapshot.HomeScore < delta || original.Command.Side == Away && snapshot.AwayScore < delta {
		return Event{}, Snapshot{}, errors.New("reversal would make the score negative")
	}
	snapshot.EventSequence++
	if original.Command.Side == Home {
		snapshot.HomeScore -= delta
	} else {
		snapshot.AwayScore -= delta
	}
	return Event{ID: reversalID, Sequence: snapshot.EventSequence, Command: AppendEventCommand{
		ClientEventID:    clientEventID,
		ExpectedSequence: snapshot.EventSequence - 1,
		ActionCode:       "EVENT_REVERSAL",
		Side:             original.Command.Side,
		Subjects:         original.Command.Subjects,
		Qualifiers:       map[string]any{"reason": reason, "reverses_event_id": original.ID},
	}}, snapshot, nil
}
