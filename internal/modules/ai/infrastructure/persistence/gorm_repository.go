package persistence

import (
	"context"
	"errors"
	"strings"

	"hyperstrate/server/internal/modules/ai/domain"

	"gorm.io/gorm"
)

// ── Scope functions ───────────────────────────────────────────────────────────

// byOrg scopes any entity that has an org_id column directly.
func byOrg(orgID string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("org_id = ?", orgID)
	}
}

// throughModel scopes model_configurations via a JOIN to models,
// ensuring the config belongs to an org even though it has no org_id column.
func throughModel(orgID string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.
			Joins("JOIN models ON models.id = model_configurations.model_id").
			Where("models.org_id = ?", orgID)
	}
}

// throughConversation scopes conversation_messages via a JOIN to conversations.
func throughConversation(orgID string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.
			Joins("JOIN conversations ON conversations.id = conversation_messages.conversation_id").
			Where("conversations.org_id = ?", orgID)
	}
}

// ── Model repository ─────────────────────────────────────────────────────────

type gormModelRepository struct{ db *gorm.DB }

func NewModelRepository(db *gorm.DB) domain.ModelRepository {
	return &gormModelRepository{db: db}
}

func (r *gormModelRepository) List(ctx context.Context, orgID, query string, offset, limit int) ([]domain.Model, int64, error) {
	scope := modelSearchScope(query)
	var total int64
	if err := r.db.WithContext(ctx).Model(&domain.Model{}).Scopes(byOrg(orgID), scope).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var models []domain.Model
	if err := r.db.WithContext(ctx).Scopes(byOrg(orgID), scope).Order("created_at DESC").Offset(offset).Limit(limit).Find(&models).Error; err != nil {
		return nil, 0, err
	}
	return models, total, nil
}

func (r *gormModelRepository) Create(ctx context.Context, model *domain.Model) error {
	return r.db.WithContext(ctx).Create(model).Error
}

func (r *gormModelRepository) FindByID(ctx context.Context, orgID, id string) (*domain.Model, error) {
	var model domain.Model
	if err := r.db.WithContext(ctx).Scopes(byOrg(orgID)).First(&model, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrModelNotFound
		}
		return nil, err
	}
	return &model, nil
}

func (r *gormModelRepository) Update(ctx context.Context, model *domain.Model) error {
	result := r.db.WithContext(ctx).Where("id = ? AND org_id = ?", model.ID, model.OrgID).Save(model)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrModelNotFound
	}
	return nil
}

func (r *gormModelRepository) ListByIDs(ctx context.Context, orgID string, ids []string) ([]domain.Model, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var models []domain.Model
	if err := r.db.WithContext(ctx).Scopes(byOrg(orgID)).Where("id IN ?", ids).Find(&models).Error; err != nil {
		return nil, err
	}
	return models, nil
}

func (r *gormModelRepository) Delete(ctx context.Context, orgID, id string) error {
	result := r.db.WithContext(ctx).Scopes(byOrg(orgID)).Delete(&domain.Model{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrModelNotFound
	}
	return nil
}

func (r *gormModelRepository) ListAll(ctx context.Context) ([]domain.Model, error) {
	var models []domain.Model
	if err := r.db.WithContext(ctx).Find(&models).Error; err != nil {
		return nil, err
	}
	return models, nil
}

func (r *gormModelRepository) ListByDefinitionKeys(ctx context.Context, orgID string, keys []string, query string, offset, limit int) ([]domain.Model, int64, error) {
	if len(keys) == 0 {
		return nil, 0, nil
	}
	scope := modelSearchScope(query)
	var total int64
	if err := r.db.WithContext(ctx).Model(&domain.Model{}).Scopes(byOrg(orgID), scope).Where("model_definition_key IN ?", keys).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var models []domain.Model
	if err := r.db.WithContext(ctx).Scopes(byOrg(orgID), scope).Where("model_definition_key IN ?", keys).Order("created_at DESC").Offset(offset).Limit(limit).Find(&models).Error; err != nil {
		return nil, 0, err
	}
	return models, total, nil
}

func modelSearchScope(query string) func(*gorm.DB) *gorm.DB {
	query = strings.TrimSpace(strings.ToLower(query))
	// Pre-compute catalog keys whose displayName / modelId / etc. match the query.
	// This lets users search by what they see (e.g. "GPT-4o") even when the DB
	// columns (alias, model_definition_key) wouldn't match on their own.
	catalogKeys := domain.DefinitionKeysMatchingQuery(query)
	return func(db *gorm.DB) *gorm.DB {
		if query == "" {
			return db
		}
		like := "%" + query + "%"
		q := db.Where(
			"LOWER(id) LIKE ? OR LOWER(alias) LIKE ? OR LOWER(model_definition_key) LIKE ?",
			like, like, like,
		)
		if len(catalogKeys) > 0 {
			q = q.Or("model_definition_key IN ?", catalogKeys)
		}
		return q
	}
}

// ── ModelConfiguration repository ───────────────────────────────────────────────────

type gormModelConfigurationRepository struct{ db *gorm.DB }

func NewModelConfigurationRepository(db *gorm.DB) domain.ModelConfigurationRepository {
	return &gormModelConfigurationRepository{db: db}
}

func (r *gormModelConfigurationRepository) FindByModelID(ctx context.Context, orgID, modelID string) (*domain.ModelConfiguration, error) {
	var cfg domain.ModelConfiguration
	err := r.db.WithContext(ctx).
		Scopes(throughModel(orgID)).
		Where("model_configurations.model_id = ?", modelID).
		First(&cfg).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrModelConfigurationNotFound
		}
		return nil, err
	}
	return &cfg, nil
}

