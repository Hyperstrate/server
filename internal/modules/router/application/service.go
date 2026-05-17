package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	authDomain "hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/modules/router/domain"
	"hyperstrate/server/internal/modules/router/infrastructure/mcp"
	"hyperstrate/server/internal/shared/agentsession"
	"hyperstrate/server/internal/shared/dbtype"
	"hyperstrate/server/internal/shared/pagination"
	tmpl "hyperstrate/server/internal/shared/template"
	"hyperstrate/server/internal/shared/webhook"

	"go.jetify.com/typeid/v2"
)

var fireWebhook = webhook.Fire

// PromptLoader retrieves system prompt content by ID. The router uses it to
// inject stored prompts before inference. Implementations live in the prompts
// module; the interface lives here to avoid a cross-module import cycle.
type PromptLoader interface {
	GetContent(ctx context.Context, promptID string) (string, error)
}

// MCPServerLoader resolves MCP server configs by ID for the pipeline. The
// pipeline calls this during inference to fetch auth headers without storing
// credentials in the feature config blob.
type MCPServerLoader interface {
	GetMCPServers(ctx context.Context, serverIDs []string) ([]*MCPServerLoadedConfig, error)
}

// MCPServerLoadedConfig is the fully-resolved server config the MCP client needs.
type MCPServerLoadedConfig struct {
	ID          string
	Name        string
	URL         string
	Headers     map[string]string
	TimeoutSecs int
}

// HealthChecker reports whether a registered model is currently considered
// healthy by the observability health monitor. It is called during target
// selection when the FeatureHealthCheck pipeline stage is enabled.
// A nil implementation (or a nil HealthChecker field) disables the check.
type HealthChecker interface {
	IsModelHealthy(modelID string) bool
}

// BudgetQuerier returns the actual spend within a time window from inference_logs.
// Used by the pipeline for log-based router budget enforcement.
type BudgetQuerier interface {
	SumCostByPeriod(orgID, routerID, virtualKeyID, teamID string, from time.Time) (requests int64, costUSD float64, err error)
}

// ModelLookup resolves model identifiers in both directions for export/import.
// Returns "" when no match is found. Implementations must be nil-safe at the
// call site — callers check for a nil ModelLookup before calling.
type ModelLookup interface {
	// FindModelIDByDefKey returns the registered model ID for a definition key.
	// Used during import to resolve exported target references.
	FindModelIDByDefKey(ctx context.Context, defKey string) string
	// FindDefKeyByModelID returns the model definition key for a registered ID.
	// Used during export to make target references portable.
	FindDefKeyByModelID(ctx context.Context, modelID string) string
}

// ModelInferResult is the outcome of a single model call made by the router.
type ModelInferResult struct {
	Content           string
	ModelDefKey       string
	Provider          string
	InputTokens       int64
	OutputTokens      int64
	CachedInputTokens int64
	CostUSD           float64
	ToolCalls         json.RawMessage
}

// ModelInferencer is the interface the router uses to call an underlying AI model.
type ModelInferencer interface {
	InferModel(ctx context.Context, modelID string, fields map[string]string, options map[string]any) (*ModelInferResult, error)
	InferModelStream(ctx context.Context, modelID string, fields map[string]string, options map[string]any) (<-chan StreamChunk, error)
}

// Service defines all router module use-cases.
type Service interface {
	// Router CRUD
	ListRouters(ctx context.Context, slice pagination.Slice, query string) (pagination.Paginated[RouterResponse], error)
	CreateRouter(ctx context.Context, input CreateRouterInput) (*RouterResponse, error)
	GetRouter(ctx context.Context, id string) (*RouterResponse, error)
	UpdateRouter(ctx context.Context, id string, input UpdateRouterInput) (*RouterResponse, error)
	DeleteRouter(ctx context.Context, id string) error

	// Targets
	ListTargets(ctx context.Context, routerID string) ([]RouterTargetResponse, error)
	AddTarget(ctx context.Context, routerID string, input AddTargetInput) (*RouterTargetResponse, error)
	UpdateTarget(ctx context.Context, routerID, targetID string, input UpdateTargetInput) (*RouterTargetResponse, error)
	RemoveTarget(ctx context.Context, routerID, targetID string) error

	// Features
	ListFeatures(ctx context.Context, routerID string) ([]RouterFeatureResponse, error)
	AddFeature(ctx context.Context, routerID string, input AddFeatureInput) (*RouterFeatureResponse, error)
	UpdateFeature(ctx context.Context, routerID, featureID string, input UpdateFeatureInput) (*RouterFeatureResponse, error)
	RemoveFeature(ctx context.Context, routerID, featureID string) error

	// Interceptors
	ListInterceptors(ctx context.Context, routerID string) ([]RouterInterceptorResponse, error)
	AddInterceptor(ctx context.Context, routerID string, input AddInterceptorInput) (*RouterInterceptorResponse, error)
	UpdateInterceptor(ctx context.Context, routerID, interceptorID string, input UpdateInterceptorInput) (*RouterInterceptorResponse, error)
	RemoveInterceptor(ctx context.Context, routerID, interceptorID string) error

	// Team access (RBAC)
	ListRouterTeamAccess(ctx context.Context, routerID string) ([]domain.RouterTeamAccess, error)
	GrantRouterTeamAccess(ctx context.Context, routerID, teamID string) error
	RevokeRouterTeamAccess(ctx context.Context, routerID, teamID string) error

	// Routing
	RouteInfer(ctx context.Context, routerID string, input RouteInferInput) (*RouteInferResult, error)
	RouteInferStream(ctx context.Context, routerID string, input RouteInferInput) (<-chan StreamChunk, error)
	RouteEmbed(ctx context.Context, routerID string, texts []string) ([][]float32, string, error)
	GetBudgetStatus(ctx context.Context, routerID string) (*BudgetStatus, error)
	MetricsSnapshot() []RouterMetricSnapshot
	LintRouter(ctx context.Context, routerID string) (*RouterLintResponse, error)

	ListMCPTools(ctx context.Context, routerID, featureID string) ([]MCPServerTools, error)

	// Export / Import
	ExportRouter(ctx context.Context, routerID string) (*RouterExport, error)
	ImportRouter(ctx context.Context, input RouterExport) (*RouterResponse, error)

	// Evaluations
	CreateEvaluation(ctx context.Context, input CreateEvaluationInput) (*EvaluationResponse, error)
	ListEvaluations(ctx context.Context, routerID string, slice pagination.Slice) (pagination.Paginated[EvaluationResponse], error)
	GetEvaluation(ctx context.Context, id string) (*EvaluationResponse, error)
	UpdateEvaluation(ctx context.Context, id string, input UpdateEvaluationInput) (*EvaluationResponse, error)
	DeleteEvaluation(ctx context.Context, id string) error
	ListEvaluationCases(ctx context.Context, evalID string) ([]EvaluationCaseResponse, error)
	AddEvaluationCase(ctx context.Context, evalID string, input EvaluationCaseInput) (*EvaluationCaseResponse, error)
	DeleteEvaluationCase(ctx context.Context, evalID, caseID string) error
	RunEvaluation(ctx context.Context, evalID, judgeModelID string) (*EvaluationRunResponse, error)
	ListEvaluationRuns(ctx context.Context, evalID string, slice pagination.Slice) (pagination.Paginated[EvaluationRunResponse], error)
}

