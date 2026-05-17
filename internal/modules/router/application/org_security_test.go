package application

// Org-isolation security tests for the router service.
//
// Uses org-aware stubs that mirror GORM behaviour: FindByID and List return
// ErrRouterNotFound / empty results when the caller's org does not own the
// resource. Tests verify the service correctly threads orgID from context
// through every repository call.

import (
	"context"
	"testing"

	authDomain "hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/modules/router/domain"
	"hyperstrate/server/internal/shared/pagination"
)

// ── org-aware router repo stub ────────────────────────────────────────────────

type orgRouterRepo struct {
	router *domain.Router
}

func (r *orgRouterRepo) List(_ context.Context, orgID, _ string, offset, limit int) ([]domain.Router, int64, error) {
	if r.router == nil || r.router.OrgID != orgID {
		return nil, 0, nil
	}
	return []domain.Router{*r.router}, 1, nil
}

func (r *orgRouterRepo) Create(_ context.Context, router *domain.Router) error {
	r.router = router
	return nil
}

func (r *orgRouterRepo) FindByID(_ context.Context, orgID, id string) (*domain.Router, error) {
	if r.router == nil || r.router.OrgID != orgID || r.router.ID != id {
		return nil, domain.ErrRouterNotFound
	}
	return r.router, nil
}

func (r *orgRouterRepo) Update(_ context.Context, router *domain.Router) error {
	r.router = router
	return nil
}

func (r *orgRouterRepo) Delete(_ context.Context, orgID, id string) error {
	if r.router == nil || r.router.OrgID != orgID || r.router.ID != id {
		return domain.ErrRouterNotFound
	}
	r.router = nil
	return nil
}

func (r *orgRouterRepo) NullifyPromptID(_ context.Context, _ string) error { return nil }

// ── passthrough stubs for unused repos ───────────────────────────────────────

type noopTargetRepo struct{}

func (r *noopTargetRepo) ListByRouterID(_ context.Context, _, _ string) ([]domain.RouterTarget, error) {
	return nil, nil
}
func (r *noopTargetRepo) Create(_ context.Context, _ *domain.RouterTarget) error { return nil }
func (r *noopTargetRepo) FindByID(_ context.Context, _, _ string) (*domain.RouterTarget, error) {
	return nil, domain.ErrRouterTargetNotFound
}
func (r *noopTargetRepo) Update(_ context.Context, _ *domain.RouterTarget) error { return nil }
func (r *noopTargetRepo) Delete(_ context.Context, _, _ string) error            { return nil }
func (r *noopTargetRepo) DeleteByRouterID(_ context.Context, _, _ string) error  { return nil }
func (r *noopTargetRepo) DeleteByModelID(_ context.Context, _, _ string) error   { return nil }
func (r *noopTargetRepo) ListByModelID(_ context.Context, _, _ string) ([]domain.RouterTarget, error) {
	return nil, nil
}
func (r *noopTargetRepo) NullifyPromptID(_ context.Context, _ string) error { return nil }

type staticTargetRepo struct {
	noopTargetRepo
	targets []domain.RouterTarget
}

func (r *staticTargetRepo) ListByRouterID(_ context.Context, _, _ string) ([]domain.RouterTarget, error) {
	return r.targets, nil
}

type noopFeatureRepo struct{}

func (r *noopFeatureRepo) ListByRouterID(_ context.Context, _, _ string) ([]domain.RouterFeature, error) {
	return nil, nil
}
func (r *noopFeatureRepo) Create(_ context.Context, _ *domain.RouterFeature) error { return nil }
func (r *noopFeatureRepo) FindByID(_ context.Context, _, _ string) (*domain.RouterFeature, error) {
	return nil, domain.ErrRouterFeatureNotFound
}
func (r *noopFeatureRepo) Update(_ context.Context, _ *domain.RouterFeature) error { return nil }
func (r *noopFeatureRepo) Delete(_ context.Context, _, _ string) error             { return nil }
func (r *noopFeatureRepo) DeleteByRouterID(_ context.Context, _, _ string) error   { return nil }
func (r *noopFeatureRepo) RemoveMCPServerID(_ context.Context, _, _ string) error  { return nil }