func (r *gormModelConfigurationRepository) ListConfiguredModelIDs(ctx context.Context, orgID string, modelIDs []string) ([]string, error) {
	if len(modelIDs) == 0 {
		return nil, nil
	}
	var ids []string
	err := r.db.WithContext(ctx).
		Model(&domain.ModelConfiguration{}).
		Scopes(throughModel(orgID)).
		Where("model_configurations.model_id IN ?", modelIDs).
		Pluck("model_id", &ids).Error
	return ids, err
}

func (r *gormModelConfigurationRepository) ListByModelIDs(ctx context.Context, orgID string, modelIDs []string) ([]domain.ModelConfiguration, error) {
	if len(modelIDs) == 0 {
		return nil, nil
	}
	var cfgs []domain.ModelConfiguration
	err := r.db.WithContext(ctx).
		Scopes(throughModel(orgID)).
		Where("model_configurations.model_id IN ?", modelIDs).
		Find(&cfgs).Error
	return cfgs, err
}

func (r *gormModelConfigurationRepository) ListAllByModelIDs(ctx context.Context, modelIDs []string) ([]domain.ModelConfiguration, error) {
	if len(modelIDs) == 0 {
		return nil, nil
	}
	var cfgs []domain.ModelConfiguration
	err := r.db.WithContext(ctx).Where("model_id IN ?", modelIDs).Find(&cfgs).Error
	return cfgs, err
}

func (r *gormModelConfigurationRepository) Upsert(ctx context.Context, orgID string, cfg *domain.ModelConfiguration) error {
	var existing domain.ModelConfiguration
	err := r.db.WithContext(ctx).
		Scopes(throughModel(orgID)).
		Where("model_configurations.model_id = ?", cfg.ModelID).
		First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.db.WithContext(ctx).Create(cfg).Error
	}
	if err != nil {
		return err
	}
	cfg.ID = existing.ID
	return r.db.WithContext(ctx).Save(cfg).Error
}

func (r *gormModelConfigurationRepository) DeleteByModelID(ctx context.Context, orgID, modelID string) error {
	// SQLite has no DELETE...JOIN; use subquery for the org scope check.
	ownedIDs := r.db.Model(&domain.Model{}).Select("id").Where("org_id = ? AND id = ?", orgID, modelID)
	return r.db.WithContext(ctx).Where("model_id IN (?)", ownedIDs).Delete(&domain.ModelConfiguration{}).Error
}

// ── Conversation repository ──────────────────────────────────────────────────

type gormConversationRepository struct{ db *gorm.DB }

func NewConversationRepository(db *gorm.DB) domain.ConversationRepository {
	return &gormConversationRepository{db: db}
}

func (r *gormConversationRepository) List(ctx context.Context, orgID string, offset, limit int) ([]domain.Conversation, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&domain.Conversation{}).Scopes(byOrg(orgID)).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []domain.Conversation
	if err := r.db.WithContext(ctx).Scopes(byOrg(orgID)).Order("created_at DESC").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *gormConversationRepository) Create(ctx context.Context, c *domain.Conversation) error {
	return r.db.WithContext(ctx).Create(c).Error
}

