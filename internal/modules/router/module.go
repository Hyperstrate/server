package router

import (
	"context"
	"fmt"
	"sync"
	"time"

	aiApplication "hyperstrate/server/internal/modules/ai/application"
	aiDomain "hyperstrate/server/internal/modules/ai/domain"
	authApplication "hyperstrate/server/internal/modules/auth/application"
	authDomain "hyperstrate/server/internal/modules/auth/domain"
	authHTTP "hyperstrate/server/internal/modules/auth/interfaces/http"
	obsApplication "hyperstrate/server/internal/modules/observability/application"
	promptsApplication "hyperstrate/server/internal/modules/prompts/application"
	"hyperstrate/server/internal/modules/router/application"
	routerDomain "hyperstrate/server/internal/modules/router/domain"
	"hyperstrate/server/internal/modules/router/infrastructure/persistence"
	httptransport "hyperstrate/server/internal/modules/router/interfaces/http"
	"hyperstrate/server/internal/shared/pagination"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

// Note on route protection:
//   - CRUD routes (/router, /router/:id, targets, features, interceptors, budget)
//     require a valid admin session token — managed via RequireAdmin middleware.
//   - Inference routes (/router/:id/infer, /router/:id/v1/chat/completions)
//     require a valid API key — managed via InferAuth middleware.

// Module wires the full router module: HTTP handler + repos + AI inferencer adapter.
func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			persistence.NewRouterRepository,
			persistence.NewRouterTargetRepository,
			persistence.NewRouterFeatureRepository,
			persistence.NewRouterInterceptorRepository,
			persistence.NewRouterTeamAccessRepository,
			persistence.NewEvaluationRepository,
			persistence.NewEvaluationCaseRepository,
			persistence.NewEvaluationRunRepository,
			newModelInferencer,
			newEmbeddingProvider,
			application.NewCacheStore,
			application.NewRouterInferenceEventBus,
			application.NewRouterTargetEventBus,
			newPromptLoaderAdapter,
			newHealthChecker,
			newMCPLoaderAdapter,
			newBudgetQuerierAdapter,
			newModelLookupAdapter,
			application.NewService,
			newModelResolverFunc,
			httptransport.NewHandler,
		),
		fx.Invoke(registerRoutes),
		fx.Invoke(registerModelListeners),
		fx.Invoke(registerTargetListeners),
		fx.Invoke(registerMCPServerListeners),
	)
}

func newModelResolverFunc(svc aiApplication.Service) httptransport.ModelResolverFunc {
	return func(ctx context.Context, modelID string) *httptransport.ModelRef {
		if modelID == "" {
			return nil
		}
		m, err := svc.GetModel(ctx, modelID)
		if err != nil || m == nil {
			return nil
		}
		def, _ := aiDomain.FindModelDefinition(m.ModelDefinitionKey)
		return &httptransport.ModelRef{
			ID:                     m.ID,
			Alias:                  m.Alias,
			DisplayName:            def.DisplayName,
			Provider:               string(def.Provider),
			ModelDefKey:            m.ModelDefinitionKey,
			InputPricePer1MTokens:  def.InputPricePer1MTokens,
			OutputPricePer1MTokens: def.OutputPricePer1MTokens,
		}
	}
}

func registerModelListeners(
	bus *aiApplication.ModelEventBus,
	targets routerDomain.RouterTargetRepository,
	interceptors routerDomain.RouterInterceptorRepository,
) {
	bus.OnDeleted(func(ctx context.Context, e aiApplication.ModelDeletedEvent) error {
		// Collect targets before deleting so we can clean up interceptor configs.
		modelTargets, err := targets.ListByModelID(ctx, e.OrgID, e.ModelID)
		if err != nil {
			return err
		}
		for _, t := range modelTargets {
			ics, err := interceptors.ListByRouterID(ctx, e.OrgID, t.RouterID)
			if err != nil {
				continue
			}
			for _, ic := range ics {
				if ic.Type != routerDomain.InterceptorSemanticClassifier {
					continue
				}
				cfgTargets, _ := ic.Config["targets"].(map[string]any)
				if _, exists := cfgTargets[t.ID]; !exists {
					continue
				}
				delete(cfgTargets, t.ID)
				ic.Config["targets"] = cfgTargets
				_ = interceptors.Update(ctx, &ic)
			}
		}
		return targets.DeleteByModelID(ctx, e.OrgID, e.ModelID)
	})
}

func registerMCPServerListeners(
	bus *aiApplication.MCPServerEventBus,
	features routerDomain.RouterFeatureRepository,
) {
	bus.OnDeleted(func(e aiApplication.MCPServerDeletedEvent) {
		_ = features.RemoveMCPServerID(context.Background(), e.OrgID, e.ServerID)
	})
}

