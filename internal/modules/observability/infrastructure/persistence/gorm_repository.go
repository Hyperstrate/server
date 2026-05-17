package persistence

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"hyperstrate/server/internal/modules/observability/domain"

	"gorm.io/gorm"
)

// ── InferenceLog repo ─────────────────────────────────────────────────────────

type gormInferenceLogRepo struct{ db *gorm.DB }

func NewInferenceLogRepository(db *gorm.DB) domain.InferenceLogRepository {
	return &gormInferenceLogRepo{db: db}
}

type sqlTimestamp struct {
	time.Time
	Valid bool
}

func (t *sqlTimestamp) Scan(value any) error {
	if value == nil {
		t.Valid = false
		t.Time = time.Time{}
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		t.Time = v
		t.Valid = true
		return nil
	case string:
		return t.scanString(v)
	case []byte:
		return t.scanString(string(v))
	default:
		return fmt.Errorf("unsupported timestamp scan type %T", value)
	}
}

func (t sqlTimestamp) Value() (driver.Value, error) {
	if !t.Valid {
		return nil, nil
	}
	return t.Time, nil
}

func (t *sqlTimestamp) scanString(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		t.Valid = false
		t.Time = time.Time{}
		return nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
	}
	var lastErr error
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			t.Time = parsed
			t.Valid = true
			return nil
		}
		lastErr = err
	}
	return fmt.Errorf("parse timestamp %q: %w", value, lastErr)
}

func (r *gormInferenceLogRepo) Create(log *domain.InferenceLog) error {
	return r.db.Create(log).Error
}

func (r *gormInferenceLogRepo) DeleteOlderThan(cutoff time.Time) error {
	return r.db.Where("created_at < ?", cutoff).Delete(&domain.InferenceLog{}).Error
}

func (r *gormInferenceLogRepo) List(filter domain.InferenceLogFilter, limit, offset int) ([]domain.InferenceLog, int64, error) {
	q := r.db.Model(&domain.InferenceLog{})
	q = applyFilter(q, filter)
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return nil, 0, err
	}
	var logs []domain.InferenceLog
	return logs, count, q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&logs).Error
}

func (r *gormInferenceLogRepo) ListAgentSessions(filter domain.InferenceLogFilter, limit, offset int) ([]domain.AgentSessionSummary, int64, error) {
	q := r.db.Model(&domain.InferenceLog{}).Where("agent_session_id != ''")
	q = applyFilter(q, filter)

	var total int64
	if err := r.db.Table("(?) AS sessions", q.Select("agent_session_id").Group("agent_session_id")).
		Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []struct {
		SessionID         string
		Agent             string
		RouterID          string
		VirtualKeyID      string
		TeamID            string
		UserID            string
		StartedAt         sqlTimestamp
		LastSeenAt        sqlTimestamp
		Turns             int64
		InputTokens       int64
		OutputTokens      int64
		CachedInputTokens int64
		TotalTokens       int64
		CostUSD           float64
		CacheHits         int64
		ErrorCount        int64
		ToolCallCount     int64
		ToolResultChars   int64
		AvgQualityScore   float64
		AvgContextFillPct float64
		LoopCount         int64
		SubagentCostUSD   float64
	}
	err := q.Select(`agent_session_id AS session_id,
			MAX(agent) AS agent,
			MAX(router_id) AS router_id,
			MAX(virtual_key_id) AS virtual_key_id,
			MAX(team_id) AS team_id,
			MAX(user_id) AS user_id,
			MIN(created_at) AS started_at,
			MAX(created_at) AS last_seen_at,
			COUNT(*) AS turns,
			SUM(input_tokens) AS input_tokens,
			SUM(output_tokens) AS output_tokens,
			SUM(cached_input_tokens) AS cached_input_tokens,
			SUM(total_tokens) AS total_tokens,
			SUM(cost_usd) AS cost_usd,
			SUM(CASE WHEN cache_hit = true THEN 1 ELSE 0 END) AS cache_hits,
			SUM(CASE WHEN status='error' THEN 1 ELSE 0 END) AS error_count,
			SUM(tool_call_count) AS tool_call_count,
			SUM(tool_result_chars) AS tool_result_chars,
			AVG(CASE WHEN quality_score > 0 THEN quality_score ELSE NULL END) AS avg_quality_score,
			AVG(CASE WHEN context_fill_pct > 0 THEN context_fill_pct ELSE NULL END) AS avg_context_fill_pct,
			SUM(CASE WHEN loop_detected = true THEN 1 ELSE 0 END) AS loop_count,
			SUM(CASE WHEN agent_role != '' AND agent_role NOT IN ('orchestrator','primary','main') THEN cost_usd ELSE 0 END) AS subagent_cost_usd`).
		Group("agent_session_id").
		Order("last_seen_at DESC").
		Limit(limit).
		Offset(offset).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	summaries := make([]domain.AgentSessionSummary, 0, len(rows))
	for i := range rows {
		row := rows[i]
		summary := domain.AgentSessionSummary{
			SessionID:         row.SessionID,
			Agent:             row.Agent,
			RouterID:          row.RouterID,
			VirtualKeyID:      row.VirtualKeyID,
			TeamID:            row.TeamID,
			UserID:            row.UserID,
			StartedAt:         row.StartedAt.Time,
			LastSeenAt:        row.LastSeenAt.Time,
			Turns:             row.Turns,
			InputTokens:       row.InputTokens,
			OutputTokens:      row.OutputTokens,
			CachedInputTokens: row.CachedInputTokens,
			TotalTokens:       row.TotalTokens,
			CostUSD:           row.CostUSD,
			CacheHits:         row.CacheHits,
			ErrorCount:        row.ErrorCount,
			ToolCallCount:     row.ToolCallCount,
			ToolResultChars:   row.ToolResultChars,
			AvgQualityScore:   row.AvgQualityScore,
			AvgContextFillPct: row.AvgContextFillPct,
			LoopCount:         row.LoopCount,
			SubagentCostUSD:   row.SubagentCostUSD,
		}
		summary.CompressionEvents, summary.CompressionSavedChars = r.compressionSummary(filter.OrgID, row.SessionID)
		summary.Checkpoints = r.eventCount(filter.OrgID, row.SessionID)
		summaries = append(summaries, summary)
	}
	return summaries, total, nil
}

