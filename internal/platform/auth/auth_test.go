package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FootGrid/footgrid/internal/platform/httpapi"
	"github.com/golang-jwt/jwt/v5"
)

type fakeVerifier struct {
	principal Principal
	err       error
}

func (verifier fakeVerifier) Verify(context.Context, string) (Principal, error) {
	return verifier.principal, verifier.err
}

func TestMiddlewareLeavesHealthPublic(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	Middleware(nil, httpapi.HealthHandler("test", nil)).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected public health, got %d", recorder.Code)
	}
}

func TestMiddlewareRejectsMissingBearerToken(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/matches", nil)
	Middleware(fakeVerifier{}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler must not be called") })).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", recorder.Code)
	}
}

func TestMiddlewarePropagatesVerifiedPrincipal(t *testing.T) {
	principal := Principal{Subject: "user-1", Claims: jwt.MapClaims{"sub": "user-1"}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/matches", nil)
	request.Header.Set("Authorization", "Bearer signed-token")
	called := false
	Middleware(fakeVerifier{principal: principal}, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		called = true
		got, ok := FromContext(request.Context())
		if !ok || got.Subject != principal.Subject {
			t.Errorf("unexpected principal: %#v", got)
		}
		writer.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, request)
	if !called || recorder.Code != http.StatusNoContent {
		t.Fatalf("expected authenticated request, called=%v status=%d", called, recorder.Code)
	}
}

func TestMiddlewareRejectsVerifierFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/matches", nil)
	request.Header.Set("Authorization", "Bearer invalid")
	Middleware(fakeVerifier{err: errors.New("invalid")}, http.NotFoundHandler()).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", recorder.Code)
	}
}
