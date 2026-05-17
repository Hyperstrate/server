package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/modules/observability/application"
	obsDomain "hyperstrate/server/internal/modules/observability/domain"
	"hyperstrate/server/internal/shared/pagination"

	"github.com/gin-gonic/gin"
)

// ModelNameFunc resolves a modelDefKey to a human-readable display name.
type ModelNameFunc func(defKey string) string

// ModelAliasFunc resolves a modelID to its registration alias.
type ModelAliasFunc func(ctx context.Context, modelID string) string

// RouterNameFunc resolves a routerID to its configured name.
type RouterNameFunc func(ctx context.Context, id string) string

// ReplayResult is the response from a replayed inference.
type ReplayResult struct {
	Content      string  `json:"content"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	CostUSD      float64 `json:"costUsd"`
	LatencyMs    int64   `json:"latencyMs"`
}

// ReplayFunc re-runs an inference given a router ID and request fields.
// Injected by the observability module from the router service.
type ReplayFunc func(ctx context.Context, routerID string, fields map[string]string) (*ReplayResult, error)

type Handler struct {
	svc        application.Service
	modelName  ModelNameFunc
	modelAlias ModelAliasFunc
	routerName RouterNameFunc
	replay     ReplayFunc
}

func NewHandler(svc application.Service, modelName ModelNameFunc, modelAlias ModelAliasFunc, routerName RouterNameFunc) *Handler {
	return &Handler{svc: svc, modelName: modelName, modelAlias: modelAlias, routerName: routerName}
}

// SetReplayFunc wires the replay callback after construction (avoids circular Fx deps).
func (h *Handler) SetReplayFunc(fn ReplayFunc) { h.replay = fn }

func (h *Handler) RegisterRoutes(g *gin.RouterGroup) {
	// Analytics
	g.GET("/analytics/usage", h.getUsageOverTime)
	g.GET("/analytics/models", h.getUsageByModel)
	g.GET("/analytics/routers", h.getUsageByRouter)
	g.GET("/analytics/errors", h.getRecentErrors)
	g.GET("/analytics/ab-test", h.getABTestResults)
	g.GET("/analytics/latency", h.getLatencyStats)
	g.GET("/analytics/virtual-keys", h.getUsageByVirtualKey)
	g.GET("/analytics/cache", h.getCacheStats)
	g.GET("/analytics/inference-logs", h.listInferenceLogs)
	g.GET("/analytics/inference-logs/export", h.exportInferenceLogsCSV)
	g.PATCH("/analytics/inference-logs/:id/feedback", h.submitFeedback)
	g.GET("/analytics/inference-logs/:id/payload", h.getInferencePayload)
	g.POST("/analytics/inference-logs/:id/replay", h.replayInference)

	// Audit log
	g.GET("/analytics/audit", h.listAuditLogs)

	// Per-router analytics
	g.GET("/analytics/routers/:routerId/webhook-deliveries", h.listWebhookDeliveries)
	g.GET("/analytics/routers/:routerId/pipeline-stats", h.getRouterPipelineStats)

	// Agent session analytics
	g.GET("/analytics/agent-sessions", h.listAgentSessions)
	g.GET("/analytics/agent-sessions/:sessionId/logs", h.listAgentSessionLogs)
	g.GET("/analytics/agent-sessions/:sessionId/insights", h.getAgentSessionInsights)
	g.GET("/analytics/agent-sessions/:sessionId/events", h.listAgentSessionEvents)
	g.GET("/analytics/tool-archives", h.listToolArchives)
	g.GET("/analytics/tool-archives/:id", h.getToolArchive)
	g.GET("/analytics/compression-events", h.listCompressionEvents)
	g.GET("/analytics/costly-prompts", h.listCostlyPrompts)
	g.GET("/analytics/subagents", h.listSubagentBreakdown)
	g.GET("/analytics/loops", h.listLoopDetections)

	// Provider health
	g.GET("/health/providers", h.getProviderHealth)
}

// getUsageOverTime godoc
// @Summary     Usage over time
// @Description Returns aggregated inference usage bucketed by the requested granularity (hour / day / month)
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       from         query  string  false  "Start date YYYY-MM-DD (default: 30 days ago)"
// @Param       to           query  string  false  "End date YYYY-MM-DD (default: today)"
// @Param       granularity  query  string  false  "Bucket size: hour | day | month (default: day)"
// @Success     200  {object}  object{data=[]obsDomain.AggregatedUsage}
// @Failure     400  {object}  object{error=string}
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/usage [get]
func (h *Handler) getUsageOverTime(c *gin.Context) {
	orgID := orgIDFromCtx(c)
	from, to, ok := parseDateRange(c)
	if !ok {
		return
	}
	gran := obsDomain.Granularity(c.DefaultQuery("granularity", "day"))
	switch gran {
	case obsDomain.GranularityHour, obsDomain.GranularityDay, obsDomain.GranularityMonth:
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "granularity must be one of: hour, day, month"})
		return
	}

	rows, err := h.svc.GetUsageOverTime(orgID, from, to, gran)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// ModelUsageResponse is ModelUsage enriched with a nested model relation.
type ModelUsageResponse struct {
	obsDomain.ModelUsage
	Model *ModelRef `json:"model,omitempty"`
}

// RouterUsageResponse is RouterUsage enriched with a nested router relation.
type RouterUsageResponse struct {
	obsDomain.RouterUsage
	Router *RouterRef `json:"router,omitempty"`
}

// getUsageByModel godoc
// @Summary     Usage by model
// @Description Returns total requests, tokens, cost, and error count aggregated per registered model
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       from  query  string  false  "Start date YYYY-MM-DD"
// @Param       to    query  string  false  "End date YYYY-MM-DD"
// @Success     200  {object}  object{data=[]ModelUsageResponse}
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/models [get]
func (h *Handler) getUsageByModel(c *gin.Context) {
	orgID := orgIDFromCtx(c)
	from, to, ok := parseDateRangeOptional(c)
	if !ok {
		return
	}

	rows, err := h.svc.GetUsageByModel(orgID, from, to)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]ModelUsageResponse, len(rows))
	for i, r := range rows {
		out[i] = ModelUsageResponse{
			ModelUsage: r,
			Model: &ModelRef{
				ID:          r.ModelID,
				Alias:       h.modelAlias(c.Request.Context(), r.ModelID),
				DisplayName: h.modelName(r.ModelDefKey),
				Provider:    r.Provider,
				ModelDefKey: r.ModelDefKey,
			},
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// getUsageByRouter godoc
// @Summary     Usage by router
// @Description Returns total requests, cost, and error count aggregated per router
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       from  query  string  false  "Start date YYYY-MM-DD"
// @Param       to    query  string  false  "End date YYYY-MM-DD"
// @Success     200  {object}  object{data=[]RouterUsageResponse}
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/routers [get]
func (h *Handler) getUsageByRouter(c *gin.Context) {
	orgID := orgIDFromCtx(c)
	from, to, ok := parseDateRangeOptional(c)
	if !ok {
		return
	}

	rows, err := h.svc.GetUsageByRouter(orgID, from, to)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]RouterUsageResponse, len(rows))
	for i, r := range rows {
		out[i] = RouterUsageResponse{
			RouterUsage: r,
			Router: &RouterRef{
				ID:   r.RouterID,
				Name: h.routerName(c.Request.Context(), r.RouterID),
			},
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// getRecentErrors godoc
// @Summary     Recent inference errors
// @Description Returns the most recent failed inference log entries, useful for debugging
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       limit  query  int  false  "Max entries to return (default 50, max 500)"
// @Success     200  {object}  object{data=[]obsDomain.InferenceLog}
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/errors [get]
func (h *Handler) getRecentErrors(c *gin.Context) {
	orgID := orgIDFromCtx(c)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := h.svc.GetRecentErrors(orgID, limit)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	enriched := make([]InferenceLogResponse, len(rows))
	for i, r := range rows {
		enriched[i] = h.enrichLog(c.Request.Context(), r)
	}
	c.JSON(http.StatusOK, gin.H{"data": enriched})
}

// listAuditLogs godoc
// @Summary     List audit logs
// @Description Returns admin-action audit log entries for the organisation, newest first
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       limit   query  int  false  "Max entries to return (default 50, max 200)"
// @Param       offset  query  int  false  "Number of entries to skip (default 0)"
// @Success     200  {object}  object{data=[]obsDomain.AuditLog,total=int}
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/audit [get]
func (h *Handler) listAuditLogs(c *gin.Context) {
	orgID := orgIDFromCtx(c)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	rows, total, err := h.svc.ListAuditLogs(orgID, limit, offset)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": total})
}

// getProviderHealth godoc
// @Summary     Provider health status
// @Description Returns the last health-check result for every registered model
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Success     200  {object}  object{data=[]obsDomain.ProviderHealth}
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /health/providers [get]
func (h *Handler) getProviderHealth(c *gin.Context) {
	rows, err := h.svc.ListProviderHealth(orgIDFromCtx(c))
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// ModelRef is the enriched model relation returned inside log responses.
type ModelRef struct {
	ID          string `json:"id"`
	Alias       string `json:"alias,omitempty"`
	DisplayName string `json:"displayName"`
	Provider    string `json:"provider"`
	ModelDefKey string `json:"modelDefKey"`
}

// RouterRef is the enriched router relation returned inside log responses.
type RouterRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// InferenceLogResponse is InferenceLog enriched with nested model/router refs and the pipeline trace.
type InferenceLogResponse struct {
	obsDomain.InferenceLog
	Model         *ModelRef       `json:"model,omitempty"`
	Router        *RouterRef      `json:"router,omitempty"`
	PipelineSteps json.RawMessage `json:"pipelineSteps,omitempty"`
}

func (r InferenceLogResponse) MarshalJSON() ([]byte, error) {
	raw, err := json.Marshal(r.InferenceLog)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	if r.Model != nil {
		body["model"] = r.Model
	}
	if r.Router != nil {
		body["router"] = r.Router
	}
	if len(r.PipelineSteps) > 0 {
		body["pipelineSteps"] = r.PipelineSteps
	}
	return json.Marshal(body)
}

// listInferenceLogs godoc
// @Summary     List inference logs
// @Description Returns a paginated list of raw inference log entries, optionally filtered by router, virtual key, status, or date range
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       routerId      query  string  false  "Filter by router ID"
// @Param       virtualKeyId  query  string  false  "Filter by virtual key ID"
// @Param       status        query  string  false  "Filter by status: success | error"
// @Param       from          query  string  false  "Start date YYYY-MM-DD"
// @Param       to            query  string  false  "End date YYYY-MM-DD"
// @Param       page          query  int     false  "Page number (default 1)"
// @Param       perPage       query  int     false  "Items per page (default 30, max 500)"
// @Success     200  {object}  pagination.Paginated[InferenceLogResponse]
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/inference-logs [get]
func (h *Handler) listInferenceLogs(c *gin.Context) {
	orgID := orgIDFromCtx(c)
	sl := pagination.ParseSlice(c)
	from, to, ok := parseDateRangeOptional(c)
	if !ok {
		return
	}

	routerID := c.Query("routerId")
	if len(routerID) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "routerId too long"})
		return
	}
	virtualKeyID := c.Query("virtualKeyId")
	if len(virtualKeyID) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "virtualKeyId too long"})
		return
	}
	status := c.Query("status")
	if status != "" && status != "success" && status != "error" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status must be 'success' or 'error'"})
		return
	}

	filter := obsDomain.InferenceLogFilter{
		OrgID:        orgID,
		RouterID:     routerID,
		VirtualKeyID: virtualKeyID,
		Status:       status,
		From:         from,
		To:           to,
	}

	logs, total, err := h.svc.ListInferenceLogs(filter, sl.PerPage, sl.Offset())
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	enriched := make([]InferenceLogResponse, len(logs))
	for i, l := range logs {
		enriched[i] = h.enrichLog(c.Request.Context(), l)
	}
	c.JSON(http.StatusOK, pagination.New(enriched, total, sl))
}

func (h *Handler) enrichLog(ctx context.Context, l obsDomain.InferenceLog) InferenceLogResponse {
	resp := InferenceLogResponse{InferenceLog: l}

	if l.ModelID != "" || l.ModelDefKey != "" {
		resp.Model = &ModelRef{
			ID:          l.ModelID,
			Alias:       h.modelAlias(ctx, l.ModelID),
			DisplayName: h.modelName(l.ModelDefKey),
			Provider:    l.Provider,
			ModelDefKey: l.ModelDefKey,
		}
	}

	if l.RouterID != "" {
		resp.Router = &RouterRef{
			ID:   l.RouterID,
			Name: h.routerName(ctx, l.RouterID),
		}
	}

	if l.PipelineTrace != "" {
		resp.PipelineSteps = json.RawMessage(l.PipelineTrace)
	}

	return resp
}

// getABTestResults godoc
// @Summary     A/B test variant results
// @Description Returns per-variant metrics for routers using the ab_test interceptor
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       routerId  query  string  true   "Router ID to fetch A/B results for"
// @Param       from      query  string  false  "Start date YYYY-MM-DD"
// @Param       to        query  string  false  "End date YYYY-MM-DD"
// @Success     200  {object}  object{data=[]obsDomain.ABVariantStats}
// @Failure     400  {object}  object{error=string}
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/ab-test [get]
func (h *Handler) getABTestResults(c *gin.Context) {
	routerID := c.Query("routerId")
	if routerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "routerId query param is required"})
		return
	}
	if len(routerID) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "routerId too long"})
		return
	}
	from, to, ok := parseDateRangeOptional(c)
	if !ok {
		return
	}
	rows, err := h.svc.GetABTestResults(orgIDFromCtx(c), routerID, from, to)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// getLatencyStats godoc
// @Summary     Latency percentiles by model
// @Description Returns p50/p95/p99 latency statistics per model for successful inferences
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       from  query  string  false  "Start date YYYY-MM-DD"
// @Param       to    query  string  false  "End date YYYY-MM-DD"
// @Success     200  {object}  object{data=[]obsDomain.LatencyStats}
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/latency [get]
func (h *Handler) getLatencyStats(c *gin.Context) {
	orgID := orgIDFromCtx(c)
	from, to, ok := parseDateRangeOptional(c)
	if !ok {
		return
	}
	rows, err := h.svc.GetLatencyStats(orgID, from, to)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// submitFeedback godoc
// @Summary     Submit inference feedback
// @Description Records a quality signal (1=positive, -1=negative, 0=clear) on an inference log entry
// @Tags        hyperstrate
// @Tags        observability
// @Accept      json
// @Produce     json
// @Param       id    path  string                        true  "Inference log ID"
// @Param       body  body  object{feedback=int}          true  "Feedback value"
// @Success     204
// @Failure     400  {object}  object{error=string}
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/inference-logs/{id}/feedback [patch]
func (h *Handler) submitFeedback(c *gin.Context) {
	id := c.Param("id")
	if id == "" || len(id) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var body struct {
		Feedback int `json:"feedback" binding:"required,oneof=-1 0 1"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.SubmitFeedback(orgIDFromCtx(c), id, body.Feedback); err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// getUsageByVirtualKey godoc
// @Summary     Usage by virtual key
// @Description Returns request count, token usage, cost, and error count aggregated per virtual key
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       from  query  string  false  "Start date YYYY-MM-DD"
// @Param       to    query  string  false  "End date YYYY-MM-DD"
// @Success     200  {object}  object{data=[]obsDomain.VirtualKeyUsage}
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/virtual-keys [get]
func (h *Handler) getUsageByVirtualKey(c *gin.Context) {
	orgID := orgIDFromCtx(c)
	from, to, ok := parseDateRangeOptional(c)
	if !ok {
		return
	}
	rows, err := h.svc.GetUsageByVirtualKey(orgID, from, to)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// getCacheStats godoc
// @Summary     Cache hit/miss statistics
// @Description Returns cache hit rate and breakdown (exact vs semantic) for the organisation
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       from  query  string  false  "Start date YYYY-MM-DD"
// @Param       to    query  string  false  "End date YYYY-MM-DD"
// @Success     200  {object}  obsDomain.CacheStats
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/cache [get]
func (h *Handler) getCacheStats(c *gin.Context) {
	orgID := orgIDFromCtx(c)
	from, to, ok := parseDateRangeOptional(c)
	if !ok {
		return
	}
	stats, err := h.svc.GetCacheStats(orgID, from, to)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// exportInferenceLogsCSV godoc
// @Summary     Export inference logs as CSV
// @Description Streams a CSV file of inference log entries matching the given filters
// @Tags        hyperstrate
// @Tags        observability
// @Produce     text/csv
// @Param       routerId      query  string  false  "Filter by router ID"
// @Param       virtualKeyId  query  string  false  "Filter by virtual key ID"
// @Param       status        query  string  false  "Filter by status: success | error"
// @Param       from          query  string  false  "Start date YYYY-MM-DD"
// @Param       to            query  string  false  "End date YYYY-MM-DD"
// @Success     200
// @Failure     400  {object}  object{error=string}
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/inference-logs/export [get]
func (h *Handler) exportInferenceLogsCSV(c *gin.Context) {
	orgID := orgIDFromCtx(c)
	from, to, ok := parseDateRangeOptional(c)
	if !ok {
		return
	}
	routerID := c.Query("routerId")
	virtualKeyID := c.Query("virtualKeyId")
	status := c.Query("status")
	if status != "" && status != "success" && status != "error" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status must be 'success' or 'error'"})
		return
	}

	filter := obsDomain.InferenceLogFilter{
		OrgID:        orgID,
		RouterID:     routerID,
		VirtualKeyID: virtualKeyID,
		Status:       status,
		From:         from,
		To:           to,
	}

	// Fetch up to 50k rows for export.
	logs, _, err := h.svc.ListInferenceLogs(filter, 50000, 0)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", `attachment; filename="inference-logs.csv"`)
	w := c.Writer
	fmt.Fprintln(w, "id,createdAt,orgId,routerId,virtualKeyId,modelId,modelDefKey,provider,status,source,inputTokens,outputTokens,totalTokens,costUsd,latencyMs,cacheHit,cacheHitType,abVariant,feedback,errorMessage")
	for _, l := range logs {
		fmt.Fprintf(w, "%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%d,%d,%d,%.8f,%d,%t,%s,%s,%d,%s\n",
			l.ID, l.CreatedAt.Format(time.RFC3339), l.OrgID,
			l.RouterID, l.VirtualKeyID, l.ModelID, l.ModelDefKey, l.Provider,
			l.Status, string(l.Source),
			l.InputTokens, l.OutputTokens, l.TotalTokens,
			l.CostUSD, l.LatencyMs,
			l.CacheHit, l.CacheHitType, l.ABVariant,
			l.Feedback, csvEscape(l.ErrorMessage),
		)
	}
}

// listWebhookDeliveries godoc
// @Summary     List webhook delivery attempts for a router
// @Description Returns recent webhook delivery records (success and failure) for the given router
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       routerId  path   string  true   "Router ID"
// @Param       limit     query  int     false  "Max entries to return (default 50, max 200)"
// @Param       offset    query  int     false  "Number of entries to skip (default 0)"
// @Success     200  {object}  object{data=[]obsDomain.WebhookDelivery,total=int}
// @Failure     400  {object}  object{error=string}
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/routers/{routerId}/webhook-deliveries [get]
func (h *Handler) listWebhookDeliveries(c *gin.Context) {
	routerID := c.Param("routerId")
	if routerID == "" || len(routerID) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid routerId"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, total, err := h.svc.ListWebhookDeliveries(orgIDFromCtx(c), routerID, limit, offset)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": total})
}

