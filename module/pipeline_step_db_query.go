package module

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/GoCodeAlone/modular"
)

// DBProvider is implemented by modules that provide a *sql.DB connection.
// Both SQLiteStorage and WorkflowDatabase satisfy this interface.
type DBProvider interface {
	DB() *sql.DB
}

// DBDriverProvider is optionally implemented by DBProvider modules that expose
// the underlying driver name (e.g. "pgx", "sqlite3"). This allows pipeline
// steps to normalize SQL placeholder syntax automatically.
type DBDriverProvider interface {
	DBProvider
	DriverName() string
}

// DBQueryStep executes a parameterized SQL SELECT against a named database service.
type DBQueryStep struct {
	name            string
	database        string
	query           string
	params          []string
	mode            string // "list" or "single"
	tenantKey       string // dot-path to resolve tenant value for automatic scoping
	allowDynamicSQL bool
	app             modular.Application
	tmpl            *TemplateEngine
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

		// Safety: reject template expressions in SQL to prevent injection,
		// unless allow_dynamic_sql is explicitly enabled.
		allowDynamicSQL, _ := config["allow_dynamic_sql"].(bool)
		if !allowDynamicSQL && strings.Contains(query, "{{") {
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

		tenantKey, _ := config["tenantKey"].(string)

		return &DBQueryStep{
			name:            name,
			database:        database,
			query:           query,
			params:          params,
			mode:            mode,
			tenantKey:       tenantKey,
			allowDynamicSQL: allowDynamicSQL,
			app:             app,
			tmpl:            NewTemplateEngine(),
		}, nil
	}
}

func (s *DBQueryStep) Name() string { return s.name }

func (s *DBQueryStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	// Resolve template expressions in the query early (before any DB access) when
	// dynamic SQL is enabled. This validates resolved identifiers against an
	// allowlist before any database interaction.
	query := s.query
	if s.allowDynamicSQL {
		var err error
		query, err = resolveDynamicSQL(s.tmpl, query, pc)
		if err != nil {
			return nil, fmt.Errorf("db_query step %q: %w", s.name, err)
		}
	}

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

	// Detect driver for placeholder normalization
	var driver string
	if dp, ok := svc.(DBDriverProvider); ok {
		driver = dp.DriverName()
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

	// Apply automatic tenant scoping when tenantKey is configured.
	if s.tenantKey != "" {
		pkp, ok := svc.(PartitionKeyProvider)
		if !ok {
			return nil, fmt.Errorf("db_query step %q: tenantKey requires database %q to implement PartitionKeyProvider (use database.partitioned)", s.name, s.database)
		}
		partKey := pkp.PartitionKey()
		if err := validateIdentifier(partKey); err != nil {
			return nil, fmt.Errorf("db_query step %q: invalid partition key %q: %w", s.name, partKey, err)
		}
		tenantVal := resolveBodyFrom(s.tenantKey, pc)
		if tenantVal == nil {
			return nil, fmt.Errorf("db_query step %q: tenantKey %q resolved to nil in pipeline context", s.name, s.tenantKey)
		}
		tenantStr := fmt.Sprintf("%v", tenantVal)
		nextParam := len(resolvedParams) + 1
		query = appendTenantFilter(query, partKey, nextParam)
		resolvedParams = append(resolvedParams, tenantStr)
	}

	// Normalize SQL placeholders: users write $1,$2,$3 (PostgreSQL style),
	// engine converts to ? for SQLite automatically.
	query = normalizePlaceholders(query, driver)

	// Execute query
	rows, err := db.QueryContext(ctx, query, resolvedParams...)
	if err != nil {
		return nil, fmt.Errorf("db_query step %q: query failed: %w", s.name, err)
	}
	defer rows.Close()

	results, err := scanSQLRows(rows)
	if err != nil {
		return nil, fmt.Errorf("db_query step %q: %w", s.name, err)
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
			// Convert []byte: try JSON parse first (handles PostgreSQL json/jsonb
			// column types returned by the pgx driver as raw JSON bytes), then
			// fall back to string conversion for non-JSON byte data (e.g. bytea).
			if b, ok := val.([]byte); ok {
				row[col] = parseJSONBytesOrString(b)
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

	return &StepResult{Output: formatQueryOutput(output, s.mode)}, nil
}
