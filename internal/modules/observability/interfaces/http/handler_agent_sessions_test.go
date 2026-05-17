package http

import (
	"context"
	"encoding/json"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/modules/observability/application"
	obsDomain "hyperstrate/server/internal/modules/observability/domain"
	"hyperstrate/server/internal/shared/pagination"

	"github.com/gin-gonic/gin"
)

const agentAnalyticsTestOrgID = "org_test"

type agentAnalyticsServiceStub struct {
	application.Service

	listAgentSessionsFn       func(filter obsDomain.InferenceLogFilter, limit, offset int) ([]obsDomain.AgentSessionSummary, int64, error)
	listInferenceLogsFn       func(filter obsDomain.InferenceLogFilter, limit, offset int) ([]obsDomain.InferenceLog, int64, error)
	getAgentSessionInsightsFn func(filter obsDomain.InferenceLogFilter, sessionID string) (*obsDomain.AgentSessionInsights, error)
	listAgentSessionEventsFn  func(filter obsDomain.InferenceLogFilter, limit, offset int) ([]obsDomain.AgentSessionEvent, int64, error)
	listToolArchivesFn        func(filter obsDomain.InferenceLogFilter, limit, offset int) ([]obsDomain.ToolCallArchive, int64, error)
	getToolArchiveFn          func(orgID, id string) (*obsDomain.ToolCallArchive, error)
	listCompressionEventsFn   func(filter obsDomain.InferenceLogFilter, limit, offset int) ([]obsDomain.CompressionEvent, int64, error)
	listCostlyPromptsFn       func(filter obsDomain.InferenceLogFilter, limit int) ([]obsDomain.CostlyPrompt, error)
	listSubagentBreakdownFn   func(filter obsDomain.InferenceLogFilter) ([]obsDomain.SubagentBreakdown, error)
	listLoopDetectionsFn      func(filter obsDomain.InferenceLogFilter, limit int) ([]obsDomain.LoopDetection, error)
}

func (s *agentAnalyticsServiceStub) ListAgentSessions(filter obsDomain.InferenceLogFilter, limit, offset int) ([]obsDomain.AgentSessionSummary, int64, error) {
	if s.listAgentSessionsFn == nil {
		panic("ListAgentSessions called unexpectedly")
	}
	return s.listAgentSessionsFn(filter, limit, offset)
}

func (s *agentAnalyticsServiceStub) ListInferenceLogs(filter obsDomain.InferenceLogFilter, limit, offset int) ([]obsDomain.InferenceLog, int64, error) {
	if s.listInferenceLogsFn == nil {
		panic("ListInferenceLogs called unexpectedly")
	}
	return s.listInferenceLogsFn(filter, limit, offset)
}

func (s *agentAnalyticsServiceStub) GetAgentSessionInsights(filter obsDomain.InferenceLogFilter, sessionID string) (*obsDomain.AgentSessionInsights, error) {
	if s.getAgentSessionInsightsFn == nil {
		panic("GetAgentSessionInsights called unexpectedly")
	}
	return s.getAgentSessionInsightsFn(filter, sessionID)
}

func (s *agentAnalyticsServiceStub) ListAgentSessionEvents(filter obsDomain.InferenceLogFilter, limit, offset int) ([]obsDomain.AgentSessionEvent, int64, error) {
	if s.listAgentSessionEventsFn == nil {
		panic("ListAgentSessionEvents called unexpectedly")
	}
	return s.listAgentSessionEventsFn(filter, limit, offset)
}

func (s *agentAnalyticsServiceStub) ListToolArchives(filter obsDomain.InferenceLogFilter, limit, offset int) ([]obsDomain.ToolCallArchive, int64, error) {
	if s.listToolArchivesFn == nil {
		panic("ListToolArchives called unexpectedly")
	}
	return s.listToolArchivesFn(filter, limit, offset)
}