func (r *gormInferenceLogRepo) ListCostlyPrompts(filter domain.InferenceLogFilter, limit int) ([]domain.CostlyPrompt, error) {
	q := r.db.Table("inference_logs AS l").
		Select(`l.id AS log_id, l.agent_session_id, l.agent, l.router_id, l.model_def_key, l.cost_usd, l.total_tokens, l.created_at, p.request_fields`).
		Joins("LEFT JOIN inference_payloads AS p ON p.log_id = l.id").
		Where("l.cost_usd > 0")
	q = applyFilterWithAlias(q, filter, "l")
	var rows []struct {
		LogID          string
		AgentSessionID string
		Agent          string
		RouterID       string
		ModelDefKey    string
		CostUSD        float64
		TotalTokens    int64
		CreatedAt      time.Time
		RequestFields  string
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	if err := q.Order("l.cost_usd DESC").Limit(limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.CostlyPrompt, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.CostlyPrompt{
			LogID:          row.LogID,
			AgentSessionID: row.AgentSessionID,
			Agent:          row.Agent,
			RouterID:       row.RouterID,
			ModelDefKey:    row.ModelDefKey,
			CostUSD:        row.CostUSD,
			TotalTokens:    row.TotalTokens,
			PromptPreview:  promptPreview(row.RequestFields),
			CreatedAt:      row.CreatedAt,
		})
	}
	return out, nil
}

func (r *gormInferenceLogRepo) ListSubagentBreakdown(filter domain.InferenceLogFilter) ([]domain.SubagentBreakdown, error) {
	q := r.db.Model(&domain.InferenceLog{}).
		Select(`agent_session_id,
			parent_session_id,
			agent_role,
			MAX(agent) AS agent,
			COUNT(*) AS turns,
			SUM(total_tokens) AS total_tokens,
			SUM(cost_usd) AS cost_usd,
			SUM(tool_call_count) AS tool_call_count,
			AVG(CASE WHEN quality_score > 0 THEN quality_score ELSE NULL END) AS avg_quality_score`).
		Where("agent_session_id != '' AND (parent_session_id != '' OR agent_role != '')").
		Group("agent_session_id, parent_session_id, agent_role").
		Order("cost_usd DESC")
	q = applyFilter(q, filter)
	var rows []domain.SubagentBreakdown
	return rows, q.Scan(&rows).Error
}

