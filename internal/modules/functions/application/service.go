package application

import (
	"context"
	"errors"
	"strings"

	authDomain "hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/modules/functions/domain"
	"hyperstrate/server/internal/shared/dbtype"
	"hyperstrate/server/internal/shared/pagination"

	"go.jetify.com/typeid/v2"
)

type Service interface {
	CreateApp(ctx context.Context, input CreateAppInput) (*AppResponse, error)
	DeployFunction(ctx context.Context, appID string, input DeployFunctionInput) (*FunctionResponse, error)
	InvokeFunction(ctx context.Context, functionID string, input InvokeFunctionInput) (*InvocationResponse, error)
	GetInvocation(ctx context.Context, invocationID string) (*InvocationResponse, error)
	ListInvocationLogs(ctx context.Context, invocationID string, slice pagination.Slice) (pagination.Paginated[LogResponse], error)
	AppendInvocationLog(ctx context.Context, invocationID string, input AppendLogInput) (*LogResponse, error)
}

type service struct {
	apps        domain.AppRepository
	functions   domain.FunctionRepository
	revisions   domain.RevisionRepository
	invocations domain.InvocationRepository
	logs        domain.LogRepository
}

func NewService(
	apps domain.AppRepository,
	functions domain.FunctionRepository,
	revisions domain.RevisionRepository,
	invocations domain.InvocationRepository,
	logs domain.LogRepository,
) Service {
	return &service{
		apps:        apps,
		functions:   functions,
		revisions:   revisions,
		invocations: invocations,
		logs:        logs,
	}
}

func (s *service) CreateApp(ctx context.Context, input CreateAppInput) (*AppResponse, error) {
	app := &domain.App{
		ID:          typeid.MustGenerate("fapp").String(),
		OrgID:       authDomain.OrgIDFromContext(ctx),
		Name:        input.Name,
		Description: input.Description,
	}
	if err := s.apps.Create(ctx, app); err != nil {
		return nil, err
	}
	resp := toAppResponse(app)
	return &resp, nil
}

func (s *service) DeployFunction(ctx context.Context, appID string, input DeployFunctionInput) (*FunctionResponse, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	normalized, err := normalizeDeployFunctionInput(input)
	if err != nil {
		return nil, err
	}
	app, err := s.apps.FindByID(ctx, orgID, appID)
	if err != nil {
		return nil, err
	}

	fn := &domain.Function{
		ID:               typeid.MustGenerate("fn").String(),
		OrgID:            orgID,
		AppID:            app.ID,
		Name:             normalized.Name,
		Entrypoint:       normalized.Entrypoint,
		Status:           domain.FunctionStatusDeploying,
		ActiveRevisionID: typeid.MustGenerate("frev").String(),
	}

	rev := &domain.FunctionRevision{
		ID:          fn.ActiveRevisionID,
		OrgID:       orgID,
		AppID:       app.ID,
		FunctionID:  fn.ID,
		Version:     1,
		Entrypoint:  normalized.Entrypoint,
		Image:       domain.ImageSpec(normalized.Image),
		Runtime:     domain.RuntimeSpec(normalized.Runtime),
		Autoscaling: domain.AutoscalingSpec(normalized.Autoscaling),
		Security:    domain.SecuritySpec(normalized.Security),
		Secrets:     secretMounts(normalized.Secrets),
		Volumes:     volumeMounts(normalized.Volumes),
		Provider:    domain.ProviderPlacementSpec(normalized.Provider),
	}
	if err := s.functions.CreateWithRevision(ctx, fn, rev); err != nil {
		return nil, err
	}

	resp := toFunctionResponse(fn)
	return &resp, nil
}

func normalizeDeployFunctionInput(input DeployFunctionInput) (DeployFunctionInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Entrypoint = strings.TrimSpace(input.Entrypoint)
	input.Image.Base = strings.TrimSpace(input.Image.Base)
	if input.Name == "" || input.Entrypoint == "" || input.Image.Base == "" {
		return input, domain.ErrInvalidFunctionSpec
	}
	if input.Runtime.PythonVersion == "" {
		input.Runtime.PythonVersion = "3.12"
	}
	if input.Runtime.TimeoutSecs <= 0 {
		input.Runtime.TimeoutSecs = 300
	}
	if input.Runtime.MemoryMB < 0 || input.Autoscaling.MinContainers < 0 || input.Autoscaling.MaxContainers < 0 {
		return input, domain.ErrInvalidFunctionSpec
	}
	if input.Autoscaling.MaxContainers == 0 {
		input.Autoscaling.MaxContainers = 1
	}
	if input.Autoscaling.MaxContainers < input.Autoscaling.MinContainers {
		return input, domain.ErrInvalidFunctionSpec
	}
	if input.Autoscaling.ScaleDownAfterSecs <= 0 {
		input.Autoscaling.ScaleDownAfterSecs = 60
	}
	if input.Autoscaling.MaxConcurrency <= 0 {
		input.Autoscaling.MaxConcurrency = 1
	}
	if input.Security.Sandbox == "" {
		input.Security.Sandbox = "container"
	}
	if input.Security.Sandbox != "container" && input.Security.Sandbox != "gvisor" && input.Security.Sandbox != "kata" && input.Security.Sandbox != "firecracker" {
		return input, domain.ErrInvalidFunctionSpec
	}
	if input.Security.NetworkPolicy == "" {
		input.Security.NetworkPolicy = "deny_all"
	}
	if input.Security.NetworkPolicy != "deny_all" && input.Security.NetworkPolicy != "restricted" && input.Security.NetworkPolicy != "allow_all" {
		return input, domain.ErrInvalidFunctionSpec
	}
	input.Security.RunAsNonRoot = true
	input.Security.ReadOnlyRootFS = true
	if input.Provider.Strategy == "" {
		input.Provider.Strategy = "portable"
	}
	return input, nil
}

