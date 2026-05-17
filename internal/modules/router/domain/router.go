package domain

import (
	"time"

	"hyperstrate/server/internal/shared/dbtype"
)

// RouterStatus controls whether a router accepts traffic.
type RouterStatus string

const (
	RouterStatusActive   RouterStatus = "active"
	RouterStatusInactive RouterStatus = "inactive"
	RouterStatusDraft    RouterStatus = "draft"
)

// RoutingStrategy determines how the router selects a target when no interceptor
// overrides the decision.
type RoutingStrategy string

const (
	RoutingStrategyRoundRobin   RoutingStrategy = "round_robin"
	RoutingStrategyWeighted     RoutingStrategy = "weighted"
	RoutingStrategyPercentage   RoutingStrategy = "percentage"
	RoutingStrategyFailover     RoutingStrategy = "failover"
	RoutingStrategyRandom       RoutingStrategy = "random"
	RoutingStrategyLatencyBased RoutingStrategy = "latency_based"
)

// RouterFeatureType identifies a pluggable pipeline stage.
type RouterFeatureType string

const (
	// FeatureTokenOptimization trims the prompt to max_chars from the start.
	FeatureTokenOptimization RouterFeatureType = "token_optimization"
	// FeatureResponseCache caches identical (exact-match) prompts.
	FeatureResponseCache RouterFeatureType = "response_cache"
	// FeatureSemanticCache caches by prompt similarity; needs EmbeddingProvider.
	FeatureSemanticCache RouterFeatureType = "semantic_cache"
	// FeatureRetry retries the same target with exponential back-off.
	FeatureRetry RouterFeatureType = "retry"
	// FeatureFallback tries each enabled target in priority order on failure.
	FeatureFallback RouterFeatureType = "fallback"
	// FeatureContextTrimming keeps the last max_chars of the prompt.
	FeatureContextTrimming RouterFeatureType = "context_trimming"
	// FeatureTokenCostOptimization applies deterministic request-payload rewrites
	// that reduce input/output token cost without an extra model call.
	// Config: fields []string (default prompt/systemPrompt/_history),
	//         minify_json bool (default true), collapse_blank_lines bool (default true),
	//         compact_whitespace bool (default false), dedupe_lines bool (default false),
	//         max_chars map[field]int, max_prompt_chars int, output_max_tokens int,
	//         rewrite_model_id string (optional), rewrite_min_chars int,
	//         rewrite_target_chars int or rewrite_target_ratio float64.
	FeatureTokenCostOptimization RouterFeatureType = "token_cost_optimization"
	// FeaturePromptOptimizer applies prompt-optimizer style sequential text
	// optimizers with protected spans. Unlike token_cost_optimization, this is
	// intended for linguistic prompt rewriting and may trade quality for cost.
	// Config: fields []string (default prompt), optimizers []string
	//         ("punctuation", "stopwords", "compact_whitespace", "dedupe_lines",
	//         "lowercase"), protected_tags []{start,end}
	FeaturePromptOptimizer RouterFeatureType = "prompt_optimizer"
	// FeaturePromptPolicyRollout canaries stored prompt/policy variants by
	// injecting a configured prompt_id into systemPrompt for a percentage of
	// traffic. Config: variants []{name string, prompt_id string, percentage float64}
	FeaturePromptPolicyRollout RouterFeatureType = "prompt_policy_rollout"
	// FeatureRateLimit enforces per-router requests-per-second limits.
	FeatureRateLimit RouterFeatureType = "rate_limit"
	// FeatureBudget enforces per-router spending / request-count limits.
	FeatureBudget RouterFeatureType = "budget"
	// FeatureMCPTools attaches one or more MCP (Model Context Protocol) servers.
	// When a model returns tool_calls, the pipeline dispatches them to the
	// configured MCP servers and re-infers with the tool results injected.
	FeatureMCPTools RouterFeatureType = "mcp_tools"
	// FeatureHealthCheck enables auto-degradation routing: targets whose model
	// has been marked unhealthy by the background health monitor are skipped
	// during target selection and the fallback chain. No additional config is
	// required — the health state is read from the observability module.
	FeatureHealthCheck RouterFeatureType = "health_check"
	// FeaturePromptCaching enables provider-side prompt caching for system
	// prompts. For Anthropic, adds the prompt-caching-2024-07-31 beta header
	// and wraps the system prompt in an ephemeral cache_control block. For
	// OpenAI, disk caching is automatic for long contexts; this feature is a
	// no-op for other providers. Config: none required.
	FeaturePromptCaching RouterFeatureType = "prompt_caching"
	// FeatureStructuredOutput enforces a JSON schema on model responses.
	// Config keys:
	//   schema  map[string]any  JSON Schema object (required)
	//   name    string          schema name sent to providers (default "response")
	//   strict  bool            enable strict mode for OpenAI (default false)
	FeatureStructuredOutput RouterFeatureType = "structured_output"
	// FeatureRequestCoalescing deduplicates concurrent requests with identical
	// prompts by making exactly one upstream call and fanning the result out to
	// all waiters. Unlike caching, coalescing operates on in-flight requests so
	// every response is fresh. Config keys:
	//   window_ms   int  max ms a waiter blocks before making its own call (default 200)
	//   max_waiters int  max concurrent waiters per key; 0 = unlimited (default 0)
	FeatureRequestCoalescing RouterFeatureType = "request_coalescing"

	// FeatureHedging fires all specified target models in parallel and returns the
	// first response that passes the quality check. Slower responses are cancelled.
	// Config: targets []string (model IDs), quality_check "any"|"min_length"|"valid_json"|"no_refusal",
	//         min_length int, timeout_ms int
	FeatureHedging RouterFeatureType = "hedging"

	// FeatureQualityGate calls a judge model to score inference results against a rubric.
	// Config: judge_model_id string, rubric_prompt string, min_score float64 (1-10, default 7),
	//         action "retry"|"error"|"flag", retry_target_id string (optional)
	FeatureQualityGate RouterFeatureType = "quality_gate"

	// FeatureContextCompression evicts lowest-relevance history turns to keep
	// total character count under a budget.
	// Config: max_chars int, keep_recent int (always keep last N turns, default 2)
	FeatureContextCompression RouterFeatureType = "context_compression"

	// FeatureSemanticMemory stores past Q&A interactions and injects similar
	// examples as few-shot context using embedding similarity search.
	// Config: model_id string (embedding), max_examples int (default 3),
	//         similarity_threshold float64 (default 0.85), ttl_days int (default 30)
	FeatureSemanticMemory RouterFeatureType = "semantic_memory"

	// FeatureCostAwareRouting selects a target based on the total input character
	// count, routing cheap short requests to smaller/cheaper models.
	// Config: thresholds []{ max_chars int, target_id string } (sorted ascending),
	//         default_target_id string
	FeatureCostAwareRouting RouterFeatureType = "cost_aware_routing"

	// FeatureResponsePrefetch speculatively infers follow-up prompts and caches
	// the results to reduce latency on predictable follow-up requests.
	// Config: follow_up_prompts []string, ttl_seconds int (default 300)
	FeatureResponsePrefetch RouterFeatureType = "response_prefetch"

	// FeatureResponseFingerprinting tracks rolling response length statistics to
	// detect anomalous response patterns (model drift, injection campaigns).
	// Config: window_size int (default 100), alert_threshold float64 (stddev multiplier, default 3.0)
	FeatureResponseFingerprinting RouterFeatureType = "response_fingerprinting"
)