func (r *gormInferenceLogRepo) ListLoopDetections(filter domain.InferenceLogFilter, limit int) ([]domain.LoopDetection, error) {
	q := r.db.Model(&domain.InferenceLog{}).
		Select("id AS log_id, agent_session_id, turn_index, loop_reason AS reason, cost_usd, created_at").
		Where("loop_detected = true")
	q = applyFilter(q, filter)
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	var rows []domain.LoopDetection
	return rows, q.Order("created_at DESC").Limit(limit).Scan(&rows).Error
}

func (r *gormInferenceLogRepo) compressionSummary(orgID, sessionID string) (int64, int64) {
	if sessionID == "" {
		return 0, 0
	}
	var row struct {
		Count int64
		Saved int64
	}
	q := r.db.Model(&domain.CompressionEvent{}).
		Select("COUNT(*) AS count, COALESCE(SUM(saved_chars), 0) AS saved").
		Where("agent_session_id = ?", sessionID)
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}
	_ = q.Scan(&row).Error
	return row.Count, row.Saved
}

func (r *gormInferenceLogRepo) eventCount(orgID, sessionID string) int64 {
	if sessionID == "" {
		return 0
	}
	var count int64
	q := r.db.Model(&domain.AgentSessionEvent{}).Where("agent_session_id = ?", sessionID)
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}
	_ = q.Count(&count).Error
	return count
}

func (r *gormInferenceLogRepo) AggregateUsage(filter domain.InferenceLogFilter, gran domain.Granularity) ([]domain.AggregatedUsage, error) {
	bucketExpr := timeBucketExpr(gran)
	q := r.db.Model(&domain.InferenceLog{}).
		Select(fmt.Sprintf(`%s AS bucket,
			COUNT(*) AS requests,
			SUM(input_tokens)  AS input_tokens,
			SUM(output_tokens) AS output_tokens,
			SUM(cost_usd)      AS cost_usd,
			SUM(CASE WHEN status='error' THEN 1 ELSE 0 END) AS error_count,
			AVG(latency_ms)    AS avg_latency_ms`, bucketExpr)).
		Group("bucket").
		Order("bucket ASC")

	q = applyFilter(q, filter)

	var rows []domain.AggregatedUsage
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *gormInferenceLogRepo) AggregateByModel(filter domain.InferenceLogFilter) ([]domain.ModelUsage, error) {
	q := r.db.Model(&domain.InferenceLog{}).
		Select(`model_id, model_def_key, provider,
			COUNT(*) AS requests,
			SUM(total_tokens) AS total_tokens,
			SUM(cost_usd)     AS cost_usd,
			SUM(CASE WHEN status='error' THEN 1 ELSE 0 END) AS error_count`).
		Group("model_id, model_def_key, provider").
		Order("requests DESC")

	q = applyFilter(q, filter)

	var rows []domain.ModelUsage
	return rows, q.Scan(&rows).Error
}

func (r *gormInferenceLogRepo) AggregateByRouter(filter domain.InferenceLogFilter) ([]domain.RouterUsage, error) {
	q := r.db.Model(&domain.InferenceLog{}).
		Where("router_id != ''").
		Select(`router_id,
			COUNT(*) AS requests,
			SUM(cost_usd) AS cost_usd,
			SUM(CASE WHEN status='error' THEN 1 ELSE 0 END) AS errors`).
		Group("router_id").
		Order("requests DESC")

	q = applyFilter(q, filter)

	var rows []domain.RouterUsage
	return rows, q.Scan(&rows).Error
}

func (r *gormInferenceLogRepo) AggregateByABVariant(orgID, routerID string, from, to *time.Time) ([]domain.ABVariantStats, error) {
	q := r.db.Model(&domain.InferenceLog{}).
		Where("router_id = ? AND ab_variant != ''", routerID).
		Select(`ab_variant AS variant,
			COUNT(*) AS requests,
			SUM(CASE WHEN status='error' THEN 1 ELSE 0 END) AS error_count,
			AVG(latency_ms) AS avg_latency_ms,
			SUM(total_tokens) AS total_tokens,
			SUM(cost_usd) AS cost_usd`).
		Group("ab_variant").
		Order("requests DESC")

	if from != nil {
		q = q.Where("created_at >= ?", *from)
	}
	if to != nil {
		q = q.Where("created_at <= ?", *to)
	}
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}

	var rows []domain.ABVariantStats
	return rows, q.Scan(&rows).Error
}

