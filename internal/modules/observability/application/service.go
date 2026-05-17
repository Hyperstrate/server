package application

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"hyperstrate/server/internal/modules/observability/domain"

	"go.jetify.com/typeid/v2"
)

// Service is the public API for the observability module.
type Service interface {
	// Inference logging
	LogInference(entry InferenceEntry)

	// Audit logging
	LogAudit(ctx context.Context, entry AuditEntry)

	// Raw log access
	ListInferenceLogs(filter domain.InferenceLogFilter, limit, offset int) ([]domain.InferenceLog, int64, error)

	// Analytics
	GetUsageOverTime(orgID string, from, to time.Time, gran domain.Granularity) ([]domain.AggregatedUsage, error)
	GetUsageByModel(orgID string, from, to *time.Time) ([]domain.ModelUsage, error)
	GetUsageByRouter(orgID string, from, to *time.Time) ([]domain.RouterUsage, error)
	GetABTestResults(orgID, routerID string, from, to *time.Time) ([]domain.ABVariantStats, error)
	GetUsageByVirtualKey(orgID string, from, to *time.Time) ([]domain.VirtualKeyUsage, error)
	GetCacheStats(orgID string, from, to *time.Time) (*domain.CacheStats, error)
	GetLatencyStats(orgID string, from, to *time.Time) ([]domain.LatencyStats, error)
	SubmitFeedback(orgID, logID string, feedback int) error
	GetRecentErrors(orgID string, limit int) ([]domain.InferenceLog, error)
	ListAuditLogs(orgID string, limit, offset int) ([]domain.AuditLog, int64, error)

	// Webhook delivery log
	RecordWebhookDelivery(d *domain.WebhookDelivery) error
	ListWebhookDeliveries(orgID, routerID string, limit, offset int) ([]domain.WebhookDelivery, int64, error)

	// Provider health (written by health monitor, read by the API)
	UpsertProviderHealth(h *domain.ProviderHealth) error
	ListProviderHealth(orgID string) ([]domain.ProviderHealth, error)
	DeleteProviderHealth(modelID string) error

	// Inference payloads
	SavePayload(p *domain.InferencePayload) error
	GetPayload(orgID, logID string) (*domain.InferencePayload, error)

	// Budget queries — used by router and auth modules for log-based enforcement
	SumCostByPeriod(orgID, routerID, virtualKeyID, teamID string, from time.Time) (requests int64, costUSD float64, err error)

	// Pipeline stats — per-router feature/interceptor outcome breakdown
	GetRouterPipelineStats(orgID, routerID string, from, to *time.Time) (*domain.RouterPipelineStats, error)

	// Retention cleanup
	PurgeOldLogs(retentionDays int) error

	// Agent session analytics
	ListAgentSessions(filter domain.InferenceLogFilter, limit, offset int) ([]domain.AgentSessionSummary, int64, error)
	GetAgentSessionInsights(filter domain.InferenceLogFilter, sessionID string) (*domain.AgentSessionInsights, error)
	ListAgentSessionEvents(filter domain.InferenceLogFilter, limit, offset int) ([]domain.AgentSessionEvent, int64, error)
	ListToolArchives(filter domain.InferenceLogFilter, limit, offset int) ([]domain.ToolCallArchive, int64, error)
	GetToolArchive(orgID, id string) (*domain.ToolCallArchive, error)
	ListCompressionEvents(filter domain.InferenceLogFilter, limit, offset int) ([]domain.CompressionEvent, int64, error)
	ListLoopDetections(filter domain.InferenceLogFilter, limit int) ([]domain.LoopDetection, error)
	ListCostlyPrompts(filter domain.InferenceLogFilter, limit int) ([]domain.CostlyPrompt, error)
	ListSubagentBreakdown(filter domain.InferenceLogFilter) ([]domain.SubagentBreakdown, error)
}

// InferenceEntry is the input for LogInference. All fields are optional except
// CreatedAt; callers should fill what they know.
// Source must be one of "direct", "router", "job".
type InferenceEntry struct {
	OrgID             string
	RouterID          string
	VirtualKeyID      string
	TeamID            string
	ModelID           string
	ModelDefKey       string
	Provider          string
	InputTokens       int64
	OutputTokens      int64
	CostUSD           float64
	LatencyMs         int64
	TTFTMs            int64  // time-to-first-token; 0 for sync calls
	Status            string // "success" | "error"
	ErrorMessage      string
	Source            string // "direct" | "router" | "job"
	SelectedTargetID  string
	ABVariant         string
	PipelineTrace     string // JSON-encoded []PipelineStep, set by router event listener
	CacheHit          bool
	CacheHitType      string // "exact" | "semantic" | ""
	StorePayloads     bool
	RequestFields     string // JSON-encoded fields map (only set when StorePayloads=true)
	ResponseContent   string // model response text (only set when StorePayloads=true)
	UserID            string
	CachedInputTokens int64
	AgentSessionID    string
	Agent             string
	AgentRole         string
	ParentSessionID   string
	TurnIndex         int
	ToolCallCount     int
	ToolResultChars   int
	QualityScore      int
	ContextFillPct    float64
	LoopDetected      bool
	LoopReason        string
	ToolArchives      []domain.ToolCallArchive
	CompressionEvents []domain.CompressionEvent
}

