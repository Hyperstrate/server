package persistence

import (
	"context"
	"errors"
	"time"

	"hyperstrate/server/internal/modules/functions/domain"
	"hyperstrate/server/internal/shared/dbtype"
	"hyperstrate/server/internal/shared/pagination"

	"gorm.io/gorm"
)

const (
	tableFunctionApps           = "function_apps"
	tableFunctions              = "functions"
	tableFunctionRevisions      = "function_revisions"
	tableFunctionBuilds         = "function_builds"
	tableFunctionInvocations    = "function_invocations"
	tableFunctionInvocationLogs = "function_invocation_logs"
	tableFunctionRunnerPools    = "function_runner_pools"
	tableFunctionRunnerAgents   = "function_runner_agents"
)

type gormAppRepository struct{ db *gorm.DB }

func NewAppRepository(db *gorm.DB) domain.AppRepository {
	return &gormAppRepository{db: db}
}

func (r *gormAppRepository) Create(ctx context.Context, app *domain.App) error {
	return r.db.WithContext(ctx).Table(tableFunctionApps).Create(app).Error
}

func (r *gormAppRepository) FindByID(ctx context.Context, orgID, id string) (*domain.App, error) {
	var app domain.App
	if err := r.db.WithContext(ctx).Table(tableFunctionApps).Where("org_id = ? AND id = ?", orgID, id).First(&app).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrAppNotFound
		}
		return nil, err
	}
	return &app, nil
}

type gormFunctionRepository struct{ db *gorm.DB }

func NewFunctionRepository(db *gorm.DB) domain.FunctionRepository {
	return &gormFunctionRepository{db: db}
}

func (r *gormFunctionRepository) Create(ctx context.Context, fn *domain.Function) error {
	return r.db.WithContext(ctx).Table(tableFunctions).Create(fn).Error
}

func (r *gormFunctionRepository) CreateWithRevision(ctx context.Context, fn *domain.Function, rev *domain.FunctionRevision) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table(tableFunctions).Create(fn).Error; err != nil {
			return err
		}
		if err := tx.Table(tableFunctionRevisions).Create(rev).Error; err != nil {
			return err
		}
		return nil
	})
}

func (r *gormFunctionRepository) FindByID(ctx context.Context, orgID, id string) (*domain.Function, error) {
	var fn domain.Function
	if err := r.db.WithContext(ctx).Table(tableFunctions).Where("org_id = ? AND id = ?", orgID, id).First(&fn).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrFunctionNotFound
		}
		return nil, err
	}
	return &fn, nil
}

func (r *gormFunctionRepository) Update(ctx context.Context, fn *domain.Function) error {
	result := r.db.WithContext(ctx).
		Table(tableFunctions).
		Where("org_id = ? AND id = ?", fn.OrgID, fn.ID).
		Select("*").
		Updates(fn)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrFunctionNotFound
	}
	return nil
}

type gormRevisionRepository struct{ db *gorm.DB }

func NewRevisionRepository(db *gorm.DB) domain.RevisionRepository {
	return &gormRevisionRepository{db: db}
}

func (r *gormRevisionRepository) Create(ctx context.Context, rev *domain.FunctionRevision) error {
	return r.db.WithContext(ctx).Table(tableFunctionRevisions).Create(rev).Error
}

func (r *gormRevisionRepository) FindByID(ctx context.Context, orgID, id string) (*domain.FunctionRevision, error) {
	var rev domain.FunctionRevision
	if err := r.db.WithContext(ctx).Table(tableFunctionRevisions).Where("org_id = ? AND id = ?", orgID, id).First(&rev).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrRevisionNotFound
		}
		return nil, err
	}
	return &rev, nil
}

func (r *gormRevisionRepository) SetBuildID(ctx context.Context, orgID, revisionID, buildID string) error {
	result := r.db.WithContext(ctx).
		Table(tableFunctionRevisions).
		Where("org_id = ? AND id = ?", orgID, revisionID).
		Update("build_id", buildID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrRevisionNotFound
	}
	return nil
}

type gormBuildRepository struct{ db *gorm.DB }

func NewBuildRepository(db *gorm.DB) domain.BuildRepository {
	return &gormBuildRepository{db: db}
}

func (r *gormBuildRepository) Create(ctx context.Context, build *domain.FunctionBuild) error {
	return r.db.WithContext(ctx).Table(tableFunctionBuilds).Create(build).Error
}

func (r *gormBuildRepository) FindByID(ctx context.Context, orgID, id string) (*domain.FunctionBuild, error) {
	var build domain.FunctionBuild
	if err := r.db.WithContext(ctx).Table(tableFunctionBuilds).Where("org_id = ? AND id = ?", orgID, id).First(&build).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrBuildNotFound
		}
		return nil, err
	}
	return &build, nil
}