// getInferencePayload godoc
// @Summary     Get stored inference payload
// @Description Returns the full request fields and response content for a log entry (only available when store_payloads is enabled on the router)
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       id  path  string  true  "Inference log ID"
// @Success     200  {object}  obsDomain.InferencePayload
// @Failure     404  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/inference-logs/{id}/payload [get]
func (h *Handler) getInferencePayload(c *gin.Context) {
	id := c.Param("id")
	if id == "" || len(id) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	payload, err := h.svc.GetPayload(orgIDFromCtx(c), id)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if payload == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "payload not found for this log entry"})
		return
	}
	c.JSON(http.StatusOK, payload)
}

// replayInference godoc
// @Summary     Replay an inference log entry
// @Description Re-runs the stored request through the same router and returns the new response
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       id  path  string  true  "Inference log ID"
// @Success     200  {object}  ReplayResult
// @Failure     404  {object}  object{error=string}
// @Failure     422  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/inference-logs/{id}/replay [post]
func (h *Handler) replayInference(c *gin.Context) {
	if h.replay == nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "replay not available"})
		return
	}
	id := c.Param("id")
	if id == "" || len(id) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	payload, err := h.svc.GetPayload(orgIDFromCtx(c), id)
	if err != nil || payload == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "payload not found — enable store_payloads on the router first"})
		return
	}
	var fields map[string]string
	if err := json.Unmarshal([]byte(payload.RequestFields), &fields); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "stored payload could not be parsed"})
		return
	}
	result, err := h.replay(c.Request.Context(), payload.RouterID, fields)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func csvEscape(s string) string {
	if s == "" {
		return ""
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// getRouterPipelineStats godoc
// @Summary     Router pipeline stats
// @Description Returns a breakdown of pipeline feature and interceptor outcomes for a specific router, derived from stored pipeline traces. Cache stats come from the database; feature/interceptor stats are aggregated from the most recent 500 traced requests in the date range.
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       routerId  path   string  true   "Router ID"
// @Param       from      query  string  false  "Start date YYYY-MM-DD (default: 30 days ago)"
// @Param       to        query  string  false  "End date YYYY-MM-DD (default: today)"
// @Success     200  {object}  obsDomain.RouterPipelineStats
// @Failure     400  {object}  object{error=string}
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/routers/{routerId}/pipeline-stats [get]
func (h *Handler) getRouterPipelineStats(c *gin.Context) {
	routerID := c.Param("routerId")
	if routerID == "" || len(routerID) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid routerId"})
		return
	}
	from, to, ok := parseDateRangeOptional(c)
	if !ok {
		return
	}
	stats, err := h.svc.GetRouterPipelineStats(orgIDFromCtx(c), routerID, from, to)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func orgIDFromCtx(c *gin.Context) string {
	return domain.OrgIDFromContext(c.Request.Context())
}

func parseDateRange(c *gin.Context) (from, to time.Time, ok bool) {
	fromStr := c.DefaultQuery("from", time.Now().AddDate(0, -1, 0).Format("2006-01-02"))
	toStr := c.DefaultQuery("to", time.Now().Format("2006-01-02"))

	var err error
	from, err = time.Parse("2006-01-02", fromStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'from' date, use YYYY-MM-DD"})
		return time.Time{}, time.Time{}, false
	}
	to, err = time.Parse("2006-01-02", toStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'to' date, use YYYY-MM-DD"})
		return time.Time{}, time.Time{}, false
	}
	// end of day
	to = to.Add(24*time.Hour - time.Second)
	return from, to, true
}

func parseDateRangeOptional(c *gin.Context) (from, to *time.Time, ok bool) {
	if s := c.Query("from"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'from' date, use YYYY-MM-DD"})
			return nil, nil, false
		}
		from = &t
	}
	if s := c.Query("to"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'to' date, use YYYY-MM-DD"})
			return nil, nil, false
		}
		t = t.Add(24*time.Hour - time.Second)
		to = &t
	}
	return from, to, true
}

