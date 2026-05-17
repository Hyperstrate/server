package application

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/modules/auth/infrastructure/vault"
	"hyperstrate/server/internal/shared/pagination"

	jose "github.com/go-jose/go-jose/v4"
	"go.jetify.com/typeid/v2"
)

// KeyValidator authenticates inbound inference requests.
// The returned context enriches the caller's context with virtual-key metadata
// needed for post-inference cost recording.
type KeyValidator interface {
	ValidateInferKey(ctx context.Context, plaintext, routerID string) (context.Context, error)
}

type userIdentity struct {
	Email  string
	Name   string
	Avatar string
}

// SessionUser is the decoded identity embedded in a session token.
type SessionUser struct {
	ID      string
	Email   string
	Name    string
	Role    domain.UserRole
	OrgID   string
	TeamIDs []string
}

// SessionValidator is the narrow interface middleware uses to verify session tokens.
type SessionValidator interface {
	ValidateSession(token string) (*SessionUser, error)
}

// Service defines all auth module use-cases.
type Service interface {
	KeyValidator
	SessionValidator

	// Setup / onboarding
	SetupStatus(ctx context.Context) (SetupStatusResponse, error)
	Setup(ctx context.Context, callerEmail string, input SetupInput) (*OrganizationResponse, error)

	// Organisations
	ListOrganizations(ctx context.Context, slice pagination.Slice) (pagination.Paginated[OrganizationResponse], error)
	CreateOrganization(ctx context.Context, input CreateOrganizationInput) (*OrganizationResponse, error)
	UpdateOrganization(ctx context.Context, id string, input UpdateOrganizationInput) (*OrganizationResponse, error)
	DeleteOrganization(ctx context.Context, id string) error

	// API Keys
	ListAPIKeys(ctx context.Context, routerID, teamID string, slice pagination.Slice) (pagination.Paginated[APIKeyResponse], error)
	CreateAPIKey(ctx context.Context, input CreateAPIKeyInput) (*APIKeyCreatedResponse, error)
	RevokeAPIKey(ctx context.Context, id string) error

	// Virtual Keys
	ListVirtualKeys(ctx context.Context, routerID, teamID string, slice pagination.Slice) (pagination.Paginated[VirtualKeyResponse], error)
	CreateVirtualKey(ctx context.Context, input CreateVirtualKeyInput) (*VirtualKeyCreatedResponse, error)
	UpdateVirtualKey(ctx context.Context, id string, input UpdateVirtualKeyInput) (*VirtualKeyResponse, error)
	RevokeVirtualKey(ctx context.Context, id string) error

	// Teams
	GetTeam(ctx context.Context, id string) (*TeamResponse, error)
	GetTeamsByIDs(ctx context.Context, ids []string) ([]TeamResponse, error)
	ListTeams(ctx context.Context, slice pagination.Slice, query string) (pagination.Paginated[TeamResponse], error)
	CreateTeam(ctx context.Context, input CreateTeamInput) (*TeamResponse, error)
	UpdateTeam(ctx context.Context, id string, input UpdateTeamInput) (*TeamResponse, error)
	DeleteTeam(ctx context.Context, id string) error
	AddTeamMember(ctx context.Context, teamID, userID string) error
	RemoveTeamMember(ctx context.Context, teamID, userID string) error

	// Virtual key lookup (used by relation enrichment)
	GetVirtualKey(ctx context.Context, id string) (*VirtualKeyResponse, error)

	// Users
	GetMe(ctx context.Context, email string) (*UserResponse, error)
	ListUsers(ctx context.Context, slice pagination.Slice) (pagination.Paginated[UserResponse], error)
	UpdateUser(ctx context.Context, id string, input UpdateUserInput) (*UserResponse, error)

	ExchangeOIDCToken(ctx context.Context, oidcJWT string) (*SessionResponse, error)

	// OIDC group → team mappings
	ListGroupMappings(ctx context.Context) ([]domain.OIDCGroupMapping, error)
	CreateGroupMapping(ctx context.Context, groupName, teamID string) (*domain.OIDCGroupMapping, error)
	DeleteGroupMapping(ctx context.Context, id string) error

	// RefreshSession re-issues a session token for the currently authenticated
	// user, picking up any changes made since the original token was signed
	// (e.g. orgId assigned after setup).
	RefreshSession(ctx context.Context, email string) (*SessionResponse, error)

	// RotateAPIKey generates a new secret for the key, revokes the old one,
	// and returns the new plaintext once.
	RotateAPIKey(ctx context.Context, id string) (*APIKeyCreatedResponse, error)
}

type ServiceConfig struct {
	JWTSecret   []byte
	AdminEmail  string
	OIDCJWKSURL string
}

type jwksCache struct {
	mu        sync.RWMutex
	keys      *jose.JSONWebKeySet
	fetchedAt time.Time
}

