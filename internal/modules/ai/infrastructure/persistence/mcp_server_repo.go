package persistence

import (
	"context"
	"errors"

	"hyperstrate/server/internal/modules/ai/domain"

	"gorm.io/gorm"
)

type gormMCPServerRepository struct{ db *gorm.DB }

func NewMCPServerRepository(db *gorm.DB) domain.MCPServerRepository {
	return &gormMCPServerRepository{db: db}
}

func (r *gormMCPServerRepository) Create(ctx context.Context, server *domain.MCPServer) error {
	return r.db.WithContext(ctx).Create(server).Error
}

func (r *gormMCPServerRepository) FindByID(ctx context.Context, orgID, serverID string) (*domain.MCPServer, error) {
	var s domain.MCPServer
	err := r.db.WithContext(ctx).
		Where("id = ? AND org_id = ?", serverID, orgID).
		First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrMCPServerNotFound
	}
	return &s, err
}

func (r *gormMCPServerRepository) FindByIDs(ctx context.Context, orgID string, serverIDs []string) ([]domain.MCPServer, error) {
	if len(serverIDs) == 0 {
		return nil, nil
	}
	var servers []domain.MCPServer
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND id IN ?", orgID, serverIDs).
		Find(&servers).Error
	return servers, err
}

func (r *gormMCPServerRepository) List(ctx context.Context, orgID string) ([]domain.MCPServer, error) {
	var servers []domain.MCPServer
	err := r.db.WithContext(ctx).
		Where("org_id = ?", orgID).
		Order("name ASC").
		Find(&servers).Error
	return servers, err
}

func (r *gormMCPServerRepository) Update(ctx context.Context, server *domain.MCPServer) error {
	return r.db.WithContext(ctx).Save(server).Error
}

func (r *gormMCPServerRepository) Delete(ctx context.Context, orgID, serverID string) error {
	result := r.db.WithContext(ctx).
		Where("id = ? AND org_id = ?", serverID, orgID).
		Delete(&domain.MCPServer{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrMCPServerNotFound
	}
	return nil
}
