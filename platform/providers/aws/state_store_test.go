//go:build aws

package aws

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/GoCodeAlone/workflow/platform"
)

type mockS3Client struct {
	putFunc    func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	getFunc    func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	deleteFunc func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

func (m *mockS3Client) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if m.putFunc != nil {
		return m.putFunc(ctx, params, optFns...)
	}
	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3Client) GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, params, optFns...)
	}
	return &s3.GetObjectOutput{}, nil
}

func (m *mockS3Client) DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, params, optFns...)
	}
	return &s3.DeleteObjectOutput{}, nil
}

type mockDynamoDBClient struct {
	putItemFunc    func(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	getItemFunc    func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	deleteItemFunc func(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	queryFunc      func(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
}

func (m *mockDynamoDBClient) PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	if m.putItemFunc != nil {
		return m.putItemFunc(ctx, params, optFns...)
	}
	return &dynamodb.PutItemOutput{}, nil
}

func (m *mockDynamoDBClient) GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	if m.getItemFunc != nil {
		return m.getItemFunc(ctx, params, optFns...)
	}
	return &dynamodb.GetItemOutput{}, nil
}

func (m *mockDynamoDBClient) DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	if m.deleteItemFunc != nil {
		return m.deleteItemFunc(ctx, params, optFns...)
	}
	return &dynamodb.DeleteItemOutput{}, nil
}

func (m *mockDynamoDBClient) Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	if m.queryFunc != nil {
		return m.queryFunc(ctx, params, optFns...)
	}
	return &dynamodb.QueryOutput{}, nil
}

func newTestStateStore(s3c S3Client, dbc DynamoDBClient) *AWSS3StateStore {
	return &AWSS3StateStore{
		s3Client:  s3c,
		dbClient:  dbc,
		bucket:    "test-bucket",
		table:     "test-table",
		lockTable: "test-table-locks",
	}
}

func TestStateStore_SaveResource(t *testing.T) {
	s3Called := false
	dbCalled := false

	store := newTestStateStore(
		&mockS3Client{
			putFunc: func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
				s3Called = true
				if *params.Bucket != "test-bucket" {
					t.Errorf("bucket = %q, want test-bucket", *params.Bucket)
				}
				return &s3.PutObjectOutput{}, nil
			},
		},
		&mockDynamoDBClient{
			putItemFunc: func(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
				dbCalled = true
				if *params.TableName != "test-table" {
					t.Errorf("table = %q, want test-table", *params.TableName)
				}
				return &dynamodb.PutItemOutput{}, nil
			},
		},
	)

	ctx := context.Background()
	output := &platform.ResourceOutput{
		Name:         "test-vpc",
		Type:         "network",
		ProviderType: "aws.vpc",
		Status:       platform.ResourceStatusActive,
	}

	err := store.SaveResource(ctx, "acme/prod", output)
	if err != nil {
		t.Fatalf("SaveResource() error: %v", err)
	}
	if !s3Called {
		t.Error("S3 PutObject not called")
	}
	if !dbCalled {
		t.Error("DynamoDB PutItem not called")
	}
}

func TestStateStore_GetResource(t *testing.T) {
	output := &platform.ResourceOutput{
		Name:         "test-vpc",
		Type:         "network",
		ProviderType: "aws.vpc",
		Status:       platform.ResourceStatusActive,
		Properties:   map[string]any{"cidr": "10.0.0.0/16"},
	}
	data, _ := json.Marshal(output)

	store := newTestStateStore(
		&mockS3Client{},
		&mockDynamoDBClient{
			getItemFunc: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
				return &dynamodb.GetItemOutput{
					Item: map[string]dbtypes.AttributeValue{
						"pk":   &dbtypes.AttributeValueMemberS{Value: "acme/prod"},
						"sk":   &dbtypes.AttributeValueMemberS{Value: "test-vpc"},
						"data": &dbtypes.AttributeValueMemberS{Value: string(data)},
					},
				}, nil
			},
		},
	)

	ctx := context.Background()
	result, err := store.GetResource(ctx, "acme/prod", "test-vpc")
	if err != nil {
		t.Fatalf("GetResource() error: %v", err)
	}
	if result.Name != "test-vpc" {
		t.Errorf("Name = %q, want test-vpc", result.Name)
	}
	if result.Status != platform.ResourceStatusActive {
		t.Errorf("Status = %q, want active", result.Status)
	}
}

func TestStateStore_GetResourceNotFound(t *testing.T) {
	store := newTestStateStore(
		&mockS3Client{},
		&mockDynamoDBClient{
			getItemFunc: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
				return &dynamodb.GetItemOutput{Item: nil}, nil
			},
		},
	)

	ctx := context.Background()
	_, err := store.GetResource(ctx, "acme/prod", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent resource")
	}
	if _, ok := err.(*platform.ResourceNotFoundError); !ok {
		t.Errorf("expected ResourceNotFoundError, got %T", err)
	}
}