type service struct {
	routerRepo      domain.RouterRepository
	targetRepo      domain.RouterTargetRepository
	featureRepo     domain.RouterFeatureRepository
	interceptorRepo domain.RouterInterceptorRepository
	accessRepo      domain.RouterTeamAccessRepository
	mcpLoader       MCPServerLoader
	inferencer      ModelInferencer
	pipeline        *featurePipeline
	inferBus        *RouterInferenceEventBus
	targetBus       *RouterTargetEventBus
	// promptLoader is optional; nil disables system-prompt injection.
	promptLoader  PromptLoader
	budgetQuerier BudgetQuerier
	// modelLookup is optional; nil disables model resolution during import.
	modelLookup  ModelLookup
	evalRepo     domain.EvaluationRepository
	evalCaseRepo domain.EvaluationCaseRepository
	evalRunRepo  domain.EvaluationRunRepository
	// rrCounters holds an atomic counter per router ID for round-robin selection.
	// Using in-process atomics avoids a read-modify-write race on the DB value.
	rrCounters sync.Map // routerID → *atomic.Int64

	budgetAlertMu   sync.Mutex
	budgetAlertSent map[string]struct{}
}

func NewService(
	routerRepo domain.RouterRepository,
	targetRepo domain.RouterTargetRepository,
	featureRepo domain.RouterFeatureRepository,
	interceptorRepo domain.RouterInterceptorRepository,
	accessRepo domain.RouterTeamAccessRepository,
	inferencer ModelInferencer,
	embedder EmbeddingProvider,
	cacheStore CacheStore,
	inferBus *RouterInferenceEventBus,
	targetBus *RouterTargetEventBus,
	promptLoader PromptLoader,
	healthChecker HealthChecker,
	mcpLoader MCPServerLoader,
	budgetQuerier BudgetQuerier,
	modelLookup ModelLookup,
	evalRepo domain.EvaluationRepository,
	evalCaseRepo domain.EvaluationCaseRepository,
	evalRunRepo domain.EvaluationRunRepository,
) Service {
	return &service{
		routerRepo:      routerRepo,
		targetRepo:      targetRepo,
		featureRepo:     featureRepo,
		interceptorRepo: interceptorRepo,
		accessRepo:      accessRepo,
		mcpLoader:       mcpLoader,
		inferencer:      inferencer,
		pipeline:        newFeaturePipeline(embedder, cacheStore, promptLoader, healthChecker, mcpLoader, budgetQuerier),
		budgetQuerier:   budgetQuerier,
		inferBus:        inferBus,
		targetBus:       targetBus,
		promptLoader:    promptLoader,
		modelLookup:     modelLookup,
		evalRepo:        evalRepo,
		evalCaseRepo:    evalCaseRepo,
		evalRunRepo:     evalRunRepo,
		budgetAlertSent: make(map[string]struct{}),
	}
}

// ── Router CRUD ───────────────────────────────────────────────────────────────

func (s *service) ListRouters(ctx context.Context, slice pagination.Slice, query string) (pagination.Paginated[RouterResponse], error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	routers, total, err := s.routerRepo.List(ctx, orgID, query, slice.Offset(), slice.PerPage)
	if err != nil {
		return pagination.Paginated[RouterResponse]{}, err
	}
	out := make([]RouterResponse, 0, len(routers))
	for _, r := range routers {
		out = append(out, toRouterResponse(&r))
	}
	return pagination.New(out, total, slice), nil
}

func (s *service) CreateRouter(ctx context.Context, input CreateRouterInput) (*RouterResponse, error) {
	r := &domain.Router{
		ID:          typeid.MustGenerate("rtr").String(),
		OrgID:       authDomain.OrgIDFromContext(ctx),
		Name:        input.Name,
		Description: input.Description,
		Status:      domain.RouterStatusDraft,
		Strategy:    input.Strategy,
		Configuration: domain.RouterConfiguration{
			WebhookURL:    input.WebhookURL,
			PromptID:      strPtr(input.PromptID),
			StorePayloads: input.StorePayloads,
		},
	}
	if err := s.routerRepo.Create(ctx, r); err != nil {
		return nil, err
	}
	resp := toRouterResponse(r)
	return &resp, nil
}

func (s *service) GetRouter(ctx context.Context, id string) (*RouterResponse, error) {
	r, err := s.routerRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), id)
	if err != nil {
		return nil, err
	}
	resp := toRouterResponse(r)
	return &resp, nil
}

func (s *service) UpdateRouter(ctx context.Context, id string, input UpdateRouterInput) (*RouterResponse, error) {
	r, err := s.routerRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), id)
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		r.Name = *input.Name
	}
	if input.Description != nil {
		r.Description = *input.Description
	}
	if input.Status != nil {
		r.Status = *input.Status
	}
	if input.Strategy != nil {
		r.Strategy = *input.Strategy
	}
	if input.WebhookURL != nil {
		r.Configuration.WebhookURL = *input.WebhookURL
	}
	if input.PromptID != nil {
		r.Configuration.PromptID = strPtr(*input.PromptID)
	}
	if input.StorePayloads != nil {
		r.Configuration.StorePayloads = *input.StorePayloads
	}
	if err := s.routerRepo.Update(ctx, r); err != nil {
		return nil, err
	}
	resp := toRouterResponse(r)
	return &resp, nil
}

func (s *service) DeleteRouter(ctx context.Context, id string) error {
	if err := s.routerRepo.Delete(ctx, authDomain.OrgIDFromContext(ctx), id); err != nil {
		return err
	}
	orgID := authDomain.OrgIDFromContext(ctx)
	if err := s.targetRepo.DeleteByRouterID(ctx, orgID, id); err != nil {
		slog.Error("DeleteRouter: failed to delete targets", "routerID", id, "err", err)
	}
	if err := s.featureRepo.DeleteByRouterID(ctx, orgID, id); err != nil {
		slog.Error("DeleteRouter: failed to delete features", "routerID", id, "err", err)
	}
	if err := s.interceptorRepo.DeleteByRouterID(ctx, orgID, id); err != nil {
		slog.Error("DeleteRouter: failed to delete interceptors", "routerID", id, "err", err)
	}
	return nil
}

// ── Targets ───────────────────────────────────────────────────────────────────

func (s *service) ListTargets(ctx context.Context, routerID string) ([]RouterTargetResponse, error) {
	if _, err := s.routerRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), routerID); err != nil {
		return nil, err
	}
	targets, err := s.targetRepo.ListByRouterID(ctx, authDomain.OrgIDFromContext(ctx), routerID)
	if err != nil {
		return nil, err
	}
	out := make([]RouterTargetResponse, 0, len(targets))
	for _, t := range targets {
		out = append(out, toTargetResponse(&t))
	}
	return out, nil
}

func (s *service) AddTarget(ctx context.Context, routerID string, input AddTargetInput) (*RouterTargetResponse, error) {
	if _, err := s.routerRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), routerID); err != nil {
		return nil, err
	}
	weight := input.Weight
	if weight <= 0 {
		weight = 1
	}
	t := &domain.RouterTarget{
		ID:         typeid.MustGenerate("rtgt").String(),
		RouterID:   routerID,
		ModelID:    input.ModelID,
		Weight:     weight,
		Percentage: input.Percentage,
		Priority:   input.Priority,
		IsEnabled:  true,
		PromptID:   strPtr(input.PromptID),
	}
	if err := s.targetRepo.Create(ctx, t); err != nil {
		return nil, err
	}
	resp := toTargetResponse(t)
	return &resp, nil
}

func (s *service) UpdateTarget(ctx context.Context, routerID, targetID string, input UpdateTargetInput) (*RouterTargetResponse, error) {
	t, err := s.targetRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), targetID)
	if err != nil {
		return nil, err
	}
	if t.RouterID != routerID {
		return nil, domain.ErrRouterTargetNotFound
	}
	if input.Weight != nil {
		t.Weight = *input.Weight
	}
	if input.Percentage != nil {
		t.Percentage = *input.Percentage
	}
	if input.Priority != nil {
		t.Priority = *input.Priority
	}
	if input.IsEnabled != nil {
		t.IsEnabled = *input.IsEnabled
	}
	if input.PromptID != nil {
		t.PromptID = strPtr(*input.PromptID)
	}
	if err := s.targetRepo.Update(ctx, t); err != nil {
		return nil, err
	}
	resp := toTargetResponse(t)
	return &resp, nil
}

