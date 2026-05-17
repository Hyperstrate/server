package domain

import (
	"context"
	"errors"
	"time"

	"hyperstrate/server/internal/shared/dbtype"
)

var ErrEvaluationNotFound = errors.New("evaluation not found")

// ScoreMethod controls how a case result is scored.
type ScoreMethod string

const (
	ScoreMethodExact     ScoreMethod = "exact"      // case-insensitive exact match
	ScoreMethodContains  ScoreMethod = "contains"   // expected is a substring of actual
	ScoreMethodLLM       ScoreMethod = "llm"        // LLM judge (requires judge_model_id in config)
	ScoreMethodAgentTask ScoreMethod = "agent_task" // coding-agent task rubric encoded as JSON
)

// RouterEvaluation is a named set of test cases for a router.
type RouterEvaluation struct {
	ID          string    `json:"id"          gorm:"primaryKey;size:50"`
	OrgID       string    `json:"orgId"       gorm:"size:50;not null;index"`
	RouterID    string    `json:"routerId"    gorm:"size:50;not null;index"`
	Name        string    `json:"name"        gorm:"size:255;not null"`
	Description string    `json:"description" gorm:"type:text"`
	CreatedAt   time.Time `json:"createdAt"`
	ModifiedAt  time.Time `json:"modifiedAt"  gorm:"autoUpdateTime"`
}

func (RouterEvaluation) TableName() string { return "router_evaluations" }

// RouterEvaluationCase is one test case within an evaluation.
type RouterEvaluationCase struct {
	ID     string `json:"id"           gorm:"primaryKey;size:50"`
	EvalID string `json:"evalId"       gorm:"size:50;not null;index"`
	// Fields is the request fields map sent to RouteInfer.
	Fields dbtype.JSONStringMap `json:"fields"       gorm:"serializer:json"`
	// Expected is the expected model output (interpretation depends on ScoreMethod).
	Expected string `json:"expected"     gorm:"type:text"`
	// ScoreMethod is one of: exact | contains | llm.
	ScoreMethod ScoreMethod `json:"scoreMethod"  gorm:"size:20;not null;default:'contains'"`
	Description string      `json:"description"  gorm:"type:text"`
	CreatedAt   time.Time   `json:"createdAt"`
}

func (RouterEvaluationCase) TableName() string { return "router_evaluation_cases" }

// RouterEvaluationRun is the result of executing all cases in one evaluation.
type RouterEvaluationRun struct {
	ID          string  `json:"id"           gorm:"primaryKey;size:50"`
	EvalID      string  `json:"evalId"       gorm:"size:50;not null;index"`
	RouterID    string  `json:"routerId"     gorm:"size:50;not null"`
	TotalCases  int     `json:"totalCases"   gorm:"not null;default:0"`
	PassedCases int     `json:"passedCases"  gorm:"not null;default:0"`
	AvgScore    float64 `json:"avgScore"     gorm:"not null;default:0"`
	// Results holds per-case outcome (JSON array of EvalCaseResult).
	Results   dbtype.JSONMap `json:"results"      gorm:"serializer:json"`
	CreatedAt time.Time      `json:"createdAt"`
}

func (RouterEvaluationRun) TableName() string { return "router_evaluation_runs" }

// ── Repositories ──────────────────────────────────────────────────────────────

type EvaluationRepository interface {
	Create(ctx context.Context, e *RouterEvaluation) error
	FindByID(ctx context.Context, orgID, id string) (*RouterEvaluation, error)
	List(ctx context.Context, orgID, routerID string, offset, limit int) ([]RouterEvaluation, int64, error)
	Update(ctx context.Context, e *RouterEvaluation) error
	Delete(ctx context.Context, orgID, id string) error
}

type EvaluationCaseRepository interface {
	ListByEvalID(ctx context.Context, evalID string) ([]RouterEvaluationCase, error)
	Create(ctx context.Context, c *RouterEvaluationCase) error
	Delete(ctx context.Context, id string) error
	DeleteByEvalID(ctx context.Context, evalID string) error
}

type EvaluationRunRepository interface {
	Create(ctx context.Context, r *RouterEvaluationRun) error
	ListByEvalID(ctx context.Context, evalID string, offset, limit int) ([]RouterEvaluationRun, int64, error)
	FindByID(ctx context.Context, id string) (*RouterEvaluationRun, error)
}