func (s *agentAnalyticsServiceStub) GetToolArchive(orgID, id string) (*obsDomain.ToolCallArchive, error) {
	if s.getToolArchiveFn == nil {
		panic("GetToolArchive called unexpectedly")
	}
	return s.getToolArchiveFn(orgID, id)
}

func (s *agentAnalyticsServiceStub) ListCompressionEvents(filter obsDomain.InferenceLogFilter, limit, offset int) ([]obsDomain.CompressionEvent, int64, error) {
	if s.listCompressionEventsFn == nil {
		panic("ListCompressionEvents called unexpectedly")
	}
	return s.listCompressionEventsFn(filter, limit, offset)
}

func (s *agentAnalyticsServiceStub) ListCostlyPrompts(filter obsDomain.InferenceLogFilter, limit int) ([]obsDomain.CostlyPrompt, error) {
	if s.listCostlyPromptsFn == nil {
		panic("ListCostlyPrompts called unexpectedly")
	}
	return s.listCostlyPromptsFn(filter, limit)
}

func (s *agentAnalyticsServiceStub) ListSubagentBreakdown(filter obsDomain.InferenceLogFilter) ([]obsDomain.SubagentBreakdown, error) {
	if s.listSubagentBreakdownFn == nil {
		panic("ListSubagentBreakdown called unexpectedly")
	}
	return s.listSubagentBreakdownFn(filter)
}

func (s *agentAnalyticsServiceStub) ListLoopDetections(filter obsDomain.InferenceLogFilter, limit int) ([]obsDomain.LoopDetection, error) {
	if s.listLoopDetectionsFn == nil {
		panic("ListLoopDetections called unexpectedly")
	}
	return s.listLoopDetectionsFn(filter, limit)
}

func TestListAgentSessionsParsesFiltersAndPagination(t *testing.T) {
	startedAt := time.Date(2026, 5, 10, 8, 30, 0, 0, time.UTC)
	var capturedFilter obsDomain.InferenceLogFilter
	var capturedLimit, capturedOffset int

	svc := &agentAnalyticsServiceStub{
		listAgentSessionsFn: func(filter obsDomain.InferenceLogFilter, limit, offset int) ([]obsDomain.AgentSessionSummary, int64, error) {
			capturedFilter = filter
			capturedLimit = limit
			capturedOffset = offset
			return []obsDomain.AgentSessionSummary{
				{
					SessionID:   "org_test:codex:eduard:sess_1",
					Agent:       "codex",
					RouterID:    "rtr_1",
					UserID:      "eduard",
					StartedAt:   startedAt,
					LastSeenAt:  startedAt.Add(2 * time.Minute),
					Turns:       3,
					TotalTokens: 4200,
					CostUSD:     0.12,
				},
			}, 7, nil
		},
	}

	rec := performAgentAnalyticsRequest(svc, nethttp.MethodGet, "/analytics/agent-sessions?agent=claude%20code&routerId=rtr_1&virtualKeyId=vkey_1&userId=eduard&from=2026-04-10&to=2026-05-10&page=2&perPage=3")
	if rec.Code != nethttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	if capturedFilter.OrgID != agentAnalyticsTestOrgID {
		t.Fatalf("OrgID = %q, want %q", capturedFilter.OrgID, agentAnalyticsTestOrgID)
	}
	if capturedFilter.Agent != "claude code" || capturedFilter.RouterID != "rtr_1" || capturedFilter.VirtualKeyID != "vkey_1" || capturedFilter.UserID != "eduard" {
		t.Fatalf("unexpected filter: %+v", capturedFilter)
	}
	if capturedFilter.From == nil || !capturedFilter.From.Equal(time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("From = %v", capturedFilter.From)
	}
	if capturedFilter.To == nil || !capturedFilter.To.Equal(time.Date(2026, 5, 10, 23, 59, 59, 0, time.UTC)) {
		t.Fatalf("To = %v", capturedFilter.To)
	}
	if capturedLimit != 3 || capturedOffset != 3 {
		t.Fatalf("limit/offset = %d/%d, want 3/3", capturedLimit, capturedOffset)
	}

	var body pagination.Paginated[obsDomain.AgentSessionSummary]
	decodeJSON(t, rec, &body)
	if body.Meta.Total != 7 || body.Meta.Page != 2 || body.Meta.PerPage != 3 || body.Meta.Count != 1 {
		t.Fatalf("unexpected pagination meta: %+v", body.Meta)
	}
	if len(body.Items) != 1 || body.Items[0].SessionID != "org_test:codex:eduard:sess_1" {
		t.Fatalf("unexpected items: %+v", body.Items)
	}

	var raw struct {
		Items []map[string]any `json:"items"`
	}
	decodeJSON(t, rec, &raw)
	if len(raw.Items) != 1 || raw.Items[0]["agent"] != "codex" {
		t.Fatalf("expected agent field, got %+v", raw.Items)
	}
}

