package domain

import (
	"errors"
	"time"
)

// Source identifies where an inference request originated.
type Source string

const (
	SourceDirect Source = "direct"
	SourceRouter Source = "router"
	SourceJob    Source = "job"
)

// InferenceLog captures every inference call for analytics, cost tracking, and debugging.
type InferenceLog struct {
	ID                string  `json:"id"              gorm:"primaryKey;size:50"`
	OrgID             string  `json:"orgId"           gorm:"size:50;not null;"`
	RouterID          string  `json:"routerId"        gorm:"size:50;not null;default:''"`
	VirtualKeyID      string  `json:"virtualKeyId"    gorm:"size:50;not null;default:''"`
	TeamID            string  `json:"teamId"          gorm:"size:50;not null;default:''"`
	UserID            string  `json:"userId"          gorm:"size:50;not null;default:'';index"`
	ModelID           string  `json:"modelId"         gorm:"size:50;not null;default:''"`
	ModelDefKey       string  `json:"modelDefKey"     gorm:"size:100;not null;default:''"`
	Provider          string  `json:"provider"        gorm:"size:50;not null;default:''"`
	InputTokens       int64   `json:"inputTokens"     gorm:"not null;default:0"`
	OutputTokens      int64   `json:"outputTokens"    gorm:"not null;default:0"`
	CachedInputTokens int64   `json:"cachedInputTokens" gorm:"not null;default:0"`
	TotalTokens       int64   `json:"totalTokens"     gorm:"not null;default:0"`
	CostUSD           float64 `json:"costUsd"         gorm:"not null;default:0"`
	LatencyMs         int64   `json:"latencyMs"       gorm:"not null;default:0"`
	// TTFTMs is the time-to-first-token in milliseconds for streaming calls; 0 for sync.
	TTFTMs           int64   `json:"ttftMs"          gorm:"not null;default:0"`
	Status           string  `json:"status"          gorm:"size:20;not null;default:'success'"`
	ErrorMessage     string  `json:"errorMessage,omitempty" gorm:"type:text"`
	Source           Source  `json:"source"          gorm:"size:20;not null;default:'direct'"`
	SelectedTargetID string  `json:"selectedTargetId,omitempty" gorm:"size:50;not null;default:''"`
	ABVariant        string  `json:"abVariant,omitempty"        gorm:"size:100;not null;default:''"`
	PipelineTrace    string  `json:"pipelineTrace,omitempty"    gorm:"type:text"`
	AgentSessionID   string  `json:"agentSessionId,omitempty"   gorm:"size:100;not null;default:'';index"`
	Agent            string  `json:"agent,omitempty"            gorm:"column:agent;size:50;not null;default:'';index"`
	AgentRole        string  `json:"agentRole,omitempty"        gorm:"size:50;not null;default:''"`
	ParentSessionID  string  `json:"parentSessionId,omitempty"  gorm:"size:100;not null;default:'';index"`
	TurnIndex        int     `json:"turnIndex,omitempty"        gorm:"not null;default:0"`
	ToolCallCount    int     `json:"toolCallCount,omitempty"    gorm:"not null;default:0"`
	ToolResultChars  int     `json:"toolResultChars,omitempty"  gorm:"not null;default:0"`
	QualityScore     int     `json:"qualityScore,omitempty"     gorm:"not null;default:0"`
	ContextFillPct   float64 `json:"contextFillPct,omitempty"   gorm:"not null;default:0"`
	LoopDetected     bool    `json:"loopDetected,omitempty"     gorm:"not null;default:false;index"`
	LoopReason       string  `json:"loopReason,omitempty"       gorm:"type:text"`
	// CacheHit is true when the response was served from cache (exact or semantic).
	CacheHit bool `json:"cacheHit"     gorm:"not null;default:false"`
	// CacheHitType is "exact", "semantic", or "" for a cache miss.
	CacheHitType string `json:"cacheHitType" gorm:"size:20;not null;default:''"`
	// Feedback is the user's quality signal: 1=positive, -1=negative, 0=none.
	Feedback  int       `json:"feedback"        gorm:"not null;default:0"`
	CreatedAt time.Time `json:"createdAt"       gorm:"not null;index"`
}

