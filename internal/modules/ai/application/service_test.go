package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"hyperstrate/server/internal/modules/ai/application"
	"hyperstrate/server/internal/modules/ai/domain"
)

// ── Stubs ─────────────────────────────────────────────────────────────────────

type stubModelRepo struct {
	model     *domain.Model
	findErr   error
	deleteErr error
}

func (r *stubModelRepo) List(_ context.Context, _, _ string, _, _ int) ([]domain.Model, int64, error) {
	return nil, 0, nil
}
func (r *stubModelRepo) Create(_ context.Context, _ *domain.Model) error { return nil }
func (r *stubModelRepo) FindByID(_ context.Context, _, _ string) (*domain.Model, error) {
	return r.model, r.findErr
}
func (r *stubModelRepo) Update(_ context.Context, _ *domain.Model) error { return nil }
func (r *stubModelRepo) Delete(_ context.Context, _, _ string) error     { return r.deleteErr }
func (r *stubModelRepo) ListByDefinitionKeys(_ context.Context, _ string, _ []string, _ string, _, _ int) ([]domain.Model, int64, error) {
	return nil, 0, nil
}
func (r *stubModelRepo) ListByIDs(_ context.Context, _ string, _ []string) ([]domain.Model, error) {
	return nil, nil
}
func (r *stubModelRepo) ListAll(_ context.Context) ([]domain.Model, error) { return nil, nil }

type stubRotationRepo struct{}

func (r *stubRotationRepo) Create(_ context.Context, _ *domain.ModelKeyRotation) error { return nil }
func (r *stubRotationRepo) ListByModelID(_ context.Context, _ string, _, _ int) ([]domain.ModelKeyRotation, int64, error) {
	return nil, 0, nil
}

type stubConfigRepo struct {
	cfg *domain.ModelConfiguration
	err error
}

func (r *stubConfigRepo) FindByModelID(_ context.Context, _, _ string) (*domain.ModelConfiguration, error) {
	return r.cfg, r.err
}
func (r *stubConfigRepo) Upsert(_ context.Context, _ string, _ *domain.ModelConfiguration) error {
	return nil
}
func (r *stubConfigRepo) DeleteByModelID(_ context.Context, _, _ string) error { return nil }
func (r *stubConfigRepo) ListConfiguredModelIDs(_ context.Context, _ string, ids []string) ([]string, error) {
	if r.cfg == nil {
		return nil, nil
	}
	for _, id := range ids {
		if id == r.cfg.ModelID {
			return []string{id}, nil
		}
	}
	return nil, nil
}
func (r *stubConfigRepo) ListByModelIDs(_ context.Context, _ string, ids []string) ([]domain.ModelConfiguration, error) {
	if r.cfg == nil {
		return nil, nil
	}
	for _, id := range ids {
		if id == r.cfg.ModelID {
			return []domain.ModelConfiguration{*r.cfg}, nil
		}
	}
	return nil, nil
}
func (r *stubConfigRepo) ListAllByModelIDs(_ context.Context, ids []string) ([]domain.ModelConfiguration, error) {
	if r.cfg == nil {
		return nil, nil
	}
	for _, id := range ids {
		if id == r.cfg.ModelID {
			return []domain.ModelConfiguration{*r.cfg}, nil
		}
	}
	return nil, nil
}

type stubConvRepo struct {
	conv          *domain.Conversation
	findErr       error
	msgs          []domain.ConversationMessage
	addErr        error
	addedMessages []*domain.ConversationMessage
}

func (r *stubConvRepo) List(_ context.Context, _ string, _, _ int) ([]domain.Conversation, int64, error) {
	return nil, 0, nil
}
func (r *stubConvRepo) Create(_ context.Context, _ *domain.Conversation) error { return nil }
func (r *stubConvRepo) FindByID(_ context.Context, _, _ string) (*domain.Conversation, error) {
	return r.conv, r.findErr
}
func (r *stubConvRepo) Delete(_ context.Context, _, _ string) error { return nil }
func (r *stubConvRepo) ListMessages(_ context.Context, _, _ string) ([]domain.ConversationMessage, error) {
	return r.msgs, nil
}
func (r *stubConvRepo) AddMessage(_ context.Context, _ string, msg *domain.ConversationMessage) error {
	r.addedMessages = append(r.addedMessages, msg)
	return r.addErr
}

