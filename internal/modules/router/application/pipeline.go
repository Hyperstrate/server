package application

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"

	authDomain "hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/modules/router/domain"
	tmpl "hyperstrate/server/internal/shared/template"
)

// featurePipeline holds all in-process state shared across requests.
// One instance is created per service and lives for the lifetime of the process.
type featurePipeline struct {
	embedder EmbeddingProvider // nil → semantic features degrade gracefully
	embCache *embeddingCache   // centroid cache keyed by (modelID, targetID, utterances)

	cacheStore      CacheStore // exact-match cache (memory or redis)
	semanticMu      sync.Mutex
	semanticEntries []semanticCacheEntry

	limiters  sync.Map // rate limiters: routerID → *tokenBucket
	latencies sync.Map // latency EMA: targetID → float64 (ms)

	circuits *circuitBreakerPool // one breaker per target
	metrics  *pipelineMetrics

	// promptLoader resolves stored system prompts by ID for target-level injection.
	// nil = target system-prompt injection is disabled.
	promptLoader PromptLoader

	// healthChecker filters out targets whose model is marked unhealthy.
	// nil = health-check feature is a no-op even when enabled.
	healthChecker HealthChecker

	// budgetQuerier reads actual spend from inference_logs.
	budgetQuerier BudgetQuerier

	// mcpLoader resolves MCP server IDs to their URL + auth config.
	// nil = only inline URL-based server config is supported.
	mcpLoader MCPServerLoader

	// coalescer deduplicates concurrent requests with identical prompts.
	// One instance per pipeline (per service), shared across all requests.
	coalescer *coalescer

	// memStores holds per-router semantic memory (in-memory brute-force vector index).
	memStores sync.Map // routerID → *memoryStore
	// fingerprints holds per-router response length statistics for anomaly detection.
	fingerprints sync.Map // routerID → *fingerprintStats
	// ucbState holds per-interceptor UCB1 arm statistics for adaptive A/B testing.
	ucbState sync.Map // interceptorID → *ucb1State
	// teamBudgets holds per-team spending counters for the team_budget interceptor.
	teamBudgets sync.Map // key → *teamBudgetCounter
	// scopedBudgets holds in-memory counters for agent/role/repo/branch budget controls.
	scopedBudgets sync.Map // key → *teamBudgetCounter
}

func newFeaturePipeline(embedder EmbeddingProvider, store CacheStore, promptLoader PromptLoader, healthChecker HealthChecker, mcpLoader MCPServerLoader, budgetQuerier BudgetQuerier) *featurePipeline {
	if store == nil {
		store = NewMemoryCacheStore()
	}
	return &featurePipeline{
		embedder:      embedder,
		embCache:      newEmbeddingCache(),
		cacheStore:    store,
		circuits:      newCircuitBreakerPool(),
		metrics:       newPipelineMetrics(),
		promptLoader:  promptLoader,
		healthChecker: healthChecker,
		mcpLoader:     mcpLoader,
		coalescer:     newCoalescer(),
		budgetQuerier: budgetQuerier,
	}
}

// hasTargetSystemPrompts returns true when any enabled target carries a
// PromptID. The pipeline uses this to decide whether the exact cache key
// must be computed after target selection (per-target) rather than before it.
func hasTargetSystemPrompts(targets []domain.RouterTarget) bool {
	for _, t := range targets {
		if t.IsEnabled && t.PromptID != nil && *t.PromptID != "" {
			return true
		}
	}
	return false
}

// injectTargetSystemPrompt loads a target's stored system prompt, interpolates
// {{variable}} placeholders from fields, and sets fields["systemPrompt"].
// Returns true when the prompt was injected (caller must recompute the cache key).
func (p *featurePipeline) injectTargetSystemPrompt(ctx context.Context, target *domain.RouterTarget, fields map[string]string) bool {
	if target.PromptID == nil || *target.PromptID == "" || p.promptLoader == nil {
		return false
	}
	content, err := p.promptLoader.GetContent(ctx, *target.PromptID)
	if err != nil || content == "" {
		return false
	}

	fields["systemPrompt"] = tmpl.Interpolate(content, fields)
	return true
}

func (p *featurePipeline) applyPromptPolicyRollout(ctx context.Context, fields map[string]string, options map[string]any, cfg map[string]any) (string, bool) {
	if p.promptLoader == nil {
		return "", false
	}
	variants, _ := cfg["variants"].([]any)
	if len(variants) == 0 {
		return "", false
	}
	forced := ""
	if options != nil {
		forced, _ = options["rollout_variant"].(string)
	}
	if forced != "" {
		for _, raw := range variants {
			if name, promptID := rolloutVariant(raw); name == forced && promptID != "" {
				return p.loadRolloutPrompt(ctx, fields, name, promptID)
			}
		}
	}
	pick := rand.Float64() * 100 //nolint:gosec
	cumulative := 0.0
	for _, raw := range variants {
		name, promptID := rolloutVariant(raw)
		if promptID == "" {
			continue
		}
		cfg, _ := raw.(map[string]any)
		cumulative += toFloat(cfg["percentage"])
		if pick <= cumulative {
			return p.loadRolloutPrompt(ctx, fields, name, promptID)
		}
	}
	return "", false
}

func rolloutVariant(raw any) (string, string) {
	cfg, _ := raw.(map[string]any)
	if cfg == nil {
		return "", ""
	}
	name, _ := cfg["name"].(string)
	promptID, _ := cfg["prompt_id"].(string)
	if promptID == "" {
		promptID, _ = cfg["promptId"].(string)
	}
	if name == "" {
		name = promptID
	}
	return name, promptID
}

func (p *featurePipeline) loadRolloutPrompt(ctx context.Context, fields map[string]string, name, promptID string) (string, bool) {
	content, err := p.promptLoader.GetContent(ctx, promptID)
	if err != nil || content == "" {
		return "", false
	}
	fields["systemPrompt"] = tmpl.Interpolate(content, fields)
	return name, true
}

