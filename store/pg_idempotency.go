package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGIdempotencyStore implements IdempotencyStore backed by PostgreSQL using pgxpool.
type PGIdempotencyStore struct {
	pool *pgxpool.Pool
}

// NewPGIdempotencyStore creates a new PGIdempotencyStore backed by the given
// connection pool and ensures the required schema exists.
func NewPGIdempotencyStore(pool *pgxpool.Pool) (*PGIdempotencyStore, error) {
	s := &PGIdempotencyStore{pool: pool}
	if err := s.createTable(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *PGIdempotencyStore) createTable(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS idempotency_keys (
			key          TEXT        PRIMARY KEY,
			execution_id UUID        NOT NULL,
			step_name    TEXT        NOT NULL,
			result       JSONB,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			expires_at   TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_idempotency_expires ON idempotency_keys(expires_at);
	`)
	if err != nil {
		return fmt.Errorf("create idempotency_keys table: %w", err)
	}
	return nil
}

func (s *PGIdempotencyStore) Check(ctx context.Context, key string) (*IdempotencyRecord, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT key, execution_id, step_name, result, created_at, expires_at
		 FROM idempotency_keys
		 WHERE key = $1 AND expires_at > NOW()`,
		key,
	)
	if err != nil {
		return nil, fmt.Errorf("query idempotency key: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("query idempotency key: %w", err)
		}
		return nil, nil // key not found or expired
	}

	var rec IdempotencyRecord
	var resultJSON []byte
	err = rows.Scan(&rec.Key, &rec.ExecutionID, &rec.StepName, &resultJSON, &rec.CreatedAt, &rec.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("scan idempotency key: %w", err)
	}
	if resultJSON != nil {
		rec.Result = json.RawMessage(resultJSON)
	}
	return &rec, nil
}

func (s *PGIdempotencyStore) Store(ctx context.Context, record *IdempotencyRecord) error {
	if record.Key == "" {
		return fmt.Errorf("idempotency key is required")
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}

	var resultJSON []byte
	if record.Result != nil {
		resultJSON = []byte(record.Result)
	}

	_, err := s.pool.Exec(ctx,
		`INSERT INTO idempotency_keys (key, execution_id, step_name, result, created_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		record.Key,
		record.ExecutionID,
		record.StepName,
		resultJSON,
		record.CreatedAt,
		record.ExpiresAt,
	)
	if err != nil {
		if isDuplicateError(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("insert idempotency key: %w", err)
	}
	return nil
}

func (s *PGIdempotencyStore) Cleanup(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM idempotency_keys WHERE expires_at <= NOW()`,
	)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired keys: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ---------------------------------------------------------------------------
// Compile-time interface assertion
// ---------------------------------------------------------------------------

var _ IdempotencyStore = (*PGIdempotencyStore)(nil)
