package module

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// DBProvider is implemented by modules that provide a *sql.DB connection.
// Both SQLiteStorage and WorkflowDatabase satisfy this interface.
type DBProvider interface {
	DB() *sql.DB
}

// DBQueryStep executes a parameterized SQL SELECT against a named database service.
type DBQueryStep struct {
	name     string
	database string
	query    string
	params   []string
	mode     string // "list" or "single"
	app      modular.Application
	tmpl     *TemplateEngine
}

// NewDBQueryStepFactory returns a StepFactory that creates DBQueryStep instances.
func NewDBQueryStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		database, _ := config["database"].(string)
		if database == "" {
			return nil, fmt.Errorf("db_query step %q: 'database' is required", name)
		}

		query, _ := config["query"].(string)
		if query == "" {
			return nil, fmt.Errorf("db_query step %q: 'query' is required", name)
		}

		// Safety: reject template expressions in SQL to prevent injection
		if strings.Contains(query, "{{") {
			return nil, fmt.Errorf("db_query step %q: query must not contain template expressions (use params instead)", name)
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

		mode, _ := config["mode"].(string)
		if mode == "" {
			mode = "list"
		}
		if mode != "list" && mode != "single" {
			return nil, fmt.Errorf("db_query step %q: mode must be 'list' or 'single', got %q", name, mode)
		}

		return &DBQueryStep{
			name:     name,
			database: database,
			query:    query,
			params:   params,
			mode:     mode,
			app:      app,
			tmpl:     NewTemplateEngine(),
		}, nil
	}
}

func (s *DBQueryStep) Name() string { return s.name }

func (s *DBQueryStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	// Resolve database service
	if s.app == nil {
		return nil, fmt.Errorf("db_query step %q: no application context", s.name)
	}

	svc, ok := s.app.SvcRegistry()[s.database]
	if !ok {
		return nil, fmt.Errorf("db_query step %q: database service %q not found", s.name, s.database)
	}

	provider, ok := svc.(DBProvider)
	if !ok {
		return nil, fmt.Errorf("db_query step %q: service %q does not implement DBProvider", s.name, s.database)
	}

	db := provider.DB()
	if db == nil {
		return nil, fmt.Errorf("db_query step %q: database connection is nil", s.name)
	}

	// Resolve template params
	resolvedParams := make([]any, len(s.params))
	for i, p := range s.params {
		resolved, err := s.tmpl.Resolve(p, pc)
		if err != nil {
			return nil, fmt.Errorf("db_query step %q: failed to resolve param %d: %w", s.name, i, err)
		}
		resolvedParams[i] = resolved
	}

	// Execute query
	rows, err := db.Query(s.query, resolvedParams...)
	if err != nil {
		return nil, fmt.Errorf("db_query step %q: query failed: %w", s.name, err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("db_query step %q: failed to get columns: %w", s.name, err)
	}

	var results []map[string]any
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("db_query step %q: scan failed: %w", s.name, err)
		}

		row := make(map[string]any, len(columns))
		for i, col := range columns {
			val := values[i]
			// Convert []byte to string for readability
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db_query step %q: row iteration error: %w", s.name, err)
	}

	output := make(map[string]any)
	if s.mode == "single" {
		if len(results) > 0 {
			output["row"] = results[0]
			output["found"] = true
		} else {
			output["row"] = map[string]any{}
			output["found"] = false
		}
	} else {
		if results == nil {
			results = []map[string]any{}
		}
		output["rows"] = results
		output["count"] = len(results)
	}

	return &StepResult{Output: output}, nil
}