func (InferenceLog) TableName() string { return "inference_logs" }

// ── Audit log ─────────────────────────────────────────────────────────────────

// AuditLog records every admin-level action for security and compliance.
type AuditLog struct {
	ID         string    `json:"id"          gorm:"primaryKey;size:50"`
	OrgID      string    `json:"orgId"       gorm:"size:50;not null;"`
	UserEmail  string    `json:"userEmail"   gorm:"size:255;not null;default:''"`
	Action     string    `json:"action"      gorm:"size:50;not null"`
	Resource   string    `json:"resource"    gorm:"size:100;not null"`
	ResourceID string    `json:"resourceId"  gorm:"size:50;not null;default:''"`
	Details    string    `json:"details,omitempty" gorm:"type:text"`
	IPAddress  string    `json:"ipAddress,omitempty" gorm:"size:45;not null;default:''"`
	CreatedAt  time.Time `json:"createdAt"   gorm:"not null;index"`
}

func (AuditLog) TableName() string { return "audit_logs" }

// ── Provider health ───────────────────────────────────────────────────────────

// ProviderHealth stores the last health-check result for a registered model.
type ProviderHealth struct {
	ModelID      string    `json:"modelId"      gorm:"primaryKey;size:50"`
	ModelDefKey  string    `json:"modelDefKey"  gorm:"size:100;not null;default:''"`
	Provider     string    `json:"provider"     gorm:"size:50;not null;default:''"`
	IsHealthy    bool      `json:"isHealthy"    gorm:"not null;default:true"`
	LatencyMs    int64     `json:"latencyMs"    gorm:"not null;default:0"`
	ErrorMessage string    `json:"errorMessage,omitempty" gorm:"type:text"`
	CheckedAt    time.Time `json:"checkedAt"    gorm:"not null"`
}

func (ProviderHealth) TableName() string { return "provider_health" }

// ── Webhook delivery log ──────────────────────────────────────────────────────

// WebhookDelivery records one attempt to POST an event to a router's webhook URL.
type WebhookDelivery struct {
	ID         string    `json:"id"         gorm:"primaryKey;size:50"`
	RouterID   string    `json:"routerId"   gorm:"size:50;not null;index"`
	Event      string    `json:"event"      gorm:"size:50;not null"`
	URL        string    `json:"url"        gorm:"size:2000;not null"`
	StatusCode int       `json:"statusCode" gorm:"not null;default:0"`
	Success    bool      `json:"success"    gorm:"not null;default:false"`
	ErrorMsg   string    `json:"errorMsg,omitempty" gorm:"type:text"`
	CreatedAt  time.Time `json:"createdAt"  gorm:"not null;index"`
}

func (WebhookDelivery) TableName() string { return "webhook_deliveries" }

var ErrWebhookDeliveryNotFound = errors.New("webhook delivery not found")

type WebhookDeliveryRepository interface {
	Create(d *WebhookDelivery) error
	ListByRouterID(orgID, routerID string, limit, offset int) ([]WebhookDelivery, int64, error)
}

// ── Repository interfaces ─────────────────────────────────────────────────────

var ErrLogNotFound = errors.New("log not found")

type InferenceLogFilter struct {
	OrgID          string
	RouterID       string
	VirtualKeyID   string
	UserID         string
	AgentSessionID string
	Agent          string
	ModelID        string
	Source         Source
	Status         string
	From           *time.Time
	To             *time.Time
}

// AggregatedUsage is returned by analytics queries.
type AggregatedUsage struct {
	Bucket       string  `json:"bucket"` // time bucket (e.g. "2026-04-26T10:00")
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	CostUSD      float64 `json:"costUsd"`
	ErrorCount   int64   `json:"errorCount"`
	AvgLatencyMs float64 `json:"avgLatencyMs"`
}

