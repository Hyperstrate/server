package http

import (
	"context"
	"errors"
	"net/http"

	"hyperstrate/server/internal/modules/auth/application"
	"hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/shared/audit"
	"hyperstrate/server/internal/shared/pagination"
	"hyperstrate/server/internal/shared/validation"

	"github.com/gin-gonic/gin"
)

// ── Relation ref types ────────────────────────────────────────────────────────

// TeamRef is the enriched team relation embedded in key responses.
type TeamRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// RouterRef is the enriched router relation embedded in key responses.
type RouterRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// VirtualKeyRef is the enriched virtual-key relation embedded in API key responses.
type VirtualKeyRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// RouterNameFunc resolves a router ID to its configured name.
type RouterNameFunc func(ctx context.Context, id string) string

// ── Enriched response types ───────────────────────────────────────────────────

// APIKeyResponse wraps the service DTO with nested relation objects.
type APIKeyResponse struct {
	application.APIKeyResponse
	Team       *TeamRef       `json:"team,omitempty"`
	Router     *RouterRef     `json:"router,omitempty"`
	VirtualKey *VirtualKeyRef `json:"virtualKey,omitempty"`
}

// APIKeyCreatedResponse adds the plaintext key to APIKeyResponse.
type APIKeyCreatedResponse struct {
	APIKeyResponse
	Key string `json:"key"`
}

// VirtualKeyResponse wraps the service DTO with nested relation objects.
type VirtualKeyResponse struct {
	application.VirtualKeyResponse
	Team   *TeamRef   `json:"team,omitempty"`
	Router *RouterRef `json:"router,omitempty"`
}

// VirtualKeyCreatedResponse adds the plaintext key to VirtualKeyResponse.
type VirtualKeyCreatedResponse struct {
	VirtualKeyResponse
	Key string `json:"key"`
}

// ── Handler ───────────────────────────────────────────────────────────────────

type Handler struct {
	svc           application.Service
	frontendURL   string
	oidcProviders []string
	routerName    RouterNameFunc
}

func NewHandler(svc application.Service, frontendURL string, oidcProviders []string, routerName RouterNameFunc) *Handler {
	return &Handler{svc: svc, frontendURL: frontendURL, oidcProviders: oidcProviders, routerName: routerName}
}

func auditLog(c *gin.Context, action, resource, resourceID string) {
	su := SessionUserFrom(c)
	email := ""
	if su != nil {
		email = su.Email
	}
	audit.Log(c.Request.Context(), audit.Record{
		OrgID:      domain.OrgIDFromContext(c.Request.Context()),
		UserEmail:  email,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		IPAddress:  audit.IPFromRequest(c.Request),
	})
}

func (h *Handler) enrichAPIKey(ctx context.Context, k application.APIKeyResponse) APIKeyResponse {
	out := APIKeyResponse{APIKeyResponse: k}
	if k.TeamID != "" {
		if t, err := h.svc.GetTeam(ctx, k.TeamID); err == nil && t != nil {
			out.Team = &TeamRef{ID: t.ID, Name: t.Name}
		}
	}
	if k.RouterID != "" {
		out.Router = &RouterRef{ID: k.RouterID, Name: h.routerName(ctx, k.RouterID)}
	}
	if k.VirtualKeyID != "" {
		if vk, err := h.svc.GetVirtualKey(ctx, k.VirtualKeyID); err == nil && vk != nil {
			out.VirtualKey = &VirtualKeyRef{ID: vk.ID, Name: vk.Name}
		}
	}
	return out
}

func (h *Handler) enrichVirtualKey(ctx context.Context, k application.VirtualKeyResponse) VirtualKeyResponse {
	out := VirtualKeyResponse{VirtualKeyResponse: k}
	if k.TeamID != "" {
		if t, err := h.svc.GetTeam(ctx, k.TeamID); err == nil && t != nil {
			out.Team = &TeamRef{ID: t.ID, Name: t.Name}
		}
	}
	if k.RouterID != "" {
		out.Router = &RouterRef{ID: k.RouterID, Name: h.routerName(ctx, k.RouterID)}
	}
	return out
}

