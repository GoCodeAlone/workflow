package environment

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store defines CRUD operations for environments.
type Store interface {
	Create(ctx context.Context, env *Environment) error
	Get(ctx context.Context, id string) (*Environment, error)
	List(ctx context.Context, filter Filter) ([]Environment, error)
	Update(ctx context.Context, env *Environment) error
	Delete(ctx context.Context, id string) error
}

// SQLiteStore implements Store backed by a SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens the SQLite database at dbPath and creates the
// environments table if it does not already exist.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	if dir := filepath.Dir(dbPath); dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create data directory: %w", err)
		}
	}
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	createSQL := `CREATE TABLE IF NOT EXISTS environments (
		id         TEXT PRIMARY KEY,
		workflow_id TEXT NOT NULL,
		name       TEXT NOT NULL,
		provider   TEXT NOT NULL,
		region     TEXT NOT NULL DEFAULT '',
		config     TEXT NOT NULL DEFAULT '{}',
		secrets    TEXT NOT NULL DEFAULT '{}',
		status     TEXT NOT NULL DEFAULT 'provisioning',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`
	if _, err := db.Exec(createSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("create table: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// Close closes the underlying database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// Create inserts a new environment. A UUID is generated for ID, and
// CreatedAt/UpdatedAt are set to now.
func (s *SQLiteStore) Create(ctx context.Context, env *Environment) error {
	env.ID = newUUID()
	now := time.Now().UTC()
	env.CreatedAt = now
	env.UpdatedAt = now

	configJSON, err := json.Marshal(env.Config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	secretsJSON, err := json.Marshal(env.Secrets)
	if err != nil {
		return fmt.Errorf("marshal secrets: %w", err)
	}

	if env.Status == "" {
		env.Status = StatusProvisioning
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO environments (id, workflow_id, name, provider, region, config, secrets, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		env.ID, env.WorkflowID, env.Name, env.Provider, env.Region,
		string(configJSON), string(secretsJSON), env.Status,
		env.CreatedAt.Format(time.RFC3339), env.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

// Get retrieves a single environment by ID.
func (s *SQLiteStore) Get(ctx context.Context, id string) (*Environment, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, workflow_id, name, provider, region, config, secrets, status, created_at, updated_at
		 FROM environments WHERE id = ?`, id)
	return scanEnvironment(row)
}

// List returns environments matching the optional filter criteria.
func (s *SQLiteStore) List(ctx context.Context, filter Filter) ([]Environment, error) {
	query := `SELECT id, workflow_id, name, provider, region, config, secrets, status, created_at, updated_at FROM environments WHERE 1=1`
	var args []any

	if filter.WorkflowID != "" {
		query += " AND workflow_id = ?"
		args = append(args, filter.WorkflowID)
	}
	if filter.Provider != "" {
		query += " AND provider = ?"
		args = append(args, filter.Provider)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}

	query += " ORDER BY created_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var envs []Environment
	for rows.Next() {
		var env Environment
		var configStr, secretsStr, createdStr, updatedStr string
		if err := rows.Scan(
			&env.ID, &env.WorkflowID, &env.Name, &env.Provider, &env.Region,
			&configStr, &secretsStr, &env.Status, &createdStr, &updatedStr,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(configStr), &env.Config); err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
		}
		if err := json.Unmarshal([]byte(secretsStr), &env.Secrets); err != nil {
			return nil, fmt.Errorf("unmarshal secrets: %w", err)
		}
		env.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		env.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		envs = append(envs, env)
	}
	return envs, rows.Err()
}

// Update modifies an existing environment. UpdatedAt is set to now.
func (s *SQLiteStore) Update(ctx context.Context, env *Environment) error {
	env.UpdatedAt = time.Now().UTC()

	configJSON, err := json.Marshal(env.Config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	secretsJSON, err := json.Marshal(env.Secrets)
	if err != nil {
		return fmt.Errorf("marshal secrets: %w", err)
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE environments SET workflow_id=?, name=?, provider=?, region=?, config=?, secrets=?, status=?, updated_at=?
		 WHERE id=?`,
		env.WorkflowID, env.Name, env.Provider, env.Region,
		string(configJSON), string(secretsJSON), env.Status,
		env.UpdatedAt.Format(time.RFC3339), env.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Delete removes an environment by ID.
func (s *SQLiteStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM environments WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// scanEnvironment scans a single row into an Environment.
func scanEnvironment(row *sql.Row) (*Environment, error) {
	var env Environment
	var configStr, secretsStr, createdStr, updatedStr string
	if err := row.Scan(
		&env.ID, &env.WorkflowID, &env.Name, &env.Provider, &env.Region,
		&configStr, &secretsStr, &env.Status, &createdStr, &updatedStr,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(configStr), &env.Config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if err := json.Unmarshal([]byte(secretsStr), &env.Secrets); err != nil {
		return nil, fmt.Errorf("unmarshal secrets: %w", err)
	}
	env.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	env.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
	return &env, nil
}

// newUUID generates a v4 UUID using crypto/rand.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