func registerTargetListeners(
	bus *application.RouterTargetEventBus,
	interceptors routerDomain.RouterInterceptorRepository,
) {
	bus.OnDeleted(func(e application.TargetDeletedEvent) {
		ics, err := interceptors.ListByRouterID(context.Background(), e.OrgID, e.RouterID)
		if err != nil {
			return
		}
		for _, ic := range ics {
			ic := ic
			switch ic.Type {
			case routerDomain.InterceptorSemanticClassifier:
				cfgTargets, _ := ic.Config["targets"].(map[string]any)
				if _, exists := cfgTargets[e.TargetID]; !exists {
					continue
				}
				delete(cfgTargets, e.TargetID)
				ic.Config["targets"] = cfgTargets
				_ = interceptors.Update(context.Background(), &ic)

			case routerDomain.InterceptorABTest:
				rawVariants, _ := ic.Config["variants"].([]any)
				filtered := make([]any, 0, len(rawVariants))
				changed := false
				for _, rv := range rawVariants {
					m, _ := rv.(map[string]any)
					if modelID, _ := m["model_id"].(string); modelID == e.ModelID {
						changed = true
						continue
					}
					filtered = append(filtered, rv)
				}
				if changed {
					ic.Config["variants"] = filtered
					_ = interceptors.Update(context.Background(), &ic)
				}
			}
		}
	})
}

func registerRoutes(
	r *gin.Engine,
	handler *httptransport.Handler,
	keyValidator authApplication.KeyValidator,
	sessionValidator authApplication.SessionValidator,
) {
	// Admin-managed CRUD — session token with admin role required.
	crudGroup := r.Group("/router")
	crudGroup.Use(authHTTP.RequireAdmin(sessionValidator))
	handler.RegisterCRUDRoutes(crudGroup)

	// Inference — API key or session token accepted.
	inferGroup := r.Group("/router")
	inferGroup.Use(authHTTP.InferAuth(keyValidator, sessionValidator))
	handler.RegisterInferRoutes(inferGroup)

	// Provider-compatible proxy under a conflict-free prefix.
	// SDK baseURL: http://host/proxy/router/:id
	proxyGroup := r.Group("/proxy/router")
	proxyGroup.Use(authHTTP.InferAuth(keyValidator, sessionValidator))
	handler.RegisterProxyRoutes(proxyGroup)
}

// ── AI inferencer adapter ─────────────────────────────────────────────────────

type aiServiceAdapter struct {
	svc aiApplication.Service
}

func (a *aiServiceAdapter) InferModel(ctx context.Context, modelID string, fields map[string]string, options map[string]any) (*application.ModelInferResult, error) {
	// Skip AI-level event emission; the router service emits its own event.
	ctx = aiApplication.SkipInferenceLog(ctx)
	result, err := a.svc.Infer(ctx, aiApplication.InferRequest{
		ModelID: modelID,
		Fields:  fields,
		Options: options,
	})
	if err != nil {
		return nil, fmt.Errorf("router inference: %w", err)
	}
	return &application.ModelInferResult{
		Content:           result.Content,
		ModelDefKey:       result.ModelDefKey,
		Provider:          result.Provider,
		InputTokens:       result.InputTokens,
		OutputTokens:      result.OutputTokens,
		CachedInputTokens: result.CachedInputTokens,
		CostUSD:           result.CostUSD,
		ToolCalls:         result.ToolCalls,
	}, nil
}

func (a *aiServiceAdapter) InferModelStream(ctx context.Context, modelID string, fields map[string]string, options map[string]any) (<-chan application.StreamChunk, error) {
	// Skip AI-level event emission; the router service emits its own event.
	ctx = aiApplication.SkipInferenceLog(ctx)

	// Pre-fetch model info so the done chunk carries ModelDefKey/Provider/CostUSD for analytics.
	model, _ := a.svc.GetModel(ctx, modelID)

	upstream, err := a.svc.InferStream(ctx, aiApplication.InferRequest{
		ModelID: modelID,
		Fields:  fields,
		Options: options,
	})
	if err != nil {
		return nil, fmt.Errorf("router stream inference: %w", err)
	}
	out := make(chan application.StreamChunk, 16)
	go func() {
		defer close(out)
		for chunk := range upstream {
			toSend := application.StreamChunk{
				Delta:             chunk.Delta,
				Done:              chunk.Done,
				Err:               chunk.Err,
				InputTokens:       chunk.InputTokens,
				OutputTokens:      chunk.OutputTokens,
				CachedInputTokens: chunk.CachedInputTokens,
				ToolCalls:         chunk.ToolCalls,
			}
			if chunk.Done && model != nil {
				def, ok := aiDomain.FindModelDefinition(model.ModelDefinitionKey)
				if ok {
					toSend.ModelDefKey = model.ModelDefinitionKey
					toSend.Provider = string(def.Provider)
					toSend.CostUSD = def.ComputeCostUSD(chunk.InputTokens, chunk.OutputTokens)
				}
			}
			select {
			case out <- toSend:
			case <-ctx.Done():
				return
			}
		}
	}()
	// Note: SelectedModelID is set by the pipeline layer (runStream), not by
	// the AI adapter. The goroutine above forwards raw AI chunks; the pipeline
	// goroutine wraps them and injects SelectedModelID on the Done chunk.
	return out, nil
}

func newModelInferencer(svc aiApplication.Service) application.ModelInferencer {
	return &aiServiceAdapter{svc: svc}
}

