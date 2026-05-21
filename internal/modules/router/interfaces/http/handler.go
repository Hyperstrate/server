package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	authDomain "hyperstrate/server/internal/modules/auth/domain"
	authHTTP "hyperstrate/server/internal/modules/auth/interfaces/http"
	"hyperstrate/server/internal/modules/router/application"
	"hyperstrate/server/internal/modules/router/domain"
	"hyperstrate/server/internal/shared/audit"
	"hyperstrate/server/internal/shared/pagination"
	"hyperstrate/server/internal/shared/validation"

	"github.com/gin-gonic/gin"
)

// ModelRef carries enriched model display info alongside the raw model ID.
type ModelRef struct {
	ID                                string  `json:"id"`
	Alias                             string  `json:"alias,omitempty"`
	DisplayName                       string  `json:"displayName"`
	Provider                          string  `json:"provider"`
	ModelDefKey                       string  `json:"modelDefKey"`
	InputPricePer1MTokens             float64 `json:"inputPricePer1MTokens"`
	CachedInputPricePer1MTokens       float64 `json:"cachedInputPricePer1MTokens,omitempty"`
	CacheWriteInputPricePer1MTokens   float64 `json:"cacheWriteInputPricePer1MTokens,omitempty"`
	CacheWrite1hInputPricePer1MTokens float64 `json:"cacheWrite1hInputPricePer1MTokens,omitempty"`
	OutputPricePer1MTokens            float64 `json:"outputPricePer1MTokens"`
}

// ModelResolverFunc returns a ModelRef for a given registered model ID.
// Returns nil when the model is not found.
type ModelResolverFunc func(ctx context.Context, modelID string) *ModelRef

// RouterTargetResponse is RouterTargetResponse enriched with a nested model relation.
type RouterTargetResponse struct {
	application.RouterTargetResponse
	Model *ModelRef `json:"model,omitempty"`
}

// Handler wires the router module's use-cases to HTTP endpoints.
type Handler struct {
	svc          application.Service
	resolveModel ModelResolverFunc
}

func NewHandler(svc application.Service, resolveModel ModelResolverFunc) *Handler {
	return &Handler{svc: svc, resolveModel: resolveModel}
}

func injectAgentSessionHeaders(c *gin.Context, options map[string]any) map[string]any {
	if options == nil {
		options = map[string]any{}
	}
	setStringHeaderOption(c, options, "X-Agent-Session-Id", "agent_session_id")
	setStringHeaderOption(c, options, "X-Session-Id", "agent_session_id")
	setStringHeaderOption(c, options, "X-Conversation-Id", "conversation_id")
	setStringHeaderOption(c, options, "X-Agent", "agent")
	setStringHeaderOption(c, options, "X-Agent-Role", "agent_role")
	setStringHeaderOption(c, options, "X-Agent-Repo", "repo")
	setStringHeaderOption(c, options, "X-Repo", "repo")
	setStringHeaderOption(c, options, "X-Agent-Branch", "branch")
	setStringHeaderOption(c, options, "X-Git-Branch", "branch")
	setStringHeaderOption(c, options, "X-Parent-Session-Id", "parent_session_id")
	setStringHeaderOption(c, options, "X-Parent-Agent-Session-Id", "parent_agent_session_id")
	setStringHeaderOption(c, options, "X-Parent-Agent", "parent_agent")
	setStringHeaderOption(c, options, "X-Agent-User-Id", "agent_user_id")
	setStringHeaderOption(c, options, "X-User-Id", "user_id")
	setStringHeaderOption(c, options, "X-Parent-Agent-User-Id", "parent_agent_user_id")
	setStringHeaderOption(c, options, "X-Parent-User-Id", "parent_user_id")
	if raw := c.GetHeader("X-Agent-Turn-Index"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			options["turn_index"] = n
		}
	}
	// Auto-detect the agent from User-Agent when not supplied via explicit header.
	if _, hasAgent := options["agent"]; !hasAgent {
		if detected := detectAgentFromUA(c.GetHeader("User-Agent")); detected != "" {
			options["agent"] = detected
		}
	}
	return options
}

// detectAgentFromUA maps known coding-agent User-Agent prefixes to a
// canonical agent label. Returns "" when the UA is unrecognised.
func detectAgentFromUA(ua string) string {
	ua = strings.ToLower(ua)
	switch {
	case strings.HasPrefix(ua, "claude-code/"), strings.HasPrefix(ua, "claude_code/"):
		return "claude_code"
	case strings.HasPrefix(ua, "cursor/"):
		return "cursor"
	case strings.HasPrefix(ua, "windsurf/"), strings.HasPrefix(ua, "codeium/"):
		return "windsurf"
	case strings.HasPrefix(ua, "continue/"), strings.Contains(ua, "continuedev"):
		return "continue"
	case strings.HasPrefix(ua, "aider/"):
		return "aider"
	case strings.HasPrefix(ua, "cline/"):
		return "cline"
	case strings.HasPrefix(ua, "codex/"):
		return "codex"
	case strings.HasPrefix(ua, "copilot/"), strings.Contains(ua, "github-copilot"):
		return "copilot"
	case strings.HasPrefix(ua, "void/"):
		return "void"
	case strings.HasPrefix(ua, "zed/") && strings.Contains(ua, "assistant"):
		return "zed"
	default:
		return ""
	}
}

func setStringHeaderOption(c *gin.Context, options map[string]any, header, key string) {
	if value := c.GetHeader(header); value != "" {
		options[key] = value
	}
}

func auditLog(c *gin.Context, action, resource, resourceID string) {
	su := authHTTP.SessionUserFrom(c)
	email := ""
	if su != nil {
		email = su.Email
	}
	audit.Log(c.Request.Context(), audit.Record{
		OrgID:      authDomain.OrgIDFromContext(c.Request.Context()),
		UserEmail:  email,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		IPAddress:  audit.IPFromRequest(c.Request),
	})
}