// listAgentSessions godoc
// @Summary     List agent sessions
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       agent        query  string  false  "Filter by agent (e.g. claude_code, cursor)"
// @Param       routerId      query  string  false  "Filter by router ID"
// @Param       virtualKeyId  query  string  false  "Filter by virtual key ID"
// @Param       userId        query  string  false  "Filter by user ID"
// @Param       from          query  string  false  "Start date YYYY-MM-DD"
// @Param       to            query  string  false  "End date YYYY-MM-DD"
// @Param       page          query  int     false  "Page number (default 1)"
// @Param       perPage       query  int     false  "Items per page (default 30, max 500)"
// @Success     200  {object}  pagination.Paginated[obsDomain.AgentSessionSummary]
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/agent-sessions [get]
func (h *Handler) listAgentSessions(c *gin.Context) {
	orgID := orgIDFromCtx(c)
	from, to, ok := parseDateRangeOptional(c)
	if !ok {
		return
	}
	slice := pagination.ParseSlice(c)
	filter := obsDomain.InferenceLogFilter{
		OrgID:        orgID,
		RouterID:     c.Query("routerId"),
		VirtualKeyID: c.Query("virtualKeyId"),
		UserID:       c.Query("userId"),
		Agent:        c.Query("agent"),
		From:         from,
		To:           to,
	}
	rows, total, err := h.svc.ListAgentSessions(filter, slice.PerPage, slice.Offset())
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, pagination.New(rows, total, slice))
}