// run is the main request path. Phases:
//
//  1. Rate limit
//  2. Budget check
//  3. Cache lookup  (exact → semantic)
//  4. Field transformations
//  5. Interceptors  → may override target selection
//  6. Target selection + inference  (fallback chain or retry)
//  7. Budget accounting
//  8. Cache store
//  9. Metrics recording
func (p *featurePipeline) run(
	ctx context.Context,
	router *domain.Router,
	targets []domain.RouterTarget,
	features []domain.RouterFeature,
	interceptors []domain.RouterInterceptor,
	fields map[string]string,
	options map[string]any,
	inferencer ModelInferencer,
	bypassCache bool,
) (*RouteInferResult, []PipelineStep, error) {
	start := time.Now()
	enabledFeat := sortedEnabledFeatures(features)
	orgID := authDomain.OrgIDFromContext(ctx)
	var steps []PipelineStep

	// step records a completed pipeline phase with its duration and pipeline offset.
	addStep := func(s PipelineStep, d time.Duration) {
		s.DurationMs = float64(d.Microseconds()) / 1000.0
		s.OffsetMs = float64((time.Since(start) - d).Microseconds()) / 1000.0
		steps = append(steps, s)
	}

	// ── Phase 1: Rate limit ───────────────────────────────────────────────────
	for _, f := range enabledFeat {
		if f.FeatureType == domain.FeatureRateLimit {
			t0 := time.Now()
			if err := p.checkRateLimit(router.ID, f.Config); err != nil {
				addStep(PipelineStep{Phase: 1, Kind: "rate_limit", Name: "Rate Limit", Outcome: "blocked", Detail: err.Error()}, time.Since(t0))
				p.metrics.record(router.ID, "rate_limited", time.Since(start))
				return nil, steps, err
			}
			addStep(PipelineStep{Phase: 1, Kind: "rate_limit", Name: "Rate Limit", Outcome: "passed"}, time.Since(t0))
		}
	}

	// ── Pre-index features touched in phases 2–3 ─────────────────────────────
	// A single pass avoids repeated O(n) scans in the hot path below.
	var budgetFeat, responseCacheFeat, semanticCacheFeat *domain.RouterFeature
	for i := range enabledFeat {
		switch enabledFeat[i].FeatureType {
		case domain.FeatureBudget:
			budgetFeat = &enabledFeat[i]
		case domain.FeatureResponseCache:
			responseCacheFeat = &enabledFeat[i]
		case domain.FeatureSemanticCache:
			semanticCacheFeat = &enabledFeat[i]
		}
	}

	// ── Phase 3a: Exact cache lookup (BEFORE budget check) ───────────────────
	// Cache hits are free — they bypass inference entirely, so there is no reason
	// to pay a budget DB query on the hot cache-hit path. Checking the cache first
	// means hits return in <1 ms without touching the database.
	prompt := fields["prompt"]
	deferExactCache := hasTargetSystemPrompts(enabledTargets(targets))
	exactKey := p.exactCacheKey(router.ID, fields)
	hasCacheFeature := responseCacheFeat != nil || semanticCacheFeat != nil
	stepsBeforePhase3 := len(steps)

	if !bypassCache && !deferExactCache && responseCacheFeat != nil {
		t0 := time.Now()
		if hit := p.cacheGet(exactKey, responseCacheFeat.Config); hit != nil {
			addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Cache", Outcome: "hit_exact", Detail: "exact key match"}, time.Since(t0))
			p.metrics.record(router.ID, "cache_hit_exact", time.Since(start))
			hit.Steps = steps
			hit = markCachedResult(hit, fields, "exact")
			return hit, steps, nil
		}
		addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Cache", Outcome: "miss"}, time.Since(t0))
	}

	// ── Phase 2 + 3b: Budget check and semantic embedding in parallel ─────────
	// Both are I/O-bound and independent: the budget check queries the database
	// while the embedding calls the embedding model. Running them concurrently
	// hides the budget DB latency behind the (usually longer) embedding call.
	// A semantic cache hit takes priority over a budget error — cached responses
	// cost nothing, so they should always be served.
	var promptEmbedding []float32
	if budgetFeat != nil && semanticCacheFeat != nil && !bypassCache && p.embedder != nil && prompt != "" {
		type budgetRes struct{ err error }
		type embRes struct {
			emb []float32
			err error
		}
		var br budgetRes
		var er embRes
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			br.err = p.checkBudget(ctx, orgID, router.ID, budgetFeat.Config, options)
		}()
		go func() {
			defer wg.Done()
			modelID, _ := semanticCacheFeat.Config["model_id"].(string)
			er.emb, er.err = p.embedder.Embed(ctx, modelID, prompt)
		}()
		wg.Wait()

		// Semantic cache takes priority: a hit means no inference, so budget is irrelevant.
		semanticModelID, _ := semanticCacheFeat.Config["model_id"].(string)
		t0sem := time.Now()
		if er.err == nil && len(er.emb) > 0 {
			promptEmbedding = er.emb
			if hit := p.semanticCacheGet(semanticCacheScope(router.ID, semanticModelID), semanticModelID, er.emb, semanticCacheFeat.Config); hit != nil {
				addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Semantic Cache", Outcome: "hit_semantic", Detail: "above similarity threshold"}, time.Since(t0sem))
				p.metrics.record(router.ID, "cache_hit_semantic", time.Since(start))
				hit.Steps = steps
				hit = markCachedResult(hit, fields, "semantic")
				return hit, steps, nil
			}
			addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Semantic Cache", Outcome: "miss", Detail: "below threshold"}, time.Since(t0sem))
		} else if er.err != nil {
			addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Semantic Cache", Outcome: "miss", Detail: "embed failed"}, time.Since(t0sem))
		}

		// Semantic cache missed — now honour the budget result.
		t0bud := time.Now()
		if br.err != nil {
			addStep(PipelineStep{Phase: 2, Kind: "budget", Name: "Budget Check", Outcome: "blocked", Detail: br.err.Error()}, time.Since(t0bud))
			p.metrics.record(router.ID, "budget_exceeded", time.Since(start))
			return nil, steps, br.err
		}
		addStep(PipelineStep{Phase: 2, Kind: "budget", Name: "Budget Check", Outcome: "passed"}, time.Since(t0bud))

	} else {
		// ── Phase 2: Budget check (serial when no semantic cache) ────────────────
		if budgetFeat != nil {
			t0 := time.Now()
			if err := p.checkBudget(ctx, orgID, router.ID, budgetFeat.Config, options); err != nil {
				addStep(PipelineStep{Phase: 2, Kind: "budget", Name: "Budget Check", Outcome: "blocked", Detail: err.Error()}, time.Since(t0))
				p.metrics.record(router.ID, "budget_exceeded", time.Since(start))
				return nil, steps, err
			}
			addStep(PipelineStep{Phase: 2, Kind: "budget", Name: "Budget Check", Outcome: "passed"}, time.Since(t0))
		}

		// ── Phase 3b: Semantic embedding (serial when no budget feature) ─────────
		if !bypassCache && semanticCacheFeat != nil && p.embedder != nil && prompt != "" {
			modelID, _ := semanticCacheFeat.Config["model_id"].(string)
			t0 := time.Now()
			emb, err := p.embedder.Embed(ctx, modelID, prompt)
			if err == nil && len(emb) > 0 {
				promptEmbedding = emb
				if hit := p.semanticCacheGet(semanticCacheScope(router.ID, modelID), modelID, emb, semanticCacheFeat.Config); hit != nil {
					addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Semantic Cache", Outcome: "hit_semantic", Detail: "above similarity threshold"}, time.Since(t0))
					p.metrics.record(router.ID, "cache_hit_semantic", time.Since(start))
					hit.Steps = steps
					hit = markCachedResult(hit, fields, "semantic")
					return hit, steps, nil
				}
				addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Semantic Cache", Outcome: "miss", Detail: "below threshold"}, time.Since(t0))
			} else {
				addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Semantic Cache", Outcome: "miss", Detail: "embed failed"}, time.Since(t0))
			}
		}
	}
	if !bypassCache && hasCacheFeature && len(steps) == stepsBeforePhase3 {
		addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Cache", Outcome: "miss"}, 0)
	}

	// ── Phase 4: Field transformations ────────────────────────────────────────
	for _, f := range enabledFeat {
		t0 := time.Now()
		switch f.FeatureType {
		case domain.FeatureTokenOptimization:
			beforeLen := len(fields["prompt"])
			fields = applyTokenOptimization(fields, f.Config)
			afterLen := len(fields["prompt"])
			detail := ""
			if beforeLen != afterLen {
				detail = fmt.Sprintf("prompt %d to %d chars", beforeLen, afterLen)
			}
			addStep(PipelineStep{Phase: 4, Kind: "transform", Name: "Token Optimization", Outcome: "applied", Detail: detail}, time.Since(t0))
		case domain.FeatureContextTrimming:
			beforeLen := len(fields["prompt"])
			fields = applyContextTrimming(fields, f.Config)
			afterLen := len(fields["prompt"])
			detail := ""
			if beforeLen != afterLen {
				detail = fmt.Sprintf("prompt %d to %d chars", beforeLen, afterLen)
			}
			addStep(PipelineStep{Phase: 4, Kind: "transform", Name: "Context Trimming", Outcome: "applied", Detail: detail}, time.Since(t0))
		case domain.FeatureTokenCostOptimization:
			var detail tokenCostOptimizationResult
			fields, options, detail = applyTokenCostOptimization(fields, options, f.Config)
			fields = applyModelTokenCostOptimization(ctx, fields, f.Config, inferencer, &detail)
			addStep(PipelineStep{Phase: 4, Kind: "transform", Name: "Token Cost Optimization", Outcome: "applied", Detail: detail.detail()}, time.Since(t0))
		case domain.FeaturePromptOptimizer:
			var detail promptOptimizerResult
			fields, detail = applyPromptOptimizer(fields, f.Config)
			addStep(PipelineStep{Phase: 4, Kind: "transform", Name: "Prompt Optimizer", Outcome: "applied", Detail: detail.detail()}, time.Since(t0))
		case domain.FeaturePromptPolicyRollout:
			if variant, ok := p.applyPromptPolicyRollout(ctx, fields, options, f.Config); ok {
				addStep(PipelineStep{Phase: 4, Kind: "transform", Name: "Prompt/Policy Rollout", Outcome: "variant_selected", Detail: variant}, time.Since(t0))
			}
		case domain.FeaturePromptCaching:
			options = copyOptions(options)
			options["_prompt_cache"] = true
			addStep(PipelineStep{Phase: 4, Kind: "transform", Name: "Prompt Caching", Outcome: "enabled", Detail: "provider-side system prompt cache requested"}, time.Since(t0))
		case domain.FeatureStructuredOutput:
			if schema, ok := f.Config["schema"]; ok && schema != nil {
				options = copyOptions(options)
				options["_structured_schema"] = schema
				if name, _ := f.Config["name"].(string); name != "" {
					options["_structured_name"] = name
				}
				if strict, ok := f.Config["strict"].(bool); ok && strict {
					options["_structured_strict"] = true
				}
				addStep(PipelineStep{Phase: 4, Kind: "transform", Name: "Structured Output", Outcome: "schema_injected"}, time.Since(t0))
			}
		case domain.FeatureContextCompression:
			historyRaw := fields["_history"]
			beforeLen := len(historyRaw)
			maxChars := int(toFloat(f.Config["max_chars"]))
			keepRecent := 2
			if v := toFloat(f.Config["keep_recent"]); v > 0 {
				keepRecent = int(v)
			}
			if maxChars > 0 && historyRaw != "" {
				if compressed := compressHistory(historyRaw, fields["prompt"], maxChars, keepRecent); compressed != "" {
					fields["_history"] = compressed
					addStep(PipelineStep{Phase: 4, Kind: "transform", Name: "Context Compression", Outcome: "applied",
						Detail: fmt.Sprintf("history %d to %d chars · max %d, keep last %d", beforeLen, len(compressed), maxChars, keepRecent),
					}, time.Since(t0))
				}
			}
		case domain.FeatureSemanticMemory:
			t0b := time.Now()
			// Copy fields so injection doesn't bleed across calls
			injectedFields := make(map[string]string, len(fields))
			for k, v := range fields {
				injectedFields[k] = v
			}
			p.injectSemanticMemory(ctx, router.ID, f.Config, injectedFields)
			if injectedFields["systemPrompt"] != fields["systemPrompt"] {
				fields = injectedFields
				addStep(PipelineStep{Phase: 4, Kind: "transform", Name: "Semantic Memory", Outcome: "examples_injected"}, time.Since(t0b))
			}
		case domain.FeatureCostAwareRouting:
			totalChars := len(fields["prompt"]) + len(fields["systemPrompt"])
			if hist := fields["_history"]; hist != "" {
				totalChars += len(hist)
			}
			targetID := costAwareTarget(f.Config, totalChars)
			if targetID != "" {
				options = copyOptions(options)
				options["_cost_aware_target_id"] = targetID
				addStep(PipelineStep{Phase: 4, Kind: "transform", Name: "Cost-Aware Routing", Outcome: "target_selected",
					Detail: fmt.Sprintf("%d chars to %s", totalChars, targetID),
				}, time.Since(t0))
			}
		}
	}

	// MCP tools must be visible to the first model call; otherwise the model has
	// no way to emit tool_calls for the post-inference MCP execution loop.
	options = p.prepareMCPToolOptions(ctx, enabledFeat, options, addStep)

	// ── Phase 5: Interceptors ─────────────────────────────────────────────────
	// Run interceptors in execution_order. The first one to return a non-nil
	// target override wins; subsequent interceptors are still executed for
	// side-effects (e.g. logging, filtering) but cannot change the target.
	var targetOverride *domain.RouterTarget
	var abVariant string
	enabled := enabledTargets(targets)

	for _, ic := range sortedEnabledInterceptors(interceptors) {
		t0 := time.Now()
		switch ic.Type {
		case domain.InterceptorSemanticClassifier:
			if p.embedder != nil && targetOverride == nil {
				override, err := p.runSemanticClassifier(ctx, ic, enabled, fields)
				if err != nil {
					slog.Error("semantic_classifier error", "err", err)
					addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Semantic Classifier", Outcome: "error", Detail: err.Error()}, time.Since(t0))
				} else if override != nil {
					targetOverride = override
					addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Semantic Classifier", Outcome: "routed", Detail: fmt.Sprintf("target %s", override.ID)}, time.Since(t0))
					slog.Info("semantic_classifier routed", "targetID", override.ID, "modelID", override.ModelID)
				} else {
					addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Semantic Classifier", Outcome: "passed", Detail: "no match above threshold"}, time.Since(t0))
					slog.Info("semantic_classifier no route matched (similarity below threshold)")
				}
			}
		case domain.InterceptorABTest:
			if targetOverride == nil {
				if options == nil {
					options = map[string]any{}
				}
				if override, variant := p.runABTestOrUCB1(ic, enabled, fields, options); override != nil {
					targetOverride = override
					abVariant = variant
					addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "A/B Test", Outcome: "routed", Detail: fmt.Sprintf("variant: %s", variant)}, time.Since(t0))
				} else {
					addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "A/B Test", Outcome: "passed"}, time.Since(t0))
				}
			}
		case domain.InterceptorPromptShield:
			if shield, blocked, detail := p.runPromptShield(ctx, ic, fields, inferencer); shield {
				if blocked {
					addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Prompt Shield", Outcome: "blocked", Detail: detail}, time.Since(t0))
					return nil, steps, domain.ErrRequestBlocked
				}
				addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Prompt Shield", Outcome: "flagged", Detail: detail}, time.Since(t0))
			} else {
				addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Prompt Shield", Outcome: "passed"}, time.Since(t0))
			}
		case domain.InterceptorTeamBudget:
			teamID := fields["team_id"]
			if teamID == "" {
				teamID = authDomain.CallerTeamIDFromContext(ctx)
			}
			if teamID == "" {
				addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Team Budget", Outcome: "skipped", Detail: "no team_id field"}, time.Since(t0))
				break
			}
			if override, blocked, detail := p.checkTeamBudget(ic, teamID, enabled); blocked {
				addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Team Budget", Outcome: "blocked", Detail: detail}, time.Since(t0))
				return nil, steps, domain.ErrBudgetExceeded
			} else if override != nil && targetOverride == nil {
				targetOverride = override
				addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Team Budget", Outcome: "overflow_routed", Detail: detail}, time.Since(t0))
			} else {
				addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Team Budget", Outcome: "passed"}, time.Since(t0))
			}
		case domain.InterceptorContentFilter:
			if blocked := applyContentFilter(fields, ic.Config); blocked {
				addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Content Filter", Outcome: "blocked", Detail: "matched blocked pattern"}, time.Since(t0))
				return nil, steps, domain.ErrRequestBlocked
			}
			addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Content Filter", Outcome: "passed"}, time.Since(t0))
		case domain.InterceptorPIIDetector:
			fields = applyPIIDetector(fields, ic.Config)
			if _, blocked := fields["__pii_blocked__"]; blocked {
				addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "PII Detector", Outcome: "blocked", Detail: "PII detected"}, time.Since(t0))
				return nil, steps, domain.ErrRequestBlocked
			}
			addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "PII Detector", Outcome: "masked", Detail: "PII redacted"}, time.Since(t0))
		case domain.InterceptorPromptGuard:
			if blocked := applyPromptGuard(fields, ic.Config); blocked {
				addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Prompt Guard", Outcome: "blocked", Detail: "jailbreak pattern detected"}, time.Since(t0))
				return nil, steps, domain.ErrRequestBlocked
			}
			addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Prompt Guard", Outcome: "passed"}, time.Since(t0))
		}
	}

	// ── Phase 6: Target selection + inference ─────────────────────────────────
	// Pre-compute coalescing config so it's available inside the inference block.
	var coalescingCfg map[string]any
	for _, f := range enabledFeat {
		if f.FeatureType == domain.FeatureRequestCoalescing && f.IsEnabled {
			coalescingCfg = f.Config
			break
		}
	}

	hasFallback := hasFeature(enabledFeat, domain.FeatureFallback)

	// Cost-aware routing: apply if no interceptor already set a target override
	if targetOverride == nil {
		if costTargetID, _ := options["_cost_aware_target_id"].(string); costTargetID != "" {
			for i := range enabled {
				if enabled[i].ModelID == costTargetID {
					targetOverride = &enabled[i]
					break
				}
			}
		}
	}

	// When the health-check feature is enabled, filter out targets whose model
	// is known-unhealthy before strategy selection and the fallback chain.
	if hasFeature(enabledFeat, domain.FeatureHealthCheck) && p.healthChecker != nil {
		t0 := time.Now()
		before := len(enabled)
		enabled = p.filterHealthyTargets(enabled)
		skipped := before - len(enabled)
		detail := fmt.Sprintf("%d healthy of %d", len(enabled), before)
		if skipped > 0 {
			detail += fmt.Sprintf(" (%d skipped unhealthy)", skipped)
		}
		addStep(PipelineStep{Phase: 6, Kind: "health_check", Name: "Health Check", Outcome: "filtered", Detail: detail}, time.Since(t0))
		if len(enabled) == 0 {
			return nil, steps, domain.ErrAllTargetsFailed
		}
	}

	// ── Hedging: race multiple models in parallel ─────────────────────────────
	var hedgingCfg map[string]any
	for _, f := range enabledFeat {
		if f.FeatureType == domain.FeatureHedging && f.IsEnabled {
			hedgingCfg = f.Config
			break
		}
	}

	var result *RouteInferResult
	var err error
	var wasCoalesced bool

	if hedgingCfg != nil && targetOverride == nil {
		t0 := time.Now()
		result, err = p.runHedged(ctx, router.ID, enabled, fields, options, inferencer, hedgingCfg)
		if err != nil {
			addStep(PipelineStep{Phase: 6, Kind: "hedging", Name: "Request Hedging", Outcome: "error", Detail: err.Error()}, time.Since(t0))
			p.metrics.record(router.ID, "error", time.Since(start))
			return nil, steps, err
		}
		addStep(PipelineStep{Phase: 6, Kind: "hedging", Name: "Request Hedging", Outcome: "winner_found",
			Detail: fmt.Sprintf("winner: %s", result.SelectedModelID)}, time.Since(t0))
	} else if hasFallback {
		t0 := time.Now()
		addStep(PipelineStep{Phase: 6, Kind: "target_select", Name: "Target Selection", Outcome: "fallback", Detail: fmt.Sprintf("strategy: %s", router.Strategy)}, time.Since(t0))
		result, err = p.inferWithFallback(ctx, enabled, fields, options, inferencer, addStep)
	} else {
		var target *domain.RouterTarget
		t0 := time.Now()
		if targetOverride != nil {
			target = targetOverride
		} else {
			// Pass the already-filtered `enabled` slice so health-check exclusions are respected.
			target, err = selectTarget(router, enabled, &p.latencies, promptEmbedding)
			if err != nil {
				addStep(PipelineStep{Phase: 6, Kind: "target_select", Name: "Target Selection", Outcome: "error", Detail: err.Error()}, time.Since(t0))
				return nil, steps, err
			}
		}
		addStep(PipelineStep{Phase: 6, Kind: "target_select", Name: "Target Selection", Outcome: "selected", Detail: fmt.Sprintf("strategy: %s", router.Strategy)}, time.Since(t0))

		// Inject target-level system prompt (higher priority than router-level).
		if p.injectTargetSystemPrompt(ctx, target, fields) && deferExactCache {
			// Recompute cache key with the target-specific system prompt.
			exactKey = p.exactCacheKey(router.ID+":"+target.ID, fields)
			if !bypassCache {
				for _, f := range enabledFeat {
					if f.FeatureType == domain.FeatureResponseCache {
						if hit := p.cacheGet(exactKey, f.Config); hit != nil {
							addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Cache", Outcome: "hit_exact", Detail: "target-specific key match"}, 0)
							p.metrics.record(router.ID, "cache_hit_exact", time.Since(start))
							hit.Steps = steps
							hit = markCachedResult(hit, fields, "exact")
							return hit, steps, nil
						}
					}
				}
			}
		} else if !bypassCache && deferExactCache {
			// No target override; use the original router-level key (deferred from phase 3).
			for _, f := range enabledFeat {
				if f.FeatureType == domain.FeatureResponseCache {
					if hit := p.cacheGet(exactKey, f.Config); hit != nil {
						addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Cache", Outcome: "hit_exact", Detail: "exact key match"}, 0)
						p.metrics.record(router.ID, "cache_hit_exact", time.Since(start))
						hit.Steps = steps
						hit = markCachedResult(hit, fields, "exact")
						return hit, steps, nil
					}
				}
			}
		}

		var retryCfg *retryConfig
		for _, f := range enabledFeat {
			if f.FeatureType == domain.FeatureRetry {
				retryCfg = parseRetryConfig(f.Config)
				break
			}
		}

		cb := p.circuits.get(target.ID)
		if !cb.Allow() {
			addStep(PipelineStep{Phase: 6, Kind: "inference", Name: "Model Inference", Outcome: "error", Detail: "circuit breaker open"}, 0)
			return nil, steps, domain.ErrAllTargetsFailed
		}

		callStart := time.Now()
		baseInferFn := func() (*ModelInferResult, error) {
			return inferencer.InferModel(ctx, target.ModelID, fields, options)
		}

		var inferResult *ModelInferResult
		var retryCount int

		// executeInference wraps baseInferFn with optional retry. The coalescer
		// (if enabled) calls this exactly once and fans the result to all waiters.
		executeInference := func() (*ModelInferResult, error) {
			if retryCfg != nil {
				return withRetryTracked(ctx, retryCfg, baseInferFn, func(attempt int, retryErr error) {
					retryCount++
					addStep(PipelineStep{Phase: 6, Kind: "inference", Name: "Model Inference", Outcome: "retry",
						Detail:   fmt.Sprintf("attempt %d: %s", attempt, retryErr.Error()),
						Attempts: attempt,
					}, time.Since(callStart))
					callStart = time.Now()
				})
			}
			return baseInferFn()
		}

		if coalescingCfg != nil {
			windowMs := int(toFloat(coalescingCfg["window_ms"]))
			maxWaiters := int(toFloat(coalescingCfg["max_waiters"]))
			cKey := coalesceKey(router.ID, target.ModelID, fields)
			inferResult, wasCoalesced, err = p.coalescer.Do(ctx, cKey, windowMs, maxWaiters, executeInference)
		} else {
			inferResult, err = executeInference()
		}

		if err != nil {
			if !wasCoalesced {
				cb.RecordFailure()
			}
			addStep(PipelineStep{Phase: 6, Kind: "inference", Name: "Model Inference", Outcome: "error", Detail: err.Error(), Attempts: retryCount + 1}, time.Since(callStart))
			return nil, steps, err
		}

		inferDur := time.Since(callStart)
		if !wasCoalesced {
			cb.RecordSuccess()
			p.updateLatency(target.ID, float64(inferDur.Milliseconds()))
		}

		inputTokens := inferResult.InputTokens
		outputTokens := inferResult.OutputTokens
		cachedInputTokens := inferResult.CachedInputTokens
		costUSD := inferResult.CostUSD
		if wasCoalesced {
			inputTokens = 0
			outputTokens = 0
			costUSD = 0
			if cachedInputTokens <= 0 {
				cachedInputTokens = estimateCachedInputTokens(fields)
			}
		}

		var inferDetail string
		if wasCoalesced {
			inferDetail = fmt.Sprintf("%s · coalesced (0 upstream cost)", inferResult.ModelDefKey)
		} else {
			inferDetail = fmt.Sprintf("%s · %d↑ %d↓ tok", inferResult.ModelDefKey, inputTokens, outputTokens)
			if cachedInputTokens > 0 {
				inferDetail += fmt.Sprintf(" · %d cached", cachedInputTokens)
			}
			if costUSD > 0 {
				inferDetail += fmt.Sprintf(" · $%.5f", costUSD)
			}
		}
		inferOutcome := "success"
		if wasCoalesced {
			inferOutcome = "coalesced"
		}
		addStep(PipelineStep{Phase: 6, Kind: "inference", Name: "Model Inference", Outcome: inferOutcome, Detail: inferDetail, Attempts: retryCount + 1}, inferDur)
		result = &RouteInferResult{
			Content:           inferResult.Content,
			SelectedModelID:   target.ModelID,
			SelectedTargetID:  target.ID,
			ModelDefKey:       inferResult.ModelDefKey,
			Provider:          inferResult.Provider,
			InputTokens:       inputTokens,
			OutputTokens:      outputTokens,
			CachedInputTokens: cachedInputTokens,
			CostUSD:           costUSD,
			ABVariant:         abVariant,
			ToolCalls:         inferResult.ToolCalls,
		}
	}
	if err != nil {
		p.metrics.record(router.ID, "error", time.Since(start))
		return nil, steps, err
	}

	// ── Phase 6b: MCP tool execution ──────────────────────────────────────────
	for _, f := range enabledFeat {
		if f.FeatureType == domain.FeatureMCPTools && f.IsEnabled && len(result.ToolCalls) > 0 {
			t0 := time.Now()
			updated, trace, mcpErr := p.executeMCPTools(ctx, inferencer, result.SelectedModelID, fields, options, result, f.Config, &steps, start)
			if updated != nil {
				result = updated
			}
			outcome := "success"
			if trace.ToolCalls == 0 {
				outcome = "skipped"
			}
			if mcpErr != nil {
				outcome = "error"
				slog.Error("mcp tool execution failed", "routerID", router.ID, "modelID", result.SelectedModelID, "err", mcpErr)
			} else if trace.MaxTurnsReached {
				outcome = "max_turns"
			}
			addStep(PipelineStep{Phase: 6, Kind: "mcp_tools", Name: "MCP Tool Execution", Outcome: outcome, Detail: trace.detail()}, time.Since(t0))
			break
		}
	}

	// ── Phase 6c: Structured output validation ────────────────────────────────
	if hasFeature(enabledFeat, domain.FeatureStructuredOutput) && result != nil && result.Content != "" {
		t0 := time.Now()
		if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(result.Content)), new(any)); jsonErr != nil {
			addStep(PipelineStep{Phase: 6, Kind: "structured_output", Name: "Structured Output Validation", Outcome: "invalid_json", Detail: jsonErr.Error()}, time.Since(t0))
		} else {
			addStep(PipelineStep{Phase: 6, Kind: "structured_output", Name: "Structured Output Validation", Outcome: "valid"}, time.Since(t0))
		}
	}

	// ── Phase 6d: Quality gate ────────────────────────────────────────────────
	var qgActivated bool
	for _, f := range enabledFeat {
		if f.FeatureType == domain.FeatureQualityGate && f.IsEnabled {
			t0 := time.Now()
			gated, activated, gateErr := p.runQualityGate(ctx, result, fields, options, f.Config, inferencer)
			if gateErr != nil {
				addStep(PipelineStep{Phase: 6, Kind: "quality_gate", Name: "Quality Gate", Outcome: "low_quality"}, time.Since(t0))
				p.metrics.record(router.ID, "error", time.Since(start))
				return nil, steps, gateErr
			}
			if activated {
				qgActivated = true
				addStep(PipelineStep{Phase: 6, Kind: "quality_gate", Name: "Quality Gate", Outcome: "retried_or_flagged"}, time.Since(t0))
			} else {
				addStep(PipelineStep{Phase: 6, Kind: "quality_gate", Name: "Quality Gate", Outcome: "passed"}, time.Since(t0))
			}
			result = gated
			break
		}
	}

	// ── Phase 6e: Response fingerprinting ────────────────────────────────────
	for _, f := range enabledFeat {
		if f.FeatureType == domain.FeatureResponseFingerprinting && f.IsEnabled && result != nil {
			t0 := time.Now()
			windowSize := int(toFloat(f.Config["window_size"]))
			alertThreshold := 3.0
			if v := toFloat(f.Config["alert_threshold"]); v > 0 {
				alertThreshold = v
			}
			fp := p.getOrCreateFingerprint(router.ID, windowSize)
			anomaly, detail := fp.record(len(result.Content), alertThreshold)
			if anomaly {
				addStep(PipelineStep{Phase: 6, Kind: "fingerprint", Name: "Response Fingerprinting", Outcome: "anomaly", Detail: detail}, time.Since(t0))
			} else {
				addStep(PipelineStep{Phase: 6, Kind: "fingerprint", Name: "Response Fingerprinting", Outcome: "normal"}, time.Since(t0))
			}
			break
		}
	}

	// ── Phase 7: Budget accounting ────────────────────────────────────────────
	for _, f := range enabledFeat {
		if f.FeatureType == domain.FeatureBudget {
			t0 := time.Now()
			p.recordBudget(router.ID, result.CostUSD, f.Config, options)
			addStep(PipelineStep{Phase: 7, Kind: "budget_accounting", Name: "Budget Accounting", Outcome: "recorded"}, time.Since(t0))
			break
		}
	}

	// ── Phase 8: Cache store ──────────────────────────────────────────────────
	if !bypassCache {
		for _, f := range enabledFeat {
			switch f.FeatureType {
			case domain.FeatureResponseCache:
				t0 := time.Now()
				p.cacheSet(exactKey, result, f.Config)
				addStep(PipelineStep{Phase: 8, Kind: "cache_store", Name: "Cache Store", Outcome: "stored", Detail: "response cached"}, time.Since(t0))
			case domain.FeatureSemanticCache:
				if len(promptEmbedding) > 0 && !wasCoalesced {
					modelID, _ := f.Config["model_id"].(string)
					t0 := time.Now()
					p.semanticCacheSet(semanticCacheScope(router.ID, modelID), modelID, promptEmbedding, result, f.Config)
					addStep(PipelineStep{Phase: 8, Kind: "cache_store", Name: "Semantic Cache Store", Outcome: "stored", Detail: "embedding cached"}, time.Since(t0))
				}
			case domain.FeatureSemanticMemory:
				if !wasCoalesced {
					go func(cfg map[string]any, promptText, responseText string) {
						memCtx, cancel := context.WithTimeout(ctx, semanticMemoryStoreTimeout)
						defer cancel()
						p.storeSemanticMemory(memCtx, router.ID, cfg, promptText, responseText)
					}(f.Config, fields["prompt"], result.Content)
				}
			}
		}
	}

	// ── Phase 9: Metrics ──────────────────────────────────────────────────────
	p.metrics.record(router.ID, "success", time.Since(start))

	// ── Phase 9b: Response prefetch (async) ──────────────────────────────────
	for _, f := range enabledFeat {
		if f.FeatureType == domain.FeatureResponsePrefetch && f.IsEnabled && result != nil {
			dispatched := p.dispatchResponsePrefetch(ctx, router.ID, result.SelectedModelID, result.Content, fields["systemPrompt"], options, f.Config, inferencer)
			if dispatched > 0 {
				addStep(PipelineStep{Phase: 9, Kind: "prefetch", Name: "Response Prefetch", Outcome: "dispatched",
					Detail: fmt.Sprintf("%d follow-up(s) queued", dispatched)}, 0)
			}
			break
		}
	}

	// Update UCB1 arm if an adaptive A/B test was used.
	// Reward is reduced to 0.5 when the quality gate activated, so variants
	// producing low-quality responses converge towards lower selection probability.
	if ucbInterceptorID, _ := options["_ucb1_interceptor_id"].(string); ucbInterceptorID != "" {
		if ucbVariant, _ := options["_ucb1_variant"].(string); ucbVariant != "" {
			reward := 1.0
			if err != nil {
				reward = 0.0
			} else if qgActivated {
				reward = 0.5
			}
			p.updateUCB1(ucbInterceptorID, ucbVariant, reward)
		}
	}

	result.Steps = steps
	return result, steps, nil
}

