package prompts

import (
	authApplication "hyperstrate/server/internal/modules/auth/application"
	authHTTP "hyperstrate/server/internal/modules/auth/interfaces/http"
	"hyperstrate/server/internal/modules/prompts/application"
	"hyperstrate/server/internal/modules/prompts/infrastructure/persistence"
	httptransport "hyperstrate/server/internal/modules/prompts/interfaces/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

// Module wires the prompts module: HTTP handler + repo + service + event bus.
func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			persistence.NewPromptRepository,
			persistence.NewPromptVersionRepository,
			application.NewPromptEventBus,
			application.NewService,
			httptransport.NewHandler,
		),
		fx.Invoke(registerRoutes),
	)
}

func registerRoutes(
	r *gin.Engine,
	handler *httptransport.Handler,
	sessionValidator authApplication.SessionValidator,
) {
	group := r.Group("/prompts")
	group.Use(authHTTP.RequireAdmin(sessionValidator))
	handler.RegisterRoutes(group)
}