func (s *service) RemoveTarget(ctx context.Context, routerID, targetID string) error {
	t, err := s.targetRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), targetID)
	if err != nil {
		return err
	}
	if t.RouterID != routerID {
		return domain.ErrRouterTargetNotFound
	}
	if err := s.targetRepo.Delete(ctx, authDomain.OrgIDFromContext(ctx), targetID); err != nil {
		return err
	}
	s.pipeline.embCache.Invalidate(targetID)
	// Remove the stale circuit-breaker entry so the pool doesn't grow unboundedly.
	remaining, _ := s.targetRepo.ListByRouterID(ctx, authDomain.OrgIDFromContext(ctx), routerID)
	activeIDs := make(map[string]struct{}, len(remaining))
	for _, t := range remaining {
		activeIDs[t.ID] = struct{}{}
	}
	s.pipeline.circuits.Cleanup(activeIDs)
	s.targetBus.EmitDeleted(TargetDeletedEvent{
		OrgID:    authDomain.OrgIDFromContext(ctx),
		RouterID: routerID,
		TargetID: targetID,
		ModelID:  t.ModelID,
	})
	return nil
}

// ── Features ──────────────────────────────────────────────────────────────────

func (s *service) ListFeatures(ctx context.Context, routerID string) ([]RouterFeatureResponse, error) {
	if _, err := s.routerRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), routerID); err != nil {
		return nil, err
	}
	features, err := s.featureRepo.ListByRouterID(ctx, authDomain.OrgIDFromContext(ctx), routerID)
	if err != nil {
		return nil, err
	}
	out := make([]RouterFeatureResponse, 0, len(features))
	for i := range features {
		resp := toFeatureResponse(&features[i])
		out = append(out, resp)
	}
	return out, nil
}

func (s *service) AddFeature(ctx context.Context, routerID string, input AddFeatureInput) (*RouterFeatureResponse, error) {
	if _, err := s.routerRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), routerID); err != nil {
		return nil, err
	}
	config, err := featureConfigRawToMap(input.FeatureType, input.Config)
	if err != nil {
		return nil, err
	}
	if input.FeatureType == domain.FeatureSemanticCache {
		if mid, _ := config["model_id"].(string); mid == "" {
			return nil, domain.ErrMissingEmbedModel
		}
	}
	order := 0
	if input.ExecutionOrder != 0 {
		order = input.ExecutionOrder
	} else {
		existing, err := s.featureRepo.ListByRouterID(ctx, authDomain.OrgIDFromContext(ctx), routerID)
		if err != nil {
			return nil, err
		}
		for _, ex := range existing {
			if ex.ExecutionOrder >= order {
				order = ex.ExecutionOrder + 1
			}
		}
	}
	f := &domain.RouterFeature{
		ID:             typeid.MustGenerate("rfeat").String(),
		RouterID:       routerID,
		FeatureType:    input.FeatureType,
		Config:         config,
		ExecutionOrder: order,
		IsEnabled:      true,
	}
	if err := s.featureRepo.Create(ctx, f); err != nil {
		return nil, err
	}
	resp := toFeatureResponse(f)
	return &resp, nil
}

func (s *service) UpdateFeature(ctx context.Context, routerID, featureID string, input UpdateFeatureInput) (*RouterFeatureResponse, error) {
	f, err := s.featureRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), featureID)
	if err != nil {
		return nil, err
	}
	if f.RouterID != routerID {
		return nil, domain.ErrRouterFeatureNotFound
	}
	if input.Config != nil {
		config, err := featureConfigRawToMap(f.FeatureType, *input.Config)
		if err != nil {
			return nil, err
		}
		f.Config = config
	}
	if input.ExecutionOrder != nil {
		f.ExecutionOrder = *input.ExecutionOrder
	}
	if input.IsEnabled != nil {
		f.IsEnabled = *input.IsEnabled
	}
	if err := s.featureRepo.Update(ctx, f); err != nil {
		return nil, err
	}
	resp := toFeatureResponse(f)
	return &resp, nil
}

func (s *service) RemoveFeature(ctx context.Context, routerID, featureID string) error {
	f, err := s.featureRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), featureID)
	if err != nil {
		return err
	}
	if f.RouterID != routerID {
		return domain.ErrRouterFeatureNotFound
	}
	return s.featureRepo.Delete(ctx, authDomain.OrgIDFromContext(ctx), featureID)
}

// ── Interceptors ──────────────────────────────────────────────────────────────

func (s *service) ListInterceptors(ctx context.Context, routerID string) ([]RouterInterceptorResponse, error) {
	if _, err := s.routerRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), routerID); err != nil {
		return nil, err
	}
	items, err := s.interceptorRepo.ListByRouterID(ctx, authDomain.OrgIDFromContext(ctx), routerID)
	if err != nil {
		return nil, err
	}
	out := make([]RouterInterceptorResponse, 0, len(items))
	for i := range items {
		resp := toInterceptorResponse(&items[i])
		out = append(out, resp)
	}
	return out, nil
}

func (s *service) AddInterceptor(ctx context.Context, routerID string, input AddInterceptorInput) (*RouterInterceptorResponse, error) {
	if _, err := s.routerRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), routerID); err != nil {
		return nil, err
	}
	if input.Type == domain.InterceptorSemanticClassifier {
		if mid, _ := input.Config["model_id"].(string); mid == "" {
			return nil, domain.ErrMissingEmbedModel
		}
	}
	if input.Type == domain.InterceptorContentFilter {
		patterns, _ := input.Config["blocked_patterns"].([]any)
		if len(patterns) == 0 {
			return nil, domain.ErrMissingBlockedPatterns
		}
		if len(patterns) > 100 {
			return nil, fmt.Errorf("blocked_patterns: maximum 100 patterns allowed")
		}
		for _, p := range patterns {
			pat, _ := p.(string)
			if len(pat) > 500 {
				return nil, fmt.Errorf("blocked_patterns: pattern exceeds maximum length of 500 characters")
			}
			if _, err := regexp.Compile(`(?i)` + pat); err != nil {
				return nil, fmt.Errorf("blocked_patterns: invalid regex %q: %w", pat, err)
			}
		}
	}

	order := 0
	if input.ExecutionOrder != nil {
		order = *input.ExecutionOrder
	} else {
		// Auto-assign: place after the last existing interceptor.
		existing, err := s.interceptorRepo.ListByRouterID(ctx, authDomain.OrgIDFromContext(ctx), routerID)
		if err != nil {
			return nil, err
		}
		for _, ex := range existing {
			if ex.ExecutionOrder >= order {
				order = ex.ExecutionOrder + 1
			}
		}
	}

	i := &domain.RouterInterceptor{
		ID:             typeid.MustGenerate("rint").String(),
		RouterID:       routerID,
		Type:           input.Type,
		Config:         input.Config,
		ExecutionOrder: order,
		IsEnabled:      true,
	}
	if err := s.interceptorRepo.Create(ctx, i); err != nil {
		return nil, err
	}
	resp := toInterceptorResponse(i)
	return &resp, nil
}

func (s *service) UpdateInterceptor(ctx context.Context, routerID, interceptorID string, input UpdateInterceptorInput) (*RouterInterceptorResponse, error) {
	i, err := s.interceptorRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), interceptorID)
	if err != nil {
		return nil, err
	}
	if i.RouterID != routerID {
		return nil, domain.ErrRouterInterceptorNotFound
	}
	if input.Config != nil {
		// Invalidate embedding cache for any targets whose utterances may have changed.
		if i.Type == domain.InterceptorSemanticClassifier {
			for id := range utteranceTargetIDs(i.Config) {
				s.pipeline.embCache.Invalidate(id)
			}
			for id := range utteranceTargetIDs(input.Config) {
				s.pipeline.embCache.Invalidate(id)
			}
		}
		i.Config = input.Config
	}
	if input.ExecutionOrder != nil {
		i.ExecutionOrder = *input.ExecutionOrder
	}
	if input.IsEnabled != nil {
		i.IsEnabled = *input.IsEnabled
	}
	if err := s.interceptorRepo.Update(ctx, i); err != nil {
		return nil, err
	}
	resp := toInterceptorResponse(i)
	return &resp, nil
}

