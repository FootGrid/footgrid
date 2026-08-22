package match_test

import (
	"errors"
	"testing"

	"github.com/FootGrid/footgrid/internal/match"
)

func TestApplyGoal(t *testing.T) {
	snapshot := match.Snapshot{MatchID: "match-1", Status: match.Live, EventSequence: 4, HomeScore: 1, AwayScore: 0}
	event, updated, err := match.ApplyEvent(snapshot, match.AppendEventCommand{
		ClientEventID: "event-1", ExpectedSequence: 4, ActionCode: "GOAL", Side: match.Home,
		Subjects: []match.Subject{{Role: "SCORER", ParticipantID: "participant-1"}},
	}, "server-event-1")
	if err != nil {
		t.Fatal(err)
	}
	if event.Sequence != 5 || updated.HomeScore != 2 || updated.EventSequence != 5 {
		t.Fatalf("unexpected event/snapshot: %#v %#v", event, updated)
	}
}

func TestApplyEventRejectsStaleSequence(t *testing.T) {
	_, _, err := match.ApplyEvent(match.Snapshot{Status: match.Live, EventSequence: 2}, match.AppendEventCommand{
		ClientEventID: "event-1", ExpectedSequence: 1, ActionCode: "GOAL", Side: match.Home,
		Subjects: []match.Subject{{Role: "SCORER", ParticipantID: "participant-1"}},
	}, "server-event-1")
	if !errors.Is(err, match.ErrSequenceConflict) {
		t.Fatalf("expected sequence conflict, got %v", err)
	}
}

func TestApplyScoreAdjustmentChangesDerivedScore(t *testing.T) {
	_, updated, err := match.ApplyEvent(match.Snapshot{Status: match.Live, EventSequence: 1, HomeScore: 2}, match.AppendEventCommand{
		ClientEventID: "event-1", ExpectedSequence: 1, ActionCode: "SCORE_ADJUSTMENT", Side: match.Home,
		Subjects:   []match.Subject{{Role: "PRIMARY", ParticipantID: "participant-1"}},
		Qualifiers: map[string]any{"reason": "official correction", "score_delta": -1},
	}, "server-event-1")
	if err != nil {
		t.Fatal(err)
	}
	if updated.HomeScore != 1 || updated.EventSequence != 2 {
		t.Fatalf("unexpected adjusted snapshot: %#v", updated)
	}
}

func TestApplyScoreAdjustmentRejectsUnderflow(t *testing.T) {
	_, _, err := match.ApplyEvent(match.Snapshot{Status: match.Live, HomeScore: 0}, match.AppendEventCommand{
		ClientEventID: "event-1", ActionCode: "SCORE_ADJUSTMENT", Side: match.Home,
		Subjects:   []match.Subject{{Role: "PRIMARY", ParticipantID: "participant-1"}},
		Qualifiers: map[string]any{"reason": "official correction", "score_delta": -1},
	}, "server-event-1")
	if !errors.Is(err, match.ErrScoreUnderflow) {
		t.Fatalf("expected score underflow, got %v", err)
	}
}

func TestStartLiveSessionRequiresReadyMatch(t *testing.T) {
	if _, err := match.StartLiveSession(match.Snapshot{Status: match.Draft}); !errors.Is(err, match.ErrMatchNotReady) {
		t.Fatalf("expected not-ready error, got %v", err)
	}
	snapshot, err := match.StartLiveSession(match.Snapshot{MatchID: "match-1", Status: match.Ready, EventSequence: 2})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != match.Live || snapshot.EventSequence != 2 {
		t.Fatalf("unexpected live snapshot: %#v", snapshot)
	}
}

func TestReverseGoalAppendsCompensatingEvent(t *testing.T) {
	snapshot := match.Snapshot{Status: match.Live, EventSequence: 4, HomeScore: 2, AwayScore: 1}
	original := match.Event{ID: "goal-1", Sequence: 3, Command: match.AppendEventCommand{
		ActionCode: "GOAL", Side: match.Home,
		Subjects: []match.Subject{{Role: "SCORER", ParticipantID: "player-1"}},
	}}
	event, updated, err := match.ReverseEvent(snapshot, original, "reversal-1", "wrong scorer", "reversal-server-1")
	if err != nil {
		t.Fatal(err)
	}
	if event.Sequence != 5 || event.Command.ActionCode != "EVENT_REVERSAL" || updated.HomeScore != 1 || updated.EventSequence != 5 {
		t.Fatalf("unexpected reversal: %#v %#v", event, updated)
	}
}

