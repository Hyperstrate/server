package persistence

import (
	"context"
	"errors"
	"testing"
	"time"

	"hyperstrate/server/internal/modules/functions/domain"
	"hyperstrate/server/internal/shared/dbtype"
	"hyperstrate/server/internal/shared/pagination"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestFunctionRepositoriesScopeWritesAndReadsByOrg(t *testing.T) {
	db := newFunctionsPersistenceTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	apps := NewAppRepository(db)
	functions := NewFunctionRepository(db)
	revisions := NewRevisionRepository(db)
	builds := NewBuildRepository(db)
	invocations := NewInvocationRepository(db)
	logs := NewLogRepository(db)
	pools := NewRunnerPoolRepository(db)
	agents := NewRunnerAgentRepository(db)

	if err := apps.Create(ctx, &domain.App{
		ID: "fapp_a", OrgID: "org_a", Name: "Org A", CreatedAt: now, ModifiedAt: now,
	}); err != nil {
		t.Fatalf("create org_a app: %v", err)
	}
	if err := apps.Create(ctx, &domain.App{
		ID: "fapp_b", OrgID: "org_b", Name: "Org B", CreatedAt: now, ModifiedAt: now,
	}); err != nil {
		t.Fatalf("create org_b app: %v", err)
	}
	app, err := apps.FindByID(ctx, "org_a", "fapp_a")
	if err != nil {
		t.Fatalf("find org_a app: %v", err)
	}
	if app.OrgID != "org_a" || app.Name != "Org A" {
		t.Fatalf("org_a app = %+v", app)
	}
	if _, err := apps.FindByID(ctx, "org_a", "fapp_b"); !errors.Is(err, domain.ErrAppNotFound) {
		t.Fatalf("cross-org app lookup error = %v, want ErrAppNotFound", err)
	}

	if err := functions.Create(ctx, &domain.Function{
		ID: "fn_a", OrgID: "org_a", AppID: "fapp_a", Name: "parse", Entrypoint: "main.parse", Status: domain.FunctionStatusDeploying, CreatedAt: now, ModifiedAt: now,
	}); err != nil {
		t.Fatalf("create org_a function: %v", err)
	}
	if err := functions.Create(ctx, &domain.Function{
		ID: "fn_b", OrgID: "org_b", AppID: "fapp_b", Name: "classify", Entrypoint: "main.classify", Status: domain.FunctionStatusDeploying, CreatedAt: now, ModifiedAt: now,
	}); err != nil {
		t.Fatalf("create org_b function: %v", err)
	}
	if _, err := functions.FindByID(ctx, "org_a", "fn_b"); !errors.Is(err, domain.ErrFunctionNotFound) {
		t.Fatalf("cross-org function lookup error = %v, want ErrFunctionNotFound", err)
	}
	if err := functions.Update(ctx, &domain.Function{
		ID: "fn_b", OrgID: "org_a", AppID: "fapp_a", Name: "stolen", Entrypoint: "main.stolen", Status: domain.FunctionStatusReady, CreatedAt: now, ModifiedAt: now,
	}); !errors.Is(err, domain.ErrFunctionNotFound) {
		t.Fatalf("cross-org function update error = %v, want ErrFunctionNotFound", err)
	}
	fnB, err := functions.FindByID(ctx, "org_b", "fn_b")
	if err != nil {
		t.Fatalf("find org_b function: %v", err)
	}
	if fnB.Name != "classify" || fnB.Status != domain.FunctionStatusDeploying {
		t.Fatalf("cross-org function changed to %+v", fnB)
	}

	if err := revisions.Create(ctx, &domain.FunctionRevision{
		ID: "frev_a", OrgID: "org_a", AppID: "fapp_a", FunctionID: "fn_a", Version: 1, Entrypoint: "main.parse",
		Image:       domain.ImageSpec{Base: "python:3.12-slim", Packages: []string{"httpx"}},
		Runtime:     domain.RuntimeSpec{PythonVersion: "3.12", TimeoutSecs: 60},
		Autoscaling: domain.AutoscalingSpec{MinContainers: 0, MaxContainers: 2, ScaleDownAfterSecs: 30, MaxConcurrency: 1},
		Security:    domain.SecuritySpec{Sandbox: "container", NetworkPolicy: "deny_all", RunAsNonRoot: true, ReadOnlyRootFS: true},
		Secrets:     []domain.SecretMountSpec{{Name: "api", Env: "API_KEY"}},
		Volumes:     []domain.VolumeMountSpec{{Name: "cache", MountPath: "/cache", ReadOnly: true}},
		Provider:    domain.ProviderPlacementSpec{Strategy: "portable", AllowedProviders: []string{"aws"}},
		CreatedAt:   now,
	}); err != nil {
		t.Fatalf("create org_a revision: %v", err)
	}
	if err := revisions.Create(ctx, &domain.FunctionRevision{
		ID: "frev_b", OrgID: "org_b", AppID: "fapp_b", FunctionID: "fn_b", Version: 1, Entrypoint: "main.classify",
		Image:     domain.ImageSpec{Base: "python:3.12-slim"},
		Runtime:   domain.RuntimeSpec{PythonVersion: "3.12", TimeoutSecs: 60},
		Security:  domain.SecuritySpec{Sandbox: "container", NetworkPolicy: "deny_all"},
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("create org_b revision: %v", err)
	}
	rev, err := revisions.FindByID(ctx, "org_a", "frev_a")
	if err != nil {
		t.Fatalf("find org_a revision: %v", err)
	}
	if rev.Image.Base != "python:3.12-slim" || len(rev.Secrets) != 1 || rev.Secrets[0].Env != "API_KEY" {
		t.Fatalf("revision JSON fields were not preserved: %+v", rev)
	}
	if _, err := revisions.FindByID(ctx, "org_a", "frev_b"); !errors.Is(err, domain.ErrRevisionNotFound) {
		t.Fatalf("cross-org revision lookup error = %v, want ErrRevisionNotFound", err)
	}
	if err := builds.Create(ctx, &domain.FunctionBuild{
		ID: "fbld_a", OrgID: "org_a", AppID: "fapp_a", FunctionID: "fn_a", RevisionID: "frev_a",
		Status:    domain.BuildStatusQueued,
		Source:    domain.BuildSourceSpec{Type: "archive", URI: "s3://source-a.tar.gz", Digest: "sha256:source"},
		Artifact:  domain.BuildArtifactSpec{ImageRef: "registry.example.com/fn-a:sha256", SourceArchiveRef: "s3://source-a.tar.gz"},
		CreatedAt: now, ModifiedAt: now,
	}); err != nil {
		t.Fatalf("create org_a build: %v", err)
	}
	if err := revisions.SetBuildID(ctx, "org_a", "frev_a", "fbld_a"); err != nil {
		t.Fatalf("set revision build id: %v", err)
	}
	build, err := builds.FindByID(ctx, "org_a", "fbld_a")
	if err != nil {
		t.Fatalf("find org_a build: %v", err)
	}
	if build.Source.URI != "s3://source-a.tar.gz" || build.Artifact.ImageRef == "" {
		t.Fatalf("build JSON fields were not preserved: %+v", build)
	}
	build.Status = domain.BuildStatusSucceeded
	if err := builds.Update(ctx, build); err != nil {
		t.Fatalf("update org_a build: %v", err)
	}
	updatedRev, err := revisions.FindByID(ctx, "org_a", "frev_a")
	if err != nil {
		t.Fatalf("find linked revision: %v", err)
	}
	if updatedRev.BuildID != "fbld_a" {
		t.Fatalf("expected linked build id, got %q", updatedRev.BuildID)
	}
	if _, err := builds.FindByID(ctx, "org_b", "fbld_a"); !errors.Is(err, domain.ErrBuildNotFound) {
		t.Fatalf("cross-org build lookup error = %v, want ErrBuildNotFound", err)
	}

	if err := invocations.Create(ctx, &domain.Invocation{
		ID: "finv_a", OrgID: "org_a", AppID: "fapp_a", FunctionID: "fn_a", RevisionID: "frev_a",
		Mode: domain.InvocationModeAsync, Status: domain.InvocationStatusQueued, Payload: dbtype.JSONMap{"text": "hello"},
		Attempt: 1, MaxAttempts: 3, IdempotencyKey: "idem-a", LeaseID: "lease-a", RunnerID: "fragent_a",
		CreatedAt: now, ModifiedAt: now,
	}); err != nil {
		t.Fatalf("create org_a invocation: %v", err)
	}
	if err := invocations.Create(ctx, &domain.Invocation{
		ID: "finv_b", OrgID: "org_b", AppID: "fapp_b", FunctionID: "fn_b", RevisionID: "frev_b",
		Mode: domain.InvocationModeSync, Status: domain.InvocationStatusQueued, Payload: dbtype.JSONMap{"text": "secret"}, CreatedAt: now, ModifiedAt: now,
	}); err != nil {
		t.Fatalf("create org_b invocation: %v", err)
	}
	inv, err := invocations.FindByID(ctx, "org_a", "finv_a")
	if err != nil {
		t.Fatalf("find org_a invocation: %v", err)
	}
	if inv.Payload["text"] != "hello" || inv.Attempt != 1 || inv.MaxAttempts != 3 || inv.IdempotencyKey != "idem-a" || inv.LeaseID != "lease-a" {
		t.Fatalf("invocation payload = %+v", inv.Payload)
	}
	if _, err := invocations.FindByID(ctx, "org_a", "finv_b"); !errors.Is(err, domain.ErrInvocationNotFound) {
		t.Fatalf("cross-org invocation lookup error = %v, want ErrInvocationNotFound", err)
	}

	if err := logs.Append(ctx, &domain.InvocationLog{
		ID: "flog_a", OrgID: "org_a", InvocationID: "finv_a", Seq: 2, Stream: domain.LogStreamStdout, Message: "ready", CreatedAt: now,
	}); err != nil {
		t.Fatalf("append org_a log: %v", err)
	}
	if err := logs.Append(ctx, &domain.InvocationLog{
		ID: "flog_a_earlier", OrgID: "org_a", InvocationID: "finv_a", Seq: 1, Stream: domain.LogStreamSystem, Message: "starting", Truncated: true, CreatedAt: now,
	}); err != nil {
		t.Fatalf("append org_a log: %v", err)
	}
	if err := logs.Append(ctx, &domain.InvocationLog{
		ID: "flog_b", OrgID: "org_b", InvocationID: "finv_b", Stream: domain.LogStreamStderr, Message: "private", CreatedAt: now,
	}); err != nil {
		t.Fatalf("append org_b log: %v", err)
	}
	orgALogs, total, err := logs.ListByInvocationID(ctx, "org_a", "finv_a", pagination.Slice{Page: 1, PerPage: 1})
	if err != nil {
		t.Fatalf("list org_a logs: %v", err)
	}
	if total != 2 || len(orgALogs) != 1 || orgALogs[0].Message != "starting" || orgALogs[0].Seq != 1 || !orgALogs[0].Truncated {
		t.Fatalf("org_a logs = %+v", orgALogs)
	}
	crossOrgLogs, total, err := logs.ListByInvocationID(ctx, "org_a", "finv_b", pagination.Slice{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("list cross-org logs: %v", err)
	}
	if total != 0 || len(crossOrgLogs) != 0 {
		t.Fatalf("cross-org logs = %+v total=%d, want none", crossOrgLogs, total)
	}

	idempotent, err := invocations.FindByIdempotencyKey(ctx, "org_a", "fn_a", "idem-a")
	if err != nil {
		t.Fatalf("find idempotent invocation: %v", err)
	}
	if idempotent.ID != "finv_a" {
		t.Fatalf("idempotent invocation = %+v", idempotent)
	}
	if _, err := invocations.FindByIdempotencyKey(ctx, "org_a", "fn_b", "idem-a"); !errors.Is(err, domain.ErrInvocationNotFound) {
		t.Fatalf("cross-function idempotency lookup error = %v, want ErrInvocationNotFound", err)
	}

	if err := pools.Create(ctx, &domain.RunnerPool{
		ID: "frpool_a", OrgID: "org_a", Name: "aws-a", Provider: "byoc", Region: "us-east-1",
		Status: domain.RunnerPoolStatusActive, Capabilities: dbtype.JSONMap{"gpu": "H100"}, BootstrapTokenHash: "hash_a",
		CreatedAt: now, ModifiedAt: now,
	}); err != nil {
		t.Fatalf("create org_a runner pool: %v", err)
	}
	if err := pools.Create(ctx, &domain.RunnerPool{
		ID: "frpool_b", OrgID: "org_b", Name: "aws-b", Provider: "byoc", Region: "us-west-2",
		Status: domain.RunnerPoolStatusActive, Capabilities: dbtype.JSONMap{"gpu": "A100"}, BootstrapTokenHash: "hash_b",
		CreatedAt: now, ModifiedAt: now,
	}); err != nil {
		t.Fatalf("create org_b runner pool: %v", err)
	}
	pool, err := pools.FindByID(ctx, "frpool_a")
	if err != nil {
		t.Fatalf("find org_a runner pool by bootstrap path: %v", err)
	}
	if pool.OrgID != "org_a" || pool.Capabilities["gpu"] != "H100" {
		t.Fatalf("runner pool JSON/scope = %+v", pool)
	}

	if err := agents.Create(ctx, &domain.RunnerAgent{
		ID: "fragent_a", OrgID: "org_a", PoolID: "frpool_a", Hostname: "runner-a", PublicKey: "pub-a",
		Status: domain.RunnerAgentStatusOnline, Capabilities: dbtype.JSONMap{"runtime": "python3.12"}, SessionTokenHash: "session_a",
		CreatedAt: now, ModifiedAt: now,
	}); err != nil {
		t.Fatalf("create org_a runner agent: %v", err)
	}
	if err := agents.Create(ctx, &domain.RunnerAgent{
		ID: "fragent_b", OrgID: "org_b", PoolID: "frpool_b", Hostname: "runner-b", PublicKey: "pub-b",
		Status: domain.RunnerAgentStatusOnline, Capabilities: dbtype.JSONMap{"runtime": "python3.11"}, SessionTokenHash: "session_b",
		CreatedAt: now, ModifiedAt: now,
	}); err != nil {
		t.Fatalf("create org_b runner agent: %v", err)
	}
	agent, err := agents.FindByID(ctx, "org_a", "fragent_a")
	if err != nil {
		t.Fatalf("find org_a runner agent: %v", err)
	}
	if agent.OrgID != "org_a" || agent.SessionTokenHash != "session_a" || agent.Capabilities["runtime"] != "python3.12" {
		t.Fatalf("runner agent JSON/scope = %+v", agent)
	}
	if _, err := agents.FindByID(ctx, "org_a", "fragent_b"); !errors.Is(err, domain.ErrRunnerAgentNotFound) {
		t.Fatalf("cross-org runner agent lookup error = %v, want ErrRunnerAgentNotFound", err)
	}
}

func TestInvocationRepositoryLeasesOnlyMatchingRunnerCapabilities(t *testing.T) {
	db := newFunctionsPersistenceTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	apps := NewAppRepository(db)
	functions := NewFunctionRepository(db)
	revisions := NewRevisionRepository(db)
	invocations := NewInvocationRepository(db)

	if err := apps.Create(ctx, &domain.App{ID: "fapp_sched", OrgID: "org_sched", Name: "Scheduler", CreatedAt: now, ModifiedAt: now}); err != nil {
		t.Fatalf("create app: %v", err)
	}
	if err := functions.Create(ctx, &domain.Function{ID: "fn_sched", OrgID: "org_sched", AppID: "fapp_sched", Name: "work", Entrypoint: "main.work", Status: domain.FunctionStatusDeploying, CreatedAt: now, ModifiedAt: now}); err != nil {
		t.Fatalf("create function: %v", err)
	}
	if err := revisions.Create(ctx, &domain.FunctionRevision{
		ID: "frev_a100", OrgID: "org_sched", AppID: "fapp_sched", FunctionID: "fn_sched", Version: 1, Entrypoint: "main.work",
		Image:     domain.ImageSpec{Base: "python:3.12-slim"},
		Runtime:   domain.RuntimeSpec{PythonVersion: "3.12", GPU: "A100"},
		Security:  domain.SecuritySpec{Sandbox: "gvisor"},
		Provider:  domain.ProviderPlacementSpec{AllowedProviders: []string{"aws"}, Regions: []string{"us-east-1"}},
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("create A100 revision: %v", err)
	}
	if err := revisions.Create(ctx, &domain.FunctionRevision{
		ID: "frev_h100", OrgID: "org_sched", AppID: "fapp_sched", FunctionID: "fn_sched", Version: 2, Entrypoint: "main.work",
		Image:     domain.ImageSpec{Base: "python:3.12-slim"},
		Runtime:   domain.RuntimeSpec{PythonVersion: "3.12", GPU: "H100"},
		Security:  domain.SecuritySpec{Sandbox: "gvisor"},
		Provider:  domain.ProviderPlacementSpec{AllowedProviders: []string{"aws"}, Regions: []string{"us-east-1"}},
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("create H100 revision: %v", err)
	}
	if err := invocations.Create(ctx, &domain.Invocation{
		ID: "finv_a100", OrgID: "org_sched", AppID: "fapp_sched", FunctionID: "fn_sched", RevisionID: "frev_a100",
		Mode: domain.InvocationModeAsync, Status: domain.InvocationStatusQueued, MaxAttempts: 1, CreatedAt: now, ModifiedAt: now,
	}); err != nil {
		t.Fatalf("create A100 invocation: %v", err)
	}
	if err := invocations.Create(ctx, &domain.Invocation{
		ID: "finv_h100", OrgID: "org_sched", AppID: "fapp_sched", FunctionID: "fn_sched", RevisionID: "frev_h100",
		Mode: domain.InvocationModeAsync, Status: domain.InvocationStatusQueued, MaxAttempts: 1, CreatedAt: now.Add(time.Second), ModifiedAt: now,
	}); err != nil {
		t.Fatalf("create H100 invocation: %v", err)
	}

	leased, err := invocations.LeaseNextQueued(ctx, "org_sched", "fragent_h100", "fls_sched", now.Add(time.Minute), domain.RunnerSelector{
		Provider: "aws",
		Region:   "us-east-1",
		Capabilities: dbtype.JSONMap{
			"runtime": "python3.12",
			"gpu":     "H100",
			"sandbox": "gvisor",
		},
	})
	if err != nil {
		t.Fatalf("lease matching invocation: %v", err)
	}
	if leased.ID != "finv_h100" || leased.RevisionID != "frev_h100" || leased.Attempt != 1 {
		t.Fatalf("leased = %+v, want H100 invocation", leased)
	}
}

func TestInvocationRepositoryReclaimsExpiredRunningLease(t *testing.T) {
	db := newFunctionsPersistenceTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	apps := NewAppRepository(db)
	functions := NewFunctionRepository(db)
	revisions := NewRevisionRepository(db)
	invocations := NewInvocationRepository(db)

	if err := apps.Create(ctx, &domain.App{ID: "fapp_reclaim", OrgID: "org_reclaim", Name: "Reclaim", CreatedAt: now, ModifiedAt: now}); err != nil {
		t.Fatalf("create app: %v", err)
	}
	if err := functions.Create(ctx, &domain.Function{
		ID: "fn_reclaim", OrgID: "org_reclaim", AppID: "fapp_reclaim", Name: "work", Entrypoint: "main.work",
		Status: domain.FunctionStatusDeploying, ActiveRevisionID: "frev_reclaim", CreatedAt: now, ModifiedAt: now,
	}); err != nil {
		t.Fatalf("create function: %v", err)
	}
	if err := revisions.Create(ctx, &domain.FunctionRevision{
		ID: "frev_reclaim", OrgID: "org_reclaim", AppID: "fapp_reclaim", FunctionID: "fn_reclaim", Version: 1, Entrypoint: "main.work",
		Image: domain.ImageSpec{Base: "python:3.12-slim"}, Runtime: domain.RuntimeSpec{PythonVersion: "3.12"}, CreatedAt: now,
	}); err != nil {
		t.Fatalf("create revision: %v", err)
	}
	expiredAt := time.Now().UTC().Add(-time.Minute)
	if err := invocations.Create(ctx, &domain.Invocation{
		ID: "finv_expired_running", OrgID: "org_reclaim", AppID: "fapp_reclaim", FunctionID: "fn_reclaim", RevisionID: "frev_reclaim",
		Mode: domain.InvocationModeAsync, Status: domain.InvocationStatusRunning, RunnerID: "fragent_dead", LeaseID: "fls_dead",
		LeaseExpiresAt: &expiredAt, Attempt: 1, MaxAttempts: 3, CreatedAt: now, ModifiedAt: now,
	}); err != nil {
		t.Fatalf("create running invocation: %v", err)
	}

	leased, err := invocations.LeaseNextQueued(ctx, "org_reclaim", "fragent_alive", "fls_alive", time.Now().UTC().Add(time.Minute), domain.RunnerSelector{
		Capabilities: dbtype.JSONMap{"runtime": "python3.12"},
	})
	if err != nil {
		t.Fatalf("lease expired running invocation: %v", err)
	}
	if leased.ID != "finv_expired_running" || leased.RunnerID != "fragent_alive" || leased.LeaseID != "fls_alive" || leased.Attempt != 2 {
		t.Fatalf("leased = %+v, want reassigned expired running invocation", leased)
	}
}

func TestFunctionRepositoryCreateWithRevisionRollsBackFunctionWhenRevisionFails(t *testing.T) {
	db := newFunctionsPersistenceTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	apps := NewAppRepository(db)
	functions := NewFunctionRepository(db)
	revisions := NewRevisionRepository(db)

	if err := apps.Create(ctx, &domain.App{ID: "fapp_atomic", OrgID: "org_atomic", Name: "Atomic", CreatedAt: now, ModifiedAt: now}); err != nil {
		t.Fatalf("create app: %v", err)
	}
	if err := functions.Create(ctx, &domain.Function{
		ID: "fn_existing", OrgID: "org_atomic", AppID: "fapp_atomic", Name: "existing", Entrypoint: "main.existing",
		Status: domain.FunctionStatusDeploying, ActiveRevisionID: "frev_conflict", CreatedAt: now, ModifiedAt: now,
	}); err != nil {
		t.Fatalf("create existing function: %v", err)
	}
	if err := revisions.Create(ctx, &domain.FunctionRevision{
		ID: "frev_conflict", OrgID: "org_atomic", AppID: "fapp_atomic", FunctionID: "fn_existing", Version: 1, Entrypoint: "main.existing",
		Image: domain.ImageSpec{Base: "python:3.12-slim"}, CreatedAt: now,
	}); err != nil {
		t.Fatalf("create existing revision: %v", err)
	}

	err := functions.CreateWithRevision(ctx, &domain.Function{
		ID: "fn_atomic", OrgID: "org_atomic", AppID: "fapp_atomic", Name: "new", Entrypoint: "main.new",
		Status: domain.FunctionStatusDeploying, ActiveRevisionID: "frev_conflict", CreatedAt: now, ModifiedAt: now,
	}, &domain.FunctionRevision{
		ID: "frev_conflict", OrgID: "org_atomic", AppID: "fapp_atomic", FunctionID: "fn_atomic", Version: 1, Entrypoint: "main.new",
		Image: domain.ImageSpec{Base: "python:3.12-slim"}, CreatedAt: now,
	})
	if err == nil {
		t.Fatal("expected duplicate revision insert to fail")
	}
	if _, err := functions.FindByID(ctx, "org_atomic", "fn_atomic"); !errors.Is(err, domain.ErrFunctionNotFound) {
		t.Fatalf("expected function insert rollback, got %v", err)
	}
}

func newFunctionsPersistenceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.Table("function_apps").AutoMigrate(&domain.App{}); err != nil {
		t.Fatalf("auto migrate function_apps: %v", err)
	}
	if err := db.Table("functions").AutoMigrate(&domain.Function{}); err != nil {
		t.Fatalf("auto migrate functions: %v", err)
	}
	if err := db.Table("function_revisions").AutoMigrate(&domain.FunctionRevision{}); err != nil {
		t.Fatalf("auto migrate function_revisions: %v", err)
	}
	if err := db.Table("function_builds").AutoMigrate(&domain.FunctionBuild{}); err != nil {
		t.Fatalf("auto migrate function_builds: %v", err)
	}
	if err := db.Table("function_invocations").AutoMigrate(&domain.Invocation{}); err != nil {
		t.Fatalf("auto migrate function_invocations: %v", err)
	}
	if err := db.Table("function_invocation_logs").AutoMigrate(&domain.InvocationLog{}); err != nil {
		t.Fatalf("auto migrate function_invocation_logs: %v", err)
	}
	if err := db.Table("function_runner_pools").AutoMigrate(&domain.RunnerPool{}); err != nil {
		t.Fatalf("auto migrate function_runner_pools: %v", err)
	}
	if err := db.Table("function_runner_agents").AutoMigrate(&domain.RunnerAgent{}); err != nil {
		t.Fatalf("auto migrate function_runner_agents: %v", err)
	}
	return db
}
