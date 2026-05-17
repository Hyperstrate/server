package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hyperstrate/server/internal/config"
	"hyperstrate/server/internal/modules/ai/application"
	"hyperstrate/server/internal/modules/ai/domain"
	"hyperstrate/server/internal/shared/pagination"
	"hyperstrate/server/internal/shared/validation"

	"github.com/gin-gonic/gin"
)

// ModelRef is the enriched model relation returned inside conversation responses.
type ModelRef struct {
	ID          string `json:"id"`
	Alias       string `json:"alias,omitempty"`
	DisplayName string `json:"displayName"`
	Provider    string `json:"provider"`
	ModelDefKey string `json:"modelDefKey"`
}

// ConversationResponse wraps the service DTO with a nested model relation.
type ConversationResponse struct {
	application.ConversationResponse
	Model *ModelRef `json:"model,omitempty"`
}

// Handler wires the AI module's use-cases to HTTP endpoints.
type Handler struct {
	svc           application.Service
	ollamaBaseURL string
}

func NewHandler(svc application.Service, cfg config.Config) *Handler {
	ollamaBaseURL := strings.TrimSpace(cfg.OllamaBaseURL)
	if ollamaBaseURL == "" {
		ollamaBaseURL = config.DefaultOllamaBaseURL
	}
	return &Handler{svc: svc, ollamaBaseURL: ollamaBaseURL}
}

func (h *Handler) enrichConversation(ctx context.Context, c application.ConversationResponse) ConversationResponse {
	out := ConversationResponse{ConversationResponse: c}
	if c.ModelID == "" {
		return out
	}
	m, err := h.svc.GetModel(ctx, c.ModelID)
	if err != nil || m == nil {
		return out
	}
	out.Model = &ModelRef{
		ID:          m.ID,
		Alias:       m.Alias,
		DisplayName: m.DisplayName,
		Provider:    string(m.Provider),
		ModelDefKey: m.ModelDefinitionKey,
	}
	return out
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error  string              `json:"error"`
	Fields map[string][]string `json:"fields,omitempty"`
}

// RegisterAdminRoutes mounts admin-managed routes (requires session + admin role).
func (h *Handler) RegisterAdminRoutes(r gin.IRoutes) {
	r.GET("/catalog", h.ListCatalog)
	r.GET("/discover", h.DiscoverOllamaModels)

	r.GET("/models", h.ListModels)
	r.POST("/models", h.RegisterModel)
	r.GET("/models/configurations", h.ListModelConfigurations)
	r.GET("/models/:id", h.GetModel)
	r.PATCH("/models/:id", h.UpdateModel)
	r.DELETE("/models/:id", h.DeleteModel)
	r.PUT("/models/:id/configuration", h.SetModelConfiguration)
	r.GET("/models/:id/configuration", h.GetModelConfiguration)
	r.POST("/models/:id/rotate-key", h.RotateAPIKey)
	r.GET("/models/:id/key-rotations", h.ListKeyRotations)

	r.GET("/conversations", h.ListConversations)
	r.POST("/conversations", h.CreateConversation)
	r.GET("/conversations/:id", h.GetConversation)
	r.DELETE("/conversations/:id", h.DeleteConversation)
	r.GET("/conversations/:id/messages", h.ListConversationMessages)
	r.POST("/conversations/:id/messages", h.AddConversationMessage)

	r.POST("/jobs", h.SubmitJob)
	r.GET("/jobs", h.ListJobs)
	r.GET("/jobs/:id", h.GetJob)

	r.GET("/mcp/servers", h.ListMCPServers)
	r.POST("/mcp/servers", h.CreateMCPServer)
	r.GET("/mcp/servers/:serverId", h.GetMCPServer)
	r.PATCH("/mcp/servers/:serverId", h.UpdateMCPServer)
	r.DELETE("/mcp/servers/:serverId", h.DeleteMCPServer)
}

