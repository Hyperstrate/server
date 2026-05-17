package application

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"hyperstrate/server/internal/modules/ai/domain"
)

// jobProcessor is the concrete implementation of JobProcessor.
// It is intentionally separate from service so that GoroutineDispatcher can
// depend on it without creating a cycle (service → dispatcher → service).
type jobProcessor struct {
	modelRepo  domain.ModelRepository
	configRepo domain.ModelConfigurationRepository
	jobRepo    domain.JobRepository
	proxy      Proxy
}

// NewJobProcessor constructs a JobProcessor that executes jobs synchronously.
func NewJobProcessor(
	modelRepo domain.ModelRepository,
	configRepo domain.ModelConfigurationRepository,
	jobRepo domain.JobRepository,
	proxy Proxy,
) JobProcessor {
	return &jobProcessor{
		modelRepo:  modelRepo,
		configRepo: configRepo,
		jobRepo:    jobRepo,
		proxy:      proxy,
	}
}

// ProcessJob transitions a PENDING job to RUNNING, calls the upstream model,
// and marks it COMPLETED or FAILED. Safe to call from a Lambda SQS trigger.
func (p *jobProcessor) ProcessJob(ctx context.Context, jobID string) error {
	job, err := p.jobRepo.FindByIDForWorker(ctx, jobID)
	if err != nil {
		return err
	}

	if job.Status != domain.JobStatusPending {
		return nil // already running or done — idempotent
	}

	now := time.Now()
	job.Status = domain.JobStatusRunning
	job.StartedAt = &now
	if err := p.jobRepo.Update(ctx, job); err != nil {
		return err
	}

	model, err := p.modelRepo.FindByID(ctx, job.OrgID, job.ModelID)
	if err != nil {
		return p.failJob(ctx, job, err)
	}

	def, ok := domain.FindModelDefinition(model.ModelDefinitionKey)
	if !ok {
		return p.failJob(ctx, job, domain.ErrModelDefinitionNotFound)
	}

	cfg, err := p.configRepo.FindByModelID(ctx, job.OrgID, job.ModelID)
	if err != nil {
		return p.failJob(ctx, job, domain.ErrModelNotConfigured)
	}

	resp, err := p.proxy.Send(ctx, &def, cfg, &ProxyRequest{
		Fields:  job.Fields,
		Options: job.Options,
	})
	if err != nil {
		return p.failJob(ctx, job, err)
	}

	finished := time.Now()
	job.Status = domain.JobStatusCompleted
	job.Result = resp.Content
	job.FinishedAt = &finished
	if err := p.jobRepo.Update(ctx, job); err != nil {
		return err
	}
	fireWebhook(job)
	return nil
}

func (p *jobProcessor) failJob(ctx context.Context, job *domain.Job, reason error) error {
	now := time.Now()
	job.Status = domain.JobStatusFailed
	job.ErrorMessage = reason.Error()
	job.FinishedAt = &now
	if err := p.jobRepo.Update(ctx, job); err != nil {
		return err
	}
	fireWebhook(job)
	return nil
}

// fireWebhook sends a best-effort POST to job.CallbackURL with the job JSON
// as the body. Failures are logged and silently ignored.
func fireWebhook(job *domain.Job) {
	if job.CallbackURL == "" {
		return
	}
	b, err := json.Marshal(job)
	if err != nil {
		return
	}
	go func() {
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Post(job.CallbackURL, "application/json", bytes.NewReader(b))
		if err != nil {
			slog.Error("webhook POST error", "url", job.CallbackURL, "err", err)
			return
		}
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			slog.Warn("webhook POST non-2xx", "url", job.CallbackURL, "status", resp.StatusCode)
		}
	}()
}
