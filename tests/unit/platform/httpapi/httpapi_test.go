package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FootGrid/footgrid/internal/platform/httpapi"
)

func TestHealthHandler(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	httpapi.WithMiddleware(httpapi.HealthHandler("match-api", nil)).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	if recorder.Header().Get("X-Request-ID") == "" {
		t.Fatal("expected trace id response header")
	}
}