// vkRateLimiter is an in-memory token bucket for one virtual key.
// Tokens refill continuously at rate RPS; bucket holds up to RPS tokens.
type vkRateLimiter struct {
	rps      float64
	tokens   float64
	lastTick time.Time
	mu       sync.Mutex
}

func (l *vkRateLimiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(l.lastTick).Seconds()
	l.tokens += elapsed * l.rps
	if l.tokens > l.rps {
		l.tokens = l.rps // cap at burst == rps
	}
	l.lastTick = now
	if l.tokens >= 1 {
		l.tokens--
		return true
	}
	return false
}

// UsageQuerier reads historical spend from inference_logs for budget enforcement.
type UsageQuerier interface {
	SumCostByPeriod(orgID, routerID, virtualKeyID, teamID string, from time.Time) (requests int64, costUSD float64, err error)
}

type service struct {
	orgRepo          domain.OrganizationRepository
	apiKeyRepo       domain.APIKeyRepository
	virtualKeyRepo   domain.VirtualKeyRepository
	teamRepo         domain.TeamRepository
	userRepo         domain.UserRepository
	groupMappingRepo domain.OIDCGroupMappingRepository
	vault            vault.Provider
	cfg              ServiceConfig
	jwks             jwksCache
	vkLocks          sync.Map // vkID → *sync.Mutex
	vkRateLimiters   sync.Map // vkID → *vkRateLimiter
	usageQuerier     UsageQuerier
}

func orgIDFromCtx(ctx context.Context) string {
	return domain.OrgIDFromContext(ctx)
}

func NewService(
	orgRepo domain.OrganizationRepository,
	apiKeyRepo domain.APIKeyRepository,
	virtualKeyRepo domain.VirtualKeyRepository,
	teamRepo domain.TeamRepository,
	userRepo domain.UserRepository,
	groupMappingRepo domain.OIDCGroupMappingRepository,
	vaultProvider vault.Provider,
	cfg ServiceConfig,
	usageQuerier UsageQuerier,
) Service {
	return &service{
		orgRepo:          orgRepo,
		apiKeyRepo:       apiKeyRepo,
		virtualKeyRepo:   virtualKeyRepo,
		teamRepo:         teamRepo,
		userRepo:         userRepo,
		groupMappingRepo: groupMappingRepo,
		vault:            vaultProvider,
		cfg:              cfg,
		usageQuerier:     usageQuerier,
	}
}

// ── Setup / onboarding ────────────────────────────────────────────────────────

func (s *service) SetupStatus(ctx context.Context) (SetupStatusResponse, error) {
	count, err := s.orgRepo.Count(ctx)
	if err != nil {
		return SetupStatusResponse{}, err
	}
	return SetupStatusResponse{Required: count == 0}, nil
}

func (s *service) Setup(ctx context.Context, callerEmail string, input SetupInput) (*OrganizationResponse, error) {
	count, err := s.orgRepo.Count(ctx)
	if err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, domain.ErrSetupAlreadyDone
	}

	org := &domain.Organization{
		ID:        typeid.MustGenerate("org").String(),
		Name:      input.OrgName,
		Slug:      slugify(input.OrgName),
		IsEnabled: true,
	}
	if err := s.orgRepo.Create(ctx, org); err != nil {
		return nil, fmt.Errorf("create organization: %w", err)
	}

	// Assign the calling user to the new org as admin.
	user, err := s.userRepo.FindByEmail(ctx, callerEmail)
	if err != nil {
		return nil, fmt.Errorf("find caller: %w", err)
	}
	user.OrgID = org.ID
	user.Role = domain.UserRoleAdmin
	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, fmt.Errorf("assign user to org: %w", err)
	}

	resp := toOrganizationResponse(org)
	return &resp, nil
}

// ── Organisations ─────────────────────────────────────────────────────────────

func (s *service) ListOrganizations(ctx context.Context, slice pagination.Slice) (pagination.Paginated[OrganizationResponse], error) {
	orgs, total, err := s.orgRepo.List(ctx, slice.Offset(), slice.PerPage)
	if err != nil {
		return pagination.Paginated[OrganizationResponse]{}, err
	}
	out := make([]OrganizationResponse, 0, len(orgs))
	for i := range orgs {
		out = append(out, toOrganizationResponse(&orgs[i]))
	}
	return pagination.New(out, total, slice), nil
}

func (s *service) CreateOrganization(ctx context.Context, input CreateOrganizationInput) (*OrganizationResponse, error) {
	org := &domain.Organization{
		ID:        typeid.MustGenerate("org").String(),
		Name:      input.Name,
		Slug:      slugify(input.Name),
		IsEnabled: true,
	}
	if err := s.orgRepo.Create(ctx, org); err != nil {
		return nil, err
	}
	resp := toOrganizationResponse(org)
	return &resp, nil
}

