package application_test

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	authDomain "hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/modules/functions/application"
	"hyperstrate/server/internal/modules/functions/domain"
	"hyperstrate/server/internal/shared/dbtype"
	"hyperstrate/server/internal/shared/pagination"
)

func TestCreateRunnerPoolStoresHashedBootstrapToken(t *testing.T) {
	repos := newRunnerMemoryRepos()
	svc := application.NewRunnerService(repos.Pools, repos.Agents, repos.Builds, repos.Revisions, repos.Invocations, repos.Logs)
	ctx := authDomain.WithOrgID(context.Background(), testOrgID)

	pool, err := svc.CreateRunnerPool(ctx, application.CreateRunnerPoolInput{
		Name:         "aws-prod",
		Provider:     "byoc",
		Region:       "us-east-1",
		Capabilities: map[string]any{"gpu": "H100", "runtime": "python3.12"},
	})
	if err != nil {
		t.Fatalf("CreateRunnerPool returned error: %v", err)
	}
	if pool.BootstrapToken == "" || !strings.HasPrefix(pool.BootstrapToken, "hsrp_") {
		t.Fatalf("expected one-time bootstrap token, got %q", pool.BootstrapToken)
	}

	stored := repos.Pools.byID[pool.ID]
	if stored == nil {
		t.Fatal("expected runner pool to be stored")
	}
	if stored.OrgID != testOrgID {
		t.Fatalf("expected org %q, got %q", testOrgID, stored.OrgID)
	}
	if stored.BootstrapTokenHash == "" {
		t.Fatal("expected hashed bootstrap token")
	}
	if stored.BootstrapTokenHash == pool.BootstrapToken {
		t.Fatal("bootstrap token was stored in plaintext")
	}
	if stored.Status != domain.RunnerPoolStatusActive {
		t.Fatalf("expected active pool, got %q", stored.Status)
	}
}

func TestRegisterRunnerAgentRequiresValidBootstrapToken(t *testing.T) {
	repos := newRunnerMemoryRepos()
	svc := application.NewRunnerService(repos.Pools, repos.Agents, repos.Builds, repos.Revisions, repos.Invocations, repos.Logs)
	ctx := authDomain.WithOrgID(context.Background(), testOrgID)
	pool, err := svc.CreateRunnerPool(ctx, application.CreateRunnerPoolInput{Name: "local", Provider: "local"})
	if err != nil {
		t.Fatalf("CreateRunnerPool returned error: %v", err)
	}

	if _, err := svc.RegisterRunnerAgent(context.Background(), application.RegisterRunnerAgentInput{
		PoolID:         pool.ID,
		BootstrapToken: "wrong",
		PublicKey:      "runner-public-key",
		Hostname:       "runner-1",
	}); err != domain.ErrRunnerPoolNotFound {
		t.Fatalf("expected ErrRunnerPoolNotFound for invalid token, got %v", err)
	}

	agent, err := svc.RegisterRunnerAgent(context.Background(), application.RegisterRunnerAgentInput{
		PoolID:         pool.ID,
		BootstrapToken: pool.BootstrapToken,
		PublicKey:      "runner-public-key",
		Hostname:       "runner-1",
		Capabilities:   map[string]any{"gpu": "none"},
	})
	if err != nil {
		t.Fatalf("RegisterRunnerAgent returned error: %v", err)
	}
	if agent.SessionToken == "" || !strings.HasPrefix(agent.SessionToken, "hsra_") {
		t.Fatalf("expected short-lived runner session token, got %q", agent.SessionToken)
	}

	stored := repos.Agents.byID[agent.ID]
	if stored == nil {
		t.Fatal("expected runner agent to be stored")
	}
	if stored.PoolID != pool.ID || stored.OrgID != testOrgID {
		t.Fatalf("unexpected agent scope/linkage: %+v", stored)
	}
	if stored.SessionTokenHash == "" || stored.SessionTokenHash == agent.SessionToken {
		t.Fatal("runner session token was not stored hashed")
	}
	if stored.Status != domain.RunnerAgentStatusOnline {
		t.Fatalf("expected online agent, got %q", stored.Status)
	}
	if stored.SessionExpiresAt.IsZero() {
		t.Fatal("expected runner session expiry")
	}
	if _, err := svc.RegisterRunnerAgent(context.Background(), application.RegisterRunnerAgentInput{
		PoolID:         pool.ID,
		BootstrapToken: pool.BootstrapToken,
		PublicKey:      "runner-public-key",
		Hostname:       "runner-2",
	}); err != domain.ErrRunnerPoolNotFound {
		t.Fatalf("expected consumed bootstrap token reuse to fail, got %v", err)
	}
}

