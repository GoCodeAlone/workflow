package module

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
)

// dbQueryCacheEntry holds a cached query result with its expiry time.
type dbQueryCacheEntry struct {
	value     map[string]any
	expiresAt time.Time
}

// DBQueryCachedStep executes a parameterized SQL SELECT and caches the result
// in an in-process, TTL-aware cache keyed by a template-resolved cache key.
// Concurrent pipeline executions are safe: access is protected by a read-write mutex.
type DBQueryCachedStep struct {
	name       string
	database   string
	query      string
	params     []string
	cacheKey   string
	cacheTTL   time.Duration
	scanFields []string
	app        modular.Application
	tmpl       *TemplateEngine

	mu    sync.RWMutex
	cache map[string]dbQueryCacheEntry
}

// NewDBQueryCachedStepFactory returns a StepFactory that creates DBQueryCachedStep instances.
func NewDBQueryCachedStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		database, _ := config["database"].(string)
		if database == "" {
			return nil, fmt.Errorf("db_query_cached step %q: 'database' is required", name)
		}

		query, _ := config["query"].(string)
		if query == "" {
			return nil, fmt.Errorf("db_query_cached step %q: 'query' is required", name)
		}

		// Safety: reject template expressions in SQL to prevent injection
		if strings.Contains(query, "{{") {
			return nil, fmt.Errorf("db_query_cached step %q: query must not contain template expressions (use params instead)", name)
		}

		cacheKey, _ := config["cache_key"].(string)
		if cacheKey == "" {
			return nil, fmt.Errorf("db_query_cached step %q: 'cache_key' is required", name)
		}

		cacheTTL := 5 * time.Minute
		if ttlStr, ok := config["cache_ttl"].(string); ok && ttlStr != "" {
			parsed, err := time.ParseDuration(ttlStr)
			if err != nil {
				return nil, fmt.Errorf("db_query_cached step %q: invalid 'cache_ttl' %q: %w", name, ttlStr, err)
			}
			cacheTTL = parsed
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

		var scanFields []string
		if sf, ok := config["scan_fields"]; ok {
			if list, ok := sf.([]any); ok {
				for _, item := range list {
					if s, ok := item.(string); ok {
						scanFields = append(scanFields, s)
					}
				}
			}
		}

		return &DBQueryCachedStep{
			name:       name,
			database:   database,
			query:      query,
			params:     params,
			cacheKey:   cacheKey,
			cacheTTL:   cacheTTL,
			scanFields: scanFields,
			app:        app,
			tmpl:       NewTemplateEngine(),
			cache:      make(map[string]dbQueryCacheEntry),
		}, nil
	}
}

// Name returns the step name.
func (s *DBQueryCachedStep) Name() string { return s.name }

// Execute checks the in-memory cache first; on a miss (or expiry) it queries
// the database, stores the result, and returns it.
func (s *DBQueryCachedStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("db_query_cached step %q: no application context", s.name)
	}

	// Resolve the cache key template
	resolvedKey, err := s.tmpl.Resolve(s.cacheKey, pc)
	if err != nil {
		return nil, fmt.Errorf("db_query_cached step %q: failed to resolve cache_key template: %w", s.name, err)
	}
	key := fmt.Sprintf("%v", resolvedKey)

	// Check cache (read lock)
	s.mu.RLock()
	entry, found := s.cache[key]
	s.mu.RUnlock()

	if found && time.Now().Before(entry.expiresAt) {
		output := copyMap(entry.value)
		output["cache_hit"] = true
		return &StepResult{Output: output}, nil
	}

	// Cache miss or expired — query the database
	result, err := s.runQuery(ctx, pc)
	if err != nil {
		return nil, err
	}

	// Store in cache (write lock)
	s.mu.Lock()
	s.cache[key] = dbQueryCacheEntry{
		value:     copyMap(result),
		expiresAt: time.Now().Add(s.cacheTTL),
	}
	s.mu.Unlock()

	result["cache_hit"] = false
	return &StepResult{Output: result}, nil
}

// runQuery executes the SQL query and returns the result as a map.
func (s *DBQueryCachedStep) runQuery(ctx context.Context, pc *PipelineContext) (map[string]any, error) {
	svc, ok := s.app.SvcRegistry()[s.database]
	if !ok {
		return nil, fmt.Errorf("db_query_cached step %q: database service %q not found", s.name, s.database)
	}

	provider, ok := svc.(DBProvider)
	if !ok {
		return nil, fmt.Errorf("db_query_cached step %q: service %q does not implement DBProvider", s.name, s.database)
	}

	db := provider.DB()
	if db == nil {
		return nil, fmt.Errorf("db_query_cached step %q: database connection is nil", s.name)
	}

	var driver string
	if dp, ok := svc.(DBDriverProvider); ok {
		driver = dp.DriverName()
	}

	// Resolve template params
	resolvedParams := make([]any, len(s.params))
	for i, p := range s.params {
		resolved, err := s.tmpl.Resolve(p, pc)
		if err != nil {
			return nil, fmt.Errorf("db_query_cached step %q: failed to resolve param %d: %w", s.name, i, err)
		}
		resolvedParams[i] = resolved
	}

	query := normalizePlaceholders(s.query, driver)

	rows, err := db.QueryContext(ctx, query, resolvedParams...)
	if err != nil {
		return nil, fmt.Errorf("db_query_cached step %q: query failed: %w", s.name, err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("db_query_cached step %q: failed to get columns: %w", s.name, err)
	}

	// If scan_fields are specified, only keep those columns
	fieldSet := make(map[string]bool, len(s.scanFields))
	for _, f := range s.scanFields {
		fieldSet[f] = true
	}

	output := make(map[string]any)
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("db_query_cached step %q: scan failed: %w", s.name, err)
		}

		for i, col := range columns {
			if len(fieldSet) > 0 && !fieldSet[col] {
				continue
			}
			val := values[i]
			if b, ok := val.([]byte); ok {
				output[col] = string(b)
			} else {
				output[col] = val
			}
		}
		// Only take the first row
		break
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db_query_cached step %q: row iteration error: %w", s.name, err)
	}

	return output, nil
}

// copyMap creates a shallow copy of a map.
func copyMap(m map[string]any) map[string]any {
	cp := make(map[string]any, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}
