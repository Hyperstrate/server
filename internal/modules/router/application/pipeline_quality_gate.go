package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"hyperstrate/server/internal/modules/router/domain"
)

var judgeScoreRe = regexp.MustCompile(`\b(10|[1-9])\b`)

// runQualityGate calls a judge model to score the inference result.
// Returns (result, gateActivated bool, err).
func (p *featurePipeline) runQualityGate(
	ctx context.Context,
	result *RouteInferResult,
	fields map[string]string,
	options map[string]any,
	cfg map[string]any,
	inferencer ModelInferencer,
) (*RouteInferResult, bool, error) {
	judgeModelID, _ := cfg["judge_model_id"].(string)
	rubricPrompt, _ := cfg["rubric_prompt"].(string)
	if judgeModelID == "" || rubricPrompt == "" {
		return result, false, nil
	}

	minScore := 7.0
	if v := toFloat(cfg["min_score"]); v > 0 {
		minScore = v
	}
	action, _ := cfg["action"].(string)
	if action == "" {
		action = "flag"
	}
	retryTargetID, _ := cfg["retry_target_id"].(string)

	judgePrompt := fmt.Sprintf(
		"User request:\n%s\n\nModel response:\n%s\n\n%s\n\nReply with only a number from 1 to 10.",
		fields["prompt"], result.Content, rubricPrompt,
	)

	judgeResult, err := inferencer.InferModel(ctx, judgeModelID, map[string]string{
		"prompt": judgePrompt,
	}, nil)
	if err != nil {
		// Judge call failed — pass through without gating
		return result, false, nil
	}

	score := parseJudgeScore(judgeResult.Content)
	if score >= minScore {
		return result, false, nil
	}

	// Below threshold
	switch action {
	case "retry":
		targetID := retryTargetID
		if targetID == "" {
			targetID = result.SelectedModelID
		}
		retried, retryErr := inferencer.InferModel(ctx, targetID, fields, options)
		if retryErr != nil {
			return result, true, nil // retry failed, return original
		}
		return &RouteInferResult{
			Content:                 retried.Content,
			SelectedModelID:         targetID,
			ModelDefKey:             retried.ModelDefKey,
			Provider:                retried.Provider,
			InputTokens:             result.InputTokens + retried.InputTokens,
			OutputTokens:            result.OutputTokens + retried.OutputTokens,
			CachedInputTokens:       result.CachedInputTokens + retried.CachedInputTokens,
			CacheWriteInputTokens:   result.CacheWriteInputTokens + retried.CacheWriteInputTokens,
			CacheWrite1hInputTokens: result.CacheWrite1hInputTokens + retried.CacheWrite1hInputTokens,
			CostUSD:                 result.CostUSD + retried.CostUSD,
			ABVariant:               result.ABVariant,
			ToolCalls:               retried.ToolCalls,
		}, true, nil
	case "error":
		return nil, true, fmt.Errorf("%w (score %.0f/10 < %.0f)", domain.ErrLowQuality, score, minScore)
	default: // "flag"
		return result, true, nil
	}
}

func parseJudgeScore(content string) float64 {
	m := judgeScoreRe.FindString(strings.TrimSpace(content))
	if m == "" {
		return 0
	}
	var score float64
	fmt.Sscanf(m, "%f", &score)
	return score
}