type ModelUsage struct {
	ModelID     string  `json:"modelId"`
	ModelDefKey string  `json:"modelDefKey"`
	Provider    string  `json:"provider"`
	Requests    int64   `json:"requests"`
	TotalTokens int64   `json:"totalTokens"`
	CostUSD     float64 `json:"costUsd"`
	ErrorCount  int64   `json:"errorCount"`
}

type RouterUsage struct {
	RouterID string  `json:"routerId"`
	Requests int64   `json:"requests"`
	CostUSD  float64 `json:"costUsd"`
	Errors   int64   `json:"errors"`
}

// LatencyStats holds per-model latency percentiles derived from recent inference logs.
type LatencyStats struct {
	ModelDefKey string  `json:"modelDefKey"`
	Provider    string  `json:"provider"`
	Count       int64   `json:"count"`
	AvgMs       float64 `json:"avgMs"`
	P50Ms       int64   `json:"p50Ms"`
	P95Ms       int64   `json:"p95Ms"`
	P99Ms       int64   `json:"p99Ms"`
}

// VirtualKeyUsage holds usage aggregated per virtual key.
type VirtualKeyUsage struct {
	VirtualKeyID string  `json:"virtualKeyId"`
	Requests     int64   `json:"requests"`
	TotalTokens  int64   `json:"totalTokens"`
	CostUSD      float64 `json:"costUsd"`
	ErrorCount   int64   `json:"errorCount"`
}

// CacheStats holds cache hit/miss counts for a time period.
type CacheStats struct {
	TotalRequests int64   `json:"totalRequests"`
	CacheHits     int64   `json:"cacheHits"`
	ExactHits     int64   `json:"exactHits"`
	SemanticHits  int64   `json:"semanticHits"`
	HitRatePct    float64 `json:"hitRatePct"`
}

// ABVariantStats holds per-variant metrics for an A/B test result view.
type ABVariantStats struct {
	Variant      string  `json:"variant"`
	Requests     int64   `json:"requests"`
	ErrorCount   int64   `json:"errorCount"`
	AvgLatencyMs float64 `json:"avgLatencyMs"`
	TotalTokens  int64   `json:"totalTokens"`
	CostUSD      float64 `json:"costUsd"`
}

// Granularity controls the time bucketing for aggregate queries.
type Granularity string

const (
	GranularityHour  Granularity = "hour"
	GranularityDay   Granularity = "day"
	GranularityMonth Granularity = "month"
)

// RouterCacheResult is returned by RouterCacheQuery for pipeline stats aggregation.
type RouterCacheResult struct {
	TotalRequests  int64
	ExactHits      int64
	SemanticHits   int64
	AvgMissCostUSD float64 // average cost_usd of non-cached requests (for savings estimate)
}

// RouterCacheBreakdown is the cache section of RouterPipelineStats.
type RouterCacheBreakdown struct {
	TotalRequests int64   `json:"totalRequests"`
	ExactHits     int64   `json:"exactHits"`
	SemanticHits  int64   `json:"semanticHits"`
	Misses        int64   `json:"misses"`
	HitRatePct    float64 `json:"hitRatePct"`
	EstSavedUSD   float64 `json:"estSavedUsd"`
}

// PipelineStepStat aggregates outcomes for one pipeline feature or interceptor.
type PipelineStepStat struct {
	Kind          string           `json:"kind"`
	Name          string           `json:"name"`
	Outcomes      map[string]int64 `json:"outcomes"`
	ExtraAttempts int64            `json:"extraAttempts,omitempty"` // retry/hedging extra API calls
}

// RouterPipelineStats holds pipeline-level performance and cost breakdown for a specific router.
type RouterPipelineStats struct {
	TotalRequests int64                `json:"totalRequests"`
	AnalyzedLogs  int                  `json:"analyzedLogs"` // number of trace-parsed logs
	Cache         RouterCacheBreakdown `json:"cache"`
	Features      []PipelineStepStat   `json:"features"`
	Interceptors  []PipelineStepStat   `json:"interceptors"`
}

