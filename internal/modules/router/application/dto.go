package application

import (
	"encoding/json"
	"time"

	"hyperstrate/server/internal/modules/router/domain"
)

// ── Router ────────────────────────────────────────────────────────────────────

type CreateRouterInput struct {
	Name          string                 `json:"name"           binding:"required,max=255"`
	Description   string                 `json:"description"`
	Strategy      domain.RoutingStrategy `json:"strategy"       binding:"required"`
	WebhookURL    string                 `json:"webhookUrl"     binding:"omitempty,url"`
	PromptID      string                 `json:"promptId"`
	StorePayloads bool                   `json:"storePayloads"`
}

type UpdateRouterInput struct {
	Name          *string                 `json:"name"`
	Description   *string                 `json:"description"`
	Status        *domain.RouterStatus    `json:"status"`
	Strategy      *domain.RoutingStrategy `json:"strategy"`
	WebhookURL    *string                 `json:"webhookUrl"`
	PromptID      *string                 `json:"promptId"`
	StorePayloads *bool                   `json:"storePayloads"`
}

type RouterResponse struct {
	ID            string                 `json:"id"             validate:"required"`
	Name          string                 `json:"name"           validate:"required"`
	Description   string                 `json:"description"    validate:"required"`
	Status        domain.RouterStatus    `json:"status"         validate:"required"`
	Strategy      domain.RoutingStrategy `json:"strategy"       validate:"required"`
	WebhookURL    string                 `json:"webhookUrl,omitempty"`
	PromptID      string                 `json:"promptId,omitempty"`
	StorePayloads bool                   `json:"storePayloads"`
	CreatedAt     time.Time              `json:"createdAt"      validate:"required"`
	ModifiedAt    time.Time              `json:"modifiedAt"     validate:"required"`
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func toRouterResponse(r *domain.Router) RouterResponse {
	return RouterResponse{
		ID:            r.ID,
		Name:          r.Name,
		Description:   r.Description,
		Status:        r.Status,
		Strategy:      r.Strategy,
		WebhookURL:    r.Configuration.WebhookURL,
		PromptID:      derefStr(r.Configuration.PromptID),
		StorePayloads: r.Configuration.StorePayloads,
		CreatedAt:     r.CreatedAt,
		ModifiedAt:    r.ModifiedAt,
	}
}

// ── RouterTarget ─────────────────────────────────────────────────────────────

type AddTargetInput struct {
	ModelID    string  `json:"modelId"        binding:"required"`
	Weight     int     `json:"weight"`
	Percentage float64 `json:"percentage"     binding:"min=0,max=100"`
	Priority   int     `json:"priority"`
	// PromptID optionally attaches a stored system prompt to this target.
	// Takes precedence over the router-level system prompt during inference.
	PromptID string `json:"promptId"`
}

type UpdateTargetInput struct {
	Weight     *int     `json:"weight"`
	Percentage *float64 `json:"percentage"`
	Priority   *int     `json:"priority"`
	IsEnabled  *bool    `json:"isEnabled"`
	PromptID   *string  `json:"promptId"`
}

type RouterTargetResponse struct {
	ID         string    `json:"id"             validate:"required"`
	RouterID   string    `json:"routerId"       validate:"required"`
	ModelID    string    `json:"modelId"        validate:"required"`
	Weight     int       `json:"weight"         validate:"required"`
	Percentage float64   `json:"percentage"     validate:"required"`
	Priority   int       `json:"priority"       validate:"required"`
	IsEnabled  bool      `json:"isEnabled"      validate:"required"`
	PromptID   string    `json:"promptId,omitempty"`
	CreatedAt  time.Time `json:"createdAt"      validate:"required"`
	ModifiedAt time.Time `json:"modifiedAt"     validate:"required"`
}

func toTargetResponse(t *domain.RouterTarget) RouterTargetResponse {
	return RouterTargetResponse{
		ID:         t.ID,
		RouterID:   t.RouterID,
		ModelID:    t.ModelID,
		Weight:     t.Weight,
		Percentage: t.Percentage,
		Priority:   t.Priority,
		IsEnabled:  t.IsEnabled,
		PromptID:   derefStr(t.PromptID),
		CreatedAt:  t.CreatedAt,
		ModifiedAt: t.ModifiedAt,
	}
}

// ── RouterFeature ─────────────────────────────────────────────────────────────

type AddFeatureInput struct {
	FeatureType    domain.RouterFeatureType `json:"featureType"    binding:"required"`
	Config         json.RawMessage          `json:"config"         swaggertype:"object"`
	ExecutionOrder int                      `json:"executionOrder"`
}

type UpdateFeatureInput struct {
	Config         *json.RawMessage `json:"config" swaggertype:"object"`
	ExecutionOrder *int             `json:"executionOrder"`
	IsEnabled      *bool            `json:"isEnabled"`
}

type RouterFeatureResponse struct {
	ID             string                   `json:"id"             validate:"required"`
	RouterID       string                   `json:"routerId"       validate:"required"`
	FeatureType    domain.RouterFeatureType `json:"featureType"    validate:"required"`
	Config         map[string]any           `json:"config"         validate:"required"`
	ExecutionOrder int                      `json:"executionOrder" validate:"required"`
	IsEnabled      bool                     `json:"isEnabled"      validate:"required"`
	CreatedAt      time.Time                `json:"createdAt"      validate:"required"`
	ModifiedAt     time.Time                `json:"modifiedAt"     validate:"required"`
}

type FeatureTokenOptimizationConfig struct {
	MaxChars int `json:"max_chars,omitempty"`
}

type FeatureContextTrimmingConfig struct {
	MaxChars int `json:"max_chars,omitempty"`
}

type FeatureResponseCacheConfig struct {
	TTLSeconds int `json:"ttl_seconds,omitempty"`
}

type FeatureSemanticCacheConfig struct {
	TTLSeconds          int     `json:"ttl_seconds,omitempty"`
	SimilarityThreshold float64 `json:"similarity_threshold,omitempty"`
	ModelID             string  `json:"model_id,omitempty"`
}

type FeatureRetryConfig struct {
	MaxRetries        int     `json:"max_retries,omitempty"`
	InitialDelayMs    int     `json:"initial_delay_ms,omitempty"`
	BackoffMultiplier float64 `json:"backoff_multiplier,omitempty"`
}

type FeatureRateLimitConfig struct {
	RPS   float64 `json:"rps,omitempty"`
	Burst int     `json:"burst,omitempty"`
}

type FeatureBudgetConfig struct {
	Period        string                  `json:"period,omitempty"`
	MaxRequests   int                     `json:"max_requests,omitempty"`
	MaxCostUSD    float64                 `json:"max_cost_usd,omitempty"`
	AlertPercent  float64                 `json:"alert_percent,omitempty"`
	AgentBudgets  map[string]ScopedBudget `json:"agent_budgets,omitempty"`
	RoleBudgets   map[string]ScopedBudget `json:"role_budgets,omitempty"`
	RepoBudgets   map[string]ScopedBudget `json:"repo_budgets,omitempty"`
	BranchBudgets map[string]ScopedBudget `json:"branch_budgets,omitempty"`
}

type FeatureMCPToolsConfig struct {
	ServerIDs       []string `json:"server_ids,omitempty"`
	MaxTurns        int      `json:"max_turns,omitempty"`
	RequireApproval bool     `json:"require_approval,omitempty"`
	AllowedTools    []string `json:"allowed_tools,omitempty"`
	BlockedTools    []string `json:"blocked_tools,omitempty"`
	AllowedTeamIDs  []string `json:"allowed_team_ids,omitempty"`
}

type FeatureStructuredOutputConfig struct {
	Schema map[string]any `json:"schema,omitempty"`
	Name   string         `json:"name,omitempty"`
	Strict bool           `json:"strict,omitempty"`
}

type FeatureRequestCoalescingConfig struct {
	WindowMs   int `json:"window_ms,omitempty"`
	MaxWaiters int `json:"max_waiters,omitempty"`
}

type FeatureHedgingConfig struct {
	QualityCheck string   `json:"quality_check,omitempty"`
	TargetIDs    []string `json:"target_ids,omitempty"`
	Targets      []string `json:"targets,omitempty"`
	MinLength    int      `json:"min_length,omitempty"`
	TimeoutMs    int      `json:"timeout_ms,omitempty"`
}

type FeatureQualityGateConfig struct {
	JudgeModelID  string  `json:"judge_model_id,omitempty"`
	MinScore      float64 `json:"min_score,omitempty"`
	Action        string  `json:"action,omitempty"`
	RubricPrompt  string  `json:"rubric_prompt,omitempty"`
	RetryTargetID string  `json:"retry_target_id,omitempty"`
}

type FeatureContextCompressionConfig struct {
	MaxChars   int `json:"max_chars,omitempty"`
	KeepRecent int `json:"keep_recent,omitempty"`
}

type FeatureSemanticMemoryConfig struct {
	ModelID             string  `json:"model_id,omitempty"`
	MaxExamples         int     `json:"max_examples,omitempty"`
	TTLDays             int     `json:"ttl_days,omitempty"`
	SimilarityThreshold float64 `json:"similarity_threshold,omitempty"`
}

type FeatureCostAwareRoutingConfig struct {
	Thresholds      []CostAwareThreshold `json:"thresholds,omitempty"`
	DefaultTargetID string               `json:"default_target_id,omitempty"`
}

type FeatureResponsePrefetchConfig struct {
	FollowUpPrompts []string `json:"follow_up_prompts,omitempty"`
	TTLSeconds      int      `json:"ttl_seconds,omitempty"`
}

type FeatureResponseFingerprintingConfig struct {
	WindowSize     int     `json:"window_size,omitempty"`
	AlertThreshold float64 `json:"alert_threshold,omitempty"`
}

type FeaturePromptOptimizerConfig struct {
	Fields        []string                      `json:"fields,omitempty"`
	Optimizers    []string                      `json:"optimizers,omitempty"`
	ProtectedTags []PromptOptimizerProtectedTag `json:"protected_tags,omitempty"`
}

type FeatureTokenCostOptimizationConfig struct {
	Fields             []string `json:"fields,omitempty"`
	MinifyJSON         *bool    `json:"minify_json,omitempty"`
	CollapseBlankLines *bool    `json:"collapse_blank_lines,omitempty"`
	CompactWhitespace  *bool    `json:"compact_whitespace,omitempty"`
	DedupeLines        *bool    `json:"dedupe_lines,omitempty"`
	MaxChars           int      `json:"max_chars,omitempty"`
	MaxPromptChars     int      `json:"max_prompt_chars,omitempty"`
	OutputMaxTokens    int      `json:"output_max_tokens,omitempty"`
	RewriteModelID     string   `json:"rewrite_model_id,omitempty"`
	RewriteMinChars    int      `json:"rewrite_min_chars,omitempty"`
	RewriteTargetChars int      `json:"rewrite_target_chars,omitempty"`
	RewriteTargetRatio float64  `json:"rewrite_target_ratio,omitempty"`
}

type FeaturePromptPolicyRolloutConfig struct {
	Variants []PromptPolicyRolloutVariant `json:"variants,omitempty"`
}

type EmptyFeatureConfig struct{}

type ScopedBudget struct {
	Period      string  `json:"period,omitempty"`
	MaxRequests int     `json:"max_requests,omitempty"`
	MaxCostUSD  float64 `json:"max_cost_usd,omitempty"`
}

type CostAwareThreshold struct {
	MaxChars int    `json:"max_chars,omitempty"`
	TargetID string `json:"target_id,omitempty"`
}

type PromptOptimizerProtectedTag struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

type PromptPolicyRolloutVariant struct {
	Name        string  `json:"name,omitempty"`
	PromptID    string  `json:"prompt_id,omitempty"`
	PromptIDAlt string  `json:"promptId,omitempty"`
	Percentage  float64 `json:"percentage,omitempty"`
}

func featureConfigSchemaForType(t domain.RouterFeatureType) any {
	switch t {
	case domain.FeatureTokenOptimization:
		return &FeatureTokenOptimizationConfig{}
	case domain.FeatureContextTrimming:
		return &FeatureContextTrimmingConfig{}
	case domain.FeatureResponseCache:
		return &FeatureResponseCacheConfig{}
	case domain.FeatureSemanticCache:
		return &FeatureSemanticCacheConfig{}
	case domain.FeatureRetry:
		return &FeatureRetryConfig{}
	case domain.FeatureRateLimit:
		return &FeatureRateLimitConfig{}
	case domain.FeatureBudget:
		return &FeatureBudgetConfig{}
	case domain.FeatureMCPTools:
		return &FeatureMCPToolsConfig{}
	case domain.FeatureStructuredOutput:
		return &FeatureStructuredOutputConfig{}
	case domain.FeatureRequestCoalescing:
		return &FeatureRequestCoalescingConfig{}
	case domain.FeatureHedging:
		return &FeatureHedgingConfig{}
	case domain.FeatureQualityGate:
		return &FeatureQualityGateConfig{}
	case domain.FeatureContextCompression:
		return &FeatureContextCompressionConfig{}
	case domain.FeatureSemanticMemory:
		return &FeatureSemanticMemoryConfig{}
	case domain.FeatureCostAwareRouting:
		return &FeatureCostAwareRoutingConfig{}
	case domain.FeatureResponsePrefetch:
		return &FeatureResponsePrefetchConfig{}
	case domain.FeatureResponseFingerprinting:
		return &FeatureResponseFingerprintingConfig{}
	case domain.FeatureTokenCostOptimization:
		return &FeatureTokenCostOptimizationConfig{}
	case domain.FeaturePromptOptimizer:
		return &FeaturePromptOptimizerConfig{}
	case domain.FeaturePromptPolicyRollout:
		return &FeaturePromptPolicyRolloutConfig{}
	case domain.FeatureFallback, domain.FeatureHealthCheck, domain.FeaturePromptCaching:
		return &EmptyFeatureConfig{}
	default:
		return &map[string]any{}
	}
}

func featureConfigRawToMap(t domain.RouterFeatureType, raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	cfg := featureConfigSchemaForType(t)
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, err
	}
	normalized, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(normalized, &out); err != nil || out == nil {
		return map[string]any{}, err
	}
	return out, nil
}

