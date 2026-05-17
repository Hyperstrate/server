package domain

import "context"

type RouterRepository interface {
	List(ctx context.Context, orgID, query string, offset, limit int) (items []Router, total int64, err error)
	Create(ctx context.Context, r *Router) error
	FindByID(ctx context.Context, orgID, id string) (*Router, error)
	Update(ctx context.Context, r *Router) error
	Delete(ctx context.Context, orgID, id string) error
}

type RouterTargetRepository interface {
	ListByRouterID(ctx context.Context, orgID, routerID string) ([]RouterTarget, error)
	Create(ctx context.Context, t *RouterTarget) error
	FindByID(ctx context.Context, orgID, id string) (*RouterTarget, error)
	Update(ctx context.Context, t *RouterTarget) error
	Delete(ctx context.Context, orgID, id string) error
	DeleteByRouterID(ctx context.Context, orgID, routerID string) error
	DeleteByModelID(ctx context.Context, orgID, modelID string) error
	ListByModelID(ctx context.Context, orgID, modelID string) ([]RouterTarget, error)
	NullifyPromptID(ctx context.Context, promptID string) error
}

type RouterFeatureRepository interface {
	ListByRouterID(ctx context.Context, orgID, routerID string) ([]RouterFeature, error)
	Create(ctx context.Context, f *RouterFeature) error
	FindByID(ctx context.Context, orgID, id string) (*RouterFeature, error)
	Update(ctx context.Context, f *RouterFeature) error
	Delete(ctx context.Context, orgID, id string) error
	DeleteByRouterID(ctx context.Context, orgID, routerID string) error
	RemoveMCPServerID(ctx context.Context, orgID, serverID string) error
}

// RouterInterceptorRepository manages the pre-routing hook records.
type RouterInterceptorRepository interface {
	ListByRouterID(ctx context.Context, orgID, routerID string) ([]RouterInterceptor, error)
	Create(ctx context.Context, i *RouterInterceptor) error
	FindByID(ctx context.Context, orgID, id string) (*RouterInterceptor, error)
	Update(ctx context.Context, i *RouterInterceptor) error
	Delete(ctx context.Context, orgID, id string) error
	DeleteByRouterID(ctx context.Context, orgID, routerID string) error
}

// RouterTeamAccessRepository manages team-level access grants on routers.
type RouterTeamAccessRepository interface {
	ListByRouterID(ctx context.Context, routerID string) ([]RouterTeamAccess, error)
	IsTeamAllowed(ctx context.Context, routerID, teamID string) (bool, error)
	Grant(ctx context.Context, a *RouterTeamAccess) error
	Revoke(ctx context.Context, routerID, teamID string) error
	DeleteByRouterID(ctx context.Context, routerID string) error
}