type AgentSessionSummary struct {
	SessionID             string    `json:"sessionId"`
	Agent                 string    `json:"agent"`
	RouterID              string    `json:"routerId"`
	VirtualKeyID          string    `json:"virtualKeyId"`
	TeamID                string    `json:"teamId"`
	UserID                string    `json:"userId"`
	StartedAt             time.Time `json:"startedAt"`
	LastSeenAt            time.Time `json:"lastSeenAt"`
	Turns                 int64     `json:"turns"`
	InputTokens           int64     `json:"inputTokens"`
	OutputTokens          int64     `json:"outputTokens"`
	CachedInputTokens     int64     `json:"cachedInputTokens"`
	TotalTokens           int64     `json:"totalTokens"`
	CostUSD               float64   `json:"costUsd"`
	CacheHits             int64     `json:"cacheHits"`
	ErrorCount            int64     `json:"errorCount"`
	ToolCallCount         int64     `json:"toolCallCount"`
	ToolResultChars       int64     `json:"toolResultChars"`
	AvgQualityScore       float64   `json:"avgQualityScore"`
	AvgContextFillPct     float64   `json:"avgContextFillPct"`
	LoopCount             int64     `json:"loopCount"`
	CompressionEvents     int64     `json:"compressionEvents"`
	CompressionSavedChars int64     `json:"compressionSavedChars"`
	Checkpoints           int64     `json:"checkpoints"`
	SubagentCostUSD       float64   `json:"subagentCostUsd"`
}

type AgentSessionEvent struct {
	ID             string    `json:"id"             gorm:"primaryKey;size:50"`
	OrgID          string    `json:"orgId"          gorm:"size:50;not null;index"`
	RouterID       string    `json:"routerId"       gorm:"size:50;not null;default:'';index"`
	VirtualKeyID   string    `json:"virtualKeyId"   gorm:"size:50;not null;default:'';index"`
	TeamID         string    `json:"teamId"         gorm:"size:50;not null;default:'';index"`
	UserID         string    `json:"userId"         gorm:"size:50;not null;default:'';index"`
	AgentSessionID string    `json:"agentSessionId" gorm:"size:100;not null;index"`
	Agent          string    `json:"agent"          gorm:"column:agent;size:50;not null;default:'';index"`
	EventType      string    `json:"eventType"      gorm:"size:50;not null;index"`
	Detail         string    `json:"detail,omitempty" gorm:"type:text"`
	CreatedAt      time.Time `json:"createdAt"      gorm:"not null;index"`
}

func (AgentSessionEvent) TableName() string { return "agent_session_events" }

type ToolCallArchive struct {
	ID              string    `json:"id"             gorm:"primaryKey;size:50"`
	OrgID           string    `json:"orgId"          gorm:"size:50;not null;index"`
	RouterID        string    `json:"routerId"       gorm:"size:50;not null;default:'';index"`
	LogID           string    `json:"logId"          gorm:"size:50;not null;index"`
	AgentSessionID  string    `json:"agentSessionId" gorm:"size:100;not null;default:'';index"`
	ToolName        string    `json:"toolName"       gorm:"size:200;not null;default:'';index"`
	ToolCallID      string    `json:"toolCallId"     gorm:"size:200;not null;default:''"`
	RequestPreview  string    `json:"requestPreview" gorm:"type:text"`
	RequestPayload  string    `json:"requestPayload,omitempty" gorm:"type:text"`
	ResponsePreview string    `json:"responsePreview" gorm:"type:text"`
	ResponsePayload string    `json:"responsePayload,omitempty" gorm:"type:text"`
	ResponseChars   int       `json:"responseChars"  gorm:"not null;default:0"`
	ErrorMessage    string    `json:"errorMessage,omitempty" gorm:"type:text"`
	Archived        bool      `json:"archived"       gorm:"not null;default:false;index"`
	CreatedAt       time.Time `json:"createdAt"      gorm:"not null;index"`
}

