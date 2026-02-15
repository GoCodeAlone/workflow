package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// IdempotencyRecord represents a stored result for an idempotency key.
type IdempotencyRecord struct {
	Key         string          `json:"key"`
	ExecutionID uuid.UUID       `json:"execution_id"`
	StepName    string          `json:"step_name"`
	Result      json.RawMessage `json:"result"`
	CreatedAt   time.Time       `json:"created_at"`
	ExpiresAt   time.Time       `json:"expires_at"`
}

// IdempotencyStore defines persistence operations for idempotency keys.
type IdempotencyStore interface {
	// Check returns the stored result if the key exists and hasn't expired.
	// Returns nil, nil if the key doesn't exist.
	Check(ctx context.Context, key string) (*IdempotencyRecord, error)
	// Store saves a result for an idempotency key with an expiration time.
	Store(ctx context.Context, record *IdempotencyRecord) error
	// Cleanup removes expired keys.
	Cleanup(ctx context.Context) (int64, error)
}

// ---------------------------------------------------------------------------
// InMemoryIdempotencyStore
// ---------------------------------------------------------------------------

// InMemoryIdempotencyStore is a thread-safe in-memory implementation of
// IdempotencyStore for testing and single-server use.
type InMemoryIdempotencyStore struct {
	mu      sync.Mutex
	records map[string]*IdempotencyRecord
}

// NewInMemoryIdempotencyStore creates a new InMemoryIdempotencyStore.
func NewInMemoryIdempotencyStore() *InMemoryIdempotencyStore {
	return &InMemoryIdempotencyStore{records: make(map[string]*IdempotencyRecord)}
}

func (s *InMemoryIdempotencyStore) Check(_ context.Context, key string) (*IdempotencyRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[key]
	if !ok {
		return nil, nil
	}
	if time.Now().After(rec.ExpiresAt) {
		delete(s.records, key)
		return nil, nil
	}
	cp := *rec
	cp.Result = make(json.RawMessage, len(rec.Result))
	copy(cp.Result, rec.Result)
	return &cp, nil
}

func (s *InMemoryIdempotencyStore) Store(_ context.Context, record *IdempotencyRecord) error {
	if record.Key == "" {
		return fmt.Errorf("idempotency key is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.records[record.Key]; exists {
		return ErrDuplicate
	}
	now := time.Now()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	cp := *record
	cp.Result = make(json.RawMessage, len(record.Result))
	copy(cp.Result, record.Result)
	s.records[record.Key] = &cp
	return nil
}

func (s *InMemoryIdempotencyStore) Cleanup(_ context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	var count int64
	for key, rec := range s.records {
		if now.After(rec.ExpiresAt) {
			delete(s.records, key)
			count++
		}
	}
	return count, nil
}

// ---------------------------------------------------------------------------
// SQLiteIdempotencyStore
// ---------------------------------------------------------------------------

const idempotencyCreateTableSQL = `
CREATE TABLE IF NOT EXISTS idempotency_keys (
	key          TEXT PRIMARY KEY,
	execution_id TEXT NOT NULL,
	step_name    TEXT NOT NULL,
	result       TEXT,
	created_at   DATETIME NOT NULL DEFAULT (datetime('now')),
	expires_at   DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_idempotency_expires ON idempotency_keys(expires_at);
`

// SQLiteIdempotencyStore is a SQLite-backed implementation of IdempotencyStore.
type SQLiteIdempotencyStore struct {
	db *sql.DB
}

// NewSQLiteIdempotencyStore creates a new SQLiteIdempotencyStore and ensures
// the required table exists. The caller is responsible for opening and closing
// the *sql.DB connection.
func NewSQLiteIdempotencyStore(db *sql.DB) (*SQLiteIdempotencyStore, error) {
	if _, err := db.Exec(idempotencyCreateTableSQL); err != nil {
		return nil, fmt.Errorf("create idempotency table: %w", err)
	}
	return &SQLiteIdempotencyStore{db: db}, nil
}

func (s *SQLiteIdempotencyStore) Check(ctx context.Context, key string) (*IdempotencyRecord, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT key, execution_id, step_name, result, created_at, expires_at
		 FROM idempotency_keys
		 WHERE key = ? AND expires_at > datetime('now')`,
		key,
	)

	var rec IdempotencyRecord
	var execID string
	var result sql.NullString
	var createdAt, expiresAt string
	err := row.Scan(&rec.Key, &execID, &rec.StepName, &result, &createdAt, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query idempotency key: %w", err)
	}

	rec.ExecutionID, err = uuid.Parse(execID)
	if err != nil {
		return nil, fmt.Errorf("parse execution_id: %w", err)
	}
	if result.Valid {
		rec.Result = json.RawMessage(result.String)
	}
	rec.CreatedAt, err = parseSQLiteTime(createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	rec.ExpiresAt, err = parseSQLiteTime(expiresAt)
	if err != nil {
		return nil, fmt.Errorf("parse expires_at: %w", err)
	}

	return &rec, nil
}

func (s *SQLiteIdempotencyStore) Store(ctx context.Context, record *IdempotencyRecord) error {
	if record.Key == "" {
		return fmt.Errorf("idempotency key is required")
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}

	var resultStr sql.NullString
	if record.Result != nil {
		resultStr = sql.NullString{String: string(record.Result), Valid: true}
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO idempotency_keys (key, execution_id, step_name, result, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		record.Key,
		record.ExecutionID.String(),
		record.StepName,
		resultStr,
		record.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
		record.ExpiresAt.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		// SQLite UNIQUE constraint violation results in an error containing "UNIQUE".
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("insert idempotency key: %w", err)
	}
	return nil
}

func (s *SQLiteIdempotencyStore) Cleanup(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM idempotency_keys WHERE expires_at <= datetime('now')`,
	)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired keys: %w", err)
	}
	return res.RowsAffected()
}

// isUniqueViolation checks if an error is a SQLite UNIQUE constraint violation.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// modernc.org/sqlite returns errors containing "UNIQUE constraint failed"
	return strings.Contains(err.Error(), "UNIQUE")
}

// sqliteTimeFormats lists the time formats that SQLite may return.
var sqliteTimeFormats = []string{
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05Z",
	"2006-01-02T15:04:05",
	time.RFC3339,
}

// parseSQLiteTime parses a time string returned by SQLite.
func parseSQLiteTime(s string) (time.Time, error) {
	for _, layout := range sqliteTimeFormats {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time format: %q", s)
}

// ---------------------------------------------------------------------------
// Compile-time interface assertions
// ---------------------------------------------------------------------------

var (
	_ IdempotencyStore = (*InMemoryIdempotencyStore)(nil)
	_ IdempotencyStore = (*SQLiteIdempotencyStore)(nil)
)
