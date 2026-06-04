package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"time"

	authDomain "hyperstrate/server/internal/modules/auth/domain"
	"hyperstrate/server/internal/modules/functions/domain"
	"hyperstrate/server/internal/shared/dbtype"
	"hyperstrate/server/internal/shared/pagination"

	"go.jetify.com/typeid/v2"
)

type RunnerService interface {
	CreateRunnerPool(ctx context.Context, input CreateRunnerPoolInput) (*RunnerPoolResponse, error)
	ListRunnerPools(ctx context.Context, slice pagination.Slice) (pagination.Paginated[RunnerPoolResponse], error)
	ListRunnerAgents(ctx context.Context, poolID string, slice pagination.Slice) (pagination.Paginated[RunnerAgentResponse], error)
	RegisterRunnerAgent(ctx context.Context, input RegisterRunnerAgentInput) (*RunnerAgentRegistrationResponse, error)
	HeartbeatRunnerAgent(ctx context.Context, input HeartbeatRunnerAgentInput) (*RunnerAgentHeartbeatResponse, error)
	LeaseNextInvocation(ctx context.Context, input LeaseInvocationInput) (*RunnerLeaseResponse, error)
	AppendInvocationLog(ctx context.Context, invocationID string, input AppendRunnerLogInput) (*LogResponse, error)
	CompleteInvocation(ctx context.Context, invocationID string, input CompleteInvocationInput) (*InvocationResponse, error)
}

type runnerService struct {
	pools       domain.RunnerPoolRepository
	agents      domain.RunnerAgentRepository
	builds      domain.BuildRepository
	revisions   domain.RevisionRepository
	invocations domain.InvocationRepository
	logs        domain.LogRepository
}

func NewRunnerService(
	pools domain.RunnerPoolRepository,
	agents domain.RunnerAgentRepository,
	builds domain.BuildRepository,
	revisions domain.RevisionRepository,
	invocations domain.InvocationRepository,
	logs domain.LogRepository,
) RunnerService {
	return &runnerService{pools: pools, agents: agents, builds: builds, revisions: revisions, invocations: invocations, logs: logs}
}

type CreateRunnerPoolInput struct {
	Name         string         `json:"name"         binding:"required,max=255"`
	Provider     string         `json:"provider"     binding:"required,max=100"`
	Region       string         `json:"region"       binding:"max=100"`
	Capabilities map[string]any `json:"capabilities,omitempty"`
}

type RegisterRunnerAgentInput struct {
	PoolID         string         `json:"poolId"         binding:"required"`
	BootstrapToken string         `json:"bootstrapToken" binding:"required"`
	PublicKey      string         `json:"publicKey"      binding:"required"`
	Hostname       string         `json:"hostname"       binding:"max=255"`
	Capabilities   map[string]any `json:"capabilities,omitempty"`
}

type HeartbeatRunnerAgentInput struct {
	AgentID      string         `json:"agentId,omitempty"`
	SessionToken string         `json:"sessionToken"`
	Capabilities map[string]any `json:"capabilities,omitempty"`
}

type LeaseInvocationInput struct {
	AgentID      string `json:"agentId,omitempty"`
	SessionToken string `json:"sessionToken"`
	LeaseSecs    int    `json:"leaseSecs,omitempty" binding:"min=0,max=3600"`
}

type AppendRunnerLogInput struct {
	AgentID      string           `json:"agentId,omitempty"`
	SessionToken string           `json:"sessionToken"`
	LeaseID      string           `json:"leaseId"      binding:"required"`
	Stream       domain.LogStream `json:"stream"       binding:"required,oneof=stdout stderr system"`
	Message      string           `json:"message"      binding:"required"`
	Seq          int64            `json:"seq,omitempty" binding:"min=0"`
	Truncated    bool             `json:"truncated,omitempty"`
}

type RunnerPoolResponse struct {
	ID             string                  `json:"id"`
	Name           string                  `json:"name"`
	Provider       string                  `json:"provider"`
	Region         string                  `json:"region,omitempty"`
	Status         domain.RunnerPoolStatus `json:"status"`
	Capabilities   dbtype.JSONMap          `json:"capabilities,omitempty"`
	BootstrapToken string                  `json:"bootstrapToken,omitempty"`
	CreatedAt      time.Time               `json:"createdAt"`
	ModifiedAt     time.Time               `json:"modifiedAt"`
}