func (ToolCallArchive) TableName() string { return "tool_call_archives" }

type CompressionEvent struct {
	ID                   string    `json:"id"             gorm:"primaryKey;size:50"`
	OrgID                string    `json:"orgId"          gorm:"size:50;not null;index"`
	RouterID             string    `json:"routerId"       gorm:"size:50;not null;default:'';index"`
	LogID                string    `json:"logId"          gorm:"size:50;not null;index"`
	AgentSessionID       string    `json:"agentSessionId" gorm:"size:100;not null;default:'';index"`
	FeatureName          string    `json:"featureName"    gorm:"size:100;not null;index"`
	BeforeChars          int       `json:"beforeChars"    gorm:"not null;default:0"`
	AfterChars           int       `json:"afterChars"     gorm:"not null;default:0"`
	SavedChars           int       `json:"savedChars"     gorm:"not null;default:0"`
	EstimatedTokensSaved int       `json:"estimatedTokensSaved" gorm:"not null;default:0"`
	Exact                bool      `json:"exact"          gorm:"not null;default:false"`
	QualityScore         int       `json:"qualityScore"   gorm:"not null;default:0"`
	Detail               string    `json:"detail,omitempty" gorm:"type:text"`
	CreatedAt            time.Time `json:"createdAt"      gorm:"not null;index"`
}

func (CompressionEvent) TableName() string { return "compression_events" }

type CostlyPrompt struct {
	LogID          string    `json:"logId"`
	AgentSessionID string    `json:"agentSessionId"`
	Agent          string    `json:"agent"`
	RouterID       string    `json:"routerId"`
	ModelDefKey    string    `json:"modelDefKey"`
	CostUSD        float64   `json:"costUsd"`
	TotalTokens    int64     `json:"totalTokens"`
	PromptPreview  string    `json:"promptPreview"`
	CreatedAt      time.Time `json:"createdAt"`
}

type SubagentBreakdown struct {
	AgentSessionID  string  `json:"agentSessionId"`
	ParentSessionID string  `json:"parentSessionId"`
	AgentRole       string  `json:"agentRole"`
	Agent           string  `json:"agent"`
	Turns           int64   `json:"turns"`
	TotalTokens     int64   `json:"totalTokens"`
	CostUSD         float64 `json:"costUsd"`
	ToolCallCount   int64   `json:"toolCallCount"`
	AvgQualityScore float64 `json:"avgQualityScore"`
}

type LoopDetection struct {
	LogID          string    `json:"logId"`
	AgentSessionID string    `json:"agentSessionId"`
	TurnIndex      int       `json:"turnIndex"`
	Reason         string    `json:"reason"`
	CostUSD        float64   `json:"costUsd"`
	CreatedAt      time.Time `json:"createdAt"`
}

type AgentSessionInsights struct {
	SessionID          string              `json:"sessionId"`
	QualityScore       float64             `json:"qualityScore"`
	ContextHealthScore float64             `json:"contextHealthScore"`
	LoopDetections     []LoopDetection     `json:"loopDetections"`
	Subagents          []SubagentBreakdown `json:"subagents"`
	CompressionEvents  []CompressionEvent  `json:"compressionEvents"`
	Events             []AgentSessionEvent `json:"events"`
	ToolArchives       []ToolCallArchive   `json:"toolArchives"`
	CostlyPrompts      []CostlyPrompt      `json:"costlyPrompts"`
}

