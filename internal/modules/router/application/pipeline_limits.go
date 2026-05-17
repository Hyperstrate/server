package application

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"hyperstrate/server/internal/modules/router/domain"
)

func budgetPeriodStart(period string) time.Time {
	now := time.Now().UTC()
	if period == "daily" {
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	}
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func (p *featurePipeline) checkBudget(ctx context.Context, orgID, routerID string, cfg map[string]any, options map[string]any) error {
	maxReqs := int64(0)
	if v, ok := cfg["max_requests"]; ok {
		if n := toFloat(v); n > 0 {
			maxReqs = int64(n)
		}
	}
	maxCost := float64(0)
	if v, ok := cfg["max_cost_usd"]; ok {
		if n := toFloat(v); n > 0 {
			maxCost = n
		}
	}
	if maxReqs > 0 || maxCost > 0 {
		if p.budgetQuerier != nil {
			period, _ := cfg["period"].(string)
			from := budgetPeriodStart(period)
			requests, costUSD, err := p.budgetQuerier.SumCostByPeriod(orgID, routerID, "", "", from)
			if err == nil {
				if maxReqs > 0 && requests >= maxReqs {
					return domain.ErrBudgetExceeded
				}
				if maxCost > 0 && costUSD >= maxCost {
					return domain.ErrBudgetExceeded
				}
			}
		}
	}
	return p.checkScopedBudgets(routerID, cfg, options)
}

func (p *featurePipeline) recordBudget(routerID string, costUSD float64, cfg map[string]any, options map[string]any) {
	p.recordScopedBudget(routerID, costUSD, cfg, options)
}

type BudgetStatus struct {
	PeriodKey        string  `json:"periodKey"        validate:"required"`
	Requests         int64   `json:"requests"         validate:"required"`
	EstimatedCostUSD float64 `json:"estimatedCostUsd" validate:"required"`
}

func (p *featurePipeline) budgetStatus(ctx context.Context, orgID, routerID string, cfg map[string]any) *BudgetStatus {
	period, _ := cfg["period"].(string)
	from := budgetPeriodStart(period)
	if p.budgetQuerier == nil {
		return &BudgetStatus{PeriodKey: from.Format("2006-01-02")}
	}
	requests, costUSD, _ := p.budgetQuerier.SumCostByPeriod(orgID, routerID, "", "", from)
	return &BudgetStatus{PeriodKey: from.Format("2006-01-02"), Requests: requests, EstimatedCostUSD: costUSD}
}

func (p *featurePipeline) checkScopedBudgets(routerID string, cfg map[string]any, options map[string]any) error {
	for _, budget := range scopedBudgetConfigs(cfg, options) {
		counter := p.scopedBudgetCounter(routerID, budget)
		counter.mu.Lock()
		exceeded := budget.exceeded(counter)
		counter.mu.Unlock()
		if exceeded {
			return domain.ErrBudgetExceeded
		}
	}
	return nil
}

func (p *featurePipeline) recordScopedBudget(routerID string, costUSD float64, cfg map[string]any, options map[string]any) {
	for _, budget := range scopedBudgetConfigs(cfg, options) {
		counter := p.scopedBudgetCounter(routerID, budget)
		counter.mu.Lock()
		counter.requests++
		counter.estimatedCostUSD += costUSD
		counter.mu.Unlock()
	}
}

type scopedBudgetConfig struct {
	Kind       string
	Value      string
	Period     string
	MaxReqs    int64
	MaxCostUSD float64
}

func (b scopedBudgetConfig) key(routerID string) string {
	return fmt.Sprintf("%s:%s:%s:%s:%s", routerID, b.Kind, b.Value, b.Period, budgetPeriodStart(b.Period).Format("2006-01-02"))
}

func (b scopedBudgetConfig) exceeded(c *teamBudgetCounter) bool {
	if b.MaxReqs > 0 && c.requests >= b.MaxReqs {
		return true
	}
	return b.MaxCostUSD > 0 && c.estimatedCostUSD >= b.MaxCostUSD
}

func (p *featurePipeline) scopedBudgetCounter(routerID string, budget scopedBudgetConfig) *teamBudgetCounter {
	periodKey := budgetPeriodStart(budget.Period).Format("2006-01-02")
	actual, _ := p.scopedBudgets.LoadOrStore(budget.key(routerID), &teamBudgetCounter{periodKey: periodKey})
	counter := actual.(*teamBudgetCounter)
	if counter.periodKey != periodKey {
		counter.mu.Lock()
		counter.periodKey, counter.estimatedCostUSD, counter.requests = periodKey, 0, 0
		counter.mu.Unlock()
	}
	return counter
}

func scopedBudgetConfigs(cfg map[string]any, options map[string]any) []scopedBudgetConfig {
	if options == nil {
		return nil
	}
	var out []scopedBudgetConfig
	out = appendScopedBudget(out, cfg, "agent_budgets", "agent", stringOption(options, "agent"))
	out = appendScopedBudget(out, cfg, "role_budgets", "agent_role", stringOption(options, "agent_role", "role"))
	out = appendScopedBudget(out, cfg, "repo_budgets", "repo", stringOption(options, "repo", "repository", "agent_repo"))
	out = appendScopedBudget(out, cfg, "branch_budgets", "branch", stringOption(options, "branch", "git_branch", "agent_branch"))
	return out
}

func appendScopedBudget(out []scopedBudgetConfig, cfg map[string]any, mapKey, kind, value string) []scopedBudgetConfig {
	if value == "" {
		return out
	}
	raw, _ := cfg[mapKey].(map[string]any)
	item, _ := raw[value].(map[string]any)
	if item == nil {
		item, _ = raw["*"].(map[string]any)
	}
	if item == nil {
		return out
	}
	maxReqs := int64(toFloat(item["max_requests"]))
	maxCost := toFloat(item["max_cost_usd"])
	if maxReqs <= 0 && maxCost <= 0 {
		return out
	}
	period, _ := item["period"].(string)
	if period == "" {
		period, _ = cfg["period"].(string)
	}
	return append(out, scopedBudgetConfig{Kind: kind, Value: value, Period: period, MaxReqs: maxReqs, MaxCostUSD: maxCost})
}

func stringOption(options map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, _ := options[key].(string); value != "" {
			return value
		}
	}
	return ""
}

type tokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	maxTokens  float64
	rps        float64
	lastRefill time.Time
}

func (p *featurePipeline) checkRateLimit(routerID string, cfg map[string]any) error {
	rps := 10.0
	if v, ok := cfg["rps"]; ok {
		if n := toFloat(v); n > 0 {
			rps = n
		}
	}
	burst := rps
	if v, ok := cfg["burst"]; ok {
		if n := toFloat(v); n > 0 {
			burst = n
		}
	}
	actual, _ := p.limiters.LoadOrStore(routerID, &tokenBucket{
		tokens: burst, maxTokens: burst, rps: rps, lastRefill: time.Now(),
	})
	bucket := actual.(*tokenBucket)
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	if bucket.rps != rps || bucket.maxTokens != burst {
		bucket.rps = rps
		bucket.maxTokens = burst
		if bucket.tokens > burst {
			bucket.tokens = burst
		}
	}
	now := time.Now()
	elapsed := now.Sub(bucket.lastRefill).Seconds()
	bucket.tokens = math.Min(bucket.maxTokens, bucket.tokens+elapsed*bucket.rps)
	bucket.lastRefill = now
	if bucket.tokens < 1 {
		return domain.ErrRateLimitExceeded
	}
	bucket.tokens--
	return nil
}