func (r *gormBuildRepository) Update(ctx context.Context, build *domain.FunctionBuild) error {
	result := r.db.WithContext(ctx).
		Table(tableFunctionBuilds).
		Where("org_id = ? AND id = ?", build.OrgID, build.ID).
		Select("*").
		Updates(build)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrBuildNotFound
	}
	return nil
}

type gormInvocationRepository struct{ db *gorm.DB }

func NewInvocationRepository(db *gorm.DB) domain.InvocationRepository {
	return &gormInvocationRepository{db: db}
}

func (r *gormInvocationRepository) Create(ctx context.Context, inv *domain.Invocation) error {
	return r.db.WithContext(ctx).Table(tableFunctionInvocations).Create(inv).Error
}

func (r *gormInvocationRepository) FindByID(ctx context.Context, orgID, id string) (*domain.Invocation, error) {
	var inv domain.Invocation
	if err := r.db.WithContext(ctx).Table(tableFunctionInvocations).Where("org_id = ? AND id = ?", orgID, id).First(&inv).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrInvocationNotFound
		}
		return nil, err
	}
	return &inv, nil
}

func (r *gormInvocationRepository) FindByIdempotencyKey(ctx context.Context, orgID, functionID, key string) (*domain.Invocation, error) {
	var inv domain.Invocation
	if err := r.db.WithContext(ctx).
		Table(tableFunctionInvocations).
		Where("org_id = ? AND function_id = ? AND idempotency_key = ? AND idempotency_key <> ''", orgID, functionID, key).
		First(&inv).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrInvocationNotFound
		}
		return nil, err
	}
	return &inv, nil
}

func (r *gormInvocationRepository) LeaseNextQueued(ctx context.Context, orgID, runnerID, leaseID string, leaseExpiresAt time.Time, selector domain.RunnerSelector) (*domain.Invocation, error) {
	var leased domain.Invocation
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		var candidates []domain.Invocation
		reclaimableStatuses := []domain.InvocationStatus{
			domain.InvocationStatusAssigned,
			domain.InvocationStatusStarting,
			domain.InvocationStatusRunning,
		}
		if err := tx.Table(tableFunctionInvocations).
			Where("org_id = ? AND (status = ? OR (status IN ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?))",
				orgID, domain.InvocationStatusQueued, reclaimableStatuses, now).
			Order("created_at ASC").
			Find(&candidates).Error; err != nil {
			return err
		}
		for _, candidate := range candidates {
			var rev domain.FunctionRevision
			if err := tx.Table(tableFunctionRevisions).Where("org_id = ? AND id = ?", orgID, candidate.RevisionID).First(&rev).Error; err != nil {
				continue
			}
			if !domain.RevisionMatchesRunner(&rev, selector) {
				continue
			}
			if candidate.Status != domain.InvocationStatusQueued && candidate.Attempt >= candidate.MaxAttempts {
				tx.Table(tableFunctionInvocations).
					Where("org_id = ? AND id = ?", orgID, candidate.ID).
					Updates(map[string]any{"status": domain.InvocationStatusDeadLettered, "error": "lease expired and max attempts reached"})
				continue
			}
			leased = candidate
			break
		}
		if leased.ID == "" {
			return domain.ErrNoInvocationAvailable
		}
		updates := map[string]any{
			"status":           domain.InvocationStatusAssigned,
			"runner_id":        runnerID,
			"lease_id":         leaseID,
			"lease_expires_at": leaseExpiresAt,
			"attempt":          leased.Attempt + 1,
		}
		result := tx.Table(tableFunctionInvocations).
			Where("org_id = ? AND id = ? AND (status = ? OR (status IN ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?))",
				orgID, leased.ID, domain.InvocationStatusQueued, reclaimableStatuses, now).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return domain.ErrNoInvocationAvailable
		}
		return tx.Table(tableFunctionInvocations).Where("org_id = ? AND id = ?", orgID, leased.ID).First(&leased).Error
	})
	if err != nil {
		return nil, err
	}
	return &leased, nil
}

func (r *gormInvocationRepository) Complete(ctx context.Context, orgID, invocationID, runnerID, leaseID string, status domain.InvocationStatus, resultPayload dbtype.JSONMap, errorMessage string, finishedAt time.Time) (*domain.Invocation, error) {
	updates := map[string]any{
		"status":      status,
		"result":      resultPayload,
		"error":       errorMessage,
		"finished_at": finishedAt,
	}
	if status == domain.InvocationStatusRunning {
		updates["started_at"] = finishedAt
		updates["finished_at"] = nil
	}
	res := r.db.WithContext(ctx).
		Table(tableFunctionInvocations).
		Where("org_id = ? AND id = ? AND runner_id = ? AND lease_id = ? AND lease_expires_at IS NOT NULL AND lease_expires_at > ? AND status IN ?",
			orgID,
			invocationID,
			runnerID,
			leaseID,
			finishedAt,
			[]domain.InvocationStatus{domain.InvocationStatusAssigned, domain.InvocationStatusStarting, domain.InvocationStatusRunning},
		).
		Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, domain.ErrInvocationNotFound
	}
	return r.FindByID(ctx, orgID, invocationID)
}