// utteranceTargetIDs returns the set of target IDs present in the targets map
// of a semantic_classifier interceptor config.
func utteranceTargetIDs(cfg map[string]any) map[string]struct{} {
	targets, _ := cfg["targets"].(map[string]any)
	out := make(map[string]struct{}, len(targets))
	for id := range targets {
		out[id] = struct{}{}
	}
	return out
}

func (s *service) RemoveInterceptor(ctx context.Context, routerID, interceptorID string) error {
	i, err := s.interceptorRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), interceptorID)
	if err != nil {
		return err
	}
	if i.RouterID != routerID {
		return domain.ErrRouterInterceptorNotFound
	}
	return s.interceptorRepo.Delete(ctx, authDomain.OrgIDFromContext(ctx), interceptorID)
}

// ── Team access (RBAC) ────────────────────────────────────────────────────────

func (s *service) ListRouterTeamAccess(ctx context.Context, routerID string) ([]domain.RouterTeamAccess, error) {
	if _, err := s.routerRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), routerID); err != nil {
		return nil, err
	}
	return s.accessRepo.ListByRouterID(ctx, routerID)
}

func (s *service) GrantRouterTeamAccess(ctx context.Context, routerID, teamID string) error {
	router, err := s.routerRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), routerID)
	if err != nil {
		return err
	}
	return s.accessRepo.Grant(ctx, &domain.RouterTeamAccess{
		ID:       typeid.MustGenerate("racl").String(),
		RouterID: routerID,
		TeamID:   teamID,
		OrgID:    router.OrgID,
	})
}

func (s *service) RevokeRouterTeamAccess(ctx context.Context, routerID, teamID string) error {
	if _, err := s.routerRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), routerID); err != nil {
		return err
	}
	return s.accessRepo.Revoke(ctx, routerID, teamID)
}

// checkTeamAccess returns ErrTeamNotAllowed when the caller's team is not in the
// router's allow-list (an empty allow-list means open access).
func (s *service) checkTeamAccess(ctx context.Context, routerID string) error {
	if authDomain.CallerCanBypassTeamAccess(ctx) {
		return nil
	}
	teamIDs := authDomain.CallerTeamIDsFromContext(ctx)
	if len(teamIDs) == 0 {
		allowed, err := s.accessRepo.IsTeamAllowed(ctx, routerID, "")
		if err != nil {
			return err
		}
		if !allowed {
			return domain.ErrTeamNotAllowed
		}
		return nil
	}
	for _, teamID := range teamIDs {
		allowed, err := s.accessRepo.IsTeamAllowed(ctx, routerID, teamID)
		if err != nil {
			return err
		}
		if allowed {
			return nil
		}
	}
	return domain.ErrTeamNotAllowed
}

// ── Routing / Inference ───────────────────────────────────────────────────────

func (s *service) RouteInfer(ctx context.Context, routerID string, input RouteInferInput) (*RouteInferResult, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	vkID, teamID := authDomain.VirtualKeyIDFromContext(ctx)
	agentMeta := agentSessionMetaFromOptions(orgID, vkID, teamID, input.Options)
	emitErr := func(msg string) {
		s.inferBus.Emit(RouterInferenceLoggedEvent{
			OrgID:           orgID,
			RouterID:        routerID,
			VirtualKeyID:    vkID,
			TeamID:          teamID,
			UserID:          agentMeta.UserID,
			Status:          "error",
			ErrorMessage:    msg,
			AgentSessionID:  agentMeta.SessionID,
			Agent:           agentMeta.Agent,
			AgentRole:       agentMeta.Role,
			ParentSessionID: agentMeta.ParentSessionID,
			TurnIndex:       agentMeta.TurnIndex,
		})
	}

	router, err := s.routerRepo.FindByID(ctx, orgID, routerID)
	if err != nil {
		return nil, err
	}
	if router.Status != domain.RouterStatusActive {
		emitErr(domain.ErrRouterInactive.Error())
		return nil, domain.ErrRouterInactive
	}
	if err := s.checkTeamAccess(ctx, routerID); err != nil {
		emitErr(err.Error())
		return nil, err
	}

	targets, err := s.targetRepo.ListByRouterID(ctx, authDomain.OrgIDFromContext(ctx), routerID)
	if err != nil {
		return nil, err
	}

	features, err := s.featureRepo.ListByRouterID(ctx, authDomain.OrgIDFromContext(ctx), routerID)
	if err != nil {
		return nil, err
	}

	interceptors, err := s.interceptorRepo.ListByRouterID(ctx, authDomain.OrgIDFromContext(ctx), routerID)
	if err != nil {
		return nil, err
	}

	s.injectSystemPrompt(ctx, router, &input)

	start := time.Now()
	result, pipelineSteps, err := s.pipeline.run(ctx, router, targets, features, interceptors, input.Fields, input.Options, s.inferencer, input.BypassCache)
	latencyMs := time.Since(start).Milliseconds()

	if err != nil {
		s.inferBus.Emit(RouterInferenceLoggedEvent{
			OrgID:           orgID,
			RouterID:        routerID,
			VirtualKeyID:    vkID,
			TeamID:          teamID,
			UserID:          agentMeta.UserID,
			LatencyMs:       latencyMs,
			Status:          "error",
			ErrorMessage:    err.Error(),
			PipelineSteps:   pipelineSteps,
			AgentSessionID:  agentMeta.SessionID,
			Agent:           agentMeta.Agent,
			AgentRole:       agentMeta.Role,
			ParentSessionID: agentMeta.ParentSessionID,
			TurnIndex:       agentMeta.TurnIndex,
		})
		s.fireErrorWebhook(router, err)
		return nil, err
	}

	ev := RouterInferenceLoggedEvent{
		OrgID:             orgID,
		RouterID:          routerID,
		VirtualKeyID:      vkID,
		TeamID:            teamID,
		UserID:            agentMeta.UserID,
		ModelID:           result.SelectedModelID,
		SelectedTargetID:  result.SelectedTargetID,
		ModelDefKey:       result.ModelDefKey,
		Provider:          result.Provider,
		InputTokens:       result.InputTokens,
		OutputTokens:      result.OutputTokens,
		CachedInputTokens: result.CachedInputTokens,
		CostUSD:           result.CostUSD,
		LatencyMs:         latencyMs,
		Status:            "",
		ABVariant:         result.ABVariant,
		CacheHit:          result.CacheHit,
		CacheHitType:      result.CacheHitType,
		PipelineSteps:     pipelineSteps,
		AgentSessionID:    agentMeta.SessionID,
		Agent:             agentMeta.Agent,
		AgentRole:         agentMeta.Role,
		ParentSessionID:   agentMeta.ParentSessionID,
		TurnIndex:         agentMeta.TurnIndex,
		ToolCallCaptures:  result.ToolCallCaptures,
	}
	if router.Configuration.StorePayloads {
		ev.StorePayloads = true
		if b, err := json.Marshal(input.Fields); err == nil {
			ev.RequestFields = string(b)
		}
		ev.ResponseContent = result.Content
	}
	s.inferBus.Emit(ev)

	if router.Configuration.WebhookURL != "" {
		go s.checkBudgetAlertThreshold(context.WithoutCancel(ctx), orgID, router, features)
		s.fireLoopWebhook(router, pipelineSteps)
	}

	if router.Strategy == domain.RoutingStrategyRoundRobin {
		if enabled := enabledTargets(targets); len(enabled) > 0 {
			ctr, _ := s.rrCounters.LoadOrStore(router.ID, new(atomic.Int64))
			router.RoundRobinIndex = int(ctr.(*atomic.Int64).Add(1)) % len(enabled)
			_ = s.routerRepo.Update(ctx, router)
		}
	}

	return result, nil
}

