package application

import (
	"time"

	"hyperstrate/server/internal/modules/functions/domain"
	"hyperstrate/server/internal/shared/dbtype"
)

type ImageSpec = domain.ImageSpec
type RuntimeSpec = domain.RuntimeSpec
type AutoscalingSpec = domain.AutoscalingSpec
type SecuritySpec = domain.SecuritySpec
type SecretMountSpec = domain.SecretMountSpec
type VolumeMountSpec = domain.VolumeMountSpec
type ProviderPlacementSpec = domain.ProviderPlacementSpec

type CreateAppInput struct {
	Name        string `json:"name"        binding:"required,max=255"`
	Description string `json:"description" binding:"max=2000"`
}

type DeployFunctionInput struct {
	Name        string                `json:"name"       binding:"required,max=255"`
	Entrypoint  string                `json:"entrypoint" binding:"required,max=512"`
	Image       ImageSpec             `json:"image"      binding:"required"`
	Runtime     RuntimeSpec           `json:"runtime"`
	Autoscaling AutoscalingSpec       `json:"autoscaling"`
	Security    SecuritySpec          `json:"security"`
	Secrets     []SecretMountSpec     `json:"secrets,omitempty"`
	Volumes     []VolumeMountSpec     `json:"volumes,omitempty"`
	Provider    ProviderPlacementSpec `json:"provider"`
}

type InvokeFunctionInput struct {
	Mode           domain.InvocationMode `json:"mode,omitempty" binding:"omitempty,oneof=async"`
	Payload        map[string]any        `json:"payload,omitempty"`
	IdempotencyKey string                `json:"idempotencyKey,omitempty" binding:"max=255"`
	MaxAttempts    int                   `json:"maxAttempts,omitempty" binding:"min=0,max=100"`
}

type AppendLogInput struct {
	Stream    domain.LogStream `json:"stream"  binding:"required,oneof=stdout stderr system"`
	Message   string           `json:"message" binding:"required"`
	Seq       int64            `json:"seq,omitempty" binding:"min=0"`
	Truncated bool             `json:"truncated,omitempty"`
}

type AppResponse struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"-"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	ModifiedAt  time.Time `json:"modifiedAt"`
}

type FunctionResponse struct {
	ID               string                `json:"id"`
	OrgID            string                `json:"-"`
	AppID            string                `json:"appId"`
	Name             string                `json:"name"`
	Entrypoint       string                `json:"entrypoint"`
	Status           domain.FunctionStatus `json:"status"`
	ActiveRevisionID string                `json:"activeRevisionId"`
	CreatedAt        time.Time             `json:"createdAt"`
	ModifiedAt       time.Time             `json:"modifiedAt"`
}

type RevisionResponse struct {
	ID          string                `json:"id"`
	AppID       string                `json:"appId"`
	FunctionID  string                `json:"functionId"`
	Version     int                   `json:"version"`
	Entrypoint  string                `json:"entrypoint"`
	Image       ImageSpec             `json:"image"`
	Runtime     RuntimeSpec           `json:"runtime"`
	Autoscaling AutoscalingSpec       `json:"autoscaling"`
	Security    SecuritySpec          `json:"security"`
	Secrets     []SecretMountSpec     `json:"secrets,omitempty"`
	Volumes     []VolumeMountSpec     `json:"volumes,omitempty"`
	Provider    ProviderPlacementSpec `json:"provider"`
	BuildID     string                `json:"buildId,omitempty"`
	CreatedAt   time.Time             `json:"createdAt"`
}

type InvocationResponse struct {
	ID             string                  `json:"id"`
	AppID          string                  `json:"appId"`
	FunctionID     string                  `json:"functionId"`
	RevisionID     string                  `json:"revisionId"`
	RunnerID       string                  `json:"runnerId,omitempty"`
	LeaseID        string                  `json:"leaseId,omitempty"`
	Mode           domain.InvocationMode   `json:"mode"`
	Status         domain.InvocationStatus `json:"status"`
	Payload        dbtype.JSONMap          `json:"payload,omitempty"`
	Result         dbtype.JSONMap          `json:"result,omitempty"`
	Error          string                  `json:"error,omitempty"`
	Attempt        int                     `json:"attempt"`
	MaxAttempts    int                     `json:"maxAttempts"`
	IdempotencyKey string                  `json:"idempotencyKey,omitempty"`
	LeaseExpiresAt *time.Time              `json:"leaseExpiresAt,omitempty"`
	StartedAt      *time.Time              `json:"startedAt,omitempty"`
	FinishedAt     *time.Time              `json:"finishedAt,omitempty"`
	CreatedAt      time.Time               `json:"createdAt"`
	ModifiedAt     time.Time               `json:"modifiedAt"`
}

