//go:build aws

package drivers

import (
	"context"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"

	"github.com/GoCodeAlone/workflow/platform"
)

type mockRDSClient struct {
	createFunc   func(ctx context.Context, params *rds.CreateDBInstanceInput, optFns ...func(*rds.Options)) (*rds.CreateDBInstanceOutput, error)
	describeFunc func(ctx context.Context, params *rds.DescribeDBInstancesInput, optFns ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error)
	modifyFunc   func(ctx context.Context, params *rds.ModifyDBInstanceInput, optFns ...func(*rds.Options)) (*rds.ModifyDBInstanceOutput, error)
	deleteFunc   func(ctx context.Context, params *rds.DeleteDBInstanceInput, optFns ...func(*rds.Options)) (*rds.DeleteDBInstanceOutput, error)
}

func (m *mockRDSClient) CreateDBInstance(ctx context.Context, params *rds.CreateDBInstanceInput, optFns ...func(*rds.Options)) (*rds.CreateDBInstanceOutput, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, params, optFns...)
	}
	return &rds.CreateDBInstanceOutput{
		DBInstance: &rdstypes.DBInstance{
			DBInstanceIdentifier: params.DBInstanceIdentifier,
			Engine:               params.Engine,
			DBInstanceClass:      params.DBInstanceClass,
			AllocatedStorage:     params.AllocatedStorage,
			MultiAZ:              params.MultiAZ,
			DBInstanceStatus:     awsv2.String("creating"),
			DBInstanceArn:        awsv2.String("arn:aws:rds:us-east-1:123456789:db/test"),
		},
	}, nil
}

func (m *mockRDSClient) DescribeDBInstances(ctx context.Context, params *rds.DescribeDBInstancesInput, optFns ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error) {
	if m.describeFunc != nil {
		return m.describeFunc(ctx, params, optFns...)
	}
	return &rds.DescribeDBInstancesOutput{
		DBInstances: []rdstypes.DBInstance{
			{
				DBInstanceIdentifier: params.DBInstanceIdentifier,
				Engine:               awsv2.String("postgres"),
				EngineVersion:        awsv2.String("15.4"),
				DBInstanceClass:      awsv2.String("db.t3.micro"),
				AllocatedStorage:     awsv2.Int32(20),
				MultiAZ:              awsv2.Bool(false),
				DBInstanceStatus:     awsv2.String("available"),
				DBInstanceArn:        awsv2.String("arn:aws:rds:us-east-1:123456789:db/test"),
				Endpoint: &rdstypes.Endpoint{
					Address: awsv2.String("test-db.cxx.us-east-1.rds.amazonaws.com"),
					Port:    awsv2.Int32(5432),
				},
			},
		},
	}, nil
}

func (m *mockRDSClient) ModifyDBInstance(ctx context.Context, params *rds.ModifyDBInstanceInput, optFns ...func(*rds.Options)) (*rds.ModifyDBInstanceOutput, error) {
	if m.modifyFunc != nil {
		return m.modifyFunc(ctx, params, optFns...)
	}
	return &rds.ModifyDBInstanceOutput{
		DBInstance: &rdstypes.DBInstance{
			DBInstanceIdentifier: params.DBInstanceIdentifier,
			DBInstanceStatus:     awsv2.String("modifying"),
		},
	}, nil
}

func (m *mockRDSClient) DeleteDBInstance(ctx context.Context, params *rds.DeleteDBInstanceInput, optFns ...func(*rds.Options)) (*rds.DeleteDBInstanceOutput, error) {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, params, optFns...)
	}
	return &rds.DeleteDBInstanceOutput{}, nil
}

func TestRDSDriver_ResourceType(t *testing.T) {
	d := NewRDSDriverWithClient(&mockRDSClient{})
	if d.ResourceType() != "aws.rds" {
		t.Errorf("ResourceType() = %q, want aws.rds", d.ResourceType())
	}
}

func TestRDSDriver_Create(t *testing.T) {
	d := NewRDSDriverWithClient(&mockRDSClient{})
	ctx := context.Background()

	out, err := d.Create(ctx, "test-db", map[string]any{
		"engine":            "postgres",
		"instance_class":    "db.r5.large",
		"allocated_storage": 50,
		"multi_az":          true,
	})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if out.Name != "test-db" {
		t.Errorf("Name = %q, want test-db", out.Name)
	}
	if out.ProviderType != "aws.rds" {
		t.Errorf("ProviderType = %q, want aws.rds", out.ProviderType)
	}
	if out.Status != platform.ResourceStatusCreating {
		t.Errorf("Status = %q, want creating", out.Status)
	}
}

func TestRDSDriver_Read(t *testing.T) {
	d := NewRDSDriverWithClient(&mockRDSClient{})
	ctx := context.Background()

	out, err := d.Read(ctx, "test-db")
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if out.Status != platform.ResourceStatusActive {
		t.Errorf("Status = %q, want active", out.Status)
	}
	if out.Endpoint != "test-db.cxx.us-east-1.rds.amazonaws.com" {
		t.Errorf("Endpoint = %q, want test-db.cxx...", out.Endpoint)
	}
	if out.ConnectionStr != "postgres://test-db.cxx.us-east-1.rds.amazonaws.com:5432" {
		t.Errorf("ConnectionStr = %q", out.ConnectionStr)
	}
	if out.Properties["engine"] != "postgres" {
		t.Errorf("engine = %v, want postgres", out.Properties["engine"])
	}
}

func TestRDSDriver_ReadNotFound(t *testing.T) {
	d := NewRDSDriverWithClient(&mockRDSClient{
		describeFunc: func(ctx context.Context, params *rds.DescribeDBInstancesInput, optFns ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error) {
			return &rds.DescribeDBInstancesOutput{DBInstances: []rdstypes.DBInstance{}}, nil
		},
	})
	ctx := context.Background()

	_, err := d.Read(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent DB")
	}
}

func TestRDSDriver_Update(t *testing.T) {
	d := NewRDSDriverWithClient(&mockRDSClient{})
	ctx := context.Background()

	_, err := d.Update(ctx, "test-db", nil, map[string]any{
		"instance_class": "db.r5.xlarge",
	})
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
}

func TestRDSDriver_Delete(t *testing.T) {
	d := NewRDSDriverWithClient(&mockRDSClient{})
	ctx := context.Background()

	if err := d.Delete(ctx, "test-db"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
}

func TestRDSDriver_Scale(t *testing.T) {
	d := NewRDSDriverWithClient(&mockRDSClient{})
	ctx := context.Background()

	_, err := d.Scale(ctx, "test-db", map[string]any{
		"instance_class": "db.r5.xlarge",
	})
	if err != nil {
		t.Fatalf("Scale() error: %v", err)
	}
}

func TestRDSDriver_ScaleMissingClass(t *testing.T) {
	d := NewRDSDriverWithClient(&mockRDSClient{})
	ctx := context.Background()

	_, err := d.Scale(ctx, "test-db", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing instance_class")
	}
}

func TestRDSDriver_HealthCheck(t *testing.T) {
	d := NewRDSDriverWithClient(&mockRDSClient{})
	ctx := context.Background()

	health, err := d.HealthCheck(ctx, "test-db")
	if err != nil {
		t.Fatalf("HealthCheck() error: %v", err)
	}
	if health.Status != "healthy" {
		t.Errorf("health = %q, want healthy", health.Status)
	}
}
