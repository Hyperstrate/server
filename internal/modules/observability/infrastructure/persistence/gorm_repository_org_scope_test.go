package persistence

import (
	"testing"
	"time"

	aiDomain "hyperstrate/server/internal/modules/ai/domain"
	obsDomain "hyperstrate/server/internal/modules/observability/domain"
	routerDomain "hyperstrate/server/internal/modules/router/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestObservabilityRepositoriesScopeSensitiveLookupsByOrg(t *testing.T) {
	db := newObservabilityOrgScopeTestDB(t)
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)

	insertOrgScopeRows(t, db, now)

	logRepo := NewInferenceLogRepository(db)
	payloadRepo := NewInferencePayloadRepository(db)
	webhookRepo := NewWebhookDeliveryRepository(db)
	healthRepo := NewProviderHealthRepository(db)

	if err := logRepo.UpdateFeedback("org_a", "log_b", 1); err != nil {
		t.Fatalf("cross-org UpdateFeedback returned error: %v", err)
	}
	var crossOrgLog obsDomain.InferenceLog
	if err := db.First(&crossOrgLog, "id = ?", "log_b").Error; err != nil {
		t.Fatalf("find cross-org log: %v", err)
	}
	if crossOrgLog.Feedback != 0 {
		t.Fatalf("cross-org feedback changed to %d, want unchanged", crossOrgLog.Feedback)
	}

	payload, err := payloadRepo.FindByLogID("org_a", "log_b")
	if err != nil {
		t.Fatalf("FindByLogID cross-org returned error: %v", err)
	}
	if payload != nil {
		t.Fatalf("FindByLogID returned cross-org payload %+v", payload)
	}

	webhooks, total, err := webhookRepo.ListByRouterID("org_a", "rtr_b", 20, 0)
	if err != nil {
		t.Fatalf("ListByRouterID cross-org returned error: %v", err)
	}
	if total != 0 || len(webhooks) != 0 {
		t.Fatalf("cross-org webhooks total=%d len=%d, want none", total, len(webhooks))
	}

	health, err := healthRepo.ListAll("org_a")
	if err != nil {
		t.Fatalf("ListAll provider health: %v", err)
	}
	if len(health) != 1 || health[0].ModelID != "mdl_a" {
		t.Fatalf("provider health = %+v, want only org_a model", health)
	}

	abRows, err := logRepo.AggregateByABVariant("org_a", "rtr_b", nil, nil)
	if err != nil {
		t.Fatalf("AggregateByABVariant cross-org returned error: %v", err)
	}
	if len(abRows) != 0 {
		t.Fatalf("cross-org AB rows = %+v, want none", abRows)
	}

	cache, err := logRepo.RouterCacheQuery("org_a", "rtr_b", nil, nil)
	if err != nil {
		t.Fatalf("RouterCacheQuery cross-org returned error: %v", err)
	}
	if cache.TotalRequests != 0 {
		t.Fatalf("cross-org cache total = %d, want 0", cache.TotalRequests)
	}

	traces, err := logRepo.ListTracesForRouter("org_a", "rtr_b", nil, nil, 20)
	if err != nil {
		t.Fatalf("ListTracesForRouter cross-org returned error: %v", err)
	}
	if len(traces) != 0 {
		t.Fatalf("cross-org traces = %+v, want none", traces)
	}
}

func newObservabilityOrgScopeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(
		&aiDomain.Model{},
		&routerDomain.Router{},
		&obsDomain.InferenceLog{},
		&obsDomain.InferencePayload{},
		&obsDomain.WebhookDelivery{},
		&obsDomain.ProviderHealth{},
	); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func insertOrgScopeRows(t *testing.T, db *gorm.DB, now time.Time) {
	t.Helper()
	for _, model := range []aiDomain.Model{
		{ID: "mdl_a", OrgID: "org_a", ModelDefinitionKey: "chatgpt/gpt-5.4", CreatedAt: now},
		{ID: "mdl_b", OrgID: "org_b", ModelDefinitionKey: "chatgpt/gpt-5.4", CreatedAt: now},
	} {
		entry := model
		if err := db.Create(&entry).Error; err != nil {
			t.Fatalf("create model %s: %v", entry.ID, err)
		}
	}
	for _, router := range []routerDomain.Router{
		{ID: "rtr_a", OrgID: "org_a", Name: "Org A router", Strategy: routerDomain.RoutingStrategyRoundRobin, CreatedAt: now},
		{ID: "rtr_b", OrgID: "org_b", Name: "Org B router", Strategy: routerDomain.RoutingStrategyRoundRobin, CreatedAt: now},
	} {
		entry := router
		if err := db.Create(&entry).Error; err != nil {
			t.Fatalf("create router %s: %v", entry.ID, err)
		}
	}
	for _, log := range []obsDomain.InferenceLog{
		{ID: "log_a", OrgID: "org_a", RouterID: "rtr_a", ModelID: "mdl_a", Status: "success", ABVariant: "A", PipelineTrace: `[{"kind":"feature","name":"retry","outcome":"passed"}]`, CacheHitType: "exact", CacheHit: true, CreatedAt: now},
		{ID: "log_b", OrgID: "org_b", RouterID: "rtr_b", ModelID: "mdl_b", Status: "success", ABVariant: "B", PipelineTrace: `[{"kind":"feature","name":"retry","outcome":"passed"}]`, CacheHitType: "exact", CacheHit: true, CreatedAt: now},
	} {
		entry := log
		if err := db.Create(&entry).Error; err != nil {
			t.Fatalf("create log %s: %v", entry.ID, err)
		}
	}
	for _, payload := range []obsDomain.InferencePayload{
		{LogID: "log_a", RouterID: "rtr_a", RequestFields: `{"prompt":"a"}`, ResponseContent: "a", CreatedAt: now},
		{LogID: "log_b", RouterID: "rtr_b", RequestFields: `{"prompt":"b"}`, ResponseContent: "b", CreatedAt: now},
	} {
		entry := payload
		if err := db.Create(&entry).Error; err != nil {
			t.Fatalf("create payload %s: %v", entry.LogID, err)
		}
	}
	for _, delivery := range []obsDomain.WebhookDelivery{
		{ID: "wh_a", RouterID: "rtr_a", Event: "inference.completed", URL: "https://a.example", Success: true, CreatedAt: now},
		{ID: "wh_b", RouterID: "rtr_b", Event: "inference.completed", URL: "https://b.example", Success: true, CreatedAt: now},
	} {
		entry := delivery
		if err := db.Create(&entry).Error; err != nil {
			t.Fatalf("create webhook %s: %v", entry.ID, err)
		}
	}
	for _, health := range []obsDomain.ProviderHealth{
		{ModelID: "mdl_a", ModelDefKey: "chatgpt/gpt-5.4", Provider: "open_ai", IsHealthy: true, CheckedAt: now},
		{ModelID: "mdl_b", ModelDefKey: "chatgpt/gpt-5.4", Provider: "open_ai", IsHealthy: true, CheckedAt: now},
	} {
		entry := health
		if err := db.Create(&entry).Error; err != nil {
			t.Fatalf("create provider health %s: %v", entry.ModelID, err)
		}
	}
}
