package module

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGRow abstracts a single-row query result, allowing mock injection in tests.
type PGRow interface {
	Scan(dest ...any) error
	IsNoRows() bool
}

// PostgresConn abstracts the database operations used by PostgresIaCStateStore.
type PostgresConn interface {
	UpsertState(ctx context.Context, state *IaCState) error
	GetState(ctx context.Context, name string) (*IaCState, error)
	ListRows(ctx context.Context) ([]*IaCState, error)
	DeleteRow(ctx context.Context, name string) (deleted bool, err error)
	AcquireAdvisoryLock(ctx context.Context, key int64) error
	ReleaseAdvisoryLock(ctx context.Context, key int64) (released bool, err error)
	Close()
}

// PostgresIaCStateStore persists IaC state in a PostgreSQL table using pgx/v5.
// Locking uses pg_advisory_lock() for serialised access per resource.
type PostgresIaCStateStore struct {
	conn PostgresConn
	mu   sync.Mutex
	held map[string]int64 // resourceID -> advisory key
}

// NewPostgresIaCStateStore creates a PostgreSQL-backed state store.
// dsn is a standard PostgreSQL connection string or DSN.
// The iac_resources table is created if it does not exist.
func NewPostgresIaCStateStore(ctx context.Context, dsn string) (*PostgresIaCStateStore, error) {
	if dsn == "" {
		return nil, fmt.Errorf("iac postgres state: dsn must not be empty")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("iac postgres state: connect: %w", err)
	}
	conn := &pgxRealConn{pool: pool}
	if err := conn.createTable(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("iac postgres state: create table: %w", err)
	}
	return &PostgresIaCStateStore{
		conn: conn,
		held: make(map[string]int64),
	}, nil
}

// NewPostgresIaCStateStoreWithConn creates a store with an injected connection (for testing).
func NewPostgresIaCStateStoreWithConn(conn PostgresConn) *PostgresIaCStateStore {
	return &PostgresIaCStateStore{
		conn: conn,
		held: make(map[string]int64),
	}
}

// GetState retrieves a state record by resource ID. Returns nil, nil when not found.
func (s *PostgresIaCStateStore) GetState(resourceID string) (*IaCState, error) {
	st, err := s.conn.GetState(context.Background(), resourceID)
	if err != nil {
		return nil, fmt.Errorf("iac postgres state: GetState %q: %w", resourceID, err)
	}
	return st, nil
}

// SaveState inserts or replaces a state record.
func (s *PostgresIaCStateStore) SaveState(state *IaCState) error {
	if state == nil {
		return fmt.Errorf("iac postgres state: SaveState: state must not be nil")
	}
	if state.ResourceID == "" {
		return fmt.Errorf("iac postgres state: SaveState: resource_id must not be empty")
	}
	if err := s.conn.UpsertState(context.Background(), state); err != nil {
		return fmt.Errorf("iac postgres state: SaveState %q: %w", state.ResourceID, err)
	}
	return nil
}

// ListStates returns all state records matching the provided key=value filter.
func (s *PostgresIaCStateStore) ListStates(filter map[string]string) ([]*IaCState, error) {
	rows, err := s.conn.ListRows(context.Background())
	if err != nil {
		return nil, fmt.Errorf("iac postgres state: ListStates: %w", err)
	}
	var results []*IaCState
	for _, st := range rows {
		if matchesFilter(st, filter) {
			results = append(results, st)
		}
	}
	return results, nil
}

// DeleteState removes a state record by resource ID.
func (s *PostgresIaCStateStore) DeleteState(resourceID string) error {
	deleted, err := s.conn.DeleteRow(context.Background(), resourceID)
	if err != nil {
		return fmt.Errorf("iac postgres state: DeleteState %q: %w", resourceID, err)
	}
	if !deleted {
		return fmt.Errorf("iac postgres state: DeleteState %q: not found", resourceID)
	}
	return nil
}

// Lock acquires a PostgreSQL advisory lock for the resource.
func (s *PostgresIaCStateStore) Lock(resourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, held := s.held[resourceID]; held {
		return fmt.Errorf("iac postgres state: Lock %q: already locked", resourceID)
	}
	key := advisoryKey(resourceID)
	if err := s.conn.AcquireAdvisoryLock(context.Background(), key); err != nil {
		return fmt.Errorf("iac postgres state: Lock %q: %w", resourceID, err)
	}
	s.held[resourceID] = key
	return nil
}

