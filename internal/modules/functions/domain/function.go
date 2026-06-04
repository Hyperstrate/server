package domain

import (
	"context"
	"errors"
	"time"

	"hyperstrate/server/internal/shared/dbtype"
	"hyperstrate/server/internal/shared/pagination"
)

var (
	ErrAppNotFound               = errors.New("function app not found")
	ErrFunctionNotFound          = errors.New("function not found")
	ErrRevisionNotFound          = errors.New("function revision not found")
	ErrInvocationNotFound        = errors.New("function invocation not found")
	ErrNoInvocationAvailable     = errors.New("no function invocation available")
	ErrUnsupportedInvocationMode = errors.New("unsupported invocation mode")
	ErrInvalidFunctionSpec       = errors.New("invalid function specification")
)

type FunctionStatus string

const (
	FunctionStatusDeploying FunctionStatus = "deploying"
	FunctionStatusReady     FunctionStatus = "ready"
	FunctionStatusFailed    FunctionStatus = "failed"
)

type InvocationMode string

const (
	InvocationModeSync  InvocationMode = "sync"
	InvocationModeAsync InvocationMode = "async"
)

type InvocationStatus string

const (
	InvocationStatusQueued       InvocationStatus = "queued"
	InvocationStatusAssigned     InvocationStatus = "assigned"
	InvocationStatusStarting     InvocationStatus = "starting"
	InvocationStatusRunning      InvocationStatus = "running"
	InvocationStatusSucceeded    InvocationStatus = "succeeded"
	InvocationStatusFailed       InvocationStatus = "failed"
	InvocationStatusCanceled     InvocationStatus = "canceled"
	InvocationStatusTimedOut     InvocationStatus = "timed_out"
	InvocationStatusExpired      InvocationStatus = "expired"
	InvocationStatusRetrying     InvocationStatus = "retrying"
	InvocationStatusDeadLettered InvocationStatus = "dead_lettered"
)

type LogStream string

const (
	LogStreamStdout LogStream = "stdout"
	LogStreamStderr LogStream = "stderr"
	LogStreamSystem LogStream = "system"
)

type App struct {
	ID          string    `json:"id"          gorm:"primaryKey;size:50"`
	OrgID       string    `json:"-"           gorm:"size:50;not null;index"`
	Name        string    `json:"name"        gorm:"size:255;not null"`
	Description string    `json:"description" gorm:"type:text"`
	CreatedAt   time.Time `json:"createdAt"`
	ModifiedAt  time.Time `json:"modifiedAt"  gorm:"autoUpdateTime"`
}

func (App) TableName() string { return "function_apps" }

type Function struct {
	ID               string         `json:"id"               gorm:"primaryKey;size:50"`
	OrgID            string         `json:"-"                gorm:"size:50;not null;index"`
	AppID            string         `json:"appId"            gorm:"size:50;not null;index"`
	Name             string         `json:"name"             gorm:"size:255;not null"`
	Entrypoint       string         `json:"entrypoint"       gorm:"size:512;not null"`
	Status           FunctionStatus `json:"status"           gorm:"size:50;not null;default:deploying"`
	ActiveRevisionID string         `json:"activeRevisionId" gorm:"size:50;not null;default:''"`
	CreatedAt        time.Time      `json:"createdAt"`
	ModifiedAt       time.Time      `json:"modifiedAt"       gorm:"autoUpdateTime"`
}

func (Function) TableName() string { return "functions" }

type ImageSpec struct {
	Base     string   `json:"base"`
	Packages []string `json:"packages,omitempty"`
	Commands []string `json:"commands,omitempty"`
}

type RuntimeSpec struct {
	PythonVersion string `json:"pythonVersion,omitempty"`
	CPU           string `json:"cpu,omitempty"`
	MemoryMB      int    `json:"memoryMb,omitempty"`
	GPU           string `json:"gpu,omitempty"`
	TimeoutSecs   int    `json:"timeoutSecs,omitempty"`
}

type AutoscalingSpec struct {
	MinContainers      int `json:"minContainers"`
	MaxContainers      int `json:"maxContainers"`
	ScaleDownAfterSecs int `json:"scaleDownAfterSecs"`
	MaxConcurrency     int `json:"maxConcurrency"`
}

