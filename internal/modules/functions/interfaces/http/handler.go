package http

import (
	"errors"
	"net/http"
	"strings"

	"hyperstrate/server/internal/modules/functions/application"
	"hyperstrate/server/internal/modules/functions/domain"
	"hyperstrate/server/internal/shared/pagination"
	"hyperstrate/server/internal/shared/validation"

	"github.com/gin-gonic/gin"
)

// Handler wires the functions module's use-cases to HTTP endpoints.
type Handler struct {
	svc       application.Service
	runnerSvc application.RunnerService
}

func NewHandler(svc application.Service, runnerSvc application.RunnerService) *Handler {
	return &Handler{svc: svc, runnerSvc: runnerSvc}
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error  string              `json:"error"`
	Fields map[string][]string `json:"fields,omitempty"`
}

// RegisterAdminRoutes mounts admin-managed routes (requires session + admin role).
func (h *Handler) RegisterAdminRoutes(r gin.IRoutes) {
	r.GET("/apps", h.ListApps)
	r.POST("/apps", h.CreateApp)
	r.GET("/apps/:appId/functions", h.ListFunctions)
	r.POST("/apps/:appId/functions", h.DeployFunction)
	r.GET("/functions/:functionId", h.GetFunction)
	r.GET("/functions/:functionId/revisions", h.ListFunctionRevisions)
	r.GET("/functions/:functionId/invocations", h.ListFunctionInvocations)
	r.GET("/runner-pools", h.ListRunnerPools)
	r.POST("/runner-pools", h.CreateRunnerPool)
	r.GET("/runner-pools/:poolId/agents", h.ListRunnerAgents)
}

// RegisterInferRoutes mounts runtime routes (requires API key or session auth).
func (h *Handler) RegisterInferRoutes(r gin.IRoutes) {
	r.POST("/:functionId/invocations", h.InvokeFunction)
	r.GET("/invocations/:invocationId", h.GetInvocation)
	r.GET("/invocations/:invocationId/logs", h.ListInvocationLogs)
}

// RegisterRunnerRoutes mounts bootstrap routes used by runner agents.
func (h *Handler) RegisterRunnerRoutes(r gin.IRoutes) {
	r.POST("/runner/agents/register", h.RegisterRunnerAgent)
	r.POST("/runner/agents/heartbeat", h.HeartbeatRunnerAgent)
	r.POST("/runner/invocations/lease", h.LeaseNextInvocation)
	r.POST("/runner/invocations/:invocationId/logs", h.AppendInvocationLog)
	r.POST("/runner/invocations/:invocationId/complete", h.CompleteInvocation)
}

