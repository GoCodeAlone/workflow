package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// DLQ types
// ---------------------------------------------------------------------------

// DLQStatus represents the status of a dead letter queue entry.
type DLQStatus string

const (
	DLQStatusPending   DLQStatus = "pending"
	DLQStatusRetrying  DLQStatus = "retrying"
	DLQStatusResolved  DLQStatus = "resolved"
	DLQStatusDiscarded DLQStatus = "discarded"
)

// DLQEntry represents a failed event/message in the dead letter queue.
type DLQEntry struct {
	ID            uuid.UUID       `json:"id"`
	OriginalEvent json.RawMessage `json:"original_event"`
	PipelineName  string          `json:"pipeline_name"`
	StepName      string          `json:"step_name"`
	ErrorMessage  string          `json:"error_message"`
	ErrorType     string          `json:"error_type"`
	RetryCount    int             `json:"retry_count"`
	MaxRetries    int             `json:"max_retries"`
	Status        DLQStatus       `json:"status"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	ResolvedAt    *time.Time      `json:"resolved_at,omitempty"`
	Metadata      map[string]any  `json:"metadata,omitempty"`
}

// DLQFilter specifies criteria for listing DLQ entries.
type DLQFilter struct {
	PipelineName string
	StepName     string
	Status       DLQStatus
	ErrorType    string
	Limit        int
	Offset       int
}

// ---------------------------------------------------------------------------
// DLQStore interface
// ---------------------------------------------------------------------------

// DLQStore defines persistence operations for dead letter queue entries.
type DLQStore interface {
	// Add inserts a new DLQ entry.
	Add(ctx context.Context, entry *DLQEntry) error
	// Get retrieves a DLQ entry by ID.
	Get(ctx context.Context, id uuid.UUID) (*DLQEntry, error)
	// List returns DLQ entries matching the given filter.
	List(ctx context.Context, filter DLQFilter) ([]*DLQEntry, error)
	// Count returns the number of DLQ entries matching the given filter.
	Count(ctx context.Context, filter DLQFilter) (int64, error)
	// UpdateStatus sets the status of a DLQ entry.
	UpdateStatus(ctx context.Context, id uuid.UUID, status DLQStatus) error
	// Retry increments retry_count and sets status to "retrying".
	Retry(ctx context.Context, id uuid.UUID) error
	// Discard sets the status to "discarded".
	Discard(ctx context.Context, id uuid.UUID) error
	// Resolve sets the status to "resolved" and sets resolved_at.
	Resolve(ctx context.Context, id uuid.UUID) error
	// Purge removes resolved/discarded entries older than the given duration.
	Purge(ctx context.Context, olderThan time.Duration) (int64, error)
}

// ===========================================================================
// InMemoryDLQStore
// ===========================================================================

// InMemoryDLQStore is a thread-safe in-memory implementation of DLQStore.
type InMemoryDLQStore struct {
	mu      sync.RWMutex
	entries map[uuid.UUID]*DLQEntry
}

// NewInMemoryDLQStore creates a new InMemoryDLQStore.
func NewInMemoryDLQStore() *InMemoryDLQStore {
	return &InMemoryDLQStore{
		entries: make(map[uuid.UUID]*DLQEntry),
	}
}

func (s *InMemoryDLQStore) Add(_ context.Context, entry *DLQEntry) error {
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	now := time.Now()
	entry.CreatedAt = now
	entry.UpdatedAt = now
	if entry.Status == "" {
		entry.Status = DLQStatusPending
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Store a copy to prevent external mutation.
	cp := *entry
	if entry.Metadata != nil {
		cp.Metadata = make(map[string]any, len(entry.Metadata))
		for k, v := range entry.Metadata {
			cp.Metadata[k] = v
		}
	}
	if entry.OriginalEvent != nil {
		cp.OriginalEvent = make(json.RawMessage, len(entry.OriginalEvent))
		copy(cp.OriginalEvent, entry.OriginalEvent)
	}
	s.entries[cp.ID] = &cp
	return nil
}

func (s *InMemoryDLQStore) Get(_ context.Context, id uuid.UUID) (*DLQEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.entries[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *entry
	return &cp, nil
}

func (s *InMemoryDLQStore) List(_ context.Context, filter DLQFilter) ([]*DLQEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*DLQEntry
	for _, entry := range s.entries {
		if !matchesDLQFilter(entry, filter) {
			continue
		}
		cp := *entry
		results = append(results, &cp)
	}

	// Sort by created_at descending (most recent first).
	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})

	// Apply offset/limit.
	if filter.Offset > 0 {
		if filter.Offset >= len(results) {
			return []*DLQEntry{}, nil
		}
		results = results[filter.Offset:]
	}
	if filter.Limit > 0 && filter.Limit < len(results) {
		results = results[:filter.Limit]
	}

	return results, nil
}

func (s *InMemoryDLQStore) Count(_ context.Context, filter DLQFilter) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int64
	for _, entry := range s.entries {
		if matchesDLQFilter(entry, filter) {
			count++
		}
	}
	return count, nil
}

func (s *InMemoryDLQStore) UpdateStatus(_ context.Context, id uuid.UUID, status DLQStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[id]
	if !ok {
		return ErrNotFound
	}
	entry.Status = status
	entry.UpdatedAt = time.Now()
	return nil
}

func (s *InMemoryDLQStore) Retry(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[id]
	if !ok {
		return ErrNotFound
	}
	entry.RetryCount++
	entry.Status = DLQStatusRetrying
	entry.UpdatedAt = time.Now()
	return nil
}

func (s *InMemoryDLQStore) Discard(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[id]
	if !ok {
		return ErrNotFound
	}
	entry.Status = DLQStatusDiscarded
	entry.UpdatedAt = time.Now()
	return nil
}

func (s *InMemoryDLQStore) Resolve(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[id]
	if !ok {
		return ErrNotFound
	}
	now := time.Now()
	entry.Status = DLQStatusResolved
	entry.ResolvedAt = &now
	entry.UpdatedAt = now
	return nil
}

func (s *InMemoryDLQStore) Purge(_ context.Context, olderThan time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	var count int64
	for id, entry := range s.entries {
		if (entry.Status == DLQStatusResolved || entry.Status == DLQStatusDiscarded) &&
			entry.UpdatedAt.Before(cutoff) {
			delete(s.entries, id)
			count++
		}
	}
	return count, nil
}

// matchesDLQFilter checks whether a DLQ entry matches the given filter criteria.
func matchesDLQFilter(entry *DLQEntry, filter DLQFilter) bool {
	if filter.PipelineName != "" && entry.PipelineName != filter.PipelineName {
		return false
	}
	if filter.StepName != "" && entry.StepName != filter.StepName {
		return false
	}
	if filter.Status != "" && entry.Status != filter.Status {
		return false
	}
	if filter.ErrorType != "" && entry.ErrorType != filter.ErrorType {
		return false
	}
	return true
}

// ===========================================================================
// SQLiteDLQStore
// ===========================================================================

// SQLiteDLQStore implements DLQStore backed by SQLite.
type SQLiteDLQStore struct {
	mu sync.Mutex // serializes writes
	db *sql.DB
}

// NewSQLiteDLQStore creates a new SQLiteDLQStore using the given database path.
func NewSQLiteDLQStore(dbPath string) (*SQLiteDLQStore, error) {
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	s := &SQLiteDLQStore{db: db}
	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// NewSQLiteDLQStoreFromDB wraps an existing *sql.DB connection.
func NewSQLiteDLQStoreFromDB(db *sql.DB) (*SQLiteDLQStore, error) {
	s := &SQLiteDLQStore{db: db}
	if err := s.init(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SQLiteDLQStore) init() error {
	schema := `
	CREATE TABLE IF NOT EXISTS dlq_entries (
		id              TEXT PRIMARY KEY,
		original_event  TEXT,
		pipeline_name   TEXT NOT NULL,
		step_name       TEXT NOT NULL,
		error_message   TEXT NOT NULL,
		error_type      TEXT NOT NULL,
		retry_count     INTEGER NOT NULL DEFAULT 0,
		max_retries     INTEGER NOT NULL DEFAULT 0,
		status          TEXT NOT NULL DEFAULT 'pending',
		created_at      TEXT NOT NULL,
		updated_at      TEXT NOT NULL,
		resolved_at     TEXT,
		metadata        TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_dlq_entries_pipeline_name ON dlq_entries(pipeline_name);
	CREATE INDEX IF NOT EXISTS idx_dlq_entries_step_name ON dlq_entries(step_name);
	CREATE INDEX IF NOT EXISTS idx_dlq_entries_status ON dlq_entries(status);
	CREATE INDEX IF NOT EXISTS idx_dlq_entries_error_type ON dlq_entries(error_type);
	CREATE INDEX IF NOT EXISTS idx_dlq_entries_created_at ON dlq_entries(created_at);
	CREATE INDEX IF NOT EXISTS idx_dlq_entries_updated_at ON dlq_entries(updated_at);
	`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("create dlq_entries table: %w", err)
	}
	return nil
}

// Close closes the underlying database connection.
func (s *SQLiteDLQStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteDLQStore) Add(ctx context.Context, entry *DLQEntry) error {
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	now := time.Now().UTC()
	entry.CreatedAt = now
	entry.UpdatedAt = now
	if entry.Status == "" {
		entry.Status = DLQStatusPending
	}

	var metadataJSON []byte
	if entry.Metadata != nil {
		var err error
		metadataJSON, err = json.Marshal(entry.Metadata)
		if err != nil {
			return fmt.Errorf("marshal metadata: %w", err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO dlq_entries (id, original_event, pipeline_name, step_name, error_message, error_type,
			retry_count, max_retries, status, created_at, updated_at, resolved_at, metadata)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID.String(),
		string(entry.OriginalEvent),
		entry.PipelineName,
		entry.StepName,
		entry.ErrorMessage,
		entry.ErrorType,
		entry.RetryCount,
		entry.MaxRetries,
		string(entry.Status),
		entry.CreatedAt.Format(time.RFC3339Nano),
		entry.UpdatedAt.Format(time.RFC3339Nano),
		nil, // resolved_at
		nullString(metadataJSON),
	)
	if err != nil {
		return fmt.Errorf("insert dlq entry: %w", err)
	}
	return nil
}

func (s *SQLiteDLQStore) Get(ctx context.Context, id uuid.UUID) (*DLQEntry, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, original_event, pipeline_name, step_name, error_message, error_type,
			retry_count, max_retries, status, created_at, updated_at, resolved_at, metadata
		 FROM dlq_entries WHERE id = ?`,
		id.String(),
	)
	return scanDLQEntry(row)
}