type stubJobRepo struct {
	createErr error
	updated   []*domain.Job
}

func (r *stubJobRepo) List(_ context.Context, _ string, _, _ int) ([]domain.Job, int64, error) {
	return nil, 0, nil
}
func (r *stubJobRepo) ListByStatus(_ context.Context, _ domain.JobStatus) ([]domain.Job, error) {
	return nil, nil
}
func (r *stubJobRepo) Create(_ context.Context, _ *domain.Job) error { return r.createErr }
func (r *stubJobRepo) FindByID(_ context.Context, _, _ string) (*domain.Job, error) {
	return nil, nil
}
func (r *stubJobRepo) FindByIDForWorker(_ context.Context, _ string) (*domain.Job, error) {
	return nil, nil
}
func (r *stubJobRepo) Update(_ context.Context, job *domain.Job) error {
	r.updated = append(r.updated, job)
	return nil
}

type stubProxy struct {
	streamFn func() (<-chan application.StreamChunk, error)
}

func (p *stubProxy) Send(_ context.Context, _ *domain.ModelDefinition, _ *domain.ModelConfiguration, _ *application.ProxyRequest) (*application.ProxyResponse, error) {
	return &application.ProxyResponse{Content: "ok"}, nil
}
func (p *stubProxy) SendStream(_ context.Context, _ *domain.ModelDefinition, _ *domain.ModelConfiguration, _ *application.ProxyRequest) (<-chan application.StreamChunk, error) {
	if p.streamFn != nil {
		return p.streamFn()
	}
	ch := make(chan application.StreamChunk)
	close(ch)
	return ch, nil
}
func (p *stubProxy) SendEmbedding(_ context.Context, _ *domain.ModelDefinition, _ *domain.ModelConfiguration, _ string) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3}, nil
}

type stubProcessor struct{}

func (p *stubProcessor) ProcessJob(_ context.Context, _ string) error { return nil }

type stubDispatcher struct{ err error }

func (d *stubDispatcher) Dispatch(_ context.Context, _ string) error { return d.err }

// ── Helpers ───────────────────────────────────────────────────────────────────

const validDefKey = "chatgpt/gpt-5.4"

func validModel() *domain.Model {
	return &domain.Model{ID: "mdl_1", ModelDefinitionKey: validDefKey}
}

func validConfig() *domain.ModelConfiguration {
	return &domain.ModelConfiguration{
		ID: "mcfg_1", ModelID: "mdl_1",
		BaseURL: "https://api.openai.com", APIKey: "sk-test", TimeoutSecs: 30,
	}
}

func buildService(
	modelRepo domain.ModelRepository,
	cfgRepo domain.ModelConfigurationRepository,
	convRepo domain.ConversationRepository,
	jobRepo domain.JobRepository,
	proxy application.Proxy,
	dispatcher application.JobDispatcher,
	bus *application.ModelEventBus,
) application.Service {
	return application.NewService(modelRepo, cfgRepo, &stubRotationRepo{}, convRepo, jobRepo, proxy, &stubProcessor{}, dispatcher, bus, application.NewInferenceEventBus(), nil, application.NewMCPServerEventBus())
}

func TestGetModelConfigurationRedactsExtraHeaderValues(t *testing.T) {
	svc := buildService(
		&stubModelRepo{model: validModel()},
		&stubConfigRepo{cfg: &domain.ModelConfiguration{
			ID:           "mcfg_1",
			ModelID:      "mdl_1",
			BaseURL:      "https://api.example.com",
			ExtraHeaders: map[string]string{"Authorization": "secret-token", "X-Tenant": "tenant-a"},
			TimeoutSecs:  30,
		}},
		&stubConvRepo{},
		&stubJobRepo{},
		&stubProxy{},
		&stubDispatcher{},
		application.NewModelEventBus(),
	)

	resp, err := svc.GetModelConfiguration(context.Background(), "mdl_1")
	if err != nil {
		t.Fatalf("GetModelConfiguration returned error: %v", err)
	}
	if got := resp.ExtraHeaders["Authorization"]; got != "<redacted>" {
		t.Fatalf("response Authorization header = %q, want <redacted>", got)
	}
	if got := resp.ExtraHeaders["X-Tenant"]; got != "<redacted>" {
		t.Fatalf("response X-Tenant header = %q, want <redacted>", got)
	}
}