func (s *service) UpdateOrganization(ctx context.Context, id string, input UpdateOrganizationInput) (*OrganizationResponse, error) {
	org, err := s.orgRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		org.Name = *input.Name
		org.Slug = slugify(*input.Name)
	}
	if input.IsEnabled != nil {
		org.IsEnabled = *input.IsEnabled
	}
	if err := s.orgRepo.Update(ctx, org); err != nil {
		return nil, err
	}
	resp := toOrganizationResponse(org)
	return &resp, nil
}

func (s *service) DeleteOrganization(ctx context.Context, id string) error {
	if _, err := s.orgRepo.FindByID(ctx, id); err != nil {
		return err
	}
	return s.orgRepo.Delete(ctx, id)
}

// ── KeyValidator ──────────────────────────────────────────────────────────────

func (s *service) ValidateInferKey(ctx context.Context, plaintext, routerID string) (context.Context, error) {
	hash := sha256.Sum256([]byte(plaintext))
	keyHash := hex.EncodeToString(hash[:])

	// Try API key first.
	k, err := s.apiKeyRepo.FindByKeyHash(ctx, keyHash)
	if err == nil {
		if !k.IsEnabled {
			return ctx, domain.ErrKeyDisabled
		}
		if k.IsExpired() {
			return ctx, domain.ErrKeyExpired
		}
		if k.Scope == domain.APIKeyScopeRouter && k.RouterID != routerID {
			return ctx, domain.ErrScopeViolation
		}
		if k.TeamID != "" {
			if team, terr := s.teamRepo.FindByID(ctx, k.OrgID, k.TeamID); terr == nil && !team.IsEnabled {
				return ctx, domain.ErrTeamDisabled
			}
		}
		now := time.Now()
		k.LastUsedAt = &now
		_ = s.apiKeyRepo.Update(ctx, k)
		ctx = domain.WithOrgID(ctx, k.OrgID)
		return domain.WithCallerTeamID(ctx, k.TeamID), nil
	}

	// Try virtual key.
	vk, err := s.virtualKeyRepo.FindByKeyHash(ctx, keyHash)
	if err != nil {
		return ctx, domain.ErrUnauthorized
	}
	if !vk.IsEnabled {
		return ctx, domain.ErrKeyDisabled
	}
	if vk.RouterID != routerID {
		return ctx, domain.ErrScopeViolation
	}

	// Serialise budget check + increment per virtual-key ID to prevent
	// concurrent requests from slipping through the budget check simultaneously.
	lockVal, _ := s.vkLocks.LoadOrStore(vk.ID, &sync.Mutex{})
	vkMu := lockVal.(*sync.Mutex)
	vkMu.Lock()
	defer vkMu.Unlock()

	if s.usageQuerier != nil && (vk.MaxRequests > 0 || vk.MaxCostUSD > 0) {
		from := virtualKeyPeriodStart(vk.ResetPeriod)
		usedReqs, usedCost, _ := s.usageQuerier.SumCostByPeriod(vk.OrgID, "", vk.ID, "", from)
		if vk.MaxRequests > 0 && usedReqs >= vk.MaxRequests {
			return ctx, domain.ErrBudgetExceeded
		}
		if vk.MaxCostUSD > 0 && usedCost >= vk.MaxCostUSD {
			return ctx, domain.ErrBudgetExceeded
		}
	}

	// Gateway-level per-key rate limit (token bucket, in-memory, no persistence).
	if vk.RateLimitRPS > 0 {
		limiterVal, _ := s.vkRateLimiters.LoadOrStore(vk.ID, &vkRateLimiter{
			rps:      vk.RateLimitRPS,
			tokens:   vk.RateLimitRPS,
			lastTick: time.Now(),
		})
		if !limiterVal.(*vkRateLimiter).Allow() {
			return ctx, domain.ErrRateLimitExceeded
		}
	}

	if vk.TeamID != nil {
		team, terr := s.teamRepo.FindByID(ctx, vk.OrgID, *vk.TeamID)
		if terr == nil {
			if !team.IsEnabled {
				return ctx, domain.ErrTeamDisabled
			}
			if s.usageQuerier != nil && (team.MaxRequests > 0 || team.MaxCostUSD > 0) {
				from := virtualKeyPeriodStart(vk.ResetPeriod)
				usedReqs, usedCost, _ := s.usageQuerier.SumCostByPeriod(vk.OrgID, "", "", *vk.TeamID, from)
				if team.MaxRequests > 0 && usedReqs >= team.MaxRequests {
					return ctx, domain.ErrTeamBudgetExceeded
				}
				if team.MaxCostUSD > 0 && usedCost >= team.MaxCostUSD {
					return ctx, domain.ErrTeamBudgetExceeded
				}
			}
		}
	}

	// Embed the virtual key and team IDs so RecordInferenceCost can update
	// actual cost after the upstream call completes.
	teamIDStr := ""
	if vk.TeamID != nil {
		teamIDStr = *vk.TeamID
	}
	ctx = domain.WithOrgID(ctx, vk.OrgID)
	return domain.WithVirtualKeyID(ctx, vk.ID, teamIDStr), nil
}