func (r *gormConversationRepository) FindByID(ctx context.Context, orgID, id string) (*domain.Conversation, error) {
	var c domain.Conversation
	if err := r.db.WithContext(ctx).Scopes(byOrg(orgID)).First(&c, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrConversationNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (r *gormConversationRepository) Delete(ctx context.Context, orgID, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if res := tx.Where("id = ? AND org_id = ?", id, orgID).Delete(&domain.Conversation{}); res.Error != nil {
			return res.Error
		} else if res.RowsAffected == 0 {
			return domain.ErrConversationNotFound
		}
		return tx.Where("conversation_id = ?", id).Delete(&domain.ConversationMessage{}).Error
	})
}

func (r *gormConversationRepository) ListMessages(ctx context.Context, orgID, conversationID string) ([]domain.ConversationMessage, error) {
	var msgs []domain.ConversationMessage
	err := r.db.WithContext(ctx).
		Scopes(throughConversation(orgID)).
		Where("conversation_messages.conversation_id = ?", conversationID).
		Order("conversation_messages.created_at ASC").
		Find(&msgs).Error
	return msgs, err
}

func (r *gormConversationRepository) AddMessage(ctx context.Context, orgID string, msg *domain.ConversationMessage) error {
	var count int64
	if err := r.db.WithContext(ctx).Model(&domain.Conversation{}).
		Where("id = ? AND org_id = ?", msg.ConversationID, orgID).
		Count(&count).Error; err != nil || count == 0 {
		return domain.ErrConversationNotFound
	}
	return r.db.WithContext(ctx).Create(msg).Error
}

// ── Job repository ───────────────────────────────────────────────────────────

type gormJobRepository struct{ db *gorm.DB }

func NewJobRepository(db *gorm.DB) domain.JobRepository {
	return &gormJobRepository{db: db}
}

func (r *gormJobRepository) List(ctx context.Context, orgID string, offset, limit int) ([]domain.Job, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&domain.Job{}).Scopes(byOrg(orgID)).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var jobs []domain.Job
	if err := r.db.WithContext(ctx).Scopes(byOrg(orgID)).Order("created_at DESC").Offset(offset).Limit(limit).Find(&jobs).Error; err != nil {
		return nil, 0, err
	}
	return jobs, total, nil
}

func (r *gormJobRepository) ListByStatus(ctx context.Context, status domain.JobStatus) ([]domain.Job, error) {
	var jobs []domain.Job
	err := r.db.WithContext(ctx).Where("status = ?", status).Order("created_at DESC").Find(&jobs).Error
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *gormJobRepository) Create(ctx context.Context, job *domain.Job) error {
	return r.db.WithContext(ctx).Create(job).Error
}

func (r *gormJobRepository) FindByID(ctx context.Context, orgID, id string) (*domain.Job, error) {
	var job domain.Job
	if err := r.db.WithContext(ctx).Scopes(byOrg(orgID)).First(&job, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrJobNotFound
		}
		return nil, err
	}
	return &job, nil
}

func (r *gormJobRepository) FindByIDForWorker(ctx context.Context, id string) (*domain.Job, error) {
	var job domain.Job
	if err := r.db.WithContext(ctx).First(&job, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrJobNotFound
		}
		return nil, err
	}
	return &job, nil
}

func (r *gormJobRepository) Update(ctx context.Context, job *domain.Job) error {
	return r.db.WithContext(ctx).Scopes(byOrg(job.OrgID)).Where("id = ?", job.ID).Save(job).Error
}

// ── ModelKeyRotation repo ─────────────────────────────────────────────────────

type gormModelKeyRotationRepository struct{ db *gorm.DB }

func NewModelKeyRotationRepository(db *gorm.DB) domain.ModelKeyRotationRepository {
	return &gormModelKeyRotationRepository{db: db}
}

func (r *gormModelKeyRotationRepository) Create(ctx context.Context, rot *domain.ModelKeyRotation) error {
	return r.db.WithContext(ctx).Create(rot).Error
}

func (r *gormModelKeyRotationRepository) ListByModelID(ctx context.Context, modelID string, limit, offset int) ([]domain.ModelKeyRotation, int64, error) {
	var total int64
	q := r.db.WithContext(ctx).Model(&domain.ModelKeyRotation{}).Where("model_id = ?", modelID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []domain.ModelKeyRotation
	return rows, total, q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error
}
