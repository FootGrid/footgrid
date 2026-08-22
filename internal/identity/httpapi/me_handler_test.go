package identityhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FootGrid/footgrid/internal/identity"
	"github.com/FootGrid/footgrid/internal/platform/auth"
	"github.com/golang-jwt/jwt/v5"
)

type fakeRepository struct{}

func (fakeRepository) GetOrCreateMe(context.Context, string, string) (identity.Me, error) {
	return identity.Me{User: identity.User{ID: "11111111-1111-4111-8111-111111111111", CognitoSubject: "22222222-2222-4222-8222-222222222222", DisplayName: "Captain"}, Memberships: []identity.Membership{}}, nil
}

func TestMeHandlerRequiresAuthentication(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	MeHandler(fakeRepository{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", recorder.Code)
	}
}

func TestMeHandlerReturnsProvisionedProfile(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{Subject: "22222222-2222-4222-8222-222222222222", Claims: jwt.MapClaims{"name": "Captain"}}))
	MeHandler(fakeRepository{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
}