func TestStateStore_ListResources(t *testing.T) {
	outputs := []*platform.ResourceOutput{
		{Name: "vpc-1", Type: "network", Status: platform.ResourceStatusActive},
		{Name: "db-1", Type: "database", Status: platform.ResourceStatusActive},
	}
	var items []map[string]dbtypes.AttributeValue
	for _, o := range outputs {
		data, _ := json.Marshal(o)
		items = append(items, map[string]dbtypes.AttributeValue{
			"data": &dbtypes.AttributeValueMemberS{Value: string(data)},
		})
	}

	store := newTestStateStore(
		&mockS3Client{},
		&mockDynamoDBClient{
			queryFunc: func(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
				return &dynamodb.QueryOutput{Items: items}, nil
			},
		},
	)

	ctx := context.Background()
	results, err := store.ListResources(ctx, "acme/prod")
	if err != nil {
		t.Fatalf("ListResources() error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(results))
	}
	if results[0].Name != "vpc-1" {
		t.Errorf("results[0].Name = %q, want vpc-1", results[0].Name)
	}
}

func TestStateStore_DeleteResource(t *testing.T) {
	s3Deleted := false
	dbDeleted := false

	store := newTestStateStore(
		&mockS3Client{
			deleteFunc: func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
				s3Deleted = true
				return &s3.DeleteObjectOutput{}, nil
			},
		},
		&mockDynamoDBClient{
			deleteItemFunc: func(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
				dbDeleted = true
				return &dynamodb.DeleteItemOutput{}, nil
			},
		},
	)

	ctx := context.Background()
	err := store.DeleteResource(ctx, "acme/prod", "test-vpc")
	if err != nil {
		t.Fatalf("DeleteResource() error: %v", err)
	}
	if !s3Deleted {
		t.Error("S3 DeleteObject not called")
	}
	if !dbDeleted {
		t.Error("DynamoDB DeleteItem not called")
	}
}

func TestStateStore_SavePlan(t *testing.T) {
	store := newTestStateStore(&mockS3Client{}, &mockDynamoDBClient{})
	ctx := context.Background()

	plan := &platform.Plan{
		ID:        "plan-123",
		Context:   "acme/prod",
		Status:    "pending",
		CreatedAt: time.Now(),
		Actions: []platform.PlanAction{
			{Action: "create", ResourceName: "vpc-1", ResourceType: "aws.vpc"},
		},
	}

	err := store.SavePlan(ctx, plan)
	if err != nil {
		t.Fatalf("SavePlan() error: %v", err)
	}
}

func TestStateStore_GetPlan(t *testing.T) {
	plan := &platform.Plan{
		ID:      "plan-123",
		Context: "acme/prod",
		Status:  "pending",
	}
	data, _ := json.Marshal(plan)

	store := newTestStateStore(
		&mockS3Client{},
		&mockDynamoDBClient{
			queryFunc: func(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
				return &dynamodb.QueryOutput{
					Items: []map[string]dbtypes.AttributeValue{
						{"data": &dbtypes.AttributeValueMemberS{Value: string(data)}},
					},
				}, nil
			},
		},
	)

	ctx := context.Background()
	result, err := store.GetPlan(ctx, "plan-123")
	if err != nil {
		t.Fatalf("GetPlan() error: %v", err)
	}
	if result.ID != "plan-123" {
		t.Errorf("ID = %q, want plan-123", result.ID)
	}
}

func TestStateStore_GetPlanNotFound(t *testing.T) {
	store := newTestStateStore(
		&mockS3Client{},
		&mockDynamoDBClient{
			queryFunc: func(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
				return &dynamodb.QueryOutput{Items: nil}, nil
			},
		},
	)

	ctx := context.Background()
	_, err := store.GetPlan(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent plan")
	}
}

func TestStateStore_ListPlans(t *testing.T) {
	plans := []*platform.Plan{
		{ID: "plan-1", Context: "acme/prod", Status: "applied"},
		{ID: "plan-2", Context: "acme/prod", Status: "pending"},
	}
	var items []map[string]dbtypes.AttributeValue
	for _, p := range plans {
		data, _ := json.Marshal(p)
		items = append(items, map[string]dbtypes.AttributeValue{
			"data": &dbtypes.AttributeValueMemberS{Value: string(data)},
		})
	}

	store := newTestStateStore(
		&mockS3Client{},
		&mockDynamoDBClient{
			queryFunc: func(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
				return &dynamodb.QueryOutput{Items: items}, nil
			},
		},
	)

	ctx := context.Background()
	results, err := store.ListPlans(ctx, "acme/prod", 10)
	if err != nil {
		t.Fatalf("ListPlans() error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(results))
	}
}

func TestStateStore_Lock(t *testing.T) {
	store := newTestStateStore(&mockS3Client{}, &mockDynamoDBClient{})
	ctx := context.Background()

	handle, err := store.Lock(ctx, "acme/prod", 5*time.Minute)
	if err != nil {
		t.Fatalf("Lock() error: %v", err)
	}
	if handle == nil {
		t.Fatal("Lock() returned nil handle")
	}

	// Unlock
	err = handle.Unlock(ctx)
	if err != nil {
		t.Fatalf("Unlock() error: %v", err)
	}

	// Double unlock should be idempotent
	err = handle.Unlock(ctx)
	if err != nil {
		t.Fatalf("double Unlock() error: %v", err)
	}
}

func TestStateStore_LockRefresh(t *testing.T) {
	store := newTestStateStore(&mockS3Client{}, &mockDynamoDBClient{})
	ctx := context.Background()

	handle, err := store.Lock(ctx, "acme/prod", 5*time.Minute)
	if err != nil {
		t.Fatalf("Lock() error: %v", err)
	}

	err = handle.Refresh(ctx, 10*time.Minute)
	if err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}
}