func TestReverseRejectsNonScoringEvent(t *testing.T) {
	_, _, err := match.ReverseEvent(match.Snapshot{Status: match.Live}, match.Event{Command: match.AppendEventCommand{ActionCode: "ASSIST", Side: match.Home}}, "reversal-1", "correction", "server-1")
	if !errors.Is(err, match.ErrEventNotReversible) {
		t.Fatalf("expected non-reversible error, got %v", err)
	}
}

func TestSubstitutionRequiresBothParticipants(t *testing.T) {
	err := (match.AppendEventCommand{ClientEventID: "event-1", ActionCode: "SUBSTITUTION", Side: match.Home, Subjects: []match.Subject{{Role: "PLAYER_ON", ParticipantID: "participant-1"}}}).Validate()
	if err == nil {
		t.Fatal("expected validation failure")
	}
}

func TestRosterValidateAcceptsTwoCompleteSides(t *testing.T) {
	roster := match.Roster{
		Home: []match.Participant{
			{ID: "11111111-1111-4111-8111-111111111111", Side: match.Home, ShirtNumber: 1, DisplayName: "Home keeper"},
			{ID: "22222222-2222-4222-8222-222222222222", Side: match.Home, ShirtNumber: 2, DisplayName: "Home player"},
		},
		Away: []match.Participant{
			{ID: "33333333-3333-4333-8333-333333333333", Side: match.Away, ShirtNumber: 1, DisplayName: "Away keeper"},
			{ID: "44444444-4444-4444-8444-444444444444", Side: match.Away, ShirtNumber: 2, DisplayName: "Away player"},
		},
	}
	if err := roster.Validate(2); err != nil {
		t.Fatalf("expected valid roster, got %v", err)
	}
}

func TestRosterValidateRejectsAPIMismatches(t *testing.T) {
	valid := match.Roster{
		Home: []match.Participant{
			{ID: "11111111-1111-4111-8111-111111111111", Side: match.Home, ShirtNumber: 1, DisplayName: "Home keeper"},
			{ID: "22222222-2222-4222-8222-222222222222", Side: match.Home, ShirtNumber: 2, DisplayName: "Home player"},
		},
		Away: []match.Participant{
			{ID: "33333333-3333-4333-8333-333333333333", Side: match.Away, ShirtNumber: 1, DisplayName: "Away keeper"},
			{ID: "44444444-4444-4444-8444-444444444444", Side: match.Away, ShirtNumber: 2, DisplayName: "Away player"},
		},
	}
	tests := []struct {
		name   string
		roster match.Roster
	}{
		{
			name: "duplicate shirt number on one side",
			roster: match.Roster{Home: []match.Participant{
				valid.Home[0],
				{ID: valid.Home[1].ID, Side: match.Home, ShirtNumber: 1, DisplayName: "Home player"},
			}, Away: valid.Away},
		},
		{
			name: "non UUID participant id",
			roster: match.Roster{Home: []match.Participant{
				{ID: "not-a-uuid", Side: match.Home, ShirtNumber: 1, DisplayName: "Home keeper"}, valid.Home[1],
			}, Away: valid.Away},
		},
		{
			name: "participant placed on the wrong side",
			roster: match.Roster{Home: []match.Participant{
				{ID: valid.Home[0].ID, Side: match.Away, ShirtNumber: 1, DisplayName: "Home keeper"}, valid.Home[1],
			}, Away: valid.Away},
		},
		{
			name: "participant id used by both sides",
			roster: match.Roster{Home: valid.Home, Away: []match.Participant{
				{ID: valid.Home[0].ID, Side: match.Away, ShirtNumber: 1, DisplayName: "Away keeper"}, valid.Away[1],
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