func (h *Handler) RegisterPublicRoutes(r gin.IRoutes) {
	r.GET("/setup/status", h.GetSetupStatus)
	r.GET("/sso-providers/public", h.ListPublicSSOProviders)
	r.POST("/oidc/exchange", h.OIDCExchange)
}

func (h *Handler) RegisterSessionRoutes(r gin.IRoutes) {
	r.POST("/setup", h.Setup)
	r.POST("/refresh", h.RefreshSession)
	r.GET("/me", h.GetMe)
}

func (h *Handler) RegisterAdminRoutes(r gin.IRoutes) {
	// Organisations
	r.GET("/organizations", h.ListOrganizations)
	r.POST("/organizations", h.CreateOrganization)
	r.PATCH("/organizations/:orgId", h.UpdateOrganization)
	r.DELETE("/organizations/:orgId", h.DeleteOrganization)

	// Users
	r.GET("/users", h.ListUsers)
	r.PATCH("/users/:id", h.UpdateUser)

	// API Keys
	r.GET("/api-keys", h.ListAPIKeys)
	r.POST("/api-keys", h.CreateAPIKey)
	r.DELETE("/api-keys/:id", h.RevokeAPIKey)
	r.POST("/api-keys/:id/rotate", h.RotateAPIKey)

	// Virtual Keys
	r.GET("/virtual-keys", h.ListVirtualKeys)
	r.POST("/virtual-keys", h.CreateVirtualKey)
	r.GET("/virtual-keys/:id", h.GetVirtualKey)
	r.PATCH("/virtual-keys/:id", h.UpdateVirtualKey)
	r.DELETE("/virtual-keys/:id", h.RevokeVirtualKey)

	// Teams
	r.GET("/teams", h.ListTeams)
	r.POST("/teams", h.CreateTeam)
	r.GET("/teams/:id", h.GetTeam)
	r.PATCH("/teams/:id", h.UpdateTeam)
	r.DELETE("/teams/:id", h.DeleteTeam)
	r.POST("/teams/:id/members", h.AddTeamMember)
	r.DELETE("/teams/:id/members/:userId", h.RemoveTeamMember)

	// OIDC group → team mappings
	r.GET("/oidc/group-mappings", h.ListGroupMappings)
	r.POST("/oidc/group-mappings", h.CreateGroupMapping)
	r.DELETE("/oidc/group-mappings/:id", h.DeleteGroupMapping)
}

// ── Setup / onboarding ────────────────────────────────────────────────────────

// GetSetupStatus godoc
// @Summary     Check if initial setup is required
// @Description Returns { required: true } when no organisation exists yet
// @Tags        hyperstrate
// @Tags        auth
// @Produce     json
// @Success     200  {object}  application.SetupStatusResponse
// @Router      /auth/setup/status [get]
func (h *Handler) GetSetupStatus(c *gin.Context) {
	status, err := h.svc.SetupStatus(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
		return
	}
	c.JSON(http.StatusOK, status)
}

// Setup godoc
// @Summary     Create the first organisation (one-time setup)
// @Description Creates the initial organisation and assigns the calling user as admin. Fails if an org already exists.
// @Tags        hyperstrate
// @Tags        auth
// @Accept      json
// @Produce     json
// @Param       body  body      application.SetupInput  true  "Setup input"
// @Success     201   {object}  application.OrganizationResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     409   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /auth/setup [post]
func (h *Handler) Setup(c *gin.Context) {
	su := sessionUserFrom(c)
	if su == nil {
		c.JSON(http.StatusUnauthorized, ErrorResponse{Error: domain.ErrSessionInvalid.Error()})
		return
	}
	var input application.SetupInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	org, err := h.svc.Setup(c.Request.Context(), su.Email, input)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrSetupAlreadyDone):
			c.JSON(http.StatusConflict, ErrorResponse{Error: err.Error()})
		case errors.Is(err, domain.ErrUserNotFound):
			c.JSON(http.StatusUnauthorized, ErrorResponse{Error: domain.ErrSessionInvalid.Error()})
		default:
			_ = c.Error(err)
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
		}
		return
	}
	c.JSON(http.StatusCreated, org)
}

// ── Organisations ─────────────────────────────────────────────────────────────