// ListApps godoc
// @Summary     List functions apps
// @Description Returns paginated functions apps owned by the authenticated organisation
// @Tags        hyperstrate
// @Tags        functions
// @Produce     json
// @Param       page     query     int  false  "Page number (default 1)"
// @Param       perPage  query     int  false  "Items per page (default 30, max 500)"
// @Success     200      {object}  pagination.Paginated[application.AppResponse]
// @Failure     400      {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /functions/apps [get]
func (h *Handler) ListApps(c *gin.Context) {
	result, err := h.svc.ListApps(c.Request.Context(), pagination.ParseSlice(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateApp godoc
// @Summary     Create a functions app
// @Description Creates a functions app owned by the authenticated organisation
// @Tags        hyperstrate
// @Tags        functions
// @Accept      json
// @Produce     json
// @Param       body  body      application.CreateAppInput  true  "App input"
// @Success     201   {object}  application.AppResponse
// @Failure     400   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /functions/apps [post]
func (h *Handler) CreateApp(c *gin.Context) {
	var input application.CreateAppInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.CreateApp(c.Request.Context(), input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ListFunctions godoc
// @Summary     List app functions
// @Description Returns paginated functions under an app
// @Tags        hyperstrate
// @Tags        functions
// @Produce     json
// @Param       appId    path      string  true   "Functions app ID"
// @Param       page     query     int     false  "Page number (default 1)"
// @Param       perPage  query     int     false  "Items per page (default 30, max 500)"
// @Success     200      {object}  pagination.Paginated[application.FunctionResponse]
// @Failure     400      {object}  ErrorResponse
// @Failure     404      {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /functions/apps/{appId}/functions [get]
func (h *Handler) ListFunctions(c *gin.Context) {
	appID, ok := validateParam(c, "appId")
	if !ok {
		return
	}
	result, err := h.svc.ListFunctions(c.Request.Context(), appID, pagination.ParseSlice(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeployFunction godoc
// @Summary     Deploy a function
// @Description Creates a function and first revision under an app
// @Tags        hyperstrate
// @Tags        functions
// @Accept      json
// @Produce     json
// @Param       appId  path      string                           true  "Functions app ID"
// @Param       body   body      application.DeployFunctionInput  true  "Function deployment"
// @Success     201    {object}  application.FunctionResponse
// @Failure     400    {object}  ErrorResponse
// @Failure     404    {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /functions/apps/{appId}/functions [post]
func (h *Handler) DeployFunction(c *gin.Context) {
	appID, ok := validateParam(c, "appId")
	if !ok {
		return
	}
	var input application.DeployFunctionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.DeployFunction(c.Request.Context(), appID, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// GetFunction godoc
// @Summary     Get a function
// @Description Returns function metadata by ID
// @Tags        hyperstrate
// @Tags        functions
// @Produce     json
// @Param       functionId  path      string  true  "Function ID"
// @Success     200         {object}  application.FunctionResponse
// @Failure     400         {object}  ErrorResponse
// @Failure     404         {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /functions/functions/{functionId} [get]
func (h *Handler) GetFunction(c *gin.Context) {
	functionID, ok := validateParam(c, "functionId")
	if !ok {
		return
	}
	result, err := h.svc.GetFunction(c.Request.Context(), functionID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListFunctionRevisions godoc
// @Summary     List function revisions
// @Description Returns paginated function revisions, latest first, including linked build details when available
// @Tags        hyperstrate
// @Tags        functions
// @Produce     json
// @Param       functionId  path      string  true   "Function ID"
// @Param       page        query     int     false  "Page number (default 1)"
// @Param       perPage     query     int     false  "Items per page (default 30, max 500)"
// @Success     200         {object}  pagination.Paginated[application.RevisionResponse]
// @Failure     400         {object}  ErrorResponse
// @Failure     404         {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /functions/functions/{functionId}/revisions [get]
func (h *Handler) ListFunctionRevisions(c *gin.Context) {
	functionID, ok := validateParam(c, "functionId")
	if !ok {
		return
	}
	result, err := h.svc.ListFunctionRevisions(c.Request.Context(), functionID, pagination.ParseSlice(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// InvokeFunction godoc
// @Summary     Invoke a function
// @Description Queues a function invocation
// @Tags        hyperstrate
// @Tags        functions
// @Accept      json
// @Produce     json
// @Param       functionId  path      string                           true  "Function ID"
// @Param       body        body      application.InvokeFunctionInput  true  "Invocation request"
// @Success     202         {object}  application.InvocationResponse
// @Failure     400         {object}  ErrorResponse
// @Failure     404         {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /functions/{functionId}/invocations [post]
func (h *Handler) InvokeFunction(c *gin.Context) {
	functionID, ok := validateParam(c, "functionId")
	if !ok {
		return
	}
	var input application.InvokeFunctionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.svc.InvokeFunction(c.Request.Context(), functionID, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, result)
}

// ListFunctionInvocations godoc
// @Summary     List function invocations
// @Description Returns paginated function invocations, latest first
// @Tags        hyperstrate
// @Tags        functions
// @Produce     json
// @Param       functionId  path      string  true   "Function ID"
// @Param       page        query     int     false  "Page number (default 1)"
// @Param       perPage     query     int     false  "Items per page (default 30, max 500)"
// @Success     200         {object}  pagination.Paginated[application.InvocationResponse]
// @Failure     400         {object}  ErrorResponse
// @Failure     404         {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /functions/functions/{functionId}/invocations [get]
func (h *Handler) ListFunctionInvocations(c *gin.Context) {
	functionID, ok := validateParam(c, "functionId")
	if !ok {
		return
	}
	result, err := h.svc.ListFunctionInvocations(c.Request.Context(), functionID, pagination.ParseSlice(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetInvocation godoc
// @Summary     Get an invocation
// @Description Returns a function invocation by ID
// @Tags        hyperstrate
// @Tags        functions
// @Produce     json
// @Param       invocationId  path      string  true  "Invocation ID"
// @Success     200           {object}  application.InvocationResponse
// @Failure     400           {object}  ErrorResponse
// @Failure     404           {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /functions/invocations/{invocationId} [get]
func (h *Handler) GetInvocation(c *gin.Context) {
	invocationID, ok := validateParam(c, "invocationId")
	if !ok {
		return
	}
	result, err := h.svc.GetInvocation(c.Request.Context(), invocationID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListInvocationLogs godoc
// @Summary     List invocation logs
// @Description Returns ordered log entries for a function invocation
// @Tags        hyperstrate
// @Tags        functions
// @Produce     json
// @Param       invocationId  path      string  true  "Invocation ID"
// @Param       page          query     int     false "Page number"
// @Param       perPage       query     int     false "Items per page"
// @Success     200           {object}  pagination.Paginated[application.LogResponse]
// @Failure     400           {object}  ErrorResponse
// @Failure     404           {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /functions/invocations/{invocationId}/logs [get]
func (h *Handler) ListInvocationLogs(c *gin.Context) {
	invocationID, ok := validateParam(c, "invocationId")
	if !ok {
		return
	}
	result, err := h.svc.ListInvocationLogs(c.Request.Context(), invocationID, pagination.ParseSlice(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// AppendInvocationLog godoc
// @Summary     Append an invocation log entry
// @Description Appends a runner-authenticated runtime log line for an invocation lease
// @Tags        hyperstrate
// @Tags        functions
// @Accept      json
// @Produce     json
// @Param       invocationId  path      string                      true  "Invocation ID"
// @Param       body          body      application.AppendRunnerLogInput  true  "Runner log entry"
// @Success     201           {object}  application.LogResponse
// @Failure     400           {object}  ErrorResponse
// @Failure     404           {object}  ErrorResponse
// @Router      /functions/runner/invocations/{invocationId}/logs [post]
func (h *Handler) AppendInvocationLog(c *gin.Context) {
	invocationID, ok := validateParam(c, "invocationId")
	if !ok {
		return
	}
	var input application.AppendRunnerLogInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	if !requireRunnerSessionToken(c, &input.SessionToken) {
		return
	}
	result, err := h.runnerSvc.AppendInvocationLog(c.Request.Context(), invocationID, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ListRunnerPools godoc
// @Summary     List runner pools
// @Description Returns paginated runner pools owned by the authenticated organisation
// @Tags        hyperstrate
// @Tags        functions
// @Produce     json
// @Param       page     query     int  false  "Page number (default 1)"
// @Param       perPage  query     int  false  "Items per page (default 30, max 500)"
// @Success     200      {object}  pagination.Paginated[application.RunnerPoolResponse]
// @Failure     400      {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /functions/runner-pools [get]
func (h *Handler) ListRunnerPools(c *gin.Context) {
	result, err := h.runnerSvc.ListRunnerPools(c.Request.Context(), pagination.ParseSlice(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateRunnerPool godoc
// @Summary     Create a runner pool
// @Description Creates a runner pool and returns its one-time bootstrap token
// @Tags        hyperstrate
// @Tags        functions
// @Accept      json
// @Produce     json
// @Param       body  body      application.CreateRunnerPoolInput  true  "Runner pool input"
// @Success     201   {object}  application.RunnerPoolResponse
// @Failure     400   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /functions/runner-pools [post]
func (h *Handler) CreateRunnerPool(c *gin.Context) {
	var input application.CreateRunnerPoolInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.runnerSvc.CreateRunnerPool(c.Request.Context(), input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ListRunnerAgents godoc
// @Summary     List runner agents
// @Description Returns paginated runner agents for a runner pool
// @Tags        hyperstrate
// @Tags        functions
// @Produce     json
// @Param       poolId   path      string  true   "Runner pool ID"
// @Param       page     query     int     false  "Page number (default 1)"
// @Param       perPage  query     int     false  "Items per page (default 30, max 500)"
// @Success     200      {object}  pagination.Paginated[application.RunnerAgentResponse]
// @Failure     400      {object}  ErrorResponse
// @Failure     404      {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /functions/runner-pools/{poolId}/agents [get]
func (h *Handler) ListRunnerAgents(c *gin.Context) {
	poolID, ok := validateParam(c, "poolId")
	if !ok {
		return
	}
	result, err := h.runnerSvc.ListRunnerAgents(c.Request.Context(), poolID, pagination.ParseSlice(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// RegisterRunnerAgent godoc
// @Summary     Register a runner agent
// @Description Exchanges a runner-pool bootstrap token for a runner session token
// @Tags        hyperstrate
// @Tags        functions
// @Accept      json
// @Produce     json
// @Param       body  body      application.RegisterRunnerAgentInput  true  "Runner registration input"
// @Success     201   {object}  application.RunnerAgentRegistrationResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Router      /functions/runner/agents/register [post]
func (h *Handler) RegisterRunnerAgent(c *gin.Context) {
	var input application.RegisterRunnerAgentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	result, err := h.runnerSvc.RegisterRunnerAgent(c.Request.Context(), input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// HeartbeatRunnerAgent godoc
// @Summary     Heartbeat a runner agent
// @Description Authenticates a runner session, updates heartbeat/capabilities, and renews the session expiry
// @Tags        hyperstrate
// @Tags        functions
// @Accept      json
// @Produce     json
// @Param       body  body      application.HeartbeatRunnerAgentInput  true  "Runner heartbeat input"
// @Success     200   {object}  application.RunnerAgentHeartbeatResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     401   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Security    BearerAuth
// @Router      /functions/runner/agents/heartbeat [post]
func (h *Handler) HeartbeatRunnerAgent(c *gin.Context) {
	var input application.HeartbeatRunnerAgentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	if !requireRunnerSessionToken(c, &input.SessionToken) {
		return
	}
	result, err := h.runnerSvc.HeartbeatRunnerAgent(c.Request.Context(), input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// LeaseNextInvocation godoc
// @Summary     Lease the next queued function invocation
// @Description Authenticates a runner session and assigns the next queued invocation in its organisation
// @Tags        hyperstrate
// @Tags        functions
// @Accept      json
// @Produce     json
// @Param       body  body      application.LeaseInvocationInput  true  "Lease input"
// @Success     200   {object}  application.InvocationResponse
// @Failure     400   {object}  ErrorResponse
// @Failure     404   {object}  ErrorResponse
// @Router      /functions/runner/invocations/lease [post]
func (h *Handler) LeaseNextInvocation(c *gin.Context) {
	var input application.LeaseInvocationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	if !requireRunnerSessionToken(c, &input.SessionToken) {
		return
	}
	result, err := h.runnerSvc.LeaseNextInvocation(c.Request.Context(), input)
	if err != nil {
		if errors.Is(err, domain.ErrNoInvocationAvailable) {
			c.Status(http.StatusNoContent)
			return
		}
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CompleteInvocation godoc
// @Summary     Complete a leased function invocation
// @Description Authenticates a runner session and records invocation status, result, or error
// @Tags        hyperstrate
// @Tags        functions
// @Accept      json
// @Produce     json
// @Param       invocationId  path      string                               true  "Invocation ID"
// @Param       body          body      application.CompleteInvocationInput  true  "Completion input"
// @Success     200           {object}  application.InvocationResponse
// @Failure     400           {object}  ErrorResponse
// @Failure     404           {object}  ErrorResponse
// @Router      /functions/runner/invocations/{invocationId}/complete [post]
func (h *Handler) CompleteInvocation(c *gin.Context) {
	invocationID, ok := validateParam(c, "invocationId")
	if !ok {
		return
	}
	var input application.CompleteInvocationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, err, &input)
		return
	}
	if !requireRunnerSessionToken(c, &input.SessionToken) {
		return
	}
	result, err := h.runnerSvc.CompleteInvocation(c.Request.Context(), invocationID, input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// Helpers

func validateParam(c *gin.Context, name string) (string, bool) {
	v := c.Param(name)
	if len(v) == 0 || len(v) > 100 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid " + name})
		return "", false
	}
	return v, true
}

func requireRunnerSessionToken(c *gin.Context, token *string) bool {
	if headerToken := bearerToken(c); headerToken != "" {
		*token = headerToken
	}
	if strings.TrimSpace(*token) == "" {
		c.JSON(http.StatusUnauthorized, ErrorResponse{Error: domain.ErrRunnerUnauthorized.Error()})
		return false
	}
	return true
}

func bearerToken(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
}

func respondBindError(c *gin.Context, err error, input any) {
	summary, fields := validation.BindingErrors(err, input)
	c.JSON(http.StatusBadRequest, ErrorResponse{Error: summary, Fields: fields})
}

func respondError(c *gin.Context, err error) {
	_ = c.Error(err)
	switch {
	case errors.Is(err, domain.ErrRunnerUnauthorized):
		c.JSON(http.StatusUnauthorized, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrAppNotFound),
		errors.Is(err, domain.ErrFunctionNotFound),
		errors.Is(err, domain.ErrRevisionNotFound),
		errors.Is(err, domain.ErrInvocationNotFound),
		errors.Is(err, domain.ErrRunnerPoolNotFound),
		errors.Is(err, domain.ErrRunnerAgentNotFound):
		c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrUnsupportedInvocationMode),
		errors.Is(err, domain.ErrInvalidFunctionSpec):
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
	}
}