type RunnerAgentRegistrationResponse struct {
	ID           string                   `json:"id"`
	PoolID       string                   `json:"poolId"`
	Hostname     string                   `json:"hostname"`
	Status       domain.RunnerAgentStatus `json:"status"`
	Capabilities dbtype.JSONMap           `json:"capabilities,omitempty"`
	SessionToken string                   `json:"sessionToken"`
}

type RunnerAgentHeartbeatResponse struct {
	ID               string                   `json:"id"`
	PoolID           string                   `json:"poolId"`
	Status           domain.RunnerAgentStatus `json:"status"`
	Capabilities     dbtype.JSONMap           `json:"capabilities,omitempty"`
	LastHeartbeatAt  *time.Time               `json:"lastHeartbeatAt,omitempty"`
	SessionExpiresAt time.Time                `json:"sessionExpiresAt"`
}

func (s *runnerService) CreateRunnerPool(ctx context.Context, input CreateRunnerPoolInput) (*RunnerPoolResponse, error) {
	token, err := randomToken("hsrp")
	if err != nil {
		return nil, err
	}
	pool := &domain.RunnerPool{
		ID:                 typeid.MustGenerate("frpool").String(),
		OrgID:              authDomain.OrgIDFromContext(ctx),
		Name:               input.Name,
		Provider:           input.Provider,
		Region:             input.Region,
		Status:             domain.RunnerPoolStatusActive,
		Capabilities:       dbtype.JSONMap(input.Capabilities),
		BootstrapTokenHash: hashToken(token),
	}
	if pool.Capabilities == nil {
		pool.Capabilities = dbtype.JSONMap{}
	}
	if err := s.pools.Create(ctx, pool); err != nil {
		return nil, err
	}
	resp := toRunnerPoolResponse(pool)
	resp.BootstrapToken = token
	return &resp, nil
}

func (s *runnerService) ListRunnerPools(ctx context.Context, slice pagination.Slice) (pagination.Paginated[RunnerPoolResponse], error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	pools, total, err := s.pools.ListByOrg(ctx, orgID, slice)
	if err != nil {
		return pagination.Paginated[RunnerPoolResponse]{}, err
	}
	resp := make([]RunnerPoolResponse, 0, len(pools))
	for i := range pools {
		item := toRunnerPoolResponse(&pools[i])
		item.BootstrapToken = ""
		resp = append(resp, item)
	}
	return pagination.New(resp, total, slice), nil
}

func (s *runnerService) ListRunnerAgents(ctx context.Context, poolID string, slice pagination.Slice) (pagination.Paginated[RunnerAgentResponse], error) {
	orgID := authDomain.OrgIDFromContext(ctx)
	pool, err := s.pools.FindByID(ctx, poolID)
	if err != nil {
		return pagination.Paginated[RunnerAgentResponse]{}, err
	}
	if pool.OrgID != orgID {
		return pagination.Paginated[RunnerAgentResponse]{}, domain.ErrRunnerPoolNotFound
	}
	agents, total, err := s.agents.ListByPool(ctx, orgID, pool.ID, slice)
	if err != nil {
		return pagination.Paginated[RunnerAgentResponse]{}, err
	}
	resp := make([]RunnerAgentResponse, 0, len(agents))
	for i := range agents {
		resp = append(resp, toRunnerAgentResponse(&agents[i]))
	}
	return pagination.New(resp, total, slice), nil
}

func (s *runnerService) RegisterRunnerAgent(ctx context.Context, input RegisterRunnerAgentInput) (*RunnerAgentRegistrationResponse, error) {
	now := time.Now().UTC()
	pool, err := s.pools.ConsumeBootstrapToken(ctx, input.PoolID, hashToken(input.BootstrapToken), now)
	if err != nil {
		return nil, err
	}
	if pool.Status != domain.RunnerPoolStatusActive {
		return nil, domain.ErrRunnerPoolNotFound
	}
	sessionToken, err := randomToken("hsra")
	if err != nil {
		return nil, err
	}
	agent := &domain.RunnerAgent{
		ID:               typeid.MustGenerate("fragent").String(),
		OrgID:            pool.OrgID,
		PoolID:           pool.ID,
		Hostname:         input.Hostname,
		PublicKey:        input.PublicKey,
		Status:           domain.RunnerAgentStatusOnline,
		Capabilities:     dbtype.JSONMap(input.Capabilities),
		SessionTokenHash: hashToken(sessionToken),
		SessionExpiresAt: now.Add(24 * time.Hour),
	}
	if agent.Capabilities == nil {
		agent.Capabilities = dbtype.JSONMap{}
	}
	if err := s.agents.Create(ctx, agent); err != nil {
		return nil, err
	}
	resp := toRunnerAgentRegistrationResponse(agent)
	resp.SessionToken = sessionToken
	return &resp, nil
}