// RegisterInferRoutes mounts inference endpoints (requires API key via InferAuth).
func (h *Handler) RegisterInferRoutes(r gin.IRoutes) {
	// Native hyperstrate format (modelId in body)
	r.POST("/infer", h.Infer)
	r.POST("/infer/stream", h.InferStream)

	// Job processor trigger for serverless runners
	r.POST("/jobs/:id/process", h.ProcessJob)
}

// RegisterProxyRoutes mounts the provider-compatible catch-all proxy.
// Registered under a separate prefix (/proxy/ai) so the wildcard *path
// does not conflict with the admin sub-routes above.
// SDK baseURL: http://host/proxy/ai/:id
func (h *Handler) RegisterProxyRoutes(r gin.IRouter) {
	r.Any("/:id/*path", h.ModelProxy)
}

// ModelProxy dispatches provider-compatible requests based on the path suffix.
func (h *Handler) ModelProxy(c *gin.Context) {
	path := c.Param("path")
	switch path {
	case "/v1/chat/completions", "/chat/completions":
		h.ModelOpenAIChatCompletions(c)
	case "/v1/messages", "/messages":
		h.ModelAnthropicMessages(c)
	default:
		c.JSON(http.StatusNotFound, gin.H{
			"error": "unsupported path " + path + "; supported: /v1/chat/completions, /v1/messages",
		})
	}
}

// ── Conversation handlers ─────────────────────────────────────────────────────

// ListConversations godoc
// @Summary     List conversations
// @Description Returns paginated conversations ordered by creation time
// @Tags        hyperstrate
// @Tags        conversations
// @Produce     json
// @Param       page     query  int  false  "Page number (default 1)"
// @Param       perPage  query  int  false  "Items per page (default 30, max 500)"
// @Success     200  {object}  pagination.Paginated[ConversationResponse]
// @Failure     500  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/conversations [get]
func (h *Handler) ListConversations(c *gin.Context) {
	result, err := h.svc.ListConversations(c.Request.Context(), pagination.ParseSlice(c))
	if err != nil {
		respondError(c, err)
		return
	}
	enriched := make([]ConversationResponse, len(result.Items))
	for i, conv := range result.Items {
		enriched[i] = h.enrichConversation(c.Request.Context(), conv)
	}
	c.JSON(http.StatusOK, pagination.New(enriched, result.Meta.Total, pagination.ParseSlice(c)))
}

// CreateConversation godoc
// @Summary     Create a conversation
// @Description Creates a new conversation tied to a registered model
// @Tags        hyperstrate
// @Tags        conversations
// @Accept      json
// @Produce     json
// @Param       body  body      application.CreateConversationInput  true  "Conversation input"
// @Success     201   {object}  ConversationResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Failure     500   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/conversations [post]
func (h *Handler) CreateConversation(c *gin.Context) {
	var input application.CreateConversationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	conv, err := h.svc.CreateConversation(c.Request.Context(), input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, h.enrichConversation(c.Request.Context(), *conv))
}

// GetConversation godoc
// @Summary     Get a conversation
// @Description Returns a single conversation by ID
// @Tags        hyperstrate
// @Tags        conversations
// @Produce     json
// @Param       id   path      string  true  "Conversation ID"
// @Success     200  {object}  ConversationResponse
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/conversations/{id} [get]
func (h *Handler) GetConversation(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	conv, err := h.svc.GetConversation(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, h.enrichConversation(c.Request.Context(), *conv))
}

