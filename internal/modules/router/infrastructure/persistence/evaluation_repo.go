package persistence

import (
	"context"

	"hyperstrate/server/internal/modules/router/domain"

	"gorm.io/gorm"
)

// ── Evaluation repository ─────────────────────────────────────────────────────

type gormEvaluationRepository struct{ db *gorm.DB }

func NewEvaluationRepository(db *gorm.DB) domain.EvaluationRepository {
	return &gormEvaluationRepository{db: db}
}

func (r *gormEvaluationRepository) Create(ctx context.Context, e *domain.RouterEvaluation) error {
	return r.db.WithContext(ctx).Create(e).Error
}

func (r *gormEvaluationRepository) FindByID(ctx context.Context, orgID, id string) (*domain.RouterEvaluation, error) {
	var e domain.RouterEvaluation
	if err := r.db.WithContext(ctx).Where("id = ? AND org_id = ?", id, orgID).First(&e).Error; err != nil {
		return nil, domain.ErrEvaluationNotFound
	}
	return &e, nil
}

func (r *gormEvaluationRepository) List(ctx context.Context, orgID, routerID string, offset, limit int) ([]domain.RouterEvaluation, int64, error) {
	q := r.db.WithContext(ctx).Model(&domain.RouterEvaluation{}).Where("org_id = ?", orgID)
	if routerID != "" {
		q = q.Where("router_id = ?", routerID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []domain.RouterEvaluation
	if err := q.Offset(offset).Limit(limit).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *gormEvaluationRepository) Update(ctx context.Context, e *domain.RouterEvaluation) error {
	return r.db.WithContext(ctx).Save(e).Error
}

func (r *gormEvaluationRepository) Delete(ctx context.Context, orgID, id string) error {
	return r.db.WithContext(ctx).Where("id = ? AND org_id = ?", id, orgID).Delete(&domain.RouterEvaluation{}).Error
}

// ── EvaluationCase repository ─────────────────────────────────────────────────

type gormEvaluationCaseRepository struct{ db *gorm.DB }

func NewEvaluationCaseRepository(db *gorm.DB) domain.EvaluationCaseRepository {
	return &gormEvaluationCaseRepository{db: db}
}

func (r *gormEvaluationCaseRepository) ListByEvalID(ctx context.Context, evalID string) ([]domain.RouterEvaluationCase, error) {
	var rows []domain.RouterEvaluationCase
	if err := r.db.WithContext(ctx).Where("eval_id = ?", evalID).Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *gormEvaluationCaseRepository) Create(ctx context.Context, c *domain.RouterEvaluationCase) error {
	return r.db.WithContext(ctx).Create(c).Error
}

func (r *gormEvaluationCaseRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&domain.RouterEvaluationCase{}).Error
}

func (r *gormEvaluationCaseRepository) DeleteByEvalID(ctx context.Context, evalID string) error {
	return r.db.WithContext(ctx).Where("eval_id = ?", evalID).Delete(&domain.RouterEvaluationCase{}).Error
}

// ── EvaluationRun repository ──────────────────────────────────────────────────

type gormEvaluationRunRepository struct{ db *gorm.DB }

func NewEvaluationRunRepository(db *gorm.DB) domain.EvaluationRunRepository {
	return &gormEvaluationRunRepository{db: db}
}

func (r *gormEvaluationRunRepository) Create(ctx context.Context, run *domain.RouterEvaluationRun) error {
	return r.db.WithContext(ctx).Create(run).Error
}

func (r *gormEvaluationRunRepository) ListByEvalID(ctx context.Context, evalID string, offset, limit int) ([]domain.RouterEvaluationRun, int64, error) {
	q := r.db.WithContext(ctx).Model(&domain.RouterEvaluationRun{}).Where("eval_id = ?", evalID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []domain.RouterEvaluationRun
	if err := q.Offset(offset).Limit(limit).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *gormEvaluationRunRepository) FindByID(ctx context.Context, id string) (*domain.RouterEvaluationRun, error) {
	var run domain.RouterEvaluationRun
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&run).Error; err != nil {
		return nil, err
	}
	return &run, nil
}