func (s *service) fireLoopWebhook(router *domain.Router, steps []PipelineStep) {
	if router == nil || router.Configuration.WebhookURL == "" {
		return
	}
	toolErrors := 0
	for _, step := range steps {
		if step.Kind == "mcp_tool_call" && step.Outcome == "error" {
			toolErrors++
		}
		if step.Kind == "mcp_tools" && step.Outcome == "max_turns" {
			webhook.Fire(router.Configuration.WebhookURL, webhook.Event{
				Event:    webhook.EventLoopDetected,
				RouterID: router.ID,
				Detail:   "MCP tool loop reached max_turns",
			})
			return
		}
	}
	if toolErrors >= 3 {
		webhook.Fire(router.Configuration.WebhookURL, webhook.Event{
			Event:    webhook.EventLoopDetected,
			RouterID: router.ID,
			Detail:   fmt.Sprintf("%d MCP tool errors in one request", toolErrors),
		})
	}
}

type agentSessionMeta struct {
	SessionID       string
	Agent           string
	Role            string
	ParentSessionID string
	UserID          string
	TurnIndex       int
}

func agentSessionMetaFromOptions(orgID, virtualKeyID, teamID string, options map[string]any) agentSessionMeta {
	if options == nil {
		return agentSessionMeta{}
	}
	rawSessionID := firstStringOption(options, "agent_session_id", "session_id", "conversation_id")
	agent := firstStringOption(options, "agent")
	userID := firstStringOption(options, "agent_user_id", "user_id", "subject_user_id")
	actorID := userID
	if actorID == "" {
		actorID = virtualKeyID
	}
	if actorID == "" {
		actorID = teamID
	}
	// No explicit session ID: auto-group by actor within a 30-minute idle window.
	// This makes passive tracking work for agents that don't send X-Agent-Session-Id.
	if rawSessionID == "" && actorID != "" {
		rawSessionID = fmt.Sprintf("auto:%d", time.Now().UTC().Truncate(30*time.Minute).Unix())
	}
	parentSessionID := firstStringOption(options, "parent_session_id", "parent_agent_session_id")
	parentAgent := firstStringOption(options, "parent_agent")
	if parentAgent == "" {
		parentAgent = agent
	}
	parentActorID := firstStringOption(options, "parent_agent_user_id", "parent_user_id", "parent_subject_user_id")
	if parentActorID == "" {
		parentActorID = actorID
	}
	return agentSessionMeta{
		SessionID:       agentsession.CanonicalID(orgID, agent, actorID, rawSessionID),
		Agent:           agent,
		Role:            firstStringOption(options, "agent_role", "role"),
		ParentSessionID: agentsession.CanonicalID(orgID, parentAgent, parentActorID, parentSessionID),
		UserID:          userID,
		TurnIndex:       int(toFloat(options["turn_index"])),
	}
}

func firstStringOption(options map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, _ := options[key].(string); value != "" {
			return value
		}
	}
	return ""
}

// RouteInferStream performs inference through the router with streaming response.
// Returns a channel that yields StreamChunk values until Done is true or Err is set.
func (s *service) RouteInferStream(ctx context.Context, routerID string, input RouteInferInput) (<-chan StreamChunk, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	streamVKIDEarly, streamTeamIDEarly := authDomain.VirtualKeyIDFromContext(ctx)
	agentMeta := agentSessionMetaFromOptions(orgID, streamVKIDEarly, streamTeamIDEarly, input.Options)
	emitErr := func(msg string) {
		s.inferBus.Emit(RouterInferenceLoggedEvent{
			OrgID:           orgID,
			RouterID:        routerID,
			VirtualKeyID:    streamVKIDEarly,
			TeamID:          streamTeamIDEarly,
			UserID:          agentMeta.UserID,
			Status:          "error",
			ErrorMessage:    msg,
			AgentSessionID:  agentMeta.SessionID,
			Agent:           agentMeta.Agent,
			AgentRole:       agentMeta.Role,
			ParentSessionID: agentMeta.ParentSessionID,
			TurnIndex:       agentMeta.TurnIndex,
		})
	}

	router, err := s.routerRepo.FindByID(ctx, orgID, routerID)
	if err != nil {
		return nil, err
	}
	if router.Status != domain.RouterStatusActive {
		emitErr(domain.ErrRouterInactive.Error())
		return nil, domain.ErrRouterInactive
	}
	if err := s.checkTeamAccess(ctx, routerID); err != nil {
		emitErr(err.Error())
		return nil, err
	}

	targets, err := s.targetRepo.ListByRouterID(ctx, orgID, routerID)
	if err != nil {
		return nil, err
	}

	features, err := s.featureRepo.ListByRouterID(ctx, orgID, routerID)
	if err != nil {
		return nil, err
	}

	interceptors, err := s.interceptorRepo.ListByRouterID(ctx, orgID, routerID)
	if err != nil {
		return nil, err
	}

	// For streaming, we can't go through the regular pipeline because it expects
	// a full result. Instead, we select targets through routing logic and call the
	// streaming inferencer directly.
	s.injectSystemPrompt(ctx, router, &input)

	streamStart := time.Now()
	streamVKID, streamTeamID := streamVKIDEarly, streamTeamIDEarly

	upstream, streamSteps, err := s.pipeline.runStream(ctx, router, targets, features, interceptors, input.Fields, input.Options, s.inferencer, input.BypassCache)
	if err != nil {
		s.inferBus.Emit(RouterInferenceLoggedEvent{
			OrgID:           orgID,
			RouterID:        routerID,
			VirtualKeyID:    streamVKID,
			TeamID:          streamTeamID,
			UserID:          agentMeta.UserID,
			LatencyMs:       time.Since(streamStart).Milliseconds(),
			Status:          "error",
			ErrorMessage:    err.Error(),
			PipelineSteps:   *streamSteps,
			AgentSessionID:  agentMeta.SessionID,
			Agent:           agentMeta.Agent,
			AgentRole:       agentMeta.Role,
			ParentSessionID: agentMeta.ParentSessionID,
			TurnIndex:       agentMeta.TurnIndex,
		})
		s.fireErrorWebhook(router, err)
		return nil, err
	}

	if router.Strategy == domain.RoutingStrategyRoundRobin {
		if enabled := enabledTargets(targets); len(enabled) > 0 {
			ctr, _ := s.rrCounters.LoadOrStore(router.ID, new(atomic.Int64))
			router.RoundRobinIndex = int(ctr.(*atomic.Int64).Add(1)) % len(enabled)
			_ = s.routerRepo.Update(ctx, router)
		}
	}

	// Capture payload settings before goroutine so they're safe to read.
	storePayloads := router.Configuration.StorePayloads
	var requestFieldsJSON string
	if storePayloads {
		if b, err := json.Marshal(input.Fields); err == nil {
			requestFieldsJSON = string(b)
		}
	}

	// Wrap upstream to log once the stream is fully consumed.
	out := make(chan StreamChunk, 16)
	go func() {
		defer close(out)
		var selectedModelID, selectedTargetID, modelDefKey, provider, abVariant string
		var lastErr error
		var inputTokens, outputTokens, cachedInputTokens int64
		var costUSD float64
		var cacheHit bool
		var cacheHitType string
		var responseContent string
		var ttftMs int64
		firstChunk := true
	loop:
		for chunk := range upstream {
			if firstChunk && chunk.Delta != "" {
				ttftMs = time.Since(streamStart).Milliseconds()
				firstChunk = false
			}
			if chunk.SelectedModelID != "" {
				selectedModelID = chunk.SelectedModelID
			}
			if chunk.SelectedTargetID != "" {
				selectedTargetID = chunk.SelectedTargetID
			}
			if chunk.Done {
				inputTokens = chunk.InputTokens
				outputTokens = chunk.OutputTokens
				cachedInputTokens = chunk.CachedInputTokens
				modelDefKey = chunk.ModelDefKey
				provider = chunk.Provider
				costUSD = chunk.CostUSD
				if chunk.ABVariant != "" {
					abVariant = chunk.ABVariant
				}
				cacheHit = chunk.CacheHit
				cacheHitType = chunk.CacheHitType
			}
			if chunk.Err != nil {
				lastErr = chunk.Err
			}
			if storePayloads && chunk.Delta != "" {
				responseContent += chunk.Delta
			}
			select {
			case out <- chunk:
			case <-ctx.Done():
				lastErr = ctx.Err()
				break loop
			}
		}
		// Channel drained — pipeline goroutine has finished phases 7-9 and closed out.
		// Update the inference step with final token/cost detail (only known after stream ends).
		// Keep Outcome = "streaming" so the UI can distinguish streamed vs non-streamed steps.
		for i := range *streamSteps {
			if (*streamSteps)[i].Kind == "inference" && (*streamSteps)[i].Outcome == "streaming" {
				detail := modelDefKey
				if inputTokens > 0 || outputTokens > 0 {
					detail += fmt.Sprintf(" · %d↑ %d↓ tok", inputTokens, outputTokens)
				}
				if cachedInputTokens > 0 {
					detail += fmt.Sprintf(" · %d cached", cachedInputTokens)
				}
				if costUSD > 0 {
					detail += fmt.Sprintf(" · $%.5f", costUSD)
				}
				(*streamSteps)[i].Detail = detail
				break
			}
		}
		ev := RouterInferenceLoggedEvent{
			OrgID:             orgID,
			RouterID:          routerID,
			VirtualKeyID:      streamVKID,
			TeamID:            streamTeamID,
			UserID:            agentMeta.UserID,
			ModelID:           selectedModelID,
			SelectedTargetID:  selectedTargetID,
			ModelDefKey:       modelDefKey,
			Provider:          provider,
			InputTokens:       inputTokens,
			OutputTokens:      outputTokens,
			CachedInputTokens: cachedInputTokens,
			CostUSD:           costUSD,
			LatencyMs:         time.Since(streamStart).Milliseconds(),
			TTFTMs:            ttftMs,
			ABVariant:         abVariant,
			CacheHit:          cacheHit,
			CacheHitType:      cacheHitType,
			PipelineSteps:     *streamSteps,
			AgentSessionID:    agentMeta.SessionID,
			Agent:             agentMeta.Agent,
			AgentRole:         agentMeta.Role,
			ParentSessionID:   agentMeta.ParentSessionID,
			TurnIndex:         agentMeta.TurnIndex,
			StorePayloads:     storePayloads,
			RequestFields:     requestFieldsJSON,
			ResponseContent:   responseContent,
		}
		if lastErr != nil {
			ev.Status = "error"
			ev.ErrorMessage = lastErr.Error()
		} else {
			ev.Status = "success"
		}
		s.inferBus.Emit(ev)
	}()
	return out, nil
}