func TestRunnerServiceReadListsScopeToOrgAndDoNotExposeTokens(t *testing.T) {
	repos := newRunnerMemoryRepos()
	svc := application.NewRunnerService(repos.Pools, repos.Agents, repos.Builds, repos.Revisions, repos.Invocations, repos.Logs)
	ctx := authDomain.WithOrgID(context.Background(), testOrgID)

	pool, err := svc.CreateRunnerPool(ctx, application.CreateRunnerPoolInput{Name: "byoc", Provider: "byoc"})
	if err != nil {
		t.Fatalf("CreateRunnerPool returned error: %v", err)
	}
	agent, err := svc.RegisterRunnerAgent(context.Background(), application.RegisterRunnerAgentInput{
		PoolID:         pool.ID,
		BootstrapToken: pool.BootstrapToken,
		PublicKey:      "pub",
		Hostname:       "runner-1",
	})
	if err != nil {
		t.Fatalf("RegisterRunnerAgent returned error: %v", err)
	}

	pools, err := svc.ListRunnerPools(ctx, pagination.Slice{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("ListRunnerPools returned error: %v", err)
	}
	if pools.Meta.Total != 1 || len(pools.Items) != 1 || pools.Items[0].ID != pool.ID || pools.Items[0].BootstrapToken != "" {
		t.Fatalf("unexpected runner pools page: %+v", pools)
	}
	agents, err := svc.ListRunnerAgents(ctx, pool.ID, pagination.Slice{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("ListRunnerAgents returned error: %v", err)
	}
	if agents.Meta.Total != 1 || len(agents.Items) != 1 || agents.Items[0].ID != agent.ID || agents.Items[0].SessionExpiresAt.IsZero() {
		t.Fatalf("unexpected runner agents page: %+v", agents)
	}

	otherOrgCtx := authDomain.WithOrgID(context.Background(), "org_other")
	if _, err := svc.ListRunnerAgents(otherOrgCtx, pool.ID, pagination.Slice{Page: 1, PerPage: 10}); err != domain.ErrRunnerPoolNotFound {
		t.Fatalf("expected ErrRunnerPoolNotFound for other org, got %v", err)
	}
}

func TestLeaseNextInvocationAuthenticatesRunnerAndAssignsLease(t *testing.T) {
	repos := newRunnerMemoryRepos()
	svc := application.NewRunnerService(repos.Pools, repos.Agents, repos.Builds, repos.Revisions, repos.Invocations, repos.Logs)
	ctx := authDomain.WithOrgID(context.Background(), testOrgID)
	pool, err := svc.CreateRunnerPool(ctx, application.CreateRunnerPoolInput{
		Name:         "byoc",
		Provider:     "aws",
		Region:       "us-east-1",
		Capabilities: map[string]any{"runtime": "python3.12", "gpu": "H100", "sandbox": "gvisor"},
	})
	if err != nil {
		t.Fatalf("CreateRunnerPool returned error: %v", err)
	}
	agent, err := svc.RegisterRunnerAgent(context.Background(), application.RegisterRunnerAgentInput{
		PoolID:         pool.ID,
		BootstrapToken: pool.BootstrapToken,
		PublicKey:      "pub",
		Hostname:       "runner-1",
	})
	if err != nil {
		t.Fatalf("RegisterRunnerAgent returned error: %v", err)
	}
	if err := repos.Invocations.Create(context.Background(), &domain.Invocation{
		ID: "finv_queued", OrgID: testOrgID, AppID: "fapp_1", FunctionID: "fn_1", RevisionID: "frev_1",
		Status: domain.InvocationStatusQueued, Mode: domain.InvocationModeAsync, MaxAttempts: 3,
	}); err != nil {
		t.Fatalf("create queued invocation: %v", err)
	}
	if err := repos.Revisions.Create(context.Background(), &domain.FunctionRevision{
		ID: "frev_1", OrgID: testOrgID, AppID: "fapp_1", FunctionID: "fn_1", Version: 1, Entrypoint: "main.handle",
		BuildID:     "fbld_1",
		Image:       domain.ImageSpec{Base: "python:3.12-slim", Packages: []string{"httpx"}},
		Runtime:     domain.RuntimeSpec{PythonVersion: "3.12", GPU: "H100", TimeoutSecs: 300},
		Security:    domain.SecuritySpec{Sandbox: "gvisor", NetworkPolicy: "restricted", RunAsNonRoot: true, ReadOnlyRootFS: true},
		Provider:    domain.ProviderPlacementSpec{AllowedProviders: []string{"aws"}, Regions: []string{"us-east-1"}},
		Autoscaling: domain.AutoscalingSpec{MinContainers: 0, MaxContainers: 5, ScaleDownAfterSecs: 60},
	}); err != nil {
		t.Fatalf("create revision: %v", err)
	}
	if err := repos.Builds.Create(context.Background(), &domain.FunctionBuild{
		ID: "fbld_1", OrgID: testOrgID, AppID: "fapp_1", FunctionID: "fn_1", RevisionID: "frev_1",
		Status: domain.BuildStatusSucceeded,
		Source: domain.BuildSourceSpec{Type: "archive", URI: "s3://artifacts/source.tar.gz"},
		Artifact: domain.BuildArtifactSpec{
			ImageRef:         "registry.example.com/functions/fn_1:sha256",
			SourceArchiveRef: "s3://artifacts/source.tar.gz",
			Digest:           "sha256:abc",
		},
	}); err != nil {
		t.Fatalf("create build: %v", err)
	}

	if _, err := svc.LeaseNextInvocation(context.Background(), application.LeaseInvocationInput{
		AgentID:      agent.ID,
		SessionToken: "wrong",
		LeaseSecs:    30,
	}); err != domain.ErrRunnerUnauthorized {
		t.Fatalf("expected ErrRunnerUnauthorized for bad session token, got %v", err)
	}
	storedAgent := repos.Agents.byID[agent.ID]
	storedAgent.SessionExpiresAt = time.Now().UTC().Add(-time.Minute)
	if _, err := svc.LeaseNextInvocation(context.Background(), application.LeaseInvocationInput{
		AgentID:      agent.ID,
		SessionToken: agent.SessionToken,
		LeaseSecs:    30,
	}); err != domain.ErrRunnerUnauthorized {
		t.Fatalf("expected ErrRunnerUnauthorized for expired session token, got %v", err)
	}
	storedAgent.SessionExpiresAt = time.Now().UTC().Add(time.Hour)

	lease, err := svc.LeaseNextInvocation(context.Background(), application.LeaseInvocationInput{
		AgentID:      agent.ID,
		SessionToken: agent.SessionToken,
		LeaseSecs:    30,
	})
	if err != nil {
		t.Fatalf("LeaseNextInvocation returned error: %v", err)
	}
	if lease.Invocation.ID != "finv_queued" || lease.Invocation.RunnerID != agent.ID || lease.Invocation.Status != domain.InvocationStatusAssigned {
		t.Fatalf("unexpected lease response: %+v", lease)
	}
	if lease.Invocation.LeaseID == "" || lease.Invocation.LeaseExpiresAt == nil {
		t.Fatalf("expected lease id and expiry, got %+v", lease)
	}
	if lease.Invocation.Attempt != 1 || lease.Invocation.MaxAttempts != 3 {
		t.Fatalf("expected attempt 1/3, got %d/%d", lease.Invocation.Attempt, lease.Invocation.MaxAttempts)
	}
	if lease.Revision.Entrypoint != "main.handle" || lease.Revision.Image.Base != "python:3.12-slim" || lease.Revision.Runtime.GPU != "H100" {
		t.Fatalf("expected execution revision spec, got %+v", lease.Revision)
	}
	if lease.Execution.Entrypoint != "main.handle" || lease.Execution.ImageRef != "registry.example.com/functions/fn_1:sha256" {
		t.Fatalf("expected executable artifact contract, got %+v", lease.Execution)
	}
	if lease.Execution.TimeoutSecs != 300 || lease.Execution.Security.NetworkPolicy != "restricted" || !lease.Execution.Security.RunAsNonRoot {
		t.Fatalf("expected secure execution limits, got %+v", lease.Execution)
	}
	if len(lease.Execution.SecretEnv) != 0 {
		t.Fatalf("execution contract leaked secret material: %+v", lease.Execution.SecretEnv)
	}
}

func TestLeaseNextInvocationDerivesRunnerFromSessionToken(t *testing.T) {
	repos := newRunnerMemoryRepos()
	svc := application.NewRunnerService(repos.Pools, repos.Agents, repos.Builds, repos.Revisions, repos.Invocations, repos.Logs)
	ctx := authDomain.WithOrgID(context.Background(), testOrgID)
	pool, err := svc.CreateRunnerPool(ctx, application.CreateRunnerPoolInput{Name: "byoc", Provider: "byoc", Capabilities: map[string]any{"runtime": "python3.12"}})
	if err != nil {
		t.Fatalf("CreateRunnerPool returned error: %v", err)
	}
	agent, err := svc.RegisterRunnerAgent(context.Background(), application.RegisterRunnerAgentInput{
		PoolID: pool.ID, BootstrapToken: pool.BootstrapToken, PublicKey: "pub", Hostname: "runner-1",
	})
	if err != nil {
		t.Fatalf("RegisterRunnerAgent returned error: %v", err)
	}
	if err := repos.Invocations.Create(context.Background(), &domain.Invocation{
		ID: "finv_queued", OrgID: testOrgID, AppID: "fapp_1", FunctionID: "fn_1", RevisionID: "frev_1",
		Status: domain.InvocationStatusQueued, Mode: domain.InvocationModeAsync, MaxAttempts: 1,
	}); err != nil {
		t.Fatalf("create queued invocation: %v", err)
	}
	if err := repos.Revisions.Create(context.Background(), &domain.FunctionRevision{
		ID: "frev_1", OrgID: testOrgID, AppID: "fapp_1", FunctionID: "fn_1", Version: 1, Entrypoint: "main.handle",
		Image: domain.ImageSpec{Base: "python:3.12-slim"}, Runtime: domain.RuntimeSpec{PythonVersion: "3.12"},
	}); err != nil {
		t.Fatalf("create revision: %v", err)
	}

	lease, err := svc.LeaseNextInvocation(context.Background(), application.LeaseInvocationInput{
		SessionToken: agent.SessionToken,
		LeaseSecs:    30,
	})
	if err != nil {
		t.Fatalf("LeaseNextInvocation returned error: %v", err)
	}
	if lease.Invocation.RunnerID != agent.ID {
		t.Fatalf("expected token-derived runner %q, got %+v", agent.ID, lease.Invocation)
	}
	if _, err := svc.LeaseNextInvocation(context.Background(), application.LeaseInvocationInput{
		AgentID:      "fragent_other",
		SessionToken: agent.SessionToken,
		LeaseSecs:    30,
	}); err != domain.ErrRunnerUnauthorized {
		t.Fatalf("expected ErrRunnerUnauthorized for mismatched agent id, got %v", err)
	}
}

func TestLeaseNextInvocationReturnsNoWorkWhenQueueIsEmpty(t *testing.T) {
	repos := newRunnerMemoryRepos()
	svc := application.NewRunnerService(repos.Pools, repos.Agents, repos.Builds, repos.Revisions, repos.Invocations, repos.Logs)
	ctx := authDomain.WithOrgID(context.Background(), testOrgID)
	pool, err := svc.CreateRunnerPool(ctx, application.CreateRunnerPoolInput{Name: "byoc", Provider: "byoc"})
	if err != nil {
		t.Fatalf("CreateRunnerPool returned error: %v", err)
	}
	agent, err := svc.RegisterRunnerAgent(context.Background(), application.RegisterRunnerAgentInput{
		PoolID: pool.ID, BootstrapToken: pool.BootstrapToken, PublicKey: "pub", Hostname: "runner-1",
	})
	if err != nil {
		t.Fatalf("RegisterRunnerAgent returned error: %v", err)
	}

	_, err = svc.LeaseNextInvocation(context.Background(), application.LeaseInvocationInput{
		AgentID: agent.ID, SessionToken: agent.SessionToken, LeaseSecs: 30,
	})
	if err != domain.ErrNoInvocationAvailable {
		t.Fatalf("expected ErrNoInvocationAvailable, got %v", err)
	}
}

func TestHeartbeatRunnerAgentRenewsSessionAndCapabilities(t *testing.T) {
	repos := newRunnerMemoryRepos()
	svc := application.NewRunnerService(repos.Pools, repos.Agents, repos.Builds, repos.Revisions, repos.Invocations, repos.Logs)
	ctx := authDomain.WithOrgID(context.Background(), testOrgID)
	pool, err := svc.CreateRunnerPool(ctx, application.CreateRunnerPoolInput{Name: "byoc", Provider: "byoc"})
	if err != nil {
		t.Fatalf("CreateRunnerPool returned error: %v", err)
	}
	agent, err := svc.RegisterRunnerAgent(context.Background(), application.RegisterRunnerAgentInput{
		PoolID: pool.ID, BootstrapToken: pool.BootstrapToken, PublicKey: "pub", Hostname: "runner-1",
	})
	if err != nil {
		t.Fatalf("RegisterRunnerAgent returned error: %v", err)
	}
	beforeExpiry := repos.Agents.byID[agent.ID].SessionExpiresAt

	heartbeat, err := svc.HeartbeatRunnerAgent(context.Background(), application.HeartbeatRunnerAgentInput{
		SessionToken: agent.SessionToken,
		Capabilities: map[string]any{
			"runtime": "python3.12",
			"gpu":     "H100",
		},
	})
	if err != nil {
		t.Fatalf("HeartbeatRunnerAgent returned error: %v", err)
	}
	if heartbeat.ID != agent.ID || heartbeat.LastHeartbeatAt == nil {
		t.Fatalf("unexpected heartbeat response: %+v", heartbeat)
	}
	if !heartbeat.SessionExpiresAt.After(beforeExpiry) {
		t.Fatalf("expected renewed session expiry after %s, got %s", beforeExpiry, heartbeat.SessionExpiresAt)
	}
	stored := repos.Agents.byID[agent.ID]
	if stored.LastHeartbeatAt == nil || stored.Capabilities["gpu"] != "H100" || stored.Capabilities["runtime"] != "python3.12" {
		t.Fatalf("stored heartbeat/capabilities = %+v", stored)
	}

	if _, err := svc.HeartbeatRunnerAgent(context.Background(), application.HeartbeatRunnerAgentInput{
		SessionToken: "wrong",
	}); err != domain.ErrRunnerUnauthorized {
		t.Fatalf("expected ErrRunnerUnauthorized for bad heartbeat token, got %v", err)
	}
}

func TestRunnerAppendInvocationLogRequiresMatchingLease(t *testing.T) {
	repos := newRunnerMemoryRepos()
	svc := application.NewRunnerService(repos.Pools, repos.Agents, repos.Builds, repos.Revisions, repos.Invocations, repos.Logs)
	ctx := authDomain.WithOrgID(context.Background(), testOrgID)
	pool, err := svc.CreateRunnerPool(ctx, application.CreateRunnerPoolInput{Name: "byoc", Provider: "byoc"})
	if err != nil {
		t.Fatalf("CreateRunnerPool returned error: %v", err)
	}
	agent, err := svc.RegisterRunnerAgent(context.Background(), application.RegisterRunnerAgentInput{
		PoolID:         pool.ID,
		BootstrapToken: pool.BootstrapToken,
		PublicKey:      "pub",
		Hostname:       "runner-1",
	})
	if err != nil {
		t.Fatalf("RegisterRunnerAgent returned error: %v", err)
	}
	if err := repos.Invocations.Create(context.Background(), &domain.Invocation{
		ID: "finv_running", OrgID: testOrgID, AppID: "fapp_1", FunctionID: "fn_1", RevisionID: "frev_1",
		Status: domain.InvocationStatusAssigned, Mode: domain.InvocationModeAsync, RunnerID: agent.ID, LeaseID: "fls_valid", LeaseExpiresAt: timePtr(time.Now().UTC().Add(time.Minute)),
	}); err != nil {
		t.Fatalf("create assigned invocation: %v", err)
	}

	if _, err := svc.AppendInvocationLog(context.Background(), "finv_running", application.AppendRunnerLogInput{
		AgentID: agent.ID, SessionToken: agent.SessionToken, LeaseID: "wrong", Stream: domain.LogStreamStdout, Message: "nope",
	}); err != domain.ErrInvocationNotFound {
		t.Fatalf("expected ErrInvocationNotFound for wrong lease, got %v", err)
	}
	inv := repos.Invocations.byID["finv_running"]
	inv.LeaseExpiresAt = timePtr(time.Now().UTC().Add(-time.Minute))
	if _, err := svc.AppendInvocationLog(context.Background(), "finv_running", application.AppendRunnerLogInput{
		AgentID: agent.ID, SessionToken: agent.SessionToken, LeaseID: "fls_valid", Stream: domain.LogStreamStdout, Message: "expired",
	}); err != domain.ErrInvocationNotFound {
		t.Fatalf("expected ErrInvocationNotFound for expired lease, got %v", err)
	}
	inv.LeaseExpiresAt = timePtr(time.Now().UTC().Add(time.Minute))

	log, err := svc.AppendInvocationLog(context.Background(), "finv_running", application.AppendRunnerLogInput{
		AgentID: agent.ID, SessionToken: agent.SessionToken, LeaseID: "fls_valid",
		Stream: domain.LogStreamStdout, Seq: 7, Message: "hello", Truncated: true,
	})
	if err != nil {
		t.Fatalf("AppendInvocationLog returned error: %v", err)
	}
	if log.Seq != 7 || !log.Truncated || log.Message != "hello" {
		t.Fatalf("unexpected log response: %+v", log)
	}
	stored := repos.Logs.byInvocation["finv_running"]
	if len(stored) != 1 || stored[0].OrgID != testOrgID || stored[0].Seq != 7 {
		t.Fatalf("stored logs = %+v", stored)
	}
}

func TestRunnerCompletesInvocationWithLease(t *testing.T) {
	repos := newRunnerMemoryRepos()
	svc := application.NewRunnerService(repos.Pools, repos.Agents, repos.Builds, repos.Revisions, repos.Invocations, repos.Logs)
	ctx := authDomain.WithOrgID(context.Background(), testOrgID)
	pool, err := svc.CreateRunnerPool(ctx, application.CreateRunnerPoolInput{Name: "byoc", Provider: "byoc"})
	if err != nil {
		t.Fatalf("CreateRunnerPool returned error: %v", err)
	}
	agent, err := svc.RegisterRunnerAgent(context.Background(), application.RegisterRunnerAgentInput{
		PoolID:         pool.ID,
		BootstrapToken: pool.BootstrapToken,
		PublicKey:      "pub",
		Hostname:       "runner-1",
	})
	if err != nil {
		t.Fatalf("RegisterRunnerAgent returned error: %v", err)
	}
	if err := repos.Invocations.Create(context.Background(), &domain.Invocation{
		ID: "finv_complete", OrgID: testOrgID, AppID: "fapp_1", FunctionID: "fn_1", RevisionID: "frev_1",
		Status: domain.InvocationStatusRunning, Mode: domain.InvocationModeAsync, RunnerID: agent.ID, LeaseID: "fls_valid", LeaseExpiresAt: timePtr(time.Now().UTC().Add(time.Minute)),
	}); err != nil {
		t.Fatalf("create running invocation: %v", err)
	}

	done, err := svc.CompleteInvocation(context.Background(), "finv_complete", application.CompleteInvocationInput{
		AgentID: agent.ID, SessionToken: agent.SessionToken, LeaseID: "fls_valid",
		Status: domain.InvocationStatusSucceeded,
		Result: map[string]any{"ok": true},
	})
	if err != nil {
		t.Fatalf("CompleteInvocation returned error: %v", err)
	}
	if done.Status != domain.InvocationStatusSucceeded || done.Result["ok"] != true || done.FinishedAt == nil {
		t.Fatalf("unexpected completion response: %+v", done)
	}
}

type runnerMemoryRepos struct {
	Pools       *memoryRunnerPoolRepo
	Agents      *memoryRunnerAgentRepo
	Builds      *memoryBuildRepo
	Revisions   *memoryRevisionRepo
	Invocations *memoryInvocationRepo
	Logs        *memoryLogRepo
}

func newRunnerMemoryRepos() *runnerMemoryRepos {
	return &runnerMemoryRepos{
		Pools:       &memoryRunnerPoolRepo{byID: map[string]*domain.RunnerPool{}},
		Agents:      &memoryRunnerAgentRepo{byID: map[string]*domain.RunnerAgent{}},
		Builds:      &memoryBuildRepo{byID: map[string]*domain.FunctionBuild{}},
		Revisions:   &memoryRevisionRepo{byID: map[string]*domain.FunctionRevision{}},
		Invocations: &memoryInvocationRepo{byID: map[string]*domain.Invocation{}},
		Logs:        &memoryLogRepo{byInvocation: map[string][]domain.InvocationLog{}},
	}
}

type memoryRunnerPoolRepo struct {
	byID map[string]*domain.RunnerPool
}

func (r *memoryRunnerPoolRepo) Create(_ context.Context, pool *domain.RunnerPool) error {
	copy := *pool
	r.byID[pool.ID] = &copy
	return nil
}

func (r *memoryRunnerPoolRepo) FindByID(_ context.Context, id string) (*domain.RunnerPool, error) {
	pool := r.byID[id]
	if pool == nil {
		return nil, domain.ErrRunnerPoolNotFound
	}
	copy := *pool
	return &copy, nil
}

func (r *memoryRunnerPoolRepo) ListByOrg(_ context.Context, orgID string, slice pagination.Slice) ([]domain.RunnerPool, int64, error) {
	out := make([]domain.RunnerPool, 0, len(r.byID))
	for _, pool := range r.byID {
		if pool.OrgID == orgID {
			out = append(out, *pool)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return pageDomain(out, slice), int64(len(out)), nil
}

func (r *memoryRunnerPoolRepo) ConsumeBootstrapToken(_ context.Context, id, tokenHash string, consumedAt time.Time) (*domain.RunnerPool, error) {
	pool := r.byID[id]
	if pool == nil || pool.BootstrapTokenConsumedAt != nil || pool.BootstrapTokenHash != tokenHash {
		return nil, domain.ErrRunnerPoolNotFound
	}
	pool.BootstrapTokenConsumedAt = &consumedAt
	copy := *pool
	return &copy, nil
}

type memoryRunnerAgentRepo struct {
	byID map[string]*domain.RunnerAgent
}

func (r *memoryRunnerAgentRepo) Create(_ context.Context, agent *domain.RunnerAgent) error {
	copy := *agent
	r.byID[agent.ID] = &copy
	return nil
}

func (r *memoryRunnerAgentRepo) FindByID(_ context.Context, orgID, id string) (*domain.RunnerAgent, error) {
	agent := r.byID[id]
	if agent == nil || agent.OrgID != orgID {
		return nil, domain.ErrRunnerAgentNotFound
	}
	copy := *agent
	return &copy, nil
}

func (r *memoryRunnerAgentRepo) FindBySessionTokenHash(_ context.Context, hash string) (*domain.RunnerAgent, error) {
	for _, agent := range r.byID {
		if agent.SessionTokenHash == hash {
			copy := *agent
			return &copy, nil
		}
	}
	return nil, domain.ErrRunnerAgentNotFound
}

func (r *memoryRunnerAgentRepo) ListByPool(_ context.Context, orgID, poolID string, slice pagination.Slice) ([]domain.RunnerAgent, int64, error) {
	out := make([]domain.RunnerAgent, 0, len(r.byID))
	for _, agent := range r.byID {
		if agent.OrgID == orgID && agent.PoolID == poolID {
			out = append(out, *agent)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastHeartbeatAt == nil && out[j].LastHeartbeatAt != nil {
			return false
		}
		if out[i].LastHeartbeatAt != nil && out[j].LastHeartbeatAt == nil {
			return true
		}
		if out[i].LastHeartbeatAt != nil && out[j].LastHeartbeatAt != nil && !out[i].LastHeartbeatAt.Equal(*out[j].LastHeartbeatAt) {
			return out[i].LastHeartbeatAt.After(*out[j].LastHeartbeatAt)
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return pageDomain(out, slice), int64(len(out)), nil
}

func (r *memoryRunnerAgentRepo) RecordHeartbeat(_ context.Context, orgID, id string, heartbeatAt, sessionExpiresAt time.Time, capabilities dbtype.JSONMap) (*domain.RunnerAgent, error) {
	agent := r.byID[id]
	if agent == nil || agent.OrgID != orgID || agent.Status != domain.RunnerAgentStatusOnline {
		return nil, domain.ErrRunnerAgentNotFound
	}
	agent.LastHeartbeatAt = &heartbeatAt
	agent.SessionExpiresAt = sessionExpiresAt
	agent.Capabilities = capabilities
	copy := *agent
	return &copy, nil
}

func (r *memoryInvocationRepo) LeaseNextQueued(_ context.Context, orgID, runnerID, leaseID string, leaseExpiresAt time.Time, selector domain.RunnerSelector) (*domain.Invocation, error) {
	for _, inv := range r.byID {
		if inv.OrgID != orgID || inv.Status != domain.InvocationStatusQueued {
			continue
		}
		inv.Status = domain.InvocationStatusAssigned
		inv.RunnerID = runnerID
		inv.LeaseID = leaseID
		inv.LeaseExpiresAt = &leaseExpiresAt
		inv.Attempt++
		copy := *inv
		return &copy, nil
	}
	return nil, domain.ErrNoInvocationAvailable
}

func (r *memoryInvocationRepo) Complete(_ context.Context, orgID, invocationID, runnerID, leaseID string, status domain.InvocationStatus, result dbtype.JSONMap, errorMessage string, finishedAt time.Time) (*domain.Invocation, error) {
	inv := r.byID[invocationID]
	if inv == nil || inv.OrgID != orgID || inv.RunnerID != runnerID || inv.LeaseID != leaseID {
		return nil, domain.ErrInvocationNotFound
	}
	inv.Status = status
	inv.Result = result
	inv.Error = errorMessage
	inv.FinishedAt = &finishedAt
	copy := *inv
	return &copy, nil
}

func timePtr(t time.Time) *time.Time {
	return &t
}
