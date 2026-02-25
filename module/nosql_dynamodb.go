package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// DynamoDBNoSQLConfig holds configuration for the nosql.dynamodb module.
//
// Full AWS DynamoDB implementation would use:
//   - github.com/aws/aws-sdk-go-v2/service/dynamodb
//   - github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue
//
// When endpoint == "local" the module falls back to the in-memory backend,
// which is useful for local development and tests without a real DynamoDB endpoint.
type DynamoDBNoSQLConfig struct {
	TableName   string `json:"tableName"   yaml:"tableName"`
	Region      string `json:"region"      yaml:"region"`
	Endpoint    string `json:"endpoint"    yaml:"endpoint"` // "local" => in-memory fallback
	Credentials string `json:"credentials" yaml:"credentials"` // ref to cloud.account module name
}

// DynamoDBNoSQL is the nosql.dynamodb module.
// In local mode (endpoint: "local") it delegates to MemoryNoSQL.
// For a real AWS implementation, replace the backend field with a DynamoDB client
// and implement Get/Put/Delete/Query using dynamodb.GetItem, PutItem, DeleteItem, Scan.
type DynamoDBNoSQL struct {
	name    string
	cfg     DynamoDBNoSQLConfig
	backend NoSQLStore // in-memory fallback when endpoint == "local"
}

// NewDynamoDBNoSQL creates a new DynamoDBNoSQL module.
func NewDynamoDBNoSQL(name string, cfg DynamoDBNoSQLConfig) *DynamoDBNoSQL {
	return &DynamoDBNoSQL{name: name, cfg: cfg}
}

func (d *DynamoDBNoSQL) Name() string { return d.name }

func (d *DynamoDBNoSQL) Init(_ modular.Application) error {
	if d.cfg.Endpoint == "local" || d.cfg.Endpoint == "" {
		d.backend = NewMemoryNoSQL(d.name+"-mem", MemoryNoSQLConfig{Collection: d.cfg.TableName})
		return nil
	}
	// Full AWS implementation:
	// cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(d.cfg.Region))
	// if err != nil { return err }
	// if d.cfg.Endpoint != "" {
	//     cfg.EndpointResolverWithOptions = aws.EndpointResolverWithOptionsFunc(...)
	// }
	// d.client = dynamodb.NewFromConfig(cfg)
	return fmt.Errorf("nosql.dynamodb %q: real AWS endpoint not yet implemented; use endpoint: local for testing", d.name)
}

func (d *DynamoDBNoSQL) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: d.name, Description: "DynamoDB NoSQL store: " + d.name, Instance: d},
	}
}

func (d *DynamoDBNoSQL) RequiresServices() []modular.ServiceDependency { return nil }

func (d *DynamoDBNoSQL) Get(ctx context.Context, key string) (map[string]any, error) {
	if d.backend == nil {
		return nil, fmt.Errorf("nosql.dynamodb %q: not initialized", d.name)
	}
	// Real implementation: dynamodb.GetItem with Key: {PK: {S: &key}}
	return d.backend.Get(ctx, key)
}

func (d *DynamoDBNoSQL) Put(ctx context.Context, key string, item map[string]any) error {
	if d.backend == nil {
		return fmt.Errorf("nosql.dynamodb %q: not initialized", d.name)
	}
	// Real implementation: attributevalue.MarshalMap(item) then dynamodb.PutItem
	return d.backend.Put(ctx, key, item)
}

func (d *DynamoDBNoSQL) Delete(ctx context.Context, key string) error {
	if d.backend == nil {
		return fmt.Errorf("nosql.dynamodb %q: not initialized", d.name)
	}
	// Real implementation: dynamodb.DeleteItem with Key: {PK: {S: &key}}
	return d.backend.Delete(ctx, key)
}

func (d *DynamoDBNoSQL) Query(ctx context.Context, params map[string]any) ([]map[string]any, error) {
	if d.backend == nil {
		return nil, fmt.Errorf("nosql.dynamodb %q: not initialized", d.name)
	}
	// Real implementation: dynamodb.Scan or Query with FilterExpression
	return d.backend.Query(ctx, params)
}