// runStream is the streaming variant of run. It executes phases 1–5 (rate
// limit, budget, exact cache, field transforms, interceptors), picks a target,
// then delegates to inferencer.InferModelStream. Post-stream accounting
// (budget, cache, metrics) runs in a background goroutine after the channel drains.
func (p *featurePipeline) runStream(
	ctx context.Context,
	router *domain.Router,
	targets []domain.RouterTarget,
	features []domain.RouterFeature,
	interceptors []domain.RouterInterceptor,
	fields map[string]string,
	options map[string]any,
	inferencer ModelInferencer,
	bypassCache bool,
) (<-chan StreamChunk, *[]PipelineStep, error) {
	start := time.Now()
	enabledFeat := sortedEnabledFeatures(features)
	orgID := authDomain.OrgIDFromContext(ctx)
	steps := make([]PipelineStep, 0, 16)
	addStep := func(s PipelineStep, d time.Duration) {
		s.DurationMs = float64(d.Microseconds()) / 1000.0
		s.OffsetMs = float64((time.Since(start) - d).Microseconds()) / 1000.0
		steps = append(steps, s)
	}

	if featureType := unsupportedStreamingFeature(enabledFeat); featureType != "" {
		addStep(PipelineStep{Phase: 0, Kind: "streaming", Name: "Streaming Compatibility", Outcome: "blocked", Detail: fmt.Sprintf("%s does not support streaming", featureType)}, 0)
		p.metrics.record(router.ID, "error", time.Since(start))
		return nil, &steps, fmt.Errorf("%w: %s", domain.ErrStreamingUnsupported, featureType)
	}

	// Phase 1: Rate limit
	for _, f := range enabledFeat {
		if f.FeatureType == domain.FeatureRateLimit {
			t0 := time.Now()
			if err := p.checkRateLimit(router.ID, f.Config); err != nil {
				addStep(PipelineStep{Phase: 1, Kind: "rate_limit", Name: "Rate Limit", Outcome: "blocked", Detail: err.Error()}, time.Since(t0))
				p.metrics.record(router.ID, "rate_limited", time.Since(start))
				return nil, &steps, err
			}
			addStep(PipelineStep{Phase: 1, Kind: "rate_limit", Name: "Rate Limit", Outcome: "passed"}, time.Since(t0))
		}
	}

	// ── Pre-index features touched in phases 2–3 (stream) ───────────────────
	var budgetFeatS, responseCacheFeatS, semanticCacheFeatS *domain.RouterFeature
	for i := range enabledFeat {
		switch enabledFeat[i].FeatureType {
		case domain.FeatureBudget:
			budgetFeatS = &enabledFeat[i]
		case domain.FeatureResponseCache:
			responseCacheFeatS = &enabledFeat[i]
		case domain.FeatureSemanticCache:
			semanticCacheFeatS = &enabledFeat[i]
		}
	}

	// Phase 3a: Exact cache lookup (before budget — cache hits cost nothing)
	prompt := fields["prompt"]
	exactKey := p.exactCacheKey(router.ID, fields)
	deferExactCache := hasTargetSystemPrompts(enabledTargets(targets))
	hasCacheFeature := responseCacheFeatS != nil || semanticCacheFeatS != nil
	stepsBeforePhase3 := len(steps)

	if !bypassCache && !deferExactCache && responseCacheFeatS != nil {
		t0 := time.Now()
		if hit := p.cacheGet(exactKey, responseCacheFeatS.Config); hit != nil {
			addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Cache", Outcome: "hit_exact", Detail: "exact key match"}, time.Since(t0))
			p.metrics.record(router.ID, "cache_hit_exact", time.Since(start))
			ch := make(chan StreamChunk, 2)
			ch <- StreamChunk{Delta: hit.Content}
			ch <- StreamChunk{Done: true, SelectedModelID: hit.SelectedModelID, SelectedTargetID: hit.SelectedTargetID, CachedInputTokens: estimateCachedInputTokens(fields), ModelDefKey: hit.ModelDefKey, Provider: hit.Provider, CacheHit: true, CacheHitType: "exact"}
			close(ch)
			return ch, &steps, nil
		}
		addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Cache", Outcome: "miss"}, time.Since(t0))
	}

	// Phase 2 + 3b: Budget check and semantic embedding in parallel (stream)
	var promptEmbedding []float32
	if budgetFeatS != nil && semanticCacheFeatS != nil && !bypassCache && p.embedder != nil && prompt != "" {
		type budgetRes struct{ err error }
		type embRes struct {
			emb []float32
			err error
		}
		var br budgetRes
		var er embRes
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			br.err = p.checkBudget(ctx, orgID, router.ID, budgetFeatS.Config, options)
		}()
		go func() {
			defer wg.Done()
			modelID, _ := semanticCacheFeatS.Config["model_id"].(string)
			er.emb, er.err = p.embedder.Embed(ctx, modelID, prompt)
		}()
		wg.Wait()

		semanticModelID, _ := semanticCacheFeatS.Config["model_id"].(string)
		t0sem := time.Now()
		if er.err == nil && len(er.emb) > 0 {
			promptEmbedding = er.emb
			if hit := p.semanticCacheGet(semanticCacheScope(router.ID, semanticModelID), semanticModelID, er.emb, semanticCacheFeatS.Config); hit != nil {
				addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Semantic Cache", Outcome: "hit_semantic", Detail: "above similarity threshold"}, time.Since(t0sem))
				p.metrics.record(router.ID, "cache_hit_semantic", time.Since(start))
				ch := make(chan StreamChunk, 2)
				ch <- StreamChunk{Delta: hit.Content}
				ch <- StreamChunk{Done: true, SelectedModelID: hit.SelectedModelID, SelectedTargetID: hit.SelectedTargetID, CachedInputTokens: estimateCachedInputTokens(fields), ModelDefKey: hit.ModelDefKey, Provider: hit.Provider, CacheHit: true, CacheHitType: "semantic"}
				close(ch)
				return ch, &steps, nil
			}
			addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Semantic Cache", Outcome: "miss", Detail: "below threshold"}, time.Since(t0sem))
		} else if er.err != nil {
			addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Semantic Cache", Outcome: "miss", Detail: "embed failed"}, time.Since(t0sem))
		}

		t0bud := time.Now()
		if br.err != nil {
			addStep(PipelineStep{Phase: 2, Kind: "budget", Name: "Budget Check", Outcome: "blocked", Detail: br.err.Error()}, time.Since(t0bud))
			p.metrics.record(router.ID, "budget_exceeded", time.Since(start))
			return nil, &steps, br.err
		}
		addStep(PipelineStep{Phase: 2, Kind: "budget", Name: "Budget Check", Outcome: "passed"}, time.Since(t0bud))

	} else {
		if budgetFeatS != nil {
			t0 := time.Now()
			if err := p.checkBudget(ctx, orgID, router.ID, budgetFeatS.Config, options); err != nil {
				addStep(PipelineStep{Phase: 2, Kind: "budget", Name: "Budget Check", Outcome: "blocked", Detail: err.Error()}, time.Since(t0))
				p.metrics.record(router.ID, "budget_exceeded", time.Since(start))
				return nil, &steps, err
			}
			addStep(PipelineStep{Phase: 2, Kind: "budget", Name: "Budget Check", Outcome: "passed"}, time.Since(t0))
		}

		if !bypassCache && semanticCacheFeatS != nil && p.embedder != nil && prompt != "" {
			modelID, _ := semanticCacheFeatS.Config["model_id"].(string)
			t0 := time.Now()
			emb, serr := p.embedder.Embed(ctx, modelID, prompt)
			if serr == nil && len(emb) > 0 {
				promptEmbedding = emb
				if hit := p.semanticCacheGet(semanticCacheScope(router.ID, modelID), modelID, emb, semanticCacheFeatS.Config); hit != nil {
					addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Semantic Cache", Outcome: "hit_semantic", Detail: "above similarity threshold"}, time.Since(t0))
					p.metrics.record(router.ID, "cache_hit_semantic", time.Since(start))
					ch := make(chan StreamChunk, 2)
					ch <- StreamChunk{Delta: hit.Content}
					ch <- StreamChunk{Done: true, SelectedModelID: hit.SelectedModelID, SelectedTargetID: hit.SelectedTargetID, CachedInputTokens: estimateCachedInputTokens(fields), ModelDefKey: hit.ModelDefKey, Provider: hit.Provider, CacheHit: true, CacheHitType: "semantic"}
					close(ch)
					return ch, &steps, nil
				}
				addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Semantic Cache", Outcome: "miss", Detail: "below threshold"}, time.Since(t0))
			} else {
				addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Semantic Cache", Outcome: "miss", Detail: "embed failed"}, time.Since(t0))
			}
		}
	}
	if !bypassCache && hasCacheFeature && len(steps) == stepsBeforePhase3 {
		addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Cache", Outcome: "miss"}, 0)
	}

	// Phase 4: Field transformations
	for _, f := range enabledFeat {
		t0 := time.Now()
		switch f.FeatureType {
		case domain.FeatureTokenOptimization:
			beforeLen := len(fields["prompt"])
			fields = applyTokenOptimization(fields, f.Config)
			afterLen := len(fields["prompt"])
			detail := ""
			if beforeLen != afterLen {
				detail = fmt.Sprintf("prompt %d to %d chars", beforeLen, afterLen)
			}
			addStep(PipelineStep{Phase: 4, Kind: "transform", Name: "Token Optimization", Outcome: "applied", Detail: detail}, time.Since(t0))
		case domain.FeatureContextTrimming:
			beforeLen := len(fields["prompt"])
			fields = applyContextTrimming(fields, f.Config)
			afterLen := len(fields["prompt"])
			detail := ""
			if beforeLen != afterLen {
				detail = fmt.Sprintf("prompt %d to %d chars", beforeLen, afterLen)
			}
			addStep(PipelineStep{Phase: 4, Kind: "transform", Name: "Context Trimming", Outcome: "applied", Detail: detail}, time.Since(t0))
		case domain.FeatureTokenCostOptimization:
			var detail tokenCostOptimizationResult
			fields, options, detail = applyTokenCostOptimization(fields, options, f.Config)
			fields = applyModelTokenCostOptimization(ctx, fields, f.Config, inferencer, &detail)
			addStep(PipelineStep{Phase: 4, Kind: "transform", Name: "Token Cost Optimization", Outcome: "applied", Detail: detail.detail()}, time.Since(t0))
		case domain.FeaturePromptOptimizer:
			var detail promptOptimizerResult
			fields, detail = applyPromptOptimizer(fields, f.Config)
			addStep(PipelineStep{Phase: 4, Kind: "transform", Name: "Prompt Optimizer", Outcome: "applied", Detail: detail.detail()}, time.Since(t0))
		case domain.FeaturePromptPolicyRollout:
			if variant, ok := p.applyPromptPolicyRollout(ctx, fields, options, f.Config); ok {
				addStep(PipelineStep{Phase: 4, Kind: "transform", Name: "Prompt/Policy Rollout", Outcome: "variant_selected", Detail: variant}, time.Since(t0))
			}
		case domain.FeaturePromptCaching:
			options = copyOptions(options)
			options["_prompt_cache"] = true
			addStep(PipelineStep{Phase: 4, Kind: "transform", Name: "Prompt Caching", Outcome: "enabled", Detail: "provider-side system prompt cache requested"}, time.Since(t0))
		case domain.FeatureStructuredOutput:
			if schema, ok := f.Config["schema"]; ok && schema != nil {
				options = copyOptions(options)
				options["_structured_schema"] = schema
				if name, _ := f.Config["name"].(string); name != "" {
					options["_structured_name"] = name
				}
				if strict, ok := f.Config["strict"].(bool); ok && strict {
					options["_structured_strict"] = true
				}
				addStep(PipelineStep{Phase: 4, Kind: "transform", Name: "Structured Output", Outcome: "schema_injected"}, time.Since(t0))
			}
		case domain.FeatureContextCompression:
			historyRaw := fields["_history"]
			beforeLen := len(historyRaw)
			maxChars := int(toFloat(f.Config["max_chars"]))
			keepRecent := 2
			if v := toFloat(f.Config["keep_recent"]); v > 0 {
				keepRecent = int(v)
			}
			if maxChars > 0 && historyRaw != "" {
				if compressed := compressHistory(historyRaw, fields["prompt"], maxChars, keepRecent); compressed != "" {
					fields["_history"] = compressed
					addStep(PipelineStep{Phase: 4, Kind: "transform", Name: "Context Compression", Outcome: "applied",
						Detail: fmt.Sprintf("history %d to %d chars · max %d, keep last %d", beforeLen, len(compressed), maxChars, keepRecent),
					}, time.Since(t0))
				}
			}
		case domain.FeatureSemanticMemory:
			t0b := time.Now()
			injectedFields := make(map[string]string, len(fields))
			for k, v := range fields {
				injectedFields[k] = v
			}
			p.injectSemanticMemory(ctx, router.ID, f.Config, injectedFields)
			if injectedFields["systemPrompt"] != fields["systemPrompt"] {
				fields = injectedFields
				addStep(PipelineStep{Phase: 4, Kind: "transform", Name: "Semantic Memory", Outcome: "examples_injected"}, time.Since(t0b))
			}
		case domain.FeatureCostAwareRouting:
			totalChars := len(fields["prompt"]) + len(fields["systemPrompt"])
			if hist := fields["_history"]; hist != "" {
				totalChars += len(hist)
			}
			targetID := costAwareTarget(f.Config, totalChars)
			if targetID != "" {
				options = copyOptions(options)
				options["_cost_aware_target_id"] = targetID
				addStep(PipelineStep{Phase: 4, Kind: "transform", Name: "Cost-Aware Routing", Outcome: "target_selected",
					Detail: fmt.Sprintf("%d chars to %s", totalChars, targetID),
				}, time.Since(t0))
			}
		}
	}

	options = p.prepareMCPToolOptions(ctx, enabledFeat, options, addStep)

	// Phase 5: Interceptors
	var targetOverride *domain.RouterTarget
	var abVariant string
	enabled := enabledTargets(targets)

	for _, ic := range sortedEnabledInterceptors(interceptors) {
		t0 := time.Now()
		switch ic.Type {
		case domain.InterceptorSemanticClassifier:
			if p.embedder != nil && targetOverride == nil {
				override, err := p.runSemanticClassifier(ctx, ic, enabled, fields)
				if err != nil {
					slog.Error("semantic_classifier error", "err", err)
					addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Semantic Classifier", Outcome: "error", Detail: err.Error()}, time.Since(t0))
				} else if override != nil {
					targetOverride = override
					addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Semantic Classifier", Outcome: "routed", Detail: fmt.Sprintf("target %s", override.ID)}, time.Since(t0))
					slog.Info("semantic_classifier routed", "targetID", override.ID, "modelID", override.ModelID)
				} else {
					addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Semantic Classifier", Outcome: "passed", Detail: "no match above threshold"}, time.Since(t0))
					slog.Info("semantic_classifier no route matched (similarity below threshold)")
				}
			}
		case domain.InterceptorABTest:
			if targetOverride == nil {
				if options == nil {
					options = map[string]any{}
				}
				if override, variant := p.runABTestOrUCB1(ic, enabled, fields, options); override != nil {
					targetOverride = override
					abVariant = variant
					addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "A/B Test", Outcome: "routed", Detail: fmt.Sprintf("variant: %s", variant)}, time.Since(t0))
				} else {
					addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "A/B Test", Outcome: "passed"}, time.Since(t0))
				}
			}
		case domain.InterceptorPromptShield:
			if shield, blocked, detail := p.runPromptShield(ctx, ic, fields, inferencer); shield {
				if blocked {
					addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Prompt Shield", Outcome: "blocked", Detail: detail}, time.Since(t0))
					return nil, &steps, domain.ErrRequestBlocked
				}
				addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Prompt Shield", Outcome: "flagged", Detail: detail}, time.Since(t0))
			} else {
				addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Prompt Shield", Outcome: "passed"}, time.Since(t0))
			}
		case domain.InterceptorTeamBudget:
			teamID := fields["team_id"]
			if teamID == "" {
				teamID = authDomain.CallerTeamIDFromContext(ctx)
			}
			if teamID == "" {
				addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Team Budget", Outcome: "skipped", Detail: "no team_id field"}, time.Since(t0))
				break
			}
			if override, blocked, detail := p.checkTeamBudget(ic, teamID, enabled); blocked {
				addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Team Budget", Outcome: "blocked", Detail: detail}, time.Since(t0))
				return nil, &steps, domain.ErrBudgetExceeded
			} else if override != nil && targetOverride == nil {
				targetOverride = override
				addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Team Budget", Outcome: "overflow_routed", Detail: detail}, time.Since(t0))
			} else {
				addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Team Budget", Outcome: "passed"}, time.Since(t0))
			}
		case domain.InterceptorContentFilter:
			if applyContentFilter(fields, ic.Config) {
				addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Content Filter", Outcome: "blocked", Detail: "matched blocked pattern"}, time.Since(t0))
				return nil, &steps, domain.ErrRequestBlocked
			}
			addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Content Filter", Outcome: "passed"}, time.Since(t0))
		case domain.InterceptorPIIDetector:
			fields = applyPIIDetector(fields, ic.Config)
			if _, blocked := fields["__pii_blocked__"]; blocked {
				addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "PII Detector", Outcome: "blocked", Detail: "PII detected"}, time.Since(t0))
				return nil, &steps, domain.ErrRequestBlocked
			}
			addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "PII Detector", Outcome: "masked", Detail: "PII redacted"}, time.Since(t0))
		case domain.InterceptorPromptGuard:
			if applyPromptGuard(fields, ic.Config) {
				addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Prompt Guard", Outcome: "blocked", Detail: "jailbreak pattern detected"}, time.Since(t0))
				return nil, &steps, domain.ErrRequestBlocked
			}
			addStep(PipelineStep{Phase: 5, Kind: "interceptor", Name: "Prompt Guard", Outcome: "passed"}, time.Since(t0))
		}
	}

	// Phase 6: Health check filter + target selection
	if hasFeature(enabledFeat, domain.FeatureHealthCheck) && p.healthChecker != nil {
		t0 := time.Now()
		before := len(enabled)
		enabled = p.filterHealthyTargets(enabled)
		skipped := before - len(enabled)
		detail := fmt.Sprintf("%d healthy of %d", len(enabled), before)
		if skipped > 0 {
			detail += fmt.Sprintf(" (%d skipped unhealthy)", skipped)
		}
		addStep(PipelineStep{Phase: 6, Kind: "health_check", Name: "Health Check", Outcome: "filtered", Detail: detail}, time.Since(t0))
		if len(enabled) == 0 {
			return nil, &steps, domain.ErrAllTargetsFailed
		}
	}

	// Cost-aware routing: apply if no interceptor already set a target override.
	if targetOverride == nil {
		if costTargetID, _ := options["_cost_aware_target_id"].(string); costTargetID != "" {
			for i := range enabled {
				if enabled[i].ModelID == costTargetID {
					targetOverride = &enabled[i]
					break
				}
			}
		}
	}

	var target *domain.RouterTarget
	var selErr error
	t0sel := time.Now()
	if targetOverride != nil {
		target = targetOverride
	} else {
		// Pass the already-filtered `enabled` slice so health-check exclusions are respected.
		// Pass promptEmbedding for latency-based routing to use embedding similarity.
		target, selErr = selectTarget(router, enabled, &p.latencies, promptEmbedding)
		if selErr != nil {
			addStep(PipelineStep{Phase: 6, Kind: "target_select", Name: "Target Selection", Outcome: "error", Detail: selErr.Error()}, time.Since(t0sel))
			return nil, &steps, selErr
		}
	}
	addStep(PipelineStep{Phase: 6, Kind: "target_select", Name: "Target Selection", Outcome: "selected", Detail: fmt.Sprintf("strategy: %s", router.Strategy)}, time.Since(t0sel))

	// Inject target-level system prompt before streaming (higher priority than router-level).
	if p.injectTargetSystemPrompt(ctx, target, fields) && deferExactCache {
		exactKey = p.exactCacheKey(router.ID+":"+target.ID, fields)
		if !bypassCache {
			for _, f := range enabledFeat {
				if f.FeatureType == domain.FeatureResponseCache {
					if hit := p.cacheGet(exactKey, f.Config); hit != nil {
						addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Cache", Outcome: "hit_exact", Detail: "target-specific key match"}, 0)
						p.metrics.record(router.ID, "cache_hit_exact", time.Since(start))
						ch := make(chan StreamChunk, 2)
						ch <- StreamChunk{Delta: hit.Content}
						ch <- StreamChunk{Done: true, SelectedModelID: hit.SelectedModelID, SelectedTargetID: hit.SelectedTargetID, CachedInputTokens: estimateCachedInputTokens(fields), ModelDefKey: hit.ModelDefKey, Provider: hit.Provider, CacheHit: true, CacheHitType: "exact"}
						close(ch)
						return ch, &steps, nil
					}
				}
			}
		}
	} else if !bypassCache && deferExactCache {
		for _, f := range enabledFeat {
			if f.FeatureType == domain.FeatureResponseCache {
				if hit := p.cacheGet(exactKey, f.Config); hit != nil {
					addStep(PipelineStep{Phase: 3, Kind: "cache", Name: "Cache", Outcome: "hit_exact", Detail: "exact key match"}, 0)
					p.metrics.record(router.ID, "cache_hit_exact", time.Since(start))
					ch := make(chan StreamChunk, 2)
					ch <- StreamChunk{Delta: hit.Content}
					ch <- StreamChunk{Done: true, SelectedModelID: hit.SelectedModelID, SelectedTargetID: hit.SelectedTargetID, CachedInputTokens: estimateCachedInputTokens(fields), ModelDefKey: hit.ModelDefKey, Provider: hit.Provider, CacheHit: true, CacheHitType: "exact"}
					close(ch)
					return ch, &steps, nil
				}
			}
		}
	}

	// Check circuit breaker before streaming.
	cb := p.circuits.get(target.ID)
	if !cb.Allow() {
		addStep(PipelineStep{Phase: 6, Kind: "inference", Name: "Model Inference", Outcome: "error", Detail: "circuit breaker open"}, 0)
		return nil, &steps, domain.ErrAllTargetsFailed
	}

	t0stream := time.Now()
	upstream, err := inferencer.InferModelStream(ctx, target.ModelID, fields, options)
	if err != nil {
		cb.RecordFailure()
		addStep(PipelineStep{Phase: 6, Kind: "inference", Name: "Model Inference", Outcome: "error", Detail: err.Error()}, time.Since(t0stream))
		p.metrics.record(router.ID, "error", time.Since(start))
		return nil, &steps, err
	}
	cb.RecordSuccess()
	addStep(PipelineStep{Phase: 6, Kind: "inference", Name: "Model Inference", Outcome: "streaming", Detail: fmt.Sprintf("model: %s (stream)", target.ModelID)}, time.Since(t0stream))

	// Wrap the upstream channel: forward each chunk, then do post-stream
	// accounting (budget, cache, metrics) once the stream is exhausted.
	selectedModelID := target.ModelID
	selectedTargetID := target.ID
	out := make(chan StreamChunk, 32)
	go func() {
		defer close(out)
		var buf strings.Builder
		var streamCostUSD float64
		for chunk := range upstream {
			toSend := chunk
			if chunk.Done {
				toSend = StreamChunk{
					Done:              true,
					SelectedModelID:   selectedModelID,
					SelectedTargetID:  selectedTargetID,
					InputTokens:       chunk.InputTokens,
					OutputTokens:      chunk.OutputTokens,
					CachedInputTokens: chunk.CachedInputTokens,
					ModelDefKey:       chunk.ModelDefKey,
					Provider:          chunk.Provider,
					CostUSD:           chunk.CostUSD,
					ABVariant:         abVariant,
					ToolCalls:         chunk.ToolCalls,
				}
			}
			select {
			case out <- toSend:
			case <-ctx.Done():
				p.metrics.record(router.ID, "error", time.Since(start))
				return
			}
			if chunk.Err != nil {
				p.metrics.record(router.ID, "error", time.Since(start))
				return
			}
			if chunk.Done {
				streamCostUSD = chunk.CostUSD
			} else {
				buf.WriteString(chunk.Delta)
			}
		}

		p.updateLatency(selectedTargetID, float64(time.Since(t0stream).Milliseconds()))

		full := buf.String()
		result := &RouteInferResult{Content: full, SelectedModelID: selectedModelID, SelectedTargetID: selectedTargetID}

		// Phase 7: Budget accounting
		for _, f := range enabledFeat {
			if f.FeatureType == domain.FeatureBudget {
				t0 := time.Now()
				p.recordBudget(router.ID, streamCostUSD, f.Config, options)
				addStep(PipelineStep{Phase: 7, Kind: "budget_accounting", Name: "Budget Accounting", Outcome: "recorded"}, time.Since(t0))
				break
			}
		}
		// Phase 8: Cache store
		if !bypassCache {
			for _, f := range enabledFeat {
				switch f.FeatureType {
				case domain.FeatureResponseCache:
					t0 := time.Now()
					p.cacheSet(exactKey, result, f.Config)
					addStep(PipelineStep{Phase: 8, Kind: "cache_store", Name: "Cache Store", Outcome: "stored", Detail: "response cached"}, time.Since(t0))
				case domain.FeatureSemanticCache:
					if len(promptEmbedding) > 0 {
						modelID, _ := f.Config["model_id"].(string)
						t0 := time.Now()
						p.semanticCacheSet(semanticCacheScope(router.ID, modelID), modelID, promptEmbedding, result, f.Config)
						addStep(PipelineStep{Phase: 8, Kind: "cache_store", Name: "Semantic Cache Store", Outcome: "stored", Detail: "embedding cached"}, time.Since(t0))
					}
				}
			}
		}
		// Phase 8b: Semantic memory
		if !bypassCache {
			for _, f := range enabledFeat {
				if f.FeatureType == domain.FeatureSemanticMemory {
					go func(cfg map[string]any, promptText, responseText string) {
						memCtx, cancel := context.WithTimeout(ctx, semanticMemoryStoreTimeout)
						defer cancel()
						p.storeSemanticMemory(memCtx, router.ID, cfg, promptText, responseText)
					}(f.Config, fields["prompt"], full)
					break
				}
			}
		}
		// Phase 8c: Response fingerprinting
		for _, f := range enabledFeat {
			if f.FeatureType == domain.FeatureResponseFingerprinting && f.IsEnabled {
				t0 := time.Now()
				windowSize := int(toFloat(f.Config["window_size"]))
				alertThreshold := 3.0
				if v := toFloat(f.Config["alert_threshold"]); v > 0 {
					alertThreshold = v
				}
				fp := p.getOrCreateFingerprint(router.ID, windowSize)
				anomaly, detail := fp.record(len(full), alertThreshold)
				if anomaly {
					addStep(PipelineStep{Phase: 6, Kind: "fingerprint", Name: "Response Fingerprinting", Outcome: "anomaly", Detail: detail}, time.Since(t0))
				} else {
					addStep(PipelineStep{Phase: 6, Kind: "fingerprint", Name: "Response Fingerprinting", Outcome: "normal"}, time.Since(t0))
				}
				break
			}
		}
		// Phase 9: Metrics
		p.metrics.record(router.ID, "success", time.Since(start))
		// Phase 9b: Response prefetch (async)
		for _, f := range enabledFeat {
			if f.FeatureType == domain.FeatureResponsePrefetch && f.IsEnabled {
				dispatched := p.dispatchResponsePrefetch(ctx, router.ID, selectedModelID, full, fields["systemPrompt"], options, f.Config, inferencer)
				if dispatched > 0 {
					addStep(PipelineStep{Phase: 9, Kind: "prefetch", Name: "Response Prefetch", Outcome: "dispatched",
						Detail: fmt.Sprintf("%d follow-up(s) queued", dispatched)}, 0)
				}
				break
			}
		}
		// Update UCB1 arm if an adaptive A/B test was used.
		if ucbInterceptorID, _ := options["_ucb1_interceptor_id"].(string); ucbInterceptorID != "" {
			if ucbVariant, _ := options["_ucb1_variant"].(string); ucbVariant != "" {
				p.updateUCB1(ucbInterceptorID, ucbVariant, 1.0)
			}
		}
	}()

	return out, &steps, nil
}