func (s *runnerService) HeartbeatRunnerAgent(ctx context.Context, input HeartbeatRunnerAgentInput) (*RunnerAgentHeartbeatResponse, error) {
	agent, err := s.authenticatedAgent(ctx, input.AgentID, input.SessionToken)
	if err != nil {
		return nil, err
	}
	capabilities := agent.Capabilities
	if input.Capabilities != nil {
		capabilities = dbtype.JSONMap(input.Capabilities)
	}
	if capabilities == nil {
		capabilities = dbtype.JSONMap{}
	}
	now := time.Now().UTC()
	updated, err := s.agents.RecordHeartbeat(ctx, agent.OrgID, agent.ID, now, now.Add(24*time.Hour), capabilities)
	if err != nil {
		return nil, err
	}
	resp := toRunnerAgentHeartbeatResponse(updated)
	return &resp, nil
}

func (s *runnerService) LeaseNextInvocation(ctx context.Context, input LeaseInvocationInput) (*RunnerLeaseResponse, error) {
	agent, err := s.authenticatedAgent(ctx, input.AgentID, input.SessionToken)
	if err != nil {
		return nil, err
	}
	pool, err := s.pools.FindByID(ctx, agent.PoolID)
	if err != nil {
		return nil, err
	}
	leaseSecs := input.LeaseSecs
	if leaseSecs <= 0 {
		leaseSecs = 30
	}
	leaseID, err := randomToken("fls")
	if err != nil {
		return nil, err
	}
	leaseExpiresAt := time.Now().UTC().Add(time.Duration(leaseSecs) * time.Second)
	selector := domain.RunnerSelector{
		Provider:     pool.Provider,
		Region:       pool.Region,
		Capabilities: mergeCapabilities(pool.Capabilities, agent.Capabilities),
	}
	inv, err := s.invocations.LeaseNextQueued(ctx, agent.OrgID, agent.ID, leaseID, leaseExpiresAt, selector)
	if err != nil {
		return nil, err
	}
	rev, err := s.revisions.FindByID(ctx, agent.OrgID, inv.RevisionID)
	if err != nil {
		return nil, err
	}
	var build *domain.FunctionBuild
	if rev.BuildID != "" {
		build, err = s.builds.FindByID(ctx, agent.OrgID, rev.BuildID)
		if err != nil {
			return nil, err
		}
		if build.Status != domain.BuildStatusSucceeded {
			return nil, domain.ErrBuildNotReady
		}
	}
	resp := &RunnerLeaseResponse{
		Invocation: toInvocationResponse(inv),
		Revision:   toRevisionResponse(rev),
		Execution:  toExecutionContract(inv, rev, build),
	}
	if build != nil {
		buildResp := toBuildResponse(build)
		resp.Build = &buildResp
	}
	return resp, nil
}