// RouterConfiguration holds per-router settings that are updated independently
// from the core routing fields. Stored in router_configurations (1:1 with routers).
type RouterConfiguration struct {
	RouterID string `json:"routerId"      gorm:"primaryKey;size:50"`
	// WebhookURL is an optional HTTP endpoint that receives POST notifications
	// when significant events occur on this router (budget exceeded, all targets
	// failed, rate limit exceeded, budget alert threshold crossed).
	WebhookURL string `json:"webhookUrl"    gorm:"size:2000"`
	// PromptID optionally references a stored prompt (prompts module).
	// When set, the prompt content is injected as fields["systemPrompt"] before
	// each inference call, with {{variable}} placeholders interpolated from fields.
	PromptID *string `json:"promptId"      gorm:"size:50"`
	// StorePayloads enables full prompt+response persistence for this router.
	// When true, the raw request fields and response content are saved to inference_payloads.
	StorePayloads bool      `json:"storePayloads" gorm:"not null;default:false"`
	ModifiedAt    time.Time `json:"modifiedAt"    gorm:"autoUpdateTime"`
}

// Router is the top-level entity.
type Router struct {
	ID              string              `json:"id"               gorm:"primaryKey;size:50"`
	OrgID           string              `json:"-"                gorm:"size:50;not null;index"`
	Name            string              `json:"name"             gorm:"size:255;not null"`
	Description     string              `json:"description"      gorm:"type:text"`
	Status          RouterStatus        `json:"status"           gorm:"size:50;not null;default:'draft'"`
	Strategy        RoutingStrategy     `json:"strategy"         gorm:"size:50;not null;default:'round_robin'"`
	RoundRobinIndex int                 `json:"roundRobinIndex"  gorm:"not null;default:0"`
	Configuration   RouterConfiguration `json:"configuration"    gorm:"foreignKey:RouterID"`
	CreatedAt       time.Time           `json:"createdAt"`
	ModifiedAt      time.Time           `json:"modifiedAt"       gorm:"autoUpdateTime"`
}