type gormLogRepository struct{ db *gorm.DB }

func NewLogRepository(db *gorm.DB) domain.LogRepository {
	return &gormLogRepository{db: db}
}

func (r *gormLogRepository) Append(ctx context.Context, log *domain.InvocationLog) error {
	return r.db.WithContext(ctx).Table(tableFunctionInvocationLogs).Create(log).Error
}

func (r *gormLogRepository) ListByInvocationID(ctx context.Context, orgID, invocationID string, slice pagination.Slice) ([]domain.InvocationLog, int64, error) {
	base := r.db.WithContext(ctx).
		Table(tableFunctionInvocationLogs).
		Where("org_id = ? AND invocation_id = ?", orgID, invocationID)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var logs []domain.InvocationLog
	err := base.
		Order("seq ASC, created_at ASC").
		Limit(slice.PerPage).
		Offset(slice.Offset()).
		Find(&logs).Error
	return logs, total, err
}

type gormRunnerPoolRepository struct{ db *gorm.DB }

func NewRunnerPoolRepository(db *gorm.DB) domain.RunnerPoolRepository {
	return &gormRunnerPoolRepository{db: db}
}

func (r *gormRunnerPoolRepository) Create(ctx context.Context, pool *domain.RunnerPool) error {
	return r.db.WithContext(ctx).Table(tableFunctionRunnerPools).Create(pool).Error
}

func (r *gormRunnerPoolRepository) FindByID(ctx context.Context, id string) (*domain.RunnerPool, error) {
	var pool domain.RunnerPool
	if err := r.db.WithContext(ctx).Table(tableFunctionRunnerPools).Where("id = ?", id).First(&pool).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrRunnerPoolNotFound
		}
		return nil, err
	}
	return &pool, nil
}

func (r *gormRunnerPoolRepository) ConsumeBootstrapToken(ctx context.Context, id, tokenHash string, consumedAt time.Time) (*domain.RunnerPool, error) {
	result := r.db.WithContext(ctx).
		Table(tableFunctionRunnerPools).
		Where("id = ? AND bootstrap_token_hash = ? AND bootstrap_token_consumed_at IS NULL AND status = ?", id, tokenHash, domain.RunnerPoolStatusActive).
		Update("bootstrap_token_consumed_at", consumedAt)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, domain.ErrRunnerPoolNotFound
	}
	return r.FindByID(ctx, id)
}

type gormRunnerAgentRepository struct{ db *gorm.DB }

func NewRunnerAgentRepository(db *gorm.DB) domain.RunnerAgentRepository {
	return &gormRunnerAgentRepository{db: db}
}

func (r *gormRunnerAgentRepository) Create(ctx context.Context, agent *domain.RunnerAgent) error {
	return r.db.WithContext(ctx).Table(tableFunctionRunnerAgents).Create(agent).Error
}

func (r *gormRunnerAgentRepository) FindByID(ctx context.Context, orgID, id string) (*domain.RunnerAgent, error) {
	var agent domain.RunnerAgent
	if err := r.db.WithContext(ctx).Table(tableFunctionRunnerAgents).Where("org_id = ? AND id = ?", orgID, id).First(&agent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrRunnerAgentNotFound
		}
		return nil, err
	}
	return &agent, nil
}

func (r *gormRunnerAgentRepository) FindBySessionTokenHash(ctx context.Context, hash string) (*domain.RunnerAgent, error) {
	var agent domain.RunnerAgent
	if err := r.db.WithContext(ctx).Table(tableFunctionRunnerAgents).Where("session_token_hash = ?", hash).First(&agent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrRunnerAgentNotFound
		}
		return nil, err
	}
	return &agent, nil
}

func (r *gormRunnerAgentRepository) RecordHeartbeat(ctx context.Context, orgID, id string, heartbeatAt, sessionExpiresAt time.Time, capabilities dbtype.JSONMap) (*domain.RunnerAgent, error) {
	result := r.db.WithContext(ctx).
		Table(tableFunctionRunnerAgents).
		Where("org_id = ? AND id = ? AND status = ?", orgID, id, domain.RunnerAgentStatusOnline).
		Updates(map[string]any{
			"last_heartbeat_at":  heartbeatAt,
			"session_expires_at": sessionExpiresAt,
			"capabilities":       capabilities,
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, domain.ErrRunnerAgentNotFound
	}
	return r.FindByID(ctx, orgID, id)
}