func TestStateStore_LockRefreshAfterRelease(t *testing.T) {
	store := newTestStateStore(&mockS3Client{}, &mockDynamoDBClient{})
	ctx := context.Background()

	handle, err := store.Lock(ctx, "acme/prod", 5*time.Minute)
	if err != nil {
		t.Fatalf("Lock() error: %v", err)
	}

	_ = handle.Unlock(ctx)
	err = handle.Refresh(ctx, 10*time.Minute)
	if err == nil {
		t.Error("expected error refreshing released lock")
	}
}

func TestStateStore_Dependencies(t *testing.T) {
	dep := platform.DependencyRef{
		SourceContext:  "acme/prod",
		SourceResource: "vpc-1",
		TargetContext:  "acme/prod",
		TargetResource: "eks-1",
		Type:           "hard",
	}
	data, _ := json.Marshal(dep)

	store := newTestStateStore(
		&mockS3Client{},
		&mockDynamoDBClient{
			queryFunc: func(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
				return &dynamodb.QueryOutput{
					Items: []map[string]dbtypes.AttributeValue{
						{"data": &dbtypes.AttributeValueMemberS{Value: string(data)}},
					},
				}, nil
			},
		},
	)

	ctx := context.Background()
	deps, err := store.Dependencies(ctx, "acme/prod", "vpc-1")
	if err != nil {
		t.Fatalf("Dependencies() error: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(deps))
	}
	if deps[0].TargetResource != "eks-1" {
		t.Errorf("TargetResource = %q, want eks-1", deps[0].TargetResource)
	}
}

func TestStateStore_AddDependency(t *testing.T) {
	store := newTestStateStore(&mockS3Client{}, &mockDynamoDBClient{})
	ctx := context.Background()

	dep := platform.DependencyRef{
		SourceContext:  "acme/prod",
		SourceResource: "vpc-1",
		TargetContext:  "acme/prod",
		TargetResource: "eks-1",
		Type:           "hard",
	}

	err := store.AddDependency(ctx, dep)
	if err != nil {
		t.Fatalf("AddDependency() error: %v", err)
	}
}

func TestStateStore_S3Key(t *testing.T) {
	store := newTestStateStore(&mockS3Client{}, &mockDynamoDBClient{})
	key := store.s3Key("acme/prod", "vpc-1")
	if key != "state/acme/prod/vpc-1.json" {
		t.Errorf("s3Key = %q, want state/acme/prod/vpc-1.json", key)
	}
}

func TestProvider_InitializedMapCapability(t *testing.T) {
	ap := &AWSProvider{
		initialized:      true,
		capabilityMapper: NewAWSCapabilityMapper(),
		drivers:          make(map[string]platform.ResourceDriver),
	}
	ctx := context.Background()

	plans, err := ap.MapCapability(ctx, platform.CapabilityDeclaration{
		Name: "test-vpc",
		Type: "network",
		Properties: map[string]any{
			"cidr": "10.0.0.0/16",
		},
	}, nil)
	if err != nil {
		t.Fatalf("MapCapability() error: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}
}

func TestProvider_InitializedMapCapabilityUnsupported(t *testing.T) {
	ap := &AWSProvider{
		initialized:      true,
		capabilityMapper: NewAWSCapabilityMapper(),
		drivers:          make(map[string]platform.ResourceDriver),
	}
	ctx := context.Background()

	_, err := ap.MapCapability(ctx, platform.CapabilityDeclaration{
		Name: "test",
		Type: "magic_service",
	}, nil)
	if err == nil {
		t.Fatal("expected error for unsupported capability")
	}
}

func TestProvider_CredentialBrokerAndStateStore(t *testing.T) {
	broker := &AWSCredentialBroker{}
	stateStore := &AWSS3StateStore{}
	ap := &AWSProvider{
		initialized: true,
		credBroker:  broker,
		stateStore:  stateStore,
		drivers:     make(map[string]platform.ResourceDriver),
	}

	if ap.CredentialBroker() != broker {
		t.Error("CredentialBroker() returned wrong instance")
	}
	if ap.StateStore() != stateStore {
		t.Error("StateStore() returned wrong instance")
	}
}

func TestProvider_Healthy(t *testing.T) {
	ap := &AWSProvider{
		initialized: true,
		drivers:     make(map[string]platform.ResourceDriver),
	}
	ctx := context.Background()

	if err := ap.Healthy(ctx); err != nil {
		t.Errorf("Healthy() = %v, want nil", err)
	}
}