func (s *SQLiteDLQStore) List(ctx context.Context, filter DLQFilter) ([]*DLQEntry, error) {
	query, args := buildDLQQuery("SELECT id, original_event, pipeline_name, step_name, error_message, error_type, retry_count, max_retries, status, created_at, updated_at, resolved_at, metadata FROM dlq_entries", filter)
	query += " ORDER BY created_at DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
		if filter.Offset > 0 {
			query += fmt.Sprintf(" OFFSET %d", filter.Offset)
		}
	} else if filter.Offset > 0 {
		// SQLite requires LIMIT before OFFSET; use -1 for unlimited.
		query += fmt.Sprintf(" LIMIT -1 OFFSET %d", filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query dlq entries: %w", err)
	}
	defer rows.Close()

	var results []*DLQEntry
	for rows.Next() {
		entry, err := scanDLQEntryRows(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if results == nil {
		results = []*DLQEntry{}
	}
	return results, nil
}

func (s *SQLiteDLQStore) Count(ctx context.Context, filter DLQFilter) (int64, error) {
	query, args := buildDLQQuery("SELECT COUNT(*) FROM dlq_entries", filter)

	var count int64
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count dlq entries: %w", err)
	}
	return count, nil
}

func (s *SQLiteDLQStore) UpdateStatus(ctx context.Context, id uuid.UUID, status DLQStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		`UPDATE dlq_entries SET status = ?, updated_at = ? WHERE id = ?`,
		string(status), now.Format(time.RFC3339Nano), id.String(),
	)
	if err != nil {
		return fmt.Errorf("update dlq status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteDLQStore) Retry(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		`UPDATE dlq_entries SET retry_count = retry_count + 1, status = ?, updated_at = ? WHERE id = ?`,
		string(DLQStatusRetrying), now.Format(time.RFC3339Nano), id.String(),
	)
	if err != nil {
		return fmt.Errorf("retry dlq entry: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteDLQStore) Discard(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		`UPDATE dlq_entries SET status = ?, updated_at = ? WHERE id = ?`,
		string(DLQStatusDiscarded), now.Format(time.RFC3339Nano), id.String(),
	)
	if err != nil {
		return fmt.Errorf("discard dlq entry: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteDLQStore) Resolve(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		`UPDATE dlq_entries SET status = ?, resolved_at = ?, updated_at = ? WHERE id = ?`,
		string(DLQStatusResolved), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), id.String(),
	)
	if err != nil {
		return fmt.Errorf("resolve dlq entry: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteDLQStore) Purge(ctx context.Context, olderThan time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().UTC().Add(-olderThan).Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM dlq_entries WHERE status IN (?, ?) AND updated_at < ?`,
		string(DLQStatusResolved), string(DLQStatusDiscarded), cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("purge dlq entries: %w", err)
	}
	return result.RowsAffected()
}

// ---------------------------------------------------------------------------
// SQLite helpers
// ---------------------------------------------------------------------------

// nullString converts a byte slice to a sql.NullString for insertion.
func nullString(b []byte) sql.NullString {
	if b == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: string(b), Valid: true}
}

// buildDLQQuery constructs a WHERE clause from a DLQFilter.
func buildDLQQuery(base string, filter DLQFilter) (string, []any) {
	var conditions []string
	var args []any

	if filter.PipelineName != "" {
		conditions = append(conditions, "pipeline_name = ?")
		args = append(args, filter.PipelineName)
	}
	if filter.StepName != "" {
		conditions = append(conditions, "step_name = ?")
		args = append(args, filter.StepName)
	}
	if filter.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, string(filter.Status))
	}
	if filter.ErrorType != "" {
		conditions = append(conditions, "error_type = ?")
		args = append(args, filter.ErrorType)
	}

	query := base
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	return query, args
}

