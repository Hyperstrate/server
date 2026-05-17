package application_test

// Org-isolation security tests.
//
// Each test uses an org-aware stub that mirrors what the GORM implementation
// does: returning ErrNotFound when the caller's org does not own the resource.
// This verifies that the service correctly threads orgID through every repo call
// rather than relying on a single integration test hitting a real database.

import (
	"context"
	"testing"

	authDomain "hyperstrate/server/internal/modules/auth/domain"

	"hyperstrate/server/internal/modules/ai/application"
	"hyperstrate/server/internal/modules/ai/domain"
	"hyperstrate/server/internal/shared/pagination"
)

// ── org-aware stubs ───────────────────────────────────────────────────────────

// orgModelRepo enforces org scoping: FindByID returns ErrModelNotFound when
// the requested orgID does not match the resource's owner.
type orgModelRepo struct {
	model *domain.Model
}

func (r *orgModelRepo) List(_ context.Context, orgID, _ string, _, _ int) ([]domain.Model, int64, error) {
	if r.model == nil || r.model.OrgID != orgID {
		return nil, 0, nil
	}
	return []domain.Model{*r.model}, 1, nil
}
func (r *orgModelRepo) Create(_ context.Context, m *domain.Model) error {
	r.model = m
	return nil
}
func (r *orgModelRepo) FindByID(_ context.Context, orgID, id string) (*domain.Model, error) {
	if r.model == nil || r.model.OrgID != orgID || r.model.ID != id {
		return nil, domain.ErrModelNotFound
	}
	return r.model, nil
}
func (r *orgModelRepo) Update(_ context.Context, m *domain.Model) error {
	r.model = m
	return nil
}
func (r *orgModelRepo) Delete(_ context.Context, orgID, id string) error {
	if r.model == nil || r.model.OrgID != orgID || r.model.ID != id {
		return domain.ErrModelNotFound
	}
	r.model = nil
	return nil
}
func (r *orgModelRepo) ListByDefinitionKeys(_ context.Context, orgID string, _ []string, _ string, _, _ int) ([]domain.Model, int64, error) {
	if r.model == nil || r.model.OrgID != orgID {
		return nil, 0, nil
	}
	return []domain.Model{*r.model}, 1, nil
}
func (r *orgModelRepo) ListByIDs(_ context.Context, orgID string, ids []string) ([]domain.Model, error) {
	if r.model == nil || r.model.OrgID != orgID {
		return nil, nil
	}
	for _, id := range ids {
		if id == r.model.ID {
			return []domain.Model{*r.model}, nil
		}
	}
	return nil, nil
}
func (r *orgModelRepo) ListAll(_ context.Context) ([]domain.Model, error) {
	if r.model == nil {
		return nil, nil
	}
	return []domain.Model{*r.model}, nil
}

// orgConvRepo enforces org scoping on conversations.
type orgConvRepo struct {
	conv *domain.Conversation
}

func (r *orgConvRepo) List(_ context.Context, orgID string, _, _ int) ([]domain.Conversation, int64, error) {
	if r.conv == nil || r.conv.OrgID != orgID {
		return nil, 0, nil
	}
	return []domain.Conversation{*r.conv}, 1, nil
}
func (r *orgConvRepo) Create(_ context.Context, c *domain.Conversation) error {
	r.conv = c
	return nil
}
func (r *orgConvRepo) FindByID(_ context.Context, orgID, id string) (*domain.Conversation, error) {
	if r.conv == nil || r.conv.OrgID != orgID || r.conv.ID != id {
		return nil, domain.ErrConversationNotFound
	}
	return r.conv, nil
}
func (r *orgConvRepo) Delete(_ context.Context, _, _ string) error { return nil }
func (r *orgConvRepo) ListMessages(_ context.Context, _, _ string) ([]domain.ConversationMessage, error) {
	return nil, nil
}
func (r *orgConvRepo) AddMessage(_ context.Context, _ string, _ *domain.ConversationMessage) error {
	return nil
}

