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

func TestRosterValidateAcceptsTwoCompleteSides(t *testing.T) {
	roster := Roster{
		Home: []Participant{
			{ID: "11111111-1111-4111-8111-111111111111", Side: Home, ShirtNumber: 1, DisplayName: "Home keeper"},
			{ID: "22222222-2222-4222-8222-222222222222", Side: Home, ShirtNumber: 2, DisplayName: "Home player"},
		},
		Away: []Participant{
			{ID: "33333333-3333-4333-8333-333333333333", Side: Away, ShirtNumber: 1, DisplayName: "Away keeper"},
			{ID: "44444444-4444-4444-8444-444444444444", Side: Away, ShirtNumber: 2, DisplayName: "Away player"},
		},
	}
	if err := roster.Validate(2); err != nil {
		t.Fatalf("expected valid roster, got %v", err)
	}
}

func TestRosterValidateRejectsAPIMismatches(t *testing.T) {
	valid := Roster{
		Home: []Participant{
			{ID: "11111111-1111-4111-8111-111111111111", Side: Home, ShirtNumber: 1, DisplayName: "Home keeper"},
			{ID: "22222222-2222-4222-8222-222222222222", Side: Home, ShirtNumber: 2, DisplayName: "Home player"},
		},
		Away: []Participant{
			{ID: "33333333-3333-4333-8333-333333333333", Side: Away, ShirtNumber: 1, DisplayName: "Away keeper"},
			{ID: "44444444-4444-4444-8444-444444444444", Side: Away, ShirtNumber: 2, DisplayName: "Away player"},
		},
	}
	tests := []struct {
		name   string
		roster Roster
	}{
		{
			name: "duplicate shirt number on one side",
			roster: Roster{Home: []Participant{
				valid.Home[0],
				{ID: valid.Home[1].ID, Side: Home, ShirtNumber: 1, DisplayName: "Home player"},
			}, Away: valid.Away},
		},
		{
			name: "non UUID participant id",
			roster: Roster{Home: []Participant{
				{ID: "not-a-uuid", Side: Home, ShirtNumber: 1, DisplayName: "Home keeper"}, valid.Home[1],
			}, Away: valid.Away},
		},
		{
			name: "participant placed on the wrong side",
			roster: Roster{Home: []Participant{
				{ID: valid.Home[0].ID, Side: Away, ShirtNumber: 1, DisplayName: "Home keeper"}, valid.Home[1],
			}, Away: valid.Away},
		},
		{
			name: "participant id used by both sides",
			roster: Roster{Home: valid.Home, Away: []Participant{
				{ID: valid.Home[0].ID, Side: Away, ShirtNumber: 1, DisplayName: "Away keeper"}, valid.Away[1],
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.roster.Validate(2); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
}
