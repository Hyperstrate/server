package application

import (
	"context"
	"testing"
	"time"

	"hyperstrate/server/internal/modules/router/domain"
	"hyperstrate/server/internal/shared/webhook"
)

type recordingBudgetQuerier struct {
	requests  int64
	costUSD   float64
	lastOrgID string
}

func (q *recordingBudgetQuerier) SumCostByPeriod(orgID, _ string, _, _ string, _ time.Time) (int64, float64, error) {
	q.lastOrgID = orgID
	return q.requests, q.costUSD, nil
}

func TestCheckBudgetAlertThresholdUsesCallerOrgAndFiresOncePerCrossing(t *testing.T) {
	querier := &recordingBudgetQuerier{requests: 12, costUSD: 85}
	svc := &service{
		pipeline:        newFeaturePipeline(nil, nil, nil, nil, nil, querier),
		budgetQuerier:   querier,
		budgetAlertSent: make(map[string]struct{}),
	}
	router := &domain.Router{
		ID:    "rtr_1",
		OrgID: "org_1",
		Configuration: domain.RouterConfiguration{
			WebhookURL: "https://hooks.example.com/budget",
		},
	}
	features := []domain.RouterFeature{{
		ID:          "feat_budget",
		RouterID:    router.ID,
		FeatureType: domain.FeatureBudget,
		IsEnabled:   true,
		Config:      map[string]any{"max_cost_usd": 100.0, "alert_percent": 80.0, "period": "monthly"},
	}}

	var events []webhook.Event
	oldFire := fireWebhook
	fireWebhook = func(_ string, event webhook.Event) {
		events = append(events, event)
	}
	t.Cleanup(func() { fireWebhook = oldFire })

	svc.checkBudgetAlertThreshold(context.Background(), "org_1", router, features)
	svc.checkBudgetAlertThreshold(context.Background(), "org_1", router, features)

	if querier.lastOrgID != "org_1" {
		t.Fatalf("budget query orgID = %q, want org_1", querier.lastOrgID)
	}
	if len(events) != 1 {
		t.Fatalf("threshold webhook fires = %d, want 1", len(events))
	}
	if events[0].Event != webhook.EventBudgetThreshold {
		t.Fatalf("event = %q, want %q", events[0].Event, webhook.EventBudgetThreshold)
	}
}

func TestCheckBudgetAlertThresholdFiresAgainAfterUsageDropsBelowThreshold(t *testing.T) {
	querier := &recordingBudgetQuerier{costUSD: 85}
	svc := &service{
		pipeline:        newFeaturePipeline(nil, nil, nil, nil, nil, querier),
		budgetQuerier:   querier,
		budgetAlertSent: make(map[string]struct{}),
	}
	router := &domain.Router{
		ID:    "rtr_1",
		OrgID: "org_1",
		Configuration: domain.RouterConfiguration{
			WebhookURL: "https://hooks.example.com/budget",
		},
	}
	features := []domain.RouterFeature{{
		ID:          "feat_budget",
		RouterID:    router.ID,
		FeatureType: domain.FeatureBudget,
		IsEnabled:   true,
		Config:      map[string]any{"max_cost_usd": 100.0, "alert_percent": 80.0, "period": "monthly"},
	}}

	var fires int
	oldFire := fireWebhook
	fireWebhook = func(string, webhook.Event) { fires++ }
	t.Cleanup(func() { fireWebhook = oldFire })

	svc.checkBudgetAlertThreshold(context.Background(), "org_1", router, features)
	querier.costUSD = 20
	svc.checkBudgetAlertThreshold(context.Background(), "org_1", router, features)
	querier.costUSD = 90
	svc.checkBudgetAlertThreshold(context.Background(), "org_1", router, features)

	if fires != 2 {
		t.Fatalf("threshold webhook fires = %d, want 2", fires)
	}
}
