package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	authDomain "hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/modules/router/domain"
	"hyperstrate/server/internal/shared/dbtype"
	"hyperstrate/server/internal/shared/pagination"

	"go.jetify.com/typeid/v2"
)

// ── DTOs ──────────────────────────────────────────────────────────────────────

type CreateEvaluationInput struct {
	RouterID    string `json:"routerId"    binding:"required"`
	Name        string `json:"name"        binding:"required,max=255"`
	Description string `json:"description"`
}

type UpdateEvaluationInput struct {
	Name        *string `json:"name"        binding:"omitempty,max=255"`
	Description *string `json:"description"`
}

type EvaluationCaseInput struct {
	Fields      map[string]string `json:"fields"      binding:"required"`
	Expected    string            `json:"expected"    binding:"required"`
	ScoreMethod string            `json:"scoreMethod" binding:"omitempty,oneof=exact contains llm agent_task"`
	Description string            `json:"description"`
}

type EvaluationResponse struct {
	ID          string    `json:"id"`
	RouterID    string    `json:"routerId"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CaseCount   int       `json:"caseCount"`
	CreatedAt   time.Time `json:"createdAt"`
	ModifiedAt  time.Time `json:"modifiedAt"`
}

type EvaluationCaseResponse struct {
	ID          string            `json:"id"`
	EvalID      string            `json:"evalId"`
	Fields      map[string]string `json:"fields"`
	Expected    string            `json:"expected"`
	ScoreMethod string            `json:"scoreMethod"`
	Description string            `json:"description"`
	CreatedAt   time.Time         `json:"createdAt"`
}

// EvalCaseResult is one scored case within a run.
type EvalCaseResult struct {
	CaseID   string  `json:"caseId"`
	Actual   string  `json:"actual"`
	Score    float64 `json:"score"` // 0.0–1.0
	Passed   bool    `json:"passed"`
	ErrorMsg string  `json:"errorMsg,omitempty"`
}

type EvaluationRunResponse struct {
	ID          string           `json:"id"`
	EvalID      string           `json:"evalId"`
	RouterID    string           `json:"routerId"`
	TotalCases  int              `json:"totalCases"`
	PassedCases int              `json:"passedCases"`
	AvgScore    float64          `json:"avgScore"`
	Results     []EvalCaseResult `json:"results"`
	CreatedAt   time.Time        `json:"createdAt"`
}

// ── Service extension ─────────────────────────────────────────────────────────

func (s *service) CreateEvaluation(ctx context.Context, input CreateEvaluationInput) (*EvaluationResponse, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	// Verify router exists and belongs to org.
	if _, err := s.routerRepo.FindByID(ctx, orgID, input.RouterID); err != nil {
		return nil, err
	}
	e := &domain.RouterEvaluation{
		ID:          typeid.MustGenerate("eval").String(),
		OrgID:       orgID,
		RouterID:    input.RouterID,
		Name:        input.Name,
		Description: input.Description,
	}
	if err := s.evalRepo.Create(ctx, e); err != nil {
		return nil, err
	}
	resp := toEvalResponse(e, 0)
	return &resp, nil
}

func (s *service) ListEvaluations(ctx context.Context, routerID string, slice pagination.Slice) (pagination.Paginated[EvaluationResponse], error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	rows, total, err := s.evalRepo.List(ctx, orgID, routerID, slice.Offset(), slice.PerPage)
	if err != nil {
		return pagination.Paginated[EvaluationResponse]{}, err
	}
	out := make([]EvaluationResponse, len(rows))
	for i, e := range rows {
		cases, _ := s.evalCaseRepo.ListByEvalID(ctx, e.ID)
		out[i] = toEvalResponse(&e, len(cases))
	}
	return pagination.New(out, total, slice), nil
}

func (s *service) GetEvaluation(ctx context.Context, id string) (*EvaluationResponse, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	e, err := s.evalRepo.FindByID(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	cases, _ := s.evalCaseRepo.ListByEvalID(ctx, e.ID)
	resp := toEvalResponse(e, len(cases))
	return &resp, nil
}

func (s *service) UpdateEvaluation(ctx context.Context, id string, input UpdateEvaluationInput) (*EvaluationResponse, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	e, err := s.evalRepo.FindByID(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		e.Name = *input.Name
	}
	if input.Description != nil {
		e.Description = *input.Description
	}
	if err := s.evalRepo.Update(ctx, e); err != nil {
		return nil, err
	}
	cases, _ := s.evalCaseRepo.ListByEvalID(ctx, e.ID)
	resp := toEvalResponse(e, len(cases))
	return &resp, nil
}

func (s *service) DeleteEvaluation(ctx context.Context, id string) error {
	orgID := authDomain.OrgIDFromContext(ctx)
	if err := s.evalCaseRepo.DeleteByEvalID(ctx, id); err != nil {
		return err
	}
	return s.evalRepo.Delete(ctx, orgID, id)
}

func (s *service) ListEvaluationCases(ctx context.Context, evalID string) ([]EvaluationCaseResponse, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	if _, err := s.evalRepo.FindByID(ctx, orgID, evalID); err != nil {
		return nil, err
	}
	rows, err := s.evalCaseRepo.ListByEvalID(ctx, evalID)
	if err != nil {
		return nil, err
	}
	out := make([]EvaluationCaseResponse, len(rows))
	for i, c := range rows {
		out[i] = toEvalCaseResponse(&c)
	}
	return out, nil
}

func (s *service) AddEvaluationCase(ctx context.Context, evalID string, input EvaluationCaseInput) (*EvaluationCaseResponse, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	if _, err := s.evalRepo.FindByID(ctx, orgID, evalID); err != nil {
		return nil, err
	}
	method := domain.ScoreMethod(input.ScoreMethod)
	if method == "" {
		method = domain.ScoreMethodContains
	}
	c := &domain.RouterEvaluationCase{
		ID:          typeid.MustGenerate("ecase").String(),
		EvalID:      evalID,
		Fields:      dbtype.JSONStringMap(input.Fields),
		Expected:    input.Expected,
		ScoreMethod: method,
		Description: input.Description,
	}
	if err := s.evalCaseRepo.Create(ctx, c); err != nil {
		return nil, err
	}
	resp := toEvalCaseResponse(c)
	return &resp, nil
}

func (s *service) DeleteEvaluationCase(ctx context.Context, evalID, caseID string) error {
	orgID := authDomain.OrgIDFromContext(ctx)
	if _, err := s.evalRepo.FindByID(ctx, orgID, evalID); err != nil {
		return err
	}
	return s.evalCaseRepo.Delete(ctx, caseID)
}

// RunEvaluation executes all cases and returns a scored run.
// judgeModelID is used only for ScoreMethodLLM cases; pass "" to skip LLM scoring.
func (s *service) RunEvaluation(ctx context.Context, evalID, judgeModelID string) (*EvaluationRunResponse, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	eval, err := s.evalRepo.FindByID(ctx, orgID, evalID)
	if err != nil {
		return nil, err
	}
	cases, err := s.evalCaseRepo.ListByEvalID(ctx, evalID)
	if err != nil {
		return nil, err
	}

	results := make([]EvalCaseResult, 0, len(cases))
	var totalScore float64
	var passed int

	for _, c := range cases {
		result, inferErr := s.RouteInfer(ctx, eval.RouterID, RouteInferInput{
			Fields: map[string]string(c.Fields),
		})
		cr := EvalCaseResult{CaseID: c.ID}
		if inferErr != nil {
			cr.ErrorMsg = inferErr.Error()
		} else {
			cr.Actual = result.Content
			cr.Score = scoreResult(c, result, judgeModelID, s, ctx)
			cr.Passed = cr.Score >= 0.5
		}
		if cr.Passed {
			passed++
		}
		totalScore += cr.Score
		results = append(results, cr)
	}

	avgScore := 0.0
	if len(cases) > 0 {
		avgScore = totalScore / float64(len(cases))
	}

	runResults := make(dbtype.JSONMap, len(results))
	for i, r := range results {
		runResults[fmt.Sprintf("%d", i)] = r
	}

	run := &domain.RouterEvaluationRun{
		ID:          typeid.MustGenerate("erun").String(),
		EvalID:      evalID,
		RouterID:    eval.RouterID,
		TotalCases:  len(cases),
		PassedCases: passed,
		AvgScore:    avgScore,
		Results:     runResults,
	}
	if err := s.evalRunRepo.Create(ctx, run); err != nil {
		return nil, err
	}
	return toRunResponse(run, results), nil
}

func (s *service) ListEvaluationRuns(ctx context.Context, evalID string, slice pagination.Slice) (pagination.Paginated[EvaluationRunResponse], error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	if _, err := s.evalRepo.FindByID(ctx, orgID, evalID); err != nil {
		return pagination.Paginated[EvaluationRunResponse]{}, err
	}
	rows, total, err := s.evalRunRepo.ListByEvalID(ctx, evalID, slice.Offset(), slice.PerPage)
	if err != nil {
		return pagination.Paginated[EvaluationRunResponse]{}, err
	}
	out := make([]EvaluationRunResponse, len(rows))
	for i, r := range rows {
		out[i] = *toRunResponse(&r, nil)
	}
	return pagination.New(out, total, slice), nil
}

// ── Scoring ───────────────────────────────────────────────────────────────────

func scoreResult(c domain.RouterEvaluationCase, result *RouteInferResult, judgeModelID string, s *service, ctx context.Context) float64 {
	actual := result.Content
	switch c.ScoreMethod {
	case domain.ScoreMethodExact:
		if strings.EqualFold(strings.TrimSpace(actual), strings.TrimSpace(c.Expected)) {
			return 1.0
		}
		return 0.0

	case domain.ScoreMethodContains:
		if strings.Contains(strings.ToLower(actual), strings.ToLower(c.Expected)) {
			return 1.0
		}
		return 0.0

	case domain.ScoreMethodLLM:
		if judgeModelID == "" || s.inferencer == nil {
			return 0.0
		}
		prompt := fmt.Sprintf(
			"Rate whether the following ACTUAL response satisfies the EXPECTED criteria.\n"+
				"EXPECTED: %s\n\nACTUAL: %s\n\n"+
				"SCORING RULES:\n"+
				"10 = perfect: fully satisfies every expected requirement with no meaningful issue.\n"+
				"7-9 = mostly correct: satisfies the main requirement but has minor omissions, extra text, or small ambiguity.\n"+
				"4-6 = partially correct: contains some relevant/correct content but misses important requirements.\n"+
				"1-3 = mostly incorrect: barely related, mostly wrong, or violates major requirements.\n"+
				"0 = completely incorrect: wrong, empty, refusal, unsafe when safety was required, or unrelated.\n\n"+
				"Return only one number from 0 to 10. Do not explain.",
			c.Expected, actual,
		)
		res, err := s.inferencer.InferModel(ctx, judgeModelID, map[string]string{"prompt": prompt}, nil)
		if err != nil {
			return 0.0
		}
		return parseJudgeScore(res.Content) / 10.0
	case domain.ScoreMethodAgentTask:
		return scoreAgentTask(c.Expected, result)
	}
	return 0.0
}

type agentTaskRubric struct {
	Contains        []string `json:"contains"`
	NotContains     []string `json:"notContains"`
	MinToolCalls    int      `json:"minToolCalls"`
	MaxCostUSD      float64  `json:"maxCostUsd"`
	MaxLatencySteps int      `json:"maxLatencySteps"`
	RequireNoErrors bool     `json:"requireNoErrors"`
}

func scoreAgentTask(expected string, result *RouteInferResult) float64 {
	var rubric agentTaskRubric
	if err := json.Unmarshal([]byte(expected), &rubric); err != nil {
		if strings.Contains(strings.ToLower(result.Content), strings.ToLower(expected)) {
			return 1
		}
		return 0
	}
	total, passed := 0, 0
	check := func(ok bool) {
		total++
		if ok {
			passed++
		}
	}
	lower := strings.ToLower(result.Content)
	for _, needle := range rubric.Contains {
		check(strings.Contains(lower, strings.ToLower(needle)))
	}
	for _, needle := range rubric.NotContains {
		check(!strings.Contains(lower, strings.ToLower(needle)))
	}
	if rubric.MinToolCalls > 0 {
		count := 0
		for _, step := range result.Steps {
			if step.Kind == "mcp_tool_call" {
				count++
			}
		}
		check(count >= rubric.MinToolCalls)
	}
	if rubric.MaxCostUSD > 0 {
		check(result.CostUSD <= rubric.MaxCostUSD)
	}
	if rubric.MaxLatencySteps > 0 {
		check(len(result.Steps) <= rubric.MaxLatencySteps)
	}
	if rubric.RequireNoErrors {
		ok := true
		for _, step := range result.Steps {
			if step.Outcome == "error" || step.Outcome == "blocked" {
				ok = false
				break
			}
		}
		check(ok)
	}
	if total == 0 {
		return 0
	}
	return float64(passed) / float64(total)
}

// ── Converters ────────────────────────────────────────────────────────────────

func toEvalResponse(e *domain.RouterEvaluation, caseCount int) EvaluationResponse {
	return EvaluationResponse{
		ID:          e.ID,
		RouterID:    e.RouterID,
		Name:        e.Name,
		Description: e.Description,
		CaseCount:   caseCount,
		CreatedAt:   e.CreatedAt,
		ModifiedAt:  e.ModifiedAt,
	}
}

func toEvalCaseResponse(c *domain.RouterEvaluationCase) EvaluationCaseResponse {
	return EvaluationCaseResponse{
		ID:          c.ID,
		EvalID:      c.EvalID,
		Fields:      map[string]string(c.Fields),
		Expected:    c.Expected,
		ScoreMethod: string(c.ScoreMethod),
		Description: c.Description,
		CreatedAt:   c.CreatedAt,
	}
}

func toRunResponse(run *domain.RouterEvaluationRun, results []EvalCaseResult) *EvaluationRunResponse {
	resp := &EvaluationRunResponse{
		ID:          run.ID,
		EvalID:      run.EvalID,
		RouterID:    run.RouterID,
		TotalCases:  run.TotalCases,
		PassedCases: run.PassedCases,
		AvgScore:    run.AvgScore,
		CreatedAt:   run.CreatedAt,
	}
	if results != nil {
		resp.Results = results
	}
	return resp
}
