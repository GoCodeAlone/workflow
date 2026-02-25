package module

import (
	"context"
	"fmt"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// DBExecStep executes parameterized SQL INSERT/UPDATE/DELETE against a named database service.
type DBExecStep struct {
	name        string
	database    string
	query       string
	params      []string
	ignoreError bool
	app         modular.Application
	tmpl        *TemplateEngine
}

// NewDBExecStepFactory returns a StepFactory that creates DBExecStep instances.
func NewDBExecStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		database, _ := config["database"].(string)
		if database == "" {
			return nil, fmt.Errorf("db_exec step %q: 'database' is required", name)
		}

		query, _ := config["query"].(string)
		if query == "" {
			return nil, fmt.Errorf("db_exec step %q: 'query' is required", name)
		}

		// Safety: reject template expressions in SQL to prevent injection
		if strings.Contains(query, "{{") {
			return nil, fmt.Errorf("db_exec step %q: query must not contain template expressions (use params instead)", name)
		}

		var params []string
		if p, ok := config["params"]; ok {
			if list, ok := p.([]any); ok {
				for _, item := range list {
					if s, ok := item.(string); ok {
						params = append(params, s)
					}
				}
			}
		}

		ignoreError, _ := config["ignore_error"].(bool)

		return &DBExecStep{
			name:        name,
			database:    database,
			query:       query,
			params:      params,
			ignoreError: ignoreError,
			app:         app,
			tmpl:        NewTemplateEngine(),
		}, nil
	}
}

func (s *DBExecStep) Name() string { return s.name }

func (s *DBExecStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("db_exec step %q: no application context", s.name)
	}

	svc, ok := s.app.SvcRegistry()[s.database]
	if !ok {
		return nil, fmt.Errorf("db_exec step %q: database service %q not found", s.name, s.database)
	}

	provider, ok := svc.(DBProvider)
	if !ok {
		return nil, fmt.Errorf("db_exec step %q: service %q does not implement DBProvider", s.name, s.database)
	}

	db := provider.DB()
	if db == nil {
		return nil, fmt.Errorf("db_exec step %q: database connection is nil", s.name)
	}

	// Resolve template params
	resolvedParams := make([]any, len(s.params))
	for i, p := range s.params {
		resolved, err := s.tmpl.Resolve(p, pc)
		if err != nil {
			return nil, fmt.Errorf("db_exec step %q: failed to resolve param %d: %w", s.name, i, err)
		}
		resolvedParams[i] = resolved
	}

	// Execute statement
	result, err := db.Exec(s.query, resolvedParams...)
	if err != nil {
		if s.ignoreError {
			return &StepResult{Output: map[string]any{
				"affected_rows": int64(0),
				"last_id":       "0",
				"ignored_error": err.Error(),
			}}, nil
		}
		return nil, fmt.Errorf("db_exec step %q: exec failed: %w", s.name, err)
	}

	affectedRows, _ := result.RowsAffected()
	lastID, _ := result.LastInsertId()

	output := map[string]any{
		"affected_rows": affectedRows,
		"last_id":       fmt.Sprintf("%d", lastID),
	}

	return &StepResult{Output: output}, nil
}
