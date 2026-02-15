package compliance

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// AuditEntry represents an auditable action in the system.
type AuditEntry struct {
	ID         string         `json:"id"`
	Timestamp  time.Time      `json:"timestamp"`
	ActorID    string         `json:"actor_id"`
	ActorType  string         `json:"actor_type"` // "user", "system", "api_key"
	Action     string         `json:"action"`     // "create", "read", "update", "delete", "execute", "login", "logout"
	Resource   string         `json:"resource"`   // "workflow", "company", "organization", "project", "api_key"
	ResourceID string         `json:"resource_id"`
	TenantID   string         `json:"tenant_id"` // company/org scope
	Details    map[string]any `json:"details,omitempty"`
	IPAddress  string         `json:"ip_address,omitempty"`
	UserAgent  string         `json:"user_agent,omitempty"`
	Success    bool           `json:"success"`
	ErrorMsg   string         `json:"error_message,omitempty"`
}

// AuditFilter specifies criteria for querying audit entries.
type AuditFilter struct {
	ActorID   string     `json:"actor_id,omitempty"`
	Action    string     `json:"action,omitempty"`
	Resource  string     `json:"resource,omitempty"`
	TenantID  string     `json:"tenant_id,omitempty"`
	StartTime *time.Time `json:"start_time,omitempty"`
	EndTime   *time.Time `json:"end_time,omitempty"`
	Success   *bool      `json:"success,omitempty"`
	Limit     int        `json:"limit,omitempty"`
	Offset    int        `json:"offset,omitempty"`
}

// AuditLog interface for recording and querying audit entries.
type AuditLog interface {
	Record(ctx context.Context, entry *AuditEntry) error
	Query(ctx context.Context, filter AuditFilter) ([]*AuditEntry, error)
	Count(ctx context.Context, filter AuditFilter) (int64, error)
	Export(ctx context.Context, filter AuditFilter, format string) ([]byte, error) // format: "json", "csv"
}

// ---------------------------------------------------------------------------
// InMemoryAuditLog
// ---------------------------------------------------------------------------

// InMemoryAuditLog stores audit entries in memory. Suitable for testing and
// development; not for production use.
type InMemoryAuditLog struct {
	mu      sync.RWMutex
	entries []*AuditEntry
}

// NewInMemoryAuditLog creates a new in-memory audit log.
func NewInMemoryAuditLog() *InMemoryAuditLog {
	return &InMemoryAuditLog{}
}

// Record adds an audit entry. It assigns an ID and timestamp if missing.
func (l *InMemoryAuditLog) Record(_ context.Context, entry *AuditEntry) error {
	if entry == nil {
		return fmt.Errorf("audit: entry must not be nil")
	}
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, entry)
	return nil
}

// Query returns entries matching the filter.
func (l *InMemoryAuditLog) Query(_ context.Context, filter AuditFilter) ([]*AuditEntry, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var results []*AuditEntry
	for _, e := range l.entries {
		if matchesFilter(e, filter) {
			results = append(results, e)
		}
	}

	// Apply offset/limit
	if filter.Offset > 0 {
		if filter.Offset >= len(results) {
			return nil, nil
		}
		results = results[filter.Offset:]
	}
	if filter.Limit > 0 && filter.Limit < len(results) {
		results = results[:filter.Limit]
	}
	return results, nil
}

// Count returns the number of entries matching the filter.
func (l *InMemoryAuditLog) Count(_ context.Context, filter AuditFilter) (int64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var count int64
	for _, e := range l.entries {
		if matchesFilter(e, filter) {
			count++
		}
	}
	return count, nil
}

// Export returns entries matching the filter in the given format ("json" or "csv").
func (l *InMemoryAuditLog) Export(ctx context.Context, filter AuditFilter, format string) ([]byte, error) {
	entries, err := l.Query(ctx, filter)
	if err != nil {
		return nil, err
	}
	return exportEntries(entries, format)
}