// ── SubmitJob tests ───────────────────────────────────────────────────────────

func TestSubmitJob_dispatchError_marksJobFailedAndReturnsError(t *testing.T) {
	jobRepo := &stubJobRepo{}
	dispatchErr := errors.New("sqs unavailable")
	svc := buildService(
		&stubModelRepo{model: validModel()},
		&stubConfigRepo{cfg: validConfig()},
		&stubConvRepo{},
		jobRepo,
		&stubProxy{},
		&stubDispatcher{err: dispatchErr},
		application.NewModelEventBus(),
	)

	_, err := svc.SubmitJob(context.Background(), application.SubmitJobRequest{
		ModelID: "mdl_1",
		Fields:  map[string]string{"prompt": "hello"},
	})

	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !errors.Is(err, dispatchErr) {
		t.Errorf("want dispatch error wrapped, got %v", err)
	}
	if len(jobRepo.updated) == 0 {
		t.Fatal("want job Update called to mark it failed, got none")
	}
	if jobRepo.updated[0].Status != domain.JobStatusFailed {
		t.Errorf("want job status FAILED, got %s", jobRepo.updated[0].Status)
	}
}

func TestSubmitJob_success_returnsPendingJob(t *testing.T) {
	svc := buildService(
		&stubModelRepo{model: validModel()},
		&stubConfigRepo{cfg: validConfig()},
		&stubConvRepo{},
		&stubJobRepo{},
		&stubProxy{},
		&stubDispatcher{},
		application.NewModelEventBus(),
	)

	job, err := svc.SubmitJob(context.Background(), application.SubmitJobRequest{
		ModelID: "mdl_1",
		Fields:  map[string]string{"prompt": "hello"},
	})
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if job == nil {
		t.Fatal("want job, got nil")
	}
	if job.Status != domain.JobStatusPending {
		t.Errorf("want status PENDING, got %s", job.Status)
	}
}

func TestSubmitJob_modelNotFound_returnsError(t *testing.T) {
	svc := buildService(
		&stubModelRepo{findErr: domain.ErrModelNotFound},
		&stubConfigRepo{},
		&stubConvRepo{},
		&stubJobRepo{},
		&stubProxy{},
		&stubDispatcher{},
		application.NewModelEventBus(),
	)
	_, err := svc.SubmitJob(context.Background(), application.SubmitJobRequest{ModelID: "missing"})
	if err == nil {
		t.Fatal("want error, got nil")
	}
}

// ── DeleteModel tests ─────────────────────────────────────────────────────────

func TestDeleteModel_listenerError_isPropagated(t *testing.T) {
	bus := application.NewModelEventBus()
	listenerErr := errors.New("cleanup failed")
	bus.OnDeleted(func(_ context.Context, _ application.ModelDeletedEvent) error {
		return listenerErr
	})

	svc := buildService(
		&stubModelRepo{model: validModel()},
		&stubConfigRepo{},
		&stubConvRepo{},
		&stubJobRepo{},
		&stubProxy{},
		&stubDispatcher{},
		bus,
	)

	err := svc.DeleteModel(context.Background(), "mdl_1")
	if !errors.Is(err, listenerErr) {
		t.Errorf("want listener error propagated, got %v", err)
	}
}

func TestDeleteModel_success(t *testing.T) {
	svc := buildService(
		&stubModelRepo{model: validModel()},
		&stubConfigRepo{},
		&stubConvRepo{},
		&stubJobRepo{},
		&stubProxy{},
		&stubDispatcher{},
		application.NewModelEventBus(),
	)
	if err := svc.DeleteModel(context.Background(), "mdl_1"); err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
}

// ── InferStream tests ─────────────────────────────────────────────────────────