// RouterTeamAccess restricts inference on a router to specific teams.
// When no rows exist for a router, the router is open to all authenticated callers.
// When one or more rows exist, only callers whose virtual key belongs to an allowed
// team may call the router (admin session tokens always bypass this check).
type RouterTeamAccess struct {
	ID       string `json:"id"       gorm:"primaryKey;size:50"`
	RouterID string `json:"routerId" gorm:"size:50;not null;index"`
	TeamID   string `json:"teamId"   gorm:"size:50;not null"`
	OrgID    string `json:"orgId"    gorm:"size:50;not null"`
}

// RouterTarget links a Router to a registered AI model and carries routing
// parameters. Utterances for semantic_classifier routing live in the interceptor
// config (config.targets[targetID]) rather than on the target itself.
type RouterTarget struct {
	ID         string  `json:"id"             gorm:"primaryKey;size:50"`
	RouterID   string  `json:"routerId"       gorm:"size:50;not null;index"`
	ModelID    string  `json:"modelId"        gorm:"size:50;not null"`
	Weight     int     `json:"weight"         gorm:"not null;default:1"`
	Percentage float64 `json:"percentage"     gorm:"not null;default:0"`
	Priority   int     `json:"priority"       gorm:"not null;default:0"`
	IsEnabled  bool    `json:"isEnabled"      gorm:"not null;default:true"`
	// PromptID overrides the router-level system prompt for inference
	// through this specific target. Takes precedence over Router.PromptID.
	PromptID   *string   `json:"promptId" gorm:"size:50"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"     gorm:"autoUpdateTime"`
}

// RouterFeature is a pluggable pipeline stage (pre/post inference processing).
type RouterFeature struct {
	ID             string            `json:"id"             gorm:"primaryKey;size:50"`
	RouterID       string            `json:"routerId"       gorm:"size:50;not null;index"`
	FeatureType    RouterFeatureType `json:"featureType"    gorm:"size:100;not null"`
	Config         dbtype.JSONMap    `json:"config"         gorm:"serializer:json"`
	ExecutionOrder int               `json:"executionOrder" gorm:"not null;default:0"`
	IsEnabled      bool              `json:"isEnabled"      gorm:"not null;default:true"`
	CreatedAt      time.Time         `json:"createdAt"`
	ModifiedAt     time.Time         `json:"modifiedAt"     gorm:"autoUpdateTime"`
}
