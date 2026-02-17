//go:build aws

package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"

	"github.com/GoCodeAlone/workflow/platform"
)

// S3Client defines the S3 operations used by the state store.
type S3Client interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// DynamoDBClient defines the DynamoDB operations used by the state store.
type DynamoDBClient interface {
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
}

// AWSS3StateStore implements platform.StateStore using S3 for state and DynamoDB for locking.
type AWSS3StateStore struct {
	s3Client  S3Client
	dbClient  DynamoDBClient
	bucket    string
	table     string
	lockTable string
}

// NewAWSS3StateStore creates a state store backed by S3 and DynamoDB.
func NewAWSS3StateStore(cfg awsSDKConfig, bucket, table string) *AWSS3StateStore {
	if bucket == "" {
		bucket = "workflow-platform-state"
	}
	if table == "" {
		table = "workflow-platform-state"
	}
	return &AWSS3StateStore{
		s3Client:  s3.NewFromConfig(cfg),
		dbClient:  dynamodb.NewFromConfig(cfg),
		bucket:    bucket,
		table:     table,
		lockTable: table + "-locks",
	}
}

func (s *AWSS3StateStore) s3Key(contextPath, resourceName string) string {
	return fmt.Sprintf("state/%s/%s.json", contextPath, resourceName)
}

func (s *AWSS3StateStore) SaveResource(ctx context.Context, contextPath string, output *platform.ResourceOutput) error {
	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("aws state: marshal resource: %w", err)
	}

	key := s.s3Key(contextPath, output.Name)
	_, err = s.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("aws state: put s3 object: %w", err)
	}

	// Also index in DynamoDB for listing
	_, err = s.dbClient.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.table),
		Item: map[string]dbtypes.AttributeValue{
			"pk":           &dbtypes.AttributeValueMemberS{Value: contextPath},
			"sk":           &dbtypes.AttributeValueMemberS{Value: output.Name},
			"resourceType": &dbtypes.AttributeValueMemberS{Value: output.ProviderType},
			"status":       &dbtypes.AttributeValueMemberS{Value: string(output.Status)},
			"data":         &dbtypes.AttributeValueMemberS{Value: string(data)},
		},
	})
	if err != nil {
		return fmt.Errorf("aws state: put dynamodb item: %w", err)
	}

	return nil
}

func (s *AWSS3StateStore) GetResource(ctx context.Context, contextPath, resourceName string) (*platform.ResourceOutput, error) {
	result, err := s.dbClient.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.table),
		Key: map[string]dbtypes.AttributeValue{
			"pk": &dbtypes.AttributeValueMemberS{Value: contextPath},
			"sk": &dbtypes.AttributeValueMemberS{Value: resourceName},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("aws state: get dynamodb item: %w", err)
	}
	if result.Item == nil {
		return nil, &platform.ResourceNotFoundError{Name: resourceName, Provider: ProviderName}
	}

	dataAttr, ok := result.Item["data"].(*dbtypes.AttributeValueMemberS)
	if !ok {
		return nil, fmt.Errorf("aws state: invalid data attribute type")
	}

	var output platform.ResourceOutput
	if err := json.Unmarshal([]byte(dataAttr.Value), &output); err != nil {
		return nil, fmt.Errorf("aws state: unmarshal resource: %w", err)
	}
	return &output, nil
}

func (s *AWSS3StateStore) ListResources(ctx context.Context, contextPath string) ([]*platform.ResourceOutput, error) {
	result, err := s.dbClient.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.table),
		KeyConditionExpression: aws.String("pk = :pk"),
		ExpressionAttributeValues: map[string]dbtypes.AttributeValue{
			":pk": &dbtypes.AttributeValueMemberS{Value: contextPath},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("aws state: query dynamodb: %w", err)
	}

	var resources []*platform.ResourceOutput
	for _, item := range result.Items {
		dataAttr, ok := item["data"].(*dbtypes.AttributeValueMemberS)
		if !ok {
			continue
		}
		var output platform.ResourceOutput
		if err := json.Unmarshal([]byte(dataAttr.Value), &output); err != nil {
			continue
		}
		resources = append(resources, &output)
	}
	return resources, nil
}