// ── Prompt loader adapter ─────────────────────────────────────────────────────

type promptLoaderAdapter struct{ svc promptsApplication.Service }

func (a *promptLoaderAdapter) GetContent(ctx context.Context, id string) (string, error) {
	p, err := a.svc.GetPrompt(ctx, id)
	if err != nil {
		return "", err
	}
	return p.Content, nil
}

func newPromptLoaderAdapter(svc promptsApplication.Service) application.PromptLoader {
	return &promptLoaderAdapter{svc: svc}
}

// ── Health checker ────────────────────────────────────────────────────────────

// obsHealthAdapter wraps the observability service and caches the health map
// for 30 seconds so the pipeline does not hit the DB on every inference call.
type obsHealthAdapter struct {
	svc     obsApplication.Service
	mu      sync.RWMutex
	cache   map[string]bool
	expires time.Time
}

func newHealthChecker(svc obsApplication.Service) application.HealthChecker {
	return &obsHealthAdapter{svc: svc, cache: make(map[string]bool)}
}

func (a *obsHealthAdapter) IsModelHealthy(modelID string) bool {
	a.mu.RLock()
	if time.Now().Before(a.expires) {
		v, ok := a.cache[modelID]
		a.mu.RUnlock()
		if !ok {
			return true // no record → assume healthy
		}
		return v
	}
	a.mu.RUnlock()

	a.mu.Lock()
	defer a.mu.Unlock()
	if time.Now().Before(a.expires) {
		v, ok := a.cache[modelID]
		if !ok {
			return true
		}
		return v
	}
	if rows, err := a.svc.ListProviderHealth(""); err == nil {
		m := make(map[string]bool, len(rows))
		for _, h := range rows {
			m[h.ModelID] = h.IsHealthy
		}
		a.cache = m
	}
	a.expires = time.Now().Add(30 * time.Second)
	v, ok := a.cache[modelID]
	if !ok {
		return true
	}
	return v
}

// ── MCP server loader ─────────────────────────────────────────────────────────

type mcpLoaderAdapter struct{ repo aiDomain.MCPServerRepository }

func newMCPLoaderAdapter(repo aiDomain.MCPServerRepository) application.MCPServerLoader {
	return &mcpLoaderAdapter{repo: repo}
}

func (a *mcpLoaderAdapter) GetMCPServers(ctx context.Context, serverIDs []string) ([]*application.MCPServerLoadedConfig, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	servers, err := a.repo.FindByIDs(ctx, orgID, serverIDs)
	if err != nil {
		return nil, err
	}
	out := make([]*application.MCPServerLoadedConfig, 0, len(servers))
	for _, s := range servers {
		out = append(out, &application.MCPServerLoadedConfig{
			ID:          s.ID,
			Name:        s.Name,
			URL:         s.URL,
			Headers:     s.AuthHeaders(),
			TimeoutSecs: s.TimeoutSecs,
		})
	}
	return out, nil
}

// ── Cost recorder adapter ─────────────────────────────────────────────────────

type budgetQuerierAdapter struct{ svc obsApplication.Service }

func (a *budgetQuerierAdapter) SumCostByPeriod(orgID, routerID, virtualKeyID, teamID string, from time.Time) (int64, float64, error) {
	return a.svc.SumCostByPeriod(orgID, routerID, virtualKeyID, teamID, from)
}

func newBudgetQuerierAdapter(svc obsApplication.Service) application.BudgetQuerier {
	return &budgetQuerierAdapter{svc: svc}
}

// ── Model lookup adapter ──────────────────────────────────────────────────────

type modelLookupAdapter struct{ svc aiApplication.Service }

func (a *modelLookupAdapter) FindModelIDByDefKey(ctx context.Context, defKey string) string {
	page, err := a.svc.ListModels(ctx, pagination.Slice{Page: 1, PerPage: 500}, nil, "")
	if err != nil {
		return ""
	}
	for _, m := range page.Items {
		if m.ModelDefinitionKey == defKey {
			return m.ID
		}
	}
	return ""
}

func (a *modelLookupAdapter) FindDefKeyByModelID(ctx context.Context, modelID string) string {
	m, err := a.svc.GetModel(ctx, modelID)
	if err != nil || m == nil {
		return ""
	}
	return m.ModelDefinitionKey
}

func newModelLookupAdapter(svc aiApplication.Service) application.ModelLookup {
	return &modelLookupAdapter{svc: svc}
}

// ── Embedding provider ────────────────────────────────────────────────────────

// aiEmbeddingAdapter routes embedding calls through the AI service so that
// semantic features use the same registered model + credential system as
// chat inference. The modelID passed to Embed must be a registered model ID
// stored in the feature/interceptor config (model_id field).
type aiEmbeddingAdapter struct{ svc aiApplication.Service }

func (a *aiEmbeddingAdapter) Embed(ctx context.Context, modelID string, text string) ([]float32, error) {
	return a.svc.Embed(ctx, modelID, text)
}

func newEmbeddingProvider(svc aiApplication.Service) application.EmbeddingProvider {
	return &aiEmbeddingAdapter{svc: svc}
}
