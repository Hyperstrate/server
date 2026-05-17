package http

import (
	"errors"
	"net/http"

	"hyperstrate/server/internal/modules/prompts/application"
	"hyperstrate/server/internal/modules/prompts/domain"
	"hyperstrate/server/internal/shared/pagination"
	"hyperstrate/server/internal/shared/validation"

	"github.com/gin-gonic/gin"
)

// Handler wires the prompts module's use-cases to HTTP endpoints.
type Handler struct {
	svc application.Service
}

func NewHandler(svc application.Service) *Handler {
	return &Handler{svc: svc}
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error  string              `json:"error"`
	Fields map[string][]string `json:"fields,omitempty"`
}

// RegisterRoutes mounts all prompt routes (admin session required).
func (h *Handler) RegisterRoutes(r gin.IRoutes) {
	r.GET("", h.ListPrompts)
	r.POST("", h.CreatePrompt)
	r.GET("/:id", h.GetPrompt)
	r.PATCH("/:id", h.UpdatePrompt)
	r.DELETE("/:id", h.DeletePrompt)
	// Version history
	r.GET("/:id/versions", h.ListPromptVersions)
	r.GET("/:id/versions/:versionId", h.GetPromptVersion)
	r.POST("/:id/versions/:versionId/restore", h.RestorePromptVersion)
}

// ListPrompts godoc
// @Summary     List system prompts
// @Description Returns paginated system prompts
// @Tags        hyperstrate
// @Tags        prompts
// @Produce     json
// @Param       page     query  int  false  "Page number (default 1)"
// @Param       perPage  query  int  false  "Items per page (default 30, max 500)"
// @Param       query    query  string  false  "Search by prompt ID, name, description, or content"
// @Success     200  {object}  pagination.Paginated[application.PromptResponse]
// @Failure     500  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /prompts [get]
func (h *Handler) ListPrompts(c *gin.Context) {
	result, err := h.svc.ListPrompts(c.Request.Context(), pagination.ParseSlice(c), c.Query("query"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreatePrompt godoc
// @Summary     Create a system prompt
// @Description Creates a reusable named system prompt with optional {{variable}} placeholders
// @Tags        hyperstrate
// @Tags        prompts
// @Accept      json
// @Produce     json
// @Param       body  body      application.CreatePromptInput  true  "Prompt input"
// @Success     201   {object}  application.PromptResponse
// @Failure     400   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /prompts [post]
func (h *Handler) CreatePrompt(c *gin.Context) {
	var input application.CreatePromptInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.CreatePrompt(c.Request.Context(), input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// GetPrompt godoc
// @Summary     Get a system prompt
// @Description Returns a single system prompt by ID
// @Tags        hyperstrate
// @Tags        prompts
// @Produce     json
// @Param       id   path      string  true  "Prompt ID"
// @Success     200  {object}  application.PromptResponse
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /prompts/{id} [get]
func (h *Handler) GetPrompt(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	result, err := h.svc.GetPrompt(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdatePrompt godoc
// @Summary     Update a system prompt
// @Description Updates the name, description, or content of a system prompt
// @Tags        hyperstrate
// @Tags        prompts
// @Accept      json
// @Produce     json
// @Param       id    path      string                         true  "Prompt ID"
// @Param       body  body      application.UpdatePromptInput  true  "Fields to update"
// @Success     200   {object}  application.PromptResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /prompts/{id} [patch]
func (h *Handler) UpdatePrompt(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	var input application.UpdatePromptInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.UpdatePrompt(c.Request.Context(), id, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeletePrompt godoc
// @Summary     Delete a system prompt
// @Tags        hyperstrate
// @Tags        prompts
// @Param       id   path  string  true  "Prompt ID"
// @Success     204
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /prompts/{id} [delete]
func (h *Handler) DeletePrompt(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	if err := h.svc.DeletePrompt(c.Request.Context(), id); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ── Version history ───────────────────────────────────────────────────────────

// ListPromptVersions godoc
// @Summary     List prompt versions
// @Description Returns immutable snapshots of a prompt, newest first
// @Tags        hyperstrate
// @Tags        prompts
// @Produce     json
// @Param       id       path  string  true  "Prompt ID"
// @Param       page     query  int  false  "Page number (default 1)"
// @Param       perPage  query  int  false  "Items per page (default 30, max 500)"
// @Success     200  {object}  pagination.Paginated[application.PromptVersionResponse]
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /prompts/{id}/versions [get]
func (h *Handler) ListPromptVersions(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	result, err := h.svc.ListPromptVersions(c.Request.Context(), id, pagination.ParseSlice(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetPromptVersion godoc
// @Summary     Get a prompt version
// @Description Returns one immutable snapshot by ID
// @Tags        hyperstrate
// @Tags        prompts
// @Produce     json
// @Param       id         path  string  true  "Prompt ID"
// @Param       versionId  path  string  true  "Version ID"
// @Success     200  {object}  application.PromptVersionResponse
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /prompts/{id}/versions/{versionId} [get]
func (h *Handler) GetPromptVersion(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	versionID, ok := validateParam(c, "versionId")
	if !ok {
		return
	}
	result, err := h.svc.GetPromptVersion(c.Request.Context(), id, versionID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// RestorePromptVersion godoc
// @Summary     Restore a prompt to a previous version
// @Description Copies the content of the specified version back to the live prompt and creates a new version snapshot
// @Tags        hyperstrate
// @Tags        prompts
// @Produce     json
// @Param       id         path  string  true  "Prompt ID"
// @Param       versionId  path  string  true  "Version ID to restore"
// @Success     200  {object}  application.PromptResponse
// @Failure     404  {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /prompts/{id}/versions/{versionId}/restore [post]
func (h *Handler) RestorePromptVersion(c *gin.Context) {
	id, ok := validateParam(c, "id")
	if !ok {
		return
	}
	versionID, ok := validateParam(c, "versionId")
	if !ok {
		return
	}
	result, err := h.svc.RestorePromptVersion(c.Request.Context(), id, versionID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
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
	case errors.Is(err, domain.ErrPromptNotFound), errors.Is(err, domain.ErrPromptVersionNotFound):
		c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
	}
}