type InferenceLogRepository interface {
	Create(log *InferenceLog) error
	DeleteOlderThan(cutoff time.Time) error
	UpdateFeedback(orgID, id string, feedback int) error

	// Paginated raw log access
	List(filter InferenceLogFilter, limit, offset int) ([]InferenceLog, int64, error)
	ListAgentSessions(filter InferenceLogFilter, limit, offset int) ([]AgentSessionSummary, int64, error)
	ListCostlyPrompts(filter InferenceLogFilter, limit int) ([]CostlyPrompt, error)
	ListSubagentBreakdown(filter InferenceLogFilter) ([]SubagentBreakdown, error)
	ListLoopDetections(filter InferenceLogFilter, limit int) ([]LoopDetection, error)

	// Analytics aggregates
	AggregateUsage(filter InferenceLogFilter, granularity Granularity) ([]AggregatedUsage, error)
	AggregateByModel(filter InferenceLogFilter) ([]ModelUsage, error)
	AggregateByRouter(filter InferenceLogFilter) ([]RouterUsage, error)
	AggregateByABVariant(orgID, routerID string, from, to *time.Time) ([]ABVariantStats, error)
	AggregateByVirtualKey(orgID string, from, to *time.Time) ([]VirtualKeyUsage, error)
	CacheStats(orgID string, from, to *time.Time) (*CacheStats, error)
	LatencyStatsByModel(filter InferenceLogFilter) ([]LatencyStats, error)
	RecentErrors(orgID string, limit int) ([]InferenceLog, error)
	// SumByPeriod returns total requests and cost_usd from inference_logs since
	// `from` for the given org. Pass non-empty routerID, virtualKeyID, or teamID to narrow.
	SumByPeriod(orgID, routerID, virtualKeyID, teamID string, from time.Time) (requests int64, costUSD float64, err error)

	// RouterCacheQuery returns cache hit/miss counts and average miss cost for a router.
	RouterCacheQuery(orgID, routerID string, from, to *time.Time) (*RouterCacheResult, error)
	// ListTracesForRouter fetches recent logs with pipeline traces for in-memory aggregation.
	// Returns at most `limit` logs, newest first.
	ListTracesForRouter(orgID, routerID string, from, to *time.Time, limit int) ([]InferenceLog, error)
}

type AgentSessionEventRepository interface {
	Create(e *AgentSessionEvent) error
	List(filter InferenceLogFilter, limit, offset int) ([]AgentSessionEvent, int64, error)
}

type ToolCallArchiveRepository interface {
	Create(a *ToolCallArchive) error
	FindByID(orgID, id string) (*ToolCallArchive, error)
	List(filter InferenceLogFilter, limit, offset int) ([]ToolCallArchive, int64, error)
}

type CompressionEventRepository interface {
	Create(e *CompressionEvent) error
	List(filter InferenceLogFilter, limit, offset int) ([]CompressionEvent, int64, error)
}

type AuditLogRepository interface {
	Create(log *AuditLog) error
	List(orgID string, limit, offset int) ([]AuditLog, int64, error)
}

type ProviderHealthRepository interface {
	Upsert(health *ProviderHealth) error
	ListAll(orgID string) ([]ProviderHealth, error)
	FindByModelID(modelID string) (*ProviderHealth, error)
	DeleteByModelID(modelID string) error
}

// ── Inference payloads ────────────────────────────────────────────────────────

// InferencePayload stores the full prompt + response for a single inference log entry.
// Written only when the router's StorePayloads flag is true.
type InferencePayload struct {
	LogID           string    `json:"logId"          gorm:"primaryKey;size:50"`
	RouterID        string    `json:"routerId"       gorm:"size:50;not null;index"`
	RequestFields   string    `json:"requestFields"  gorm:"type:text"` // JSON-encoded fields map
	ResponseContent string    `json:"responseContent" gorm:"type:text"`
	CreatedAt       time.Time `json:"createdAt"      gorm:"not null;index"`
}

func (InferencePayload) TableName() string { return "inference_payloads" }

type InferencePayloadRepository interface {
	Create(p *InferencePayload) error
	FindByLogID(orgID, logID string) (*InferencePayload, error)
	DeleteOlderThan(cutoff time.Time) error
}
