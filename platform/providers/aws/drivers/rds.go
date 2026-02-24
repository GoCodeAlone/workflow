//go:build aws

package drivers

import (
	"context"
	"fmt"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"

	"github.com/GoCodeAlone/workflow/platform"
)

// RDSClient defines the RDS operations used by the driver.
type RDSClient interface {
	CreateDBInstance(ctx context.Context, params *rds.CreateDBInstanceInput, optFns ...func(*rds.Options)) (*rds.CreateDBInstanceOutput, error)
	DescribeDBInstances(ctx context.Context, params *rds.DescribeDBInstancesInput, optFns ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error)
	ModifyDBInstance(ctx context.Context, params *rds.ModifyDBInstanceInput, optFns ...func(*rds.Options)) (*rds.ModifyDBInstanceOutput, error)
	DeleteDBInstance(ctx context.Context, params *rds.DeleteDBInstanceInput, optFns ...func(*rds.Options)) (*rds.DeleteDBInstanceOutput, error)
}

// RDSDriver manages RDS database instances.
type RDSDriver struct {
	client RDSClient
}

// NewRDSDriver creates a new RDS driver.
func NewRDSDriver(cfg awsv2.Config) *RDSDriver {
	return &RDSDriver{
		client: rds.NewFromConfig(cfg),
	}
}

// NewRDSDriverWithClient creates an RDS driver with a custom client.
func NewRDSDriverWithClient(client RDSClient) *RDSDriver {
	return &RDSDriver{client: client}
}

func (d *RDSDriver) ResourceType() string { return "aws.rds" }

func (d *RDSDriver) Create(ctx context.Context, name string, properties map[string]any) (*platform.ResourceOutput, error) {
	engine, _ := properties["engine"].(string)
	engineVersion, _ := properties["engine_version"].(string)
	instanceClass, _ := properties["instance_class"].(string)
	if instanceClass == "" {
		instanceClass = "db.t3.micro"
	}
	allocatedStorage := int32(intPropDrivers(properties, "allocated_storage", 20))
	multiAZ := boolPropDrivers(properties, "multi_az", false)
	masterUser, _ := properties["master_username"].(string)
	if masterUser == "" {
		masterUser = "admin"
	}
	masterPass, _ := properties["master_password"].(string)
	if masterPass == "" {
		return nil, fmt.Errorf("rds: create %q: master_password is required", name)
	}

	input := &rds.CreateDBInstanceInput{
		DBInstanceIdentifier: awsv2.String(name),
		Engine:               awsv2.String(engine),
		DBInstanceClass:      awsv2.String(instanceClass),
		AllocatedStorage:     awsv2.Int32(allocatedStorage),
		MultiAZ:              awsv2.Bool(multiAZ),
		MasterUsername:       awsv2.String(masterUser),
		MasterUserPassword:   awsv2.String(masterPass),
	}
	if engineVersion != "" {
		input.EngineVersion = awsv2.String(engineVersion)
	}

	out, err := d.client.CreateDBInstance(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("rds: create %q: %w", name, err)
	}

	return dbInstanceToOutput(out.DBInstance), nil
}

func (d *RDSDriver) Read(ctx context.Context, name string) (*platform.ResourceOutput, error) {
	out, err := d.client.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: awsv2.String(name),
	})
	if err != nil {
		return nil, fmt.Errorf("rds: describe %q: %w", name, err)
	}
	if len(out.DBInstances) == 0 {
		return nil, &platform.ResourceNotFoundError{Name: name, Provider: "aws"}
	}
	return dbInstanceToOutput(&out.DBInstances[0]), nil
}

func (d *RDSDriver) Update(ctx context.Context, name string, _, desired map[string]any) (*platform.ResourceOutput, error) {
	input := &rds.ModifyDBInstanceInput{
		DBInstanceIdentifier: awsv2.String(name),
		ApplyImmediately:     awsv2.Bool(true),
	}

	if ic, ok := desired["instance_class"].(string); ok && ic != "" {
		input.DBInstanceClass = awsv2.String(ic)
	}
	if storage := intPropDrivers(desired, "allocated_storage", 0); storage > 0 {
		input.AllocatedStorage = awsv2.Int32(int32(storage))
	}
	if multiAZ, ok := desired["multi_az"].(bool); ok {
		input.MultiAZ = awsv2.Bool(multiAZ)
	}

	out, err := d.client.ModifyDBInstance(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("rds: modify %q: %w", name, err)
	}
	return dbInstanceToOutput(out.DBInstance), nil
}