func virtualKeyPeriodStart(period domain.ResetPeriod) time.Time {
	now := time.Now().UTC()
	switch period {
	case domain.ResetPeriodDaily:
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	case domain.ResetPeriodMonthly:
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		return time.Time{} // no reset: count from epoch
	}
}

// RotateAPIKey generates a new secret for the given key, revokes the old
// secret in place, and returns the new plaintext value once.
func (s *service) RotateAPIKey(ctx context.Context, id string) (*APIKeyCreatedResponse, error) {
	k, err := s.apiKeyRepo.FindByID(ctx, orgIDFromCtx(ctx), id)
	if err != nil {
		return nil, err
	}

	plaintext, keyHash, err := generateKey()
	if err != nil {
		return nil, err
	}

	k.KeyHash = keyHash
	if err := s.apiKeyRepo.Update(ctx, k); err != nil {
		return nil, err
	}

	if s.vault != nil {
		_ = s.vault.StoreKey(ctx, k.ID, plaintext)
	}

	return &APIKeyCreatedResponse{APIKeyResponse: toAPIKeyResponse(k), Key: plaintext}, nil
}

// ── API Keys ──────────────────────────────────────────────────────────────────

func (s *service) ListAPIKeys(ctx context.Context, routerID, teamID string, slice pagination.Slice) (pagination.Paginated[APIKeyResponse], error) {
	orgID := domain.OrgIDFromContext(ctx)
	keys, total, err := s.apiKeyRepo.ListByOrg(ctx, orgID, routerID, teamID, slice.Offset(), slice.PerPage)
	if err != nil {
		return pagination.Paginated[APIKeyResponse]{}, err
	}
	out := make([]APIKeyResponse, 0, len(keys))
	for i := range keys {
		out = append(out, toAPIKeyResponse(&keys[i]))
	}
	return pagination.New(out, total, slice), nil
}

func (s *service) CreateAPIKey(ctx context.Context, input CreateAPIKeyInput) (*APIKeyCreatedResponse, error) {
	orgID := domain.OrgIDFromContext(ctx)

	if input.TeamID == "" {
		return nil, domain.ErrTeamRequired
	}
	if input.TeamID != "" {
		team, err := s.teamRepo.FindByID(ctx, orgIDFromCtx(ctx), input.TeamID)
		if err != nil {
			return nil, err
		}
		if team.OrgID != orgID {
			return nil, domain.ErrTeamNotFound
		}
	}

	scope := input.Scope
	if scope == "" {
		if input.RouterID != "" {
			scope = domain.APIKeyScopeRouter
		} else {
			scope = domain.APIKeyScopeGlobal
		}
	}

	plaintext, keyHash, err := generateKey()
	if err != nil {
		return nil, err
	}

	k := &domain.APIKey{
		ID:           typeid.MustGenerate("akey").String(),
		OrgID:        orgID,
		TeamID:       input.TeamID,
		RouterID:     input.RouterID,
		VirtualKeyID: input.VirtualKeyID,
		Name:         input.Name,
		Description:  input.Description,
		KeyHash:      keyHash,
		Scope:        scope,
		ExpiresAt:    input.ExpiresAt,
		IsEnabled:    true,
	}

	if s.vault != nil {
		if err := s.vault.StoreKey(ctx, k.ID, plaintext); err != nil {
			return nil, fmt.Errorf("vault store key: %w", err)
		}
	}

	if err := s.apiKeyRepo.Create(ctx, k); err != nil {
		return nil, err
	}
	return &APIKeyCreatedResponse{APIKeyResponse: toAPIKeyResponse(k), Key: plaintext}, nil
}

func (s *service) RevokeAPIKey(ctx context.Context, id string) error {
	k, err := s.apiKeyRepo.FindByID(ctx, orgIDFromCtx(ctx), id)
	if err != nil {
		return err
	}
	if s.vault != nil {
		_ = s.vault.DeleteKey(ctx, k.ID)
	}
	return s.apiKeyRepo.Delete(ctx, orgIDFromCtx(ctx), id)
}

// ── Virtual Keys ──────────────────────────────────────────────────────────────

func (s *service) GetVirtualKey(ctx context.Context, id string) (*VirtualKeyResponse, error) {
	if id == "" {
		return nil, nil
	}
	k, err := s.virtualKeyRepo.FindByID(ctx, orgIDFromCtx(ctx), id)
	if err != nil {
		return nil, err
	}
	resp := toVirtualKeyResponse(k)
	return &resp, nil
}

