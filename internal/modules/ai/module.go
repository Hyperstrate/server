package ai

import (
	"fmt"

	"hyperstrate/server/internal/config"
	"hyperstrate/server/internal/modules/ai/application"
	"hyperstrate/server/internal/modules/ai/infrastructure/dispatcher"
	"hyperstrate/server/internal/modules/ai/infrastructure/persistence"
	"hyperstrate/server/internal/modules/ai/infrastructure/proxy"
	httptransport "hyperstrate/server/internal/modules/ai/interfaces/http"
	authApplication "hyperstrate/server/internal/modules/auth/application"
	authHTTP "hyperstrate/server/internal/modules/auth/interfaces/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

// Module wires the full AI module: HTTP handler + repos + proxy + dispatcher.
func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			persistence.NewModelRepository,
			persistence.NewModelConfigurationRepository,
			persistence.NewModelKeyRotationRepository,
			persistence.NewJobRepository,
			persistence.NewConversationRepository,
			persistence.NewMCPServerRepository,
			proxy.NewHTTPProxy,
			application.NewJobProcessor,
			newJobDispatcher,
			application.NewModelEventBus,
			application.NewInferenceEventBus,
			application.NewMCPServerEventBus,
			application.NewService,
			httptransport.NewHandler,
		),
		fx.Invoke(registerRoutes),
	)
}

// WorkerModule is the minimal module for the SQS worker Lambda.
// It provides only the repos, proxy, and job processor — no HTTP server.
func WorkerModule() fx.Option {
	return fx.Options(
		fx.Provide(
			persistence.NewModelRepository,
			persistence.NewModelConfigurationRepository,
			persistence.NewModelKeyRotationRepository,
			persistence.NewJobRepository,
			proxy.NewHTTPProxy,
			application.NewJobProcessor,
		),
	)
}

// newJobDispatcher selects the right dispatcher from config:
//   - SQSQueueURL set → SQSDispatcher (Lambda production mode)
//   - otherwise       → GoroutineDispatcher (local dev mode)
func newJobDispatcher(cfg config.Config, processor application.JobProcessor) (application.JobDispatcher, error) {
	if cfg.SQSQueueURL != "" {
		d, err := dispatcher.NewSQSDispatcher(cfg.SQSQueueURL)
		if err != nil {
			return nil, fmt.Errorf("create SQS dispatcher: %w", err)
		}
		return d, nil
	}
	return dispatcher.NewGoroutineDispatcher(processor), nil
}

func registerRoutes(
	router *gin.Engine,
	handler *httptransport.Handler,
	keyValidator authApplication.KeyValidator,
	sessionValidator authApplication.SessionValidator,
) {
	// Admin-managed CRUD — session token with admin role required.
	adminGroup := router.Group("/ai")
	adminGroup.Use(authHTTP.RequireAdmin(sessionValidator))
	handler.RegisterAdminRoutes(adminGroup)

	// Inference and async job processing — API key or session token accepted.
	inferGroup := router.Group("/ai")
	inferGroup.Use(authHTTP.InferAuth(keyValidator, sessionValidator))
	handler.RegisterInferRoutes(inferGroup)

	// Provider-compatible proxy under a conflict-free prefix.
	// SDK baseURL: http://host/proxy/ai/:id
	proxyGroup := router.Group("/proxy/ai")
	proxyGroup.Use(authHTTP.InferAuth(keyValidator, sessionValidator))
	handler.RegisterProxyRoutes(proxyGroup)
}
