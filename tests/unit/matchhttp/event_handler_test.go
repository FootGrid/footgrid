package matchhttp_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FootGrid/footgrid/internal/match"
	matchhttp "github.com/FootGrid/footgrid/internal/match/httpapi"
)

type fakeEventAppender struct{ err error }

func (fake *fakeEventAppender) Append(context.Context, string, match.AppendEventCommand, match.Idempotency) (match.Event, match.Snapshot, error) {
	return match.Event{}, match.Snapshot{}, fake.err
}

type fakeEventReverser struct{ err error }

func (fake *fakeEventReverser) Reverse(context.Context, string, string, string, int, string, match.Idempotency) (match.Event, match.Snapshot, error) {
	return match.Event{}, match.Snapshot{}, fake.err
}

func TestAppendEventHandlerMapsMatchNotFound(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/matches/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/events", strings.NewReader(`{"client_event_id":"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","expected_sequence":0,"action_code":"GOAL","side":"HOME","subjects":[{"role":"SCORER","participant_id":"cccccccc-cccc-4ccc-8ccc-cccccccccccc"}]}`))
	request.Header.Set("Idempotency-Key", "append-event-request-001")
	recorder := httptest.NewRecorder()

	matchhttp.AppendEventHandler(&fakeEventAppender{err: match.ErrMatchNotFound}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestReverseEventHandlerMapsNotFoundErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "match", err: match.ErrMatchNotFound},
		{name: "event", err: match.ErrEventNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/matches/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/events/dddddddd-dddd-4ddd-8ddd-dddddddddddd/reverse", strings.NewReader(`{"client_event_id":"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","expected_sequence":1,"reason":"operator correction"}`))
			request.Header.Set("Idempotency-Key", "reverse-event-request-001")
			recorder := httptest.NewRecorder()

			matchhttp.ReverseEventHandler(&fakeEventReverser{err: test.err}).ServeHTTP(recorder, request)

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("expected 404, got %d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestAppendEventHandlerRejectsMalformedRequestBeforeRepository(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/matches/not-a-uuid/events", strings.NewReader(`{"action_code":"GOAL",}`))
	request.Header.Set("Idempotency-Key", "append-event-request-001")
	recorder := httptest.NewRecorder()

	matchhttp.AppendEventHandler(&fakeEventAppender{err: errors.New("repository must not be called")}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", recorder.Code, recorder.Body.String())
	}
}