// listAgentSessionLogs godoc
// @Summary     List inference logs for an agent session
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       sessionId  path   string  true   "Agent session ID"
// @Param       from       query  string  false  "Start date YYYY-MM-DD"
// @Param       to         query  string  false  "End date YYYY-MM-DD"
// @Param       page       query  int     false  "Page number (default 1)"
// @Param       perPage    query  int     false  "Items per page (default 30, max 500)"
// @Success     200  {object}  pagination.Paginated[InferenceLogResponse]
// @Failure     400  {object}  object{error=string}
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/agent-sessions/{sessionId}/logs [get]
func (h *Handler) listAgentSessionLogs(c *gin.Context) {
	orgID := orgIDFromCtx(c)
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sessionId required"})
		return
	}
	from, to, ok := parseDateRangeOptional(c)
	if !ok {
		return
	}
	slice := pagination.ParseSlice(c)
	filter := obsDomain.InferenceLogFilter{
		OrgID:          orgID,
		AgentSessionID: sessionID,
		From:           from,
		To:             to,
	}
	logs, total, err := h.svc.ListInferenceLogs(filter, slice.PerPage, slice.Offset())
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	enriched := make([]InferenceLogResponse, len(logs))
	for i, l := range logs {
		enriched[i] = h.enrichLog(ctx, l)
	}
	c.JSON(http.StatusOK, pagination.New(enriched, total, slice))
}