// AuditEntry is the input for LogAudit.
type AuditEntry struct {
	OrgID      string
	UserEmail  string
	Action     string
	Resource   string
	ResourceID string
	Details    string
	IPAddress  string
}

type service struct {
	logRepo      domain.InferenceLogRepository
	auditRepo    domain.AuditLogRepository
	healthRepo   domain.ProviderHealthRepository
	webhookRepo  domain.WebhookDeliveryRepository
	payloadRepo  domain.InferencePayloadRepository
	eventRepo    domain.AgentSessionEventRepository
	toolRepo     domain.ToolCallArchiveRepository
	compressRepo domain.CompressionEventRepository
}

func NewService(
	logRepo domain.InferenceLogRepository,
	auditRepo domain.AuditLogRepository,
	healthRepo domain.ProviderHealthRepository,
	webhookRepo domain.WebhookDeliveryRepository,
	payloadRepo domain.InferencePayloadRepository,
	eventRepo domain.AgentSessionEventRepository,
	toolRepo domain.ToolCallArchiveRepository,
	compressRepo domain.CompressionEventRepository,
) Service {
	return &service{
		logRepo:      logRepo,
		auditRepo:    auditRepo,
		healthRepo:   healthRepo,
		webhookRepo:  webhookRepo,
		payloadRepo:  payloadRepo,
		eventRepo:    eventRepo,
		toolRepo:     toolRepo,
		compressRepo: compressRepo,
	}
}

func (s *service) LogInference(e InferenceEntry) {
	status := e.Status
	if status == "" {
		status = "success"
	}
	src := domain.Source(e.Source)
	if src == "" {
		src = domain.SourceDirect
	}

	rec := &domain.InferenceLog{
		ID:                typeid.MustGenerate("ilog").String(),
		OrgID:             e.OrgID,
		RouterID:          e.RouterID,
		VirtualKeyID:      e.VirtualKeyID,
		TeamID:            e.TeamID,
		ModelID:           e.ModelID,
		ModelDefKey:       e.ModelDefKey,
		Provider:          e.Provider,
		InputTokens:       e.InputTokens,
		OutputTokens:      e.OutputTokens,
		TotalTokens:       e.InputTokens + e.OutputTokens,
		CostUSD:           e.CostUSD,
		LatencyMs:         e.LatencyMs,
		TTFTMs:            e.TTFTMs,
		Status:            status,
		ErrorMessage:      e.ErrorMessage,
		Source:            src,
		SelectedTargetID:  e.SelectedTargetID,
		ABVariant:         e.ABVariant,
		PipelineTrace:     e.PipelineTrace,
		CacheHit:          e.CacheHit,
		CacheHitType:      e.CacheHitType,
		UserID:            e.UserID,
		CachedInputTokens: e.CachedInputTokens,
		AgentSessionID:    e.AgentSessionID,
		Agent:             e.Agent,
		AgentRole:         e.AgentRole,
		ParentSessionID:   e.ParentSessionID,
		TurnIndex:         e.TurnIndex,
		ToolCallCount:     e.ToolCallCount,
		ToolResultChars:   e.ToolResultChars,
		QualityScore:      e.QualityScore,
		ContextFillPct:    e.ContextFillPct,
		LoopDetected:      e.LoopDetected,
		LoopReason:        e.LoopReason,
		CreatedAt:         time.Now(),
	}

	// Fire-and-forget; logging failure must never crash inference.
	if err := s.logRepo.Create(rec); err != nil {
		slog.Error("write inference log", "err", err)
		return
	}
	if e.StorePayloads {
		p := &domain.InferencePayload{
			LogID:           rec.ID,
			RouterID:        rec.RouterID,
			RequestFields:   e.RequestFields,
			ResponseContent: e.ResponseContent,
			CreatedAt:       rec.CreatedAt,
		}
		if err := s.payloadRepo.Create(p); err != nil {
			slog.Error("write inference payload", "err", err)
		}
	}
	for i := range e.ToolArchives {
		a := e.ToolArchives[i]
		a.ID = typeid.MustGenerate("tcar").String()
		a.OrgID = e.OrgID
		a.RouterID = e.RouterID
		a.LogID = rec.ID
		a.AgentSessionID = e.AgentSessionID
		a.CreatedAt = rec.CreatedAt
		if err := s.toolRepo.Create(&a); err != nil {
			slog.Error("write tool archive", "err", err)
		}
	}
	for i := range e.CompressionEvents {
		ce := e.CompressionEvents[i]
		ce.ID = typeid.MustGenerate("cevt").String()
		ce.OrgID = e.OrgID
		ce.LogID = rec.ID
		ce.AgentSessionID = e.AgentSessionID
		ce.CreatedAt = rec.CreatedAt
		if err := s.compressRepo.Create(&ce); err != nil {
			slog.Error("write compression event", "err", err)
		}
	}
}

