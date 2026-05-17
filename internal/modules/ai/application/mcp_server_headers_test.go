package application_test

import (
	"context"
	"testing"

	"hyperstrate/server/internal/modules/ai/application"
	"hyperstrate/server/internal/modules/ai/domain"
	authDomain "hyperstrate/server/internal/modules/auth/domain"
)

type stubMCPServerRepo struct {
	created *domain.MCPServer
	server  *domain.MCPServer
}

func (r *stubMCPServerRepo) Create(_ context.Context, server *domain.MCPServer) error {
	copy := *server
	r.created = &copy
	r.server = &copy
	return nil
}

func (r *stubMCPServerRepo) FindByID(_ context.Context, _, _ string) (*domain.MCPServer, error) {
	if r.server == nil {
		return nil, domain.ErrMCPServerNotFound
	}
	return r.server, nil
}

func (r *stubMCPServerRepo) FindByIDs(_ context.Context, _ string, _ []string) ([]domain.MCPServer, error) {
	if r.server == nil {
		return nil, nil
	}
	return []domain.MCPServer{*r.server}, nil
}

func (r *stubMCPServerRepo) List(_ context.Context, _ string) ([]domain.MCPServer, error) {
	if r.server == nil {
		return nil, nil
	}
	return []domain.MCPServer{*r.server}, nil
}

func (r *stubMCPServerRepo) Update(_ context.Context, server *domain.MCPServer) error {
	copy := *server
	r.server = &copy
	return nil
}

func (r *stubMCPServerRepo) Delete(_ context.Context, _, _ string) error { return nil }

func TestCreateMCPServerPersistsExtraHeaders(t *testing.T) {
	repo := &stubMCPServerRepo{}
	svc := newMCPHeaderTestService(repo)

	_, err := svc.CreateMCPServer(authDomain.WithOrgID(context.Background(), "org_1"), application.CreateMCPServerInput{
		Name:         "Search",
		URL:          "https://mcp.example/rpc",
		AuthType:     "bearer",
		AuthToken:    "token",
		ExtraHeaders: map[string]string{"X-Tenant": "tenant-a"},
	})
	if err != nil {
		t.Fatalf("CreateMCPServer returned error: %v", err)
	}
	if repo.created == nil {
		t.Fatal("server was not created")
	}
	if got := repo.created.ExtraHeaders["X-Tenant"]; got != "tenant-a" {
		t.Fatalf("ExtraHeaders[X-Tenant] = %q, want tenant-a", got)
	}
}

func TestUpdateMCPServerReplacesExtraHeaders(t *testing.T) {
	repo := &stubMCPServerRepo{
		server: &domain.MCPServer{
			ID:           "rmcp_1",
			OrgID:        "org_1",
			Name:         "Search",
			URL:          "https://mcp.example/rpc",
			ExtraHeaders: map[string]string{"X-Old": "old"},
		},
	}
	svc := newMCPHeaderTestService(repo)
	headers := map[string]string{"X-New": "new"}

	_, err := svc.UpdateMCPServer(authDomain.WithOrgID(context.Background(), "org_1"), "rmcp_1", application.UpdateMCPServerInput{
		ExtraHeaders: &headers,
	})
	if err != nil {
		t.Fatalf("UpdateMCPServer returned error: %v", err)
	}
	if got := repo.server.ExtraHeaders["X-New"]; got != "new" {
		t.Fatalf("ExtraHeaders[X-New] = %q, want new", got)
	}
	if _, exists := repo.server.ExtraHeaders["X-Old"]; exists {
		t.Fatalf("old header was preserved: %+v", repo.server.ExtraHeaders)
	}
}

func TestMCPServerResponseRedactsExtraHeaderValues(t *testing.T) {
	repo := &stubMCPServerRepo{}
	svc := newMCPHeaderTestService(repo)

	resp, err := svc.CreateMCPServer(authDomain.WithOrgID(context.Background(), "org_1"), application.CreateMCPServerInput{
		Name:         "Search",
		URL:          "https://mcp.example/rpc",
		ExtraHeaders: map[string]string{"Authorization": "secret-token", "X-Tenant": "tenant-a"},
	})
	if err != nil {
		t.Fatalf("CreateMCPServer returned error: %v", err)
	}

	if got := repo.created.ExtraHeaders["Authorization"]; got != "secret-token" {
		t.Fatalf("stored Authorization header = %q, want original secret", got)
	}
	if got := resp.ExtraHeaders["Authorization"]; got != "<redacted>" {
		t.Fatalf("response Authorization header = %q, want <redacted>", got)
	}
	if got := resp.ExtraHeaders["X-Tenant"]; got != "<redacted>" {
		t.Fatalf("response X-Tenant header = %q, want <redacted>", got)
	}
}

func newMCPHeaderTestService(repo domain.MCPServerRepository) application.Service {
	return application.NewService(
		&stubModelRepo{},
		&stubConfigRepo{},
		&stubRotationRepo{},
		&stubConvRepo{},
		&stubJobRepo{},
		&stubProxy{},
		&stubProcessor{},
		&stubDispatcher{},
		application.NewModelEventBus(),
		application.NewInferenceEventBus(),
		repo,
		application.NewMCPServerEventBus(),
	)
}
