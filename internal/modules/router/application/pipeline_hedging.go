package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"hyperstrate/server/internal/modules/router/domain"
)

// runHedged fires all specified target models in parallel and returns the first
// response that passes the quality check. Slower responses are cancelled.
func (p *featurePipeline) runHedged(
	ctx context.Context,
	routerID string,
	enabled []domain.RouterTarget,
	fields map[string]string,
	options map[string]any,
	inferencer ModelInferencer,
	cfg map[string]any,
) (*RouteInferResult, error) {
	hedgeTargets := resolveHedgeTargets(enabled, cfg)
	if len(hedgeTargets) == 0 {
		// Fall back to first two enabled targets
		for _, t := range enabled {
			hedgeTargets = append(hedgeTargets, t)
			if len(hedgeTargets) >= 2 {
				break
			}
		}
	}
	if len(hedgeTargets) == 0 {
		return nil, domain.ErrNoTargetsAvailable
	}

	qualityCheck := "any"
	if q, ok := cfg["quality_check"].(string); ok && q != "" {
		qualityCheck = q
	}
	minLength := 50
	if v := toFloat(cfg["min_length"]); v > 0 {
		minLength = int(v)
	}
	timeoutMs := 10000
	if v := toFloat(cfg["timeout_ms"]); v > 0 {
		timeoutMs = int(v)
	}

	raceCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	type hedgeResult struct {
		r        *ModelInferResult
		modelID  string
		targetID string
		err      error
	}
	ch := make(chan hedgeResult, len(hedgeTargets))

	for i := range hedgeTargets {
		target := hedgeTargets[i]
		// Check circuit breaker before launching goroutine for this model.
		if !p.circuits.get(target.ID).Allow() {
			ch <- hedgeResult{modelID: target.ModelID, targetID: target.ID, err: fmt.Errorf("circuit open: %s", target.ModelID)}
			continue
		}
		go func(t domain.RouterTarget) {
			attemptFields := make(map[string]string, len(fields))
			for k, v := range fields {
				attemptFields[k] = v
			}
			p.injectTargetSystemPrompt(raceCtx, &t, attemptFields)
			r, err := inferencer.InferModel(raceCtx, t.ModelID, attemptFields, options)
			ch <- hedgeResult{r: r, modelID: t.ModelID, targetID: t.ID, err: err}
		}(target)
	}

	received := 0
	var lastErr error
	for received < len(hedgeTargets) {
		select {
		case res := <-ch:
			received++
			if res.err != nil {
				lastErr = res.err
				continue
			}
			if passesHedgeQualityCheck(res.r.Content, qualityCheck, minLength) {
				cancel()
				return &RouteInferResult{
					Content:                 res.r.Content,
					SelectedModelID:         res.modelID,
					SelectedTargetID:        res.targetID,
					ModelDefKey:             res.r.ModelDefKey,
					Provider:                res.r.Provider,
					InputTokens:             res.r.InputTokens,
					OutputTokens:            res.r.OutputTokens,
					CachedInputTokens:       res.r.CachedInputTokens,
					CacheWriteInputTokens:   res.r.CacheWriteInputTokens,
					CacheWrite1hInputTokens: res.r.CacheWrite1hInputTokens,
					CostUSD:                 res.r.CostUSD,
					ToolCalls:               res.r.ToolCalls,
				}, nil
			}
		case <-raceCtx.Done():
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, raceCtx.Err()
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, domain.ErrAllTargetsFailed
}

func resolveHedgeTargets(enabled []domain.RouterTarget, cfg map[string]any) []domain.RouterTarget {
	ids := extractStringSlice(cfg["target_ids"])
	if len(ids) == 0 {
		ids = extractStringSlice(cfg["targets"])
	}
	if len(ids) == 0 {
		return nil
	}
	out := make([]domain.RouterTarget, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		for _, t := range enabled {
			if t.ID != id && t.ModelID != id {
				continue
			}
			if _, ok := seen[t.ID]; ok {
				break
			}
			seen[t.ID] = struct{}{}
			out = append(out, t)
			break
		}
	}
	return out
}

func passesHedgeQualityCheck(content, check string, minLength int) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		return false
	}
	switch check {
	case "min_length":
		return len(content) >= minLength
	case "valid_json":
		return json.Valid([]byte(content))
	case "no_refusal":
		lower := strings.ToLower(content)
		for _, phrase := range hedgeRefusalPhrases {
			if strings.Contains(lower, phrase) {
				return false
			}
		}
		return true
	default: // "any"
		return true
	}
}

var hedgeRefusalPhrases = []string{
	"i cannot", "i can't", "i'm unable", "i am unable",
	"i'm sorry, but i", "i apologize, but i",
	"as an ai, i cannot", "as a language model",
}

// extractStringSlice safely converts []any → []string, skipping empty entries.
func extractStringSlice(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
