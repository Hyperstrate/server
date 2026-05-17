package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"

	"hyperstrate/server/internal/app"
	"hyperstrate/server/internal/modules/ai/application"
	"hyperstrate/server/internal/shared/logger"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

var processor application.JobProcessor

func init() {
	logger.Init()

	workerApp := app.NewWorkerApp(&processor)
	if err := workerApp.Start(context.Background()); err != nil {
		slog.Error("failed to start worker app", "err", err)
		os.Exit(1)
	}
}

// handler processes one SQS batch. BatchSize is set to 1 in the SAM template
// so each invocation handles exactly one job.
// Returning an error causes SQS to retry (up to maxReceiveCount) then route to DLQ.
func handler(ctx context.Context, event events.SQSEvent) error {
	for _, record := range event.Records {
		var msg struct {
			JobID string `json:"jobId"`
		}
		if err := json.Unmarshal([]byte(record.Body), &msg); err != nil {
			// Malformed message — log and skip; no retry benefit.
			slog.Error("skipping unparseable SQS message", "messageId", record.MessageId, "err", err)
			continue
		}
		if msg.JobID == "" {
			slog.Warn("skipping SQS message: missing jobId", "messageId", record.MessageId)
			continue
		}
		if err := processor.ProcessJob(ctx, msg.JobID); err != nil {
			// Return the error so Lambda reports a partial batch failure.
			// With ReportBatchItemFailures the message is retried and eventually DLQ'd.
			slog.Error("job failed", "jobId", msg.JobID, "err", err)
			return err
		}
		slog.Info("job completed", "jobId", msg.JobID)
	}
	return nil
}

func main() {
	lambda.Start(handler)
}
