package http

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRegisterRoutes_mountsAgentSessionAnalyticsRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	handler := NewHandler(nil, nil, nil, nil)
	handler.RegisterRoutes(engine.Group(""))

	routes := map[string]bool{}
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = true
	}

	expected := []string{
		"GET /analytics/agent-sessions",
		"GET /analytics/agent-sessions/:sessionId/logs",
		"GET /analytics/agent-sessions/:sessionId/insights",
		"GET /analytics/agent-sessions/:sessionId/events",
		"GET /analytics/tool-archives",
		"GET /analytics/tool-archives/:id",
		"GET /analytics/compression-events",
		"GET /analytics/costly-prompts",
		"GET /analytics/subagents",
		"GET /analytics/loops",
	}

	for _, route := range expected {
		if !routes[route] {
			t.Fatalf("expected route %q to be registered", route)
		}
	}
}
