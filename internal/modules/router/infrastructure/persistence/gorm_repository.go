package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"hyperstrate/server/internal/modules/router/domain"

	"gorm.io/gorm"
)

// ── Scope functions ───────────────────────────────────────────────────────────

// byOrg scopes any entity with a direct org_id column.
func byOrg(orgID string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("org_id = ?", orgID)
	}
}

// targetBelongsToOrg scopes router_targets via JOIN to routers.
func targetBelongsToOrg(orgID string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.
			Joins("JOIN routers ON routers.id = router_targets.router_id").
			Where("routers.org_id = ?", orgID)
	}
}

// featureBelongsToOrg scopes router_features via JOIN to routers.
func featureBelongsToOrg(orgID string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.
			Joins("JOIN routers ON routers.id = router_features.router_id").
			Where("routers.org_id = ?", orgID)
	}
}

// interceptorBelongsToOrg scopes router_interceptors via JOIN to routers.
func interceptorBelongsToOrg(orgID string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.
			Joins("JOIN routers ON routers.id = router_interceptors.router_id").
			Where("routers.org_id = ?", orgID)
	}
}

// ── Router repository ─────────────────────────────────────────────────────────

type gormRouterRepository struct{ db *gorm.DB }

func NewRouterRepository(db *gorm.DB) domain.RouterRepository {
	return &gormRouterRepository{db: db}
}

func (r *gormRouterRepository) List(ctx context.Context, orgID, query string, offset, limit int) ([]domain.Router, int64, error) {
	scope := routerSearchScope(query)
	q := r.db.WithContext(ctx).Model(&domain.Router{}).Scopes(byOrg(orgID), scope)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var routers []domain.Router
	if err := r.db.WithContext(ctx).Scopes(byOrg(orgID), scope).Preload("Configuration").Order("created_at DESC").Offset(offset).Limit(limit).Find(&routers).Error; err != nil {
		return nil, 0, err
	}
	return routers, total, nil
}

func routerSearchScope(query string) func(*gorm.DB) *gorm.DB {
	query = strings.TrimSpace(strings.ToLower(query))
	return func(db *gorm.DB) *gorm.DB {
		if query == "" {
			return db
		}
		like := "%" + query + "%"
		return db.Where(
			"LOWER(id) LIKE ? OR LOWER(name) LIKE ? OR LOWER(description) LIKE ?",
			like,
			like,
			like,
		)
	}
}

func (r *gormRouterRepository) Create(ctx context.Context, router *domain.Router) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Configuration").Create(router).Error; err != nil {
			return err
		}
		router.Configuration.RouterID = router.ID
		return tx.Create(&router.Configuration).Error
	})
}

func (r *gormRouterRepository) FindByID(ctx context.Context, orgID, id string) (*domain.Router, error) {
	var router domain.Router
	err := r.db.WithContext(ctx).Scopes(byOrg(orgID)).Preload("Configuration").First(&router, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrRouterNotFound
		}
		return nil, err
	}
	return &router, nil
}

func (r *gormRouterRepository) Update(ctx context.Context, router *domain.Router) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Scope by both id and org_id so a mis-scoped entity can never overwrite another org's data.
		result := tx.Scopes(byOrg(router.OrgID)).Omit("Configuration").Where("id = ?", router.ID).Save(router)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return domain.ErrRouterNotFound
		}
		return tx.Save(&router.Configuration).Error
	})
}