// getAgentSessionInsights godoc
// @Summary     Get insights for an agent session
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       sessionId  path  string  true  "Agent session ID"
// @Success     200  {object}  object{data=obsDomain.AgentSessionInsights}
// @Failure     400  {object}  object{error=string}
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/agent-sessions/{sessionId}/insights [get]
func (h *Handler) getAgentSessionInsights(c *gin.Context) {
	orgID := orgIDFromCtx(c)
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sessionId required"})
		return
	}
	filter := obsDomain.InferenceLogFilter{OrgID: orgID}
	insights, err := h.svc.GetAgentSessionInsights(filter, sessionID)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": insights})
}

// listAgentSessionEvents godoc
// @Summary     List events for an agent session
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       sessionId  path  string  true  "Agent session ID"
// @Param       page       query  int    false  "Page number (default 1)"
// @Param       perPage    query  int    false  "Items per page (default 30, max 500)"
// @Success     200  {object}  pagination.Paginated[obsDomain.AgentSessionEvent]
// @Failure     400  {object}  object{error=string}
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/agent-sessions/{sessionId}/events [get]
func (h *Handler) listAgentSessionEvents(c *gin.Context) {
	orgID := orgIDFromCtx(c)
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sessionId required"})
		return
	}
	slice := pagination.ParseSlice(c)
	filter := obsDomain.InferenceLogFilter{OrgID: orgID, AgentSessionID: sessionID}
	rows, total, err := h.svc.ListAgentSessionEvents(filter, slice.PerPage, slice.Offset())
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, pagination.New(rows, total, slice))
}

