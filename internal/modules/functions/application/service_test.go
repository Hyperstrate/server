package application_test

import (
	"context"
	"sort"
	"testing"

	authDomain "hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/modules/functions/application"
	"hyperstrate/server/internal/modules/functions/domain"
	"hyperstrate/server/internal/shared/pagination"
)

const (
	testOrgID = "org_functions_test"
)

func functionsCtx() context.Context {
	return authDomain.WithOrgID(context.Background(), testOrgID)
}

func TestServiceCreatesAppFromOrgContext(t *testing.T) {
	repos := newMemoryRepos()
	svc := application.NewService(repos.Apps, repos.Functions, repos.Revisions, repos.Builds, repos.Invocations, repos.Logs)

	app, err := svc.CreateApp(functionsCtx(), application.CreateAppInput{
		Name:        "image-pipeline",
		Description: "Modal-style Python app",
	})
	if err != nil {
		t.Fatalf("CreateApp returned error: %v", err)
	}
	if app.ID == "" {
		t.Fatal("expected generated app id")
	}
	if app.OrgID != "" {
		t.Fatalf("response leaked org id %q", app.OrgID)
	}

	stored := repos.Apps.byID[app.ID]
	if stored == nil {
		t.Fatal("expected app to be persisted")
	}
	if stored.OrgID != testOrgID {
		t.Fatalf("expected app org %q, got %q", testOrgID, stored.OrgID)
	}
	if stored.Name != "image-pipeline" {
		t.Fatalf("expected app name to persist, got %q", stored.Name)
	}
}

func TestDeployFunctionCreatesRevisionWithRuntimeSecurityAndAutoscaleContract(t *testing.T) {
	repos := newMemoryRepos()
	svc := application.NewService(repos.Apps, repos.Functions, repos.Revisions, repos.Builds, repos.Invocations, repos.Logs)
	ctx := functionsCtx()

	app, err := svc.CreateApp(ctx, application.CreateAppInput{Name: "agents"})
	if err != nil {
		t.Fatalf("CreateApp returned error: %v", err)
	}

	fn, err := svc.DeployFunction(ctx, app.ID, application.DeployFunctionInput{
		Name:        "summarize",
		Entrypoint:  "app.summarize",
		Image:       application.ImageSpec{Base: "python:3.12-slim", Packages: []string{"pydantic", "httpx"}},
		Runtime:     application.RuntimeSpec{PythonVersion: "3.12", CPU: "2", MemoryMB: 4096, GPU: "H100", TimeoutSecs: 600},
		Autoscaling: application.AutoscalingSpec{MinContainers: 0, MaxContainers: 20, ScaleDownAfterSecs: 120, MaxConcurrency: 8},
		Security: application.SecuritySpec{
			Sandbox:            "gvisor",
			NetworkPolicy:      "restricted",
			AllowOutboundHosts: []string{"api.openai.com"},
			RunAsNonRoot:       true,
			ReadOnlyRootFS:     true,
		},
		Secrets:  []application.SecretMountSpec{{Name: "openai-key", Env: "OPENAI_API_KEY"}},
		Volumes:  []application.VolumeMountSpec{{Name: "model-cache", MountPath: "/models", ReadOnly: false}},
		Provider: application.ProviderPlacementSpec{Strategy: "portable", AllowedProviders: []string{"aws", "coreweave", "runpod"}},
	})
	if err != nil {
		t.Fatalf("DeployFunction returned error: %v", err)
	}
	if fn.ActiveRevisionID == "" {
		t.Fatal("expected active revision id")
	}
	if fn.Status != domain.FunctionStatusDeploying {
		t.Fatalf("expected function status deploying, got %q", fn.Status)
	}

	rev := repos.Revisions.byID[fn.ActiveRevisionID]
	if rev == nil {
		t.Fatal("expected function revision to be persisted")
	}
	if rev.OrgID != testOrgID {
		t.Fatalf("expected revision org %q, got %q", testOrgID, rev.OrgID)
	}
	if rev.AppID != app.ID || rev.FunctionID != fn.ID {
		t.Fatalf("revision linkage mismatch: app=%q function=%q", rev.AppID, rev.FunctionID)
	}
	if rev.Image.Base != "python:3.12-slim" || len(rev.Image.Packages) != 2 {
		t.Fatalf("image spec was not preserved: %+v", rev.Image)
	}
	if rev.Runtime.GPU != "H100" || rev.Runtime.TimeoutSecs != 600 {
		t.Fatalf("runtime spec was not preserved: %+v", rev.Runtime)
	}
	if rev.Autoscaling.MinContainers != 0 || rev.Autoscaling.MaxContainers != 20 || rev.Autoscaling.ScaleDownAfterSecs != 120 {
		t.Fatalf("autoscaling spec was not preserved: %+v", rev.Autoscaling)
	}
	if rev.Security.Sandbox != "gvisor" || !rev.Security.RunAsNonRoot || !rev.Security.ReadOnlyRootFS {
		t.Fatalf("security spec was not preserved: %+v", rev.Security)
	}
	if len(rev.Secrets) != 1 || rev.Secrets[0].Env != "OPENAI_API_KEY" {
		t.Fatalf("secret mounts were not preserved: %+v", rev.Secrets)
	}
	if len(rev.Volumes) != 1 || rev.Volumes[0].MountPath != "/models" {
		t.Fatalf("volume mounts were not preserved: %+v", rev.Volumes)
	}
	if rev.Provider.Strategy != "portable" || len(rev.Provider.AllowedProviders) != 3 {
		t.Fatalf("provider placement was not preserved: %+v", rev.Provider)
	}
}

