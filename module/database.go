package module

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
)

// validIdentifier matches safe SQL identifiers (alphanumeric, underscore, dot for schema.table).
var validIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.]*$`)

// validateIdentifier checks that a SQL identifier (table/column name) is safe.
func validateIdentifier(name string) error {
	if !validIdentifier.MatchString(name) {
		return fmt.Errorf("invalid SQL identifier: %q", name)
	}
	return nil
}

// DatabaseTLSConfig holds TLS settings for database connections.
type DatabaseTLSConfig struct {
	// Mode controls SSL behaviour: disable | require | verify-ca | verify-full (PostgreSQL naming).
	Mode   string `json:"mode" yaml:"mode"`
	CAFile string `json:"ca_file" yaml:"ca_file"`
}

// DatabaseConfig holds configuration for the workflow database module
type DatabaseConfig struct {
	Driver          string            `json:"driver" yaml:"driver"`
	DSN             string            `json:"dsn" yaml:"dsn"`
	MaxOpenConns    int               `json:"maxOpenConns" yaml:"maxOpenConns"`
	MaxIdleConns    int               `json:"maxIdleConns" yaml:"maxIdleConns"`
	ConnMaxLifetime time.Duration     `json:"connMaxLifetime" yaml:"connMaxLifetime"`
	MigrationsDir   string            `json:"migrationsDir" yaml:"migrationsDir"`
	TLS             DatabaseTLSConfig `json:"tls" yaml:"tls"`
}

// QueryResult represents the result of a query
type QueryResult struct {
	Columns []string         `json:"columns"`
	Rows    []map[string]any `json:"rows"`
	Count   int              `json:"count"`
}

// WorkflowDatabase wraps database/sql for workflow use
type WorkflowDatabase struct {
	name   string
	config DatabaseConfig
	db     *sql.DB
	mu     sync.RWMutex
}

// NewWorkflowDatabase creates a new WorkflowDatabase module
func NewWorkflowDatabase(name string, config DatabaseConfig) *WorkflowDatabase {
	return &WorkflowDatabase{
		name:   name,
		config: config,
	}
}

// Name returns the module name
func (w *WorkflowDatabase) Name() string {
	return w.name
}

// Init registers the database as a service
func (w *WorkflowDatabase) Init(app modular.Application) error {
	return app.RegisterService(w.name, w)
}

// ProvidesServices declares the service this module provides, enabling proper
// dependency ordering in the modular framework.
func (w *WorkflowDatabase) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        w.name,
			Description: "Workflow Database: " + w.name,
			Instance:    w,
		},
	}
}

// RequiresServices returns no dependencies.
func (w *WorkflowDatabase) RequiresServices() []modular.ServiceDependency {
	return nil
}

// buildDSN returns the DSN with TLS parameters appended for supported drivers.
func (w *WorkflowDatabase) buildDSN() string {
	dsn := w.config.DSN
	mode := w.config.TLS.Mode
	if mode == "" || mode == "disable" {
		return dsn
	}

	switch w.config.Driver {
	case "postgres", "pgx", "pgx/v5":
		sep := "?"
		if strings.ContainsRune(dsn, '?') {
			sep = "&"
		}
		dsn += sep + "sslmode=" + mode
		if w.config.TLS.CAFile != "" {
			dsn += "&sslrootcert=" + w.config.TLS.CAFile
		}
	}
	return dsn
}

// Start opens the database connection during application startup.
// Implements the modular.Startable interface so the framework automatically
// calls Open() after all modules have been initialized.
func (w *WorkflowDatabase) Start(ctx context.Context) error {
	_, err := w.Open()
	return err
}

// Stop closes the database connection during application shutdown.
// Implements the modular.Stoppable interface so the framework automatically
// calls Close() during graceful shutdown.
func (w *WorkflowDatabase) Stop(ctx context.Context) error {
	return w.Close()
}

// Open opens the database connection using config
func (w *WorkflowDatabase) Open() (*sql.DB, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.db != nil {
		return w.db, nil
	}

	db, err := sql.Open(w.config.Driver, w.buildDSN())
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if w.config.MaxOpenConns > 0 {
		db.SetMaxOpenConns(w.config.MaxOpenConns)
	}
	if w.config.MaxIdleConns > 0 {
		db.SetMaxIdleConns(w.config.MaxIdleConns)
	}
	if w.config.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(w.config.ConnMaxLifetime)
	}

	w.db = db
	return db, nil
}

// Close closes the database connection
func (w *WorkflowDatabase) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.db != nil {
		err := w.db.Close()
		w.db = nil
		return err
	}
	return nil
}

// DB returns the underlying *sql.DB
func (w *WorkflowDatabase) DB() *sql.DB {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.db
}

// Ping checks the database connection
func (w *WorkflowDatabase) Ping(ctx context.Context) error {
	w.mu.RLock()
	db := w.db
	w.mu.RUnlock()

	if db == nil {
		return fmt.Errorf("database not open")
	}
	return db.PingContext(ctx)
}

// Query executes a query and returns structured results
func (w *WorkflowDatabase) Query(ctx context.Context, sqlStr string, args ...any) (*QueryResult, error) {
	w.mu.RLock()
	db := w.db
	w.mu.RUnlock()

	if db == nil {
		return nil, fmt.Errorf("database not open")
	}

	rows, err := db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	result := &QueryResult{
		Columns: columns,
		Rows:    make([]map[string]any, 0),
	}

	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make(map[string]any)
		for i, col := range columns {
			val := values[i]
			// Convert byte slices to strings for readability
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		result.Rows = append(result.Rows, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	result.Count = len(result.Rows)
	return result, nil
}

// Execute executes a statement and returns rows affected
func (w *WorkflowDatabase) Execute(ctx context.Context, sqlStr string, args ...any) (int64, error) {
	w.mu.RLock()
	db := w.db
	w.mu.RUnlock()

	if db == nil {
		return 0, fmt.Errorf("database not open")
	}

	result, err := db.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return 0, fmt.Errorf("execute failed: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return rowsAffected, nil
}

// InsertRow builds and executes an INSERT statement
func (w *WorkflowDatabase) InsertRow(ctx context.Context, table string, data map[string]any) (int64, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("no data to insert")
	}
	if err := validateIdentifier(table); err != nil {
		return 0, fmt.Errorf("invalid table name: %w", err)
	}

	// Sort keys for deterministic SQL generation
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	columns := make([]string, len(keys))
	placeholders := make([]string, len(keys))
	values := make([]any, len(keys))

	for i, k := range keys {
		if err := validateIdentifier(k); err != nil {
			return 0, fmt.Errorf("invalid column name: %w", err)
		}
		columns[i] = k
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		values[i] = data[k]
	}

	sqlStr := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		table,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)

	return w.Execute(ctx, sqlStr, values...)
}

// UpdateRows builds and executes an UPDATE statement
func (w *WorkflowDatabase) UpdateRows(ctx context.Context, table string, data map[string]any, where string, whereArgs ...any) (int64, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("no data to update")
	}
	if err := validateIdentifier(table); err != nil {
		return 0, fmt.Errorf("invalid table name: %w", err)
	}

	// Overflow check for allocation size
	if len(data) > math.MaxInt-len(whereArgs) {
		return 0, fmt.Errorf("too many parameters: data(%d) + whereArgs(%d) overflows", len(data), len(whereArgs))
	}

	// Sort keys for deterministic SQL generation
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	setClauses := make([]string, len(keys))
	values := make([]any, 0, len(keys)+len(whereArgs))

	for i, k := range keys {
		if err := validateIdentifier(k); err != nil {
			return 0, fmt.Errorf("invalid column name: %w", err)
		}
		setClauses[i] = fmt.Sprintf("%s = $%d", k, i+1)
		values = append(values, data[k])
	}

	sqlStr := fmt.Sprintf("UPDATE %s SET %s",
		table,
		strings.Join(setClauses, ", "),
	)

	if where != "" {
		sqlStr += " WHERE " + where
		values = append(values, whereArgs...)
	}

	return w.Execute(ctx, sqlStr, values...)
}

// DeleteRows builds and executes a DELETE statement
func (w *WorkflowDatabase) DeleteRows(ctx context.Context, table string, where string, whereArgs ...any) (int64, error) {
	if err := validateIdentifier(table); err != nil {
		return 0, fmt.Errorf("invalid table name: %w", err)
	}
	sqlStr := fmt.Sprintf("DELETE FROM %s", table)
	if where != "" {
		sqlStr += " WHERE " + where
	}
	return w.Execute(ctx, sqlStr, whereArgs...)
}

// BuildInsertSQL builds an INSERT SQL string and returns it with values (exported for testing).
// Returns an error if table or column names contain unsafe characters.
func BuildInsertSQL(table string, data map[string]any) (string, []any, error) {
	if len(data) == 0 {
		return "", nil, nil
	}
	if err := validateIdentifier(table); err != nil {
		return "", nil, fmt.Errorf("invalid table name: %w", err)
	}

	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	columns := make([]string, len(keys))
	placeholders := make([]string, len(keys))
	values := make([]any, len(keys))

	for i, k := range keys {
		if err := validateIdentifier(k); err != nil {
			return "", nil, fmt.Errorf("invalid column name: %w", err)
		}
		columns[i] = k
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		values[i] = data[k]
	}

	sqlStr := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		table,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)

	return sqlStr, values, nil
}

// BuildUpdateSQL builds an UPDATE SQL string and returns it with values (exported for testing).
// Returns an error if table or column names contain unsafe characters.
func BuildUpdateSQL(table string, data map[string]any, where string, whereArgs ...any) (string, []any, error) {
	if len(data) == 0 {
		return "", nil, nil
	}
	if err := validateIdentifier(table); err != nil {
		return "", nil, fmt.Errorf("invalid table name: %w", err)
	}

	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	setClauses := make([]string, len(keys))
	values := make([]any, 0, len(keys)+len(whereArgs))

	for i, k := range keys {
		if err := validateIdentifier(k); err != nil {
			return "", nil, fmt.Errorf("invalid column name: %w", err)
		}
		setClauses[i] = fmt.Sprintf("%s = $%d", k, i+1)
		values = append(values, data[k])
	}

	sqlStr := fmt.Sprintf("UPDATE %s SET %s",
		table,
		strings.Join(setClauses, ", "),
	)

	if where != "" {
		sqlStr += " WHERE " + where
		values = append(values, whereArgs...)
	}

	return sqlStr, values, nil
}

// BuildDeleteSQL builds a DELETE SQL string (exported for testing).
// Returns an error if the table name contains unsafe characters.
func BuildDeleteSQL(table string, where string, whereArgs ...any) (string, []any, error) {
	if err := validateIdentifier(table); err != nil {
		return "", nil, fmt.Errorf("invalid table name: %w", err)
	}
	sqlStr := fmt.Sprintf("DELETE FROM %s", table)
	var values []any
	if where != "" {
		sqlStr += " WHERE " + where
		values = whereArgs
	}
	return sqlStr, values, nil
}

// DatabaseIntegrationConnector implements IntegrationConnector for database operations
type DatabaseIntegrationConnector struct {
	name      string
	db        *WorkflowDatabase
	connected bool
}

// NewDatabaseIntegrationConnector creates a new database integration connector
func NewDatabaseIntegrationConnector(name string, db *WorkflowDatabase) *DatabaseIntegrationConnector {
	return &DatabaseIntegrationConnector{
		name: name,
		db:   db,
	}
}

// GetName returns the connector name
func (c *DatabaseIntegrationConnector) GetName() string {
	return c.name
}

// Connect opens the database connection
func (c *DatabaseIntegrationConnector) Connect(ctx context.Context) error {
	_, err := c.db.Open()
	if err != nil {
		return fmt.Errorf("failed to connect database: %w", err)
	}
	c.connected = true
	return nil
}

// Disconnect closes the database connection
func (c *DatabaseIntegrationConnector) Disconnect(ctx context.Context) error {
	c.connected = false
	return c.db.Close()
}

// IsConnected returns whether the connector is connected
func (c *DatabaseIntegrationConnector) IsConnected() bool {
	return c.connected
}

// Execute dispatches to the appropriate WorkflowDatabase method based on action
func (c *DatabaseIntegrationConnector) Execute(ctx context.Context, action string, params map[string]any) (map[string]any, error) {
	if !c.connected {
		return nil, fmt.Errorf("connector not connected")
	}

	switch action {
	case "query":
		sqlStr, _ := params["sql"].(string)
		if sqlStr == "" {
			return nil, fmt.Errorf("sql parameter required for query action")
		}
		args := extractArgs(params)
		result, err := c.db.Query(ctx, sqlStr, args...)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"columns": result.Columns,
			"rows":    result.Rows,
			"count":   result.Count,
		}, nil

	case "execute":
		sqlStr, _ := params["sql"].(string)
		if sqlStr == "" {
			return nil, fmt.Errorf("sql parameter required for execute action")
		}
		args := extractArgs(params)
		rowsAffected, err := c.db.Execute(ctx, sqlStr, args...)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"rowsAffected": rowsAffected,
		}, nil

	case "insert":
		table, _ := params["table"].(string)
		if table == "" {
			return nil, fmt.Errorf("table parameter required for insert action")
		}
		data, _ := params["data"].(map[string]any)
		if len(data) == 0 {
			return nil, fmt.Errorf("data parameter required for insert action")
		}
		rowsAffected, err := c.db.InsertRow(ctx, table, data)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"rowsAffected": rowsAffected,
		}, nil

	case "update":
		table, _ := params["table"].(string)
		if table == "" {
			return nil, fmt.Errorf("table parameter required for update action")
		}
		data, _ := params["data"].(map[string]any)
		if len(data) == 0 {
			return nil, fmt.Errorf("data parameter required for update action")
		}
		where, _ := params["where"].(string)
		whereArgs := extractArgs(params)
		rowsAffected, err := c.db.UpdateRows(ctx, table, data, where, whereArgs...)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"rowsAffected": rowsAffected,
		}, nil

	case "delete":
		table, _ := params["table"].(string)
		if table == "" {
			return nil, fmt.Errorf("table parameter required for delete action")
		}
		where, _ := params["where"].(string)
		whereArgs := extractArgs(params)
		rowsAffected, err := c.db.DeleteRows(ctx, table, where, whereArgs...)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"rowsAffected": rowsAffected,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported action: %s (supported: query, execute, insert, update, delete)", action)
	}
}

// extractArgs extracts the "args" parameter as a slice of interface{}
func extractArgs(params map[string]any) []any {
	argsRaw, ok := params["args"]
	if !ok {
		return nil
	}
	switch v := argsRaw.(type) {
	case []any:
		return v
	default:
		return []any{v}
	}
}