func (s *service) ListVirtualKeys(ctx context.Context, routerID, teamID string, slice pagination.Slice) (pagination.Paginated[VirtualKeyResponse], error) {
	orgID := domain.OrgIDFromContext(ctx)
	keys, total, err := s.virtualKeyRepo.List(ctx, orgID, routerID, teamID, slice.Offset(), slice.PerPage)
	if err != nil {
		return pagination.Paginated[VirtualKeyResponse]{}, err
	}
	out := make([]VirtualKeyResponse, 0, len(keys))
	for i := range keys {
		out = append(out, toVirtualKeyResponse(&keys[i]))
	}
	return pagination.New(out, total, slice), nil
}

func (s *service) CreateVirtualKey(ctx context.Context, input CreateVirtualKeyInput) (*VirtualKeyCreatedResponse, error) {
	orgID := domain.OrgIDFromContext(ctx)

	if input.TeamID != "" {
		team, err := s.teamRepo.FindByID(ctx, orgIDFromCtx(ctx), input.TeamID)
		if err != nil {
			return nil, err
		}
		if team.OrgID != orgID {
			return nil, domain.ErrTeamNotFound
		}
	}

	plaintext, keyHash, err := generateKey()
	if err != nil {
		return nil, err
	}

	var teamID *string
	if input.TeamID != "" {
		teamID = &input.TeamID
	}

	vk := &domain.VirtualKey{
		ID:           typeid.MustGenerate("vkey").String(),
		OrgID:        orgID,
		RouterID:     input.RouterID,
		TeamID:       teamID,
		Name:         input.Name,
		Description:  input.Description,
		KeyHash:      keyHash,
		MaxRequests:  input.MaxRequests,
		MaxCostUSD:   input.MaxCostUSD,
		ResetPeriod:  input.ResetPeriod,
		RateLimitRPS: input.RateLimitRPS,
		IsEnabled:    true,
	}
	if err := s.virtualKeyRepo.Create(ctx, vk); err != nil {
		return nil, err
	}
	return &VirtualKeyCreatedResponse{VirtualKeyResponse: toVirtualKeyResponse(vk), Key: plaintext}, nil
}

func (s *service) UpdateVirtualKey(ctx context.Context, id string, input UpdateVirtualKeyInput) (*VirtualKeyResponse, error) {
	vk, err := s.virtualKeyRepo.FindByID(ctx, orgIDFromCtx(ctx), id)
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		vk.Name = *input.Name
	}
	if input.Description != nil {
		vk.Description = *input.Description
	}
	if input.MaxRequests != nil {
		vk.MaxRequests = *input.MaxRequests
	}
	if input.MaxCostUSD != nil {
		vk.MaxCostUSD = *input.MaxCostUSD
	}
	if input.ResetPeriod != nil {
		vk.ResetPeriod = *input.ResetPeriod
	}
	if input.IsEnabled != nil {
		vk.IsEnabled = *input.IsEnabled
	}
	if input.RateLimitRPS != nil {
		vk.RateLimitRPS = *input.RateLimitRPS
		// Evict stale rate-limiter bucket so the next request picks up the new rate.
		s.vkRateLimiters.Delete(vk.ID)
	}
	if err := s.virtualKeyRepo.Update(ctx, vk); err != nil {
		return nil, err
	}
	resp := toVirtualKeyResponse(vk)
	return &resp, nil
}

func (s *service) RevokeVirtualKey(ctx context.Context, id string) error {
	if _, err := s.virtualKeyRepo.FindByID(ctx, orgIDFromCtx(ctx), id); err != nil {
		return err
	}
	return s.virtualKeyRepo.Delete(ctx, orgIDFromCtx(ctx), id)
}

// ── Teams ─────────────────────────────────────────────────────────────────────

func (s *service) GetTeam(ctx context.Context, id string) (*TeamResponse, error) {
	if id == "" {
		return nil, nil
	}
	t, err := s.teamRepo.FindByID(ctx, orgIDFromCtx(ctx), id)
	if err != nil {
		return nil, err
	}
	resp := toTeamResponse(t)
	return &resp, nil
}

func (s *service) GetTeamsByIDs(ctx context.Context, ids []string) ([]TeamResponse, error) {
	orgID := domain.OrgIDFromContext(ctx)
	teams, err := s.teamRepo.ListByIDs(ctx, orgID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]TeamResponse, 0, len(teams))
	for i := range teams {
		out = append(out, toTeamResponse(&teams[i]))
	}
	return out, nil
}

func (s *service) ListTeams(ctx context.Context, slice pagination.Slice, query string) (pagination.Paginated[TeamResponse], error) {
	orgID := domain.OrgIDFromContext(ctx)
	teams, total, err := s.teamRepo.ListByOrgID(ctx, orgID, query, slice.Offset(), slice.PerPage)
	if err != nil {
		return pagination.Paginated[TeamResponse]{}, err
	}
	out := make([]TeamResponse, 0, len(teams))
	for i := range teams {
		out = append(out, toTeamResponse(&teams[i]))
	}
	return pagination.New(out, total, slice), nil
}