// ListOrganizations godoc
// @Summary     List organisations
// @Tags        hyperstrate
// @Tags        auth
// @Produce     json
// @Param       page     query  int  false  "Page number (default 1)"
// @Param       perPage  query  int  false  "Items per page (default 30, max 500)"
// @Success     200  {object}  pagination.Paginated[application.OrganizationResponse]
// @Security    BearerAuth
// @Router      /auth/organizations [get]
func (h *Handler) ListOrganizations(c *gin.Context) {
	result, err := h.svc.ListOrganizations(c.Request.Context(), pagination.ParseSlice(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateOrganization godoc
// @Summary     Create an organisation
// @Tags        hyperstrate
// @Tags        auth
// @Accept      json
// @Produce     json
// @Param       body  body      application.CreateOrganizationInput  true  "Org input"
// @Success     201   {object}  application.OrganizationResponse
// @Failure     400   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /auth/organizations [post]
func (h *Handler) CreateOrganization(c *gin.Context) {
	var input application.CreateOrganizationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	org, err := h.svc.CreateOrganization(c.Request.Context(), input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, org)
}

// UpdateOrganization godoc
// @Summary     Update an organisation
// @Tags        hyperstrate
// @Tags        auth
// @Accept      json
// @Produce     json
// @Param       orgId  path      string                              true  "Org ID"
// @Param       body   body      application.UpdateOrganizationInput true  "Fields to update"
// @Success     200    {object}  application.OrganizationResponse
// @Failure     400    {object}  ErrorResponse
// @Failure     404    {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /auth/organizations/{orgId} [patch]
func (h *Handler) UpdateOrganization(c *gin.Context) {
	orgId, ok := validateParam(c, "orgId")
	if !ok {
		return
	}
	var input application.UpdateOrganizationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	org, err := h.svc.UpdateOrganization(c.Request.Context(), orgId, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, org)
}

// DeleteOrganization godoc
// @Summary     Delete an organisation
// @Tags        hyperstrate
// @Tags        auth
// @Param       orgId  path  string  true  "Org ID"
// @Success     204
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /auth/organizations/{orgId} [delete]
func (h *Handler) DeleteOrganization(c *gin.Context) {
	orgId, ok := validateParam(c, "orgId")
	if !ok {
		return
	}
	if err := h.svc.DeleteOrganization(c.Request.Context(), orgId); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ── API Keys ──────────────────────────────────────────────────────────────────

// ListAPIKeys godoc
// @Summary     List API keys
// @Tags        hyperstrate
// @Tags        auth
// @Produce     json
// @Param       routerId  query  string  false  "Filter by router ID"
// @Param       teamId    query  string  false  "Filter by team ID"
// @Param       page      query  int     false  "Page number (default 1)"
// @Param       perPage   query  int     false  "Items per page (default 30, max 500)"
// @Success     200       {object}  pagination.Paginated[APIKeyResponse]
// @Security    BearerAuth
// @Router      /auth/api-keys [get]
func (h *Handler) ListAPIKeys(c *gin.Context) {
	result, err := h.svc.ListAPIKeys(c.Request.Context(), c.Query("routerId"), c.Query("teamId"), pagination.ParseSlice(c))
	if err != nil {
		respondError(c, err)
		return
	}
	enriched := make([]APIKeyResponse, len(result.Items))
	for i, k := range result.Items {
		enriched[i] = h.enrichAPIKey(c.Request.Context(), k)
	}
	c.JSON(http.StatusOK, pagination.New(enriched, result.Meta.Total, pagination.ParseSlice(c)))
}

// CreateAPIKey godoc
// @Summary     Create an API key
// @Description Creates a team-owned API key, optionally scoped to a router. The plaintext key is returned once.
// @Tags        hyperstrate
// @Tags        auth
// @Accept      json
// @Produce     json
// @Param       body  body      application.CreateAPIKeyInput  true  "Key input"
// @Success     201   {object}  APIKeyCreatedResponse
// @Failure     400   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /auth/api-keys [post]
func (h *Handler) CreateAPIKey(c *gin.Context) {
	var input application.CreateAPIKeyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.CreateAPIKey(c.Request.Context(), input)
	if err != nil {
		respondError(c, err)
		return
	}
	auditLog(c, "create", "api_key", result.APIKeyResponse.ID)
	c.JSON(http.StatusCreated, APIKeyCreatedResponse{
		APIKeyResponse: h.enrichAPIKey(c.Request.Context(), result.APIKeyResponse),
		Key:            result.Key,
	})
}

// RevokeAPIKey godoc
// @Summary     Revoke an API key
// @Tags        hyperstrate
// @Tags        auth
// @Param       id   path  string  true  "Key ID"
// @Success     204
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /auth/api-keys/{id} [delete]
func (h *Handler) RevokeAPIKey(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	if err := h.svc.RevokeAPIKey(c.Request.Context(), id); err != nil {
		respondError(c, err)
		return
	}
	auditLog(c, "revoke", "api_key", id)
	c.Status(http.StatusNoContent)
}

// ── Virtual Keys ──────────────────────────────────────────────────────────────

// ListVirtualKeys godoc
// @Summary     List virtual keys
// @Tags        hyperstrate
// @Tags        auth
// @Produce     json
// @Param       routerId  query  string  false  "Filter by router ID"
// @Param       teamId    query  string  false  "Filter by team ID"
// @Param       page      query  int     false  "Page number (default 1)"
// @Param       perPage   query  int     false  "Items per page (default 30, max 500)"
// @Success     200       {object}  pagination.Paginated[VirtualKeyResponse]
// @Security    BearerAuth
// @Router      /auth/virtual-keys [get]
func (h *Handler) ListVirtualKeys(c *gin.Context) {
	result, err := h.svc.ListVirtualKeys(c.Request.Context(), c.Query("routerId"), c.Query("teamId"), pagination.ParseSlice(c))
	if err != nil {
		respondError(c, err)
		return
	}
	enriched := make([]VirtualKeyResponse, len(result.Items))
	for i, k := range result.Items {
		enriched[i] = h.enrichVirtualKey(c.Request.Context(), k)
	}
	c.JSON(http.StatusOK, pagination.New(enriched, result.Meta.Total, pagination.ParseSlice(c)))
}

// CreateVirtualKey godoc
// @Summary     Create a virtual key
// @Tags        hyperstrate
// @Tags        auth
// @Accept      json
// @Produce     json
// @Param       body  body      application.CreateVirtualKeyInput  true  "Virtual key input"
// @Success     201   {object}  VirtualKeyCreatedResponse
// @Security    BearerAuth
// @Router      /auth/virtual-keys [post]
func (h *Handler) CreateVirtualKey(c *gin.Context) {
	var input application.CreateVirtualKeyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.CreateVirtualKey(c.Request.Context(), input)
	if err != nil {
		respondError(c, err)
		return
	}
	auditLog(c, "create", "virtual_key", result.VirtualKeyResponse.ID)
	c.JSON(http.StatusCreated, VirtualKeyCreatedResponse{
		VirtualKeyResponse: h.enrichVirtualKey(c.Request.Context(), result.VirtualKeyResponse),
		Key:                result.Key,
	})
}

// UpdateVirtualKey godoc
// @Summary     Update a virtual key
// @Tags        hyperstrate
// @Tags        auth
// @Accept      json
// @Produce     json
// @Param       id    path      string                            true  "Virtual key ID"
// @Param       body  body      application.UpdateVirtualKeyInput true  "Fields to update"
// @Success     200   {object}  VirtualKeyResponse
// @Security    BearerAuth
// @Router      /auth/virtual-keys/{id} [patch]
func (h *Handler) UpdateVirtualKey(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	var input application.UpdateVirtualKeyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.UpdateVirtualKey(c.Request.Context(), id, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, h.enrichVirtualKey(c.Request.Context(), *result))
}

// RevokeVirtualKey godoc
// @Summary     Revoke a virtual key
// @Tags        hyperstrate
// @Tags        auth
// @Param       id   path  string  true  "Virtual key ID"
// @Success     204
// @Security    BearerAuth
// @Router      /auth/virtual-keys/{id} [delete]
func (h *Handler) RevokeVirtualKey(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	if err := h.svc.RevokeVirtualKey(c.Request.Context(), id); err != nil {
		respondError(c, err)
		return
	}
	auditLog(c, "revoke", "virtual_key", id)
	c.Status(http.StatusNoContent)
}

// GetVirtualKey godoc
// @Summary     Get a virtual key by ID
// @Tags        hyperstrate
// @Tags        auth
// @Produce     json
// @Param       id  path  string  true  "Virtual key ID"
// @Success     200  {object}  VirtualKeyResponse
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /auth/virtual-keys/{id} [get]
func (h *Handler) GetVirtualKey(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	vk, err := h.svc.GetVirtualKey(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, h.enrichVirtualKey(c.Request.Context(), *vk))
}

// ── Teams ─────────────────────────────────────────────────────────────────────

// ListTeams godoc
// @Summary     List teams in the caller's organisation
// @Tags        hyperstrate
// @Tags        auth
// @Produce     json
// @Param       ids      query  []string  false  "Fetch specific teams by ID (bypasses pagination)"  collectionFormat(multi)
// @Param       page     query  int       false  "Page number (default 1)"
// @Param       perPage  query  int       false  "Items per page (default 30, max 500)"
// @Param       query    query  string    false  "Search by team ID, name, or description"
// @Success     200  {object}  pagination.Paginated[application.TeamResponse]
// @Security    BearerAuth
// @Router      /auth/teams [get]
func (h *Handler) ListTeams(c *gin.Context) {
	if ids := c.QueryArray("ids"); len(ids) > 0 {
		result, err := h.svc.GetTeamsByIDs(c.Request.Context(), ids)
		if err != nil {
			respondError(c, err)
			return
		}
		sl := pagination.Slice{Page: 1, PerPage: len(result)}
		c.JSON(http.StatusOK, pagination.New(result, int64(len(result)), sl))
		return
	}
	result, err := h.svc.ListTeams(c.Request.Context(), pagination.ParseSlice(c), c.Query("query"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateTeam godoc
// @Summary     Create a team
// @Tags        hyperstrate
// @Tags        auth
// @Accept      json
// @Produce     json
// @Param       body  body      application.CreateTeamInput  true  "Team input"
// @Success     201   {object}  application.TeamResponse
// @Security    BearerAuth
// @Router      /auth/teams [post]
func (h *Handler) CreateTeam(c *gin.Context) {
	var input application.CreateTeamInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.CreateTeam(c.Request.Context(), input)
	if err != nil {
		respondError(c, err)
		return
	}
	auditLog(c, "create", "team", result.ID)
	c.JSON(http.StatusCreated, result)
}

// UpdateTeam godoc
// @Summary     Update a team
// @Tags        hyperstrate
// @Tags        auth
// @Accept      json
// @Produce     json
// @Param       id    path      string                      true  "Team ID"
// @Param       body  body      application.UpdateTeamInput true  "Fields to update"
// @Success     200   {object}  application.TeamResponse
// @Security    BearerAuth
// @Router      /auth/teams/{id} [patch]
func (h *Handler) UpdateTeam(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	var input application.UpdateTeamInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.UpdateTeam(c.Request.Context(), id, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteTeam godoc
// @Summary     Delete a team
// @Tags        hyperstrate
// @Tags        auth
// @Param       id   path  string  true  "Team ID"
// @Success     204
// @Security    BearerAuth
// @Router      /auth/teams/{id} [delete]
func (h *Handler) DeleteTeam(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	if err := h.svc.DeleteTeam(c.Request.Context(), id); err != nil {
		respondError(c, err)
		return
	}
	auditLog(c, "delete", "team", id)
	c.Status(http.StatusNoContent)
}

// GetTeam godoc
// @Summary     Get a team by ID
// @Tags        hyperstrate
// @Tags        auth
// @Produce     json
// @Param       id  path  string  true  "Team ID"
// @Success     200  {object}  application.TeamResponse
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /auth/teams/{id} [get]
func (h *Handler) GetTeam(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	team, err := h.svc.GetTeam(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, team)
}

// AddTeamMember godoc
// @Summary     Add a user to a team
// @Tags        hyperstrate
// @Tags        auth
// @Accept      json
// @Param       id    path  string  true  "Team ID"
// @Param       body  body  object{userId=string}  true  "User ID"
// @Success     204
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /auth/teams/{id}/members [post]
func (h *Handler) AddTeamMember(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	var body struct {
		UserID string `json:"userId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondBindError(c, err, &body)
		return
	}
	if err := h.svc.AddTeamMember(c.Request.Context(), id, body.UserID); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// RemoveTeamMember godoc
// @Summary     Remove a user from a team
// @Tags        hyperstrate
// @Tags        auth
// @Param       id      path  string  true  "Team ID"
// @Param       userId  path  string  true  "User ID"
// @Success     204
// @Security    BearerAuth
// @Router      /auth/teams/{id}/members/{userId} [delete]
func (h *Handler) RemoveTeamMember(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	userId, ok := validateParam(c, "userId")
	if !ok {
		return
	}
	if err := h.svc.RemoveTeamMember(c.Request.Context(), id, userId); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ── SSO / OIDC ────────────────────────────────────────────────────────────────

// ListPublicSSOProviders godoc
// @Summary     List enabled SSO provider types (public)
// @Tags        hyperstrate
// @Tags        auth
// @Produce     json
// @Success     200  {array}  object
// @Router      /auth/sso-providers/public [get]
func (h *Handler) ListPublicSSOProviders(c *gin.Context) {
	type publicProvider struct {
		Type string `json:"type"`
	}
	result := make([]publicProvider, 0, len(h.oidcProviders))
	for _, t := range h.oidcProviders {
		result = append(result, publicProvider{Type: t})
	}
	c.JSON(http.StatusOK, result)
}

// OIDCExchange godoc
// @Summary     Exchange an OIDC access token for a session token
// @Tags        hyperstrate
// @Tags        auth
// @Accept      json
// @Produce     json
// @Param       body  body      object{token=string}  true  "OIDC access token"
// @Success     200   {object}  object{token=string,user=application.UserResponse}
// @Failure     400   {object}  ErrorResponse
// @Failure     401   {object}  ErrorResponse
// @Router      /auth/oidc/exchange [post]
func (h *Handler) OIDCExchange(c *gin.Context) {
	var input struct {
		Token string `json:"token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	session, err := h.svc.ExchangeOIDCToken(c.Request.Context(), input.Token)
	if err != nil {
		if errors.Is(err, domain.ErrSessionInvalid) {
			c.JSON(http.StatusUnauthorized, ErrorResponse{Error: domain.ErrSessionInvalid.Error()})
			return
		}
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "exchange failed"})
		return
	}
	c.JSON(http.StatusOK, session)
}

// ── Users ─────────────────────────────────────────────────────────────────────

// GetMe godoc
// @Summary     Get current user
// @Tags        hyperstrate
// @Tags        auth
// @Produce     json
// @Success     200  {object}  application.UserResponse
// @Security    BearerAuth
// @Router      /auth/me [get]
// RefreshSession godoc
// @Summary     Re-issue a session token for the current user
// @Description Picks up any changes since the original token was signed (e.g. orgId assigned after setup).
// @Tags        hyperstrate
// @Tags        auth
// @Produce     json
// @Success     200  {object}  application.SessionResponse
// @Failure     401  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /auth/refresh [post]
func (h *Handler) RefreshSession(c *gin.Context) {
	su := sessionUserFrom(c)
	if su == nil {
		c.JSON(http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}
	session, err := h.svc.RefreshSession(c.Request.Context(), su.Email)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, session)
}

func (h *Handler) GetMe(c *gin.Context) {
	su := sessionUserFrom(c)
	if su == nil {
		c.JSON(http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}
	resp, err := h.svc.GetMe(c.Request.Context(), su.Email)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ListUsers godoc
// @Summary     List all users
// @Tags        hyperstrate
// @Tags        auth
// @Produce     json
// @Param       page     query  int  false  "Page number (default 1)"
// @Param       perPage  query  int  false  "Items per page (default 30, max 500)"
// @Success     200  {object}  pagination.Paginated[application.UserResponse]
// @Security    BearerAuth
// @Router      /auth/users [get]
func (h *Handler) ListUsers(c *gin.Context) {
	result, err := h.svc.ListUsers(c.Request.Context(), pagination.ParseSlice(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateUser godoc
// @Summary     Update a user (role / org assignment)
// @Tags        hyperstrate
// @Tags        auth
// @Accept      json
// @Produce     json
// @Param       id    path      string                       true  "User ID"
// @Param       body  body      application.UpdateUserInput  true  "Fields to update"
// @Success     200   {object}  application.UserResponse
// @Security    BearerAuth
// @Router      /auth/users/{id} [patch]
func (h *Handler) UpdateUser(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	var input application.UpdateUserInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	resp, err := h.svc.UpdateUser(c.Request.Context(), id, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ── Error mapping ─────────────────────────────────────────────────────────────

func validateParam(c *gin.Context, name string) (string, bool) {
	v := c.Param(name)
	if len(v) == 0 || len(v) > 100 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid " + name})
		return "", false
	}
	return v, true
}

func respondBindError(c *gin.Context, err error, input any) {
	summary, fields := validation.BindingErrors(err, input)
	c.JSON(http.StatusBadRequest, ErrorResponse{Error: summary, Fields: fields})
}

func respondError(c *gin.Context, err error) {
	_ = c.Error(err)
	switch {
	case errors.Is(err, domain.ErrTeamRequired):
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrOrganizationNotFound),
		errors.Is(err, domain.ErrAPIKeyNotFound),
		errors.Is(err, domain.ErrVirtualKeyNotFound),
		errors.Is(err, domain.ErrTeamNotFound),
		errors.Is(err, domain.ErrUserNotFound):
		c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrUnauthorized),
		errors.Is(err, domain.ErrKeyExpired),
		errors.Is(err, domain.ErrKeyDisabled):
		c.JSON(http.StatusUnauthorized, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrScopeViolation),
		errors.Is(err, domain.ErrForbidden):
		c.JSON(http.StatusForbidden, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrBudgetExceeded):
		c.JSON(http.StatusPaymentRequired, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrSetupAlreadyDone):
		c.JSON(http.StatusConflict, ErrorResponse{Error: err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
	}
}

// RotateAPIKey godoc
// @Summary     Rotate an API key secret
// @Description Generates a new secret for the key, revokes the old one, and returns the new plaintext once.
// @Tags        hyperstrate
// @Tags        auth
// @Produce     json
// @Param       id  path  string  true  "API key ID"
// @Success     200  {object}  APIKeyCreatedResponse
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /auth/api-keys/{id}/rotate [post]
func (h *Handler) RotateAPIKey(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	resp, err := h.svc.RotateAPIKey(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, APIKeyCreatedResponse{
		APIKeyResponse: h.enrichAPIKey(c.Request.Context(), resp.APIKeyResponse),
		Key:            resp.Key,
	})
}

// ── OIDC group mappings ───────────────────────────────────────────────────────

// ListGroupMappings godoc
// @Summary     List OIDC group → team mappings
// @Tags        hyperstrate
// @Tags        auth
// @Produce     json
// @Success     200  {object}  object{data=[]domain.OIDCGroupMapping}
// @Security    BearerAuth
// @Router      /auth/oidc/group-mappings [get]
func (h *Handler) ListGroupMappings(c *gin.Context) {
	rows, err := h.svc.ListGroupMappings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// CreateGroupMapping godoc
// @Summary     Create an OIDC group → team mapping
// @Tags        hyperstrate
// @Tags        auth
// @Accept      json
// @Produce     json
// @Param       body  body  object{groupName=string,teamId=string}  true  "Mapping"
// @Success     201   {object}  domain.OIDCGroupMapping
// @Failure     400   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /auth/oidc/group-mappings [post]
func (h *Handler) CreateGroupMapping(c *gin.Context) {
	var body struct {
		GroupName string `json:"groupName" binding:"required,max=255"`
		TeamID    string `json:"teamId"    binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondBindError(c, err, &body)
		return
	}
	m, err := h.svc.CreateGroupMapping(c.Request.Context(), body.GroupName, body.TeamID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	auditLog(c, "create", "oidc_group_mapping", m.ID)
	c.JSON(http.StatusCreated, m)
}

// DeleteGroupMapping godoc
// @Summary     Delete an OIDC group → team mapping
// @Tags        hyperstrate
// @Tags        auth
// @Param       id   path  string  true  "Mapping ID"
// @Success     204
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /auth/oidc/group-mappings/{id} [delete]
func (h *Handler) DeleteGroupMapping(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	if err := h.svc.DeleteGroupMapping(c.Request.Context(), id); err != nil {
		respondError(c, err)
		return
	}
	auditLog(c, "delete", "oidc_group_mapping", id)
	c.Status(http.StatusNoContent)
}
