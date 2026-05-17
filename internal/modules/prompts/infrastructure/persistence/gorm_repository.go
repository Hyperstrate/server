package persistence

import (
	"context"
	"errors"
	"strings"

	"hyperstrate/server/internal/modules/prompts/domain"

	"gorm.io/gorm"
)

func orgScope(db *gorm.DB, orgID string) *gorm.DB {
	return db.Where("org_id = ?", orgID)
}

type gormPromptRepository struct{ db *gorm.DB }

func NewPromptRepository(db *gorm.DB) domain.PromptRepository {
	return &gormPromptRepository{db: db}
}

func (r *gormPromptRepository) List(ctx context.Context, orgID, query string, offset, limit int) ([]domain.Prompt, int64, error) {
	scope := promptSearchScope(query)
	var total int64
	if err := scope(orgScope(r.db.WithContext(ctx).Model(&domain.Prompt{}), orgID)).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []domain.Prompt
	if err := scope(orgScope(r.db.WithContext(ctx), orgID)).Order("created_at DESC").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func promptSearchScope(query string) func(*gorm.DB) *gorm.DB {
	query = strings.TrimSpace(strings.ToLower(query))
	return func(db *gorm.DB) *gorm.DB {
		if query == "" {
			return db
		}
		like := "%" + query + "%"
		return db.Where(
			"LOWER(id) LIKE ? OR LOWER(name) LIKE ? OR LOWER(description) LIKE ? OR LOWER(content) LIKE ?",
			like,
			like,
			like,
			like,
		)
	}
}

func (r *gormPromptRepository) Create(ctx context.Context, p *domain.Prompt) error {
	return r.db.WithContext(ctx).Create(p).Error
}

func (r *gormPromptRepository) FindByID(ctx context.Context, orgID, id string) (*domain.Prompt, error) {
	var p domain.Prompt
	if err := orgScope(r.db.WithContext(ctx), orgID).First(&p, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrPromptNotFound
		}
		return nil, err
	}
	return &p, nil
}

func (r *gormPromptRepository) Update(ctx context.Context, p *domain.Prompt) error {
	result := orgScope(r.db.WithContext(ctx), p.OrgID).Where("id = ?", p.ID).Save(p)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrPromptNotFound
	}
	return nil
}

func (r *gormPromptRepository) Delete(ctx context.Context, orgID, id string) error {
	result := orgScope(r.db.WithContext(ctx), orgID).Delete(&domain.Prompt{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrPromptNotFound
	}
	return nil
}

// ── PromptVersion repo ────────────────────────────────────────────────────────

type gormPromptVersionRepository struct{ db *gorm.DB }

func NewPromptVersionRepository(db *gorm.DB) domain.PromptVersionRepository {
	return &gormPromptVersionRepository{db: db}
}

func (r *gormPromptVersionRepository) Create(ctx context.Context, v *domain.PromptVersion) error {
	return r.db.WithContext(ctx).Create(v).Error
}

func (r *gormPromptVersionRepository) ListByPromptID(ctx context.Context, orgID, promptID string, offset, limit int) ([]domain.PromptVersion, int64, error) {
	var total int64
	q := r.db.WithContext(ctx).Model(&domain.PromptVersion{}).Where("prompt_id = ? AND org_id = ?", promptID, orgID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []domain.PromptVersion
	if err := q.Order("version DESC").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *gormPromptVersionRepository) FindByID(ctx context.Context, orgID, id string) (*domain.PromptVersion, error) {
	var v domain.PromptVersion
	if err := r.db.WithContext(ctx).Where("id = ? AND org_id = ?", id, orgID).First(&v).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrPromptVersionNotFound
		}
		return nil, err
	}
	return &v, nil
}

func (r *gormPromptVersionRepository) LatestVersion(ctx context.Context, promptID string) (int, error) {
	var max int
	err := r.db.WithContext(ctx).Model(&domain.PromptVersion{}).
		Where("prompt_id = ?", promptID).
		Select("COALESCE(MAX(version), 0)").
		Scan(&max).Error
	return max, err
}
