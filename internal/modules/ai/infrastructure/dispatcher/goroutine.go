package dispatcher

import (
	"context"
	"log/slog"
	"time"

	"hyperstrate/server/internal/modules/ai/application"
)

// GoroutineDispatcher processes jobs inline using a background goroutine.
// Used in local development mode when SQS_QUEUE_URL is not set.
type GoroutineDispatcher struct {
	processor application.JobProcessor
}

func NewGoroutineDispatcher(processor application.JobProcessor) *GoroutineDispatcher {
	return &GoroutineDispatcher{processor: processor}
}

func (d *GoroutineDispatcher) Dispatch(ctx context.Context, jobID string) error {
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := d.processor.ProcessJob(bgCtx, jobID); err != nil {
			slog.Error("goroutine job failed", "jobID", jobID, "err", err)
		}
	}()
	return nil
}
