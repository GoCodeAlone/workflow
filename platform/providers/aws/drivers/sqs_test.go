//go:build aws

package drivers

import (
	"context"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/GoCodeAlone/workflow/platform"
)

type mockSQSClient struct {
	createFunc    func(ctx context.Context, params *sqs.CreateQueueInput, optFns ...func(*sqs.Options)) (*sqs.CreateQueueOutput, error)
	getURLFunc    func(ctx context.Context, params *sqs.GetQueueUrlInput, optFns ...func(*sqs.Options)) (*sqs.GetQueueUrlOutput, error)
	getAttrsFunc  func(ctx context.Context, params *sqs.GetQueueAttributesInput, optFns ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error)
	setAttrsFunc  func(ctx context.Context, params *sqs.SetQueueAttributesInput, optFns ...func(*sqs.Options)) (*sqs.SetQueueAttributesOutput, error)
	deleteFunc    func(ctx context.Context, params *sqs.DeleteQueueInput, optFns ...func(*sqs.Options)) (*sqs.DeleteQueueOutput, error)
}

func (m *mockSQSClient) CreateQueue(ctx context.Context, params *sqs.CreateQueueInput, optFns ...func(*sqs.Options)) (*sqs.CreateQueueOutput, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, params, optFns...)
	}
	return &sqs.CreateQueueOutput{
		QueueUrl: awsv2.String("https://sqs.us-east-1.amazonaws.com/123456789/test-queue"),
	}, nil
}

func (m *mockSQSClient) GetQueueUrl(ctx context.Context, params *sqs.GetQueueUrlInput, optFns ...func(*sqs.Options)) (*sqs.GetQueueUrlOutput, error) {
	if m.getURLFunc != nil {
		return m.getURLFunc(ctx, params, optFns...)
	}
	return &sqs.GetQueueUrlOutput{
		QueueUrl: awsv2.String("https://sqs.us-east-1.amazonaws.com/123456789/test-queue"),
	}, nil
}

func (m *mockSQSClient) GetQueueAttributes(ctx context.Context, params *sqs.GetQueueAttributesInput, optFns ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error) {
	if m.getAttrsFunc != nil {
		return m.getAttrsFunc(ctx, params, optFns...)
	}
	return &sqs.GetQueueAttributesOutput{
		Attributes: map[string]string{
			"VisibilityTimeout":      "30",
			"MessageRetentionPeriod": "345600",
			"QueueArn":               "arn:aws:sqs:us-east-1:123456789:test-queue",
		},
	}, nil
}

func (m *mockSQSClient) SetQueueAttributes(ctx context.Context, params *sqs.SetQueueAttributesInput, optFns ...func(*sqs.Options)) (*sqs.SetQueueAttributesOutput, error) {
	if m.setAttrsFunc != nil {
		return m.setAttrsFunc(ctx, params, optFns...)
	}
	return &sqs.SetQueueAttributesOutput{}, nil
}

func (m *mockSQSClient) DeleteQueue(ctx context.Context, params *sqs.DeleteQueueInput, optFns ...func(*sqs.Options)) (*sqs.DeleteQueueOutput, error) {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, params, optFns...)
	}
	return &sqs.DeleteQueueOutput{}, nil
}

func TestSQSDriver_ResourceType(t *testing.T) {
	d := NewSQSDriverWithClient(&mockSQSClient{})
	if d.ResourceType() != "aws.sqs" {
		t.Errorf("ResourceType() = %q, want aws.sqs", d.ResourceType())
	}
}

func TestSQSDriver_Create(t *testing.T) {
	d := NewSQSDriverWithClient(&mockSQSClient{})
	ctx := context.Background()

	out, err := d.Create(ctx, "test-queue", map[string]any{
		"visibility_timeout": 60,
		"retention_period":   86400,
	})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if out.Name != "test-queue" {
		t.Errorf("Name = %q, want test-queue", out.Name)
	}
	if out.ProviderType != "aws.sqs" {
		t.Errorf("ProviderType = %q, want aws.sqs", out.ProviderType)
	}
	if out.Endpoint == "" {
		t.Error("expected non-empty endpoint (queue URL)")
	}
}

func TestSQSDriver_CreateFIFO(t *testing.T) {
	d := NewSQSDriverWithClient(&mockSQSClient{})
	ctx := context.Background()

	out, err := d.Create(ctx, "test-queue", map[string]any{
		"fifo": true,
	})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	// FIFO queues get .fifo suffix
	if out.Name != "test-queue.fifo" {
		t.Errorf("Name = %q, want test-queue.fifo", out.Name)
	}
}

func TestSQSDriver_Read(t *testing.T) {
	d := NewSQSDriverWithClient(&mockSQSClient{})
	ctx := context.Background()

	out, err := d.Read(ctx, "test-queue")
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if out.Endpoint == "" {
		t.Error("expected queue URL endpoint")
	}
	if out.Properties["QueueArn"] != "arn:aws:sqs:us-east-1:123456789:test-queue" {
		t.Errorf("QueueArn = %v", out.Properties["QueueArn"])
	}
}

func TestSQSDriver_Update(t *testing.T) {
	d := NewSQSDriverWithClient(&mockSQSClient{})
	ctx := context.Background()

	out, err := d.Update(ctx, "test-queue",
		map[string]any{"queue_url": "https://sqs.us-east-1.amazonaws.com/123456789/test-queue"},
		map[string]any{"visibility_timeout": 120},
	)
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if out == nil {
		t.Fatal("Update() returned nil")
	}
}

func TestSQSDriver_Delete(t *testing.T) {
	d := NewSQSDriverWithClient(&mockSQSClient{})
	ctx := context.Background()

	if err := d.Delete(ctx, "test-queue"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
}

func TestSQSDriver_Scale(t *testing.T) {
	d := NewSQSDriverWithClient(&mockSQSClient{})
	ctx := context.Background()

	_, err := d.Scale(ctx, "test-queue", nil)
	if err == nil {
		t.Fatal("expected NotScalableError")
	}
	if _, ok := err.(*platform.NotScalableError); !ok {
		t.Errorf("expected NotScalableError, got %T", err)
	}
}

func TestSQSDriver_HealthCheck(t *testing.T) {
	d := NewSQSDriverWithClient(&mockSQSClient{})
	ctx := context.Background()

	health, err := d.HealthCheck(ctx, "test-queue")
	if err != nil {
		t.Fatalf("HealthCheck() error: %v", err)
	}
	if health.Status != "healthy" {
		t.Errorf("health = %q, want healthy", health.Status)
	}
}