func (s *service) InvokeFunction(ctx context.Context, functionID string, input InvokeFunctionInput) (*InvocationResponse, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	fn, err := s.functions.FindByID(ctx, orgID, functionID)
	if err != nil {
		return nil, err
	}
	if _, err := s.revisions.FindByID(ctx, orgID, fn.ActiveRevisionID); err != nil {
		return nil, err
	}
	if input.IdempotencyKey != "" {
		existing, err := s.invocations.FindByIdempotencyKey(ctx, orgID, fn.ID, input.IdempotencyKey)
		if err == nil {
			resp := toInvocationResponse(existing)
			return &resp, nil
		}
		if !errors.Is(err, domain.ErrInvocationNotFound) {
			return nil, err
		}
	}

	mode := input.Mode
	if mode == "" {
		mode = domain.InvocationModeAsync
	}
	if mode != domain.InvocationModeAsync {
		return nil, domain.ErrUnsupportedInvocationMode
	}
	inv := &domain.Invocation{
		ID:             typeid.MustGenerate("finv").String(),
		OrgID:          orgID,
		AppID:          fn.AppID,
		FunctionID:     fn.ID,
		RevisionID:     fn.ActiveRevisionID,
		Mode:           mode,
		Status:         domain.InvocationStatusQueued,
		Payload:        dbtype.JSONMap(input.Payload),
		Attempt:        0,
		MaxAttempts:    maxAttemptsOrDefault(input.MaxAttempts),
		IdempotencyKey: input.IdempotencyKey,
	}
	if inv.Payload == nil {
		inv.Payload = dbtype.JSONMap{}
	}
	if err := s.invocations.Create(ctx, inv); err != nil {
		return nil, err
	}
	resp := toInvocationResponse(inv)
	return &resp, nil
}

func (s *service) AppendInvocationLog(ctx context.Context, invocationID string, input AppendLogInput) (*LogResponse, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	inv, err := s.invocations.FindByID(ctx, orgID, invocationID)
	if err != nil {
		return nil, err
	}
	log := &domain.InvocationLog{
		ID:           typeid.MustGenerate("flog").String(),
		OrgID:        orgID,
		InvocationID: inv.ID,
		Seq:          input.Seq,
		Stream:       input.Stream,
		Message:      input.Message,
		Truncated:    input.Truncated,
	}
	if err := s.logs.Append(ctx, log); err != nil {
		return nil, err
	}
	resp := toLogResponse(log)
	return &resp, nil
}

func (s *service) GetInvocation(ctx context.Context, invocationID string) (*InvocationResponse, error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	inv, err := s.invocations.FindByID(ctx, orgID, invocationID)
	if err != nil {
		return nil, err
	}
	resp := toInvocationResponse(inv)
	return &resp, nil
}

func (s *service) ListInvocationLogs(ctx context.Context, invocationID string, slice pagination.Slice) (pagination.Paginated[LogResponse], error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	inv, err := s.invocations.FindByID(ctx, orgID, invocationID)
	if err != nil {
		return pagination.Paginated[LogResponse]{}, err
	}
	logs, total, err := s.logs.ListByInvocationID(ctx, orgID, inv.ID, slice)
	if err != nil {
		return pagination.Paginated[LogResponse]{}, err
	}
	resp := make([]LogResponse, 0, len(logs))
	for i := range logs {
		resp = append(resp, toLogResponse(&logs[i]))
	}
	return pagination.New(resp, total, slice), nil
}

func maxAttemptsOrDefault(maxAttempts int) int {
	if maxAttempts <= 0 {
		return 1
	}
	return maxAttempts
}

func secretMounts(in []SecretMountSpec) []domain.SecretMountSpec {
	out := make([]domain.SecretMountSpec, len(in))
	for i, item := range in {
		out[i] = domain.SecretMountSpec(item)
	}
	return out
}

func volumeMounts(in []VolumeMountSpec) []domain.VolumeMountSpec {
	out := make([]domain.VolumeMountSpec, len(in))
	for i, item := range in {
		out[i] = domain.VolumeMountSpec(item)
	}
	return out
}