func (s *service) CreateTeam(ctx context.Context, input CreateTeamInput) (*TeamResponse, error) {
	orgID := domain.OrgIDFromContext(ctx)
	t := &domain.Team{
		ID:          typeid.MustGenerate("team").String(),
		OrgID:       orgID,
		Name:        input.Name,
		Description: input.Description,
		MaxRequests: input.MaxRequests,
		MaxCostUSD:  input.MaxCostUSD,
		IsEnabled:   true,
	}
	if err := s.teamRepo.Create(ctx, t); err != nil {
		return nil, err
	}
	resp := toTeamResponse(t)
	return &resp, nil
}

func (s *service) UpdateTeam(ctx context.Context, id string, input UpdateTeamInput) (*TeamResponse, error) {
	t, err := s.teamRepo.FindByID(ctx, orgIDFromCtx(ctx), id)
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		t.Name = *input.Name
	}
	if input.Description != nil {
		t.Description = *input.Description
	}
	if input.MaxRequests != nil {
		t.MaxRequests = *input.MaxRequests
	}
	if input.MaxCostUSD != nil {
		t.MaxCostUSD = *input.MaxCostUSD
	}
	if input.IsEnabled != nil {
		t.IsEnabled = *input.IsEnabled
	}
	if err := s.teamRepo.Update(ctx, t); err != nil {
		return nil, err
	}
	resp := toTeamResponse(t)
	return &resp, nil
}

func (s *service) DeleteTeam(ctx context.Context, id string) error {
	if _, err := s.teamRepo.FindByID(ctx, orgIDFromCtx(ctx), id); err != nil {
		return err
	}
	return s.teamRepo.Delete(ctx, orgIDFromCtx(ctx), id)
}

func (s *service) AddTeamMember(ctx context.Context, teamID, userID string) error {
	orgID := orgIDFromCtx(ctx)
	if _, err := s.teamRepo.FindByID(ctx, orgID, teamID); err != nil {
		return err
	}
	if orgID != "" {
		if _, err := s.userRepo.FindByIDInOrg(ctx, orgID, userID); err != nil {
			return err
		}
	} else if _, err := s.userRepo.FindByID(ctx, userID); err != nil {
		return err
	}
	return s.teamRepo.AddMember(ctx, teamID, userID)
}

func (s *service) RemoveTeamMember(ctx context.Context, teamID, userID string) error {
	orgID := orgIDFromCtx(ctx)
	if _, err := s.teamRepo.FindByID(ctx, orgID, teamID); err != nil {
		return err
	}
	if orgID != "" {
		if _, err := s.userRepo.FindByIDInOrg(ctx, orgID, userID); err != nil {
			return err
		}
	}
	return s.teamRepo.RemoveMember(ctx, teamID, userID)
}

// ── OIDC / session ────────────────────────────────────────────────────────────