// matchesFilter checks if an entry matches the given filter criteria.
func matchesFilter(e *AuditEntry, f AuditFilter) bool {
	if f.ActorID != "" && e.ActorID != f.ActorID {
		return false
	}
	if f.Action != "" && e.Action != f.Action {
		return false
	}
	if f.Resource != "" && e.Resource != f.Resource {
		return false
	}
	if f.TenantID != "" && e.TenantID != f.TenantID {
		return false
	}
	if f.StartTime != nil && e.Timestamp.Before(*f.StartTime) {
		return false
	}
	if f.EndTime != nil && e.Timestamp.After(*f.EndTime) {
		return false
	}
	if f.Success != nil && e.Success != *f.Success {
		return false
	}
	return true
}

// exportEntries serializes entries to JSON or CSV.
func exportEntries(entries []*AuditEntry, format string) ([]byte, error) {
	switch strings.ToLower(format) {
	case "json":
		return json.MarshalIndent(entries, "", "  ")
	case "csv":
		return exportCSV(entries)
	default:
		return nil, fmt.Errorf("audit: unsupported export format: %s", format)
	}
}

func exportCSV(entries []*AuditEntry) ([]byte, error) {
	var buf strings.Builder
	w := csv.NewWriter(&buf)

	// Header
	if err := w.Write([]string{
		"id", "timestamp", "actor_id", "actor_type", "action",
		"resource", "resource_id", "tenant_id", "ip_address",
		"user_agent", "success", "error_message",
	}); err != nil {
		return nil, err
	}

	for _, e := range entries {
		success := "false"
		if e.Success {
			success = "true"
		}
		if err := w.Write([]string{
			e.ID,
			e.Timestamp.Format(time.RFC3339),
			e.ActorID,
			e.ActorType,
			e.Action,
			e.Resource,
			e.ResourceID,
			e.TenantID,
			e.IPAddress,
			e.UserAgent,
			success,
			e.ErrorMsg,
		}); err != nil {
			return nil, err
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// ---------------------------------------------------------------------------
// SQLiteAuditLog
// ---------------------------------------------------------------------------

// SQLiteAuditLog persists audit entries in a SQLite database.
type SQLiteAuditLog struct {
	db *sql.DB
}

// NewSQLiteAuditLog opens (or creates) the SQLite database at dbPath and
// initializes the audit_entries table.
func NewSQLiteAuditLog(dbPath string) (*SQLiteAuditLog, error) {
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("audit: open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("audit: ping sqlite: %w", err)
	}

	s := &SQLiteAuditLog{db: db}
	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLiteAuditLog) init() error {
	schema := `
	CREATE TABLE IF NOT EXISTS audit_entries (
		id            TEXT PRIMARY KEY,
		timestamp     DATETIME NOT NULL,
		actor_id      TEXT NOT NULL DEFAULT '',
		actor_type    TEXT NOT NULL DEFAULT '',
		action        TEXT NOT NULL DEFAULT '',
		resource      TEXT NOT NULL DEFAULT '',
		resource_id   TEXT NOT NULL DEFAULT '',
		tenant_id     TEXT NOT NULL DEFAULT '',
		details       TEXT,
		ip_address    TEXT NOT NULL DEFAULT '',
		user_agent    TEXT NOT NULL DEFAULT '',
		success       INTEGER NOT NULL DEFAULT 1,
		error_message TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_audit_tenant ON audit_entries(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_entries(actor_id);
	CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_entries(action);
	CREATE INDEX IF NOT EXISTS idx_audit_resource ON audit_entries(resource);
	CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_entries(timestamp);
	`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("audit: create table: %w", err)
	}
	return nil
}

// Record inserts an audit entry into the database.
func (s *SQLiteAuditLog) Record(_ context.Context, entry *AuditEntry) error {
	if entry == nil {
		return fmt.Errorf("audit: entry must not be nil")
	}
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}

	detailsJSON, err := json.Marshal(entry.Details)
	if err != nil {
		return fmt.Errorf("audit: marshal details: %w", err)
	}

	successInt := 0
	if entry.Success {
		successInt = 1
	}

	_, err = s.db.Exec(`
		INSERT INTO audit_entries (id, timestamp, actor_id, actor_type, action, resource, resource_id,
			tenant_id, details, ip_address, user_agent, success, error_message)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.Timestamp.UTC().Format(time.RFC3339Nano),
		entry.ActorID, entry.ActorType, entry.Action, entry.Resource, entry.ResourceID,
		entry.TenantID, string(detailsJSON), entry.IPAddress, entry.UserAgent,
		successInt, entry.ErrorMsg,
	)
	if err != nil {
		return fmt.Errorf("audit: insert entry: %w", err)
	}
	return nil
}

// Query returns entries matching the filter.
func (s *SQLiteAuditLog) Query(ctx context.Context, filter AuditFilter) ([]*AuditEntry, error) {
	query, args := buildSQLQuery("SELECT id, timestamp, actor_id, actor_type, action, resource, resource_id, tenant_id, details, ip_address, user_agent, success, error_message FROM audit_entries", filter)

	query += " ORDER BY timestamp DESC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("audit: query: %w", err)
	}
	defer rows.Close()

	var results []*AuditEntry
	for rows.Next() {
		e, err := scanAuditEntry(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, e)
	}
	return results, rows.Err()
}

// Count returns the number of entries matching the filter.
func (s *SQLiteAuditLog) Count(ctx context.Context, filter AuditFilter) (int64, error) {
	query, args := buildSQLQuery("SELECT COUNT(*) FROM audit_entries", filter)

	var count int64
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("audit: count: %w", err)
	}
	return count, nil
}

// Export returns entries matching the filter serialized in the given format.
func (s *SQLiteAuditLog) Export(ctx context.Context, filter AuditFilter, format string) ([]byte, error) {
	entries, err := s.Query(ctx, filter)
	if err != nil {
		return nil, err
	}
	return exportEntries(entries, format)
}

// Close closes the underlying database connection.
func (s *SQLiteAuditLog) Close() error {
	return s.db.Close()
}

// buildSQLQuery constructs a WHERE clause from the filter.
func buildSQLQuery(base string, f AuditFilter) (string, []any) {
	var conditions []string
	var args []any

	if f.ActorID != "" {
		conditions = append(conditions, "actor_id = ?")
		args = append(args, f.ActorID)
	}
	if f.Action != "" {
		conditions = append(conditions, "action = ?")
		args = append(args, f.Action)
	}
	if f.Resource != "" {
		conditions = append(conditions, "resource = ?")
		args = append(args, f.Resource)
	}
	if f.TenantID != "" {
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, f.TenantID)
	}
	if f.StartTime != nil {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, f.StartTime.UTC().Format(time.RFC3339Nano))
	}
	if f.EndTime != nil {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, f.EndTime.UTC().Format(time.RFC3339Nano))
	}
	if f.Success != nil {
		val := 0
		if *f.Success {
			val = 1
		}
		conditions = append(conditions, "success = ?")
		args = append(args, val)
	}

	query := base
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	return query, args
}

// scanAuditEntry reads an AuditEntry from a sql.Rows cursor.
func scanAuditEntry(rows *sql.Rows) (*AuditEntry, error) {
	var e AuditEntry
	var tsStr string
	var detailsStr string
	var successInt int

	err := rows.Scan(
		&e.ID, &tsStr, &e.ActorID, &e.ActorType, &e.Action,
		&e.Resource, &e.ResourceID, &e.TenantID, &detailsStr,
		&e.IPAddress, &e.UserAgent, &successInt, &e.ErrorMsg,
	)
	if err != nil {
		return nil, fmt.Errorf("audit: scan row: %w", err)
	}

	e.Timestamp, _ = time.Parse(time.RFC3339Nano, tsStr)
	e.Success = successInt != 0
	if detailsStr != "" && detailsStr != "null" {
		_ = json.Unmarshal([]byte(detailsStr), &e.Details)
	}
	return &e, nil
}