func (h *Handler) enrichTarget(ctx context.Context, t application.RouterTargetResponse) RouterTargetResponse {
	return RouterTargetResponse{
		RouterTargetResponse: t,
		Model:                h.resolveModel(ctx, t.ModelID),
	}
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error  string              `json:"error"`
	Fields map[string][]string `json:"fields,omitempty"`
}

// RegisterCRUDRoutes mounts all admin-managed routes (requires session + admin role).
func (h *Handler) RegisterCRUDRoutes(r gin.IRoutes) {
	// Router CRUD
	r.GET("", h.ListRouters)
	r.POST("", h.CreateRouter)
	r.GET("/:id", h.GetRouter)
	r.PATCH("/:id", h.UpdateRouter)
	r.DELETE("/:id", h.DeleteRouter)

	// Targets
	r.GET("/:id/targets", h.ListTargets)
	r.POST("/:id/targets", h.AddTarget)
	r.PATCH("/:id/targets/:targetId", h.UpdateTarget)
	r.DELETE("/:id/targets/:targetId", h.RemoveTarget)

	// Features
	r.GET("/:id/features", h.ListFeatures)
	r.POST("/:id/features", h.AddFeature)
	r.PATCH("/:id/features/:featureId", h.UpdateFeature)
	r.DELETE("/:id/features/:featureId", h.RemoveFeature)

	// Interceptors
	r.GET("/:id/interceptors", h.ListInterceptors)
	r.POST("/:id/interceptors", h.AddInterceptor)
	r.PATCH("/:id/interceptors/:interceptorId", h.UpdateInterceptor)
	r.DELETE("/:id/interceptors/:interceptorId", h.RemoveInterceptor)

	// Budget status
	r.GET("/:id/budget", h.GetBudgetStatus)
	r.GET("/:id/lint", h.LintRouter)

	// Team access (RBAC)
	r.GET("/:id/access", h.ListTeamAccess)
	r.POST("/:id/access", h.GrantTeamAccess)
	r.DELETE("/:id/access/:teamId", h.RevokeTeamAccess)

	// MCP tool discovery
	r.GET("/:id/features/:featureId/mcp/tools", h.ListMCPTools)

	// Export / Import
	r.GET("/:id/export", h.ExportRouter)
	r.POST("/import", h.ImportRouter)

	// Evaluations
	r.GET("/evaluations", h.ListEvaluations)
	r.POST("/evaluations", h.CreateEvaluation)
	r.GET("/evaluations/:evalId", h.GetEvaluation)
	r.PATCH("/evaluations/:evalId", h.UpdateEvaluation)
	r.DELETE("/evaluations/:evalId", h.DeleteEvaluation)
	r.GET("/evaluations/:evalId/cases", h.ListEvaluationCases)
	r.POST("/evaluations/:evalId/cases", h.AddEvaluationCase)
	r.DELETE("/evaluations/:evalId/cases/:caseId", h.DeleteEvaluationCase)
	r.POST("/evaluations/:evalId/run", h.RunEvaluation)
	r.GET("/evaluations/:evalId/runs", h.ListEvaluationRuns)
}

// RegisterInferRoutes mounts inference endpoints (requires API key via InferAuth).
func (h *Handler) RegisterInferRoutes(r gin.IRoutes) {
	// Metrics
	r.GET("/metrics", h.GetMetrics)

	// Native hyperstrate inference format
	r.POST("/:id/infer", h.RouteInfer)
	r.POST("/:id/infer/stream", h.RouteInferStream)
	r.POST("/:id/benchmark", h.Benchmark)

	// Provider-compatible paths directly under /router/:id (mirrors /proxy/router/:id)
	r.POST("/:id/v1/chat/completions", h.OpenAIChatCompletions)
	r.POST("/:id/v1/messages", h.AnthropicMessages)
	r.POST("/:id/v1/embeddings", h.OpenAIEmbeddings)
}

// RegisterProxyRoutes mounts the provider-compatible catch-all proxy.
// Registered under a separate prefix (/proxy/router) so the wildcard *path
// does not conflict with the CRUD sub-routes above.
// SDK baseURL: http://host/proxy/router/:id
func (h *Handler) RegisterProxyRoutes(r gin.IRouter) {
	r.Any("/:id/*path", h.Proxy)
}

// Proxy dispatches provider-compatible requests based on the path suffix.
func (h *Handler) Proxy(c *gin.Context) {
	path := c.Param("path")
	switch path {
	case "/v1/chat/completions", "/chat/completions":
		h.OpenAIChatCompletions(c)
	case "/v1/messages", "/messages":
		h.AnthropicMessages(c)
	case "/v1/embeddings", "/embeddings":
		h.OpenAIEmbeddings(c)
	default:
		c.JSON(http.StatusNotFound, gin.H{
			"error": "unsupported path " + path + "; supported: /v1/chat/completions, /v1/messages, /v1/embeddings",
		})
	}
}

// ── Router CRUD ───────────────────────────────────────────────────────────────