func TestInferStream_withConversation_persistsAssistantReply(t *testing.T) {
	upstream := make(chan application.StreamChunk, 4)
	upstream <- application.StreamChunk{Delta: "hello"}
	upstream <- application.StreamChunk{Delta: " world"}
	upstream <- application.StreamChunk{Done: true}
	close(upstream)

	convRepo := &stubConvRepo{
		conv: &domain.Conversation{ID: "conv_1", ModelID: "mdl_1"},
	}
	svc := buildService(
		&stubModelRepo{model: validModel()},
		&stubConfigRepo{cfg: validConfig()},
		convRepo,
		&stubJobRepo{},
		&stubProxy{streamFn: func() (<-chan application.StreamChunk, error) { return upstream, nil }},
		&stubDispatcher{},
		application.NewModelEventBus(),
	)

	out, err := svc.InferStream(context.Background(), application.InferRequest{
		ModelID:        "mdl_1",
		ConversationID: "conv_1",
		Fields:         map[string]string{"prompt": "hi"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range out {
	} // drain to completion; persistence happens before out closes

	assistantMsg := findAssistantMessage(convRepo.addedMessages)
	if assistantMsg == nil {
		t.Fatal("want assistant message persisted, got none")
	}
	if assistantMsg.Content != "hello world" {
		t.Errorf("want content %q, got %q", "hello world", assistantMsg.Content)
	}
}

func TestInferStream_withConversation_persistsFullReplyAfterClientDisconnect(t *testing.T) {
	// 17 delta chunks exceeds the out channel buffer (16), guaranteeing the
	// ctx.Done() drain path is exercised when the buffer fills.
	const nChunks = 17
	upstream := make(chan application.StreamChunk, nChunks+1)
	wantContent := ""
	for range nChunks {
		upstream <- application.StreamChunk{Delta: "x"}
		wantContent += "x"
	}
	upstream <- application.StreamChunk{Done: true}
	close(upstream)

	convRepo := &stubConvRepo{
		conv: &domain.Conversation{ID: "conv_1", ModelID: "mdl_1"},
	}
	svc := buildService(
		&stubModelRepo{model: validModel()},
		&stubConfigRepo{cfg: validConfig()},
		convRepo,
		&stubJobRepo{},
		&stubProxy{streamFn: func() (<-chan application.StreamChunk, error) { return upstream, nil }},
		&stubDispatcher{},
		application.NewModelEventBus(),
	)

	// Cancel before InferStream so the goroutine's select encounters ctx.Done()
	// at the latest when the out buffer fills at chunk 17.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out, err := svc.InferStream(ctx, application.InferRequest{
		ModelID:        "mdl_1",
		ConversationID: "conv_1",
		Fields:         map[string]string{"prompt": "hi"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for out to close. Drain in background so the goroutine doesn't
	// block when filling the buffer before hitting ctx.Done().
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for stream goroutine to finish")
	}

	// All chunks must be accumulated regardless of which select branch fired.
	assistantMsg := findAssistantMessage(convRepo.addedMessages)
	if assistantMsg == nil {
		t.Fatal("want assistant message persisted after client disconnect, got none")
	}
	if assistantMsg.Content != wantContent {
		t.Errorf("want content %q, got %q", wantContent, assistantMsg.Content)
	}
}

func TestInferStream_withoutConversation_returnsUpstreamDirectly(t *testing.T) {
	upstream := make(chan application.StreamChunk, 2)
	upstream <- application.StreamChunk{Delta: "ok"}
	upstream <- application.StreamChunk{Done: true}
	close(upstream)

	convRepo := &stubConvRepo{}
	svc := buildService(
		&stubModelRepo{model: validModel()},
		&stubConfigRepo{cfg: validConfig()},
		convRepo,
		&stubJobRepo{},
		&stubProxy{streamFn: func() (<-chan application.StreamChunk, error) { return upstream, nil }},
		&stubDispatcher{},
		application.NewModelEventBus(),
	)

	out, err := svc.InferStream(context.Background(), application.InferRequest{
		ModelID: "mdl_1",
		Fields:  map[string]string{"prompt": "hi"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range out {
	}

	// No conversation: no assistant message should be persisted.
	if findAssistantMessage(convRepo.addedMessages) != nil {
		t.Error("want no assistant message persisted for stateless request, got one")
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func findAssistantMessage(msgs []*domain.ConversationMessage) *domain.ConversationMessage {
	for _, m := range msgs {
		if m.Role == "assistant" {
			return m
		}
	}
	return nil
}