// Unlock releases the PostgreSQL advisory lock for the resource.
func (s *PostgresIaCStateStore) Unlock(resourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key, held := s.held[resourceID]
	if !held {
		return fmt.Errorf("iac postgres state: Unlock %q: not locked", resourceID)
	}
	if _, err := s.conn.ReleaseAdvisoryLock(context.Background(), key); err != nil {
		return fmt.Errorf("iac postgres state: Unlock %q: %w", resourceID, err)
	}
	delete(s.held, resourceID)
	return nil
}

// advisoryKey converts a string resource ID to a stable int64 for pg_advisory_lock.
func advisoryKey(resourceID string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(resourceID))
	return int64(h.Sum64())
}

// pgxRealConn wraps the real pgxpool.Pool to satisfy PostgresConn.
type pgxRealConn struct {
	pool *pgxpool.Pool
}

const createTableSQL = `
CREATE TABLE IF NOT EXISTS iac_resources (
    name           TEXT PRIMARY KEY,
    type           TEXT NOT NULL DEFAULT '',
    provider       TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT '',
    config_hash    TEXT NOT NULL DEFAULT '',
    applied_config JSONB NOT NULL DEFAULT '{}',
    outputs        JSONB NOT NULL DEFAULT '{}',
    dependencies   TEXT[] NOT NULL DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`

func (c *pgxRealConn) createTable(ctx context.Context) error {
	_, err := c.pool.Exec(ctx, createTableSQL)
	return err
}

func (c *pgxRealConn) UpsertState(ctx context.Context, st *IaCState) error {
	cfg, err := json.Marshal(st.Config)
	if err != nil {
		return err
	}
	out, err := json.Marshal(st.Outputs)
	if err != nil {
		return err
	}
	_, err = c.pool.Exec(ctx, `
		INSERT INTO iac_resources (name, type, provider, status, applied_config, outputs, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (name) DO UPDATE SET
			type           = EXCLUDED.type,
			provider       = EXCLUDED.provider,
			status         = EXCLUDED.status,
			applied_config = EXCLUDED.applied_config,
			outputs        = EXCLUDED.outputs,
			updated_at     = NOW()
	`, st.ResourceID, st.ResourceType, st.Provider, st.Status, string(cfg), string(out))
	return err
}

func (c *pgxRealConn) GetState(ctx context.Context, name string) (*IaCState, error) {
	var st IaCState
	var cfgJSON, outJSON string
	var deps []string
	err := c.pool.QueryRow(ctx, `
		SELECT name, type, provider, status, applied_config::text, outputs::text, dependencies, created_at, updated_at
		FROM iac_resources WHERE name = $1
	`, name).Scan(&st.ResourceID, &st.ResourceType, &st.Provider, &st.Status,
		&cfgJSON, &outJSON, &deps, &st.CreatedAt, &st.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(cfgJSON), &st.Config)
	_ = json.Unmarshal([]byte(outJSON), &st.Outputs)
	return &st, nil
}

func (c *pgxRealConn) ListRows(ctx context.Context) ([]*IaCState, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT name, type, provider, status, applied_config::text, outputs::text
		FROM iac_resources
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []*IaCState
	for rows.Next() {
		var st IaCState
		var cfgJSON, outJSON string
		if err := rows.Scan(&st.ResourceID, &st.ResourceType, &st.Provider, &st.Status, &cfgJSON, &outJSON); err != nil {
			continue
		}
		_ = json.Unmarshal([]byte(cfgJSON), &st.Config)
		_ = json.Unmarshal([]byte(outJSON), &st.Outputs)
		results = append(results, &st)
	}
	return results, rows.Err()
}

func (c *pgxRealConn) DeleteRow(ctx context.Context, name string) (bool, error) {
	tag, err := c.pool.Exec(ctx, `DELETE FROM iac_resources WHERE name = $1`, name)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (c *pgxRealConn) AcquireAdvisoryLock(ctx context.Context, key int64) error {
	_, err := c.pool.Exec(ctx, `SELECT pg_advisory_lock($1)`, key)
	return err
}

func (c *pgxRealConn) ReleaseAdvisoryLock(ctx context.Context, key int64) (bool, error) {
	var released bool
	err := c.pool.QueryRow(ctx, `SELECT pg_advisory_unlock($1)`, key).Scan(&released)
	return released, err
}

func (c *pgxRealConn) Close() {
	c.pool.Close()
}
