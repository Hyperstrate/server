package domain

import (
	"context"
	"errors"
	"time"

	"hyperstrate/server/internal/shared/pagination"
)

var (
	ErrBuildNotFound = errors.New("function build not found")
	ErrBuildNotReady = errors.New("function build not ready")
)

type BuildStatus string

const (
	BuildStatusQueued    BuildStatus = "queued"
	BuildStatusBuilding  BuildStatus = "building"
	BuildStatusSucceeded BuildStatus = "succeeded"
	BuildStatusFailed    BuildStatus = "failed"
)

type BuildSourceSpec struct {
	Type      string `json:"type"`
	URI       string `json:"uri,omitempty"`
	Digest    string `json:"digest,omitempty"`
	SizeBytes int64  `json:"sizeBytes,omitempty"`
	RootDir   string `json:"rootDir,omitempty"`
}

type BuildArtifactSpec struct {
	ImageRef         string `json:"imageRef,omitempty"`
	SourceArchiveRef string `json:"sourceArchiveRef,omitempty"`
	Digest           string `json:"digest,omitempty"`
	SizeBytes        int64  `json:"sizeBytes,omitempty"`
}

type FunctionBuild struct {
	ID         string            `json:"id"         gorm:"primaryKey;size:50"`
	OrgID      string            `json:"-"          gorm:"size:50;not null;index"`
	AppID      string            `json:"appId"      gorm:"size:50;not null;index"`
	FunctionID string            `json:"functionId" gorm:"size:50;not null;index"`
	RevisionID string            `json:"revisionId" gorm:"size:50;not null;index"`
	Status     BuildStatus       `json:"status"     gorm:"size:50;not null;default:queued"`
	Source     BuildSourceSpec   `json:"source"     gorm:"serializer:json;column:source"`
	Artifact   BuildArtifactSpec `json:"artifact"   gorm:"serializer:json;column:artifact"`
	Error      string            `json:"error,omitempty" gorm:"type:text"`
	StartedAt  *time.Time        `json:"startedAt,omitempty"`
	FinishedAt *time.Time        `json:"finishedAt,omitempty"`
	CreatedAt  time.Time         `json:"createdAt"`
	ModifiedAt time.Time         `json:"modifiedAt" gorm:"autoUpdateTime"`
}

func (FunctionBuild) TableName() string { return "function_builds" }

type BuildRepository interface {
	Create(ctx context.Context, build *FunctionBuild) error
	FindByID(ctx context.Context, orgID, id string) (*FunctionBuild, error)
	ListByRevision(ctx context.Context, orgID, revisionID string, slice pagination.Slice) ([]FunctionBuild, int64, error)
	ListByFunction(ctx context.Context, orgID, functionID string, slice pagination.Slice) ([]FunctionBuild, int64, error)
	Update(ctx context.Context, build *FunctionBuild) error
}