func TestListAgentSessionsUsesAgentQuery(t *testing.T) {
	var capturedFilter obsDomain.InferenceLogFilter
	svc := &agentAnalyticsServiceStub{
		listAgentSessionsFn: func(filter obsDomain.InferenceLogFilter, limit, offset int) ([]obsDomain.AgentSessionSummary, int64, error) {
			capturedFilter = filter
			return nil, 0, nil
		},
	}

	rec := performAgentAnalyticsRequest(svc, nethttp.MethodGet, "/analytics/agent-sessions?agent=claude%20code")
	if rec.Code != nethttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if capturedFilter.Agent != "claude code" {
		t.Fatalf("Agent = %q, want query value", capturedFilter.Agent)
	}
}

func TestInferenceLogResponseIncludesAgentAndRelations(t *testing.T) {
	raw, err := json.Marshal(InferenceLogResponse{
		InferenceLog: obsDomain.InferenceLog{ID: "ilog_1", Agent: "codex"},
		Model:        &ModelRef{ID: "model_1"},
		Router:       &RouterRef{ID: "rtr_1"},
	})
	if err != nil {
		t.Fatalf("Marshal = %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("Unmarshal = %v", err)
	}
	if body["id"] != "ilog_1" || body["agent"] != "codex" {
		t.Fatalf("unexpected agent response shape: %v", body)
	}
	if _, ok := body["model"].(map[string]any); !ok {
		t.Fatalf("missing model relation: %v", body)
	}
	if _, ok := body["router"].(map[string]any); !ok {
		t.Fatalf("missing router relation: %v", body)
	}
}

func TestListAgentSessionsRejectsInvalidDateBeforeCallingService(t *testing.T) {
	called := false
	svc := &agentAnalyticsServiceStub{
		listAgentSessionsFn: func(obsDomain.InferenceLogFilter, int, int) ([]obsDomain.AgentSessionSummary, int64, error) {
			called = true
			return nil, 0, nil
		},
	}

	rec := performAgentAnalyticsRequest(svc, nethttp.MethodGet, "/analytics/agent-sessions?from=not-a-date")
	if rec.Code != nethttp.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("service should not be called when date parsing fails")
	}
}

