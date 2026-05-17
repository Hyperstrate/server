package application

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"hyperstrate/server/internal/modules/router/domain"
	"hyperstrate/server/internal/shared/dbtype"
)

type memoryFeatureRepo struct {
	items []domain.RouterFeature
}

func (r *memoryFeatureRepo) ListByRouterID(_ context.Context, _ string, routerID string) ([]domain.RouterFeature, error) {
	out := make([]domain.RouterFeature, 0, len(r.items))
	for _, item := range r.items {
		if item.RouterID == routerID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (r *memoryFeatureRepo) Create(_ context.Context, f *domain.RouterFeature) error {
	r.items = append(r.items, *f)
	return nil
}

func (r *memoryFeatureRepo) FindByID(_ context.Context, _ string, id string) (*domain.RouterFeature, error) {
	for i := range r.items {
		if r.items[i].ID == id {
			return &r.items[i], nil
		}
	}
	return nil, domain.ErrRouterFeatureNotFound
}

func (r *memoryFeatureRepo) Update(_ context.Context, f *domain.RouterFeature) error {
	for i := range r.items {
		if r.items[i].ID == f.ID {
			r.items[i] = *f
			return nil
		}
	}
	return domain.ErrRouterFeatureNotFound
}

func (r *memoryFeatureRepo) Delete(_ context.Context, _ string, id string) error {
	for i := range r.items {
		if r.items[i].ID == id {
			r.items = append(r.items[:i], r.items[i+1:]...)
			return nil
		}
	}
	return domain.ErrRouterFeatureNotFound
}

func (r *memoryFeatureRepo) DeleteByRouterID(_ context.Context, _ string, routerID string) error {
	filtered := r.items[:0]
	for _, item := range r.items {
		if item.RouterID != routerID {
			filtered = append(filtered, item)
		}
	}
	r.items = filtered
	return nil
}

func (r *memoryFeatureRepo) RemoveMCPServerID(_ context.Context, _, serverID string) error {
	for i := range r.items {
		cfg := map[string]any(r.items[i].Config)
		rawIDs, _ := cfg["server_ids"].([]any)
		next := make([]any, 0, len(rawIDs))
		for _, rawID := range rawIDs {
			if rawID != serverID {
				next = append(next, rawID)
			}
		}
		cfg["server_ids"] = next
		r.items[i].Config = cfg
	}
	return nil
}

type memoryInterceptorRepo struct {
	items []domain.RouterInterceptor
}

func (r *memoryInterceptorRepo) ListByRouterID(_ context.Context, _ string, routerID string) ([]domain.RouterInterceptor, error) {
	out := make([]domain.RouterInterceptor, 0, len(r.items))
	for _, item := range r.items {
		if item.RouterID == routerID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (r *memoryInterceptorRepo) Create(_ context.Context, i *domain.RouterInterceptor) error {
	r.items = append(r.items, *i)
	return nil
}

func (r *memoryInterceptorRepo) FindByID(_ context.Context, _ string, id string) (*domain.RouterInterceptor, error) {
	for i := range r.items {
		if r.items[i].ID == id {
			return &r.items[i], nil
		}
	}
	return nil, domain.ErrRouterInterceptorNotFound
}

func (r *memoryInterceptorRepo) Update(_ context.Context, item *domain.RouterInterceptor) error {
	for i := range r.items {
		if r.items[i].ID == item.ID {
			r.items[i] = *item
			return nil
		}
	}
	return domain.ErrRouterInterceptorNotFound
}

func (r *memoryInterceptorRepo) Delete(_ context.Context, _ string, id string) error {
	for i := range r.items {
		if r.items[i].ID == id {
			r.items = append(r.items[:i], r.items[i+1:]...)
			return nil
		}
	}
	return domain.ErrRouterInterceptorNotFound
}

func (r *memoryInterceptorRepo) DeleteByRouterID(_ context.Context, _ string, routerID string) error {
	filtered := r.items[:0]
	for _, item := range r.items {
		if item.RouterID != routerID {
			filtered = append(filtered, item)
		}
	}
	r.items = filtered
	return nil
}

func buildRouterServiceWithFeatureInterceptorRepos(featureRepo domain.RouterFeatureRepository, interceptorRepo domain.RouterInterceptorRepository) Service {
	return NewService(
		&orgRouterRepo{router: routerOwnedByOrgA()},
		&noopTargetRepo{},
		featureRepo,
		interceptorRepo,
		&noopAccessRepo{},
		&noopInferencer{},
		nil,
		nil,
		NewRouterInferenceEventBus(),
		NewRouterTargetEventBus(),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
}

func rawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func ptrRaw(v json.RawMessage) *json.RawMessage { return &v }

func assertMapEqual(t *testing.T, got, want map[string]any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func featureCreateEditCases() []struct {
	name string
	typ  domain.RouterFeatureType
	add  map[string]any
	edit map[string]any
} {
	return []struct {
		name string
		typ  domain.RouterFeatureType
		add  map[string]any
		edit map[string]any
	}{
		{"token optimization", domain.FeatureTokenOptimization, map[string]any{"max_chars": 1200}, map[string]any{"max_chars": 900}},
		{"context trimming", domain.FeatureContextTrimming, map[string]any{"max_chars": 2200}, map[string]any{"max_chars": 1800}},
		{"token cost optimization", domain.FeatureTokenCostOptimization, map[string]any{"fields": []string{"prompt"}, "max_prompt_chars": 4000}, map[string]any{"fields": []string{"systemPrompt"}, "output_max_tokens": 120}},
		{"prompt optimizer", domain.FeaturePromptOptimizer, map[string]any{"optimizers": []string{"compact_whitespace"}}, map[string]any{"optimizers": []string{"dedupe_lines"}}},
		{"prompt policy rollout", domain.FeaturePromptPolicyRollout, map[string]any{"variants": []map[string]any{{"name": "a", "prompt_id": "prm_a", "percentage": 25}}}, map[string]any{"variants": []map[string]any{{"name": "b", "prompt_id": "prm_b", "percentage": 75}}}},
		{"response cache", domain.FeatureResponseCache, map[string]any{"ttl_seconds": 300}, map[string]any{"ttl_seconds": 600}},
		{"semantic cache", domain.FeatureSemanticCache, map[string]any{"ttl_seconds": 300, "similarity_threshold": 0.91, "model_id": "mdl_embed"}, map[string]any{"ttl_seconds": 600, "similarity_threshold": 0.88, "model_id": "mdl_embed_2"}},
		{"retry", domain.FeatureRetry, map[string]any{"max_retries": 3, "initial_delay_ms": 100, "backoff_multiplier": 2}, map[string]any{"max_retries": 5, "initial_delay_ms": 200, "backoff_multiplier": 1.5}},
		{"rate limit", domain.FeatureRateLimit, map[string]any{"rps": 10, "burst": 20}, map[string]any{"rps": 5, "burst": 10}},
		{"budget", domain.FeatureBudget, map[string]any{"period": "monthly", "max_requests": 100, "max_cost_usd": 10.5, "alert_percent": 80, "agent_budgets": map[string]any{"codex": map[string]any{"max_requests": 10}}}, map[string]any{"period": "daily", "max_requests": 20, "max_cost_usd": 2.5, "alert_percent": 90}},
		{"fallback", domain.FeatureFallback, map[string]any{}, map[string]any{}},
		{"mcp tools", domain.FeatureMCPTools, map[string]any{"server_ids": []string{"srv_1"}, "max_turns": 4, "require_approval": true, "allowed_tools": []string{"search"}, "blocked_tools": []string{"delete"}, "allowed_team_ids": []string{"team_1"}}, map[string]any{"server_ids": []string{"srv_2"}, "max_turns": 6}},
		{"health check", domain.FeatureHealthCheck, map[string]any{}, map[string]any{}},
		{"structured output", domain.FeatureStructuredOutput, map[string]any{"schema": map[string]any{"type": "object"}, "name": "answer", "strict": true}, map[string]any{"schema": map[string]any{"type": "array"}, "name": "items"}},
		{"request coalescing", domain.FeatureRequestCoalescing, map[string]any{"window_ms": 200, "max_waiters": 5}, map[string]any{"window_ms": 50, "max_waiters": 2}},
		{"prompt caching", domain.FeaturePromptCaching, map[string]any{}, map[string]any{}},
		{"hedging", domain.FeatureHedging, map[string]any{"quality_check": "min_length", "target_ids": []string{"tgt_a"}, "min_length": 20, "timeout_ms": 5000}, map[string]any{"quality_check": "any", "target_ids": []string{"tgt_b"}, "timeout_ms": 3000}},
		{"quality gate", domain.FeatureQualityGate, map[string]any{"judge_model_id": "mdl_judge", "min_score": 7.5, "action": "reject", "rubric_prompt": "score"}, map[string]any{"judge_model_id": "mdl_judge_2", "min_score": 8.5, "action": "retry"}},
		{"context compression", domain.FeatureContextCompression, map[string]any{"max_chars": 8000, "keep_recent": 2}, map[string]any{"max_chars": 4000, "keep_recent": 1}},
		{"semantic memory", domain.FeatureSemanticMemory, map[string]any{"model_id": "mdl_embed", "max_examples": 5, "ttl_days": 30, "similarity_threshold": 0.85}, map[string]any{"model_id": "mdl_embed_2", "max_examples": 3, "ttl_days": 7, "similarity_threshold": 0.9}},
		{"cost aware routing", domain.FeatureCostAwareRouting, map[string]any{"thresholds": []map[string]any{{"max_chars": 1000, "target_id": "tgt_a"}}, "default_target_id": "tgt_b"}, map[string]any{"thresholds": []map[string]any{{"max_chars": 500, "target_id": "tgt_b"}}}},
		{"response prefetch", domain.FeatureResponsePrefetch, map[string]any{"follow_up_prompts": []string{"next"}, "ttl_seconds": 300}, map[string]any{"follow_up_prompts": []string{"again"}, "ttl_seconds": 120}},
		{"response fingerprinting", domain.FeatureResponseFingerprinting, map[string]any{"window_size": 100, "alert_threshold": 3.0}, map[string]any{"window_size": 50, "alert_threshold": 2.0}},
	}
}

func TestService_AddFeature_createsEveryFeatureTypeWithNormalizedConfig(t *testing.T) {
	for _, tc := range featureCreateEditCases() {
		t.Run(tc.name, func(t *testing.T) {
			repo := &memoryFeatureRepo{items: []domain.RouterFeature{{ID: "existing", RouterID: "rtr_a1", ExecutionOrder: 2}}}
			svc := buildRouterServiceWithFeatureInterceptorRepos(repo, &memoryInterceptorRepo{})

			resp, err := svc.AddFeature(routerCtx(routerOrgA), "rtr_a1", AddFeatureInput{
				FeatureType: tc.typ,
				Config:      rawJSON(t, tc.add),
			})
			if err != nil {
				t.Fatalf("AddFeature failed: %v", err)
			}

			if resp.FeatureType != tc.typ || !resp.IsEnabled || resp.ExecutionOrder != 3 {
				t.Fatalf("unexpected feature response: %+v", resp)
			}
			want, err := featureConfigRawToMap(tc.typ, rawJSON(t, tc.add))
			if err != nil {
				t.Fatal(err)
			}
			assertMapEqual(t, resp.Config, want)
			if len(repo.items) != 2 {
				t.Fatalf("expected repo to contain created feature, got %d items", len(repo.items))
			}
			assertMapEqual(t, map[string]any(repo.items[1].Config), want)
		})
	}
}

func TestService_UpdateFeature_editsEveryFeatureTypeConfigAndState(t *testing.T) {
	for _, tc := range featureCreateEditCases() {
		t.Run(tc.name, func(t *testing.T) {
			repo := &memoryFeatureRepo{items: []domain.RouterFeature{{
				ID:             "rfeat_1",
				RouterID:       "rtr_a1",
				FeatureType:    tc.typ,
				Config:         dbtype.JSONMap{"old": true},
				ExecutionOrder: 1,
				IsEnabled:      true,
			}}}
			svc := buildRouterServiceWithFeatureInterceptorRepos(repo, &memoryInterceptorRepo{})
			order := 9
			enabled := false

			resp, err := svc.UpdateFeature(routerCtx(routerOrgA), "rtr_a1", "rfeat_1", UpdateFeatureInput{
				Config:         ptrRaw(rawJSON(t, tc.edit)),
				ExecutionOrder: &order,
				IsEnabled:      &enabled,
			})
			if err != nil {
				t.Fatalf("UpdateFeature failed: %v", err)
			}

			want, err := featureConfigRawToMap(tc.typ, rawJSON(t, tc.edit))
			if err != nil {
				t.Fatal(err)
			}
			if resp.ExecutionOrder != order || resp.IsEnabled {
				t.Fatalf("expected edited order/enabled, got %+v", resp)
			}
			assertMapEqual(t, resp.Config, want)
			assertMapEqual(t, map[string]any(repo.items[0].Config), want)
		})
	}
}

func interceptorCreateEditCases() []struct {
	name string
	typ  domain.RouterInterceptorType
	add  map[string]any
	edit map[string]any
} {
	return []struct {
		name string
		typ  domain.RouterInterceptorType
		add  map[string]any
		edit map[string]any
	}{
		{"semantic classifier", domain.InterceptorSemanticClassifier, map[string]any{"model_id": "mdl_embed", "threshold": 0.82, "targets": map[string]any{"tgt_a": []any{"sales"}}}, map[string]any{"model_id": "mdl_embed_2", "threshold": 0.9, "targets": map[string]any{"tgt_b": []any{"support"}}}},
		{"content filter", domain.InterceptorContentFilter, map[string]any{"blocked_patterns": []any{"secret", "password"}}, map[string]any{"blocked_patterns": []any{"token"}}},
		{"pii detector", domain.InterceptorPIIDetector, map[string]any{"redact": true}, map[string]any{"redact": false}},
		{"prompt guard", domain.InterceptorPromptGuard, map[string]any{"sensitivity": "medium"}, map[string]any{"sensitivity": "high"}},
		{"ab test", domain.InterceptorABTest, map[string]any{"variants": []any{map[string]any{"name": "a", "model_id": "mdl_a", "weight": 1}}, "partition_key": "user_id"}, map[string]any{"variants": []any{map[string]any{"name": "b", "model_id": "mdl_b", "weight": 2}}}},
		{"prompt shield", domain.InterceptorPromptShield, map[string]any{"shield_model_id": "mdl_shield", "policies": []any{"no exfiltration"}}, map[string]any{"shield_model_id": "mdl_shield_2", "policies": []any{"no secrets"}}},
		{"team budget", domain.InterceptorTeamBudget, map[string]any{"budgets": map[string]any{"team_1": map[string]any{"max_cost_usd": 10.0, "max_requests": 100.0}}}, map[string]any{"budgets": map[string]any{"team_2": map[string]any{"max_cost_usd": 2.0, "overflow_target_id": "tgt_b"}}}},
	}
}

func TestService_AddInterceptor_createsEveryInterceptorType(t *testing.T) {
	for _, tc := range interceptorCreateEditCases() {
		t.Run(tc.name, func(t *testing.T) {
			repo := &memoryInterceptorRepo{items: []domain.RouterInterceptor{{ID: "existing", RouterID: "rtr_a1", ExecutionOrder: 1}}}
			svc := buildRouterServiceWithFeatureInterceptorRepos(&memoryFeatureRepo{}, repo)

			resp, err := svc.AddInterceptor(routerCtx(routerOrgA), "rtr_a1", AddInterceptorInput{
				Type:   tc.typ,
				Config: tc.add,
			})
			if err != nil {
				t.Fatalf("AddInterceptor failed: %v", err)
			}

			if resp.Type != tc.typ || !resp.IsEnabled || resp.ExecutionOrder != 2 {
				t.Fatalf("unexpected interceptor response: %+v", resp)
			}
			assertMapEqual(t, resp.Config, tc.add)
			if len(repo.items) != 2 {
				t.Fatalf("expected repo to contain created interceptor, got %d items", len(repo.items))
			}
			assertMapEqual(t, map[string]any(repo.items[1].Config), tc.add)
		})
	}
}

func TestService_UpdateInterceptor_editsEveryInterceptorTypeConfigAndState(t *testing.T) {
	for _, tc := range interceptorCreateEditCases() {
		t.Run(tc.name, func(t *testing.T) {
			repo := &memoryInterceptorRepo{items: []domain.RouterInterceptor{{
				ID:             "rint_1",
				RouterID:       "rtr_a1",
				Type:           tc.typ,
				Config:         dbtype.JSONMap{"old": true},
				ExecutionOrder: 1,
				IsEnabled:      true,
			}}}
			svc := buildRouterServiceWithFeatureInterceptorRepos(&memoryFeatureRepo{}, repo)
			order := 7
			enabled := false

			resp, err := svc.UpdateInterceptor(routerCtx(routerOrgA), "rtr_a1", "rint_1", UpdateInterceptorInput{
				Config:         tc.edit,
				ExecutionOrder: &order,
				IsEnabled:      &enabled,
			})
			if err != nil {
				t.Fatalf("UpdateInterceptor failed: %v", err)
			}
			if resp.ExecutionOrder != order || resp.IsEnabled {
				t.Fatalf("expected edited order/enabled, got %+v", resp)
			}
			assertMapEqual(t, resp.Config, tc.edit)
			assertMapEqual(t, map[string]any(repo.items[0].Config), tc.edit)
		})
	}
}