func featureConfigMapForType(t domain.RouterFeatureType, cfg map[string]any) map[string]any {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return map[string]any{}
	}
	out, err := featureConfigRawToMap(t, raw)
	if err != nil {
		return map[string]any{}
	}
	return out
}

// RouterLintIssue describes a configuration problem or recommendation for a router.
type RouterLintIssue struct {
	Severity string `json:"severity"` // error|warning|info
	Code     string `json:"code"`
	Message  string `json:"message"`
	Feature  string `json:"feature,omitempty"`
}

// RouterLintResponse is returned by the router compatibility checker.
type RouterLintResponse struct {
	RouterID string            `json:"routerId"`
	OK       bool              `json:"ok"`
	Issues   []RouterLintIssue `json:"issues"`
}

func toFeatureResponse(f *domain.RouterFeature) RouterFeatureResponse {
	return RouterFeatureResponse{
		ID:             f.ID,
		RouterID:       f.RouterID,
		FeatureType:    f.FeatureType,
		Config:         featureConfigMapForType(f.FeatureType, f.Config),
		ExecutionOrder: f.ExecutionOrder,
		IsEnabled:      f.IsEnabled,
		CreatedAt:      f.CreatedAt,
		ModifiedAt:     f.ModifiedAt,
	}
}

// ── RouterInterceptor ─────────────────────────────────────────────────────────

