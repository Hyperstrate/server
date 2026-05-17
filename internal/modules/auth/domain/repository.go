package domain

import "context"

type OrganizationRepository interface {
	Count(ctx context.Context) (int64, error)
	Create(ctx context.Context, o *Organization) error
	FindByID(ctx context.Context, id string) (*Organization, error)
	FindBySlug(ctx context.Context, slug string) (*Organization, error)
	List(ctx context.Context, offset, limit int) ([]Organization, int64, error)
	Update(ctx context.Context, o *Organization) error
	Delete(ctx context.Context, id string) error
}

type APIKeyRepository interface {
	// ListByOrg returns API keys for orgID, optionally filtered by routerID or teamID.
	// Empty filter values are ignored.
	ListByOrg(ctx context.Context, orgID, routerID, teamID string, offset, limit int) ([]APIKey, int64, error)
	Create(ctx context.Context, k *APIKey) error
	FindByID(ctx context.Context, orgID, id string) (*APIKey, error)
	FindByKeyHash(ctx context.Context, hash string) (*APIKey, error)
	Update(ctx context.Context, k *APIKey) error
	Delete(ctx context.Context, orgID, id string) error
	DeleteByRouterID(ctx context.Context, routerID string) error
}

type VirtualKeyRepository interface {
	// List returns virtual keys for orgID, optionally filtered by routerID.
	// When routerID is empty all virtual keys for the org are returned.
	List(ctx context.Context, orgID, routerID, teamID string, offset, limit int) ([]VirtualKey, int64, error)
	ListByTeamID(ctx context.Context, teamID string) ([]VirtualKey, error)
	Create(ctx context.Context, k *VirtualKey) error
	FindByID(ctx context.Context, orgID, id string) (*VirtualKey, error)
	FindByKeyHash(ctx context.Context, hash string) (*VirtualKey, error)
	Update(ctx context.Context, k *VirtualKey) error
	Delete(ctx context.Context, orgID, id string) error
}

type TeamRepository interface {
	ListByOrgID(ctx context.Context, orgID, query string, offset, limit int) ([]Team, int64, error)
	ListByIDs(ctx context.Context, orgID string, ids []string) ([]Team, error)
	Create(ctx context.Context, t *Team) error
	FindByID(ctx context.Context, orgID, id string) (*Team, error)
	Update(ctx context.Context, t *Team) error
	Delete(ctx context.Context, orgID, id string) error
	// User-team membership
	AddMember(ctx context.Context, teamID, userID string) error
	RemoveMember(ctx context.Context, teamID, userID string) error
	ListMemberIDs(ctx context.Context, teamID string) ([]string, error)
	ListTeamIDsForUser(ctx context.Context, userID string) ([]string, error)
}

type UserRepository interface {
	Count(ctx context.Context) (int64, error)
	Create(ctx context.Context, u *User) error
	FindByID(ctx context.Context, id string) (*User, error)
	FindByIDInOrg(ctx context.Context, orgID, id string) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	ListAll(ctx context.Context, offset, limit int) ([]User, int64, error)
	ListByOrg(ctx context.Context, orgID string, offset, limit int) ([]User, int64, error)
	Update(ctx context.Context, u *User) error
}

// OIDCGroupMappingRepository manages group → team rules used during OIDC login.
type OIDCGroupMappingRepository interface {
	List(ctx context.Context, orgID string) ([]OIDCGroupMapping, error)
	FindByGroup(ctx context.Context, orgID, groupName string) ([]OIDCGroupMapping, error)
	Create(ctx context.Context, m *OIDCGroupMapping) error
	Delete(ctx context.Context, orgID, id string) error
}