func (d *RDSDriver) Delete(ctx context.Context, name string) error {
	_, err := d.client.DeleteDBInstance(ctx, &rds.DeleteDBInstanceInput{
		DBInstanceIdentifier: awsv2.String(name),
		SkipFinalSnapshot:    awsv2.Bool(true),
	})
	if err != nil {
		return fmt.Errorf("rds: delete %q: %w", name, err)
	}
	return nil
}

func (d *RDSDriver) HealthCheck(ctx context.Context, name string) (*platform.HealthStatus, error) {
	out, err := d.Read(ctx, name)
	if err != nil {
		return &platform.HealthStatus{
			Status:    "unhealthy",
			Message:   err.Error(),
			CheckedAt: time.Now(),
		}, nil
	}
	status := "healthy"
	if out.Status != platform.ResourceStatusActive {
		status = "degraded"
	}
	return &platform.HealthStatus{
		Status:    status,
		Message:   string(out.Status),
		CheckedAt: time.Now(),
	}, nil
}

func (d *RDSDriver) Scale(ctx context.Context, name string, scaleParams map[string]any) (*platform.ResourceOutput, error) {
	// RDS scaling = changing instance class
	ic, _ := scaleParams["instance_class"].(string)
	if ic == "" {
		return nil, fmt.Errorf("rds: scale requires 'instance_class' parameter")
	}
	return d.Update(ctx, name, nil, scaleParams)
}

func (d *RDSDriver) Diff(ctx context.Context, name string, desired map[string]any) ([]platform.DiffEntry, error) {
	current, err := d.Read(ctx, name)
	if err != nil {
		return nil, err
	}
	return diffProperties(current.Properties, desired), nil
}

func dbInstanceToOutput(db *rdstypes.DBInstance) *platform.ResourceOutput {
	if db == nil {
		return nil
	}

	status := platform.ResourceStatusActive
	dbStatus := ""
	if db.DBInstanceStatus != nil {
		dbStatus = *db.DBInstanceStatus
	}
	switch dbStatus {
	case "creating", "backing-up":
		status = platform.ResourceStatusCreating
	case "deleting":
		status = platform.ResourceStatusDeleting
	case "modifying":
		status = platform.ResourceStatusUpdating
	case "failed", "incompatible-parameters", "storage-full":
		status = platform.ResourceStatusFailed
	}

	props := map[string]any{
		"db_status": dbStatus,
	}
	if db.Engine != nil {
		props["engine"] = *db.Engine
	}
	if db.EngineVersion != nil {
		props["engine_version"] = *db.EngineVersion
	}
	if db.DBInstanceClass != nil {
		props["instance_class"] = *db.DBInstanceClass
	}
	if db.AllocatedStorage != nil {
		props["allocated_storage"] = int(*db.AllocatedStorage)
	}
	if db.MultiAZ != nil {
		props["multi_az"] = *db.MultiAZ
	}
	if db.DBInstanceArn != nil {
		props["arn"] = *db.DBInstanceArn
	}

	endpoint := ""
	connStr := ""
	if db.Endpoint != nil {
		if db.Endpoint.Address != nil {
			endpoint = *db.Endpoint.Address
			port := int32(0)
			if db.Endpoint.Port != nil {
				port = *db.Endpoint.Port
			}
			eng := ""
			if db.Engine != nil {
				eng = *db.Engine
			}
			connStr = fmt.Sprintf("%s://%s:%d", eng, endpoint, port)
		}
	}

	name := ""
	if db.DBInstanceIdentifier != nil {
		name = *db.DBInstanceIdentifier
	}

	return &platform.ResourceOutput{
		Name:          name,
		Type:          "database",
		ProviderType:  "aws.rds",
		Endpoint:      endpoint,
		ConnectionStr: connStr,
		Properties:    props,
		Status:        status,
		LastSynced:    time.Now(),
	}
}

var _ platform.ResourceDriver = (*RDSDriver)(nil)
