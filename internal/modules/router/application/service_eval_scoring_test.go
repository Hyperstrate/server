package application

import (
	"context"
	"strings"
	"testing"

	"hyperstrate/server/internal/modules/router/domain"
)

type evalJudgeInferencer struct {
	prompt string
}

func (e *evalJudgeInferencer) InferModel(_ context.Context, _ string, fields map[string]string, _ map[string]any) (*ModelInferResult, error) {
	e.prompt = fields["prompt"]
	return &ModelInferResult{Content: "7"}, nil
}

func (e *evalJudgeInferencer) InferModelStream(_ context.Context, _ string, _ map[string]string, _ map[string]any) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk)
	close(ch)
	return ch, nil
}

func TestScoreResultLLMAddsStrictScoringRules(t *testing.T) {
	judge := &evalJudgeInferencer{}
	svc := &service{inferencer: judge}
	c := domain.RouterEvaluationCase{
		Expected:    `The answer must be exactly "Paris".`,
		ScoreMethod: domain.ScoreMethodLLM,
	}
	result := &RouteInferResult{Content: "Paris is the capital of France."}

	score := scoreResult(c, result, "mdl_judge", svc, context.Background())

	if score != 0.7 {
		t.Fatalf("expected normalized judge score 0.7, got %v", score)
	}
	for _, want := range []string{
		"SCORING RULES:",
		"10 = perfect",
		"7-9 = mostly correct",
		"4-6 = partially correct",
		"1-3 = mostly incorrect",
		"0 = completely incorrect",
		"Return only one number",
	} {
		if !strings.Contains(judge.prompt, want) {
			t.Fatalf("judge prompt missing %q:\n%s", want, judge.prompt)
		}
	}
}