func (s *service) LogAudit(_ context.Context, e AuditEntry) {
	rec := &domain.AuditLog{
		ID:         typeid.MustGenerate("audt").String(),
		OrgID:      e.OrgID,
		UserEmail:  e.UserEmail,
		Action:     e.Action,
		Resource:   e.Resource,
		ResourceID: e.ResourceID,
		Details:    e.Details,
		IPAddress:  e.IPAddress,
		CreatedAt:  time.Now(),
	}
	if err := s.auditRepo.Create(rec); err != nil {
		slog.Error("write audit log", "err", err)
	}
}

func (s *service) ListInferenceLogs(filter domain.InferenceLogFilter, limit, offset int) ([]domain.InferenceLog, int64, error) {
	return s.logRepo.List(filter, limit, offset)
}

func (s *service) GetUsageOverTime(orgID string, from, to time.Time, gran domain.Granularity) ([]domain.AggregatedUsage, error) {
	f := domain.InferenceLogFilter{OrgID: orgID, From: &from, To: &to}
	return s.logRepo.AggregateUsage(f, gran)
}

func (s *service) GetUsageByModel(orgID string, from, to *time.Time) ([]domain.ModelUsage, error) {
	return s.logRepo.AggregateByModel(domain.InferenceLogFilter{OrgID: orgID, From: from, To: to})
}

func (s *service) GetUsageByRouter(orgID string, from, to *time.Time) ([]domain.RouterUsage, error) {
	return s.logRepo.AggregateByRouter(domain.InferenceLogFilter{OrgID: orgID, From: from, To: to})
}

func (s *service) GetABTestResults(orgID, routerID string, from, to *time.Time) ([]domain.ABVariantStats, error) {
	return s.logRepo.AggregateByABVariant(orgID, routerID, from, to)
}

func (s *service) GetUsageByVirtualKey(orgID string, from, to *time.Time) ([]domain.VirtualKeyUsage, error) {
	return s.logRepo.AggregateByVirtualKey(orgID, from, to)
}

func (s *service) GetCacheStats(orgID string, from, to *time.Time) (*domain.CacheStats, error) {
	return s.logRepo.CacheStats(orgID, from, to)
}

func (s *service) GetLatencyStats(orgID string, from, to *time.Time) ([]domain.LatencyStats, error) {
	return s.logRepo.LatencyStatsByModel(domain.InferenceLogFilter{OrgID: orgID, From: from, To: to})
}

func (s *service) SubmitFeedback(orgID, logID string, feedback int) error {
	if feedback != 1 && feedback != -1 && feedback != 0 {
		return errors.New("feedback must be 1, -1, or 0")
	}
	return s.logRepo.UpdateFeedback(orgID, logID, feedback)
}

func (s *service) GetRecentErrors(orgID string, limit int) ([]domain.InferenceLog, error) {
	return s.logRepo.RecentErrors(orgID, limit)
}

func (s *service) ListAuditLogs(orgID string, limit, offset int) ([]domain.AuditLog, int64, error) {
	return s.auditRepo.List(orgID, limit, offset)
}

func (s *service) RecordWebhookDelivery(d *domain.WebhookDelivery) error {
	return s.webhookRepo.Create(d)
}

func (s *service) ListWebhookDeliveries(orgID, routerID string, limit, offset int) ([]domain.WebhookDelivery, int64, error) {
	return s.webhookRepo.ListByRouterID(orgID, routerID, limit, offset)
}

func (s *service) UpsertProviderHealth(h *domain.ProviderHealth) error {
	return s.healthRepo.Upsert(h)
}

