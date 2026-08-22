package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct{ pool *pgxpool.Pool }

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (repository *PostgresRepository) GetOrCreateMe(ctx context.Context, cognitoSubject, displayName string) (Me, error) {
	if !isUUID(cognitoSubject) {
		return Me{}, errors.New("cognito subject must be a UUID")
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || len(displayName) > 100 {
		return Me{}, errors.New("display name must contain 1 to 100 characters")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Me{}, fmt.Errorf("begin current-user transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var user User
	if err := tx.QueryRow(ctx, `INSERT INTO identity.users (cognito_subject, display_name) VALUES ($1::uuid, $2) ON CONFLICT (cognito_subject) DO UPDATE SET display_name = EXCLUDED.display_name RETURNING id::text, cognito_subject::text, display_name`, cognitoSubject, displayName).Scan(&user.ID, &user.CognitoSubject, &user.DisplayName); err != nil {
		return Me{}, fmt.Errorf("upsert current user: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT id::text, organization_id::text, role::text, status::text FROM identity.organization_memberships WHERE user_id = $1::uuid AND status = 'ACTIVE' ORDER BY organization_id, id`, user.ID)
	if err != nil {
		return Me{}, fmt.Errorf("list current-user memberships: %w", err)
	}
	defer rows.Close()
	result := Me{User: user, Memberships: make([]Membership, 0)}
	for rows.Next() {
		var membership Membership
		if err := rows.Scan(&membership.ID, &membership.OrganizationID, &membership.Role, &membership.Status); err != nil {
			return Me{}, fmt.Errorf("scan membership: %w", err)
		}
		result.Memberships = append(result.Memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return Me{}, fmt.Errorf("read memberships: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Me{}, fmt.Errorf("commit current-user transaction: %w", err)
	}
	return result, nil
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
