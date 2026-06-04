package functions

import (
	"hyperstrate/server/internal/modules/auth/application"
	authHTTP "hyperstrate/server/internal/modules/auth/interfaces/http"
	functionsApplication "hyperstrate/server/internal/modules/functions/application"
	"hyperstrate/server/internal/modules/functions/infrastructure/persistence"
	httptransport "hyperstrate/server/internal/modules/functions/interfaces/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

// Module wires the full functions module: HTTP handler + repos + service.
func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			persistence.NewAppRepository,
			persistence.NewFunctionRepository,
			persistence.NewRevisionRepository,
			persistence.NewBuildRepository,
			persistence.NewInvocationRepository,
			persistence.NewLogRepository,
			persistence.NewRunnerPoolRepository,
			persistence.NewRunnerAgentRepository,
			functionsApplication.NewService,
			functionsApplication.NewBuildService,
			functionsApplication.NewRunnerService,
			httptransport.NewHandler,
		),
		fx.Invoke(registerRoutes),
	)
}

func registerRoutes(
	r *gin.Engine,
	handler *httptransport.Handler,
	keyValidator application.KeyValidator,
	sessionValidator application.SessionValidator,
) {
	adminGroup := r.Group("/functions")
	adminGroup.Use(authHTTP.RequireAdmin(sessionValidator))
	handler.RegisterAdminRoutes(adminGroup)

	inferGroup := r.Group("/functions")
	inferGroup.Use(authHTTP.InferAuth(keyValidator, sessionValidator))
	handler.RegisterInferRoutes(inferGroup)

	runnerGroup := r.Group("/functions")
	handler.RegisterRunnerRoutes(runnerGroup)
}
