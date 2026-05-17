package application

import (
	"testing"

	"hyperstrate/server/internal/modules/router/domain"
)

func TestLintRouterConfig_flagsUnsafeCombinations(t *testing.T) {
	resp := lintRouterConfig(
		"rtr_1",
		twoTargets(),
		[]domain.RouterFeature{
			feat(domain.FeatureSemanticCache, map[string]any{}),
			feat(domain.FeatureHedging, map[string]any{"target_ids": []any{"missing"}}),
			feat(domain.FeatureMCPTools, map[string]any{}),
			feat(domain.FeatureQualityGate, map[string]any{"judge_model_id": "mdl_judge"}),
		},
		nil,
		[]domain.RouterTeamAccess{{TeamID: "team_1"}},
	)

	if resp.OK {
		t.Fatal("expected lint errors")
	}
	for _, code := range []string{"semantic_cache_missing_model", "hedging_unknown_target", "mcp_unrestricted_tools", "mcp_missing_team_allowlist", "streaming_feature_unsupported"} {
		if !hasLintCode(resp.Issues, code) {
			t.Fatalf("missing lint code %q in %+v", code, resp.Issues)
		}
	}
}

func hasLintCode(issues []RouterLintIssue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
