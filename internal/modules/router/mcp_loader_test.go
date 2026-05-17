package router

import (
	"context"
	"testing"

	aiDomain "hyperstrate/server/internal/modules/ai/domain"
	authDomain "hyperstrate/server/internal/modules/auth/domain"
)

type mcpLoaderRepoStub struct {
	server aiDomain.MCPServer
}

func (r *mcpLoaderRepoStub) Create(context.Context, *aiDomain.MCPServer) error { return nil }
func (r *mcpLoaderRepoStub) FindByID(context.Context, string, string) (*aiDomain.MCPServer, error) {
	return &r.server, nil
}
func (r *mcpLoaderRepoStub) FindByIDs(context.Context, string, []string) ([]aiDomain.MCPServer, error) {
	return []aiDomain.MCPServer{r.server}, nil
}
func (r *mcpLoaderRepoStub) List(context.Context, string) ([]aiDomain.MCPServer, error) {
	return []aiDomain.MCPServer{r.server}, nil
}
func (r *mcpLoaderRepoStub) Update(context.Context, *aiDomain.MCPServer) error { return nil }
func (r *mcpLoaderRepoStub) Delete(context.Context, string, string) error      { return nil }

func TestMCPLoaderMergesAuthAndExtraHeaders(t *testing.T) {
	loader := newMCPLoaderAdapter(&mcpLoaderRepoStub{
		server: aiDomain.MCPServer{
			ID:           "rmcp_1",
			OrgID:        "org_1",
			Name:         "Search",
			URL:          "https://mcp.example/rpc",
			AuthType:     "api_key",
			AuthHeader:   "X-API-Key",
			AuthToken:    "secret",
			ExtraHeaders: map[string]string{"X-Tenant": "tenant-a"},
			TimeoutSecs:  30,
		},
	})

	resolved, err := loader.GetMCPServers(authDomain.WithOrgID(context.Background(), "org_1"), []string{"rmcp_1"})
	if err != nil {
		t.Fatalf("GetMCPServers returned error: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved len = %d, want 1", len(resolved))
	}
	if got := resolved[0].Headers["X-API-Key"]; got != "secret" {
		t.Fatalf("auth header = %q, want secret", got)
	}
	if got := resolved[0].Headers["X-Tenant"]; got != "tenant-a" {
		t.Fatalf("extra header = %q, want tenant-a", got)
	}
}
