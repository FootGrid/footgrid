package identityhttp

import (
	"net/http"
	"strings"

	"github.com/FootGrid/footgrid/internal/identity"
	"github.com/FootGrid/footgrid/internal/platform/auth"
	platformhttp "github.com/FootGrid/footgrid/internal/platform/httpapi"
)

func MeHandler(repository identity.Repository) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := auth.FromContext(request.Context())
		if !ok {
			platformhttp.WriteProblem(writer, http.StatusUnauthorized, "unauthorized", "authenticated principal is required", request)
			return
		}
		displayName := principal.Subject
		if value, ok := principal.Claims["name"].(string); ok && strings.TrimSpace(value) != "" {
			displayName = value
		}
		result, err := repository.GetOrCreateMe(request.Context(), principal.Subject, displayName)
		if err != nil {
			platformhttp.WriteProblem(writer, http.StatusInternalServerError, "internal-error", "unable to load current user", request)
			return
		}
		platformhttp.WriteJSON(writer, http.StatusOK, result)
	})
}