type LogResponse struct {
	ID           string           `json:"id"`
	InvocationID string           `json:"invocationId"`
	Seq          int64            `json:"seq"`
	Stream       domain.LogStream `json:"stream"`
	Message      string           `json:"message"`
	Truncated    bool             `json:"truncated"`
	CreatedAt    time.Time        `json:"createdAt"`
}

type RunnerLeaseResponse struct {
	Invocation InvocationResponse `json:"invocation"`
	Revision   RevisionResponse   `json:"revision"`
	Build      *BuildResponse     `json:"build,omitempty"`
	Execution  ExecutionContract  `json:"execution"`
}

type ExecutionContract struct {
	Entrypoint       string            `json:"entrypoint"`
	ImageRef         string            `json:"imageRef"`
	SourceArchiveRef string            `json:"sourceArchiveRef,omitempty"`
	SourceDigest     string            `json:"sourceDigest,omitempty"`
	Payload          dbtype.JSONMap    `json:"payload,omitempty"`
	Runtime          RuntimeSpec       `json:"runtime"`
	TimeoutSecs      int               `json:"timeoutSecs"`
	Security         SecuritySpec      `json:"security"`
	SecretEnv        []SecretMountSpec `json:"secretEnv,omitempty"`
	Volumes          []VolumeMountSpec `json:"volumes,omitempty"`
}

type CompleteInvocationInput struct {
	AgentID      string                  `json:"agentId,omitempty"`
	SessionToken string                  `json:"sessionToken"`
	LeaseID      string                  `json:"leaseId"      binding:"required"`
	Status       domain.InvocationStatus `json:"status"       binding:"required,oneof=running succeeded failed timed_out canceled"`
	Result       map[string]any          `json:"result,omitempty"`
	Error        string                  `json:"error,omitempty" binding:"max=4000"`
}

func toAppResponse(app *domain.App) AppResponse {
	return AppResponse{
		ID:          app.ID,
		Name:        app.Name,
		Description: app.Description,
		CreatedAt:   app.CreatedAt,
		ModifiedAt:  app.ModifiedAt,
	}
}

func toFunctionResponse(fn *domain.Function) FunctionResponse {
	return FunctionResponse{
		ID:               fn.ID,
		AppID:            fn.AppID,
		Name:             fn.Name,
		Entrypoint:       fn.Entrypoint,
		Status:           fn.Status,
		ActiveRevisionID: fn.ActiveRevisionID,
		CreatedAt:        fn.CreatedAt,
		ModifiedAt:       fn.ModifiedAt,
	}
}

func toRevisionResponse(rev *domain.FunctionRevision) RevisionResponse {
	return RevisionResponse{
		ID:          rev.ID,
		AppID:       rev.AppID,
		FunctionID:  rev.FunctionID,
		Version:     rev.Version,
		Entrypoint:  rev.Entrypoint,
		Image:       rev.Image,
		Runtime:     rev.Runtime,
		Autoscaling: rev.Autoscaling,
		Security:    rev.Security,
		Secrets:     rev.Secrets,
		Volumes:     rev.Volumes,
		Provider:    rev.Provider,
		BuildID:     rev.BuildID,
		CreatedAt:   rev.CreatedAt,
	}
}

func toInvocationResponse(inv *domain.Invocation) InvocationResponse {
	return InvocationResponse{
		ID:             inv.ID,
		AppID:          inv.AppID,
		FunctionID:     inv.FunctionID,
		RevisionID:     inv.RevisionID,
		RunnerID:       inv.RunnerID,
		LeaseID:        inv.LeaseID,
		Mode:           inv.Mode,
		Status:         inv.Status,
		Payload:        inv.Payload,
		Result:         inv.Result,
		Error:          inv.Error,
		Attempt:        inv.Attempt,
		MaxAttempts:    inv.MaxAttempts,
		IdempotencyKey: inv.IdempotencyKey,
		LeaseExpiresAt: inv.LeaseExpiresAt,
		StartedAt:      inv.StartedAt,
		FinishedAt:     inv.FinishedAt,
		CreatedAt:      inv.CreatedAt,
		ModifiedAt:     inv.ModifiedAt,
	}
}

func toLogResponse(log *domain.InvocationLog) LogResponse {
	return LogResponse{
		ID:           log.ID,
		InvocationID: log.InvocationID,
		Seq:          log.Seq,
		Stream:       log.Stream,
		Message:      log.Message,
		Truncated:    log.Truncated,
		CreatedAt:    log.CreatedAt,
	}
}
