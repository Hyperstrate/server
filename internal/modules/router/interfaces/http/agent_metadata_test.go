package http

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestInjectAgentSessionHeadersUsesPreferredAgentKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("POST", "/proxy/router/rtr_1/v1/chat/completions", nil)
	req.Header.Set("X-Agent", "codex")
	req.Header.Set("X-Parent-Agent", "claude_code")
	c.Request = req

	options := injectAgentSessionHeaders(c, nil)

	if options["agent"] != "codex" {
		t.Fatalf("agent = %v, want codex", options["agent"])
	}
	if options["parent_agent"] != "claude_code" {
		t.Fatalf("parent_agent = %v, want claude_code", options["parent_agent"])
	}
}

func TestInjectAgentSessionHeadersDetectsAgentFromUserAgent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("POST", "/proxy/router/rtr_1/v1/chat/completions", nil)
	req.Header.Set("User-Agent", "Codex/1.2.3")
	c.Request = req

	options := injectAgentSessionHeaders(c, nil)

	if options["agent"] != "codex" {
		t.Fatalf("agent = %v, want codex", options["agent"])
	}
}

func TestOpenAIChatRequestOptionsAcceptAgentMetadata(t *testing.T) {
	req := openAIChatRequest{
		Metadata: map[string]any{
			"agent":        "codex",
			"parent_agent": "claude_code",
			"temperature":  "metadata option should not leak",
		},
	}

	options := req.toOptions()

	if options["agent"] != "codex" {
		t.Fatalf("agent = %v, want codex", options["agent"])
	}
	if options["parent_agent"] != "claude_code" {
		t.Fatalf("parent_agent = %v, want claude_code", options["parent_agent"])
	}
	if _, ok := options["temperature"]; ok {
		encoded, _ := json.Marshal(options)
		t.Fatalf("unexpected arbitrary metadata copied into options: %s", encoded)
	}
}