type AddInterceptorInput struct {
	Type           domain.RouterInterceptorType `json:"type"           binding:"required"`
	Config         map[string]any               `json:"config"`
	ExecutionOrder *int                         `json:"executionOrder"` // nil → auto-assign next slot
}

type UpdateInterceptorInput struct {
	Config         map[string]any `json:"config"`
	ExecutionOrder *int           `json:"executionOrder"`
	IsEnabled      *bool          `json:"isEnabled"`
}

type RouterInterceptorResponse struct {
	ID             string                       `json:"id"             validate:"required"`
	RouterID       string                       `json:"routerId"       validate:"required"`
	Type           domain.RouterInterceptorType `json:"type"           validate:"required"`
	Config         map[string]any               `json:"config"         validate:"required"`
	ExecutionOrder int                          `json:"executionOrder" validate:"required"`
	IsEnabled      bool                         `json:"isEnabled"      validate:"required"`
	CreatedAt      time.Time                    `json:"createdAt"      validate:"required"`
	ModifiedAt     time.Time                    `json:"modifiedAt"     validate:"required"`
}

func toInterceptorResponse(i *domain.RouterInterceptor) RouterInterceptorResponse {
	cfg := i.Config
	if cfg == nil {
		cfg = map[string]any{}
	}
	return RouterInterceptorResponse{
		ID:             i.ID,
		RouterID:       i.RouterID,
		Type:           i.Type,
		Config:         cfg,
		ExecutionOrder: i.ExecutionOrder,
		IsEnabled:      i.IsEnabled,
		CreatedAt:      i.CreatedAt,
		ModifiedAt:     i.ModifiedAt,
	}
}