type SecuritySpec struct {
	Sandbox            string   `json:"sandbox"`
	NetworkPolicy      string   `json:"networkPolicy"`
	AllowOutboundHosts []string `json:"allowOutboundHosts,omitempty"`
	RunAsNonRoot       bool     `json:"runAsNonRoot"`
	ReadOnlyRootFS     bool     `json:"readOnlyRootFs"`
}

type SecretMountSpec struct {
	Name string `json:"name"`
	Env  string `json:"env"`
}

type VolumeMountSpec struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
	ReadOnly  bool   `json:"readOnly"`
}

type ProviderPlacementSpec struct {
	Strategy         string   `json:"strategy"`
	AllowedProviders []string `json:"allowedProviders,omitempty"`
	Regions          []string `json:"regions,omitempty"`
}

type RunnerSelector struct {
	Provider     string
	Region       string
	Capabilities dbtype.JSONMap
}

type FunctionRevision struct {
	ID          string                `json:"id"         gorm:"primaryKey;size:50"`
	OrgID       string                `json:"-"          gorm:"size:50;not null;index"`
	AppID       string                `json:"appId"      gorm:"size:50;not null;index"`
	FunctionID  string                `json:"functionId" gorm:"size:50;not null;index"`
	Version     int                   `json:"version"    gorm:"not null;default:1"`
	Entrypoint  string                `json:"entrypoint" gorm:"size:512;not null"`
	Image       ImageSpec             `json:"image"      gorm:"serializer:json;column:image"`
	Runtime     RuntimeSpec           `json:"runtime"    gorm:"serializer:json;column:runtime"`
	Autoscaling AutoscalingSpec       `json:"autoscaling" gorm:"serializer:json;column:autoscaling"`
	Security    SecuritySpec          `json:"security"    gorm:"serializer:json;column:security"`
	Secrets     []SecretMountSpec     `json:"secrets"     gorm:"serializer:json;column:secrets"`
	Volumes     []VolumeMountSpec     `json:"volumes"     gorm:"serializer:json;column:volumes"`
	Provider    ProviderPlacementSpec `json:"provider"    gorm:"serializer:json;column:provider"`
	BuildID     string                `json:"buildId,omitempty" gorm:"size:50;not null;default:''"`
	CreatedAt   time.Time             `json:"createdAt"`
}

func (FunctionRevision) TableName() string { return "function_revisions" }

type Invocation struct {
	ID             string           `json:"id"         gorm:"primaryKey;size:50"`
	OrgID          string           `json:"-"          gorm:"size:50;not null;index"`
	AppID          string           `json:"appId"      gorm:"size:50;not null;index"`
	FunctionID     string           `json:"functionId" gorm:"size:50;not null;index"`
	RevisionID     string           `json:"revisionId" gorm:"size:50;not null;index"`
	RunnerID       string           `json:"runnerId,omitempty" gorm:"size:50;not null;default:''"`
	LeaseID        string           `json:"leaseId,omitempty"  gorm:"size:100;not null;default:''"`
	Mode           InvocationMode   `json:"mode"       gorm:"size:50;not null;default:sync"`
	Status         InvocationStatus `json:"status"     gorm:"size:50;not null;default:queued"`
	Payload        dbtype.JSONMap   `json:"payload"    gorm:"serializer:json;column:payload"`
	Result         dbtype.JSONMap   `json:"result,omitempty" gorm:"serializer:json;column:result"`
	Error          string           `json:"error,omitempty" gorm:"type:text"`
	Attempt        int              `json:"attempt"    gorm:"not null;default:0"`
	MaxAttempts    int              `json:"maxAttempts" gorm:"not null;default:1"`
	IdempotencyKey string           `json:"idempotencyKey,omitempty" gorm:"size:255;not null;default:''"`
	LeaseExpiresAt *time.Time       `json:"leaseExpiresAt,omitempty"`
	StartedAt      *time.Time       `json:"startedAt,omitempty"`
	FinishedAt     *time.Time       `json:"finishedAt,omitempty"`
	CreatedAt      time.Time        `json:"createdAt"`
	ModifiedAt     time.Time        `json:"modifiedAt" gorm:"autoUpdateTime"`
}