func (r *gormInferenceLogRepo) UpdateFeedback(orgID, id string, feedback int) error {
	q := r.db.Model(&domain.InferenceLog{}).Where("id = ?", id)
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}
	return q.Update("feedback", feedback).Error
}

type latencyRow struct {
	ModelDefKey string `gorm:"column:model_def_key"`
	Provider    string `gorm:"column:provider"`
	LatencyMs   int64  `gorm:"column:latency_ms"`
}

func (r *gormInferenceLogRepo) LatencyStatsByModel(filter domain.InferenceLogFilter) ([]domain.LatencyStats, error) {
	var rows []latencyRow
	q := r.db.Model(&domain.InferenceLog{}).
		Select("model_def_key, provider, latency_ms").
		Where("status = 'success' AND latency_ms > 0").
		Order("model_def_key, latency_ms").
		Limit(50000)
	q = applyFilter(q, filter)
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}

	// Group sorted values by model and compute percentiles in Go (SQLite compatible).
	type group struct {
		provider  string
		latencies []int64
	}
	groups := map[string]*group{}
	order := []string{}
	for _, row := range rows {
		if _, ok := groups[row.ModelDefKey]; !ok {
			groups[row.ModelDefKey] = &group{provider: row.Provider}
			order = append(order, row.ModelDefKey)
		}
		groups[row.ModelDefKey].latencies = append(groups[row.ModelDefKey].latencies, row.LatencyMs)
	}

	out := make([]domain.LatencyStats, 0, len(groups))
	for _, key := range order {
		g := groups[key]
		lat := g.latencies // already sorted from ORDER BY
		n := len(lat)
		var avg float64
		for _, v := range lat {
			avg += float64(v)
		}
		if n > 0 {
			avg /= float64(n)
		}
		out = append(out, domain.LatencyStats{
			ModelDefKey: key,
			Provider:    g.provider,
			Count:       int64(n),
			AvgMs:       avg,
			P50Ms:       lat[percentileIdx(n, 50)],
			P95Ms:       lat[percentileIdx(n, 95)],
			P99Ms:       lat[percentileIdx(n, 99)],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out, nil
}

func percentileIdx(n, pct int) int {
	idx := int(float64(n) * float64(pct) / 100.0)
	if idx >= n {
		idx = n - 1
	}
	return idx
}

func (r *gormInferenceLogRepo) RecentErrors(orgID string, limit int) ([]domain.InferenceLog, error) {
	var logs []domain.InferenceLog
	q := r.db.Model(&domain.InferenceLog{}).
		Where("status = 'error'")
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}
	return logs, q.Order("created_at DESC").Limit(limit).Find(&logs).Error
}

func (r *gormInferenceLogRepo) AggregateByVirtualKey(orgID string, from, to *time.Time) ([]domain.VirtualKeyUsage, error) {
	q := r.db.Model(&domain.InferenceLog{}).
		Where("org_id = ? AND virtual_key_id != ''", orgID).
		Select(`virtual_key_id,
			COUNT(*) AS requests,
			SUM(total_tokens) AS total_tokens,
			SUM(cost_usd) AS cost_usd,
			SUM(CASE WHEN status='error' THEN 1 ELSE 0 END) AS error_count`).
		Group("virtual_key_id").
		Order("requests DESC")
	if from != nil {
		q = q.Where("created_at >= ?", *from)
	}
	if to != nil {
		q = q.Where("created_at <= ?", *to)
	}
	var rows []domain.VirtualKeyUsage
	return rows, q.Scan(&rows).Error
}

func (r *gormInferenceLogRepo) CacheStats(orgID string, from, to *time.Time) (*domain.CacheStats, error) {
	q := r.db.Model(&domain.InferenceLog{}).Where("org_id = ?", orgID)
	if from != nil {
		q = q.Where("created_at >= ?", *from)
	}
	if to != nil {
		q = q.Where("created_at <= ?", *to)
	}
	var result struct {
		Total        int64 `gorm:"column:total"`
		Hits         int64 `gorm:"column:hits"`
		ExactHits    int64 `gorm:"column:exact_hits"`
		SemanticHits int64 `gorm:"column:semantic_hits"`
	}
	err := q.Select(`COUNT(*) AS total,
		SUM(CASE WHEN cache_hit = true THEN 1 ELSE 0 END) AS hits,
		SUM(CASE WHEN cache_hit_type='exact' THEN 1 ELSE 0 END) AS exact_hits,
		SUM(CASE WHEN cache_hit_type='semantic' THEN 1 ELSE 0 END) AS semantic_hits`).
		Scan(&result).Error
	if err != nil {
		return nil, err
	}
	hitRate := 0.0
	if result.Total > 0 {
		hitRate = float64(result.Hits) / float64(result.Total) * 100
	}
	return &domain.CacheStats{
		TotalRequests: result.Total,
		CacheHits:     result.Hits,
		ExactHits:     result.ExactHits,
		SemanticHits:  result.SemanticHits,
		HitRatePct:    hitRate,
	}, nil
}

func (r *gormInferenceLogRepo) RouterCacheQuery(orgID, routerID string, from, to *time.Time) (*domain.RouterCacheResult, error) {
	q := r.db.Model(&domain.InferenceLog{}).Where("router_id = ?", routerID)
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}
	if from != nil {
		q = q.Where("created_at >= ?", *from)
	}
	if to != nil {
		q = q.Where("created_at <= ?", *to)
	}
	var result struct {
		Total        int64   `gorm:"column:total"`
		ExactHits    int64   `gorm:"column:exact_hits"`
		SemanticHits int64   `gorm:"column:semantic_hits"`
		AvgMissCost  float64 `gorm:"column:avg_miss_cost"`
	}
	err := q.Select(`COUNT(*) AS total,
		SUM(CASE WHEN cache_hit_type='exact'    THEN 1 ELSE 0 END) AS exact_hits,
		SUM(CASE WHEN cache_hit_type='semantic' THEN 1 ELSE 0 END) AS semantic_hits,
		AVG(CASE WHEN cache_hit = false AND cost_usd > 0 THEN cost_usd ELSE NULL END) AS avg_miss_cost`).
		Scan(&result).Error
	if err != nil {
		return nil, err
	}
	return &domain.RouterCacheResult{
		TotalRequests:  result.Total,
		ExactHits:      result.ExactHits,
		SemanticHits:   result.SemanticHits,
		AvgMissCostUSD: result.AvgMissCost,
	}, nil
}