// ── Inference ─────────────────────────────────────────────────────────────────

// DryRunTarget is the cost estimate for one eligible model target.
type DryRunTarget struct {
	ModelID                string  `json:"modelId"`
	ModelDefKey            string  `json:"modelDefKey"`
	DisplayName            string  `json:"displayName"`
	Provider               string  `json:"provider"`
	InputPricePer1MTokens  float64 `json:"inputPricePer1MTokens"`
	OutputPricePer1MTokens float64 `json:"outputPricePer1MTokens"`
	EstimatedInputCostUSD  float64 `json:"estimatedInputCostUsd"`
}

// DryRunResult is returned when ?dryRun=true is passed to an inference endpoint.
// No model is called; the result contains only cost estimates based on token counts.
type DryRunResult struct {
	EstimatedInputTokens int            `json:"estimatedInputTokens"`
	Targets              []DryRunTarget `json:"targets"`
}

// RouteInferInput is the payload for routing an inference request through a router.
type RouteInferInput struct {
	Fields      map[string]string `json:"fields"   binding:"required"`
	Options     map[string]any    `json:"options"`
	BypassCache bool              `json:"bypassCache"`
}

// PipelineStep records what happened at one phase of the request lifecycle.
type PipelineStep struct {
	Phase      int     `json:"phase"`
	Kind       string  `json:"kind"` // rate_limit|budget|cache|transform|interceptor|target_select|inference|budget_accounting|cache_store
	Name       string  `json:"name"`
	Outcome    string  `json:"outcome"` // passed|blocked|hit_exact|hit_semantic|miss|applied|skipped|routed|masked|success|error|recorded|stored
	Detail     string  `json:"detail,omitempty"`
	DurationMs float64 `json:"durationMs"`         // how long this step took, fractional ms (µs resolution)
	OffsetMs   float64 `json:"offsetMs"`           // ms elapsed since pipeline start when step started, fractional ms
	Attempts   int     `json:"attempts,omitempty"` // retry count (inference step only)
}