type noopInterceptorRepo struct{}

func (r *noopInterceptorRepo) ListByRouterID(_ context.Context, _, _ string) ([]domain.RouterInterceptor, error) {
	return nil, nil
}
func (r *noopInterceptorRepo) Create(_ context.Context, _ *domain.RouterInterceptor) error {
	return nil
}
func (r *noopInterceptorRepo) FindByID(_ context.Context, _, _ string) (*domain.RouterInterceptor, error) {
	return nil, domain.ErrRouterInterceptorNotFound
}
func (r *noopInterceptorRepo) Update(_ context.Context, _ *domain.RouterInterceptor) error {
	return nil
}
func (r *noopInterceptorRepo) Delete(_ context.Context, _, _ string) error           { return nil }
func (r *noopInterceptorRepo) DeleteByRouterID(_ context.Context, _, _ string) error { return nil }

type noopInferencer struct{}

func (n *noopInferencer) InferModel(_ context.Context, _ string, _ map[string]string, _ map[string]any) (*ModelInferResult, error) {
	return &ModelInferResult{Content: "ok"}, nil
}
func (n *noopInferencer) InferModelStream(_ context.Context, _ string, _ map[string]string, _ map[string]any) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk)
	close(ch)
	return ch, nil
}

type staticEmbedder struct{}