func TestAgentSessionDetailEndpointsScopeByOrgAndSession(t *testing.T) {
	sessionID := "org_test:codex:eduard:sess_1"
	calls := map[string]bool{}
	svc := &agentAnalyticsServiceStub{
		listInferenceLogsFn: func(filter obsDomain.InferenceLogFilter, limit, offset int) ([]obsDomain.InferenceLog, int64, error) {
			calls["logs"] = true
			assertAgentSessionFilter(t, filter, sessionID)
			if limit != 2 || offset != 2 {
				t.Fatalf("logs limit/offset = %d/%d, want 2/2", limit, offset)
			}
			return []obsDomain.InferenceLog{{ID: "ilog_1", OrgID: agentAnalyticsTestOrgID, AgentSessionID: sessionID}}, 3, nil
		},
		getAgentSessionInsightsFn: func(filter obsDomain.InferenceLogFilter, gotSessionID string) (*obsDomain.AgentSessionInsights, error) {
			calls["insights"] = true
			if filter.OrgID != agentAnalyticsTestOrgID {
				t.Fatalf("insights OrgID = %q, want %q", filter.OrgID, agentAnalyticsTestOrgID)
			}
			if gotSessionID != sessionID {
				t.Fatalf("insights sessionID = %q, want %q", gotSessionID, sessionID)
			}
			return &obsDomain.AgentSessionInsights{SessionID: sessionID, QualityScore: 91}, nil
		},
		listAgentSessionEventsFn: func(filter obsDomain.InferenceLogFilter, limit, offset int) ([]obsDomain.AgentSessionEvent, int64, error) {
			calls["events"] = true
			assertAgentSessionFilter(t, filter, sessionID)
			if limit != 4 || offset != 4 {
				t.Fatalf("events limit/offset = %d/%d, want 4/4", limit, offset)
			}
			return []obsDomain.AgentSessionEvent{{ID: "asevt_1", OrgID: agentAnalyticsTestOrgID, AgentSessionID: sessionID, EventType: "checkpoint_created"}}, 5, nil
		},
	}

	tests := []struct {
		name string
		path string
		call string
	}{
		{name: "logs", path: "/analytics/agent-sessions/" + sessionID + "/logs?page=2&perPage=2", call: "logs"},
		{name: "insights", path: "/analytics/agent-sessions/" + sessionID + "/insights", call: "insights"},
		{name: "events", path: "/analytics/agent-sessions/" + sessionID + "/events?page=2&perPage=4", call: "events"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := performAgentAnalyticsRequest(svc, nethttp.MethodGet, tt.path)
			if rec.Code != nethttp.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if !calls[tt.call] {
				t.Fatalf("%s service method was not called", tt.call)
			}
		})
	}
}

func TestAgentSessionSupportingAnalyticsEndpointsScopeByOrgAndSessionQuery(t *testing.T) {
	sessionID := "org_test:cursor:eduard:sess_2"
	calls := map[string]bool{}
	svc := &agentAnalyticsServiceStub{
		listToolArchivesFn: func(filter obsDomain.InferenceLogFilter, limit, offset int) ([]obsDomain.ToolCallArchive, int64, error) {
			calls["toolArchives"] = true
			assertAgentSessionFilter(t, filter, sessionID)
			if limit != 5 || offset != 5 {
				t.Fatalf("tool archive limit/offset = %d/%d, want 5/5", limit, offset)
			}
			return []obsDomain.ToolCallArchive{{ID: "tcar_1", OrgID: agentAnalyticsTestOrgID, AgentSessionID: sessionID, ToolName: "rg.search"}}, 6, nil
		},
		listCompressionEventsFn: func(filter obsDomain.InferenceLogFilter, limit, offset int) ([]obsDomain.CompressionEvent, int64, error) {
			calls["compression"] = true
			assertAgentSessionFilter(t, filter, sessionID)
			if limit != 3 || offset != 6 {
				t.Fatalf("compression limit/offset = %d/%d, want 3/6", limit, offset)
			}
			return []obsDomain.CompressionEvent{{ID: "cevt_1", OrgID: agentAnalyticsTestOrgID, AgentSessionID: sessionID, SavedChars: 1200}}, 8, nil
		},
		listCostlyPromptsFn: func(filter obsDomain.InferenceLogFilter, limit int) ([]obsDomain.CostlyPrompt, error) {
			calls["costlyPrompts"] = true
			if filter.OrgID != agentAnalyticsTestOrgID {
				t.Fatalf("costly prompts OrgID = %q, want %q", filter.OrgID, agentAnalyticsTestOrgID)
			}
			if limit != 20 {
				t.Fatalf("costly prompt limit = %d, want 20", limit)
			}
			return []obsDomain.CostlyPrompt{{LogID: "ilog_2", AgentSessionID: sessionID, CostUSD: 0.42}}, nil
		},
		listSubagentBreakdownFn: func(filter obsDomain.InferenceLogFilter) ([]obsDomain.SubagentBreakdown, error) {
			calls["subagents"] = true
			assertAgentSessionFilter(t, filter, sessionID)
			return []obsDomain.SubagentBreakdown{{AgentSessionID: sessionID, AgentRole: "worker", CostUSD: 0.08}}, nil
		},
		listLoopDetectionsFn: func(filter obsDomain.InferenceLogFilter, limit int) ([]obsDomain.LoopDetection, error) {
			calls["loops"] = true
			assertAgentSessionFilter(t, filter, sessionID)
			if limit != 50 {
				t.Fatalf("loop limit = %d, want 50", limit)
			}
			return []obsDomain.LoopDetection{{LogID: "ilog_3", AgentSessionID: sessionID, Reason: "repeated tool error"}}, nil
		},
	}

	tests := []struct {
		name string
		path string
		call string
	}{
		{name: "tool archives", path: "/analytics/tool-archives?sessionId=" + sessionID + "&page=2&perPage=5", call: "toolArchives"},
		{name: "compression", path: "/analytics/compression-events?sessionId=" + sessionID + "&page=3&perPage=3", call: "compression"},
		{name: "costly prompts", path: "/analytics/costly-prompts", call: "costlyPrompts"},
		{name: "subagents", path: "/analytics/subagents?sessionId=" + sessionID, call: "subagents"},
		{name: "loops", path: "/analytics/loops?sessionId=" + sessionID, call: "loops"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := performAgentAnalyticsRequest(svc, nethttp.MethodGet, tt.path)
			if rec.Code != nethttp.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if !calls[tt.call] {
				t.Fatalf("%s service method was not called", tt.call)
			}
		})
	}
}