// scanDLQEntry scans a single row into a DLQEntry.
func scanDLQEntry(row *sql.Row) (*DLQEntry, error) {
	var entry DLQEntry
	var idStr, statusStr, createdStr, updatedStr string
	var originalEvent, metadataStr sql.NullString
	var resolvedStr sql.NullString

	err := row.Scan(
		&idStr, &originalEvent,
		&entry.PipelineName, &entry.StepName,
		&entry.ErrorMessage, &entry.ErrorType,
		&entry.RetryCount, &entry.MaxRetries,
		&statusStr, &createdStr, &updatedStr,
		&resolvedStr, &metadataStr,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan dlq entry: %w", err)
	}

	entry.ID, _ = uuid.Parse(idStr)
	entry.Status = DLQStatus(statusStr)
	entry.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	entry.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)

	if originalEvent.Valid {
		entry.OriginalEvent = json.RawMessage(originalEvent.String)
	}
	if resolvedStr.Valid {
		t, _ := time.Parse(time.RFC3339Nano, resolvedStr.String)
		entry.ResolvedAt = &t
	}
	if metadataStr.Valid && metadataStr.String != "" {
		_ = json.Unmarshal([]byte(metadataStr.String), &entry.Metadata)
	}

	return &entry, nil
}

// scanDLQEntryRows scans a row from *sql.Rows into a DLQEntry.
func scanDLQEntryRows(rows *sql.Rows) (*DLQEntry, error) {
	var entry DLQEntry
	var idStr, statusStr, createdStr, updatedStr string
	var originalEvent, metadataStr sql.NullString
	var resolvedStr sql.NullString

	err := rows.Scan(
		&idStr, &originalEvent,
		&entry.PipelineName, &entry.StepName,
		&entry.ErrorMessage, &entry.ErrorType,
		&entry.RetryCount, &entry.MaxRetries,
		&statusStr, &createdStr, &updatedStr,
		&resolvedStr, &metadataStr,
	)
	if err != nil {
		return nil, fmt.Errorf("scan dlq entry: %w", err)
	}

	entry.ID, _ = uuid.Parse(idStr)
	entry.Status = DLQStatus(statusStr)
	entry.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	entry.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)

	if originalEvent.Valid {
		entry.OriginalEvent = json.RawMessage(originalEvent.String)
	}
	if resolvedStr.Valid {
		t, _ := time.Parse(time.RFC3339Nano, resolvedStr.String)
		entry.ResolvedAt = &t
	}
	if metadataStr.Valid && metadataStr.String != "" {
		_ = json.Unmarshal([]byte(metadataStr.String), &entry.Metadata)
	}

	return &entry, nil
}

// ---------------------------------------------------------------------------
// Compile-time interface assertions
// ---------------------------------------------------------------------------

var (
	_ DLQStore = (*InMemoryDLQStore)(nil)
	_ DLQStore = (*SQLiteDLQStore)(nil)
)