func (r *gormRouterRepository) Delete(ctx context.Context, orgID, id string) error {
	result := r.db.WithContext(ctx).Scopes(byOrg(orgID)).Delete(&domain.Router{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrRouterNotFound
	}
	return nil
}

func (r *gormRouterRepository) NullifyPromptID(ctx context.Context, promptID string) error {
	return r.db.WithContext(ctx).
		Model(&domain.RouterConfiguration{}).
		Where("prompt_id = ?", promptID).
		Update("prompt_id", nil).Error
}

// ── RouterTarget repository ───────────────────────────────────────────────────

type gormRouterTargetRepository struct{ db *gorm.DB }

func NewRouterTargetRepository(db *gorm.DB) domain.RouterTargetRepository {
	return &gormRouterTargetRepository{db: db}
}

func (r *gormRouterTargetRepository) ListByRouterID(ctx context.Context, orgID, routerID string) ([]domain.RouterTarget, error) {
	var targets []domain.RouterTarget
	err := r.db.WithContext(ctx).
		Select("router_targets.*").
		Scopes(targetBelongsToOrg(orgID)).
		Where("router_targets.router_id = ?", routerID).
		Order("router_targets.priority ASC, router_targets.created_at ASC").
		Find(&targets).Error
	return targets, err
}

func (r *gormRouterTargetRepository) Create(ctx context.Context, t *domain.RouterTarget) error {
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *gormRouterTargetRepository) FindByID(ctx context.Context, orgID, id string) (*domain.RouterTarget, error) {
	var t domain.RouterTarget
	err := r.db.WithContext(ctx).
		Select("router_targets.*").
		Scopes(targetBelongsToOrg(orgID)).
		Where("router_targets.id = ?", id).
		First(&t).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrRouterTargetNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (r *gormRouterTargetRepository) Update(ctx context.Context, t *domain.RouterTarget) error {
	// Scope by router_id (validated against org by the service) to prevent cross-router writes.
	result := r.db.WithContext(ctx).Where("id = ? AND router_id = ?", t.ID, t.RouterID).Save(t)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrRouterTargetNotFound
	}
	return nil
}

func (r *gormRouterTargetRepository) Delete(ctx context.Context, orgID, id string) error {
	ownedRouters := r.db.Model(&domain.Router{}).Select("id").Where("org_id = ?", orgID)
	result := r.db.WithContext(ctx).Where("id = ? AND router_id IN (?)", id, ownedRouters).Delete(&domain.RouterTarget{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrRouterTargetNotFound
	}
	return nil
}

func (r *gormRouterTargetRepository) DeleteByRouterID(ctx context.Context, orgID, routerID string) error {
	ownedRouters := r.db.Model(&domain.Router{}).Select("id").Where("org_id = ? AND id = ?", orgID, routerID)
	return r.db.WithContext(ctx).Where("router_id IN (?)", ownedRouters).Delete(&domain.RouterTarget{}).Error
}

func (r *gormRouterTargetRepository) DeleteByModelID(ctx context.Context, orgID, modelID string) error {
	ownedRouters := r.db.Model(&domain.Router{}).Select("id").Where("org_id = ?", orgID)
	return r.db.WithContext(ctx).Where("model_id = ? AND router_id IN (?)", modelID, ownedRouters).Delete(&domain.RouterTarget{}).Error
}

func (r *gormRouterTargetRepository) ListByModelID(ctx context.Context, orgID, modelID string) ([]domain.RouterTarget, error) {
	var targets []domain.RouterTarget
	err := r.db.WithContext(ctx).
		Select("router_targets.*").
		Scopes(targetBelongsToOrg(orgID)).
		Where("router_targets.model_id = ?", modelID).
		Find(&targets).Error
	return targets, err
}

func (r *gormRouterTargetRepository) NullifyPromptID(ctx context.Context, promptID string) error {
	return r.db.WithContext(ctx).
		Model(&domain.RouterTarget{}).
		Where("prompt_id = ?", promptID).
		Update("prompt_id", nil).Error
}

// ── RouterFeature repository ──────────────────────────────────────────────────

type gormRouterFeatureRepository struct{ db *gorm.DB }

func NewRouterFeatureRepository(db *gorm.DB) domain.RouterFeatureRepository {
	return &gormRouterFeatureRepository{db: db}
}

func (r *gormRouterFeatureRepository) ListByRouterID(ctx context.Context, orgID, routerID string) ([]domain.RouterFeature, error) {
	var features []domain.RouterFeature
	err := r.db.WithContext(ctx).
		Select("router_features.*").
		Scopes(featureBelongsToOrg(orgID)).
		Where("router_features.router_id = ?", routerID).
		Order("router_features.execution_order ASC, router_features.created_at ASC").
		Find(&features).Error
	return features, err
}

func (r *gormRouterFeatureRepository) Create(ctx context.Context, f *domain.RouterFeature) error {
	return r.db.WithContext(ctx).Create(f).Error
}

func (r *gormRouterFeatureRepository) FindByID(ctx context.Context, orgID, id string) (*domain.RouterFeature, error) {
	var f domain.RouterFeature
	err := r.db.WithContext(ctx).
		Select("router_features.*").
		Scopes(featureBelongsToOrg(orgID)).
		Where("router_features.id = ?", id).
		First(&f).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrRouterFeatureNotFound
		}
		return nil, err
	}
	return &f, nil
}