// ToolCallCapture keeps the full payload/response for tool calls separately
// from the compact pipeline trace.
type ToolCallCapture struct {
	ToolName        string `json:"toolName"`
	ToolCallID      string `json:"toolCallId,omitempty"`
	RequestPayload  string `json:"requestPayload,omitempty"`
	ResponsePayload string `json:"responsePayload,omitempty"`
	ResponseChars   int    `json:"responseChars,omitempty"`
	ErrorMessage    string `json:"errorMessage,omitempty"`
}

// RouteInferResult contains the model's response and routing metadata.
type RouteInferResult struct {
	Content           string         `json:"content"              validate:"required"`
	SelectedModelID   string         `json:"selectedModelId"      validate:"required"`
	SelectedTargetID  string         `json:"selectedTargetId,omitempty"`
	ModelDefKey       string         `json:"modelDefKey"`
	Provider          string         `json:"provider"`
	InputTokens       int64          `json:"inputTokens"`
	OutputTokens      int64          `json:"outputTokens"`
	CachedInputTokens int64          `json:"cachedInputTokens,omitempty"`
	CostUSD           float64        `json:"costUsd"`
	ABVariant         string         `json:"abVariant,omitempty"`
	CacheHit          bool           `json:"-"`
	CacheHitType      string         `json:"-"` // "exact" | "semantic" | ""
	Steps             []PipelineStep `json:"pipelineSteps,omitempty"`
	// ToolCalls is non-nil when the model responded with tool/function calls
	// instead of text content.
	ToolCalls        json.RawMessage   `json:"toolCalls,omitempty"`
	ToolCallCaptures []ToolCallCapture `json:"-"`
}