func (r *gormInferenceLogRepo) ListTracesForRouter(orgID, routerID string, from, to *time.Time, limit int) ([]domain.InferenceLog, error) {
	q := r.db.Model(&domain.InferenceLog{}).
		Select("id, cost_usd, pipeline_trace").
		Where("router_id = ? AND pipeline_trace != ''", routerID).
		Order("created_at DESC").
		Limit(limit)
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}
	if from != nil {
		q = q.Where("created_at >= ?", *from)
	}
	if to != nil {
		q = q.Where("created_at <= ?", *to)
	}
	var logs []domain.InferenceLog
	return logs, q.Find(&logs).Error
}

func applyFilter(q *gorm.DB, f domain.InferenceLogFilter) *gorm.DB {
	if f.OrgID != "" {
		q = q.Where("org_id = ?", f.OrgID)
	}
	if f.RouterID != "" {
		q = q.Where("router_id = ?", f.RouterID)
	}
	if f.VirtualKeyID != "" {
		q = q.Where("virtual_key_id = ?", f.VirtualKeyID)
	}
	if f.UserID != "" {
		q = q.Where("user_id = ?", f.UserID)
	}
	if f.AgentSessionID != "" {
		q = q.Where("agent_session_id = ?", f.AgentSessionID)
	}
	if f.Agent != "" {
		q = applyLooseTextFilter(q, "agent", f.Agent)
	}
	if f.ModelID != "" {
		q = q.Where("model_id = ?", f.ModelID)
	}
	if f.Source != "" {
		q = q.Where("source = ?", f.Source)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.From != nil {
		q = q.Where("created_at >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("created_at <= ?", *f.To)
	}
	return q
}

func applyFilterWithAlias(q *gorm.DB, f domain.InferenceLogFilter, alias string) *gorm.DB {
	col := func(name string) string { return alias + "." + name }
	if f.OrgID != "" {
		q = q.Where(col("org_id")+" = ?", f.OrgID)
	}
	if f.RouterID != "" {
		q = q.Where(col("router_id")+" = ?", f.RouterID)
	}
	if f.VirtualKeyID != "" {
		q = q.Where(col("virtual_key_id")+" = ?", f.VirtualKeyID)
	}
	if f.UserID != "" {
		q = q.Where(col("user_id")+" = ?", f.UserID)
	}
	if f.AgentSessionID != "" {
		q = q.Where(col("agent_session_id")+" = ?", f.AgentSessionID)
	}
	if f.Agent != "" {
		q = applyLooseTextFilter(q, col("agent"), f.Agent)
	}
	if f.ModelID != "" {
		q = q.Where(col("model_id")+" = ?", f.ModelID)
	}
	if f.Source != "" {
		q = q.Where(col("source")+" = ?", f.Source)
	}
	if f.Status != "" {
		q = q.Where(col("status")+" = ?", f.Status)
	}
	if f.From != nil {
		q = q.Where(col("created_at")+" >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where(col("created_at")+" <= ?", *f.To)
	}
	return q
}

func applyLooseTextFilter(q *gorm.DB, column, value string) *gorm.DB {
	raw := strings.TrimSpace(strings.ToLower(value))
	if raw == "" {
		return q
	}
	spaced := normalizeSearchWords(raw)
	compact := compactSearchText(raw)

	clauses := []string{"LOWER(" + column + ") LIKE ? ESCAPE '\\'"}
	args := []any{likeContains(raw)}

	if spaced != "" {
		clauses = append(clauses, normalizedTextExpr(column)+" LIKE ? ESCAPE '\\'")
		args = append(args, likeContains(spaced))
	}
	if compact != "" {
		clauses = append(clauses, compactTextExpr(column)+" LIKE ? ESCAPE '\\'")
		args = append(args, likeContains(compact))
	}

	return q.Where("("+strings.Join(clauses, " OR ")+")", args...)
}

func normalizedTextExpr(column string) string {
	return "LOWER(REPLACE(REPLACE(REPLACE(REPLACE(" + column + ", '_', ' '), '-', ' '), '.', ' '), '/', ' '))"
}

func compactTextExpr(column string) string {
	return "LOWER(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(" + column + ", '_', ''), '-', ''), ' ', ''), '.', ''), '/', ''))"
}

func normalizeSearchWords(value string) string {
	value = strings.NewReplacer("_", " ", "-", " ", ".", " ", "/", " ").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

func compactSearchText(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func likeContains(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + replacer.Replace(value) + "%"
}

func promptPreview(requestFields string) string {
	if requestFields == "" {
		return ""
	}
	var fields map[string]string
	if err := json.Unmarshal([]byte(requestFields), &fields); err != nil {
		return truncateString(requestFields, 240)
	}
	for _, key := range []string{"prompt", "input", "message", "systemPrompt"} {
		if v := strings.TrimSpace(fields[key]); v != "" {
			return truncateString(v, 240)
		}
	}
	return ""
}

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// timeBucketExpr returns a SQL expression for truncating created_at to the granularity.
// Uses SQLite-compatible strftime; compatible with PostgreSQL when using the
// same string but could be swapped for date_trunc on Postgres via a build tag.
func timeBucketExpr(gran domain.Granularity) string {
	switch gran {
	case domain.GranularityDay:
		return `strftime('%Y-%m-%d', created_at)`
	case domain.GranularityMonth:
		return `strftime('%Y-%m', created_at)`
	default: // hour
		return `strftime('%Y-%m-%dT%H:00', created_at)`
	}
}

// ── WebhookDelivery repo ──────────────────────────────────────────────────────

type gormWebhookDeliveryRepo struct{ db *gorm.DB }

func NewWebhookDeliveryRepository(db *gorm.DB) domain.WebhookDeliveryRepository {
	return &gormWebhookDeliveryRepo{db: db}
}

func (r *gormWebhookDeliveryRepo) Create(d *domain.WebhookDelivery) error {
	return r.db.Create(d).Error
}

func (r *gormWebhookDeliveryRepo) ListByRouterID(orgID, routerID string, limit, offset int) ([]domain.WebhookDelivery, int64, error) {
	var total int64
	q := r.db.Model(&domain.WebhookDelivery{}).Where("webhook_deliveries.router_id = ?", routerID)
	if orgID != "" {
		q = q.Joins("JOIN routers ON routers.id = webhook_deliveries.router_id").
			Where("routers.org_id = ?", orgID)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []domain.WebhookDelivery
	return rows, total, q.Order("webhook_deliveries.created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error
}

func (r *gormInferenceLogRepo) SumByPeriod(orgID, routerID, virtualKeyID, teamID string, from time.Time) (int64, float64, error) {
	type result struct {
		Requests int64
		CostUSD  float64
	}
	q := r.db.Model(&domain.InferenceLog{}).
		Select("COUNT(*) AS requests, COALESCE(SUM(cost_usd), 0) AS cost_usd").
		Where("org_id = ? AND created_at >= ?", orgID, from)
	if routerID != "" {
		q = q.Where("router_id = ?", routerID)
	}
	if virtualKeyID != "" {
		q = q.Where("virtual_key_id = ?", virtualKeyID)
	}
	if teamID != "" {
		q = q.Where("team_id = ?", teamID)
	}
	var row result
	if err := q.Scan(&row).Error; err != nil {
		return 0, 0, err
	}
	return row.Requests, row.CostUSD, nil
}

// ── AuditLog repo ─────────────────────────────────────────────────────────────

type gormAuditLogRepo struct{ db *gorm.DB }

func NewAuditLogRepository(db *gorm.DB) domain.AuditLogRepository {
	return &gormAuditLogRepo{db: db}
}

func (r *gormAuditLogRepo) Create(log *domain.AuditLog) error {
	return r.db.Create(log).Error
}

func (r *gormAuditLogRepo) List(orgID string, limit, offset int) ([]domain.AuditLog, int64, error) {
	q := r.db.Model(&domain.AuditLog{})
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return nil, 0, err
	}
	var logs []domain.AuditLog
	return logs, count, q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&logs).Error
}

// ── ProviderHealth repo ───────────────────────────────────────────────────────

type gormProviderHealthRepo struct{ db *gorm.DB }

func NewProviderHealthRepository(db *gorm.DB) domain.ProviderHealthRepository {
	return &gormProviderHealthRepo{db: db}
}

func (r *gormProviderHealthRepo) Upsert(h *domain.ProviderHealth) error {
	return r.db.Save(h).Error
}

func (r *gormProviderHealthRepo) ListAll(orgID string) ([]domain.ProviderHealth, error) {
	var rows []domain.ProviderHealth
	q := r.db.Model(&domain.ProviderHealth{})
	if orgID != "" {
		q = q.Joins("JOIN models ON models.id = provider_health.model_id").
			Where("models.org_id = ?", orgID)
	}
	return rows, q.Find(&rows).Error
}

func (r *gormProviderHealthRepo) FindByModelID(modelID string) (*domain.ProviderHealth, error) {
	var h domain.ProviderHealth
	err := r.db.First(&h, "model_id = ?", modelID).Error
	return &h, err
}

func (r *gormProviderHealthRepo) DeleteByModelID(modelID string) error {
	return r.db.Where("model_id = ?", modelID).Delete(&domain.ProviderHealth{}).Error
}

// ── InferencePayload repo ─────────────────────────────────────────────────────

type gormInferencePayloadRepo struct{ db *gorm.DB }

func NewInferencePayloadRepository(db *gorm.DB) domain.InferencePayloadRepository {
	return &gormInferencePayloadRepo{db: db}
}

func (r *gormInferencePayloadRepo) Create(p *domain.InferencePayload) error {
	return r.db.Create(p).Error
}

func (r *gormInferencePayloadRepo) FindByLogID(orgID, logID string) (*domain.InferencePayload, error) {
	var p domain.InferencePayload
	q := r.db.Model(&domain.InferencePayload{}).Where("inference_payloads.log_id = ?", logID)
	if orgID != "" {
		q = q.Joins("JOIN inference_logs ON inference_logs.id = inference_payloads.log_id").
			Where("inference_logs.org_id = ?", orgID)
	}
	if err := q.First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func (r *gormInferencePayloadRepo) DeleteOlderThan(cutoff time.Time) error {
	return r.db.Where("created_at < ?", cutoff).Delete(&domain.InferencePayload{}).Error
}

// ── Agent session events / archives / compression telemetry ─────────────────

type gormAgentSessionEventRepo struct{ db *gorm.DB }

func NewAgentSessionEventRepository(db *gorm.DB) domain.AgentSessionEventRepository {
	return &gormAgentSessionEventRepo{db: db}
}

func (r *gormAgentSessionEventRepo) Create(e *domain.AgentSessionEvent) error {
	return r.db.Create(e).Error
}

func (r *gormAgentSessionEventRepo) List(filter domain.InferenceLogFilter, limit, offset int) ([]domain.AgentSessionEvent, int64, error) {
	q := r.db.Model(&domain.AgentSessionEvent{})
	if filter.OrgID != "" {
		q = q.Where("org_id = ?", filter.OrgID)
	}
	if filter.RouterID != "" {
		q = q.Where("router_id = ?", filter.RouterID)
	}
	if filter.VirtualKeyID != "" {
		q = q.Where("virtual_key_id = ?", filter.VirtualKeyID)
	}
	if filter.UserID != "" {
		q = q.Where("user_id = ?", filter.UserID)
	}
	if filter.AgentSessionID != "" {
		q = q.Where("agent_session_id = ?", filter.AgentSessionID)
	}
	if filter.Agent != "" {
		q = applyLooseTextFilter(q, "agent", filter.Agent)
	}
	if filter.From != nil {
		q = q.Where("created_at >= ?", *filter.From)
	}
	if filter.To != nil {
		q = q.Where("created_at <= ?", *filter.To)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []domain.AgentSessionEvent
	return rows, total, q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error
}

type gormToolCallArchiveRepo struct{ db *gorm.DB }

func NewToolCallArchiveRepository(db *gorm.DB) domain.ToolCallArchiveRepository {
	return &gormToolCallArchiveRepo{db: db}
}

func (r *gormToolCallArchiveRepo) Create(a *domain.ToolCallArchive) error {
	return r.db.Create(a).Error
}

func (r *gormToolCallArchiveRepo) FindByID(orgID, id string) (*domain.ToolCallArchive, error) {
	var row domain.ToolCallArchive
	q := r.db.Where("id = ?", id)
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}
	if err := q.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func (r *gormToolCallArchiveRepo) List(filter domain.InferenceLogFilter, limit, offset int) ([]domain.ToolCallArchive, int64, error) {
	q := r.db.Model(&domain.ToolCallArchive{})
	q = applyArchiveFilter(q, filter)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []domain.ToolCallArchive
	return rows, total, q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error
}

type gormCompressionEventRepo struct{ db *gorm.DB }

func NewCompressionEventRepository(db *gorm.DB) domain.CompressionEventRepository {
	return &gormCompressionEventRepo{db: db}
}

func (r *gormCompressionEventRepo) Create(e *domain.CompressionEvent) error {
	return r.db.Create(e).Error
}

func (r *gormCompressionEventRepo) List(filter domain.InferenceLogFilter, limit, offset int) ([]domain.CompressionEvent, int64, error) {
	q := r.db.Model(&domain.CompressionEvent{})
	q = applyArchiveFilter(q, filter)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []domain.CompressionEvent
	return rows, total, q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error
}

func applyArchiveFilter(q *gorm.DB, filter domain.InferenceLogFilter) *gorm.DB {
	if filter.OrgID != "" {
		q = q.Where("org_id = ?", filter.OrgID)
	}
	if filter.RouterID != "" {
		q = q.Where("router_id = ?", filter.RouterID)
	}
	if filter.AgentSessionID != "" {
		q = q.Where("agent_session_id = ?", filter.AgentSessionID)
	}
	if filter.From != nil {
		q = q.Where("created_at >= ?", *filter.From)
	}
	if filter.To != nil {
		q = q.Where("created_at <= ?", *filter.To)
	}
	return q
}