func TestDeployFunctionRejectsMissingImageBase(t *testing.T) {
	repos := newMemoryRepos()
	svc := application.NewService(repos.Apps, repos.Functions, repos.Revisions, repos.Builds, repos.Invocations, repos.Logs)
	ctx := functionsCtx()
	app, err := svc.CreateApp(ctx, application.CreateAppInput{Name: "invalid"})
	if err != nil {
		t.Fatalf("CreateApp returned error: %v", err)
	}
	if _, err := svc.DeployFunction(ctx, app.ID, application.DeployFunctionInput{
		Name:       "bad",
		Entrypoint: "main.bad",
	}); err != domain.ErrInvalidFunctionSpec {
		t.Fatalf("expected ErrInvalidFunctionSpec, got %v", err)
	}
}

func TestDeployFunctionAppliesSecurePortableDefaults(t *testing.T) {
	repos := newMemoryRepos()
	svc := application.NewService(repos.Apps, repos.Functions, repos.Revisions, repos.Builds, repos.Invocations, repos.Logs)
	ctx := functionsCtx()
	app, err := svc.CreateApp(ctx, application.CreateAppInput{Name: "defaults"})
	if err != nil {
		t.Fatalf("CreateApp returned error: %v", err)
	}
	fn, err := svc.DeployFunction(ctx, app.ID, application.DeployFunctionInput{
		Name:       "defaulted",
		Entrypoint: "main.handle",
		Image:      application.ImageSpec{Base: "python:3.12-slim"},
	})
	if err != nil {
		t.Fatalf("DeployFunction returned error: %v", err)
	}
	rev := repos.Revisions.byID[fn.ActiveRevisionID]
	if rev.Runtime.PythonVersion != "3.12" || rev.Runtime.TimeoutSecs != 300 {
		t.Fatalf("runtime defaults = %+v", rev.Runtime)
	}
	if rev.Autoscaling.MinContainers != 0 || rev.Autoscaling.MaxContainers != 1 || rev.Autoscaling.ScaleDownAfterSecs != 60 || rev.Autoscaling.MaxConcurrency != 1 {
		t.Fatalf("autoscaling defaults = %+v", rev.Autoscaling)
	}
	if rev.Security.Sandbox != "container" || rev.Security.NetworkPolicy != "deny_all" || !rev.Security.RunAsNonRoot || !rev.Security.ReadOnlyRootFS {
		t.Fatalf("security defaults = %+v", rev.Security)
	}
	if rev.Provider.Strategy != "portable" {
		t.Fatalf("provider defaults = %+v", rev.Provider)
	}
}