func (s *service) ListProviderHealth(orgID string) ([]domain.ProviderHealth, error) {
	return s.healthRepo.ListAll(orgID)
}

func (s *service) DeleteProviderHealth(modelID string) error {
	return s.healthRepo.DeleteByModelID(modelID)
}

func (s *service) SavePayload(p *domain.InferencePayload) error {
	return s.payloadRepo.Create(p)
}

func (s *service) GetPayload(orgID, logID string) (*domain.InferencePayload, error) {
	return s.payloadRepo.FindByLogID(orgID, logID)
}

func (s *service) SumCostByPeriod(orgID, routerID, virtualKeyID, teamID string, from time.Time) (int64, float64, error) {
	return s.logRepo.SumByPeriod(orgID, routerID, virtualKeyID, teamID, from)
}

func (s *service) PurgeOldLogs(retentionDays int) error {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	return s.logRepo.DeleteOlderThan(cutoff)
}

// pipelineStep mirrors router/application.PipelineStep for JSON unmarshaling.
// Defined locally to avoid cross-module imports.
type pipelineStep struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Outcome  string `json:"outcome"`
	Attempts int    `json:"attempts"`
}

// featureKinds are pipeline step kinds shown in the Features section.
// cache is handled separately; target_select/budget_accounting/cache_store/fingerprint/prefetch are internal.
var featureKinds = map[string]bool{
	"rate_limit":        true,
	"budget":            true,
	"transform":         true,
	"health_check":      true,
	"hedging":           true,
	"mcp_tools":         true,
	"quality_gate":      true,
	"structured_output": true,
}

func (s *service) GetRouterPipelineStats(orgID, routerID string, from, to *time.Time) (*domain.RouterPipelineStats, error) {
	cache, err := s.logRepo.RouterCacheQuery(orgID, routerID, from, to)
	if err != nil {
		return nil, err
	}

	logs, err := s.logRepo.ListTracesForRouter(orgID, routerID, from, to, 500)
	if err != nil {
		return nil, err
	}

	type stepKey struct{ kind, name string }
	type stepAgg struct {
		outcomes      map[string]int64
		extraAttempts int64
	}
	features := map[stepKey]*stepAgg{}
	interceptors := map[stepKey]*stepAgg{}
	featureOrder := []stepKey{}
	interceptorOrder := []stepKey{}
	retryAttempts := int64(0)

	for _, log := range logs {
		if log.PipelineTrace == "" {
			continue
		}
		var steps []pipelineStep
		if err := json.Unmarshal([]byte(log.PipelineTrace), &steps); err != nil {
			continue
		}
		for _, step := range steps {
			switch {
			case step.Kind == "cache" || step.Kind == "cache_store" ||
				step.Kind == "budget_accounting" || step.Kind == "target_select" ||
				step.Kind == "fingerprint" || step.Kind == "prefetch":
				// skip — covered by DB query or internal implementation detail

			case step.Kind == "inference":
				// "retry" outcome steps each represent one extra API call
				if step.Outcome == "retry" {
					retryAttempts++
				}

			case step.Kind == "interceptor":
				k := stepKey{step.Kind, step.Name}
				if _, ok := interceptors[k]; !ok {
					interceptors[k] = &stepAgg{outcomes: map[string]int64{}}
					interceptorOrder = append(interceptorOrder, k)
				}
				interceptors[k].outcomes[step.Outcome]++

			case featureKinds[step.Kind]:
				k := stepKey{step.Kind, step.Name}
				if _, ok := features[k]; !ok {
					features[k] = &stepAgg{outcomes: map[string]int64{}}
					featureOrder = append(featureOrder, k)
				}
				features[k].outcomes[step.Outcome]++
			}
		}
	}

	// Build the cache breakdown using the DB-level query result.
	hits := cache.ExactHits + cache.SemanticHits
	misses := cache.TotalRequests - hits
	hitRate := 0.0
	if cache.TotalRequests > 0 {
		hitRate = float64(hits) / float64(cache.TotalRequests) * 100
	}
	cacheBreakdown := domain.RouterCacheBreakdown{
		TotalRequests: cache.TotalRequests,
		ExactHits:     cache.ExactHits,
		SemanticHits:  cache.SemanticHits,
		Misses:        misses,
		HitRatePct:    hitRate,
		EstSavedUSD:   float64(hits) * cache.AvgMissCostUSD,
	}

	// Convert feature map to ordered slice; append retry entry if any retries occurred.
	featureStats := make([]domain.PipelineStepStat, 0, len(featureOrder)+1)
	for _, k := range featureOrder {
		agg := features[k]
		featureStats = append(featureStats, domain.PipelineStepStat{
			Kind:     k.kind,
			Name:     k.name,
			Outcomes: agg.outcomes,
		})
	}
	if retryAttempts > 0 {
		featureStats = append(featureStats, domain.PipelineStepStat{
			Kind:          "retry",
			Name:          "Retry",
			Outcomes:      map[string]int64{},
			ExtraAttempts: retryAttempts,
		})
	}

	// Convert interceptor map to ordered slice.
	interceptorStats := make([]domain.PipelineStepStat, 0, len(interceptorOrder))
	for _, k := range interceptorOrder {
		agg := interceptors[k]
		interceptorStats = append(interceptorStats, domain.PipelineStepStat{
			Kind:     k.kind,
			Name:     k.name,
			Outcomes: agg.outcomes,
		})
	}

	return &domain.RouterPipelineStats{
		TotalRequests: cache.TotalRequests,
		AnalyzedLogs:  len(logs),
		Cache:         cacheBreakdown,
		Features:      featureStats,
		Interceptors:  interceptorStats,
	}, nil
}