// RouteEmbed embeds texts using the first enabled target model on the router.
// Returns the vectors, the model def key used, and any error.
func (s *service) RouteEmbed(ctx context.Context, routerID string, texts []string) ([][]float32, string, error) {
	router, err := s.routerRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), routerID)
	if err != nil {
		return nil, "", err
	}
	if router.Status != domain.RouterStatusActive {
		return nil, "", domain.ErrRouterInactive
	}
	if err := s.checkTeamAccess(ctx, routerID); err != nil {
		return nil, "", err
	}
	if s.pipeline.embedder == nil {
		return nil, "", domain.ErrMissingEmbedModel
	}
	targets, err := s.targetRepo.ListByRouterID(ctx, authDomain.OrgIDFromContext(ctx), routerID)
	if err != nil {
		return nil, "", err
	}
	enabled := enabledTargets(targets)
	if len(enabled) == 0 {
		return nil, "", domain.ErrNoTargetsAvailable
	}
	modelID := enabled[0].ModelID
	vecs := make([][]float32, 0, len(texts))
	for _, t := range texts {
		v, err := s.pipeline.embedder.Embed(ctx, modelID, t)
		if err != nil {
			return nil, "", err
		}
		vecs = append(vecs, v)
	}
	return vecs, modelID, nil
}

func (s *service) GetBudgetStatus(ctx context.Context, routerID string) (*BudgetStatus, error) {
	router, err := s.routerRepo.FindByID(ctx, authDomain.OrgIDFromContext(ctx), routerID)
	if err != nil {
		return nil, err
	}
	features, err := s.featureRepo.ListByRouterID(ctx, authDomain.OrgIDFromContext(ctx), router.ID)
	if err != nil {
		return nil, err
	}
	for _, f := range features {
		if f.FeatureType == domain.FeatureBudget && f.IsEnabled {
			orgID := authDomain.OrgIDFromContext(ctx)
			return s.pipeline.budgetStatus(ctx, orgID, routerID, f.Config), nil
		}
	}
	return &BudgetStatus{PeriodKey: budgetPeriodStart("").Format("2006-01")}, nil
}

func (s *service) MetricsSnapshot() []RouterMetricSnapshot {
	return s.pipeline.metrics.Snapshot()
}

func (s *service) LintRouter(ctx context.Context, routerID string) (*RouterLintResponse, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	if _, err := s.routerRepo.FindByID(ctx, orgID, routerID); err != nil {
		return nil, err
	}
	targets, err := s.targetRepo.ListByRouterID(ctx, orgID, routerID)
	if err != nil {
		return nil, err
	}
	features, err := s.featureRepo.ListByRouterID(ctx, orgID, routerID)
	if err != nil {
		return nil, err
	}
	interceptors, err := s.interceptorRepo.ListByRouterID(ctx, orgID, routerID)
	if err != nil {
		return nil, err
	}
	access, err := s.accessRepo.ListByRouterID(ctx, routerID)
	if err != nil {
		return nil, err
	}

	return lintRouterConfig(routerID, targets, features, interceptors, access), nil
}

// ── System prompt injection ───────────────────────────────────────────────────

// injectSystemPrompt loads the router's stored system prompt (if any), interpolates
// {{variable}} placeholders from the request fields, and sets fields["systemPrompt"]
// unless the caller already provided one.
func (s *service) injectSystemPrompt(ctx context.Context, router *domain.Router, input *RouteInferInput) {
	if router.Configuration.PromptID == nil || *router.Configuration.PromptID == "" || s.promptLoader == nil {
		return
	}
	if input.Fields == nil {
		input.Fields = make(map[string]string)
	}
	if input.Fields["systemPrompt"] != "" {
		return // caller-provided value takes precedence
	}
	content, err := s.promptLoader.GetContent(ctx, *router.Configuration.PromptID)
	if err != nil || content == "" {
		return
	}
	input.Fields["systemPrompt"] = tmpl.Interpolate(content, input.Fields)
}

// ── Webhook helpers ───────────────────────────────────────────────────────────