func TestInvokeFunctionPersistsQueuedInvocationWithPayload(t *testing.T) {
	repos := newMemoryRepos()
	svc := application.NewService(repos.Apps, repos.Functions, repos.Revisions, repos.Builds, repos.Invocations, repos.Logs)
	ctx := functionsCtx()
	app, err := svc.CreateApp(ctx, application.CreateAppInput{Name: "workers"})
	if err != nil {
		t.Fatalf("CreateApp returned error: %v", err)
	}
	fn, err := svc.DeployFunction(ctx, app.ID, application.DeployFunctionInput{
		Name:        "parse",
		Entrypoint:  "tasks.parse",
		Image:       application.ImageSpec{Base: "python:3.12-slim"},
		Runtime:     application.RuntimeSpec{PythonVersion: "3.12", TimeoutSecs: 60},
		Autoscaling: application.AutoscalingSpec{MinContainers: 0, MaxContainers: 5, ScaleDownAfterSecs: 60, MaxConcurrency: 1},
		Security:    application.SecuritySpec{Sandbox: "container", NetworkPolicy: "deny_all", RunAsNonRoot: true, ReadOnlyRootFS: true},
	})
	if err != nil {
		t.Fatalf("DeployFunction returned error: %v", err)
	}

	inv, err := svc.InvokeFunction(ctx, fn.ID, application.InvokeFunctionInput{
		Mode:    domain.InvocationModeAsync,
		Payload: map[string]any{"text": "hello"},
	})
	if err != nil {
		t.Fatalf("InvokeFunction returned error: %v", err)
	}
	if inv.Status != domain.InvocationStatusQueued {
		t.Fatalf("expected queued invocation, got %q", inv.Status)
	}
	if inv.RevisionID != fn.ActiveRevisionID {
		t.Fatalf("expected active revision %q, got %q", fn.ActiveRevisionID, inv.RevisionID)
	}

	stored := repos.Invocations.byID[inv.ID]
	if stored == nil {
		t.Fatal("expected invocation to be persisted")
	}
	if stored.OrgID != testOrgID {
		t.Fatalf("expected invocation org %q, got %q", testOrgID, stored.OrgID)
	}
	if stored.Payload["text"] != "hello" {
		t.Fatalf("expected payload to persist, got %+v", stored.Payload)
	}
	if stored.Attempt != 0 || stored.MaxAttempts != 1 {
		t.Fatalf("expected first queued invocation attempt defaults, got attempt=%d max=%d", stored.Attempt, stored.MaxAttempts)
	}
}

func TestInvokeFunctionReturnsExistingInvocationForIdempotencyKey(t *testing.T) {
	repos := newMemoryRepos()
	svc := application.NewService(repos.Apps, repos.Functions, repos.Revisions, repos.Builds, repos.Invocations, repos.Logs)
	ctx := functionsCtx()
	app, err := svc.CreateApp(ctx, application.CreateAppInput{Name: "workers"})
	if err != nil {
		t.Fatalf("CreateApp returned error: %v", err)
	}
	fn, err := svc.DeployFunction(ctx, app.ID, application.DeployFunctionInput{
		Name:       "parse",
		Entrypoint: "tasks.parse",
		Image:      application.ImageSpec{Base: "python:3.12-slim"},
	})
	if err != nil {
		t.Fatalf("DeployFunction returned error: %v", err)
	}

	first, err := svc.InvokeFunction(ctx, fn.ID, application.InvokeFunctionInput{
		Payload:        map[string]any{"text": "first"},
		IdempotencyKey: "client-job-1",
	})
	if err != nil {
		t.Fatalf("first InvokeFunction returned error: %v", err)
	}
	second, err := svc.InvokeFunction(ctx, fn.ID, application.InvokeFunctionInput{
		Payload:        map[string]any{"text": "second"},
		IdempotencyKey: "client-job-1",
	})
	if err != nil {
		t.Fatalf("second InvokeFunction returned error: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected existing invocation %q, got %q", first.ID, second.ID)
	}
	if len(repos.Invocations.byID) != 1 {
		t.Fatalf("expected one stored invocation, got %d", len(repos.Invocations.byID))
	}
	if second.Payload["text"] != "first" {
		t.Fatalf("expected original payload to be preserved, got %+v", second.Payload)
	}
}