func (s *AWSS3StateStore) DeleteResource(ctx context.Context, contextPath, resourceName string) error {
	// Delete from S3
	key := s.s3Key(contextPath, resourceName)
	_, err := s.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("aws state: delete s3 object: %w", err)
	}

	// Delete from DynamoDB
	_, err = s.dbClient.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.table),
		Key: map[string]dbtypes.AttributeValue{
			"pk": &dbtypes.AttributeValueMemberS{Value: contextPath},
			"sk": &dbtypes.AttributeValueMemberS{Value: resourceName},
		},
	})
	if err != nil {
		return fmt.Errorf("aws state: delete dynamodb item: %w", err)
	}
	return nil
}

func (s *AWSS3StateStore) SavePlan(ctx context.Context, plan *platform.Plan) error {
	data, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("aws state: marshal plan: %w", err)
	}

	_, err = s.dbClient.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.table),
		Item: map[string]dbtypes.AttributeValue{
			"pk":        &dbtypes.AttributeValueMemberS{Value: "plan:" + plan.Context},
			"sk":        &dbtypes.AttributeValueMemberS{Value: plan.ID},
			"status":    &dbtypes.AttributeValueMemberS{Value: plan.Status},
			"createdAt": &dbtypes.AttributeValueMemberS{Value: plan.CreatedAt.Format(time.RFC3339)},
			"data":      &dbtypes.AttributeValueMemberS{Value: string(data)},
		},
	})
	if err != nil {
		return fmt.Errorf("aws state: put plan: %w", err)
	}
	return nil
}

func (s *AWSS3StateStore) GetPlan(ctx context.Context, planID string) (*platform.Plan, error) {
	// Scan for plan by ID across contexts (simplified; production would use a GSI)
	result, err := s.dbClient.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.table),
		KeyConditionExpression: aws.String("sk = :sk"),
		ExpressionAttributeValues: map[string]dbtypes.AttributeValue{
			":sk": &dbtypes.AttributeValueMemberS{Value: planID},
		},
		IndexName: aws.String("sk-index"),
	})
	if err != nil {
		return nil, fmt.Errorf("aws state: query plan: %w", err)
	}
	if len(result.Items) == 0 {
		return nil, fmt.Errorf("plan %q not found", planID)
	}

	dataAttr, ok := result.Items[0]["data"].(*dbtypes.AttributeValueMemberS)
	if !ok {
		return nil, fmt.Errorf("aws state: invalid plan data attribute")
	}

	var plan platform.Plan
	if err := json.Unmarshal([]byte(dataAttr.Value), &plan); err != nil {
		return nil, fmt.Errorf("aws state: unmarshal plan: %w", err)
	}
	return &plan, nil
}

func (s *AWSS3StateStore) ListPlans(ctx context.Context, contextPath string, limit int) ([]*platform.Plan, error) {
	input := &dynamodb.QueryInput{
		TableName:              aws.String(s.table),
		KeyConditionExpression: aws.String("pk = :pk"),
		ExpressionAttributeValues: map[string]dbtypes.AttributeValue{
			":pk": &dbtypes.AttributeValueMemberS{Value: "plan:" + contextPath},
		},
		ScanIndexForward: aws.Bool(false), // newest first
	}
	if limit > 0 {
		input.Limit = aws.Int32(int32(limit))
	}

	result, err := s.dbClient.Query(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("aws state: list plans: %w", err)
	}

	var plans []*platform.Plan
	for _, item := range result.Items {
		dataAttr, ok := item["data"].(*dbtypes.AttributeValueMemberS)
		if !ok {
			continue
		}
		var plan platform.Plan
		if err := json.Unmarshal([]byte(dataAttr.Value), &plan); err != nil {
			continue
		}
		plans = append(plans, &plan)
	}
	return plans, nil
}

func (s *AWSS3StateStore) Lock(ctx context.Context, contextPath string, ttl time.Duration) (platform.LockHandle, error) {
	lockID := uuid.New().String()
	expiresAt := time.Now().Add(ttl)

	_, err := s.dbClient.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.lockTable),
		Item: map[string]dbtypes.AttributeValue{
			"pk":        &dbtypes.AttributeValueMemberS{Value: contextPath},
			"lockId":    &dbtypes.AttributeValueMemberS{Value: lockID},
			"expiresAt": &dbtypes.AttributeValueMemberS{Value: expiresAt.Format(time.RFC3339)},
		},
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
	})
	if err != nil {
		return nil, &platform.LockConflictError{ContextPath: contextPath}
	}

	return &dynamoDBLock{
		client:      s.dbClient,
		table:       s.lockTable,
		contextPath: contextPath,
		lockID:      lockID,
	}, nil
}