// ListRouters godoc
// @Summary     List routers
// @Description Returns paginated routers
// @Tags        hyperstrate
// @Tags        routers
// @Produce     json
// @Param       page     query  int  false  "Page number (default 1)"
// @Param       perPage  query  int  false  "Items per page (default 30, max 500)"
// @Param       query    query  string  false  "Search by router ID, name, or description"
// @Success     200  {object}  pagination.Paginated[application.RouterResponse]
// @Failure     500  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router [get]
func (h *Handler) ListRouters(c *gin.Context) {
	result, err := h.svc.ListRouters(c.Request.Context(), pagination.ParseSlice(c), c.Query("query"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateRouter godoc
// @Summary     Create a router
// @Description Creates a new router with the given name, description, and routing strategy
// @Tags        hyperstrate
// @Tags        routers
// @Accept      json
// @Produce     json
// @Param       body  body      application.CreateRouterInput  true  "Router input"
// @Success     201   {object}  application.RouterResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     500   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router [post]
func (h *Handler) CreateRouter(c *gin.Context) {
	var input application.CreateRouterInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.CreateRouter(c.Request.Context(), input)
	if err != nil {
		respondError(c, err)
		return
	}
	auditLog(c, "create", "router", result.ID)
	c.JSON(http.StatusCreated, result)
}

// GetRouter godoc
// @Summary     Get a router
// @Description Returns a single router by ID
// @Tags        hyperstrate
// @Tags        routers
// @Produce     json
// @Param       id   path      string  true  "Router ID"
// @Success     200  {object}  application.RouterResponse
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id} [get]
func (h *Handler) GetRouter(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	result, err := h.svc.GetRouter(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateRouter godoc
// @Summary     Update a router
// @Description Updates the name, description, status, or strategy of a router
// @Tags        hyperstrate
// @Tags        routers
// @Accept      json
// @Produce     json
// @Param       id    path      string                        true  "Router ID"
// @Param       body  body      application.UpdateRouterInput true  "Fields to update"
// @Success     200   {object}  application.RouterResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id} [patch]
func (h *Handler) UpdateRouter(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	var input application.UpdateRouterInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.UpdateRouter(c.Request.Context(), id, input)
	if err != nil {
		respondError(c, err)
		return
	}
	auditLog(c, "update", "router", id)
	c.JSON(http.StatusOK, result)
}

// DeleteRouter godoc
// @Summary     Delete a router
// @Description Deletes a router and all its targets, features, and interceptors
// @Tags        hyperstrate
// @Tags        routers
// @Produce     json
// @Param       id   path  string  true  "Router ID"
// @Success     204
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id} [delete]
func (h *Handler) DeleteRouter(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	if err := h.svc.DeleteRouter(c.Request.Context(), id); err != nil {
		respondError(c, err)
		return
	}
	auditLog(c, "delete", "router", id)
	c.Status(http.StatusNoContent)
}

// ── Targets ───────────────────────────────────────────────────────────────────

// ListTargets godoc
// @Summary     List router targets
// @Description Returns all targets (model assignments) for a router
// @Tags        hyperstrate
// @Tags        routers
// @Produce     json
// @Param       id   path      string  true  "Router ID"
// @Success     200  {array}   RouterTargetResponse
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/targets [get]
func (h *Handler) ListTargets(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	result, err := h.svc.ListTargets(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	enriched := make([]RouterTargetResponse, len(result))
	for i, t := range result {
		enriched[i] = h.enrichTarget(c.Request.Context(), t)
	}
	c.JSON(http.StatusOK, enriched)
}

// AddTarget godoc
// @Summary     Add a target to a router
// @Description Attaches a model to a router with strategy-specific routing parameters
// @Tags        hyperstrate
// @Tags        routers
// @Accept      json
// @Produce     json
// @Param       id    path      string                      true  "Router ID"
// @Param       body  body      application.AddTargetInput  true  "Target input"
// @Success     201   {object}  RouterTargetResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/targets [post]
func (h *Handler) AddTarget(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	var input application.AddTargetInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.AddTarget(c.Request.Context(), id, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, h.enrichTarget(c.Request.Context(), *result))
}

// UpdateTarget godoc
// @Summary     Update a router target
// @Description Updates weight, percentage, priority, or enabled state of a target
// @Tags        hyperstrate
// @Tags        routers
// @Accept      json
// @Produce     json
// @Param       id        path      string                         true  "Router ID"
// @Param       targetId  path      string                         true  "Target ID"
// @Param       body      body      application.UpdateTargetInput  true  "Fields to update"
// @Success     200       {object}  RouterTargetResponse
// @Failure     400       {object}  ErrorResponse
// @Failure     404       {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/targets/{targetId} [patch]
func (h *Handler) UpdateTarget(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	targetId, ok := validateParam(c, "targetId")
	if !ok {
		return
	}
	var input application.UpdateTargetInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.UpdateTarget(c.Request.Context(), id, targetId, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, h.enrichTarget(c.Request.Context(), *result))
}

// RemoveTarget godoc
// @Summary     Remove a router target
// @Description Detaches a model from a router
// @Tags        hyperstrate
// @Tags        routers
// @Produce     json
// @Param       id        path  string  true  "Router ID"
// @Param       targetId  path  string  true  "Target ID"
// @Success     204
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/targets/{targetId} [delete]
func (h *Handler) RemoveTarget(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	targetId, ok := validateParam(c, "targetId")
	if !ok {
		return
	}
	if err := h.svc.RemoveTarget(c.Request.Context(), id, targetId); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ── Features ──────────────────────────────────────────────────────────────────

// ListFeatures godoc
// @Summary     List router features
// @Description Returns all pipeline features attached to a router
// @Tags        hyperstrate
// @Tags        routers
// @Produce     json
// @Param       id   path      string  true  "Router ID"
// @Success     200  {array}   application.RouterFeatureResponse
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/features [get]
func (h *Handler) ListFeatures(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	result, err := h.svc.ListFeatures(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// AddFeature godoc
// @Summary     Add a feature to a router
// @Description Attaches a pipeline feature (e.g. retry, cache, rate-limit) to a router
// @Tags        hyperstrate
// @Tags        routers
// @Accept      json
// @Produce     json
// @Param       id    path      string                      true  "Router ID"
// @Param       body  body      application.AddFeatureInput true  "Feature input"
// @Success     201   {object}  application.RouterFeatureResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/features [post]
func (h *Handler) AddFeature(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	var input application.AddFeatureInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.AddFeature(c.Request.Context(), id, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// UpdateFeature godoc
// @Summary     Update a router feature
// @Description Updates the config, execution order, or enabled state of a feature
// @Tags        hyperstrate
// @Tags        routers
// @Accept      json
// @Produce     json
// @Param       id         path      string                          true  "Router ID"
// @Param       featureId  path      string                          true  "Feature ID"
// @Param       body       body      application.UpdateFeatureInput  true  "Fields to update"
// @Success     200        {object}  application.RouterFeatureResponse
// @Failure     400        {object}  ErrorResponse
// @Failure     404        {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/features/{featureId} [patch]
func (h *Handler) UpdateFeature(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	featureId, ok := validateParam(c, "featureId")
	if !ok {
		return
	}
	var input application.UpdateFeatureInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.UpdateFeature(c.Request.Context(), id, featureId, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// RemoveFeature godoc
// @Summary     Remove a router feature
// @Description Detaches a pipeline feature from a router
// @Tags        hyperstrate
// @Tags        routers
// @Produce     json
// @Param       id         path  string  true  "Router ID"
// @Param       featureId  path  string  true  "Feature ID"
// @Success     204
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/features/{featureId} [delete]
func (h *Handler) RemoveFeature(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	featureId, ok := validateParam(c, "featureId")
	if !ok {
		return
	}
	if err := h.svc.RemoveFeature(c.Request.Context(), id, featureId); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ── Interceptors ──────────────────────────────────────────────────────────────

// ListInterceptors godoc
// @Summary     List router interceptors
// @Description Returns all pre-routing interceptors attached to a router, ordered by execution_order
// @Tags        hyperstrate
// @Tags        routers
// @Produce     json
// @Param       id   path      string  true  "Router ID"
// @Success     200  {array}   application.RouterInterceptorResponse
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/interceptors [get]
func (h *Handler) ListInterceptors(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	result, err := h.svc.ListInterceptors(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// AddInterceptor godoc
// @Summary     Add an interceptor to a router
// @Description Attaches a pre-routing interceptor (e.g. semantic_classifier, content_filter) to a router
// @Tags        hyperstrate
// @Tags        routers
// @Accept      json
// @Produce     json
// @Param       id    path      string                           true  "Router ID"
// @Param       body  body      application.AddInterceptorInput  true  "Interceptor input"
// @Success     201   {object}  application.RouterInterceptorResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/interceptors [post]
func (h *Handler) AddInterceptor(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	var input application.AddInterceptorInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.AddInterceptor(c.Request.Context(), id, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// UpdateInterceptor godoc
// @Summary     Update a router interceptor
// @Description Updates the config, execution order, or enabled state of an interceptor
// @Tags        hyperstrate
// @Tags        routers
// @Accept      json
// @Produce     json
// @Param       id             path      string                              true  "Router ID"
// @Param       interceptorId  path      string                              true  "Interceptor ID"
// @Param       body           body      application.UpdateInterceptorInput  true  "Fields to update"
// @Success     200            {object}  application.RouterInterceptorResponse
// @Failure     400            {object}  ErrorResponse
// @Failure     404            {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/interceptors/{interceptorId} [patch]
func (h *Handler) UpdateInterceptor(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	interceptorId, ok := validateParam(c, "interceptorId")
	if !ok {
		return
	}
	var input application.UpdateInterceptorInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.UpdateInterceptor(c.Request.Context(), id, interceptorId, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// RemoveInterceptor godoc
// @Summary     Remove a router interceptor
// @Description Detaches an interceptor from a router
// @Tags        hyperstrate
// @Tags        routers
// @Produce     json
// @Param       id             path  string  true  "Router ID"
// @Param       interceptorId  path  string  true  "Interceptor ID"
// @Success     204
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/interceptors/{interceptorId} [delete]
func (h *Handler) RemoveInterceptor(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	interceptorId, ok := validateParam(c, "interceptorId")
	if !ok {
		return
	}
	if err := h.svc.RemoveInterceptor(c.Request.Context(), id, interceptorId); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ── Budget ────────────────────────────────────────────────────────────────────

// GetBudgetStatus godoc
// @Summary     Get current budget usage for a router
// @Tags        hyperstrate
// @Tags        routers
// @Produce     json
// @Param       id   path      string  true  "Router ID"
// @Success     200  {object}  application.BudgetStatus
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/budget [get]
func (h *Handler) GetBudgetStatus(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	result, err := h.svc.GetBudgetStatus(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// LintRouter godoc
// @Summary     Check router compatibility
// @Description Returns warnings and errors for potentially unsafe feature/interceptor combinations
// @Tags        hyperstrate
// @Tags        routers
// @Produce     json
// @Param       id  path  string  true  "Router ID"
// @Success     200  {object}  application.RouterLintResponse
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/lint [get]
func (h *Handler) LintRouter(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	result, err := h.svc.LintRouter(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ── Metrics ───────────────────────────────────────────────────────────────────

// GetMetrics godoc
// @Summary     Get router pipeline metrics
// @Description Returns runtime counters and average latencies for all routers
// @Tags        hyperstrate
// @Tags        routers
// @Produce     json
// @Success     200  {array}  application.RouterMetricSnapshot
// @Security    BearerAuth
// @Router      /router/metrics [get]
func (h *Handler) GetMetrics(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.MetricsSnapshot())
}

// ── Inference ─────────────────────────────────────────────────────────────────

// RouteInfer godoc
// @Summary     Route an inference request
// @Description Selects a target model according to the router's strategy and forwards the inference request
// @Tags        hyperstrate
// @Tags        routers
// @Accept      json
// @Produce     json
// @Param       id    path      string                       true  "Router ID"
// @Param       body  body      application.RouteInferInput  true  "Inference request"
// @Success     200   {object}  application.RouteInferResult
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Failure     422   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/infer [post]
func (h *Handler) RouteInfer(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	var input application.RouteInferInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	if c.Query("dryRun") == "true" {
		h.handleDryRun(c, id, input.Fields["prompt"])
		return
	}
	input.Options = injectAgentSessionHeaders(c, input.Options)
	result, err := h.svc.RouteInfer(c.Request.Context(), id, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// RouteInferStream godoc
// @Summary     Route a streaming inference request
// @Description Streams inference deltas as server-sent events (text/event-stream)
// @Tags        hyperstrate
// @Tags        routers
// @Accept      json
// @Produce     text/event-stream
// @Param       id    path      string                       true  "Router ID"
// @Param       body  body      application.RouteInferInput  true  "Inference request"
// @Success     200
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/infer/stream [post]
func (h *Handler) RouteInferStream(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	var input application.RouteInferInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	input.Options = injectAgentSessionHeaders(c, input.Options)

	ch, err := h.svc.RouteInferStream(c.Request.Context(), id, input)
	if err != nil {
		respondError(c, err)
		return
	}

	writeSSEStream(c, ch)
}

// OpenAIChatCompletions godoc
// @Summary     OpenAI-compatible chat completions
// @Description Accepts an OpenAI /v1/chat/completions request body and routes it through the router pipeline.
// @Description When stream=true the response is an SSE stream of chat.completion.chunk objects.
// @Tags        hyperstrate
// @Tags        routers
// @Accept      json
// @Produce     json
// @Param       id    path      string  true  "Router ID"
// @Param       body  body      object  true  "OpenAI chat completions request"
// @Success     200   {object}  object
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/v1/chat/completions [post]
func (h *Handler) OpenAIChatCompletions(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	var req openAIChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err, &req)
		return
	}

	parsed := req.parseMessages()
	if parsed.UserContent == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "no user message found in messages array"})
		return
	}

	if c.Query("dryRun") == "true" {
		h.handleDryRun(c, id, parsed.UserContent)
		return
	}

	// Build the fields map. History is JSON-encoded so it flows through the
	// pipeline without changing internal interface signatures.
	fields := map[string]string{
		"prompt": parsed.UserContent,
	}
	if parsed.SystemPrompt != "" {
		fields["systemPrompt"] = parsed.SystemPrompt
	}
	if len(parsed.History) > 0 {
		histJSON, _ := json.Marshal(parsed.History)
		fields["_history"] = string(histJSON)
	}
	if len(parsed.ImageURLs) > 0 {
		imgJSON, _ := json.Marshal(parsed.ImageURLs)
		fields["image"] = string(imgJSON)
	}

	options := req.toOptions()
	options = injectAgentSessionHeaders(c, options)

	input := application.RouteInferInput{Fields: fields, Options: options}

	if req.Stream {
		ch, err := h.svc.RouteInferStream(c.Request.Context(), id, input)
		if err != nil {
			respondError(c, err)
			return
		}
		writeOpenAISSEStream(c, ch, req.Model)
		return
	}

	result, err := h.svc.RouteInfer(c.Request.Context(), id, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, buildOpenAIResponse(result, req.Model))
}

// ── Dry-run ───────────────────────────────────────────────────────────────────

// estimateTokens approximates the token count using the ~4 chars/token heuristic.
func estimateTokens(text string) int {
	if n := len(text) / 4; n > 0 {
		return n
	}
	if text != "" {
		return 1
	}
	return 0
}

// handleDryRun returns a DryRunResult without calling any model. It enumerates
// the router's enabled targets, resolves each model's catalog pricing, and
// estimates cost based on the prompt token count.
func (h *Handler) handleDryRun(c *gin.Context, routerID, promptText string) {
	targets, err := h.svc.ListTargets(c.Request.Context(), routerID)
	if err != nil {
		respondError(c, err)
		return
	}

	inputTok := estimateTokens(promptText)
	dryTargets := make([]application.DryRunTarget, 0, len(targets))
	for _, t := range targets {
		if !t.IsEnabled {
			continue
		}
		ref := h.resolveModel(c.Request.Context(), t.ModelID)
		if ref == nil {
			continue
		}
		estimated := float64(inputTok) * ref.InputPricePer1MTokens / 1_000_000
		dryTargets = append(dryTargets, application.DryRunTarget{
			ModelID:                           t.ModelID,
			ModelDefKey:                       ref.ModelDefKey,
			DisplayName:                       ref.DisplayName,
			Provider:                          ref.Provider,
			InputPricePer1MTokens:             ref.InputPricePer1MTokens,
			CachedInputPricePer1MTokens:       ref.CachedInputPricePer1MTokens,
			CacheWriteInputPricePer1MTokens:   ref.CacheWriteInputPricePer1MTokens,
			CacheWrite1hInputPricePer1MTokens: ref.CacheWrite1hInputPricePer1MTokens,
			OutputPricePer1MTokens:            ref.OutputPricePer1MTokens,
			EstimatedInputCostUSD:             estimated,
		})
	}

	c.JSON(http.StatusOK, application.DryRunResult{
		EstimatedInputTokens: inputTok,
		Targets:              dryTargets,
	})
}

// ── SSE helpers ───────────────────────────────────────────────────────────────

// writeSSEStream drains a StreamChunk channel and writes each delta as a plain SSE event.
// Format: data: {"delta":"...","done":false}\n\n
func writeSSEStream(c *gin.Context, ch <-chan application.StreamChunk) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	w := c.Writer
	for chunk := range ch {
		if chunk.Err != nil {
			data, _ := json.Marshal(map[string]string{"error": chunk.Err.Error()})
			fmt.Fprintf(w, "data: %s\n\n", data)
			w.Flush()
			return
		}
		payload := map[string]any{"delta": chunk.Delta, "done": chunk.Done}
		if chunk.Done {
			if chunk.SelectedModelID != "" {
				payload["selectedModelId"] = chunk.SelectedModelID
			}
			if chunk.ModelDefKey != "" {
				payload["selectedModelDefKey"] = chunk.ModelDefKey
			}
		}
		data, _ := json.Marshal(payload)
		fmt.Fprintf(w, "data: %s\n\n", data)
		w.Flush()
	}
}

// writeOpenAISSEStream drains a StreamChunk channel and writes OpenAI-format SSE chunks.
func writeOpenAISSEStream(c *gin.Context, ch <-chan application.StreamChunk, requestedModel string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	w := c.Writer
	model := requestedModel
	first := true
	var inputTok, outputTok int

	for chunk := range ch {
		if chunk.Err != nil {
			data, _ := json.Marshal(map[string]string{"error": chunk.Err.Error()})
			fmt.Fprintf(w, "data: %s\n\n", data)
			w.Flush()
			return
		}
		if chunk.Done {
			inputTok = int(chunk.InputTokens)
			outputTok = int(chunk.OutputTokens)
			if len(chunk.ToolCalls) > 0 {
				var calls []openAIToolCall
				if json.Unmarshal(chunk.ToolCalls, &calls) == nil && len(calls) > 0 {
					data, _ := json.Marshal(buildOpenAIStreamToolCallsDone(id, model, calls, inputTok, outputTok))
					fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
					w.Flush()
					return
				}
			}
			break
		}
		if first {
			roleChunk := buildOpenAIStreamChunk(id, model, "")
			roleChunk.Choices[0].Delta.Role = "assistant"
			roleChunk.Choices[0].Delta.Content = nil
			data, _ := json.Marshal(roleChunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			first = false
		}
		if chunk.Delta != "" {
			data, _ := json.Marshal(buildOpenAIStreamChunk(id, model, chunk.Delta))
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		w.Flush()
	}

	data, _ := json.Marshal(buildOpenAIStreamDone(id, model, inputTok, outputTok))
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
	w.Flush()
}

// ── Team access (RBAC) ────────────────────────────────────────────────────────

// ListTeamAccess godoc
// @Summary     List team access grants for a router
// @Description Returns all team IDs that have explicit inference access to the router. An empty list means the router is open to all authenticated callers.
// @Tags        hyperstrate
// @Tags        routers
// @Produce     json
// @Param       id   path      string  true  "Router ID"
// @Success     200  {array}   domain.RouterTeamAccess
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/access [get]
func (h *Handler) ListTeamAccess(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	result, err := h.svc.ListRouterTeamAccess(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GrantTeamAccess godoc
// @Summary     Grant a team access to a router
// @Description Adds the given team to the router's explicit allow-list
// @Tags        hyperstrate
// @Tags        routers
// @Accept      json
// @Produce     json
// @Param       id    path      string                    true  "Router ID"
// @Param       body  body      object{teamId=string}     true  "Team ID to grant"
// @Success     204
// @Failure     400  {object}  ErrorResponse
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/access [post]
func (h *Handler) GrantTeamAccess(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	var body struct {
		TeamID string `json:"teamId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondBindError(c, err, &body)
		return
	}
	if err := h.svc.GrantRouterTeamAccess(c.Request.Context(), id, body.TeamID); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// RevokeTeamAccess godoc
// @Summary     Revoke a team's access to a router
// @Description Removes the given team from the router's explicit allow-list
// @Tags        hyperstrate
// @Tags        routers
// @Param       id      path  string  true  "Router ID"
// @Param       teamId  path  string  true  "Team ID to revoke"
// @Success     204
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/access/{teamId} [delete]
func (h *Handler) RevokeTeamAccess(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	teamID, ok := validateParam(c, "teamId")
	if !ok {
		return
	}
	if err := h.svc.RevokeRouterTeamAccess(c.Request.Context(), id, teamID); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
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
	case errors.Is(err, domain.ErrRouterNotFound):
		c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrRouterTargetNotFound):
		c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrRouterFeatureNotFound):
		c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrRouterInterceptorNotFound):
		c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrRouterInactive):
		c.JSON(http.StatusUnprocessableEntity, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrNoTargetsAvailable):
		c.JSON(http.StatusUnprocessableEntity, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrAllTargetsFailed):
		c.JSON(http.StatusBadGateway, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrInvalidPercentages):
		c.JSON(http.StatusUnprocessableEntity, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrRateLimitExceeded):
		c.JSON(http.StatusTooManyRequests, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrBudgetExceeded):
		c.JSON(http.StatusPaymentRequired, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrRequestBlocked):
		c.JSON(http.StatusForbidden, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrMissingEmbedModel):
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrMissingBlockedPatterns):
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrTeamNotAllowed):
		c.JSON(http.StatusForbidden, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrStreamingUnsupported):
		c.JSON(http.StatusUnprocessableEntity, ErrorResponse{Error: err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
	}
}

// ── Benchmarking / load test ──────────────────────────────────────────────────

// BenchmarkResult is the report from a load test run.
type BenchmarkResult struct {
	TotalRequests  int     `json:"totalRequests"`
	Concurrency    int     `json:"concurrency"`
	SuccessCount   int     `json:"successCount"`
	ErrorCount     int     `json:"errorCount"`
	DurationMs     int64   `json:"durationMs"`
	AvgLatencyMs   float64 `json:"avgLatencyMs"`
	P50LatencyMs   int64   `json:"p50LatencyMs"`
	P95LatencyMs   int64   `json:"p95LatencyMs"`
	P99LatencyMs   int64   `json:"p99LatencyMs"`
	TotalCostUSD   float64 `json:"totalCostUsd"`
	RequestsPerSec float64 `json:"requestsPerSec"`
}

// Benchmark godoc
// @Summary     Run a load test against a router
// @Description Fires N concurrent inference requests and returns P50/P95/P99 latency, throughput, and cost
// @Tags        hyperstrate
// @Tags        routers
// @Accept      json
// @Produce     json
// @Param       id    path      string              true  "Router ID"
// @Param       body  body      object{prompt=string,totalRequests=int,concurrency=int,options=object}  true  "Benchmark input"
// @Success     200   {object}  BenchmarkResult
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/benchmark [post]
func (h *Handler) Benchmark(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	var body struct {
		Prompt        string         `json:"prompt"         binding:"required"`
		TotalRequests int            `json:"totalRequests"`
		Concurrency   int            `json:"concurrency"`
		Options       map[string]any `json:"options"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondBindError(c, err, &body)
		return
	}
	if body.TotalRequests <= 0 {
		body.TotalRequests = 10
	}
	if body.TotalRequests > 500 {
		body.TotalRequests = 500
	}
	if body.Concurrency <= 0 {
		body.Concurrency = 5
	}
	if body.Concurrency > body.TotalRequests {
		body.Concurrency = body.TotalRequests
	}

	type result struct {
		latencyMs int64
		costUSD   float64
		err       error
	}

	work := make(chan struct{}, body.TotalRequests)
	for i := 0; i < body.TotalRequests; i++ {
		work <- struct{}{}
	}
	close(work)

	results := make([]result, body.TotalRequests)
	var wg sync.WaitGroup
	resultCh := make(chan result, body.TotalRequests)
	sem := make(chan struct{}, body.Concurrency)

	benchStart := time.Now()
	for range work {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			t0 := time.Now()
			r, err := h.svc.RouteInfer(c.Request.Context(), id, application.RouteInferInput{
				Fields:  map[string]string{"prompt": body.Prompt},
				Options: body.Options,
			})
			ms := time.Since(t0).Milliseconds()
			if err != nil {
				resultCh <- result{latencyMs: ms, err: err}
			} else {
				resultCh <- result{latencyMs: ms, costUSD: r.CostUSD}
			}
		}()
	}
	go func() { wg.Wait(); close(resultCh) }()

	i := 0
	for r := range resultCh {
		results[i] = r
		i++
	}
	totalDuration := time.Since(benchStart)

	successCount, errCount := 0, 0
	var totalCost float64
	latencies := make([]int64, 0, body.TotalRequests)
	for _, r := range results[:i] {
		if r.err != nil {
			errCount++
		} else {
			successCount++
			latencies = append(latencies, r.latencyMs)
			totalCost += r.costUSD
		}
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	var avg float64
	var p50, p95, p99 int64
	n := len(latencies)
	if n > 0 {
		var sum int64
		for _, v := range latencies {
			sum += v
		}
		avg = float64(sum) / float64(n)
		p50 = latencies[n*50/100]
		p95 = latencies[int(float64(n)*0.95)]
		if int(float64(n)*0.95) >= n {
			p95 = latencies[n-1]
		}
		p99 = latencies[int(float64(n)*0.99)]
		if int(float64(n)*0.99) >= n {
			p99 = latencies[n-1]
		}
	}

	rps := 0.0
	if totalDuration.Seconds() > 0 {
		rps = float64(i) / totalDuration.Seconds()
	}

	c.JSON(http.StatusOK, BenchmarkResult{
		TotalRequests:  i,
		Concurrency:    body.Concurrency,
		SuccessCount:   successCount,
		ErrorCount:     errCount,
		DurationMs:     totalDuration.Milliseconds(),
		AvgLatencyMs:   avg,
		P50LatencyMs:   p50,
		P95LatencyMs:   p95,
		P99LatencyMs:   p99,
		TotalCostUSD:   totalCost,
		RequestsPerSec: rps,
	})
}

// ListMCPTools godoc
// @Summary     Preview MCP tools
// @Description Calls ListTools on all MCP servers attached to a router's mcp_tools feature
// @Tags        hyperstrate
// @Tags        routers
// @Produce     json
// @Param       id         path  string  true  "Router ID"
// @Param       featureId  path  string  true  "Feature ID"
// @Success     200  {array}   application.MCPServerTools
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/features/{featureId}/mcp/tools [get]
func (h *Handler) ListMCPTools(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	featureID, ok := validateParam(c, "featureId")
	if !ok {
		return
	}
	result, err := h.svc.ListMCPTools(c.Request.Context(), id, featureID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ── Export / Import ───────────────────────────────────────────────────────────

// ExportRouter godoc
// @Summary     Export router configuration
// @Description Returns a portable JSON snapshot of the router, its targets, features, and interceptors
// @Tags        hyperstrate
// @Tags        routers
// @Produce     json
// @Param       id  path  string  true  "Router ID"
// @Success     200  {object}  application.RouterExport
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/{id}/export [get]
func (h *Handler) ExportRouter(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	result, err := h.svc.ExportRouter(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ImportRouter godoc
// @Summary     Import router configuration
// @Description Creates a new router from an exported snapshot; model references are resolved by definition key
// @Tags        hyperstrate
// @Tags        routers
// @Accept      json
// @Produce     json
// @Param       body  body  application.ImportRouterInput  true  "Router export payload"
// @Success     201   {object}  application.RouterResponse
// @Failure     400   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/import [post]
func (h *Handler) ImportRouter(c *gin.Context) {
	var input application.ImportRouterInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	if input.NameOverride != "" {
		input.Router.Name = input.NameOverride
	}
	result, err := h.svc.ImportRouter(c.Request.Context(), input.RouterExport)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ── Evaluations ───────────────────────────────────────────────────────────────

// ListEvaluations godoc
// @Summary     List evaluations
// @Tags        hyperstrate
// @Tags        routers
// @Produce     json
// @Param       routerId  query  string  false  "Filter by router ID"
// @Param       page      query  int     false  "Page number (default 1)"
// @Param       perPage   query  int     false  "Items per page (default 30, max 500)"
// @Success     200  {object}  pagination.Paginated[application.EvaluationResponse]
// @Security    BearerAuth
// @Router      /router/evaluations [get]
func (h *Handler) ListEvaluations(c *gin.Context) {
	slice := pagination.ParseSlice(c)
	routerID := c.Query("routerId")
	result, err := h.svc.ListEvaluations(c.Request.Context(), routerID, slice)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateEvaluation godoc
// @Summary     Create an evaluation
// @Tags        hyperstrate
// @Tags        routers
// @Accept      json
// @Produce     json
// @Param       body  body  application.CreateEvaluationInput  true  "Evaluation input"
// @Success     201   {object}  application.EvaluationResponse
// @Failure     400   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/evaluations [post]
func (h *Handler) CreateEvaluation(c *gin.Context) {
	var input application.CreateEvaluationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.CreateEvaluation(c.Request.Context(), input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// GetEvaluation godoc
// @Summary     Get an evaluation
// @Tags        hyperstrate
// @Tags        routers
// @Produce     json
// @Param       evalId  path  string  true  "Evaluation ID"
// @Success     200  {object}  application.EvaluationResponse
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/evaluations/{evalId} [get]
func (h *Handler) GetEvaluation(c *gin.Context) {
	id, ok := validateParam(c, "evalId")
	if !ok {
		return
	}
	result, err := h.svc.GetEvaluation(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateEvaluation godoc
// @Summary     Update an evaluation
// @Tags        hyperstrate
// @Tags        routers
// @Accept      json
// @Produce     json
// @Param       evalId  path  string                           true  "Evaluation ID"
// @Param       body    body  application.UpdateEvaluationInput  true  "Fields to update"
// @Success     200  {object}  application.EvaluationResponse
// @Failure     400  {object}  ErrorResponse
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/evaluations/{evalId} [patch]
func (h *Handler) UpdateEvaluation(c *gin.Context) {
	id, ok := validateParam(c, "evalId")
	if !ok {
		return
	}
	var input application.UpdateEvaluationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.UpdateEvaluation(c.Request.Context(), id, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteEvaluation godoc
// @Summary     Delete an evaluation
// @Tags        hyperstrate
// @Tags        routers
// @Param       evalId  path  string  true  "Evaluation ID"
// @Success     204
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/evaluations/{evalId} [delete]
func (h *Handler) DeleteEvaluation(c *gin.Context) {
	id, ok := validateParam(c, "evalId")
	if !ok {
		return
	}
	if err := h.svc.DeleteEvaluation(c.Request.Context(), id); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ListEvaluationCases godoc
// @Summary     List evaluation cases
// @Tags        hyperstrate
// @Tags        routers
// @Produce     json
// @Param       evalId  path  string  true  "Evaluation ID"
// @Success     200  {array}  application.EvaluationCaseResponse
// @Security    BearerAuth
// @Router      /router/evaluations/{evalId}/cases [get]
func (h *Handler) ListEvaluationCases(c *gin.Context) {
	id, ok := validateParam(c, "evalId")
	if !ok {
		return
	}
	result, err := h.svc.ListEvaluationCases(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// AddEvaluationCase godoc
// @Summary     Add a test case to an evaluation
// @Tags        hyperstrate
// @Tags        routers
// @Accept      json
// @Produce     json
// @Param       evalId  path  string                         true  "Evaluation ID"
// @Param       body    body  application.EvaluationCaseInput  true  "Case input"
// @Success     201  {object}  application.EvaluationCaseResponse
// @Failure     400  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/evaluations/{evalId}/cases [post]
func (h *Handler) AddEvaluationCase(c *gin.Context) {
	id, ok := validateParam(c, "evalId")
	if !ok {
		return
	}
	var input application.EvaluationCaseInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.AddEvaluationCase(c.Request.Context(), id, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// DeleteEvaluationCase godoc
// @Summary     Delete a test case
// @Tags        hyperstrate
// @Tags        routers
// @Param       evalId  path  string  true  "Evaluation ID"
// @Param       caseId  path  string  true  "Case ID"
// @Success     204
// @Security    BearerAuth
// @Router      /router/evaluations/{evalId}/cases/{caseId} [delete]
func (h *Handler) DeleteEvaluationCase(c *gin.Context) {
	evalID, ok := validateParam(c, "evalId")
	if !ok {
		return
	}
	caseID, ok := validateParam(c, "caseId")
	if !ok {
		return
	}
	if err := h.svc.DeleteEvaluationCase(c.Request.Context(), evalID, caseID); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// RunEvaluation godoc
// @Summary     Run all test cases in an evaluation
// @Tags        hyperstrate
// @Tags        routers
// @Produce     json
// @Param       evalId        path   string  true   "Evaluation ID"
// @Param       judgeModelId  query  string  false  "Registered model ID used as LLM judge for 'llm' score method"
// @Success     200  {object}  application.EvaluationRunResponse
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /router/evaluations/{evalId}/run [post]
func (h *Handler) RunEvaluation(c *gin.Context) {
	id, ok := validateParam(c, "evalId")
	if !ok {
		return
	}
	judgeModelID := c.Query("judgeModelId")
	result, err := h.svc.RunEvaluation(c.Request.Context(), id, judgeModelID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListEvaluationRuns godoc
// @Summary     List past evaluation runs
// @Tags        hyperstrate
// @Tags        routers
// @Produce     json
// @Param       evalId   path   string  true   "Evaluation ID"
// @Param       page     query  int     false  "Page number (default 1)"
// @Param       perPage  query  int     false  "Items per page (default 30, max 500)"
// @Success     200  {object}  pagination.Paginated[application.EvaluationRunResponse]
// @Security    BearerAuth
// @Router      /router/evaluations/{evalId}/runs [get]
func (h *Handler) ListEvaluationRuns(c *gin.Context) {
	id, ok := validateParam(c, "evalId")
	if !ok {
		return
	}
	slice := pagination.ParseSlice(c)
	result, err := h.svc.ListEvaluationRuns(c.Request.Context(), id, slice)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