func (e *staticEmbedder) Embed(_ context.Context, _ string, _ string) ([]float32, error) {
	return []float32{1, 0}, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

const (
	routerOrgA = "org-alpha"
	routerOrgB = "org-bravo"
)

func routerCtx(orgID string) context.Context {
	return authDomain.WithOrgID(context.Background(), orgID)
}

func routerOwnedByOrgA() *domain.Router {
	return &domain.Router{
		ID:       "rtr_a1",
		OrgID:    routerOrgA,
		Name:     "router-a",
		Strategy: domain.RoutingStrategyRoundRobin,
		Status:   domain.RouterStatusActive,
	}
}

func buildRouterService(routerRepo domain.RouterRepository) Service {
	return buildRouterServiceWithDeps(
		routerRepo,
		&noopTargetRepo{},
		&noopAccessRepo{},
		nil,
	)
}

func buildRouterServiceWithDeps(
	routerRepo domain.RouterRepository,
	targetRepo domain.RouterTargetRepository,
	accessRepo domain.RouterTeamAccessRepository,
	embedder EmbeddingProvider,
) Service {
	return NewService(
		routerRepo,
		targetRepo,
		&noopFeatureRepo{},
		&noopInterceptorRepo{},
		accessRepo,
		&noopInferencer{},
		embedder,
		nil, // cacheStore — nil uses memory fallback
		NewRouterInferenceEventBus(),
		NewRouterTargetEventBus(),
		nil, // promptLoader — nil, not needed for CRUD tests
		nil, // healthChecker — nil, feature disabled in these tests
		nil, // mcpLoader — nil, not needed for CRUD tests
		nil, // budgetQuerier — nil, not needed for CRUD tests
		nil, // modelLookup — nil, not needed for CRUD tests
		nil, // evalRepo — nil, not needed for CRUD tests
		nil, // evalCaseRepo — nil, not needed for CRUD tests
		nil, // evalRunRepo — nil, not needed for CRUD tests
	)
}

type noopAccessRepo struct{}

func (r *noopAccessRepo) ListByRouterID(_ context.Context, _ string) ([]domain.RouterTeamAccess, error) {
	return nil, nil
}
func (r *noopAccessRepo) IsTeamAllowed(_ context.Context, _, _ string) (bool, error) {
	return true, nil
}
func (r *noopAccessRepo) Grant(_ context.Context, _ *domain.RouterTeamAccess) error { return nil }
func (r *noopAccessRepo) Revoke(_ context.Context, _, _ string) error               { return nil }
func (r *noopAccessRepo) DeleteByRouterID(_ context.Context, _ string) error        { return nil }

type restrictedAccessRepo struct {
	allowed map[string]bool
}

func (r *restrictedAccessRepo) ListByRouterID(_ context.Context, routerID string) ([]domain.RouterTeamAccess, error) {
	rows := make([]domain.RouterTeamAccess, 0, len(r.allowed))
	for teamID := range r.allowed {
		rows = append(rows, domain.RouterTeamAccess{RouterID: routerID, TeamID: teamID})
	}
	return rows, nil
}

func (r *restrictedAccessRepo) IsTeamAllowed(_ context.Context, _, teamID string) (bool, error) {
	if len(r.allowed) == 0 {
		return true, nil
	}
	return r.allowed[teamID], nil
}

func (r *restrictedAccessRepo) Grant(_ context.Context, _ *domain.RouterTeamAccess) error { return nil }
func (r *restrictedAccessRepo) Revoke(_ context.Context, _, _ string) error               { return nil }
func (r *restrictedAccessRepo) DeleteByRouterID(_ context.Context, _ string) error        { return nil }

// ── tests ─────────────────────────────────────────────────────────────────────

func TestOrgIsolation_GetRouter_crossOrgReturnsNotFound(t *testing.T) {
	svc := buildRouterService(&orgRouterRepo{router: routerOwnedByOrgA()})

	_, err := svc.GetRouter(routerCtx(routerOrgB), "rtr_a1")
	if err != domain.ErrRouterNotFound {
		t.Errorf("want ErrRouterNotFound when org-B reads org-A router, got %v", err)
	}
}

func TestOrgIsolation_GetRouter_ownerOrgSucceeds(t *testing.T) {
	svc := buildRouterService(&orgRouterRepo{router: routerOwnedByOrgA()})

	r, err := svc.GetRouter(routerCtx(routerOrgA), "rtr_a1")
	if err != nil {
		t.Fatalf("want success for owner org, got %v", err)
	}
	if r.ID != "rtr_a1" {
		t.Errorf("want router ID rtr_a1, got %s", r.ID)
	}
}

func TestOrgIsolation_UpdateRouter_crossOrgReturnsNotFound(t *testing.T) {
	svc := buildRouterService(&orgRouterRepo{router: routerOwnedByOrgA()})

	name := "hijacked"
	_, err := svc.UpdateRouter(routerCtx(routerOrgB), "rtr_a1", UpdateRouterInput{Name: &name})
	if err != domain.ErrRouterNotFound {
		t.Errorf("want ErrRouterNotFound when org-B updates org-A router, got %v", err)
	}
}

func TestOrgIsolation_DeleteRouter_crossOrgReturnsNotFound(t *testing.T) {
	svc := buildRouterService(&orgRouterRepo{router: routerOwnedByOrgA()})

	err := svc.DeleteRouter(routerCtx(routerOrgB), "rtr_a1")
	if err != domain.ErrRouterNotFound {
		t.Errorf("want ErrRouterNotFound when org-B deletes org-A router, got %v", err)
	}
}

func TestOrgIsolation_ListTargets_crossOrgReturnsNotFound(t *testing.T) {
	svc := buildRouterService(&orgRouterRepo{router: routerOwnedByOrgA()})

	// org-B tries to list targets of org-A's router — the router lookup gates this
	_, err := svc.ListTargets(routerCtx(routerOrgB), "rtr_a1")
	if err != domain.ErrRouterNotFound {
		t.Errorf("want ErrRouterNotFound when org-B lists targets of org-A router, got %v", err)
	}
}

func TestOrgIsolation_ListRouters_onlyReturnsCalling(t *testing.T) {
	svc := buildRouterService(&orgRouterRepo{router: routerOwnedByOrgA()})

	// org-A sees its own router
	resultA, err := svc.ListRouters(routerCtx(routerOrgA), pagination.Slice{Page: 1, PerPage: 10}, "")
	if err != nil {
		t.Fatal(err)
	}
	if resultA.Meta.Total != 1 {
		t.Errorf("want org-A to see 1 router, got %d", resultA.Meta.Total)
	}

	// org-B sees nothing
	resultB, err := svc.ListRouters(routerCtx(routerOrgB), pagination.Slice{Page: 1, PerPage: 10}, "")
	if err != nil {
		t.Fatal(err)
	}
	if resultB.Meta.Total != 0 {
		t.Errorf("want org-B to see 0 routers (cross-org), got %d", resultB.Meta.Total)
	}
}

func TestOrgIsolation_CreateRouter_setsOrgIDFromContext(t *testing.T) {
	repo := &orgRouterRepo{}
	svc := buildRouterService(repo)

	_, err := svc.CreateRouter(routerCtx(routerOrgA), CreateRouterInput{
		Name:     "my-router",
		Strategy: domain.RoutingStrategyRoundRobin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repo.router == nil {
		t.Fatal("want router created, got nil")
	}
	if repo.router.OrgID != routerOrgA {
		t.Errorf("want OrgID %q set on new router, got %q", routerOrgA, repo.router.OrgID)
	}
}

func TestOrgIsolation_GetBudgetStatus_crossOrgReturnsNotFound(t *testing.T) {
	svc := buildRouterService(&orgRouterRepo{router: routerOwnedByOrgA()})

	_, err := svc.GetBudgetStatus(routerCtx(routerOrgB), "rtr_a1")
	if err != domain.ErrRouterNotFound {
		t.Errorf("want ErrRouterNotFound when org-B reads org-A budget status, got %v", err)
	}
}

func TestTeamAccess_noTeamCallerDeniedWhenRouterRestricted(t *testing.T) {
	svc := buildRouterServiceWithDeps(
		&orgRouterRepo{router: routerOwnedByOrgA()},
		&noopTargetRepo{},
		&restrictedAccessRepo{allowed: map[string]bool{"team_allowed": true}},
		nil,
	).(*service)

	err := svc.checkTeamAccess(routerCtx(routerOrgA), "rtr_a1")
	if err != domain.ErrTeamNotAllowed {
		t.Fatalf("want ErrTeamNotAllowed for restricted router without caller team, got %v", err)
	}
}

func TestTeamAccess_allowsMatchingTeam(t *testing.T) {
	svc := buildRouterServiceWithDeps(
		&orgRouterRepo{router: routerOwnedByOrgA()},
		&noopTargetRepo{},
		&restrictedAccessRepo{allowed: map[string]bool{"team_allowed": true}},
		nil,
	).(*service)

	ctx := authDomain.WithCallerTeamID(routerCtx(routerOrgA), "team_allowed")
	if err := svc.checkTeamAccess(ctx, "rtr_a1"); err != nil {
		t.Fatalf("want matching team to pass, got %v", err)
	}
}

func TestRouteEmbed_enforcesTeamAccess(t *testing.T) {
	svc := buildRouterServiceWithDeps(
		&orgRouterRepo{router: routerOwnedByOrgA()},
		&staticTargetRepo{targets: []domain.RouterTarget{{
			ID:        "tgt_1",
			RouterID:  "rtr_a1",
			ModelID:   "mdl_1",
			IsEnabled: true,
		}}},
		&restrictedAccessRepo{allowed: map[string]bool{"team_allowed": true}},
		&staticEmbedder{},
	)

	ctx := authDomain.WithCallerTeamID(routerCtx(routerOrgA), "team_other")
	_, _, err := svc.RouteEmbed(ctx, "rtr_a1", []string{"hello"})
	if err != domain.ErrTeamNotAllowed {
		t.Fatalf("want ErrTeamNotAllowed from embeddings route, got %v", err)
	}
}