func (r *gormRouterFeatureRepository) Update(ctx context.Context, f *domain.RouterFeature) error {
	result := r.db.WithContext(ctx).Where("id = ? AND router_id = ?", f.ID, f.RouterID).Save(f)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrRouterFeatureNotFound
	}
	return nil
}

func (r *gormRouterFeatureRepository) Delete(ctx context.Context, orgID, id string) error {
	ownedRouters := r.db.Model(&domain.Router{}).Select("id").Where("org_id = ?", orgID)
	result := r.db.WithContext(ctx).Where("id = ? AND router_id IN (?)", id, ownedRouters).Delete(&domain.RouterFeature{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrRouterFeatureNotFound
	}
	return nil
}

func (r *gormRouterFeatureRepository) DeleteByRouterID(ctx context.Context, orgID, routerID string) error {
	ownedRouters := r.db.Model(&domain.Router{}).Select("id").Where("org_id = ? AND id = ?", orgID, routerID)
	return r.db.WithContext(ctx).Where("router_id IN (?)", ownedRouters).Delete(&domain.RouterFeature{}).Error
}

func (r *gormRouterFeatureRepository) RemoveMCPServerID(ctx context.Context, orgID, serverID string) error {
	var features []domain.RouterFeature
	err := r.db.WithContext(ctx).
		Select("router_features.*").
		Scopes(featureBelongsToOrg(orgID)).
		Where("router_features.feature_type = ?", domain.FeatureMCPTools).
		Find(&features).Error
	if err != nil {
		return err
	}
	for i := range features {
		raw, ok := features[i].Config["server_ids"].([]any)
		if !ok {
			continue
		}
		filtered := make([]any, 0, len(raw))
		changed := false
		for _, v := range raw {
			if s, ok := v.(string); ok && s == serverID {
				changed = true
				continue
			}
			filtered = append(filtered, v)
		}
		if !changed {
			continue
		}
		features[i].Config["server_ids"] = filtered
		configJSON, err := json.Marshal(features[i].Config)
		if err != nil {
			return err
		}
		if err := r.db.WithContext(ctx).Model(&domain.RouterFeature{}).
			Where("id = ?", features[i].ID).
			Update("config", string(configJSON)).Error; err != nil {
			return err
		}
	}
	return nil
}

// ── RouterInterceptor repository ──────────────────────────────────────────────

type gormRouterInterceptorRepository struct{ db *gorm.DB }

func NewRouterInterceptorRepository(db *gorm.DB) domain.RouterInterceptorRepository {
	return &gormRouterInterceptorRepository{db: db}
}

func (r *gormRouterInterceptorRepository) ListByRouterID(ctx context.Context, orgID, routerID string) ([]domain.RouterInterceptor, error) {
	var items []domain.RouterInterceptor
	err := r.db.WithContext(ctx).
		Select("router_interceptors.*").
		Scopes(interceptorBelongsToOrg(orgID)).
		Where("router_interceptors.router_id = ?", routerID).
		Order("router_interceptors.execution_order ASC, router_interceptors.created_at ASC").
		Find(&items).Error
	return items, err
}

func (r *gormRouterInterceptorRepository) Create(ctx context.Context, i *domain.RouterInterceptor) error {
	return r.db.WithContext(ctx).Create(i).Error
}

func (r *gormRouterInterceptorRepository) FindByID(ctx context.Context, orgID, id string) (*domain.RouterInterceptor, error) {
	var i domain.RouterInterceptor
	err := r.db.WithContext(ctx).
		Select("router_interceptors.*").
		Scopes(interceptorBelongsToOrg(orgID)).
		Where("router_interceptors.id = ?", id).
		First(&i).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrRouterInterceptorNotFound
		}
		return nil, err
	}
	return &i, nil
}

