package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	platformhttp "github.com/FootGrid/footgrid/internal/platform/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrForbidden = errors.New("organization access denied")

type OrganizationAuthorizer interface {
	AuthorizeOrganization(ctx context.Context, subject, organizationID string, roles ...string) error
}

type MatchAuthorizer interface {
	AuthorizeMatch(ctx context.Context, subject, matchID string, roles ...string) error
}

type DatabaseAuthorizer struct{ pool *pgxpool.Pool }

func NewDatabaseAuthorizer(pool *pgxpool.Pool) *DatabaseAuthorizer {
	return &DatabaseAuthorizer{pool: pool}
}

func (authorizer *DatabaseAuthorizer) AuthorizeOrganization(ctx context.Context, subject, organizationID string, roles ...string) error {
	if !isUUID(subject) || !isUUID(organizationID) {
		return ErrForbidden
	}
	var allowed bool
	err := authorizer.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM identity.users u JOIN identity.organization_memberships m ON m.user_id = u.id WHERE u.cognito_subject = $1::uuid AND m.organization_id = $2::uuid AND m.status = 'ACTIVE' AND m.role::text = ANY($3::text[]))`, subject, organizationID, roles).Scan(&allowed)
	if err != nil {
		return fmt.Errorf("authorize organization membership: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func (authorizer *DatabaseAuthorizer) AuthorizeMatch(ctx context.Context, subject, matchID string, roles ...string) error {
	if !isUUID(subject) || !isUUID(matchID) {
		return ErrForbidden
	}
	var allowed bool
	err := authorizer.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM match_data.matches x JOIN identity.users u ON u.cognito_subject = $1::uuid JOIN identity.organization_memberships m ON m.organization_id = x.organization_id AND m.user_id = u.id WHERE x.id = $2::uuid AND m.status = 'ACTIVE' AND m.role::text = ANY($3::text[]))`, subject, matchID, roles).Scan(&allowed)
	if err != nil {
		return fmt.Errorf("authorize match membership: %w", err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func RequireMatch(authorizer MatchAuthorizer, roles []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := FromContext(request.Context())
		if !ok {
			platformhttp.WriteProblem(writer, http.StatusUnauthorized, "unauthorized", "authenticated principal is required", request)
			return
		}
		if authorizer == nil {
			platformhttp.WriteProblem(writer, http.StatusServiceUnavailable, "auth-unavailable", "match authorizer is not configured", request)
			return
		}
		if err := authorizer.AuthorizeMatch(request.Context(), principal.Subject, request.PathValue("matchId"), roles...); err != nil {
			if errors.Is(err, ErrForbidden) {
				platformhttp.WriteProblem(writer, http.StatusForbidden, "forbidden", "membership does not permit this match operation", request)
			} else {
				platformhttp.WriteProblem(writer, http.StatusInternalServerError, "internal-error", "unable to authorize match access", request)
			}
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}
