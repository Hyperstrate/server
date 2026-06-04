package domain

import (
	"context"
	"errors"
	"time"

	"hyperstrate/server/internal/shared/dbtype"
)

var (
	ErrRunnerPoolNotFound  = errors.New("runner pool not found")
	ErrRunnerAgentNotFound = errors.New("runner agent not found")
	ErrRunnerUnauthorized  = errors.New("missing or invalid runner session token")
)

type RunnerPoolStatus string

const (
	RunnerPoolStatusActive  RunnerPoolStatus = "active"
	RunnerPoolStatusRevoked RunnerPoolStatus = "revoked"
)

type RunnerAgentStatus string

const (
	RunnerAgentStatusOnline  RunnerAgentStatus = "online"
	RunnerAgentStatusOffline RunnerAgentStatus = "offline"
	RunnerAgentStatusRevoked RunnerAgentStatus = "revoked"
)

type RunnerPool struct {
	ID                       string           `json:"id"                 gorm:"primaryKey;size:50"`
	OrgID                    string           `json:"-"                  gorm:"size:50;not null;index"`
	Name                     string           `json:"name"               gorm:"size:255;not null"`
	Provider                 string           `json:"provider"           gorm:"size:100;not null"`
	Region                   string           `json:"region,omitempty"   gorm:"size:100;not null;default:''"`
	Status                   RunnerPoolStatus `json:"status"             gorm:"size:50;not null;default:active"`
	Capabilities             dbtype.JSONMap   `json:"capabilities"       gorm:"serializer:json;column:capabilities"`
	BootstrapTokenHash       string           `json:"-"                  gorm:"size:128;not null"`
	BootstrapTokenConsumedAt *time.Time       `json:"bootstrapTokenConsumedAt,omitempty"`
	CreatedAt                time.Time        `json:"createdAt"`
	ModifiedAt               time.Time        `json:"modifiedAt"         gorm:"autoUpdateTime"`
}

func (RunnerPool) TableName() string { return "function_runner_pools" }

type RunnerAgent struct {
	ID               string            `json:"id"               gorm:"primaryKey;size:50"`
	OrgID            string            `json:"-"                gorm:"size:50;not null;index"`
	PoolID           string            `json:"poolId"           gorm:"size:50;not null;index"`
	Hostname         string            `json:"hostname"         gorm:"size:255;not null;default:''"`
	PublicKey        string            `json:"publicKey"        gorm:"type:text;not null"`
	Status           RunnerAgentStatus `json:"status"           gorm:"size:50;not null;default:online"`
	Capabilities     dbtype.JSONMap    `json:"capabilities"     gorm:"serializer:json;column:capabilities"`
	SessionTokenHash string            `json:"-"                gorm:"size:128;not null"`
	SessionExpiresAt time.Time         `json:"sessionExpiresAt" gorm:"not null"`
	LastHeartbeatAt  *time.Time        `json:"lastHeartbeatAt,omitempty"`
	CreatedAt        time.Time         `json:"createdAt"`
	ModifiedAt       time.Time         `json:"modifiedAt"       gorm:"autoUpdateTime"`
}

func (RunnerAgent) TableName() string { return "function_runner_agents" }

type RunnerPoolRepository interface {
	Create(ctx context.Context, pool *RunnerPool) error
	FindByID(ctx context.Context, id string) (*RunnerPool, error)
	ConsumeBootstrapToken(ctx context.Context, id, tokenHash string, consumedAt time.Time) (*RunnerPool, error)
}

type RunnerAgentRepository interface {
	Create(ctx context.Context, agent *RunnerAgent) error
	FindByID(ctx context.Context, orgID, id string) (*RunnerAgent, error)
	FindBySessionTokenHash(ctx context.Context, hash string) (*RunnerAgent, error)
	RecordHeartbeat(ctx context.Context, orgID, id string, heartbeatAt, sessionExpiresAt time.Time, capabilities dbtype.JSONMap) (*RunnerAgent, error)
}
