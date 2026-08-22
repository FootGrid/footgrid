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

type fakeSetupRepository struct{ err error }

func (fake *fakeSetupRepository) ReplaceRoster(context.Context, string, match.Roster, match.Idempotency) (match.Roster, error) {
	return match.Roster{}, fake.err
}

func (fake *fakeSetupRepository) SetInitialLineups(context.Context, string, []string, []string, match.Idempotency) (match.Roster, error) {
	return match.Roster{}, fake.err
}

func (fake *fakeSetupRepository) MarkReady(context.Context, string, match.Idempotency) (match.Match, error) {
	return match.Match{}, fake.err
}

func (fake *fakeSetupRepository) StartLiveSession(context.Context, string, match.Idempotency) (match.Snapshot, error) {
	return match.Snapshot{}, fake.err
}

func TestSetupHandlersMapMissingMatchToNotFound(t *testing.T) {
	rosterHandler, _, _ := matchhttp.SetupHandlers(&fakeSetupRepository{err: match.ErrMatchNotFound})
	request := httptest.NewRequest(http.MethodPut, "/v1/matches/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/roster", strings.NewReader(`{"home":[],"away":[]}`))
	request.SetPathValue("matchId", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	request.Header.Set("Idempotency-Key", "setup-roster-request-001")
	recorder := httptest.NewRecorder()

	rosterHandler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestSetupHandlersRejectOversizedBody(t *testing.T) {
	rosterHandler, _, _ := matchhttp.SetupHandlers(&fakeSetupRepository{err: errors.New("repository must not be called")})
	request := httptest.NewRequest(http.MethodPut, "/v1/matches/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/roster", strings.NewReader(`{"home":[],"away":[]}`+strings.Repeat(" ", (1<<20)+1)))
	request.Header.Set("Idempotency-Key", "setup-roster-request-001")
	recorder := httptest.NewRecorder()

	rosterHandler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", recorder.Code, recorder.Body.String())
	}
}