func (s *service) ListAgentSessions(filter domain.InferenceLogFilter, limit, offset int) ([]domain.AgentSessionSummary, int64, error) {
	return s.logRepo.ListAgentSessions(filter, limit, offset)
}

func (s *service) ListAgentSessionEvents(filter domain.InferenceLogFilter, limit, offset int) ([]domain.AgentSessionEvent, int64, error) {
	return s.eventRepo.List(filter, limit, offset)
}

func (s *service) ListToolArchives(filter domain.InferenceLogFilter, limit, offset int) ([]domain.ToolCallArchive, int64, error) {
	return s.toolRepo.List(filter, limit, offset)
}

func (s *service) GetToolArchive(orgID, id string) (*domain.ToolCallArchive, error) {
	return s.toolRepo.FindByID(orgID, id)
}

func (s *service) ListCompressionEvents(filter domain.InferenceLogFilter, limit, offset int) ([]domain.CompressionEvent, int64, error) {
	return s.compressRepo.List(filter, limit, offset)
}

func (s *service) ListLoopDetections(filter domain.InferenceLogFilter, limit int) ([]domain.LoopDetection, error) {
	return s.logRepo.ListLoopDetections(filter, limit)
}

func (s *service) ListCostlyPrompts(filter domain.InferenceLogFilter, limit int) ([]domain.CostlyPrompt, error) {
	return s.logRepo.ListCostlyPrompts(filter, limit)
}

func (s *service) ListSubagentBreakdown(filter domain.InferenceLogFilter) ([]domain.SubagentBreakdown, error) {
	return s.logRepo.ListSubagentBreakdown(filter)
}

func (s *service) GetAgentSessionInsights(filter domain.InferenceLogFilter, sessionID string) (*domain.AgentSessionInsights, error) {
	filter.AgentSessionID = sessionID
	logs, _, err := s.logRepo.List(filter, 200, 0)
	if err != nil {
		return nil, err
	}
	loops, err := s.logRepo.ListLoopDetections(filter, 25)
	if err != nil {
		return nil, err
	}
	subagents, err := s.logRepo.ListSubagentBreakdown(filter)
	if err != nil {
		return nil, err
	}
	compressions, _, err := s.compressRepo.List(filter, 50, 0)
	if err != nil {
		return nil, err
	}
	events, _, err := s.eventRepo.List(filter, 50, 0)
	if err != nil {
		return nil, err
	}
	tools, _, err := s.toolRepo.List(filter, 50, 0)
	if err != nil {
		return nil, err
	}
	prompts, err := s.logRepo.ListCostlyPrompts(filter, 10)
	if err != nil {
		return nil, err
	}
	var quality, contextHealth float64
	var scored, contextCount int
	for _, l := range logs {
		if l.QualityScore > 0 {
			quality += float64(l.QualityScore)
			scored++
		}
		if l.ContextFillPct > 0 {
			contextHealth += l.ContextFillPct
			contextCount++
		}
	}
	if scored > 0 {
		quality /= float64(scored)
	}
	if contextCount > 0 {
		contextHealth /= float64(contextCount)
	}
	return &domain.AgentSessionInsights{
		SessionID:          sessionID,
		QualityScore:       quality,
		ContextHealthScore: contextHealth,
		LoopDetections:     loops,
		Subagents:          subagents,
		CompressionEvents:  compressions,
		Events:             events,
		ToolArchives:       tools,
		CostlyPrompts:      prompts,
	}, nil
}
