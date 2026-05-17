package application

import (
	"fmt"

	"hyperstrate/server/internal/modules/router/domain"
)

func lintRouterConfig(
	routerID string,
	targets []domain.RouterTarget,
	features []domain.RouterFeature,
	interceptors []domain.RouterInterceptor,
	access []domain.RouterTeamAccess,
) *RouterLintResponse {
	resp := &RouterLintResponse{RouterID: routerID, OK: true}
	add := func(severity, code, message string, feature domain.RouterFeatureType) {
		resp.Issues = append(resp.Issues, RouterLintIssue{
			Severity: severity,
			Code:     code,
			Message:  message,
			Feature:  string(feature),
		})
		if severity == "error" {
			resp.OK = false
		}
	}

	enabledTargets := enabledTargets(targets)
	targetIDs := map[string]struct{}{}
	modelIDs := map[string]struct{}{}
	for _, t := range enabledTargets {
		targetIDs[t.ID] = struct{}{}
		modelIDs[t.ModelID] = struct{}{}
	}
	if len(enabledTargets) == 0 {
		add("error", "no_enabled_targets", "Router has no enabled targets.", "")
	}

	for _, f := range sortedEnabledFeatures(features) {
		switch f.FeatureType {
		case domain.FeatureSemanticCache:
			if modelID, _ := f.Config["model_id"].(string); modelID == "" {
				add("error", "semantic_cache_missing_model", "Semantic cache requires an embedding model_id.", f.FeatureType)
			}
		case domain.FeatureHedging:
			ids := extractStringSlice(f.Config["target_ids"])
			if len(ids) == 0 {
				ids = extractStringSlice(f.Config["targets"])
			}
			for _, id := range ids {
				if _, ok := targetIDs[id]; ok {
					continue
				}
				if _, ok := modelIDs[id]; ok {
					add("warning", "hedging_uses_model_id", fmt.Sprintf("Hedging target %q is a model ID; prefer target_ids so target prompts and health checks are unambiguous.", id), f.FeatureType)
					continue
				}
				add("error", "hedging_unknown_target", fmt.Sprintf("Hedging target %q does not match any enabled target.", id), f.FeatureType)
			}
		case domain.FeatureMCPTools:
			if boolCfg(f.Config, "require_approval", false) {
				add("info", "mcp_requires_approval", "MCP tool calls require request-level approval via options.mcp_approved=true.", f.FeatureType)
			}
			if len(extractStringSlice(f.Config["blocked_tools"])) == 0 && len(extractStringSlice(f.Config["allowed_tools"])) == 0 {
				add("warning", "mcp_unrestricted_tools", "MCP tools are unrestricted; configure allowed_tools or blocked_tools for governance.", f.FeatureType)
			}
			if len(access) > 0 && len(extractStringSlice(f.Config["allowed_team_ids"])) == 0 {
				add("warning", "mcp_missing_team_allowlist", "Router has team access rules but MCP feature has no allowed_team_ids allow-list.", f.FeatureType)
			}
		case domain.FeatureBudget:
			if hasScopedBudgetConfig(f.Config) {
				add("info", "agent_budget_controls", "Scoped agent/role/repo/branch budget controls are enabled.", f.FeatureType)
			}
		case domain.FeaturePromptPolicyRollout:
			variants, _ := f.Config["variants"].([]any)
			if len(variants) == 0 {
				add("error", "rollout_missing_variants", "Prompt/policy rollout requires variants with prompt_id and percentage.", f.FeatureType)
			} else {
				add("info", "prompt_policy_rollout", "Prompt/policy rollout is enabled; use quality/cost analytics to decide rollback.", f.FeatureType)
			}
		}
	}

	for _, ic := range sortedEnabledInterceptors(interceptors) {
		if ic.Type == domain.InterceptorTeamBudget && len(access) == 0 {
			resp.Issues = append(resp.Issues, RouterLintIssue{
				Severity: "warning",
				Code:     "team_budget_without_router_access",
				Message:  "Team budget interceptor is enabled but router access is open to all teams.",
				Feature:  string(ic.Type),
			})
		}
	}

	if featureType := unsupportedStreamingFeature(sortedEnabledFeatures(features)); featureType != "" {
		add("error", "streaming_feature_unsupported", fmt.Sprintf("%s is enabled; streaming requests will be rejected for this router.", featureType), featureType)
	}

	return resp
}

func hasScopedBudgetConfig(cfg map[string]any) bool {
	for _, key := range []string{"agent_budgets", "role_budgets", "repo_budgets", "branch_budgets"} {
		if raw, ok := cfg[key].(map[string]any); ok && len(raw) > 0 {
			return true
		}
	}
	return false
}
