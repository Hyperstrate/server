package domain

import (
	"time"

	"hyperstrate/server/internal/shared/dbtype"
)

// RouterInterceptorType identifies the kind of pre-routing hook.
type RouterInterceptorType string

const (
	// InterceptorSemanticClassifier embeds the incoming prompt and routes to the
	// target whose utterances are semantically closest.
	//
	// Required config:
	//   model_id  string   - model used to produce embeddings (must be registered)
	//
	// Optional config:
	//   threshold         float64  - minimum cosine similarity to accept a match (default 0.85)
	//   fallback_strategy string   - "default_strategy" | "first_target" (default "default_strategy")
	InterceptorSemanticClassifier RouterInterceptorType = "semantic_classifier"

	// InterceptorContentFilter inspects or modifies the prompt/response.
	// Config keys: blocked_patterns []string, action "block"|"redact"
	InterceptorContentFilter RouterInterceptorType = "content_filter"

	// InterceptorPIIDetector detects and optionally masks personally identifiable
	// information before the prompt reaches the model.
	// Config keys: action "mask"|"block", entities []string (e.g. ["email","phone"])
	InterceptorPIIDetector RouterInterceptorType = "pii_detector"

	// InterceptorPromptGuard validates prompts against a policy (e.g. jailbreak
	// detection). Blocks the request when a violation is found.
	// Config keys: policy "strict"|"moderate"
	InterceptorPromptGuard RouterInterceptorType = "prompt_guard"

	// InterceptorABTest splits traffic across model variants, optionally sticking
	// the same requester to the same variant via a deterministic hash.
	//
	// Required config:
	//   variants []{ name string, model_id string, weight int }
	//     - name      human-readable label recorded in analytics (e.g. "control")
	//     - model_id  registered model ID of the target to route to
	//     - weight    relative traffic weight (not required to sum to 100)
	//
	// Optional config:
	//   partition_key string  - field name whose value is hashed for sticky assignment;
	//                           when absent or the field is missing, assignment is random
	//   mode          string  - "ucb1" to use UCB1 bandit algorithm for adaptive arm selection
	InterceptorABTest RouterInterceptorType = "ab_test"

	// InterceptorPromptShield calls a fast LLM to evaluate the prompt against
	// configurable policies before forwarding. More context-aware than regex filters.
	// Config: shield_model_id string, policies []{ name, prompt, action: "block"|"flag" }
	InterceptorPromptShield RouterInterceptorType = "prompt_shield"

	// InterceptorTeamBudget enforces per-team cost/request budgets, optionally routing
	// exhausted teams to a cheaper overflow target instead of blocking.
	// Config: budgets map[team_id]{ max_cost_usd float64, overflow_target_id string, period string }
	// Team ID is read from fields["team_id"] on the request.
	InterceptorTeamBudget RouterInterceptorType = "team_budget"
)

// RouterInterceptor is a pre-routing hook that runs before target selection.
// Interceptors execute in ascending ExecutionOrder and can override which target
// receives the request. The first interceptor to produce a TargetID wins.
type RouterInterceptor struct {
	ID             string                `json:"id"             gorm:"primaryKey;size:50"`
	RouterID       string                `json:"routerId"       gorm:"size:50;not null;index"`
	Type           RouterInterceptorType `json:"type"           gorm:"size:100;not null"`
	// Config holds interceptor-specific settings. See each constant's doc for keys.
	Config         dbtype.JSONMap        `json:"config"         gorm:"serializer:json"`
	ExecutionOrder int                   `json:"executionOrder" gorm:"not null;default:0"`
	IsEnabled      bool                  `json:"isEnabled"      gorm:"not null;default:true"`
	CreatedAt      time.Time             `json:"createdAt"`
	ModifiedAt     time.Time             `json:"modifiedAt"      gorm:"autoUpdateTime"`
}