func (r *gormRouterInterceptorRepository) Update(ctx context.Context, i *domain.RouterInterceptor) error {
	result := r.db.WithContext(ctx).Where("id = ? AND router_id = ?", i.ID, i.RouterID).Save(i)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrRouterInterceptorNotFound
	}
	return nil
}

func (r *gormRouterInterceptorRepository) Delete(ctx context.Context, orgID, id string) error {
	ownedRouters := r.db.Model(&domain.Router{}).Select("id").Where("org_id = ?", orgID)
	result := r.db.WithContext(ctx).Where("id = ? AND router_id IN (?)", id, ownedRouters).Delete(&domain.RouterInterceptor{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrRouterInterceptorNotFound
	}
	return nil
}

func (r *gormRouterInterceptorRepository) DeleteByRouterID(ctx context.Context, orgID, routerID string) error {
	ownedRouters := r.db.Model(&domain.Router{}).Select("id").Where("org_id = ? AND id = ?", orgID, routerID)
	return r.db.WithContext(ctx).Where("router_id IN (?)", ownedRouters).Delete(&domain.RouterInterceptor{}).Error
}

// ── RouterTeamAccess repo ─────────────────────────────────────────────────────

type gormRouterTeamAccessRepository struct{ db *gorm.DB }

func NewRouterTeamAccessRepository(db *gorm.DB) domain.RouterTeamAccessRepository {
	return &gormRouterTeamAccessRepository{db: db}
}

func (r *gormRouterTeamAccessRepository) ListByRouterID(ctx context.Context, routerID string) ([]domain.RouterTeamAccess, error) {
	var rows []domain.RouterTeamAccess
	return rows, r.db.WithContext(ctx).Where("router_id = ?", routerID).Find(&rows).Error
}

func (r *gormRouterTeamAccessRepository) IsTeamAllowed(ctx context.Context, routerID, teamID string) (bool, error) {
	// If no rows exist for this router, access is unrestricted.
	var count int64
	if err := r.db.WithContext(ctx).Model(&domain.RouterTeamAccess{}).Where("router_id = ?", routerID).Count(&count).Error; err != nil {
		return false, err
	}
	if count == 0 {
		return true, nil
	}
	var allowed int64
	err := r.db.WithContext(ctx).Model(&domain.RouterTeamAccess{}).
		Where("router_id = ? AND team_id = ?", routerID, teamID).Count(&allowed).Error
	return allowed > 0, err
}

func (r *gormRouterTeamAccessRepository) Grant(ctx context.Context, a *domain.RouterTeamAccess) error {
	return r.db.WithContext(ctx).
		Where(domain.RouterTeamAccess{RouterID: a.RouterID, TeamID: a.TeamID}).
		FirstOrCreate(a).Error
}

func (r *gormRouterTeamAccessRepository) Revoke(ctx context.Context, routerID, teamID string) error {
	return r.db.WithContext(ctx).
		Where("router_id = ? AND team_id = ?", routerID, teamID).
		Delete(&domain.RouterTeamAccess{}).Error
}

func (r *gormRouterTeamAccessRepository) DeleteByRouterID(ctx context.Context, routerID string) error {
	return r.db.WithContext(ctx).Where("router_id = ?", routerID).Delete(&domain.RouterTeamAccess{}).Error
}