func (Invocation) TableName() string { return "function_invocations" }

type InvocationLog struct {
	ID           string    `json:"id"           gorm:"primaryKey;size:50"`
	OrgID        string    `json:"-"            gorm:"size:50;not null;index"`
	InvocationID string    `json:"invocationId" gorm:"size:50;not null;index"`
	Seq          int64     `json:"seq"          gorm:"not null;default:0;index"`
	Stream       LogStream `json:"stream"       gorm:"size:50;not null"`
	Message      string    `json:"message"      gorm:"type:text;not null"`
	Truncated    bool      `json:"truncated"    gorm:"not null;default:false"`
	CreatedAt    time.Time `json:"createdAt"`
}

func (InvocationLog) TableName() string { return "function_invocation_logs" }

type AppRepository interface {
	Create(ctx context.Context, app *App) error
	FindByID(ctx context.Context, orgID, id string) (*App, error)
	ListByOrg(ctx context.Context, orgID string, slice pagination.Slice) ([]App, int64, error)
}

type FunctionRepository interface {
	Create(ctx context.Context, fn *Function) error
	CreateWithRevision(ctx context.Context, fn *Function, rev *FunctionRevision) error
	FindByID(ctx context.Context, orgID, id string) (*Function, error)
	ListByApp(ctx context.Context, orgID, appID string, slice pagination.Slice) ([]Function, int64, error)
	Update(ctx context.Context, fn *Function) error
}

type RevisionRepository interface {
	Create(ctx context.Context, rev *FunctionRevision) error
	FindByID(ctx context.Context, orgID, id string) (*FunctionRevision, error)
	ListByFunction(ctx context.Context, orgID, functionID string, slice pagination.Slice) ([]FunctionRevision, int64, error)
	SetBuildID(ctx context.Context, orgID, revisionID, buildID string) error
}

type InvocationRepository interface {
	Create(ctx context.Context, inv *Invocation) error
	FindByID(ctx context.Context, orgID, id string) (*Invocation, error)
	FindByIdempotencyKey(ctx context.Context, orgID, functionID, key string) (*Invocation, error)
	ListByFunction(ctx context.Context, orgID, functionID string, slice pagination.Slice) ([]Invocation, int64, error)
	LeaseNextQueued(ctx context.Context, orgID, runnerID, leaseID string, leaseExpiresAt time.Time, selector RunnerSelector) (*Invocation, error)
	Complete(ctx context.Context, orgID, invocationID, runnerID, leaseID string, status InvocationStatus, result dbtype.JSONMap, errorMessage string, finishedAt time.Time) (*Invocation, error)
}

type LogRepository interface {
	Append(ctx context.Context, log *InvocationLog) error
	ListByInvocationID(ctx context.Context, orgID, invocationID string, slice pagination.Slice) ([]InvocationLog, int64, error)
}

func RevisionMatchesRunner(rev *FunctionRevision, selector RunnerSelector) bool {
	if len(rev.Provider.AllowedProviders) > 0 && !stringIn(selector.Provider, rev.Provider.AllowedProviders) {
		return false
	}
	if len(rev.Provider.Regions) > 0 && !stringIn(selector.Region, rev.Provider.Regions) {
		return false
	}
	if rev.Runtime.GPU != "" && rev.Runtime.GPU != "none" && capabilityString(selector.Capabilities, "gpu") != rev.Runtime.GPU {
		return false
	}
	if rev.Runtime.PythonVersion != "" {
		runtime := capabilityString(selector.Capabilities, "runtime")
		python := capabilityString(selector.Capabilities, "pythonVersion")
		pythonRuntime := "python" + rev.Runtime.PythonVersion
		if runtime != rev.Runtime.PythonVersion && runtime != pythonRuntime && python != rev.Runtime.PythonVersion {
			return false
		}
	}
	if rev.Security.Sandbox != "" && capabilityString(selector.Capabilities, "sandbox") != rev.Security.Sandbox {
		return false
	}
	return true
}

func capabilityString(caps dbtype.JSONMap, key string) string {
	if caps == nil {
		return ""
	}
	v, _ := caps[key].(string)
	return v
}

func stringIn(value string, values []string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
