package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// SQSDispatcher publishes a job ID to an SQS queue.
// The worker Lambda consumes these messages and calls ProcessJob.
type SQSDispatcher struct {
	client   *sqs.Client
	queueURL string
}

func NewSQSDispatcher(queueURL string) (*SQSDispatcher, error) {
	cfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return &SQSDispatcher{
		client:   sqs.NewFromConfig(cfg),
		queueURL: queueURL,
	}, nil
}

func (d *SQSDispatcher) Dispatch(ctx context.Context, jobID string) error {
	body, _ := json.Marshal(map[string]string{"jobId": jobID})
	_, err := d.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(d.queueURL),
		MessageBody: aws.String(string(body)),
	})
	if err != nil {
		return fmt.Errorf("sqs send: %w", err)
	}
	return nil
}