// fireErrorWebhook fires the router's webhook URL for errors that are
// meaningful to an operator: budget exceeded, all targets failed, rate limit,
// quality gate failure, and request blocked by an interceptor.
// It is intentionally fire-and-forget — failures are logged, not surfaced.
func (s *service) fireErrorWebhook(router *domain.Router, err error) {
	if router == nil || router.Configuration.WebhookURL == "" {
		return
	}
	var eventName string
	switch {
	case errors.Is(err, domain.ErrBudgetExceeded):
		eventName = webhook.EventBudgetExceeded
	case errors.Is(err, domain.ErrAllTargetsFailed):
		eventName = webhook.EventAllTargetsFailed
	case errors.Is(err, domain.ErrRateLimitExceeded):
		eventName = webhook.EventRateLimitExceeded
	case errors.Is(err, domain.ErrLowQuality):
		eventName = webhook.EventQualityGateFailed
	case errors.Is(err, domain.ErrRequestBlocked):
		eventName = webhook.EventRequestBlocked
	default:
		return
	}
	fireWebhook(router.Configuration.WebhookURL, webhook.Event{
		Event:    eventName,
		RouterID: router.ID,
		Detail:   err.Error(),
	})
}

// checkBudgetAlertThreshold checks whether the router's budget usage has
// crossed the configured alert_percent threshold after a successful inference,
// and fires the webhook once per crossing. alert_percent defaults to 80.
func (s *service) checkBudgetAlertThreshold(ctx context.Context, orgID string, router *domain.Router, features []domain.RouterFeature) {
	for _, f := range features {
		if f.FeatureType != domain.FeatureBudget || !f.IsEnabled {
			continue
		}
		alertPct := 80.0
		if v, ok := f.Config["alert_percent"]; ok {
			if n := toFloat(v); n > 0 {
				alertPct = n
			}
		}
		maxCost := math.MaxFloat64
		if v, ok := f.Config["max_cost_usd"]; ok {
			if n := toFloat(v); n > 0 {
				maxCost = n
			}
		}
		if maxCost == math.MaxFloat64 {
			return // no cost limit configured
		}
		status := s.pipeline.budgetStatus(ctx, orgID, router.ID, f.Config)
		usedPct := status.EstimatedCostUSD / maxCost * 100
		key := budgetAlertKey(router.ID, f.ID, status.PeriodKey, alertPct)
		if s.markBudgetThresholdCrossed(key, usedPct >= alertPct) {
			fireWebhook(router.Configuration.WebhookURL, webhook.Event{
				Event:             webhook.EventBudgetThreshold,
				RouterID:          router.ID,
				BudgetUsedPercent: usedPct,
				Detail:            fmt.Sprintf("%.1f%% of budget used (alert at %.0f%%)", usedPct, alertPct),
			})
		}
		return
	}
}

func budgetAlertKey(routerID, featureID, periodKey string, alertPct float64) string {
	return fmt.Sprintf("%s:%s:%s:%g", routerID, featureID, periodKey, alertPct)
}

func (s *service) markBudgetThresholdCrossed(key string, crossed bool) bool {
	s.budgetAlertMu.Lock()
	defer s.budgetAlertMu.Unlock()

	if s.budgetAlertSent == nil {
		s.budgetAlertSent = make(map[string]struct{})
	}
	if !crossed {
		delete(s.budgetAlertSent, key)
		return false
	}
	if _, alreadySent := s.budgetAlertSent[key]; alreadySent {
		return false
	}
	s.budgetAlertSent[key] = struct{}{}
	return true
}

// ensure fmt is used
var _ = fmt.Sprintf

func (s *service) ListMCPTools(ctx context.Context, routerID, featureID string) ([]MCPServerTools, error) {
	orgID := authDomain.OrgIDFromContext(ctx)

	feature, err := s.featureRepo.FindByID(ctx, orgID, featureID)
	if err != nil {
		return nil, err
	}
	if feature.RouterID != routerID {
		return nil, domain.ErrRouterFeatureNotFound
	}
	if feature.FeatureType != domain.FeatureMCPTools {
		return nil, errors.New("feature is not an mcp_tools feature")
	}

	// Resolve server IDs → loaded configs.
	serverIDs := extractMCPServerIDs(feature.Config)
	var resolved []*MCPServerLoadedConfig
	if len(serverIDs) > 0 {
		resolved, err = s.mcpLoader.GetMCPServers(ctx, serverIDs)
		if err != nil {
			return nil, err
		}
	}

	// Fallback: inline URL-based servers (legacy config format).
	if len(resolved) == 0 {
		if rawServers, ok := feature.Config["servers"].([]any); ok {
			for _, rs := range rawServers {
				m, ok := rs.(map[string]any)
				if !ok {
					continue
				}
				u, _ := m["url"].(string)
				if u == "" {
					continue
				}
				resolved = append(resolved, &MCPServerLoadedConfig{URL: u, TimeoutSecs: 30})
			}
		}
	}

	if len(resolved) == 0 {
		return []MCPServerTools{}, nil
	}

	out := make([]MCPServerTools, 0, len(resolved))
	for _, cfg := range resolved {
		client := mcp.NewClientWithConfig(cfg.URL, cfg.Headers, cfg.TimeoutSecs)
		rawTools, err := client.ListTools(ctx)
		if err != nil {
			slog.Error("mcp list tools failed", "serverID", cfg.ID, "url", cfg.URL, "err", err)
			out = append(out, MCPServerTools{ServerID: cfg.ID, ServerName: cfg.Name, ServerURL: cfg.URL, Tools: []MCPToolDefinition{}})
			continue
		}
		defs := make([]MCPToolDefinition, 0, len(rawTools))
		for _, t := range rawTools {
			fn, _ := t["function"].(map[string]any)
			if fn == nil {
				continue
			}
			name, _ := fn["name"].(string)
			desc, _ := fn["description"].(string)
			params, _ := fn["parameters"].(map[string]any)
			defs = append(defs, MCPToolDefinition{Name: name, Description: desc, InputSchema: params})
		}
		out = append(out, MCPServerTools{ServerID: cfg.ID, ServerName: cfg.Name, ServerURL: cfg.URL, Tools: defs})
	}
	return out, nil
}

// ── Export / Import ───────────────────────────────────────────────────────────

func (s *service) ExportRouter(ctx context.Context, routerID string) (*RouterExport, error) {
	orgID := authDomain.OrgIDFromContext(ctx)

	router, err := s.routerRepo.FindByID(ctx, orgID, routerID)
	if err != nil {
		return nil, err
	}
	targets, err := s.targetRepo.ListByRouterID(ctx, orgID, routerID)
	if err != nil {
		return nil, err
	}
	features, err := s.featureRepo.ListByRouterID(ctx, orgID, routerID)
	if err != nil {
		return nil, err
	}
	interceptors, err := s.interceptorRepo.ListByRouterID(ctx, orgID, routerID)
	if err != nil {
		return nil, err
	}

	exp := &RouterExport{
		Version: "1",
		Router: ExportedRouter{
			Name:          router.Name,
			Strategy:      router.Strategy,
			Configuration: router.Configuration,
		},
	}

	for i, t := range targets {
		defKey := t.ModelID // fallback: use model ID as-is
		if s.modelLookup != nil {
			if k := s.modelLookup.FindDefKeyByModelID(ctx, t.ModelID); k != "" {
				defKey = k
			}
		}
		exp.Targets = append(exp.Targets, ExportedTarget{
			Key:                exportedTargetKey(i),
			ModelDefinitionKey: defKey,
			Weight:             t.Weight,
			Priority:           t.Priority,
			Percentage:         t.Percentage,
			IsEnabled:          t.IsEnabled,
			PromptID:           derefStr(t.PromptID),
		})
	}
	for _, f := range features {
		exp.Features = append(exp.Features, ExportedFeature{
			FeatureType:    f.FeatureType,
			Config:         s.exportConfigRefs(ctx, map[string]any(f.Config), targets),
			IsEnabled:      f.IsEnabled,
			ExecutionOrder: f.ExecutionOrder,
		})
	}
	for _, ic := range interceptors {
		exp.Interceptors = append(exp.Interceptors, ExportedInterceptor{
			Type:           ic.Type,
			Config:         s.exportConfigRefs(ctx, map[string]any(ic.Config), targets),
			IsEnabled:      ic.IsEnabled,
			ExecutionOrder: ic.ExecutionOrder,
		})
	}
	return exp, nil
}