// DeleteConversation godoc
// @Summary     Delete a conversation
// @Description Deletes a conversation and all its messages
// @Tags        hyperstrate
// @Tags        conversations
// @Produce     json
// @Param       id   path  string  true  "Conversation ID"
// @Success     204
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/conversations/{id} [delete]
func (h *Handler) DeleteConversation(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	if err := h.svc.DeleteConversation(c.Request.Context(), id); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ListConversationMessages godoc
// @Summary     List conversation messages
// @Description Returns all messages in a conversation ordered by creation time
// @Tags        hyperstrate
// @Tags        conversations
// @Produce     json
// @Param       id   path      string  true  "Conversation ID"
// @Success     200  {array}   domain.ConversationMessage
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/conversations/{id}/messages [get]
func (h *Handler) ListConversationMessages(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	msgs, err := h.svc.ListConversationMessages(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, msgs)
}

// AddConversationMessage godoc
// @Summary     Add a message to a conversation
// @Description Appends a user or assistant message to an existing conversation
// @Tags        hyperstrate
// @Tags        conversations
// @Accept      json
// @Produce     json
// @Param       id    path      string                               true  "Conversation ID"
// @Param       body  body      application.AddConversationMessageInput  true  "Message input"
// @Success     201   {object}  domain.ConversationMessage
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/conversations/{id}/messages [post]
func (h *Handler) AddConversationMessage(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	var input application.AddConversationMessageInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	msg, err := h.svc.AddConversationMessage(c.Request.Context(), id, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, msg)
}

// ── Catalog handlers ──────────────────────────────────────────────────────────

// ListCatalog godoc
// @Summary     List catalog model definitions
// @Description Returns all static model definitions available for registration
// @Tags        hyperstrate
// @Tags        catalog
// @Produce     json
// @Param       query  query  string  false  "Search by key, display name, model id, provider, description, or capability"
// @Success     200  {array}  domain.ModelDefinition
// @Security    BearerAuth
// @Router      /ai/catalog [get]
func (h *Handler) ListCatalog(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.ListCatalog(c.Query("query")))
}

// ── Model handlers ────────────────────────────────────────────────────────────

// ListModels godoc
// @Summary     List registered models
// @Description Returns paginated registered AI model instances. When `ids` is provided, fetches only those specific models and ignores pagination/capabilities.
// @Tags        hyperstrate
// @Tags        models
// @Produce     json
// @Param       ids          query  []string  false  "Fetch specific models by ID (bypasses pagination)"  collectionFormat(multi)
// @Param       page         query  int       false  "Page number (default 1)"
// @Param       perPage      query  int       false  "Items per page (default 30, max 500)"
// @Param       capabilities query  []string  false  "Capability filter (multi-value)"  collectionFormat(multi)
// @Param       query        query  string    false  "Search by model ID, alias, or model definition key"
// @Success     200  {object}  pagination.Paginated[application.ModelResponse]
// @Failure     500  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/models [get]
func (h *Handler) ListModels(c *gin.Context) {
	if ids := c.QueryArray("ids"); len(ids) > 0 {
		result, err := h.svc.GetModelsByIDs(c.Request.Context(), ids)
		if err != nil {
			respondError(c, err)
			return
		}
		sl := pagination.Slice{Page: 1, PerPage: len(result)}
		c.JSON(http.StatusOK, pagination.New(result, int64(len(result)), sl))
		return
	}
	caps := c.QueryArray("capabilities")
	result, err := h.svc.ListModels(c.Request.Context(), pagination.ParseSlice(c), caps, c.Query("query"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// RegisterModel godoc
// @Summary     Register a model
// @Description Registers a new AI model instance from a catalog model definition
// @Tags        hyperstrate
// @Tags        models
// @Accept      json
// @Produce     json
// @Param       body  body      application.RegisterModelInput  true  "Registration input"
// @Success     201   {object}  application.ModelResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Failure     500   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/models [post]
func (h *Handler) RegisterModel(c *gin.Context) {
	var input application.RegisterModelInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}

	model, err := h.svc.RegisterModel(c.Request.Context(), input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, model)
}

// GetModel godoc
// @Summary     Get a registered model
// @Description Returns a single model registration by ID
// @Tags        hyperstrate
// @Tags        models
// @Produce     json
// @Param       id   path      string  true  "Model ID"
// @Success     200  {object}  application.ModelResponse
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/models/{id} [get]
func (h *Handler) GetModel(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	model, err := h.svc.GetModel(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, model)
}

// UpdateModel godoc
// @Summary     Update a model
// @Description Updates the alias of a model registration
// @Tags        hyperstrate
// @Tags        models
// @Accept      json
// @Produce     json
// @Param       id    path      string                        true  "Model ID"
// @Param       body  body      application.UpdateModelInput  true  "Fields to update"
// @Success     200   {object}  application.ModelResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/models/{id} [patch]
func (h *Handler) UpdateModel(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}

	var input application.UpdateModelInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}

	model, err := h.svc.UpdateModel(c.Request.Context(), id, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, model)
}