func (s *runnerService) AppendInvocationLog(ctx context.Context, invocationID string, input AppendRunnerLogInput) (*LogResponse, error) {
	agent, err := s.authenticatedAgent(ctx, input.AgentID, input.SessionToken)
	if err != nil {
		return nil, err
	}
	inv, err := s.invocations.FindByID(ctx, agent.OrgID, invocationID)
	if err != nil {
		return nil, err
	}
	if inv.RunnerID != agent.ID || inv.LeaseID != input.LeaseID {
		return nil, domain.ErrInvocationNotFound
	}
	if !activeLease(inv, time.Now().UTC()) {
		return nil, domain.ErrInvocationNotFound
	}
	log := &domain.InvocationLog{
		ID:           typeid.MustGenerate("flog").String(),
		OrgID:        agent.OrgID,
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

func (s *runnerService) CompleteInvocation(ctx context.Context, invocationID string, input CompleteInvocationInput) (*InvocationResponse, error) {
	agent, err := s.authenticatedAgent(ctx, input.AgentID, input.SessionToken)
	if err != nil {
		return nil, err
	}
	finishedAt := time.Now().UTC()
	inv, err := s.invocations.Complete(ctx, agent.OrgID, invocationID, agent.ID, input.LeaseID, input.Status, dbtype.JSONMap(input.Result), input.Error, finishedAt)
	if err != nil {
		return nil, err
	}
	resp := toInvocationResponse(inv)
	return &resp, nil
}

func (s *runnerService) authenticatedAgent(ctx context.Context, agentID, sessionToken string) (*domain.RunnerAgent, error) {
	if sessionToken == "" {
		return nil, domain.ErrRunnerUnauthorized
	}
	agent, err := s.agents.FindBySessionTokenHash(ctx, hashToken(sessionToken))
	if err != nil {
		return nil, domain.ErrRunnerUnauthorized
	}
	if agentID != "" && agent.ID != agentID {
		return nil, domain.ErrRunnerUnauthorized
	}
	if agent.Status != domain.RunnerAgentStatusOnline {
		return nil, domain.ErrRunnerUnauthorized
	}
	if !agent.SessionExpiresAt.IsZero() && time.Now().UTC().After(agent.SessionExpiresAt) {
		return nil, domain.ErrRunnerUnauthorized
	}
	return agent, nil
}

func toRunnerPoolResponse(pool *domain.RunnerPool) RunnerPoolResponse {
	return RunnerPoolResponse{
		ID:           pool.ID,
		Name:         pool.Name,
		Provider:     pool.Provider,
		Region:       pool.Region,
		Status:       pool.Status,
		Capabilities: pool.Capabilities,
		CreatedAt:    pool.CreatedAt,
		ModifiedAt:   pool.ModifiedAt,
	}
}

func toRunnerAgentRegistrationResponse(agent *domain.RunnerAgent) RunnerAgentRegistrationResponse {
	return RunnerAgentRegistrationResponse{
		ID:           agent.ID,
		PoolID:       agent.PoolID,
		Hostname:     agent.Hostname,
		Status:       agent.Status,
		Capabilities: agent.Capabilities,
	}
}

func toRunnerAgentHeartbeatResponse(agent *domain.RunnerAgent) RunnerAgentHeartbeatResponse {
	return RunnerAgentHeartbeatResponse{
		ID:               agent.ID,
		PoolID:           agent.PoolID,
		Status:           agent.Status,
		Capabilities:     agent.Capabilities,
		LastHeartbeatAt:  agent.LastHeartbeatAt,
		SessionExpiresAt: agent.SessionExpiresAt,
	}
}

func randomToken(prefix string) (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(buf[:]), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func tokenHashesEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func mergeCapabilities(poolCaps, agentCaps dbtype.JSONMap) dbtype.JSONMap {
	merged := dbtype.JSONMap{}
	for k, v := range poolCaps {
		merged[k] = v
	}
	for k, v := range agentCaps {
		merged[k] = v
	}
	return merged
}

func activeLease(inv *domain.Invocation, now time.Time) bool {
	switch inv.Status {
	case domain.InvocationStatusAssigned, domain.InvocationStatusStarting, domain.InvocationStatusRunning:
	default:
		return false
	}
	return inv.LeaseExpiresAt != nil && now.Before(*inv.LeaseExpiresAt)
}

func toExecutionContract(inv *domain.Invocation, rev *domain.FunctionRevision, build *domain.FunctionBuild) ExecutionContract {
	imageRef := rev.Image.Base
	sourceArchiveRef := ""
	sourceDigest := ""
	if build != nil {
		if build.Artifact.ImageRef != "" {
			imageRef = build.Artifact.ImageRef
		}
		sourceArchiveRef = build.Artifact.SourceArchiveRef
		sourceDigest = build.Artifact.Digest
	}
	timeoutSecs := rev.Runtime.TimeoutSecs
	if timeoutSecs <= 0 {
		timeoutSecs = 300
	}
	return ExecutionContract{
		Entrypoint:       rev.Entrypoint,
		ImageRef:         imageRef,
		SourceArchiveRef: sourceArchiveRef,
		SourceDigest:     sourceDigest,
		Payload:          inv.Payload,
		Runtime:          RuntimeSpec(rev.Runtime),
		TimeoutSecs:      timeoutSecs,
		Security:         SecuritySpec(rev.Security),
		SecretEnv:        secretMountResponses(rev.Secrets),
		Volumes:          volumeMountResponses(rev.Volumes),
	}
}

func secretMountResponses(in []domain.SecretMountSpec) []SecretMountSpec {
	out := make([]SecretMountSpec, len(in))
	for i, item := range in {
		out[i] = SecretMountSpec(item)
	}
	return out
}

func volumeMountResponses(in []domain.VolumeMountSpec) []VolumeMountSpec {
	out := make([]VolumeMountSpec, len(in))
	for i, item := range in {
		out[i] = VolumeMountSpec(item)
	}
	return out
}
