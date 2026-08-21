package matchhttp_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FootGrid/footgrid/internal/match"
	matchhttp "github.com/FootGrid/footgrid/internal/match/httpapi"
	platformhttpapi "github.com/FootGrid/footgrid/internal/platform/httpapi"
)

const validCreateMatchJSON = `{
  "organization_id":"11111111-1111-4111-8111-111111111111",
  "venue_name":"Turf Arena",
  "format_code":"6V6",
  "players_per_side":6,
  "period_count":2,
  "total_duration_seconds":2400,
  "home":{"display_name":"Gachibowli FC"},
  "away":{"display_name":"Kondapur SC"}
}`

type fakeDraftCreator struct {
	input       match.CreateInput
	idempotency match.Idempotency
	response    match.Match
	err         error
	calls       int
}

func (creator *fakeDraftCreator) Create(_ context.Context, input match.CreateInput, idempotency match.Idempotency) (match.Match, error) {
	creator.calls++
	creator.input = input
	creator.idempotency = idempotency
	return creator.response, creator.err
}

func TestCreateHandlerCreatesDraftFromAPIPayload(t *testing.T) {
	creator := &fakeDraftCreator{response: match.Match{
		ID:             "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		OrganizationID: "11111111-1111-4111-8111-111111111111",
		Status:         match.Draft,
		Home:           match.MatchSide{DisplayName: "Gachibowli FC"},
		Away:           match.MatchSide{DisplayName: "Kondapur SC"},
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/matches", strings.NewReader(validCreateMatchJSON))
	request.Header.Set("Idempotency-Key", "create-draft-request-001")
	platformhttpapi.WithMiddleware(matchhttp.CreateHandler(creator)).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if creator.calls != 1 {
		t.Fatalf("expected one create call, got %d", creator.calls)
	}
	if creator.input.FormatCode != "6V6" || creator.input.TotalDurationSeconds != 2400 || creator.input.PeriodCount != 2 {
		t.Fatalf("unexpected converted input: %#v", creator.input)
	}
	expectedHash := sha256.Sum256([]byte(validCreateMatchJSON))
	if string(creator.idempotency.RequestHash) != string(expectedHash[:]) {
		t.Fatal("expected hash of the submitted request body")
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("expected JSON response, got %q", got)
	}
}

func TestCreateHandlerRejectsInvalidRequestsBeforePersistence(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		idempotencyKey string
		wantStatus     int
	}{
		{name: "missing idempotency key", body: validCreateMatchJSON, wantStatus: http.StatusUnprocessableEntity},
		{name: "invalid match dimensions", body: strings.Replace(validCreateMatchJSON, `"players_per_side":6`, `"players_per_side":5`, 1), idempotencyKey: "create-draft-request-001", wantStatus: http.StatusUnprocessableEntity},
		{name: "unknown JSON field", body: `{"organization_id":"11111111-1111-4111-8111-111111111111","unexpected":true}`, idempotencyKey: "create-draft-request-001", wantStatus: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			creator := &fakeDraftCreator{}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/v1/matches", strings.NewReader(test.body))
			request.Header.Set("Idempotency-Key", test.idempotencyKey)
			platformhttpapi.WithMiddleware(matchhttp.CreateHandler(creator)).ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus {
				t.Fatalf("expected %d, got %d: %s", test.wantStatus, recorder.Code, recorder.Body.String())
			}
			if creator.calls != 0 {
				t.Fatal("invalid request must not reach persistence")
			}
		})
	}
}

func TestCreateHandlerReturnsConflictForChangedIdempotentRequest(t *testing.T) {
	creator := &fakeDraftCreator{err: match.ErrIdempotencyConflict}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/matches", strings.NewReader(validCreateMatchJSON))
	request.Header.Set("Idempotency-Key", "create-draft-request-001")
	platformhttpapi.WithMiddleware(matchhttp.CreateHandler(creator)).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateHandlerMapsRepositoryValidationToUnprocessableEntity(t *testing.T) {
	creator := &fakeDraftCreator{err: errors.New("wrapped: " + match.ErrInvalidMatchInput.Error())}
	// The fake deliberately returns the sentinel as a wrapped error to cover the
	// handler's repository-validation branch rather than JSON validation above.
	creator.err = errors.Join(match.ErrInvalidMatchInput, creator.err)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/matches", strings.NewReader(validCreateMatchJSON))
	request.Header.Set("Idempotency-Key", "create-draft-request-001")
	platformhttpapi.WithMiddleware(matchhttp.CreateHandler(creator)).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", recorder.Code, recorder.Body.String())
	}
}