// StreamChunk represents a single delta in a streaming inference response.
// The channel returned by RouteInferStream emits these until Done is true or Err is set.
// SelectedModelID, InputTokens, OutputTokens, ModelDefKey, Provider, CostUSD, and
// ToolCalls are populated on the final (Done) chunk.
type StreamChunk struct {
	Delta             string
	Done              bool
	Err               error
	SelectedModelID   string
	SelectedTargetID  string
	InputTokens       int64
	OutputTokens      int64
	CachedInputTokens int64
	ModelDefKey       string
	Provider          string
	CostUSD           float64
	ABVariant         string
	CacheHit          bool
	CacheHitType      string // "exact" | "semantic" | ""
	ToolCalls         json.RawMessage
}

// ── Router export / import ────────────────────────────────────────────────────

// RouterExport is a portable snapshot of a router configuration that can be
// used to clone a router within the same org or restore from a backup.
// Model IDs in targets are replaced with their model-definition keys so the
// snapshot is portable across environments.
type RouterExport struct {
	Version      string                `json:"version"`
	Router       ExportedRouter        `json:"router"`
	Targets      []ExportedTarget      `json:"targets"`
	Features     []ExportedFeature     `json:"features"`
	Interceptors []ExportedInterceptor `json:"interceptors"`
}

type ExportedRouter struct {
	Name          string                     `json:"name"`
	Strategy      domain.RoutingStrategy     `json:"strategy"`
	Configuration domain.RouterConfiguration `json:"configuration"`
}

type ExportedTarget struct {
	Key                string  `json:"key,omitempty"`
	ModelDefinitionKey string  `json:"modelDefinitionKey"`
	Weight             int     `json:"weight"`
	Priority           int     `json:"priority"`
	Percentage         float64 `json:"percentage,omitempty"`
	IsEnabled          bool    `json:"isEnabled"`
	PromptID           string  `json:"promptId,omitempty"`
	// Note: execution order for targets is implicit (list index).
}

type ExportedFeature struct {
	FeatureType    domain.RouterFeatureType `json:"featureType"`
	Config         map[string]any           `json:"config"`
	IsEnabled      bool                     `json:"isEnabled"`
	ExecutionOrder int                      `json:"executionOrder"`
}

type ExportedInterceptor struct {
	Type           domain.RouterInterceptorType `json:"type"`
	Config         map[string]any               `json:"config"`
	IsEnabled      bool                         `json:"isEnabled"`
	ExecutionOrder int                          `json:"executionOrder"`
}

// ImportRouterInput wraps a RouterExport for the import endpoint with an
// optional name override so users can clone a router without renaming the JSON.
type ImportRouterInput struct {
	RouterExport
	NameOverride string `json:"nameOverride,omitempty"`
}

// ── MCP Tool discovery ────────────────────────────────────────────────────────

// MCPToolDefinition mirrors the tool shape returned by an MCP server's tools/list.
type MCPToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// MCPServerTools groups tool definitions returned by one MCP server.
type MCPServerTools struct {
	ServerID   string              `json:"serverId"`
	ServerName string              `json:"serverName"`
	ServerURL  string              `json:"serverUrl"`
	Tools      []MCPToolDefinition `json:"tools"`
}