func TestGetInvocationScopesToOwningOrg(t *testing.T) {
	repos := newMemoryRepos()
	svc := application.NewService(repos.Apps, repos.Functions, repos.Revisions, repos.Builds, repos.Invocations, repos.Logs)
	ctx := functionsCtx()
	app, err := svc.CreateApp(ctx, application.CreateAppInput{Name: "workers"})
	if err != nil {
		t.Fatalf("CreateApp returned error: %v", err)
	}
	fn, err := svc.DeployFunction(ctx, app.ID, application.DeployFunctionInput{
		Name:       "parse",
		Entrypoint: "tasks.parse",
		Image:      application.ImageSpec{Base: "python:3.12-slim"},
	})
	if err != nil {
		t.Fatalf("DeployFunction returned error: %v", err)
	}
	inv, err := svc.InvokeFunction(ctx, fn.ID, application.InvokeFunctionInput{})
	if err != nil {
		t.Fatalf("InvokeFunction returned error: %v", err)
	}

	found, err := svc.GetInvocation(ctx, inv.ID)
	if err != nil {
		t.Fatalf("GetInvocation returned error: %v", err)
	}
	if found.ID != inv.ID || found.FunctionID != fn.ID {
		t.Fatalf("unexpected invocation response: %+v", found)
	}

	otherOrgCtx := authDomain.WithOrgID(context.Background(), "org_other")
	if _, err := svc.GetInvocation(otherOrgCtx, inv.ID); err != domain.ErrInvocationNotFound {
		t.Fatalf("expected ErrInvocationNotFound for other org, got %v", err)
	}
}

