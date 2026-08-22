package identity

import "context"

type User struct {
	ID             string `json:"id"`
	CognitoSubject string `json:"cognito_subject"`
	DisplayName    string `json:"display_name"`
}

type Membership struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	Role           string `json:"role"`
	Status         string `json:"status"`
}

type Me struct {
	User        User         `json:"user"`
	Memberships []Membership `json:"memberships"`
}

type Repository interface {
	GetOrCreateMe(ctx context.Context, cognitoSubject, displayName string) (Me, error)
}
