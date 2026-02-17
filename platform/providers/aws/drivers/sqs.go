//go:build aws

package drivers

import (
	"context"
	"fmt"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/GoCodeAlone/workflow/platform"
)

// SQSClient defines the SQS operations used by the driver.
type SQSClient interface {
	CreateQueue(ctx context.Context, params *sqs.CreateQueueInput, optFns ...func(*sqs.Options)) (*sqs.CreateQueueOutput, error)
	GetQueueUrl(ctx context.Context, params *sqs.GetQueueUrlInput, optFns ...func(*sqs.Options)) (*sqs.GetQueueUrlOutput, error)
	GetQueueAttributes(ctx context.Context, params *sqs.GetQueueAttributesInput, optFns ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error)
	SetQueueAttributes(ctx context.Context, params *sqs.SetQueueAttributesInput, optFns ...func(*sqs.Options)) (*sqs.SetQueueAttributesOutput, error)
	DeleteQueue(ctx context.Context, params *sqs.DeleteQueueInput, optFns ...func(*sqs.Options)) (*sqs.DeleteQueueOutput, error)
}

// SQSDriver manages SQS queue resources.
type SQSDriver struct {
	client SQSClient
}

// NewSQSDriver creates a new SQS driver.
func NewSQSDriver(cfg awsv2.Config) *SQSDriver {
	return &SQSDriver{
		client: sqs.NewFromConfig(cfg),
	}
}

// NewSQSDriverWithClient creates an SQS driver with a custom client.
func NewSQSDriverWithClient(client SQSClient) *SQSDriver {
	return &SQSDriver{client: client}
}

func (d *SQSDriver) ResourceType() string { return "aws.sqs" }

func (d *SQSDriver) Create(ctx context.Context, name string, properties map[string]any) (*platform.ResourceOutput, error) {
	attrs := map[string]string{}
	if fifo := boolPropDrivers(properties, "fifo", false); fifo {
		attrs["FifoQueue"] = "true"
		// FIFO queues require .fifo suffix
		if len(name) < 5 || name[len(name)-5:] != ".fifo" {
			name += ".fifo"
		}
	}
	if vt := intPropDrivers(properties, "visibility_timeout", 30); vt > 0 {
		attrs["VisibilityTimeout"] = fmt.Sprintf("%d", vt)
	}
	if rp := intPropDrivers(properties, "retention_period", 345600); rp > 0 {
		attrs["MessageRetentionPeriod"] = fmt.Sprintf("%d", rp)
	}

	out, err := d.client.CreateQueue(ctx, &sqs.CreateQueueInput{
		QueueName:  awsv2.String(name),
		Attributes: attrs,
	})
	if err != nil {
		return nil, fmt.Errorf("sqs: create queue %q: %w", name, err)
	}

	queueURL := ""
	if out.QueueUrl != nil {
		queueURL = *out.QueueUrl
	}

	return &platform.ResourceOutput{
		Name:         name,
		Type:         "message_queue",
		ProviderType: "aws.sqs",
		Endpoint:     queueURL,
		Properties: map[string]any{
			"queue_url":  queueURL,
			"attributes": attrs,
		},
		Status:     platform.ResourceStatusActive,
		LastSynced: time.Now(),
	}, nil
}

func (d *SQSDriver) Read(ctx context.Context, name string) (*platform.ResourceOutput, error) {
	urlOut, err := d.client.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: awsv2.String(name),
	})
	if err != nil {
		return nil, fmt.Errorf("sqs: get queue url %q: %w", name, err)
	}

	queueURL := ""
	if urlOut.QueueUrl != nil {
		queueURL = *urlOut.QueueUrl
	}

	attrOut, err := d.client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       awsv2.String(queueURL),
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameAll},
	})
	if err != nil {
		return nil, fmt.Errorf("sqs: get queue attributes %q: %w", name, err)
	}

	props := map[string]any{
		"queue_url": queueURL,
	}
	for k, v := range attrOut.Attributes {
		props[k] = v
	}

	return &platform.ResourceOutput{
		Name:         name,
		Type:         "message_queue",
		ProviderType: "aws.sqs",
		Endpoint:     queueURL,
		Properties:   props,
		Status:       platform.ResourceStatusActive,
		LastSynced:   time.Now(),
	}, nil
}

func (d *SQSDriver) Update(ctx context.Context, name string, current, desired map[string]any) (*platform.ResourceOutput, error) {
	queueURL, _ := current["queue_url"].(string)
	if queueURL == "" {
		out, err := d.Read(ctx, name)
		if err != nil {
			return nil, err
		}
		queueURL, _ = out.Properties["queue_url"].(string)
	}

	attrs := map[string]string{}
	if vt := intPropDrivers(desired, "visibility_timeout", 0); vt > 0 {
		attrs["VisibilityTimeout"] = fmt.Sprintf("%d", vt)
	}
	if rp := intPropDrivers(desired, "retention_period", 0); rp > 0 {
		attrs["MessageRetentionPeriod"] = fmt.Sprintf("%d", rp)
	}

	if len(attrs) > 0 {
		_, err := d.client.SetQueueAttributes(ctx, &sqs.SetQueueAttributesInput{
			QueueUrl:   awsv2.String(queueURL),
			Attributes: attrs,
		})
		if err != nil {
			return nil, fmt.Errorf("sqs: update queue %q: %w", name, err)
		}
	}
	return d.Read(ctx, name)
}

func (d *SQSDriver) Delete(ctx context.Context, name string) error {
	urlOut, err := d.client.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: awsv2.String(name),
	})
	if err != nil {
		return fmt.Errorf("sqs: get queue url for delete %q: %w", name, err)
	}

	_, err = d.client.DeleteQueue(ctx, &sqs.DeleteQueueInput{
		QueueUrl: urlOut.QueueUrl,
	})
	if err != nil {
		return fmt.Errorf("sqs: delete queue %q: %w", name, err)
	}
	return nil
}

func (d *SQSDriver) HealthCheck(ctx context.Context, name string) (*platform.HealthStatus, error) {
	_, err := d.Read(ctx, name)
	if err != nil {
		return &platform.HealthStatus{
			Status:    "unhealthy",
			Message:   err.Error(),
			CheckedAt: time.Now(),
		}, nil
	}
	return &platform.HealthStatus{
		Status:    "healthy",
		Message:   "queue accessible",
		CheckedAt: time.Now(),
	}, nil
}

func (d *SQSDriver) Scale(_ context.Context, _ string, _ map[string]any) (*platform.ResourceOutput, error) {
	return nil, &platform.NotScalableError{ResourceType: "aws.sqs"}
}

func (d *SQSDriver) Diff(ctx context.Context, name string, desired map[string]any) ([]platform.DiffEntry, error) {
	current, err := d.Read(ctx, name)
	if err != nil {
		return nil, err
	}
	return diffProperties(current.Properties, desired), nil
}

var _ platform.ResourceDriver = (*SQSDriver)(nil)