// listToolArchives godoc
// @Summary     List tool call archives
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       sessionId  query  string  false  "Filter by agent session ID"
// @Param       page       query  int     false  "Page number (default 1)"
// @Param       perPage    query  int     false  "Items per page (default 30, max 500)"
// @Success     200  {object}  pagination.Paginated[obsDomain.ToolCallArchive]
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/tool-archives [get]
func (h *Handler) listToolArchives(c *gin.Context) {
	orgID := orgIDFromCtx(c)
	slice := pagination.ParseSlice(c)
	filter := obsDomain.InferenceLogFilter{
		OrgID:          orgID,
		AgentSessionID: c.Query("sessionId"),
	}
	rows, total, err := h.svc.ListToolArchives(filter, slice.PerPage, slice.Offset())
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, pagination.New(rows, total, slice))
}

// getToolArchive godoc
// @Summary     Get a tool call archive by ID
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       id  path  string  true  "Tool archive ID"
// @Success     200  {object}  object{data=obsDomain.ToolCallArchive}
// @Failure     400  {object}  object{error=string}
// @Failure     404  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/tool-archives/{id} [get]
func (h *Handler) getToolArchive(c *gin.Context) {
	orgID := orgIDFromCtx(c)
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id required"})
		return
	}
	archive, err := h.svc.GetToolArchive(orgID, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": archive})
}