func (s *service) ImportRouter(ctx context.Context, input RouterExport) (*RouterResponse, error) {
	name := input.Router.Name
	router := &domain.Router{
		ID:            typeid.MustGenerate("rtr").String(),
		OrgID:         authDomain.OrgIDFromContext(ctx),
		Name:          name,
		Strategy:      input.Router.Strategy,
		Status:        domain.RouterStatusActive,
		Configuration: input.Router.Configuration,
	}
	if err := s.routerRepo.Create(ctx, router); err != nil {
		return nil, err
	}

	importedTargetIDs := map[string]string{}
	for i, t := range input.Targets {
		modelID := t.ModelDefinitionKey // fallback: store def key as model ID
		if s.modelLookup != nil {
			if resolved := s.modelLookup.FindModelIDByDefKey(ctx, t.ModelDefinitionKey); resolved != "" {
				modelID = resolved
			}
		}
		target := &domain.RouterTarget{
			ID:         typeid.MustGenerate("rtgt").String(),
			RouterID:   router.ID,
			ModelID:    modelID,
			Weight:     t.Weight,
			Priority:   t.Priority,
			Percentage: t.Percentage,
			IsEnabled:  t.IsEnabled,
			PromptID:   strPtr(t.PromptID),
		}
		if err := s.targetRepo.Create(ctx, target); err != nil {
			slog.Warn("router import: skipping target", "def_key", t.ModelDefinitionKey, "err", err)
		} else {
			if t.Key != "" {
				importedTargetIDs[t.Key] = target.ID
			}
			importedTargetIDs[exportedTargetKey(i)] = target.ID
			importedTargetIDs[t.ModelDefinitionKey] = target.ID
		}
	}
	for _, f := range input.Features {
		feat := &domain.RouterFeature{
			ID:             typeid.MustGenerate("rfeat").String(),
			RouterID:       router.ID,
			FeatureType:    f.FeatureType,
			Config:         dbtype.JSONMap(s.importConfigRefs(ctx, f.Config, importedTargetIDs)),
			IsEnabled:      f.IsEnabled,
			ExecutionOrder: f.ExecutionOrder,
		}
		if err := s.featureRepo.Create(ctx, feat); err != nil {
			slog.Warn("router import: skipping feature", "type", f.FeatureType, "err", err)
		}
	}
	for _, ic := range input.Interceptors {
		interceptor := &domain.RouterInterceptor{
			ID:             typeid.MustGenerate("rint").String(),
			RouterID:       router.ID,
			Type:           ic.Type,
			Config:         dbtype.JSONMap(s.importConfigRefs(ctx, ic.Config, importedTargetIDs)),
			IsEnabled:      ic.IsEnabled,
			ExecutionOrder: ic.ExecutionOrder,
		}
		if err := s.interceptorRepo.Create(ctx, interceptor); err != nil {
			slog.Warn("router import: skipping interceptor", "type", ic.Type, "err", err)
		}
	}

	resp := toRouterResponse(router)
	return &resp, nil
}

func (s *service) exportConfigRefs(ctx context.Context, cfg map[string]any, targets []domain.RouterTarget) map[string]any {
	targetRefs := make(map[string]string, len(targets))
	for i, target := range targets {
		targetRefs[target.ID] = exportedTargetKey(i)
	}
	return s.rewriteConfigRefs(cfg, func(value string) string {
		return s.exportModelRef(ctx, value)
	}, func(value string) string {
		if ref := targetRefs[value]; ref != "" {
			return ref
		}
		return value
	})
}

func exportedTargetKey(index int) string {
	return fmt.Sprintf("target_%d", index+1)
}

func (s *service) importConfigRefs(ctx context.Context, cfg map[string]any, importedTargetIDs map[string]string) map[string]any {
	return s.rewriteConfigRefs(cfg, func(value string) string {
		return s.importModelRef(ctx, value)
	}, func(value string) string {
		if id := importedTargetIDs[value]; id != "" {
			return id
		}
		return value
	})
}

func (s *service) exportModelRef(ctx context.Context, modelID string) string {
	if modelID == "" || s.modelLookup == nil {
		return modelID
	}
	if defKey := s.modelLookup.FindDefKeyByModelID(ctx, modelID); defKey != "" {
		return defKey
	}
	return modelID
}

func (s *service) importModelRef(ctx context.Context, ref string) string {
	if ref == "" || s.modelLookup == nil {
		return ref
	}
	if modelID := s.modelLookup.FindModelIDByDefKey(ctx, ref); modelID != "" {
		return modelID
	}
	return ref
}

func (s *service) rewriteConfigRefs(cfg map[string]any, modelRef func(string) string, targetRef func(string) string) map[string]any {
	out := make(map[string]any, len(cfg))
	for key, value := range cfg {
		switch key {
		case "model_id", "judge_model_id", "shield_model_id", "overflow_target_id", "retry_target_id", "target_id", "default_target_id":
			if str, ok := value.(string); ok {
				out[key] = modelRef(str)
			} else {
				out[key] = value
			}
		case "targets":
			out[key] = rewriteTargetsConfig(value, modelRef, targetRef)
		case "variants":
			out[key] = rewriteVariantRefs(value, modelRef)
		case "budgets":
			out[key] = rewriteBudgetRefs(value, modelRef)
		case "thresholds":
			out[key] = rewriteThresholdRefs(value, modelRef)
		default:
			out[key] = value
		}
	}
	return out
}

func rewriteTargetsConfig(value any, modelRef func(string) string, targetRef func(string) string) any {
	switch typed := value.(type) {
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			if str, ok := item.(string); ok {
				out[i] = modelRef(str)
			} else {
				out[i] = item
			}
		}
		return out
	case []string:
		out := make([]string, len(typed))
		for i, item := range typed {
			out[i] = modelRef(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[targetRef(key)] = item
		}
		return out
	default:
		return value
	}
}

func rewriteVariantRefs(value any, modelRef func(string) string) any {
	items, ok := value.([]any)
	if !ok {
		return value
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		next := make(map[string]any, len(row))
		for key, value := range row {
			next[key] = value
		}
		if modelID, ok := next["model_id"].(string); ok {
			next["model_id"] = modelRef(modelID)
		}
		out = append(out, next)
	}
	return out
}

func rewriteBudgetRefs(value any, modelRef func(string) string) any {
	rows, ok := value.(map[string]any)
	if !ok {
		return value
	}
	out := make(map[string]any, len(rows))
	for teamID, item := range rows {
		row, ok := item.(map[string]any)
		if !ok {
			out[teamID] = item
			continue
		}
		next := make(map[string]any, len(row))
		for key, value := range row {
			next[key] = value
		}
		if modelID, ok := next["overflow_target_id"].(string); ok {
			next["overflow_target_id"] = modelRef(modelID)
		}
		out[teamID] = next
	}
	return out
}

func rewriteThresholdRefs(value any, modelRef func(string) string) any {
	items, ok := value.([]any)
	if !ok {
		return value
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		next := make(map[string]any, len(row))
		for key, value := range row {
			next[key] = value
		}
		if modelID, ok := next["target_id"].(string); ok {
			next["target_id"] = modelRef(modelID)
		}
		out = append(out, next)
	}
	return out
}

// extractMCPServerIDs reads server_ids from an mcp_tools feature config.
func extractMCPServerIDs(cfg map[string]any) []string {
	raw, ok := cfg["server_ids"].([]any)
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && s != "" {
			ids = append(ids, s)
		}
	}
	return ids
}
