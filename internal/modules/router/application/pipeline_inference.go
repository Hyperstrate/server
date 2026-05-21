package application

import (
	"context"
	"fmt"
	"time"

	"hyperstrate/server/internal/modules/router/domain"
)

func (p *featurePipeline) inferWithFallback(
	ctx context.Context,
	targets []domain.RouterTarget,
	fields map[string]string,
	options map[string]any,
	inferencer ModelInferencer,
	addStep func(PipelineStep, time.Duration),
) (*RouteInferResult, error) {
	healthy := p.circuits.filterHealthy(targets)
	for i := range healthy {
		cb := p.circuits.get(healthy[i].ID)
		if !cb.Allow() {
			addStep(PipelineStep{Phase: 6, Kind: "inference", Name: "Model Inference", Outcome: "skipped",
				Detail: fmt.Sprintf("circuit open: %s", healthy[i].ModelID)}, 0)
			continue
		}
		attemptFields := make(map[string]string, len(fields))
		for k, v := range fields {
			attemptFields[k] = v
		}
		p.injectTargetSystemPrompt(ctx, &healthy[i], attemptFields)
		t0 := time.Now()
		r, err := inferencer.InferModel(ctx, healthy[i].ModelID, attemptFields, options)
		if err != nil {
			cb.RecordFailure()
			addStep(PipelineStep{Phase: 6, Kind: "inference", Name: "Model Inference", Outcome: "error",
				Detail: fmt.Sprintf("%s: %v", healthy[i].ModelID, err)}, time.Since(t0))
			continue
		}
		cb.RecordSuccess()
		p.updateLatency(healthy[i].ID, float64(time.Since(t0).Milliseconds()))
		addStep(PipelineStep{Phase: 6, Kind: "inference", Name: "Model Inference", Outcome: "success",
			Detail: fmt.Sprintf("fallback winner: %s", healthy[i].ModelID)}, time.Since(t0))
		return &RouteInferResult{
			Content:                 r.Content,
			SelectedModelID:         healthy[i].ModelID,
			SelectedTargetID:        healthy[i].ID,
			ModelDefKey:             r.ModelDefKey,
			Provider:                r.Provider,
			InputTokens:             r.InputTokens,
			OutputTokens:            r.OutputTokens,
			CachedInputTokens:       r.CachedInputTokens,
			CacheWriteInputTokens:   r.CacheWriteInputTokens,
			CacheWrite1hInputTokens: r.CacheWrite1hInputTokens,
			CostUSD:                 r.CostUSD,
			ToolCalls:               r.ToolCalls,
		}, nil
	}
	return nil, domain.ErrAllTargetsFailed
}

type retryConfig struct {
	MaxRetries     int
	InitialDelayMs int
	BackoffMult    float64
}

func parseRetryConfig(cfg map[string]any) *retryConfig {
	rc := &retryConfig{MaxRetries: 3, InitialDelayMs: 100, BackoffMult: 2.0}
	if v, ok := cfg["max_retries"]; ok {
		if n := toFloat(v); n > 0 {
			rc.MaxRetries = int(n)
		}
	}
	if v, ok := cfg["initial_delay_ms"]; ok {
		if n := toFloat(v); n > 0 {
			rc.InitialDelayMs = int(n)
		}
	}
	if v, ok := cfg["backoff_multiplier"]; ok {
		if n := toFloat(v); n > 0 {
			rc.BackoffMult = n
		}
	}
	return rc
}

func withRetry(ctx context.Context, cfg *retryConfig, fn func() (*ModelInferResult, error)) (*ModelInferResult, error) {
	return withRetryTracked(ctx, cfg, fn, nil)
}

func withRetryTracked(ctx context.Context, cfg *retryConfig, fn func() (*ModelInferResult, error), onRetry func(attempt int, err error)) (*ModelInferResult, error) {
	delay := time.Duration(cfg.InitialDelayMs) * time.Millisecond
	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		result, err := fn()
		if err == nil {
			return result, nil
		}
		lastErr = err
		if attempt < cfg.MaxRetries {
			if onRetry != nil {
				onRetry(attempt+1, err)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
			delay = time.Duration(float64(delay) * cfg.BackoffMult)
		}
	}
	return nil, fmt.Errorf("all %d attempts failed: %w", cfg.MaxRetries+1, lastErr)
}
