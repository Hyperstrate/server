package auth

import (
	"context"
	"time"

	"hyperstrate/server/internal/config"
	"hyperstrate/server/internal/modules/auth/application"
	"hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/modules/auth/infrastructure/persistence"
	"hyperstrate/server/internal/modules/auth/infrastructure/vault"
	httptransport "hyperstrate/server/internal/modules/auth/interfaces/http"
	routerApplication "hyperstrate/server/internal/modules/router/application"
	obsApplication "hyperstrate/server/internal/modules/observability/application"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			persistence.NewOrganizationRepository,
			persistence.NewAPIKeyRepository,
			persistence.NewVirtualKeyRepository,
			persistence.NewTeamRepository,
			persistence.NewUserRepository,
			persistence.NewOIDCGroupMappingRepository,
			newVaultProvider,
			newAuthService,
			newHandler,
			fx.Annotate(
				func(svc application.Service) application.KeyValidator { return svc },
				fx.As(new(application.KeyValidator)),
			),
			fx.Annotate(
				func(svc application.Service) application.SessionValidator { return svc },
				fx.As(new(application.SessionValidator)),
			),
		),
		fx.Invoke(registerRoutes),
	)
}

func registerRoutes(r *gin.Engine, handler *httptransport.Handler, sessionValidator application.SessionValidator) {
	base := r.Group("/auth")

	// Public: no authentication required.
	handler.RegisterPublicRoutes(base)

	// Session: any valid session token (no org required — used for setup).
	session := base.Group("")
	session.Use(httptransport.RequireSession(sessionValidator))
	handler.RegisterSessionRoutes(session)

	// Admin: valid session + admin role.
	admin := base.Group("")
	admin.Use(httptransport.RequireAdmin(sessionValidator))
	handler.RegisterAdminRoutes(admin)
}

type usageQuerierAdapter struct{ svc obsApplication.Service }

func (a *usageQuerierAdapter) SumCostByPeriod(orgID, routerID, virtualKeyID, teamID string, from time.Time) (int64, float64, error) {
	return a.svc.SumCostByPeriod(orgID, routerID, virtualKeyID, teamID, from)
}

func newAuthService(
	orgRepo          domain.OrganizationRepository,
	apiKeyRepo       domain.APIKeyRepository,
	vkRepo           domain.VirtualKeyRepository,
	teamRepo         domain.TeamRepository,
	userRepo         domain.UserRepository,
	groupMappingRepo domain.OIDCGroupMappingRepository,
	vaultProvider    vault.Provider,
	cfg              config.Config,
	obsSvc           obsApplication.Service,
) application.Service {
	return application.NewService(
		orgRepo,
		apiKeyRepo,
		vkRepo,
		teamRepo,
		userRepo,
		groupMappingRepo,
		vaultProvider,
		application.ServiceConfig{
			JWTSecret:   []byte(cfg.JWTSecret),
			AdminEmail:  cfg.AdminEmail,
			OIDCJWKSURL: cfg.OIDCJWKSUrl,
		},
		&usageQuerierAdapter{svc: obsSvc},
	)
}

func newHandler(svc application.Service, cfg config.Config, routerSvc routerApplication.Service) *httptransport.Handler {
	routerName := func(ctx context.Context, id string) string {
		if id == "" {
			return ""
		}
		r, err := routerSvc.GetRouter(ctx, id)
		if err != nil || r == nil {
			return id
		}
		return r.Name
	}
	return httptransport.NewHandler(svc, cfg.FrontendURL, cfg.OIDCProviders, routerName)
}

func newVaultProvider() vault.Provider {
	return vault.NoopProvider{}
}
