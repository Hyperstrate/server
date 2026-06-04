package application

import (
	"context"
	"strings"
	"time"

	authDomain "hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/modules/functions/domain"

	"go.jetify.com/typeid/v2"
)

type BuildSourceSpec = domain.BuildSourceSpec
type BuildArtifactSpec = domain.BuildArtifactSpec

type BuildService interface {
	CreateRevisionBuild(ctx context.Context, revisionID string, input CreateBuildInput) (*BuildResponse, error)
	CompleteBuild(ctx context.Context, buildID string, input CompleteBuildInput) (*BuildResponse, error)
}

type buildService struct {
	builds    domain.BuildRepository
	revisions domain.RevisionRepository
}

func NewBuildService(builds domain.BuildRepository, revisions domain.RevisionRepository) BuildService {
	return &buildService{builds: builds, revisions: revisions}
}

type CreateBuildInput struct {
	Source BuildSourceSpec `json:"source" binding:"required"`
}

type CompleteBuildInput struct {
	Status   domain.BuildStatus `json:"status"   binding:"required,oneof=succeeded failed"`
	Artifact BuildArtifactSpec  `json:"artifact"`
	Error    string             `json:"error,omitempty" binding:"max=4000"`
}

type BuildResponse struct {
	ID         string             `json:"id"`
	AppID      string             `json:"appId"`
	FunctionID string             `json:"functionId"`
	RevisionID string             `json:"revisionId"`
	Status     domain.BuildStatus `json:"status"`
	Source     BuildSourceSpec    `json:"source"`
	Artifact   BuildArtifactSpec  `json:"artifact"`
	Error      string             `json:"error,omitempty"`
	StartedAt  *time.Time         `json:"startedAt,omitempty"`
	FinishedAt *time.Time         `json:"finishedAt,omitempty"`
	CreatedAt  time.Time          `json:"createdAt"`
	ModifiedAt time.Time          `json:"modifiedAt"`
}

func (s *buildService) CreateRevisionBuild(ctx context.Context, revisionID string, input CreateBuildInput) (*BuildResponse, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	source := normalizeBuildSource(input.Source)
	rev, err := s.revisions.FindByID(ctx, orgID, revisionID)
	if err != nil {
		return nil, err
	}
	build := &domain.FunctionBuild{
		ID:         typeid.MustGenerate("fbld").String(),
		OrgID:      orgID,
		AppID:      rev.AppID,
		FunctionID: rev.FunctionID,
		RevisionID: rev.ID,
		Status:     domain.BuildStatusQueued,
		Source:     domain.BuildSourceSpec(source),
	}
	if err := s.builds.Create(ctx, build); err != nil {
		return nil, err
	}
	if err := s.revisions.SetBuildID(ctx, orgID, rev.ID, build.ID); err != nil {
		return nil, err
	}
	resp := toBuildResponse(build)
	return &resp, nil
}

func (s *buildService) CompleteBuild(ctx context.Context, buildID string, input CompleteBuildInput) (*BuildResponse, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	build, err := s.builds.FindByID(ctx, orgID, buildID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if build.StartedAt == nil {
		build.StartedAt = &now
	}
	build.Status = input.Status
	build.Artifact = domain.BuildArtifactSpec(input.Artifact)
	build.Error = input.Error
	build.FinishedAt = &now
	if err := s.builds.Update(ctx, build); err != nil {
		return nil, err
	}
	resp := toBuildResponse(build)
	return &resp, nil
}

func normalizeBuildSource(source BuildSourceSpec) BuildSourceSpec {
	source.Type = strings.TrimSpace(source.Type)
	source.URI = strings.TrimSpace(source.URI)
	source.Digest = strings.TrimSpace(source.Digest)
	source.RootDir = strings.TrimSpace(source.RootDir)
	if source.Type == "" {
		source.Type = "archive"
	}
	return source
}

func toBuildResponse(build *domain.FunctionBuild) BuildResponse {
	return BuildResponse{
		ID:         build.ID,
		AppID:      build.AppID,
		FunctionID: build.FunctionID,
		RevisionID: build.RevisionID,
		Status:     build.Status,
		Source:     BuildSourceSpec(build.Source),
		Artifact:   BuildArtifactSpec(build.Artifact),
		Error:      build.Error,
		StartedAt:  build.StartedAt,
		FinishedAt: build.FinishedAt,
		CreatedAt:  build.CreatedAt,
		ModifiedAt: build.ModifiedAt,
	}
}