// orgJobRepo enforces org scoping on jobs.
type orgJobRepo struct {
	job *domain.Job
}

func (r *orgJobRepo) List(_ context.Context, orgID string, _, _ int) ([]domain.Job, int64, error) {
	if r.job == nil || r.job.OrgID != orgID {
		return nil, 0, nil
	}
	return []domain.Job{*r.job}, 1, nil
}
func (r *orgJobRepo) ListByStatus(_ context.Context, _ domain.JobStatus) ([]domain.Job, error) {
	return nil, nil
}
func (r *orgJobRepo) Create(_ context.Context, j *domain.Job) error {
	r.job = j
	return nil
}
func (r *orgJobRepo) FindByID(_ context.Context, orgID, id string) (*domain.Job, error) {
	if r.job == nil || r.job.OrgID != orgID || r.job.ID != id {
		return nil, domain.ErrJobNotFound
	}
	return r.job, nil
}
func (r *orgJobRepo) FindByIDForWorker(_ context.Context, id string) (*domain.Job, error) {
	if r.job == nil || r.job.ID != id {
		return nil, domain.ErrJobNotFound
	}
	return r.job, nil
}
func (r *orgJobRepo) Update(_ context.Context, j *domain.Job) error {
	r.job = j
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func orgCtx(orgID string) context.Context {
	return authDomain.WithOrgID(context.Background(), orgID)
}

const (
	orgA = "org-alpha"
	orgB = "org-bravo"
)

func modelOwnedByOrgA() *domain.Model {
	return &domain.Model{ID: "mdl_a1", OrgID: orgA, ModelDefinitionKey: validDefKey}
}

func convOwnedByOrgA() *domain.Conversation {
	return &domain.Conversation{ID: "conv_a1", OrgID: orgA, ModelID: "mdl_a1"}
}

func jobOwnedByOrgA() *domain.Job {
	return &domain.Job{ID: "job_a1", OrgID: orgA, ModelID: "mdl_a1", Status: domain.JobStatusPending}
}

func buildSecureService(modelRepo domain.ModelRepository, convRepo domain.ConversationRepository, jobRepo domain.JobRepository) application.Service {
	return application.NewService(
		modelRepo,
		&stubConfigRepo{cfg: validConfig()},
		&stubRotationRepo{},
		convRepo,
		jobRepo,
		&stubProxy{},
		&stubProcessor{},
		&stubDispatcher{},
		application.NewModelEventBus(),
		application.NewInferenceEventBus(),
		nil, // mcpServerRepo — not needed for security tests
		application.NewMCPServerEventBus(),
	)
}

// ── Model isolation tests ─────────────────────────────────────────────────────

func TestOrgIsolation_GetModel_crossOrgReturnsNotFound(t *testing.T) {
	svc := buildSecureService(&orgModelRepo{model: modelOwnedByOrgA()}, &orgConvRepo{}, &orgJobRepo{})

	_, err := svc.GetModel(orgCtx(orgB), "mdl_a1")
	if err != domain.ErrModelNotFound {
		t.Errorf("want ErrModelNotFound when org-B reads org-A model, got %v", err)
	}
}

func TestOrgIsolation_GetModel_ownerOrgSucceeds(t *testing.T) {
	svc := buildSecureService(&orgModelRepo{model: modelOwnedByOrgA()}, &orgConvRepo{}, &orgJobRepo{})

	m, err := svc.GetModel(orgCtx(orgA), "mdl_a1")
	if err != nil {
		t.Fatalf("want success for owner org, got %v", err)
	}
	if m.ID != "mdl_a1" {
		t.Errorf("want model ID mdl_a1, got %s", m.ID)
	}
}

func TestOrgIsolation_UpdateModel_crossOrgReturnsNotFound(t *testing.T) {
	svc := buildSecureService(&orgModelRepo{model: modelOwnedByOrgA()}, &orgConvRepo{}, &orgJobRepo{})

	alias := "stolen"
	_, err := svc.UpdateModel(orgCtx(orgB), "mdl_a1", application.UpdateModelInput{Alias: &alias})
	if err != domain.ErrModelNotFound {
		t.Errorf("want ErrModelNotFound when org-B updates org-A model, got %v", err)
	}
}

func TestOrgIsolation_DeleteModel_crossOrgReturnsNotFound(t *testing.T) {
	svc := buildSecureService(&orgModelRepo{model: modelOwnedByOrgA()}, &orgConvRepo{}, &orgJobRepo{})

	err := svc.DeleteModel(orgCtx(orgB), "mdl_a1")
	if err != domain.ErrModelNotFound {
		t.Errorf("want ErrModelNotFound when org-B deletes org-A model, got %v", err)
	}
}

func TestOrgIsolation_ListModels_onlyReturnsCalling(t *testing.T) {
	svc := buildSecureService(&orgModelRepo{model: modelOwnedByOrgA()}, &orgConvRepo{}, &orgJobRepo{})

	// org-A sees its own model
	resultA, err := svc.ListModels(orgCtx(orgA), pagination.Slice{Page: 1, PerPage: 10}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if resultA.Meta.Total != 1 {
		t.Errorf("want org-A to see 1 model, got %d", resultA.Meta.Total)
	}

	// org-B sees nothing
	resultB, err := svc.ListModels(orgCtx(orgB), pagination.Slice{Page: 1, PerPage: 10}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if resultB.Meta.Total != 0 {
		t.Errorf("want org-B to see 0 models (cross-org), got %d", resultB.Meta.Total)
	}
}

func TestOrgIsolation_RegisterModel_setsOrgIDFromContext(t *testing.T) {
	repo := &orgModelRepo{}
	svc := buildSecureService(repo, &orgConvRepo{}, &orgJobRepo{})

	_, err := svc.RegisterModel(orgCtx(orgA), application.RegisterModelInput{ModelDefinitionKey: validDefKey})
	if err != nil {
		t.Fatal(err)
	}
	if repo.model == nil {
		t.Fatal("want model created, got nil")
	}
	if repo.model.OrgID != orgA {
		t.Errorf("want OrgID %q set on new model, got %q", orgA, repo.model.OrgID)
	}
}

// ── Conversation isolation tests ──────────────────────────────────────────────

func TestOrgIsolation_GetConversation_crossOrgReturnsNotFound(t *testing.T) {
	svc := buildSecureService(&orgModelRepo{model: modelOwnedByOrgA()}, &orgConvRepo{conv: convOwnedByOrgA()}, &orgJobRepo{})

	_, err := svc.GetConversation(orgCtx(orgB), "conv_a1")
	if err != domain.ErrConversationNotFound {
		t.Errorf("want ErrConversationNotFound when org-B reads org-A conversation, got %v", err)
	}
}

func TestOrgIsolation_GetConversation_ownerOrgSucceeds(t *testing.T) {
	svc := buildSecureService(&orgModelRepo{model: modelOwnedByOrgA()}, &orgConvRepo{conv: convOwnedByOrgA()}, &orgJobRepo{})

	c, err := svc.GetConversation(orgCtx(orgA), "conv_a1")
	if err != nil {
		t.Fatalf("want success for owner org, got %v", err)
	}
	if c.ID != "conv_a1" {
		t.Errorf("want conv ID conv_a1, got %s", c.ID)
	}
}

func TestOrgIsolation_ListConversationMessages_crossOrgReturnsNotFound(t *testing.T) {
	svc := buildSecureService(&orgModelRepo{model: modelOwnedByOrgA()}, &orgConvRepo{conv: convOwnedByOrgA()}, &orgJobRepo{})

	_, err := svc.ListConversationMessages(orgCtx(orgB), "conv_a1")
	if err != domain.ErrConversationNotFound {
		t.Errorf("want ErrConversationNotFound for cross-org message list, got %v", err)
	}
}

func TestOrgIsolation_ListConversations_onlyReturnsCalling(t *testing.T) {
	svc := buildSecureService(&orgModelRepo{model: modelOwnedByOrgA()}, &orgConvRepo{conv: convOwnedByOrgA()}, &orgJobRepo{})

	resultA, _ := svc.ListConversations(orgCtx(orgA), pagination.Slice{Page: 1, PerPage: 10})
	if resultA.Meta.Total != 1 {
		t.Errorf("want org-A to see 1 conversation, got %d", resultA.Meta.Total)
	}

	resultB, _ := svc.ListConversations(orgCtx(orgB), pagination.Slice{Page: 1, PerPage: 10})
	if resultB.Meta.Total != 0 {
		t.Errorf("want org-B to see 0 conversations (cross-org), got %d", resultB.Meta.Total)
	}
}

func TestOrgIsolation_CreateConversation_setsOrgIDFromContext(t *testing.T) {
	convRepo := &orgConvRepo{}
	svc := buildSecureService(&orgModelRepo{model: modelOwnedByOrgA()}, convRepo, &orgJobRepo{})

	_, err := svc.CreateConversation(orgCtx(orgA), application.CreateConversationInput{ModelID: "mdl_a1"})
	if err != nil {
		t.Fatal(err)
	}
	if convRepo.conv == nil {
		t.Fatal("want conversation created, got nil")
	}
	if convRepo.conv.OrgID != orgA {
		t.Errorf("want OrgID %q set on new conversation, got %q", orgA, convRepo.conv.OrgID)
	}
}

// ── Job isolation tests ───────────────────────────────────────────────────────

func TestOrgIsolation_GetJob_crossOrgReturnsNotFound(t *testing.T) {
	jobRepo := &orgJobRepo{job: jobOwnedByOrgA()}
	svc := buildSecureService(&orgModelRepo{model: modelOwnedByOrgA()}, &orgConvRepo{}, jobRepo)

	_, err := svc.GetJob(orgCtx(orgB), "job_a1")
	if err != domain.ErrJobNotFound {
		t.Errorf("want ErrJobNotFound when org-B reads org-A job, got %v", err)
	}
}

func TestOrgIsolation_GetJob_ownerOrgSucceeds(t *testing.T) {
	jobRepo := &orgJobRepo{job: jobOwnedByOrgA()}
	svc := buildSecureService(&orgModelRepo{model: modelOwnedByOrgA()}, &orgConvRepo{}, jobRepo)

	j, err := svc.GetJob(orgCtx(orgA), "job_a1")
	if err != nil {
		t.Fatalf("want success for owner org, got %v", err)
	}
	if j.ID != "job_a1" {
		t.Errorf("want job ID job_a1, got %s", j.ID)
	}
}

func TestOrgIsolation_ListJobs_onlyReturnsCalling(t *testing.T) {
	jobRepo := &orgJobRepo{job: jobOwnedByOrgA()}
	svc := buildSecureService(&orgModelRepo{}, &orgConvRepo{}, jobRepo)

	resultA, _ := svc.ListJobs(orgCtx(orgA), pagination.Slice{Page: 1, PerPage: 10})
	if resultA.Meta.Total != 1 {
		t.Errorf("want org-A to see 1 job, got %d", resultA.Meta.Total)
	}

	resultB, _ := svc.ListJobs(orgCtx(orgB), pagination.Slice{Page: 1, PerPage: 10})
	if resultB.Meta.Total != 0 {
		t.Errorf("want org-B to see 0 jobs (cross-org), got %d", resultB.Meta.Total)
	}
}

func TestOrgIsolation_SubmitJob_setsOrgIDFromContext(t *testing.T) {
	jobRepo := &orgJobRepo{}
	svc := buildSecureService(&orgModelRepo{model: modelOwnedByOrgA()}, &orgConvRepo{}, jobRepo)

	_, err := svc.SubmitJob(orgCtx(orgA), application.SubmitJobRequest{
		ModelID: "mdl_a1",
		Fields:  map[string]string{"prompt": "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if jobRepo.job == nil {
		t.Fatal("want job created, got nil")
	}
	if jobRepo.job.OrgID != orgA {
		t.Errorf("want OrgID %q set on new job, got %q", orgA, jobRepo.job.OrgID)
	}
}