// listCompressionEvents godoc
// @Summary     List context compression events
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       sessionId  query  string  false  "Filter by agent session ID"
// @Param       page       query  int     false  "Page number (default 1)"
// @Param       perPage    query  int     false  "Items per page (default 30, max 500)"
// @Success     200  {object}  pagination.Paginated[obsDomain.CompressionEvent]
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/compression-events [get]
func (h *Handler) listCompressionEvents(c *gin.Context) {
	orgID := orgIDFromCtx(c)
	slice := pagination.ParseSlice(c)
	filter := obsDomain.InferenceLogFilter{
		OrgID:          orgID,
		AgentSessionID: c.Query("sessionId"),
	}
	rows, total, err := h.svc.ListCompressionEvents(filter, slice.PerPage, slice.Offset())
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, pagination.New(rows, total, slice))
}

// listCostlyPrompts godoc
// @Summary     List costly prompt turns
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Success     200  {object}  object{data=[]obsDomain.CostlyPrompt}
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/costly-prompts [get]
func (h *Handler) listCostlyPrompts(c *gin.Context) {
	orgID := orgIDFromCtx(c)
	filter := obsDomain.InferenceLogFilter{OrgID: orgID}
	rows, err := h.svc.ListCostlyPrompts(filter, 20)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// listSubagentBreakdown godoc
// @Summary     List subagent cost breakdown
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       sessionId  query  string  false  "Filter by parent session ID"
// @Success     200  {object}  object{data=[]obsDomain.SubagentBreakdown}
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/subagents [get]
func (h *Handler) listSubagentBreakdown(c *gin.Context) {
	orgID := orgIDFromCtx(c)
	filter := obsDomain.InferenceLogFilter{
		OrgID:          orgID,
		AgentSessionID: c.Query("sessionId"),
	}
	rows, err := h.svc.ListSubagentBreakdown(filter)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// listLoopDetections godoc
// @Summary     List loop detections
// @Tags        hyperstrate
// @Tags        observability
// @Produce     json
// @Param       sessionId  query  string  false  "Filter by agent session ID"
// @Success     200  {object}  object{data=[]obsDomain.LoopDetection}
// @Failure     500  {object}  object{error=string}
// @Security    BearerAuth
// @Router      /analytics/loops [get]
func (h *Handler) listLoopDetections(c *gin.Context) {
	orgID := orgIDFromCtx(c)
	filter := obsDomain.InferenceLogFilter{
		OrgID:          orgID,
		AgentSessionID: c.Query("sessionId"),
	}
	rows, err := h.svc.ListLoopDetections(filter, 50)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}