func TestGetToolArchiveUsesOrgScopeAndMapsMissingArchiveToNotFound(t *testing.T) {
	var capturedOrgID, capturedArchiveID string
	svc := &agentAnalyticsServiceStub{
		getToolArchiveFn: func(orgID, id string) (*obsDomain.ToolCallArchive, error) {
			capturedOrgID = orgID
			capturedArchiveID = id
			return &obsDomain.ToolCallArchive{ID: id, OrgID: orgID, ToolName: "terminal.exec"}, nil
		},
	}

	rec := performAgentAnalyticsRequest(svc, nethttp.MethodGet, "/analytics/tool-archives/tcar_1")
	if rec.Code != nethttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if capturedOrgID != agentAnalyticsTestOrgID || capturedArchiveID != "tcar_1" {
		t.Fatalf("captured org/id = %q/%q", capturedOrgID, capturedArchiveID)
	}

	svc.getToolArchiveFn = func(orgID, id string) (*obsDomain.ToolCallArchive, error) {
		return nil, errors.New("not found")
	}
	rec = performAgentAnalyticsRequest(svc, nethttp.MethodGet, "/analytics/tool-archives/missing")
	if rec.Code != nethttp.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func performAgentAnalyticsRequest(svc application.Service, method, path string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(domain.WithOrgID(c.Request.Context(), agentAnalyticsTestOrgID))
		c.Next()
	})

	handler := NewHandler(
		svc,
		func(defKey string) string { return defKey },
		func(context.Context, string) string { return "" },
		func(context.Context, string) string { return "" },
	)
	handler.RegisterRoutes(engine.Group(""))

	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func assertAgentSessionFilter(t *testing.T, filter obsDomain.InferenceLogFilter, sessionID string) {
	t.Helper()
	if filter.OrgID != agentAnalyticsTestOrgID {
		t.Fatalf("OrgID = %q, want %q", filter.OrgID, agentAnalyticsTestOrgID)
	}
	if filter.AgentSessionID != sessionID {
		t.Fatalf("AgentSessionID = %q, want %q", filter.AgentSessionID, sessionID)
	}
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
}
