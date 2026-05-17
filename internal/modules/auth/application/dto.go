package application

import (
	"time"

	"hyperstrate/server/internal/modules/auth/domain"
)

// ── Organization ──────────────────────────────────────────────────────────────

type SetupInput struct {
	OrgName string `json:"orgName" binding:"required,max=255"`
}

type SetupStatusResponse struct {
	Required bool `json:"required" validate:"required"`
}

type CreateOrganizationInput struct {
	Name string `json:"name" binding:"required,max=255"`
}

type UpdateOrganizationInput struct {
	Name      *string `json:"name"`
	IsEnabled *bool   `json:"isEnabled"`
}

type OrganizationResponse struct {
	ID         string    `json:"id"         validate:"required"`
	Name       string    `json:"name"       validate:"required"`
	Slug       string    `json:"slug"       validate:"required"`
	IsEnabled  bool      `json:"isEnabled"  validate:"required"`
	CreatedAt  time.Time `json:"createdAt"  validate:"required"`
	ModifiedAt time.Time `json:"modifiedAt" validate:"required"`
}

func toOrganizationResponse(o *domain.Organization) OrganizationResponse {
	return OrganizationResponse{
		ID:         o.ID,
		Name:       o.Name,
		Slug:       o.Slug,
		IsEnabled:  o.IsEnabled,
		CreatedAt:  o.CreatedAt,
		ModifiedAt: o.ModifiedAt,
	}
}

// ── APIKey ────────────────────────────────────────────────────────────────────

type CreateAPIKeyInput struct {
	RouterID     string             `json:"routerId"`
	TeamID       string             `json:"teamId" binding:"required"`
	VirtualKeyID string             `json:"virtualKeyId"`
	Name         string             `json:"name"    binding:"required,max=255"`
	Description  string             `json:"description"`
	Scope        domain.APIKeyScope `json:"scope"`
	ExpiresAt    *time.Time         `json:"expiresAt"`
}

type APIKeyResponse struct {
	ID           string             `json:"id"           validate:"required"`
	OrgID        string             `json:"orgId"        validate:"required"`
	TeamID       string             `json:"teamId"       validate:"required"`
	RouterID     string             `json:"routerId"     validate:"required"`
	VirtualKeyID string             `json:"virtualKeyId" validate:"required"`
	Name         string             `json:"name"         validate:"required"`
	Description  string             `json:"description"  validate:"required"`
	Scope        domain.APIKeyScope `json:"scope"        validate:"required"`
	ExpiresAt    *time.Time         `json:"expiresAt"`
	LastUsedAt   *time.Time         `json:"lastUsedAt"`
	IsEnabled    bool               `json:"isEnabled"    validate:"required"`
	CreatedAt    time.Time          `json:"createdAt"    validate:"required"`
}

type APIKeyCreatedResponse struct {
	APIKeyResponse
	Key string `json:"key"`
}

func toAPIKeyResponse(k *domain.APIKey) APIKeyResponse {
	return APIKeyResponse{
		ID:           k.ID,
		OrgID:        k.OrgID,
		TeamID:       k.TeamID,
		RouterID:     k.RouterID,
		VirtualKeyID: k.VirtualKeyID,
		Name:         k.Name,
		Description:  k.Description,
		Scope:        k.Scope,
		ExpiresAt:    k.ExpiresAt,
		LastUsedAt:   k.LastUsedAt,
		IsEnabled:    k.IsEnabled,
		CreatedAt:    k.CreatedAt,
	}
}

// ── VirtualKey ────────────────────────────────────────────────────────────────

type CreateVirtualKeyInput struct {
	RouterID     string             `json:"routerId"    binding:"required"`
	TeamID       string             `json:"teamId"`
	Name         string             `json:"name"        binding:"required"`
	Description  string             `json:"description"`
	MaxRequests  int64              `json:"maxRequests"  binding:"min=0"`
	MaxCostUSD   float64            `json:"maxCostUsd"   binding:"min=0"`
	ResetPeriod  domain.ResetPeriod `json:"resetPeriod"`
	RateLimitRPS float64            `json:"rateLimitRps" binding:"min=0"`
}

type UpdateVirtualKeyInput struct {
	Name         *string             `json:"name"`
	Description  *string             `json:"description"`
	MaxRequests  *int64              `json:"maxRequests"`
	MaxCostUSD   *float64            `json:"maxCostUsd"`
	ResetPeriod  *domain.ResetPeriod `json:"resetPeriod"`
	IsEnabled    *bool               `json:"isEnabled"`
	RateLimitRPS *float64            `json:"rateLimitRps"`
}

