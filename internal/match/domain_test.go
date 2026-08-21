package match

import (
	"errors"
	"testing"
)

func TestApplyGoal(t *testing.T) {
	snapshot := Snapshot{MatchID: "match-1", Status: Live, EventSequence: 4, HomeScore: 1, AwayScore: 0}
	event, updated, err := ApplyEvent(snapshot, AppendEventCommand{
		ClientEventID: "event-1", ExpectedSequence: 4, ActionCode: "GOAL", Side: Home,
		Subjects: []Subject{{Role: "SCORER", ParticipantID: "participant-1"}},
	}, "server-event-1")
	if err != nil {
		t.Fatal(err)
	}
	if event.Sequence != 5 || updated.HomeScore != 2 || updated.EventSequence != 5 {
		t.Fatalf("unexpected event/snapshot: %#v %#v", event, updated)
	}
}

func TestApplyEventRejectsStaleSequence(t *testing.T) {
	_, _, err := ApplyEvent(Snapshot{Status: Live, EventSequence: 2}, AppendEventCommand{
		ClientEventID: "event-1", ExpectedSequence: 1, ActionCode: "GOAL", Side: Home,
		Subjects: []Subject{{Role: "SCORER", ParticipantID: "participant-1"}},
	}, "server-event-1")
	if !errors.Is(err, ErrSequenceConflict) {
		t.Fatalf("expected sequence conflict, got %v", err)
	}
}

func TestSubstitutionRequiresBothParticipants(t *testing.T) {
	err := (AppendEventCommand{ClientEventID: "event-1", ActionCode: "SUBSTITUTION", Side: Home, Subjects: []Subject{{Role: "PLAYER_ON", ParticipantID: "participant-1"}}}).Validate()
	if err == nil {
		t.Fatal("expected validation failure")
	}
}
