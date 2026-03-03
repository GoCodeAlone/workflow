package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// DBCreatePartitionStep creates a PostgreSQL LIST partition for a given tenant value
// on all tables managed by a database.partitioned module.
type DBCreatePartitionStep struct {
	name      string
	database  string
	tenantKey string // dot-path in PipelineContext to resolve the tenant value
	app       modular.Application
	tmpl      *TemplateEngine
}

// NewDBCreatePartitionStepFactory returns a StepFactory for DBCreatePartitionStep.
func NewDBCreatePartitionStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		database, _ := config["database"].(string)
		if database == "" {
			return nil, fmt.Errorf("db_create_partition step %q: 'database' is required", name)
		}

		tenantKey, _ := config["tenantKey"].(string)
		if tenantKey == "" {
			return nil, fmt.Errorf("db_create_partition step %q: 'tenantKey' is required", name)
		}

		return &DBCreatePartitionStep{
			name:      name,
			database:  database,
			tenantKey: tenantKey,
			app:       app,
			tmpl:      NewTemplateEngine(),
		}, nil
	}
}

func (s *DBCreatePartitionStep) Name() string { return s.name }

func (s *DBCreatePartitionStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("db_create_partition step %q: no application context", s.name)
	}

	svc, ok := s.app.SvcRegistry()[s.database]
	if !ok {
		return nil, fmt.Errorf("db_create_partition step %q: database service %q not found", s.name, s.database)
	}

	mgr, ok := svc.(PartitionManager)
	if !ok {
		return nil, fmt.Errorf("db_create_partition step %q: service %q does not implement PartitionManager (use database.partitioned)", s.name, s.database)
	}

	tenantVal := resolveBodyFrom(s.tenantKey, pc)
	if tenantVal == nil {
		return nil, fmt.Errorf("db_create_partition step %q: tenantKey %q resolved to nil in pipeline context", s.name, s.tenantKey)
	}
	tenantStr := fmt.Sprintf("%v", tenantVal)

	if err := mgr.EnsurePartition(ctx, tenantStr); err != nil {
		return nil, fmt.Errorf("db_create_partition step %q: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"tenant":    tenantStr,
		"partition": "created",
	}}, nil
}