func TestServiceReadListsScopeToOrgAndPaginate(t *testing.T) {
	repos := newMemoryRepos()
	svc := application.NewService(repos.Apps, repos.Functions, repos.Revisions, repos.Builds, repos.Invocations, repos.Logs)
	ctx := functionsCtx()
	app, err := svc.CreateApp(ctx, application.CreateAppInput{Name: "control-plane"})
	if err != nil {
		t.Fatalf("CreateApp returned error: %v", err)
	}
	fn, err := svc.DeployFunction(ctx, app.ID, application.DeployFunctionInput{
		Name:       "parse",
		Entrypoint: "tasks.parse",
		Image:      application.ImageSpec{Base: "python:3.12-slim"},
	})
	if err != nil {
		t.Fatalf("DeployFunction returned error: %v", err)
	}
	build := &domain.FunctionBuild{
		ID:         "fbld_service",
		OrgID:      testOrgID,
		AppID:      app.ID,
		FunctionID: fn.ID,
		RevisionID: fn.ActiveRevisionID,
		Status:     domain.BuildStatusSucceeded,
		Artifact:   domain.BuildArtifactSpec{ImageRef: "registry.example.com/parse:latest"},
	}
	if err := repos.Builds.Create(ctx, build); err != nil {
		t.Fatalf("create build: %v", err)
	}
	if err := repos.Revisions.SetBuildID(ctx, testOrgID, fn.ActiveRevisionID, build.ID); err != nil {
		t.Fatalf("set build id: %v", err)
	}
	if _, err := svc.InvokeFunction(ctx, fn.ID, application.InvokeFunctionInput{Payload: map[string]any{"text": "one"}}); err != nil {
		t.Fatalf("InvokeFunction one returned error: %v", err)
	}
	if _, err := svc.InvokeFunction(ctx, fn.ID, application.InvokeFunctionInput{Payload: map[string]any{"text": "two"}}); err != nil {
		t.Fatalf("InvokeFunction two returned error: %v", err)
	}

	apps, err := svc.ListApps(ctx, pagination.Slice{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("ListApps returned error: %v", err)
	}
	if apps.Meta.Total != 1 || len(apps.Items) != 1 || apps.Items[0].ID != app.ID {
		t.Fatalf("unexpected apps page: %+v", apps)
	}
	functions, err := svc.ListFunctions(ctx, app.ID, pagination.Slice{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("ListFunctions returned error: %v", err)
	}
	if functions.Meta.Total != 1 || len(functions.Items) != 1 || functions.Items[0].ID != fn.ID {
		t.Fatalf("unexpected functions page: %+v", functions)
	}
	revisions, err := svc.ListFunctionRevisions(ctx, fn.ID, pagination.Slice{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("ListFunctionRevisions returned error: %v", err)
	}
	if revisions.Meta.Total != 1 || len(revisions.Items) != 1 || revisions.Items[0].Build == nil || revisions.Items[0].Build.Artifact.ImageRef == "" {
		t.Fatalf("unexpected revisions page: %+v", revisions)
	}
	invocations, err := svc.ListFunctionInvocations(ctx, fn.ID, pagination.Slice{Page: 2, PerPage: 1})
	if err != nil {
		t.Fatalf("ListFunctionInvocations returned error: %v", err)
	}
	if invocations.Meta.Total != 2 || invocations.Meta.Page != 2 || invocations.Meta.Count != 1 {
		t.Fatalf("unexpected invocations page: %+v", invocations)
	}

	otherOrgCtx := authDomain.WithOrgID(context.Background(), "org_other")
	if _, err := svc.ListFunctions(otherOrgCtx, app.ID, pagination.Slice{Page: 1, PerPage: 10}); err != domain.ErrAppNotFound {
		t.Fatalf("expected ErrAppNotFound for other org functions, got %v", err)
	}
	if _, err := svc.ListFunctionRevisions(otherOrgCtx, fn.ID, pagination.Slice{Page: 1, PerPage: 10}); err != domain.ErrFunctionNotFound {
		t.Fatalf("expected ErrFunctionNotFound for other org revisions, got %v", err)
	}
}

func TestInvokeFunctionRejectsSyncUntilActivatorWaitPathExists(t *testing.T) {
	repos := newMemoryRepos()
	svc := application.NewService(repos.Apps, repos.Functions, repos.Revisions, repos.Builds, repos.Invocations, repos.Logs)
	ctx := functionsCtx()
	app, err := svc.CreateApp(ctx, application.CreateAppInput{Name: "workers"})
	if err != nil {
		t.Fatalf("CreateApp returned error: %v", err)
	}
	fn, err := svc.DeployFunction(ctx, app.ID, application.DeployFunctionInput{
		Name:       "parse",
		Entrypoint: "tasks.parse",
		Image:      application.ImageSpec{Base: "python:3.12-slim"},
	})
	if err != nil {
		t.Fatalf("DeployFunction returned error: %v", err)
	}
	if _, err := svc.InvokeFunction(ctx, fn.ID, application.InvokeFunctionInput{Mode: domain.InvocationModeSync}); err != domain.ErrUnsupportedInvocationMode {
		t.Fatalf("expected ErrUnsupportedInvocationMode, got %v", err)
	}
}

func TestAppendInvocationLogScopesToOwningOrg(t *testing.T) {
	repos := newMemoryRepos()
	svc := application.NewService(repos.Apps, repos.Functions, repos.Revisions, repos.Builds, repos.Invocations, repos.Logs)
	ctx := functionsCtx()
	app, err := svc.CreateApp(ctx, application.CreateAppInput{Name: "logs"})
	if err != nil {
		t.Fatalf("CreateApp returned error: %v", err)
	}
	fn, err := svc.DeployFunction(ctx, app.ID, application.DeployFunctionInput{
		Name:        "emit",
		Entrypoint:  "main.emit",
		Image:       application.ImageSpec{Base: "python:3.12-slim"},
		Runtime:     application.RuntimeSpec{PythonVersion: "3.12", TimeoutSecs: 30},
		Autoscaling: application.AutoscalingSpec{MinContainers: 0, MaxContainers: 2, ScaleDownAfterSecs: 30},
		Security:    application.SecuritySpec{Sandbox: "container", NetworkPolicy: "deny_all", RunAsNonRoot: true, ReadOnlyRootFS: true},
	})
	if err != nil {
		t.Fatalf("DeployFunction returned error: %v", err)
	}
	inv, err := svc.InvokeFunction(ctx, fn.ID, application.InvokeFunctionInput{Payload: map[string]any{"n": float64(1)}})
	if err != nil {
		t.Fatalf("InvokeFunction returned error: %v", err)
	}

	log, err := svc.AppendInvocationLog(ctx, inv.ID, application.AppendLogInput{
		Stream:  domain.LogStreamStdout,
		Message: "cold start complete",
		Seq:     42,
	})
	if err != nil {
		t.Fatalf("AppendInvocationLog returned error: %v", err)
	}
	if log.InvocationID != inv.ID || log.Message != "cold start complete" {
		t.Fatalf("unexpected log response: %+v", log)
	}
	if log.Seq != 42 {
		t.Fatalf("expected log sequence 42, got %d", log.Seq)
	}
	if repos.Logs.byInvocation[inv.ID][0].OrgID != testOrgID {
		t.Fatalf("expected log org %q, got %q", testOrgID, repos.Logs.byInvocation[inv.ID][0].OrgID)
	}

	otherOrgCtx := authDomain.WithOrgID(context.Background(), "org_other")
	if _, err := svc.AppendInvocationLog(otherOrgCtx, inv.ID, application.AppendLogInput{
		Stream:  domain.LogStreamStderr,
		Message: "should fail",
	}); err != domain.ErrInvocationNotFound {
		t.Fatalf("expected ErrInvocationNotFound for other org, got %v", err)
	}
}

func TestListInvocationLogsScopesToOwningOrg(t *testing.T) {
	repos := newMemoryRepos()
	svc := application.NewService(repos.Apps, repos.Functions, repos.Revisions, repos.Builds, repos.Invocations, repos.Logs)
	ctx := functionsCtx()
	app, err := svc.CreateApp(ctx, application.CreateAppInput{Name: "logs"})
	if err != nil {
		t.Fatalf("CreateApp returned error: %v", err)
	}
	fn, err := svc.DeployFunction(ctx, app.ID, application.DeployFunctionInput{
		Name:       "emit",
		Entrypoint: "main.emit",
		Image:      application.ImageSpec{Base: "python:3.12-slim"},
	})
	if err != nil {
		t.Fatalf("DeployFunction returned error: %v", err)
	}
	inv, err := svc.InvokeFunction(ctx, fn.ID, application.InvokeFunctionInput{})
	if err != nil {
		t.Fatalf("InvokeFunction returned error: %v", err)
	}
	if _, err := svc.AppendInvocationLog(ctx, inv.ID, application.AppendLogInput{
		Stream: domain.LogStreamSystem, Message: "starting", Seq: 1,
	}); err != nil {
		t.Fatalf("append starting log: %v", err)
	}
	if _, err := svc.AppendInvocationLog(ctx, inv.ID, application.AppendLogInput{
		Stream: domain.LogStreamStdout, Message: "ready", Seq: 2,
	}); err != nil {
		t.Fatalf("append ready log: %v", err)
	}

	logs, err := svc.ListInvocationLogs(ctx, inv.ID, pagination.Slice{Page: 1, PerPage: 1})
	if err != nil {
		t.Fatalf("ListInvocationLogs returned error: %v", err)
	}
	if logs.Meta.Total != 2 || logs.Meta.Count != 1 || logs.Meta.Page != 1 || logs.Meta.PerPage != 1 {
		t.Fatalf("unexpected pagination metadata: %+v", logs.Meta)
	}
	if len(logs.Items) != 1 || logs.Items[0].Message != "starting" {
		t.Fatalf("unexpected logs: %+v", logs)
	}

	otherOrgCtx := authDomain.WithOrgID(context.Background(), "org_other")
	if _, err := svc.ListInvocationLogs(otherOrgCtx, inv.ID, pagination.Slice{Page: 1, PerPage: 10}); err != domain.ErrInvocationNotFound {
		t.Fatalf("expected ErrInvocationNotFound for other org, got %v", err)
	}
}

type memoryRepos struct {
	Apps        *memoryAppRepo
	Functions   *memoryFunctionRepo
	Revisions   *memoryRevisionRepo
	Builds      *memoryBuildRepo
	Invocations *memoryInvocationRepo
	Logs        *memoryLogRepo
}

func newMemoryRepos() *memoryRepos {
	revisions := &memoryRevisionRepo{byID: map[string]*domain.FunctionRevision{}}
	return &memoryRepos{
		Apps:        &memoryAppRepo{byID: map[string]*domain.App{}},
		Functions:   &memoryFunctionRepo{byID: map[string]*domain.Function{}, revisions: revisions},
		Revisions:   revisions,
		Builds:      &memoryBuildRepo{byID: map[string]*domain.FunctionBuild{}},
		Invocations: &memoryInvocationRepo{byID: map[string]*domain.Invocation{}},
		Logs:        &memoryLogRepo{byInvocation: map[string][]domain.InvocationLog{}},
	}
}

type memoryAppRepo struct {
	byID map[string]*domain.App
}

func (r *memoryAppRepo) Create(_ context.Context, app *domain.App) error {
	copy := *app
	r.byID[app.ID] = &copy
	return nil
}

func (r *memoryAppRepo) FindByID(_ context.Context, orgID, id string) (*domain.App, error) {
	app := r.byID[id]
	if app == nil || app.OrgID != orgID {
		return nil, domain.ErrAppNotFound
	}
	copy := *app
	return &copy, nil
}

func (r *memoryAppRepo) ListByOrg(_ context.Context, orgID string, slice pagination.Slice) ([]domain.App, int64, error) {
	out := make([]domain.App, 0, len(r.byID))
	for _, app := range r.byID {
		if app.OrgID == orgID {
			out = append(out, *app)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return pageDomain(out, slice), int64(len(out)), nil
}

type memoryFunctionRepo struct {
	byID      map[string]*domain.Function
	revisions *memoryRevisionRepo
}

func (r *memoryFunctionRepo) Create(_ context.Context, fn *domain.Function) error {
	copy := *fn
	r.byID[fn.ID] = &copy
	return nil
}

func (r *memoryFunctionRepo) CreateWithRevision(ctx context.Context, fn *domain.Function, rev *domain.FunctionRevision) error {
	if err := r.Create(ctx, fn); err != nil {
		return err
	}
	if err := r.revisions.Create(ctx, rev); err != nil {
		delete(r.byID, fn.ID)
		return err
	}
	return nil
}

func (r *memoryFunctionRepo) FindByID(_ context.Context, orgID, id string) (*domain.Function, error) {
	fn := r.byID[id]
	if fn == nil || fn.OrgID != orgID {
		return nil, domain.ErrFunctionNotFound
	}
	copy := *fn
	return &copy, nil
}

func (r *memoryFunctionRepo) ListByApp(_ context.Context, orgID, appID string, slice pagination.Slice) ([]domain.Function, int64, error) {
	out := make([]domain.Function, 0, len(r.byID))
	for _, fn := range r.byID {
		if fn.OrgID == orgID && fn.AppID == appID {
			out = append(out, *fn)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return pageDomain(out, slice), int64(len(out)), nil
}

func (r *memoryFunctionRepo) Update(_ context.Context, fn *domain.Function) error {
	if _, ok := r.byID[fn.ID]; !ok {
		return domain.ErrFunctionNotFound
	}
	copy := *fn
	r.byID[fn.ID] = &copy
	return nil
}

type memoryRevisionRepo struct {
	byID map[string]*domain.FunctionRevision
}

func (r *memoryRevisionRepo) Create(_ context.Context, rev *domain.FunctionRevision) error {
	copy := *rev
	r.byID[rev.ID] = &copy
	return nil
}

func (r *memoryRevisionRepo) FindByID(_ context.Context, orgID, id string) (*domain.FunctionRevision, error) {
	rev := r.byID[id]
	if rev == nil || rev.OrgID != orgID {
		return nil, domain.ErrRevisionNotFound
	}
	copy := *rev
	return &copy, nil
}

func (r *memoryRevisionRepo) ListByFunction(_ context.Context, orgID, functionID string, slice pagination.Slice) ([]domain.FunctionRevision, int64, error) {
	out := make([]domain.FunctionRevision, 0, len(r.byID))
	for _, rev := range r.byID {
		if rev.OrgID == orgID && rev.FunctionID == functionID {
			out = append(out, *rev)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Version != out[j].Version {
			return out[i].Version > out[j].Version
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return pageDomain(out, slice), int64(len(out)), nil
}

func (r *memoryRevisionRepo) SetBuildID(_ context.Context, orgID, revisionID, buildID string) error {
	rev := r.byID[revisionID]
	if rev == nil || rev.OrgID != orgID {
		return domain.ErrRevisionNotFound
	}
	rev.BuildID = buildID
	return nil
}

type memoryBuildRepo struct {
	byID map[string]*domain.FunctionBuild
}

func (r *memoryBuildRepo) Create(_ context.Context, build *domain.FunctionBuild) error {
	copy := *build
	r.byID[build.ID] = &copy
	return nil
}

func (r *memoryBuildRepo) FindByID(_ context.Context, orgID, id string) (*domain.FunctionBuild, error) {
	build := r.byID[id]
	if build == nil || build.OrgID != orgID {
		return nil, domain.ErrBuildNotFound
	}
	copy := *build
	return &copy, nil
}

func (r *memoryBuildRepo) ListByRevision(_ context.Context, orgID, revisionID string, slice pagination.Slice) ([]domain.FunctionBuild, int64, error) {
	out := make([]domain.FunctionBuild, 0, len(r.byID))
	for _, build := range r.byID {
		if build.OrgID == orgID && build.RevisionID == revisionID {
			out = append(out, *build)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return pageDomain(out, slice), int64(len(out)), nil
}

func (r *memoryBuildRepo) ListByFunction(_ context.Context, orgID, functionID string, slice pagination.Slice) ([]domain.FunctionBuild, int64, error) {
	out := make([]domain.FunctionBuild, 0, len(r.byID))
	for _, build := range r.byID {
		if build.OrgID == orgID && build.FunctionID == functionID {
			out = append(out, *build)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return pageDomain(out, slice), int64(len(out)), nil
}

func (r *memoryBuildRepo) Update(_ context.Context, build *domain.FunctionBuild) error {
	if _, ok := r.byID[build.ID]; !ok {
		return domain.ErrBuildNotFound
	}
	copy := *build
	r.byID[build.ID] = &copy
	return nil
}

type memoryInvocationRepo struct {
	byID map[string]*domain.Invocation
}

func (r *memoryInvocationRepo) Create(_ context.Context, inv *domain.Invocation) error {
	copy := *inv
	r.byID[inv.ID] = &copy
	return nil
}

func (r *memoryInvocationRepo) FindByID(_ context.Context, orgID, id string) (*domain.Invocation, error) {
	inv := r.byID[id]
	if inv == nil || inv.OrgID != orgID {
		return nil, domain.ErrInvocationNotFound
	}
	copy := *inv
	return &copy, nil
}

func (r *memoryInvocationRepo) FindByIdempotencyKey(_ context.Context, orgID, functionID, key string) (*domain.Invocation, error) {
	for _, inv := range r.byID {
		if inv.OrgID == orgID && inv.FunctionID == functionID && inv.IdempotencyKey == key && inv.IdempotencyKey != "" {
			copy := *inv
			return &copy, nil
		}
	}
	return nil, domain.ErrInvocationNotFound
}

func (r *memoryInvocationRepo) ListByFunction(_ context.Context, orgID, functionID string, slice pagination.Slice) ([]domain.Invocation, int64, error) {
	out := make([]domain.Invocation, 0, len(r.byID))
	for _, inv := range r.byID {
		if inv.OrgID == orgID && inv.FunctionID == functionID {
			out = append(out, *inv)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return pageDomain(out, slice), int64(len(out)), nil
}

type memoryLogRepo struct {
	byInvocation map[string][]domain.InvocationLog
}

func (r *memoryLogRepo) Append(_ context.Context, log *domain.InvocationLog) error {
	copy := *log
	r.byInvocation[log.InvocationID] = append(r.byInvocation[log.InvocationID], copy)
	return nil
}

func (r *memoryLogRepo) ListByInvocationID(_ context.Context, orgID, invocationID string, slice pagination.Slice) ([]domain.InvocationLog, int64, error) {
	logs := r.byInvocation[invocationID]
	out := make([]domain.InvocationLog, 0, len(logs))
	for _, log := range logs {
		if log.OrgID == orgID {
			out = append(out, log)
		}
	}
	total := int64(len(out))
	start := slice.Offset()
	if start >= len(out) {
		return []domain.InvocationLog{}, total, nil
	}
	end := start + slice.PerPage
	if end > len(out) {
		end = len(out)
	}
	return out[start:end], total, nil
}

func pageDomain[T any](items []T, slice pagination.Slice) []T {
	start := slice.Offset()
	if start >= len(items) {
		return []T{}
	}
	end := start + slice.PerPage
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}