type VirtualKeyResponse struct {
	ID           string             `json:"id"           validate:"required"`
	OrgID        string             `json:"orgId"        validate:"required"`
	RouterID     string             `json:"routerId"     validate:"required"`
	TeamID       string             `json:"teamId"       validate:"required"`
	Name         string             `json:"name"         validate:"required"`
	Description  string             `json:"description"  validate:"required"`
	MaxRequests  int64              `json:"maxRequests"  validate:"required"`
	MaxCostUSD   float64            `json:"maxCostUsd"   validate:"required"`
	ResetPeriod  domain.ResetPeriod `json:"resetPeriod"  validate:"required"`
	RateLimitRPS float64            `json:"rateLimitRps"`
	IsEnabled    bool               `json:"isEnabled"    validate:"required"`
	CreatedAt    time.Time          `json:"createdAt"    validate:"required"`
	ModifiedAt   time.Time          `json:"modifiedAt"   validate:"required"`
}

type VirtualKeyCreatedResponse struct {
	VirtualKeyResponse
	Key string `json:"key"`
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func toVirtualKeyResponse(k *domain.VirtualKey) VirtualKeyResponse {
	return VirtualKeyResponse{
		ID:           k.ID,
		OrgID:        k.OrgID,
		RouterID:     k.RouterID,
		TeamID:       derefString(k.TeamID),
		Name:         k.Name,
		Description:  k.Description,
		MaxRequests:  k.MaxRequests,
		MaxCostUSD:   k.MaxCostUSD,
		ResetPeriod:  k.ResetPeriod,
		RateLimitRPS: k.RateLimitRPS,
		IsEnabled:    k.IsEnabled,
		CreatedAt:    k.CreatedAt,
		ModifiedAt:   k.ModifiedAt,
	}
}

// ── Team ──────────────────────────────────────────────────────────────────────

type CreateTeamInput struct {
	Name        string  `json:"name"        binding:"required,max=255"`
	Description string  `json:"description"`
	MaxRequests int64   `json:"maxRequests" binding:"min=0"`
	MaxCostUSD  float64 `json:"maxCostUsd"  binding:"min=0"`
}

type UpdateTeamInput struct {
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	MaxRequests *int64   `json:"maxRequests"`
	MaxCostUSD  *float64 `json:"maxCostUsd"`
	IsEnabled   *bool    `json:"isEnabled"`
}

type TeamResponse struct {
	ID          string    `json:"id"           validate:"required"`
	OrgID       string    `json:"orgId"        validate:"required"`
	Name        string    `json:"name"         validate:"required"`
	Description string    `json:"description"  validate:"required"`
	MaxRequests int64     `json:"maxRequests"  validate:"required"`
	MaxCostUSD  float64   `json:"maxCostUsd"   validate:"required"`
	IsEnabled   bool      `json:"isEnabled"    validate:"required"`
	CreatedAt   time.Time `json:"createdAt"    validate:"required"`
	ModifiedAt  time.Time `json:"modifiedAt"   validate:"required"`
}

func toTeamResponse(t *domain.Team) TeamResponse {
	return TeamResponse{
		ID:          t.ID,
		OrgID:       t.OrgID,
		Name:        t.Name,
		Description: t.Description,
		MaxRequests: t.MaxRequests,
		MaxCostUSD:  t.MaxCostUSD,
		IsEnabled:   t.IsEnabled,
		CreatedAt:   t.CreatedAt,
		ModifiedAt:  t.ModifiedAt,
	}
}

// ── User ──────────────────────────────────────────────────────────────────────

type UpdateUserInput struct {
	Role  *domain.UserRole `json:"role"`
	OrgID *string          `json:"orgId"`
}

type UserResponse struct {
	ID          string          `json:"id"          validate:"required"`
	OrgID       string          `json:"orgId"       validate:"required"`
	Email       string          `json:"email"       validate:"required"`
	Name        string          `json:"name"        validate:"required"`
	Avatar      string          `json:"avatar"      validate:"required"`
	Role        domain.UserRole `json:"role"        validate:"required"`
	LastLoginAt *time.Time      `json:"lastLoginAt"`
	CreatedAt   time.Time       `json:"createdAt"   validate:"required"`
	ModifiedAt  time.Time       `json:"modifiedAt"  validate:"required"`
}

// SessionResponse is returned by login and token-refresh endpoints.
type SessionResponse struct {
	Token string       `json:"token"`
	User  UserResponse `json:"user"`
}

func toUserResponse(u *domain.User) UserResponse {
	return UserResponse{
		ID:          u.ID,
		OrgID:       u.OrgID,
		Email:       u.Email,
		Name:        u.Name,
		Avatar:      u.Avatar,
		Role:        u.Role,
		LastLoginAt: u.LastLoginAt,
		CreatedAt:   u.CreatedAt,
		ModifiedAt:  u.ModifiedAt,
	}
}