// DeleteModel godoc
// @Summary     Delete a model
// @Description Deletes a model registration and its associated config
// @Tags        hyperstrate
// @Tags        models
// @Produce     json
// @Param       id   path  string  true  "Model ID"
// @Success     204
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/models/{id} [delete]
func (h *Handler) DeleteModel(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	if err := h.svc.DeleteModel(c.Request.Context(), id); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ── Config handlers ───────────────────────────────────────────────────────────

// SetModelConfiguration godoc
// @Summary     Configure a model
// @Description Sets (or replaces) the endpoint URL, credentials, and options for a model
// @Tags        hyperstrate
// @Tags        models
// @Accept      json
// @Produce     json
// @Param       id    path      string                          true  "Model ID"
// @Param       body  body      application.SetModelConfigurationInput true  "Configuration"
// @Success     200   {object}  application.ModelConfigurationResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/models/{id}/configuration [put]
func (h *Handler) SetModelConfiguration(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}

	var input application.SetModelConfigurationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}

	resp, err := h.svc.SetModelConfiguration(c.Request.Context(), id, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetModelConfiguration godoc
// @Summary     Get model configuration
// @Description Returns model configuration (credentials are never exposed)
// @Tags        hyperstrate
// @Tags        models
// @Produce     json
// @Param       id   path      string  true  "Model ID"
// @Success     200  {object}  application.ModelConfigurationResponse
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/models/{id}/configuration [get]
func (h *Handler) GetModelConfiguration(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	resp, err := h.svc.GetModelConfiguration(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ── Inference handler ─────────────────────────────────────────────────────────

// Infer godoc
// @Summary     Synchronous inference
// @Description Forwards a request to the upstream model and returns the response immediately
// @Tags        hyperstrate
// @Tags        inference
// @Accept      json
// @Produce     json
// @Param       body  body      application.InferRequest  true  "Inference request"
// @Success     200   {object}  application.InferenceResult
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Failure     422   {object}  ErrorResponse
// @Failure     502   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/infer [post]
func (h *Handler) Infer(c *gin.Context) {
	var input application.InferRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}

	result, err := h.svc.Infer(c.Request.Context(), input)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// InferStream godoc
// @Summary     Streaming inference
// @Description Forwards a request to the upstream model and streams response tokens as SSE.
//
//	Each event is: data: {"delta":"<token>"}\n\n
//	The stream ends with: data: [DONE]\n\n
//
// @Tags        hyperstrate
// @Tags        inference
// @Accept      json
// @Produce     text/event-stream
// @Param       body  body  application.InferRequest  true  "Inference request"
// @Success     200
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Failure     422   {object}  ErrorResponse
// @Failure     502   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/infer/stream [post]
func (h *Handler) InferStream(c *gin.Context) {
	var input application.InferRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}

	ch, err := h.svc.InferStream(c.Request.Context(), input)
	if err != nil {
		respondError(c, err)
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no") // disable nginx buffering

	c.Stream(func(w io.Writer) bool {
		chunk, ok := <-ch
		if !ok {
			return false
		}
		if chunk.Err != nil {
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", chunk.Err.Error())
			return false
		}
		if chunk.Done {
			fmt.Fprintf(w, "data: [DONE]\n\n")
			return false
		}
		data, _ := json.Marshal(map[string]string{"delta": chunk.Delta})
		fmt.Fprintf(w, "data: %s\n\n", data)
		return true
	})
}

// ── Job handlers ──────────────────────────────────────────────────────────────

// SubmitJob godoc
// @Summary     Submit an async inference job
// @Description Creates a background job and returns its ID immediately.
//
//	Poll GET /ai/jobs/:id for the result.
//
// @Tags        hyperstrate
// @Tags        jobs
// @Accept      json
// @Produce     json
// @Param       body  body      application.SubmitJobRequest  true  "Job payload"
// @Success     202   {object}  domain.Job
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/jobs [post]
func (h *Handler) SubmitJob(c *gin.Context) {
	var input application.SubmitJobRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}

	job, err := h.svc.SubmitJob(c.Request.Context(), input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, job)
}

// ListJobs godoc
// @Summary     List jobs
// @Description Returns paginated inference jobs ordered by creation time (newest first)
// @Tags        hyperstrate
// @Tags        jobs
// @Produce     json
// @Param       page     query  int  false  "Page number (default 1)"
// @Param       perPage  query  int  false  "Items per page (default 30, max 500)"
// @Success     200  {object}  pagination.Paginated[domain.Job]
// @Failure     500  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/jobs [get]
func (h *Handler) ListJobs(c *gin.Context) {
	result, err := h.svc.ListJobs(c.Request.Context(), pagination.ParseSlice(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetJob godoc
// @Summary     Get a job
// @Description Returns a job by ID, including its status and result when complete
// @Tags        hyperstrate
// @Tags        jobs
// @Produce     json
// @Param       id   path      string  true  "Job ID"
// @Success     200  {object}  domain.Job
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/jobs/{id} [get]
func (h *Handler) GetJob(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	job, err := h.svc.GetJob(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, job)
}

// ProcessJob godoc
// @Summary     Process a pending job
// @Description Runs a PENDING job synchronously. Intended for serverless triggers
//
//	(Lambda async invoke, SQS consumer) rather than direct client calls.
//
// @Tags        hyperstrate
// @Tags        jobs
// @Produce     json
// @Param       id   path  string  true  "Job ID"
// @Success     204
// @Failure     404  {object}  ErrorResponse
// @Failure     500  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/jobs/{id}/process [post]
func (h *Handler) ProcessJob(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	if err := h.svc.ProcessJob(c.Request.Context(), id); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

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
	case errors.Is(err, domain.ErrConversationNotFound),
		errors.Is(err, domain.ErrModelNotFound),
		errors.Is(err, domain.ErrJobNotFound),
		errors.Is(err, domain.ErrModelConfigurationNotFound),
		errors.Is(err, domain.ErrModelDefinitionNotFound),
		errors.Is(err, domain.ErrMCPServerNotFound):
		c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrInvalidProvider),
		errors.Is(err, domain.ErrInvalidInputType):
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrModelNotConfigured):
		c.JSON(http.StatusUnprocessableEntity, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrProxyFailed):
		c.JSON(http.StatusBadGateway, ErrorResponse{Error: err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
	}
}

// ListModelConfigurations godoc
// @Summary     List model configurations
// @Description Returns full configuration DTOs for the given model IDs (only models that have a configuration are included)
// @Tags        hyperstrate
// @Tags        models
// @Produce     json
// @Param       ids  query     []string  true  "Model IDs"  collectionFormat(multi)
// @Success     200  {array}   application.ModelConfigurationResponse
// @Security    BearerAuth
// @Router      /ai/models/configurations [get]
func (h *Handler) ListModelConfigurations(c *gin.Context) {
	ids := c.QueryArray("ids")
	if len(ids) == 0 {
		c.JSON(http.StatusOK, []application.ModelConfigurationResponse{})
		return
	}
	result, err := h.svc.ListModelConfigurations(c.Request.Context(), ids)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ── Key rotation ──────────────────────────────────────────────────────────────

// RotateAPIKey godoc
// @Summary     Rotate provider API key
// @Description Replaces the API key stored for a model configuration and records the rotation event
// @Tags        hyperstrate
// @Tags        models
// @Accept      json
// @Produce     json
// @Param       id    path      string  true  "Model ID"
// @Param       body  body      object{newKey=string,gracePeriodHours=int}  true  "Rotation input"
// @Success     201   {object}  domain.ModelKeyRotation
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/models/{id}/rotate-key [post]
func (h *Handler) RotateAPIKey(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	var body struct {
		NewKey           string `json:"newKey"           binding:"required"`
		GracePeriodHours int    `json:"gracePeriodHours"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	rot, err := h.svc.RotateAPIKey(c.Request.Context(), id, body.NewKey, body.GracePeriodHours)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, rot)
}

// ListKeyRotations godoc
// @Summary     List API key rotation history
// @Description Returns a chronological log of provider API key rotations for a model
// @Tags        hyperstrate
// @Tags        models
// @Produce     json
// @Param       id      path   string  true   "Model ID"
// @Param       limit   query  int     false  "Max entries (default 50)"
// @Param       offset  query  int     false  "Skip entries (default 0)"
// @Success     200  {object}  object{data=[]domain.ModelKeyRotation,total=int}
// @Security    BearerAuth
// @Router      /ai/models/{id}/key-rotations [get]
func (h *Handler) ListKeyRotations(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, total, err := h.svc.ListKeyRotations(c.Request.Context(), id, limit, offset)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": total})
}

// ── Ollama auto-discovery ─────────────────────────────────────────────────────

// ollamaTagsResponse is a subset of the Ollama /api/tags response.
type ollamaTagsResponse struct {
	Models []struct {
		Name    string `json:"name"`
		Size    int64  `json:"size"`
		Digest  string `json:"digest"`
		Details struct {
			ParameterSize     string `json:"parameter_size"`
			QuantizationLevel string `json:"quantization_level"`
		} `json:"details"`
	} `json:"models"`
}

// DiscoveredModel is a model found on a local Ollama instance.
type DiscoveredModel struct {
	Name              string `json:"name"`
	ParameterSize     string `json:"parameterSize,omitempty"`
	QuantizationLevel string `json:"quantizationLevel,omitempty"`
	SizeBytes         int64  `json:"sizeBytes"`
	// SuggestedCatalogKey is the closest match in the Hyperstrate catalog, if any.
	SuggestedCatalogKey string `json:"suggestedCatalogKey,omitempty"`
}

// DiscoverOllamaModels godoc
// @Summary     Discover models on a local Ollama instance
// @Description Polls the given Ollama base URL for available models and suggests catalog matches
// @Tags        hyperstrate
// @Tags        models
// @Produce     json
// @Param       baseUrl  query  string  false  "Ollama base URL (defaults to configured OLLAMA_BASE_URL)"
// @Success     200  {object}  object{data=[]DiscoveredModel}
// @Failure     502  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/discover [get]
func (h *Handler) DiscoverOllamaModels(c *gin.Context) {
	baseURL := strings.TrimSpace(c.Query("baseUrl"))
	if baseURL == "" {
		baseURL = h.ollamaBaseURL
	}
	if len(baseURL) > 500 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "baseUrl too long"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/tags", nil)
	if err != nil {
		c.JSON(http.StatusBadGateway, ErrorResponse{Error: "invalid baseUrl: " + err.Error()})
		return
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, ErrorResponse{Error: "could not reach Ollama at " + baseURL + ": " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, ErrorResponse{Error: fmt.Sprintf("Ollama returned HTTP %d", resp.StatusCode)})
		return
	}

	var tags ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		c.JSON(http.StatusBadGateway, ErrorResponse{Error: "could not parse Ollama response: " + err.Error()})
		return
	}

	catalog := h.svc.ListCatalog("")
	out := make([]DiscoveredModel, 0, len(tags.Models))
	for _, m := range tags.Models {
		dm := DiscoveredModel{
			Name:              m.Name,
			ParameterSize:     m.Details.ParameterSize,
			QuantizationLevel: m.Details.QuantizationLevel,
			SizeBytes:         m.Size,
		}
		// Try to find a catalog entry whose ModelID substring matches the Ollama name.
		shortName := strings.Split(m.Name, ":")[0]
		for _, def := range catalog {
			if strings.Contains(strings.ToLower(def.ModelID), strings.ToLower(shortName)) ||
				strings.Contains(strings.ToLower(shortName), strings.ToLower(def.ModelID)) {
				dm.SuggestedCatalogKey = def.Key
				break
			}
		}
		out = append(out, dm)
	}

	c.JSON(http.StatusOK, gin.H{"data": out})
}

// ── MCP Server management ─────────────────────────────────────────────────────

// ListMCPServers godoc
// @Summary     List MCP servers
// @Description Returns all MCP server configurations for the organisation
// @Tags        hyperstrate
// @Tags        models
// @Produce     json
// @Success     200  {array}   application.MCPServerResponse
// @Security    BearerAuth
// @Router      /ai/mcp/servers [get]
func (h *Handler) ListMCPServers(c *gin.Context) {
	result, err := h.svc.ListMCPServers(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateMCPServer godoc
// @Summary     Create an MCP server
// @Description Registers a new MCP server with optional auth credentials
// @Tags        hyperstrate
// @Tags        models
// @Accept      json
// @Produce     json
// @Param       body  body      application.CreateMCPServerInput  true  "MCP server config"
// @Success     201   {object}  application.MCPServerResponse
// @Failure     400   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/mcp/servers [post]
func (h *Handler) CreateMCPServer(c *gin.Context) {
	var input application.CreateMCPServerInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.CreateMCPServer(c.Request.Context(), input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// GetMCPServer godoc
// @Summary     Get an MCP server
// @Tags        hyperstrate
// @Tags        models
// @Produce     json
// @Param       serverId  path      string  true  "MCP Server ID"
// @Success     200       {object}  application.MCPServerResponse
// @Failure     404       {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/mcp/servers/{serverId} [get]
func (h *Handler) GetMCPServer(c *gin.Context) {
	serverID, ok := validateParam(c, "serverId")
	if !ok {
		return
	}
	result, err := h.svc.GetMCPServer(c.Request.Context(), serverID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateMCPServer godoc
// @Summary     Update an MCP server
// @Tags        hyperstrate
// @Tags        models
// @Accept      json
// @Produce     json
// @Param       serverId  path      string                            true  "MCP Server ID"
// @Param       body      body      application.UpdateMCPServerInput  true  "Fields to update"
// @Success     200       {object}  application.MCPServerResponse
// @Failure     400       {object}  ErrorResponse
// @Failure     404       {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/mcp/servers/{serverId} [patch]
func (h *Handler) UpdateMCPServer(c *gin.Context) {
	serverID, ok := validateParam(c, "serverId")
	if !ok {
		return
	}
	var input application.UpdateMCPServerInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.UpdateMCPServer(c.Request.Context(), serverID, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteMCPServer godoc
// @Summary     Delete an MCP server
// @Tags        hyperstrate
// @Tags        models
// @Param       serverId  path  string  true  "MCP Server ID"
// @Success     204
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /ai/mcp/servers/{serverId} [delete]
func (h *Handler) DeleteMCPServer(c *gin.Context) {
	serverID, ok := validateParam(c, "serverId")
	if !ok {
		return
	}
	if err := h.svc.DeleteMCPServer(c.Request.Context(), serverID); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