type oidcClaims struct {
	Email        string   `json:"email"`
	Groups       []string `json:"groups"`
	UserMetadata struct {
		FullName  string `json:"full_name"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
		Picture   string `json:"picture"`
	} `json:"user_metadata"`
	ExpiresAt int64 `json:"exp"`
}

const jwksCacheTTL = time.Hour

func (s *service) fetchJWKS(ctx context.Context, forceRefresh bool) (*jose.JSONWebKeySet, error) {
	if !forceRefresh {
		s.jwks.mu.RLock()
		if s.jwks.keys != nil && time.Since(s.jwks.fetchedAt) < jwksCacheTTL {
			ks := s.jwks.keys
			s.jwks.mu.RUnlock()
			return ks, nil
		}
		s.jwks.mu.RUnlock()
	}
	s.jwks.mu.Lock()
	defer s.jwks.mu.Unlock()
	if !forceRefresh && s.jwks.keys != nil && time.Since(s.jwks.fetchedAt) < jwksCacheTTL {
		return s.jwks.keys, nil
	}
	if s.cfg.OIDCJWKSURL == "" {
		return nil, fmt.Errorf("OIDC_JWKS_URL not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.OIDCJWKSURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build jwks request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()
	var ks jose.JSONWebKeySet
	if err := json.NewDecoder(resp.Body).Decode(&ks); err != nil {
		return nil, fmt.Errorf("parse jwks: %w", err)
	}
	s.jwks.keys = &ks
	s.jwks.fetchedAt = time.Now()
	return &ks, nil
}

func (s *service) validateOIDCJWT(ctx context.Context, token string) (*oidcClaims, error) {
	jws, err := jose.ParseSigned(token, []jose.SignatureAlgorithm{jose.RS256, jose.ES256})
	if err != nil {
		return nil, fmt.Errorf("parse jwt: %w", err)
	}
	if len(jws.Signatures) == 0 {
		return nil, fmt.Errorf("jwt has no signatures")
	}
	kid := jws.Signatures[0].Header.KeyID
	verify := func(ks *jose.JSONWebKeySet) ([]byte, error) {
		keys := ks.Key(kid)
		if len(keys) == 0 {
			return nil, fmt.Errorf("no key for kid %q", kid)
		}
		return jws.Verify(keys[0].Public())
	}
	ks, err := s.fetchJWKS(ctx, false)
	if err != nil {
		return nil, err
	}
	payload, err := verify(ks)
	if err != nil {
		ks, err = s.fetchJWKS(ctx, true)
		if err != nil {
			return nil, err
		}
		payload, err = verify(ks)
		if err != nil {
			return nil, fmt.Errorf("invalid jwt signature: %w", err)
		}
	}
	var claims oidcClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("parse jwt claims: %w", err)
	}
	if time.Now().Unix() > claims.ExpiresAt {
		return nil, fmt.Errorf("jwt expired")
	}
	if claims.Email == "" {
		return nil, fmt.Errorf("jwt missing email claim")
	}
	return &claims, nil
}

func (s *service) ExchangeOIDCToken(ctx context.Context, oidcJWT string) (*SessionResponse, error) {
	claims, err := s.validateOIDCJWT(ctx, oidcJWT)
	if err != nil {
		return nil, domain.ErrSessionInvalid
	}
	name := claims.UserMetadata.FullName
	if name == "" {
		name = claims.UserMetadata.Name
	}
	avatar := claims.UserMetadata.AvatarURL
	if avatar == "" {
		avatar = claims.UserMetadata.Picture
	}
	identity := &userIdentity{Email: claims.Email, Name: name, Avatar: avatar}
	dbUser, err := s.upsertUser(ctx, identity)
	if err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}
	go s.applyGroupMappings(context.Background(), dbUser.ID, dbUser.OrgID, claims.Groups)
	token, err := s.signSessionToken(identity, dbUser.Role, dbUser.OrgID)
	if err != nil {
		return nil, err
	}
	return &SessionResponse{Token: token, User: toUserResponse(dbUser)}, nil
}

func (s *service) RefreshSession(ctx context.Context, email string) (*SessionResponse, error) {
	dbUser, err := s.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, domain.ErrUserNotFound
	}
	identity := &userIdentity{Email: dbUser.Email, Name: dbUser.Name, Avatar: dbUser.Avatar}
	token, err := s.signSessionToken(identity, dbUser.Role, dbUser.OrgID)
	if err != nil {
		return nil, err
	}
	return &SessionResponse{Token: token, User: toUserResponse(dbUser)}, nil
}

// ── Users ─────────────────────────────────────────────────────────────────────

func (s *service) GetMe(ctx context.Context, email string) (*UserResponse, error) {
	u, err := s.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	resp := toUserResponse(u)
	return &resp, nil
}

func (s *service) ListUsers(ctx context.Context, slice pagination.Slice) (pagination.Paginated[UserResponse], error) {
	orgID := orgIDFromCtx(ctx)
	var (
		users []domain.User
		total int64
		err   error
	)
	if orgID == "" {
		users, total, err = s.userRepo.ListAll(ctx, slice.Offset(), slice.PerPage)
	} else {
		users, total, err = s.userRepo.ListByOrg(ctx, orgID, slice.Offset(), slice.PerPage)
	}
	if err != nil {
		return pagination.Paginated[UserResponse]{}, err
	}
	out := make([]UserResponse, 0, len(users))
	for i := range users {
		out = append(out, toUserResponse(&users[i]))
	}
	return pagination.New(out, total, slice), nil
}

func (s *service) UpdateUser(ctx context.Context, id string, input UpdateUserInput) (*UserResponse, error) {
	orgID := orgIDFromCtx(ctx)
	var (
		u   *domain.User
		err error
	)
	if orgID == "" {
		u, err = s.userRepo.FindByID(ctx, id)
	} else {
		if input.OrgID != nil && *input.OrgID != orgID {
			return nil, domain.ErrUserNotFound
		}
		u, err = s.userRepo.FindByIDInOrg(ctx, orgID, id)
	}
	if err != nil {
		return nil, err
	}
	if input.Role != nil {
		u.Role = *input.Role
	}
	if input.OrgID != nil {
		u.OrgID = *input.OrgID
	}
	if err := s.userRepo.Update(ctx, u); err != nil {
		return nil, err
	}
	resp := toUserResponse(u)
	return &resp, nil
}

func (s *service) upsertUser(ctx context.Context, ssoUser *userIdentity) (*domain.User, error) {
	isAdminEmail := s.cfg.AdminEmail != "" &&
		strings.EqualFold(s.cfg.AdminEmail, ssoUser.Email)

	existing, err := s.userRepo.FindByEmail(ctx, ssoUser.Email)
	if err == nil {
		existing.Name = ssoUser.Name
		existing.Avatar = ssoUser.Avatar
		if isAdminEmail {
			existing.Role = domain.UserRoleAdmin
		}
		now := time.Now()
		existing.LastLoginAt = &now
		_ = s.userRepo.Update(ctx, existing)
		return existing, nil
	}

	role := domain.UserRoleMember
	if isAdminEmail {
		role = domain.UserRoleAdmin
	} else {
		count, countErr := s.userRepo.Count(ctx)
		if countErr == nil && count == 0 {
			role = domain.UserRoleAdmin
		}
	}

	now := time.Now()
	u := &domain.User{
		ID:          typeid.MustGenerate("user").String(),
		Email:       ssoUser.Email,
		Name:        ssoUser.Name,
		Avatar:      ssoUser.Avatar,
		Role:        role,
		LastLoginAt: &now,
	}
	return u, s.userRepo.Create(ctx, u)
}

// ── Session token ─────────────────────────────────────────────────────────────

type sessionClaims struct {
	Email     string          `json:"email"`
	Name      string          `json:"name"`
	Role      domain.UserRole `json:"role"`
	OrgID     string          `json:"orgId"`
	ExpiresAt time.Time       `json:"exp"`
}

func (s *service) signSessionToken(user *userIdentity, role domain.UserRole, orgID string) (string, error) {
	claims := sessionClaims{
		Email:     user.Email,
		Name:      user.Name,
		Role:      role,
		OrgID:     orgID,
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.cfg.JWTSecret)
	mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig, nil
}

func (s *service) ValidateSession(token string) (*SessionUser, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, domain.ErrSessionInvalid
	}
	encoded, sig := parts[0], parts[1]
	mac := hmac.New(sha256.New, s.cfg.JWTSecret)
	mac.Write([]byte(encoded))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return nil, domain.ErrSessionInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, domain.ErrSessionInvalid
	}
	var claims sessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, domain.ErrSessionInvalid
	}
	if time.Now().After(claims.ExpiresAt) {
		return nil, domain.ErrSessionInvalid
	}
	dbUser, err := s.userRepo.FindByEmail(context.Background(), claims.Email)
	if err != nil {
		return nil, domain.ErrSessionInvalid
	}
	teamIDs, err := s.teamRepo.ListTeamIDsForUser(context.Background(), dbUser.ID)
	if err != nil {
		return nil, domain.ErrSessionInvalid
	}
	return &SessionUser{
		ID:      dbUser.ID,
		Email:   dbUser.Email,
		Name:    dbUser.Name,
		Role:    dbUser.Role,
		OrgID:   dbUser.OrgID,
		TeamIDs: teamIDs,
	}, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func generateKey() (plaintext, keyHash string, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate key: %w", err)
	}
	plaintext = hex.EncodeToString(raw)
	h := sha256.Sum256([]byte(plaintext))
	keyHash = hex.EncodeToString(h[:])
	return plaintext, keyHash, nil
}

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(s)
	s = nonAlphanumeric.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// ── OIDC group → team mappings ────────────────────────────────────────────────

func (s *service) ListGroupMappings(ctx context.Context) ([]domain.OIDCGroupMapping, error) {
	orgID := orgIDFromCtx(ctx)
	return s.groupMappingRepo.List(ctx, orgID)
}

func (s *service) CreateGroupMapping(ctx context.Context, groupName, teamID string) (*domain.OIDCGroupMapping, error) {
	orgID := orgIDFromCtx(ctx)
	m := &domain.OIDCGroupMapping{
		ID:        typeid.MustGenerate("ogmap").String(),
		OrgID:     orgID,
		GroupName: groupName,
		TeamID:    teamID,
	}
	if err := s.groupMappingRepo.Create(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *service) DeleteGroupMapping(ctx context.Context, id string) error {
	orgID := orgIDFromCtx(ctx)
	return s.groupMappingRepo.Delete(ctx, orgID, id)
}

// applyGroupMappings auto-enrolls the user into teams based on OIDC groups claim.
func (s *service) applyGroupMappings(ctx context.Context, userID, orgID string, groups []string) {
	if len(groups) == 0 || s.groupMappingRepo == nil {
		return
	}
	existing, _ := s.teamRepo.ListTeamIDsForUser(ctx, userID)
	enrolled := make(map[string]bool, len(existing))
	for _, id := range existing {
		enrolled[id] = true
	}
	for _, g := range groups {
		mappings, err := s.groupMappingRepo.FindByGroup(ctx, orgID, g)
		if err != nil {
			continue
		}
		for _, m := range mappings {
			if !enrolled[m.TeamID] {
				_ = s.teamRepo.AddMember(ctx, m.TeamID, userID)
				enrolled[m.TeamID] = true
			}
		}
	}
}