func (s *AWSS3StateStore) Dependencies(ctx context.Context, contextPath, resourceName string) ([]platform.DependencyRef, error) {
	result, err := s.dbClient.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.table),
		KeyConditionExpression: aws.String("pk = :pk AND begins_with(sk, :prefix)"),
		ExpressionAttributeValues: map[string]dbtypes.AttributeValue{
			":pk":     &dbtypes.AttributeValueMemberS{Value: "dep:" + contextPath},
			":prefix": &dbtypes.AttributeValueMemberS{Value: resourceName + ":"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("aws state: query dependencies: %w", err)
	}

	var deps []platform.DependencyRef
	for _, item := range result.Items {
		dataAttr, ok := item["data"].(*dbtypes.AttributeValueMemberS)
		if !ok {
			continue
		}
		var dep platform.DependencyRef
		if err := json.Unmarshal([]byte(dataAttr.Value), &dep); err != nil {
			continue
		}
		deps = append(deps, dep)
	}
	return deps, nil
}

func (s *AWSS3StateStore) AddDependency(ctx context.Context, dep platform.DependencyRef) error {
	data, err := json.Marshal(dep)
	if err != nil {
		return fmt.Errorf("aws state: marshal dependency: %w", err)
	}

	sk := dep.SourceResource + ":" + dep.TargetContext + "/" + dep.TargetResource
	_, err = s.dbClient.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.table),
		Item: map[string]dbtypes.AttributeValue{
			"pk":   &dbtypes.AttributeValueMemberS{Value: "dep:" + dep.SourceContext},
			"sk":   &dbtypes.AttributeValueMemberS{Value: sk},
			"data": &dbtypes.AttributeValueMemberS{Value: string(data)},
		},
	})
	if err != nil {
		return fmt.Errorf("aws state: put dependency: %w", err)
	}
	return nil
}

// Verify interface compliance.
var _ platform.StateStore = (*AWSS3StateStore)(nil)

// dynamoDBLock implements platform.LockHandle using DynamoDB conditional writes.
type dynamoDBLock struct {
	client      DynamoDBClient
	table       string
	contextPath string
	lockID      string
	mu          sync.Mutex
	released    bool
}

func (l *dynamoDBLock) Unlock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.released {
		return nil
	}

	_, err := l.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(l.table),
		Key: map[string]dbtypes.AttributeValue{
			"pk": &dbtypes.AttributeValueMemberS{Value: l.contextPath},
		},
		ConditionExpression: aws.String("lockId = :lid"),
		ExpressionAttributeValues: map[string]dbtypes.AttributeValue{
			":lid": &dbtypes.AttributeValueMemberS{Value: l.lockID},
		},
	})
	if err != nil {
		return fmt.Errorf("aws state: unlock: %w", err)
	}
	l.released = true
	return nil
}

func (l *dynamoDBLock) Refresh(ctx context.Context, ttl time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.released {
		return fmt.Errorf("lock already released")
	}

	newExpiry := time.Now().Add(ttl)
	_, err := l.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(l.table),
		Item: map[string]dbtypes.AttributeValue{
			"pk":        &dbtypes.AttributeValueMemberS{Value: l.contextPath},
			"lockId":    &dbtypes.AttributeValueMemberS{Value: l.lockID},
			"expiresAt": &dbtypes.AttributeValueMemberS{Value: newExpiry.Format(time.RFC3339)},
		},
		ConditionExpression: aws.String("lockId = :lid"),
		ExpressionAttributeValues: map[string]dbtypes.AttributeValue{
			":lid": &dbtypes.AttributeValueMemberS{Value: l.lockID},
		},
	})
	if err != nil {
		return fmt.Errorf("aws state: refresh lock: %w", err)
	}
	return nil
}

var _ platform.LockHandle = (*dynamoDBLock)(nil)
