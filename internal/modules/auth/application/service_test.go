package application_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"hyperstrate/server/internal/modules/auth/application"
	"hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/modules/auth/infrastructure/vault"
)

// stubUsageQuerier returns configurable usage totals for testing.
type stubUsageQuerier struct {
	requests int64
	costUSD  float64
}

func (q *stubUsageQuerier) SumCostByPeriod(_, _, _, _ string, _ time.Time) (int64, float64, error) {
	return q.requests, q.costUSD, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func hashKey(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

func strPtr(s string) *string { return &s }

const (
	testOrgID    = "org_test"
	testRouterID = "rtr_test"
	testKeyPlain = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
)

// ── stub repos ────────────────────────────────────────────────────────────────

type stubOrgRepo struct {
	count int64
	orgs  map[string]*domain.Organization
}

func (r *stubOrgRepo) Count(_ context.Context) (int64, error) { return r.count, nil }
func (r *stubOrgRepo) Create(_ context.Context, o *domain.Organization) error {
	if r.orgs == nil {
		r.orgs = map[string]*domain.Organization{}
	}
	r.orgs[o.ID] = o
	r.count++
	return nil
}
func (r *stubOrgRepo) FindByID(_ context.Context, id string) (*domain.Organization, error) {
	if o, ok := r.orgs[id]; ok {
		return o, nil
	}
	return nil, domain.ErrOrganizationNotFound
}
func (r *stubOrgRepo) FindBySlug(_ context.Context, _ string) (*domain.Organization, error) {
	return nil, domain.ErrOrganizationNotFound
}
func (r *stubOrgRepo) List(_ context.Context, _, _ int) ([]domain.Organization, int64, error) {
	return nil, 0, nil
}
func (r *stubOrgRepo) Update(_ context.Context, o *domain.Organization) error {
	if r.orgs != nil {
		r.orgs[o.ID] = o
	}
	return nil
}
func (r *stubOrgRepo) Delete(_ context.Context, id string) error {
	delete(r.orgs, id)
	return nil
}

type stubAPIKeyRepo struct {
	byHash  map[string]*domain.APIKey
	byID    map[string]*domain.APIKey
	updated *domain.APIKey
}

func (r *stubAPIKeyRepo) FindByKeyHash(_ context.Context, hash string) (*domain.APIKey, error) {
	if k, ok := r.byHash[hash]; ok {
		return k, nil
	}
	return nil, errors.New("not found")
}
func (r *stubAPIKeyRepo) FindByID(_ context.Context, _, id string) (*domain.APIKey, error) {
	if k, ok := r.byID[id]; ok {
		return k, nil
	}
	return nil, errors.New("not found")
}
func (r *stubAPIKeyRepo) Update(_ context.Context, k *domain.APIKey) error {
	r.updated = k
	return nil
}
func (r *stubAPIKeyRepo) ListByOrg(_ context.Context, _, _, _ string, _, _ int) ([]domain.APIKey, int64, error) {
	return nil, 0, nil
}
func (r *stubAPIKeyRepo) Create(_ context.Context, k *domain.APIKey) error {
	if r.byHash == nil {
		r.byHash = map[string]*domain.APIKey{}
	}
	r.byHash[k.KeyHash] = k
	return nil
}
func (r *stubAPIKeyRepo) Delete(_ context.Context, _, _ string) error        { return nil }
func (r *stubAPIKeyRepo) DeleteByRouterID(_ context.Context, _ string) error { return nil }

type stubVirtualKeyRepo struct {
	byHash    map[string]*domain.VirtualKey
	byID      map[string]*domain.VirtualKey
	created   *domain.VirtualKey
	reqDelta  int64
	costDelta float64
}

func (r *stubVirtualKeyRepo) FindByKeyHash(_ context.Context, hash string) (*domain.VirtualKey, error) {
	if k, ok := r.byHash[hash]; ok {
		return k, nil
	}
	return nil, domain.ErrVirtualKeyNotFound
}
func (r *stubVirtualKeyRepo) FindByID(_ context.Context, orgID, id string) (*domain.VirtualKey, error) {
	if k, ok := r.byID[id]; ok && k.OrgID == orgID {
		return k, nil
	}
	return nil, domain.ErrVirtualKeyNotFound
}
func (r *stubVirtualKeyRepo) Create(_ context.Context, k *domain.VirtualKey) error {
	r.created = k
	return nil
}
func (r *stubVirtualKeyRepo) Update(_ context.Context, k *domain.VirtualKey) error {
	if r.byID == nil {
		r.byID = map[string]*domain.VirtualKey{}
	}
	r.byID[k.ID] = k
	return nil
}
func (r *stubVirtualKeyRepo) Delete(_ context.Context, _, _ string) error { return nil }
func (r *stubVirtualKeyRepo) IncrementUsage(_ context.Context, _ string, req int64, cost float64) error {
	r.reqDelta += req
	r.costDelta += cost
	return nil
}
func (r *stubVirtualKeyRepo) ResetUsageIfExpired(_ context.Context, _ string, _ domain.ResetPeriod) (bool, error) {
	return false, nil
}
func (r *stubVirtualKeyRepo) List(_ context.Context, _, _, _ string, _, _ int) ([]domain.VirtualKey, int64, error) {
	return nil, 0, nil
}
func (r *stubVirtualKeyRepo) ListByTeamID(_ context.Context, _ string) ([]domain.VirtualKey, error) {
	return nil, nil
}

type teamUsageDelta struct {
	requests int64
	costUSD  float64
}

type stubTeamRepo struct {
	teams      map[string]*domain.Team
	usageDelta map[string]teamUsageDelta
	members    map[string][]string
}

func newStubTeamRepo() *stubTeamRepo {
	return &stubTeamRepo{
		teams:      map[string]*domain.Team{},
		usageDelta: map[string]teamUsageDelta{},
		members:    map[string][]string{},
	}
}

func (r *stubTeamRepo) FindByID(_ context.Context, orgID, id string) (*domain.Team, error) {
	if t, ok := r.teams[id]; ok && t.OrgID == orgID {
		return t, nil
	}
	return nil, domain.ErrTeamNotFound
}
func (r *stubTeamRepo) IncrementTeamUsage(_ context.Context, id string, req int64, cost float64) error {
	d := r.usageDelta[id]
	d.requests += req
	d.costUSD += cost
	r.usageDelta[id] = d
	return nil
}
func (r *stubTeamRepo) Create(_ context.Context, t *domain.Team) error {
	r.teams[t.ID] = t
	return nil
}
func (r *stubTeamRepo) Update(_ context.Context, t *domain.Team) error {
	r.teams[t.ID] = t
	return nil
}
func (r *stubTeamRepo) Delete(_ context.Context, _, id string) error {
	delete(r.teams, id)
	return nil
}
func (r *stubTeamRepo) ListByOrgID(_ context.Context, _, _ string, _, _ int) ([]domain.Team, int64, error) {
	return nil, 0, nil
}
func (r *stubTeamRepo) ListByIDs(_ context.Context, _ string, _ []string) ([]domain.Team, error) {
	return nil, nil
}
func (r *stubTeamRepo) AddMember(_ context.Context, teamID, userID string) error {
	r.members[teamID] = append(r.members[teamID], userID)
	return nil
}
func (r *stubTeamRepo) RemoveMember(_ context.Context, teamID, userID string) error {
	ms := r.members[teamID]
	for i, id := range ms {
		if id == userID {
			r.members[teamID] = append(ms[:i], ms[i+1:]...)
			return nil
		}
	}
	return nil
}
func (r *stubTeamRepo) ListMemberIDs(_ context.Context, teamID string) ([]string, error) {
	return r.members[teamID], nil
}
func (r *stubTeamRepo) ListTeamIDsForUser(_ context.Context, userID string) ([]string, error) {
	var ids []string
	for teamID, members := range r.members {
		for _, memberID := range members {
			if memberID == userID {
				ids = append(ids, teamID)
				break
			}
		}
	}
	return ids, nil
}

type stubUserRepo struct {
	byEmail map[string]*domain.User
	byID    map[string]*domain.User
	updated *domain.User
}

func newStubUserRepo() *stubUserRepo {
	return &stubUserRepo{
		byEmail: map[string]*domain.User{},
		byID:    map[string]*domain.User{},
	}
}

func (r *stubUserRepo) Count(_ context.Context) (int64, error) { return int64(len(r.byEmail)), nil }
func (r *stubUserRepo) Create(_ context.Context, u *domain.User) error {
	r.byEmail[u.Email] = u
	r.byID[u.ID] = u
	return nil
}
func (r *stubUserRepo) FindByEmail(_ context.Context, email string) (*domain.User, error) {
	if u, ok := r.byEmail[email]; ok {
		return u, nil
	}
	return nil, errors.New("user not found")
}
func (r *stubUserRepo) FindByID(_ context.Context, id string) (*domain.User, error) {
	if u, ok := r.byID[id]; ok {
		return u, nil
	}
	return nil, errors.New("user not found")
}
func (r *stubUserRepo) FindByIDInOrg(_ context.Context, orgID, id string) (*domain.User, error) {
	if u, ok := r.byID[id]; ok && u.OrgID == orgID {
		return u, nil
	}
	return nil, domain.ErrUserNotFound
}
func (r *stubUserRepo) ListAll(_ context.Context, _, _ int) ([]domain.User, int64, error) {
	users := make([]domain.User, 0, len(r.byID))
	for _, u := range r.byID {
		users = append(users, *u)
	}
	return users, int64(len(users)), nil
}
func (r *stubUserRepo) ListByOrg(_ context.Context, orgID string, _, _ int) ([]domain.User, int64, error) {
	users := make([]domain.User, 0, len(r.byID))
	for _, u := range r.byID {
		if u.OrgID == orgID {
			users = append(users, *u)
		}
	}
	return users, int64(len(users)), nil
}
func (r *stubUserRepo) Update(_ context.Context, u *domain.User) error {
	r.updated = u
	r.byEmail[u.Email] = u
	r.byID[u.ID] = u
	return nil
}

type stubGroupMappingRepo struct{}

func (r *stubGroupMappingRepo) List(_ context.Context, _ string) ([]domain.OIDCGroupMapping, error) {
	return nil, nil
}
func (r *stubGroupMappingRepo) FindByGroup(_ context.Context, _, _ string) ([]domain.OIDCGroupMapping, error) {
	return nil, nil
}
func (r *stubGroupMappingRepo) Create(_ context.Context, _ *domain.OIDCGroupMapping) error {
	return nil
}
func (r *stubGroupMappingRepo) Delete(_ context.Context, _, _ string) error { return nil }

// ── service builder ───────────────────────────────────────────────────────────

type testDeps struct {
	orgs     *stubOrgRepo
	apiKeys  *stubAPIKeyRepo
	vkeys    *stubVirtualKeyRepo
	teams    *stubTeamRepo
	users    *stubUserRepo
	mappings *stubGroupMappingRepo
	usageQ   application.UsageQuerier
}

func newDeps() *testDeps {
	return &testDeps{
		orgs:     &stubOrgRepo{},
		apiKeys:  &stubAPIKeyRepo{byHash: map[string]*domain.APIKey{}, byID: map[string]*domain.APIKey{}},
		vkeys:    &stubVirtualKeyRepo{byHash: map[string]*domain.VirtualKey{}, byID: map[string]*domain.VirtualKey{}},
		teams:    newStubTeamRepo(),
		users:    newStubUserRepo(),
		mappings: &stubGroupMappingRepo{},
	}
}

func (d *testDeps) build() application.Service {
	return application.NewService(
		d.orgs,
		d.apiKeys,
		d.vkeys,
		d.teams,
		d.users,
		d.mappings,
		vault.NoopProvider{},
		application.ServiceConfig{JWTSecret: []byte("test-secret")},
		d.usageQ,
	)
}

// ── ValidateInferKey: API key path ────────────────────────────────────────────

func TestValidateInferKey_apiKey_globalScope_success(t *testing.T) {
	d := newDeps()
	d.apiKeys.byHash[hashKey(testKeyPlain)] = &domain.APIKey{
		ID: "key_1", OrgID: testOrgID, IsEnabled: true, Scope: domain.APIKeyScopeGlobal,
	}
	ctx, err := d.build().ValidateInferKey(context.Background(), testKeyPlain, testRouterID)
	if err != nil {
		t.Fatalf("want success, got %v", err)
	}
	if got := domain.OrgIDFromContext(ctx); got != testOrgID {
		t.Errorf("orgID = %q, want %q", got, testOrgID)
	}
}

func TestValidateInferKey_apiKey_disabled_returnsKeyDisabled(t *testing.T) {
	d := newDeps()
	d.apiKeys.byHash[hashKey(testKeyPlain)] = &domain.APIKey{IsEnabled: false}
	_, err := d.build().ValidateInferKey(context.Background(), testKeyPlain, testRouterID)
	if !errors.Is(err, domain.ErrKeyDisabled) {
		t.Errorf("want ErrKeyDisabled, got %v", err)
	}
}

func TestCreateAPIKey_missingTeamReturnsTeamRequired(t *testing.T) {
	d := newDeps()
	_, err := d.build().CreateAPIKey(domain.WithOrgID(context.Background(), testOrgID), application.CreateAPIKeyInput{
		Name:  "global",
		Scope: domain.APIKeyScopeGlobal,
	})
	if !errors.Is(err, domain.ErrTeamRequired) {
		t.Fatalf("want ErrTeamRequired, got %v", err)
	}
}

func TestValidateInferKey_apiKey_expired_returnsKeyExpired(t *testing.T) {
	d := newDeps()
	past := time.Now().Add(-time.Hour)
	d.apiKeys.byHash[hashKey(testKeyPlain)] = &domain.APIKey{IsEnabled: true, ExpiresAt: &past}
	_, err := d.build().ValidateInferKey(context.Background(), testKeyPlain, testRouterID)
	if !errors.Is(err, domain.ErrKeyExpired) {
		t.Errorf("want ErrKeyExpired, got %v", err)
	}
}

func TestValidateInferKey_apiKey_routerScope_wrongRouter_returnsScopeViolation(t *testing.T) {
	d := newDeps()
	d.apiKeys.byHash[hashKey(testKeyPlain)] = &domain.APIKey{
		IsEnabled: true, Scope: domain.APIKeyScopeRouter, RouterID: "rtr_other",
	}
	_, err := d.build().ValidateInferKey(context.Background(), testKeyPlain, testRouterID)
	if !errors.Is(err, domain.ErrScopeViolation) {
		t.Errorf("want ErrScopeViolation, got %v", err)
	}
}

func TestValidateInferKey_apiKey_routerScope_rightRouter_success(t *testing.T) {
	d := newDeps()
	d.apiKeys.byHash[hashKey(testKeyPlain)] = &domain.APIKey{
		ID: "key_1", OrgID: testOrgID, IsEnabled: true, Scope: domain.APIKeyScopeRouter, RouterID: testRouterID,
	}
	_, err := d.build().ValidateInferKey(context.Background(), testKeyPlain, testRouterID)
	if err != nil {
		t.Errorf("want success, got %v", err)
	}
}

// ── ValidateInferKey: virtual key path ───────────────────────────────────────

func TestValidateInferKey_virtualKey_disabled_returnsKeyDisabled(t *testing.T) {
	d := newDeps()
	d.vkeys.byHash[hashKey(testKeyPlain)] = &domain.VirtualKey{
		OrgID: testOrgID, RouterID: testRouterID, IsEnabled: false,
	}
	_, err := d.build().ValidateInferKey(context.Background(), testKeyPlain, testRouterID)
	if !errors.Is(err, domain.ErrKeyDisabled) {
		t.Errorf("want ErrKeyDisabled, got %v", err)
	}
}

func TestValidateInferKey_virtualKey_wrongRouter_returnsScopeViolation(t *testing.T) {
	d := newDeps()
	d.vkeys.byHash[hashKey(testKeyPlain)] = &domain.VirtualKey{
		OrgID: testOrgID, RouterID: "rtr_other", IsEnabled: true,
	}
	_, err := d.build().ValidateInferKey(context.Background(), testKeyPlain, testRouterID)
	if !errors.Is(err, domain.ErrScopeViolation) {
		t.Errorf("want ErrScopeViolation, got %v", err)
	}
}

func TestValidateInferKey_virtualKey_maxRequestsExceeded_returnsBudgetExceeded(t *testing.T) {
	d := newDeps()
	d.vkeys.byHash[hashKey(testKeyPlain)] = &domain.VirtualKey{
		OrgID: testOrgID, RouterID: testRouterID, IsEnabled: true,
		MaxRequests: 10,
	}
	d.usageQ = &stubUsageQuerier{requests: 10}
	_, err := d.build().ValidateInferKey(context.Background(), testKeyPlain, testRouterID)
	if !errors.Is(err, domain.ErrBudgetExceeded) {
		t.Errorf("want ErrBudgetExceeded, got %v", err)
	}
}

func TestValidateInferKey_virtualKey_maxCostExceeded_returnsBudgetExceeded(t *testing.T) {
	d := newDeps()
	d.vkeys.byHash[hashKey(testKeyPlain)] = &domain.VirtualKey{
		OrgID: testOrgID, RouterID: testRouterID, IsEnabled: true,
		MaxCostUSD: 5.0,
	}
	d.usageQ = &stubUsageQuerier{costUSD: 5.0}
	_, err := d.build().ValidateInferKey(context.Background(), testKeyPlain, testRouterID)
	if !errors.Is(err, domain.ErrBudgetExceeded) {
		t.Errorf("want ErrBudgetExceeded, got %v", err)
	}
}

func TestValidateInferKey_virtualKey_teamDisabled_returnsTeamDisabled(t *testing.T) {
	const teamID = "team_1"
	d := newDeps()
	d.vkeys.byHash[hashKey(testKeyPlain)] = &domain.VirtualKey{
		OrgID: testOrgID, RouterID: testRouterID, IsEnabled: true, TeamID: strPtr(teamID),
	}
	d.teams.teams[teamID] = &domain.Team{ID: teamID, OrgID: testOrgID, IsEnabled: false}
	_, err := d.build().ValidateInferKey(context.Background(), testKeyPlain, testRouterID)
	if !errors.Is(err, domain.ErrTeamDisabled) {
		t.Errorf("want ErrTeamDisabled, got %v", err)
	}
}

func TestValidateInferKey_virtualKey_teamRequestBudgetExceeded_returnsTeamBudgetExceeded(t *testing.T) {
	const teamID = "team_1"
	d := newDeps()
	d.vkeys.byHash[hashKey(testKeyPlain)] = &domain.VirtualKey{
		OrgID: testOrgID, RouterID: testRouterID, IsEnabled: true, TeamID: strPtr(teamID),
	}
	d.teams.teams[teamID] = &domain.Team{
		ID: teamID, OrgID: testOrgID, IsEnabled: true, MaxRequests: 100,
	}
	d.usageQ = &stubUsageQuerier{requests: 100}
	_, err := d.build().ValidateInferKey(context.Background(), testKeyPlain, testRouterID)
	if !errors.Is(err, domain.ErrTeamBudgetExceeded) {
		t.Errorf("want ErrTeamBudgetExceeded, got %v", err)
	}
}

func TestValidateInferKey_virtualKey_teamCostBudgetExceeded_returnsTeamBudgetExceeded(t *testing.T) {
	const teamID = "team_1"
	d := newDeps()
	d.vkeys.byHash[hashKey(testKeyPlain)] = &domain.VirtualKey{
		OrgID: testOrgID, RouterID: testRouterID, IsEnabled: true, TeamID: strPtr(teamID),
	}
	d.teams.teams[teamID] = &domain.Team{
		ID: teamID, OrgID: testOrgID, IsEnabled: true, MaxCostUSD: 10.0,
	}
	d.usageQ = &stubUsageQuerier{costUSD: 10.0}
	_, err := d.build().ValidateInferKey(context.Background(), testKeyPlain, testRouterID)
	if !errors.Is(err, domain.ErrTeamBudgetExceeded) {
		t.Errorf("want ErrTeamBudgetExceeded, got %v", err)
	}
}

func TestValidateInferKey_virtualKey_embeds_vkAndTeamIDInContext(t *testing.T) {
	const teamID = "team_1"
	d := newDeps()
	d.vkeys.byHash[hashKey(testKeyPlain)] = &domain.VirtualKey{
		ID: "vkey_1", OrgID: testOrgID, RouterID: testRouterID, IsEnabled: true, TeamID: strPtr(teamID),
	}
	d.teams.teams[teamID] = &domain.Team{ID: teamID, OrgID: testOrgID, IsEnabled: true}
	ctx, err := d.build().ValidateInferKey(context.Background(), testKeyPlain, testRouterID)
	if err != nil {
		t.Fatalf("want success, got %v", err)
	}
	vkID, gotTeam := domain.VirtualKeyIDFromContext(ctx)
	if vkID != "vkey_1" {
		t.Errorf("vkID = %q, want vkey_1", vkID)
	}
	if gotTeam != teamID {
		t.Errorf("teamID = %q, want %q", gotTeam, teamID)
	}
}

func TestValidateInferKey_unknownKey_returnsUnauthorized(t *testing.T) {
	d := newDeps()
	_, err := d.build().ValidateInferKey(context.Background(), testKeyPlain, testRouterID)
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("want ErrUnauthorized, got %v", err)
	}
}

// ── SetupStatus / Setup ───────────────────────────────────────────────────────

func TestSetupStatus_noOrgs_required(t *testing.T) {
	d := newDeps()
	status, err := d.build().SetupStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Required {
		t.Error("want Required=true when no orgs exist")
	}
}

func TestSetupStatus_hasOrgs_notRequired(t *testing.T) {
	d := newDeps()
	d.orgs.count = 1
	status, err := d.build().SetupStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Required {
		t.Error("want Required=false when orgs exist")
	}
}

func TestSetup_success_createsOrgAndAssignsAdmin(t *testing.T) {
	const callerEmail = "admin@example.com"
	d := newDeps()
	d.users.byEmail[callerEmail] = &domain.User{ID: "usr_1", Email: callerEmail, Role: domain.UserRoleMember}

	resp, err := d.build().Setup(context.Background(), callerEmail, application.SetupInput{OrgName: "Acme Corp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Name != "Acme Corp" {
		t.Errorf("org name = %q, want Acme Corp", resp.Name)
	}
	if resp.Slug != "acme-corp" {
		t.Errorf("slug = %q, want acme-corp", resp.Slug)
	}
	if d.users.updated == nil {
		t.Fatal("expected user to be updated")
	}
	if d.users.updated.Role != domain.UserRoleAdmin {
		t.Errorf("user role = %q, want admin", d.users.updated.Role)
	}
	if d.users.updated.OrgID == "" {
		t.Error("expected user OrgID to be set")
	}
}

func TestSetup_alreadyDone_returnsError(t *testing.T) {
	d := newDeps()
	d.orgs.count = 1
	_, err := d.build().Setup(context.Background(), "admin@example.com", application.SetupInput{OrgName: "X"})
	if !errors.Is(err, domain.ErrSetupAlreadyDone) {
		t.Errorf("want ErrSetupAlreadyDone, got %v", err)
	}
}

// ── CreateVirtualKey ──────────────────────────────────────────────────────────

func TestCreateVirtualKey_noTeam_storesNilTeamID(t *testing.T) {
	d := newDeps()
	ctx := domain.WithOrgID(context.Background(), testOrgID)
	_, err := d.build().CreateVirtualKey(ctx, application.CreateVirtualKeyInput{
		RouterID: testRouterID, Name: "test key", TeamID: "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.vkeys.created == nil {
		t.Fatal("expected VirtualKey to be created")
	}
	if d.vkeys.created.TeamID != nil {
		t.Errorf("TeamID = %q, want nil", *d.vkeys.created.TeamID)
	}
}

func TestCreateVirtualKey_withTeam_storesTeamIDPointer(t *testing.T) {
	const teamID = "team_1"
	d := newDeps()
	d.teams.teams[teamID] = &domain.Team{ID: teamID, OrgID: testOrgID}
	ctx := domain.WithOrgID(context.Background(), testOrgID)
	_, err := d.build().CreateVirtualKey(ctx, application.CreateVirtualKeyInput{
		RouterID: testRouterID, Name: "test key", TeamID: teamID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.vkeys.created == nil || d.vkeys.created.TeamID == nil {
		t.Fatal("expected non-nil TeamID pointer")
	}
	if *d.vkeys.created.TeamID != teamID {
		t.Errorf("TeamID = %q, want %q", *d.vkeys.created.TeamID, teamID)
	}
}

func TestCreateVirtualKey_teamNotFound_returnsError(t *testing.T) {
	d := newDeps()
	ctx := domain.WithOrgID(context.Background(), testOrgID)
	_, err := d.build().CreateVirtualKey(ctx, application.CreateVirtualKeyInput{
		RouterID: testRouterID, Name: "test key", TeamID: "team_nonexistent",
	})
	if err == nil {
		t.Error("expected error for unknown team, got nil")
	}
}

func TestCreateVirtualKey_setsOrgIDFromContext(t *testing.T) {
	d := newDeps()
	ctx := domain.WithOrgID(context.Background(), testOrgID)
	_, err := d.build().CreateVirtualKey(ctx, application.CreateVirtualKeyInput{
		RouterID: testRouterID, Name: "test key",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.vkeys.created.OrgID != testOrgID {
		t.Errorf("OrgID = %q, want %q", d.vkeys.created.OrgID, testOrgID)
	}
}

// ── UpdateVirtualKey ──────────────────────────────────────────────────────────

func TestUpdateVirtualKey_partialUpdate_onlyChangesSpecifiedFields(t *testing.T) {
	d := newDeps()
	d.vkeys.byID["vkey_1"] = &domain.VirtualKey{
		ID: "vkey_1", OrgID: testOrgID, RouterID: testRouterID,
		Name: "original", MaxRequests: 100, IsEnabled: true,
	}
	ctx := domain.WithOrgID(context.Background(), testOrgID)
	newName := "updated"
	resp, err := d.build().UpdateVirtualKey(ctx, "vkey_1", application.UpdateVirtualKeyInput{Name: &newName})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Name != "updated" {
		t.Errorf("Name = %q, want updated", resp.Name)
	}
	if resp.MaxRequests != 100 {
		t.Errorf("MaxRequests changed: got %d, want 100", resp.MaxRequests)
	}
}

func TestUpdateVirtualKey_notFound_returnsError(t *testing.T) {
	d := newDeps()
	ctx := domain.WithOrgID(context.Background(), testOrgID)
	_, err := d.build().UpdateVirtualKey(ctx, "vkey_nonexistent", application.UpdateVirtualKeyInput{})
	if !errors.Is(err, domain.ErrVirtualKeyNotFound) {
		t.Errorf("want ErrVirtualKeyNotFound, got %v", err)
	}
}

// ── CreateTeam ────────────────────────────────────────────────────────────────

func TestCreateTeam_setsOrgIDFromContext(t *testing.T) {
	d := newDeps()
	ctx := domain.WithOrgID(context.Background(), testOrgID)
	resp, err := d.build().CreateTeam(ctx, application.CreateTeamInput{Name: "Engineering"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OrgID != testOrgID {
		t.Errorf("OrgID = %q, want %q", resp.OrgID, testOrgID)
	}
	if resp.Name != "Engineering" {
		t.Errorf("Name = %q, want Engineering", resp.Name)
	}
}

// ── Team membership ───────────────────────────────────────────────────────────

func TestAddTeamMember_teamNotFound_returnsError(t *testing.T) {
	d := newDeps()
	ctx := domain.WithOrgID(context.Background(), testOrgID)
	err := d.build().AddTeamMember(ctx, "team_nonexistent", "usr_1")
	if err == nil {
		t.Error("expected error for unknown team")
	}
}

func TestAddTeamMember_userNotFound_returnsError(t *testing.T) {
	const teamID = "team_1"
	d := newDeps()
	d.teams.teams[teamID] = &domain.Team{ID: teamID, OrgID: testOrgID}
	ctx := domain.WithOrgID(context.Background(), testOrgID)
	err := d.build().AddTeamMember(ctx, teamID, "usr_nonexistent")
	if err == nil {
		t.Error("expected error for unknown user")
	}
}

func TestAddTeamMember_success(t *testing.T) {
	const teamID = "team_1"
	d := newDeps()
	d.teams.teams[teamID] = &domain.Team{ID: teamID, OrgID: testOrgID}
	d.users.byID["usr_1"] = &domain.User{ID: "usr_1", OrgID: testOrgID}
	ctx := domain.WithOrgID(context.Background(), testOrgID)
	if err := d.build().AddTeamMember(ctx, teamID, "usr_1"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(d.teams.members[teamID]) != 1 || d.teams.members[teamID][0] != "usr_1" {
		t.Error("member not added to team")
	}
}

func TestRemoveTeamMember_teamNotFound_returnsError(t *testing.T) {
	d := newDeps()
	ctx := domain.WithOrgID(context.Background(), testOrgID)
	err := d.build().RemoveTeamMember(ctx, "team_nonexistent", "usr_1")
	if err == nil {
		t.Error("expected error for unknown team")
	}
}

// ── UpdateTeam / DeleteTeam ───────────────────────────────────────────────────

func TestUpdateTeam_partialUpdate_onlyChangesSpecifiedFields(t *testing.T) {
	const teamID = "team_1"
	d := newDeps()
	d.teams.teams[teamID] = &domain.Team{
		ID: teamID, OrgID: testOrgID, Name: "original", MaxRequests: 50, IsEnabled: true,
	}
	ctx := domain.WithOrgID(context.Background(), testOrgID)
	newName := "updated"
	resp, err := d.build().UpdateTeam(ctx, teamID, application.UpdateTeamInput{Name: &newName})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Name != "updated" {
		t.Errorf("Name = %q, want updated", resp.Name)
	}
	if resp.MaxRequests != 50 {
		t.Errorf("MaxRequests changed: got %d, want 50", resp.MaxRequests)
	}
}

func TestUpdateTeam_notFound_returnsError(t *testing.T) {
	d := newDeps()
	ctx := domain.WithOrgID(context.Background(), testOrgID)
	_, err := d.build().UpdateTeam(ctx, "team_nonexistent", application.UpdateTeamInput{})
	if !errors.Is(err, domain.ErrTeamNotFound) {
		t.Errorf("want ErrTeamNotFound, got %v", err)
	}
}

func TestDeleteTeam_success(t *testing.T) {
	const teamID = "team_1"
	d := newDeps()
	d.teams.teams[teamID] = &domain.Team{ID: teamID, OrgID: testOrgID, IsEnabled: true}
	ctx := domain.WithOrgID(context.Background(), testOrgID)
	if err := d.build().DeleteTeam(ctx, teamID); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if _, ok := d.teams.teams[teamID]; ok {
		t.Error("expected team to be deleted")
	}
}

func TestDeleteTeam_notFound_returnsError(t *testing.T) {
	d := newDeps()
	ctx := domain.WithOrgID(context.Background(), testOrgID)
	err := d.build().DeleteTeam(ctx, "team_nonexistent")
	if !errors.Is(err, domain.ErrTeamNotFound) {
		t.Errorf("want ErrTeamNotFound, got %v", err)
	}
}

// ── ValidateSession / RefreshSession ─────────────────────────────────────────

func TestValidateSession_roundtrip_returnsCorrectUser(t *testing.T) {
	d := newDeps()
	svc := d.build()

	// Sign a token by going through RefreshSession.
	d.users.byEmail["alice@example.com"] = &domain.User{
		ID: "usr_alice", Email: "alice@example.com", Name: "Alice",
		Role: domain.UserRoleAdmin, OrgID: testOrgID,
	}
	d.teams.members["team_1"] = []string{"usr_alice"}
	session, err := svc.RefreshSession(context.Background(), "alice@example.com")
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}

	// Now validate the token we just received.
	user, err := svc.ValidateSession(session.Token)
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if user.Email != "alice@example.com" {
		t.Errorf("Email = %q, want alice@example.com", user.Email)
	}
	if user.Role != domain.UserRoleAdmin {
		t.Errorf("Role = %q, want admin", user.Role)
	}
	if user.OrgID != testOrgID {
		t.Errorf("OrgID = %q, want %q", user.OrgID, testOrgID)
	}
	if len(user.TeamIDs) != 1 || user.TeamIDs[0] != "team_1" {
		t.Errorf("TeamIDs = %#v, want [team_1]", user.TeamIDs)
	}
}

func TestValidateSession_tamperedSignature_returnsSessionInvalid(t *testing.T) {
	d := newDeps()
	svc := d.build()
	d.users.byEmail["bob@example.com"] = &domain.User{
		ID: "usr_bob", Email: "bob@example.com", Role: domain.UserRoleMember,
	}
	session, err := svc.RefreshSession(context.Background(), "bob@example.com")
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}

	tampered := session.Token + "x"
	_, err = svc.ValidateSession(tampered)
	if !errors.Is(err, domain.ErrSessionInvalid) {
		t.Errorf("want ErrSessionInvalid for tampered token, got %v", err)
	}
}

func TestValidateSession_wrongSecret_returnsSessionInvalid(t *testing.T) {
	// Token signed with one secret must not validate under a different secret.
	d1 := newDeps()
	svc1 := application.NewService(
		d1.orgs, d1.apiKeys, d1.vkeys, d1.teams, d1.users, d1.mappings,
		nil, application.ServiceConfig{JWTSecret: []byte("secret-A")}, nil,
	)
	d1.users.byEmail["carol@example.com"] = &domain.User{
		ID: "usr_carol", Email: "carol@example.com", Role: domain.UserRoleMember,
	}
	session, err := svc1.RefreshSession(context.Background(), "carol@example.com")
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}

	d2 := newDeps()
	svc2 := application.NewService(
		d2.orgs, d2.apiKeys, d2.vkeys, d2.teams, d2.users, d2.mappings,
		nil, application.ServiceConfig{JWTSecret: []byte("secret-B")}, nil,
	)
	_, err = svc2.ValidateSession(session.Token)
	if !errors.Is(err, domain.ErrSessionInvalid) {
		t.Errorf("want ErrSessionInvalid for wrong secret, got %v", err)
	}
}

func TestValidateSession_malformedToken_returnsSessionInvalid(t *testing.T) {
	d := newDeps()
	svc := d.build()
	_, err := svc.ValidateSession("not-a-valid-token")
	if !errors.Is(err, domain.ErrSessionInvalid) {
		t.Errorf("want ErrSessionInvalid, got %v", err)
	}
}

func TestRefreshSession_userNotFound_returnsError(t *testing.T) {
	d := newDeps()
	svc := d.build()
	_, err := svc.RefreshSession(context.Background(), "nobody@example.com")
	if !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("want ErrUserNotFound, got %v", err)
	}
}

// ── UpdateUser ────────────────────────────────────────────────────────────────

func TestUpdateUser_roleAndOrgID_changesApplied(t *testing.T) {
	d := newDeps()
	d.users.byID["usr_1"] = &domain.User{
		ID: "usr_1", Email: "x@x.com", Role: domain.UserRoleMember, OrgID: "",
	}
	svc := d.build()

	role := domain.UserRoleAdmin
	newOrg := testOrgID
	resp, err := svc.UpdateUser(context.Background(), "usr_1", application.UpdateUserInput{
		Role: &role, OrgID: &newOrg,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Role != domain.UserRoleAdmin {
		t.Errorf("Role = %q, want admin", resp.Role)
	}
	if resp.OrgID != testOrgID {
		t.Errorf("OrgID = %q, want %q", resp.OrgID, testOrgID)
	}
}

func TestUpdateUser_notFound_returnsError(t *testing.T) {
	d := newDeps()
	svc := d.build()
	_, err := svc.UpdateUser(context.Background(), "usr_nonexistent", application.UpdateUserInput{})
	if err == nil {
		t.Error("expected error for unknown user, got nil")
	}
}

// ── RevokeVirtualKey ──────────────────────────────────────────────────────────

func TestRevokeVirtualKey_success_removesKey(t *testing.T) {
	d := newDeps()
	d.vkeys.byID["vkey_1"] = &domain.VirtualKey{ID: "vkey_1", OrgID: testOrgID}
	ctx := domain.WithOrgID(context.Background(), testOrgID)
	if err := d.build().RevokeVirtualKey(ctx, "vkey_1"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRevokeVirtualKey_notFound_returnsError(t *testing.T) {
	d := newDeps()
	ctx := domain.WithOrgID(context.Background(), testOrgID)
	err := d.build().RevokeVirtualKey(ctx, "vkey_nonexistent")
	if !errors.Is(err, domain.ErrVirtualKeyNotFound) {
		t.Errorf("want ErrVirtualKeyNotFound, got %v", err)
	}
}
