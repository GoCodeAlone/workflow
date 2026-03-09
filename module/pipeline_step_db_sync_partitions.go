package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
)

// DBSyncPartitionsStep synchronizes partitions from a source table (e.g., tenants)
// for all tables managed by a database.partitioned module. This enables automatic
// partition creation when new tenants are onboarded.
type DBSyncPartitionsStep struct {
	name         string
	database     string
	partitionKey string // optional: target a specific partition config by key
	app          modular.Application
}

// NewDBSyncPartitionsStepFactory returns a StepFactory for DBSyncPartitionsStep.
func NewDBSyncPartitionsStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		database, _ := config["database"].(string)
		if database == "" {
			return nil, fmt.Errorf("db_sync_partitions step %q: 'database' is required", name)
		}

		partitionKey, _ := config["partitionKey"].(string)

		return &DBSyncPartitionsStep{
			name:         name,
			database:     database,
			partitionKey: partitionKey,
			app:          app,
		}, nil
	}
}

func (s *DBSyncPartitionsStep) Name() string { return s.name }

func (s *DBSyncPartitionsStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("db_sync_partitions step %q: no application context", s.name)
	}

	svc, ok := s.app.SvcRegistry()[s.database]
	if !ok {
		return nil, fmt.Errorf("db_sync_partitions step %q: database service %q not found", s.name, s.database)
	}

	mgr, ok := svc.(PartitionManager)
	if !ok {
		return nil, fmt.Errorf("db_sync_partitions step %q: service %q does not implement PartitionManager (use database.partitioned)", s.name, s.database)
	}

	if s.partitionKey != "" {
		multiMgr, ok := svc.(MultiPartitionManager)
		if !ok {
			return nil, fmt.Errorf("db_sync_partitions step %q: service %q does not implement MultiPartitionManager (required when partitionKey is set)", s.name, s.database)
		}
		if err := multiMgr.SyncPartitionsForKey(ctx, s.partitionKey); err != nil {
			return nil, fmt.Errorf("db_sync_partitions step %q: %w", s.name, err)
		}
	} else {
		if err := mgr.SyncPartitionsFromSource(ctx); err != nil {
			return nil, fmt.Errorf("db_sync_partitions step %q: %w", s.name, err)
		}
	}

	return &StepResult{Output: map[string]any{
		"synced": true,
	}}, nil
}
