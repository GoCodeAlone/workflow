package generic

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Store persists flag definitions, targeting rules, and overrides in SQLite.
type Store struct {
	db *sql.DB
}

// NewStore opens (or creates) a SQLite database at the given path and
// ensures the schema tables exist.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// NewStoreFromDB wraps an existing *sql.DB (useful for testing).
func NewStoreFromDB(db *sql.DB) (*Store, error) {
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS feature_flags (
			key         TEXT PRIMARY KEY,
			type        TEXT NOT NULL DEFAULT 'boolean',
			description TEXT NOT NULL DEFAULT '',
			enabled     INTEGER NOT NULL DEFAULT 1,
			default_val TEXT NOT NULL DEFAULT 'false',
			tags        TEXT NOT NULL DEFAULT '[]',
			percentage  REAL NOT NULL DEFAULT 0,
			created_at  TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS flag_rules (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			flag_key    TEXT NOT NULL REFERENCES feature_flags(key) ON DELETE CASCADE,
			priority    INTEGER NOT NULL DEFAULT 0,
			attribute   TEXT NOT NULL,
			operator    TEXT NOT NULL,
			value       TEXT NOT NULL,
			serve_value TEXT NOT NULL,
			created_at  TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS flag_overrides (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			flag_key   TEXT NOT NULL REFERENCES feature_flags(key) ON DELETE CASCADE,
			scope      TEXT NOT NULL DEFAULT 'user',
			scope_key  TEXT NOT NULL,
			value      TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			UNIQUE(flag_key, scope, scope_key)
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt[:40], err)
		}
	}
	return nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// ---------- Flag CRUD ----------

// FlagRow represents a row in the feature_flags table.
type FlagRow struct {
	Key         string    `json:"key"`
	Type        string    `json:"type"`
	Description string    `json:"description"`
	Enabled     bool      `json:"enabled"`
	DefaultVal  string    `json:"default_val"`
	Tags        []string  `json:"tags"`
	Percentage  float64   `json:"percentage"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// GetFlag retrieves a single flag definition by key.
func (s *Store) GetFlag(ctx context.Context, key string) (*FlagRow, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT key, type, description, enabled, default_val, tags, percentage, created_at, updated_at
		 FROM feature_flags WHERE key = ?`, key)

	var f FlagRow
	var enabled int
	var tagsJSON string
	var createdStr, updatedStr string
	err := row.Scan(&f.Key, &f.Type, &f.Description, &enabled, &f.DefaultVal, &tagsJSON, &f.Percentage, &createdStr, &updatedStr)
	if err != nil {
		return nil, err
	}
	f.Enabled = enabled != 0
	_ = json.Unmarshal([]byte(tagsJSON), &f.Tags)
	if f.Tags == nil {
		f.Tags = []string{}
	}
	f.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
	f.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedStr)
	return &f, nil
}

// ListFlags returns all flag definitions.
func (s *Store) ListFlags(ctx context.Context) ([]FlagRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, type, description, enabled, default_val, tags, percentage, created_at, updated_at
		 FROM feature_flags ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var flags []FlagRow
	for rows.Next() {
		var f FlagRow
		var enabled int
		var tagsJSON string
		var createdStr, updatedStr string
		if err := rows.Scan(&f.Key, &f.Type, &f.Description, &enabled, &f.DefaultVal, &tagsJSON, &f.Percentage, &createdStr, &updatedStr); err != nil {
			return nil, err
		}
		f.Enabled = enabled != 0
		_ = json.Unmarshal([]byte(tagsJSON), &f.Tags)
		if f.Tags == nil {
			f.Tags = []string{}
		}
		f.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
		f.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedStr)
		flags = append(flags, f)
	}
	return flags, rows.Err()
}

// UpsertFlag creates or updates a flag definition.
func (s *Store) UpsertFlag(ctx context.Context, f *FlagRow) error {
	tagsJSON, _ := json.Marshal(f.Tags)
	enabled := 0
	if f.Enabled {
		enabled = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO feature_flags (key, type, description, enabled, default_val, tags, percentage, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
		 ON CONFLICT(key) DO UPDATE SET
			type=excluded.type,
			description=excluded.description,
			enabled=excluded.enabled,
			default_val=excluded.default_val,
			tags=excluded.tags,
			percentage=excluded.percentage,
			updated_at=datetime('now')`,
		f.Key, f.Type, f.Description, enabled, f.DefaultVal, string(tagsJSON), f.Percentage)
	return err
}

// DeleteFlag removes a flag and its related rules and overrides (cascade).
func (s *Store) DeleteFlag(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM feature_flags WHERE key = ?`, key)
	return err
}

// ---------- Rules ----------

// RuleRow represents a targeting rule row.
type RuleRow struct {
	ID         int64  `json:"id"`
	FlagKey    string `json:"flag_key"`
	Priority   int    `json:"priority"`
	Attribute  string `json:"attribute"`
	Operator   string `json:"operator"`
	Value      string `json:"value"`
	ServeValue string `json:"serve_value"`
}

// GetRules returns all targeting rules for a flag, ordered by priority ascending.
func (s *Store) GetRules(ctx context.Context, flagKey string) ([]RuleRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, flag_key, priority, attribute, operator, value, serve_value
		 FROM flag_rules WHERE flag_key = ? ORDER BY priority ASC`, flagKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []RuleRow
	for rows.Next() {
		var r RuleRow
		if err := rows.Scan(&r.ID, &r.FlagKey, &r.Priority, &r.Attribute, &r.Operator, &r.Value, &r.ServeValue); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// AddRule inserts a new targeting rule.
func (s *Store) AddRule(ctx context.Context, r *RuleRow) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO flag_rules (flag_key, priority, attribute, operator, value, serve_value)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		r.FlagKey, r.Priority, r.Attribute, r.Operator, r.Value, r.ServeValue)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	r.ID = id
	return nil
}

// DeleteRule removes a targeting rule by ID.
func (s *Store) DeleteRule(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM flag_rules WHERE id = ?`, id)
	return err
}

// ---------- Overrides ----------

// OverrideRow represents a flag override for a specific user or group.
type OverrideRow struct {
	ID       int64  `json:"id"`
	FlagKey  string `json:"flag_key"`
	Scope    string `json:"scope"`     // "user" or "group"
	ScopeKey string `json:"scope_key"` // user ID or group name
	Value    string `json:"value"`
}

// GetOverrides returns all overrides for a flag.
func (s *Store) GetOverrides(ctx context.Context, flagKey string) ([]OverrideRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, flag_key, scope, scope_key, value
		 FROM flag_overrides WHERE flag_key = ? ORDER BY scope, scope_key`, flagKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var overrides []OverrideRow
	for rows.Next() {
		var o OverrideRow
		if err := rows.Scan(&o.ID, &o.FlagKey, &o.Scope, &o.ScopeKey, &o.Value); err != nil {
			return nil, err
		}
		overrides = append(overrides, o)
	}
	return overrides, rows.Err()
}

// UpsertOverride creates or updates an override.
func (s *Store) UpsertOverride(ctx context.Context, o *OverrideRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO flag_overrides (flag_key, scope, scope_key, value)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(flag_key, scope, scope_key) DO UPDATE SET value=excluded.value`,
		o.FlagKey, o.Scope, o.ScopeKey, o.Value)
	return err
}

// DeleteOverride removes an override by ID.
func (s *Store) DeleteOverride(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM flag_overrides WHERE id = ?`, id)
	return err
}
