package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGDLQStore implements DLQStore backed by PostgreSQL using pgxpool.
type PGDLQStore struct {
	pool *pgxpool.Pool
}

// NewPGDLQStore creates a new PGDLQStore backed by the given connection pool
// and ensures the required schema exists.
func NewPGDLQStore(pool *pgxpool.Pool) (*PGDLQStore, error) {
	s := &PGDLQStore{pool: pool}
	if err := s.init(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *PGDLQStore) init(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS dlq_entries (
			id              UUID        PRIMARY KEY,
			original_event  JSONB,
			pipeline_name   TEXT        NOT NULL,
			step_name       TEXT        NOT NULL,
			error_message   TEXT        NOT NULL,
			error_type      TEXT        NOT NULL,
			retry_count     INTEGER     NOT NULL DEFAULT 0,
			max_retries     INTEGER     NOT NULL DEFAULT 0,
			status          TEXT        NOT NULL DEFAULT 'pending',
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			resolved_at     TIMESTAMPTZ,
			metadata        JSONB
		);
		CREATE INDEX IF NOT EXISTS idx_dlq_entries_pipeline_name ON dlq_entries(pipeline_name);
		CREATE INDEX IF NOT EXISTS idx_dlq_entries_step_name     ON dlq_entries(step_name);
		CREATE INDEX IF NOT EXISTS idx_dlq_entries_status        ON dlq_entries(status);
		CREATE INDEX IF NOT EXISTS idx_dlq_entries_error_type    ON dlq_entries(error_type);
		CREATE INDEX IF NOT EXISTS idx_dlq_entries_created_at    ON dlq_entries(created_at);
		CREATE INDEX IF NOT EXISTS idx_dlq_entries_updated_at    ON dlq_entries(updated_at);
	`)
	if err != nil {
		return fmt.Errorf("create dlq_entries table: %w", err)
	}
	return nil
}

func (s *PGDLQStore) Add(ctx context.Context, entry *DLQEntry) error {
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

	var originalEvent []byte
	if entry.OriginalEvent != nil {
		originalEvent = []byte(entry.OriginalEvent)
	}

	_, err := s.pool.Exec(ctx,
		`INSERT INTO dlq_entries (id, original_event, pipeline_name, step_name, error_message, error_type,
			retry_count, max_retries, status, created_at, updated_at, resolved_at, metadata)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		entry.ID,
		originalEvent,
		entry.PipelineName,
		entry.StepName,
		entry.ErrorMessage,
		entry.ErrorType,
		entry.RetryCount,
		entry.MaxRetries,
		string(entry.Status),
		entry.CreatedAt,
		entry.UpdatedAt,
		entry.ResolvedAt,
		metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("insert dlq entry: %w", err)
	}
	return nil
}

func (s *PGDLQStore) Get(ctx context.Context, id uuid.UUID) (*DLQEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, original_event, pipeline_name, step_name, error_message, error_type,
			retry_count, max_retries, status, created_at, updated_at, resolved_at, metadata
		 FROM dlq_entries WHERE id = $1`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("query dlq entry: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("query dlq entry: %w", err)
		}
		return nil, ErrNotFound
	}
	return scanPGDLQEntry(rows)
}

func (s *PGDLQStore) List(ctx context.Context, filter DLQFilter) ([]*DLQEntry, error) {
	query, args := buildPGDLQQuery(
		`SELECT id, original_event, pipeline_name, step_name, error_message, error_type,
			retry_count, max_retries, status, created_at, updated_at, resolved_at, metadata
		 FROM dlq_entries`,
		filter,
	)
	query += " ORDER BY created_at DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", len(args)+1)
		args = append(args, filter.Limit)
		if filter.Offset > 0 {
			query += fmt.Sprintf(" OFFSET $%d", len(args)+1)
			args = append(args, filter.Offset)
		}
	} else if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", len(args)+1)
		args = append(args, filter.Offset)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query dlq entries: %w", err)
	}
	defer rows.Close()

	var results []*DLQEntry
	for rows.Next() {
		entry, err := scanPGDLQEntry(rows)
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

func (s *PGDLQStore) Count(ctx context.Context, filter DLQFilter) (int64, error) {
	query, args := buildPGDLQQuery(`SELECT COUNT(*) FROM dlq_entries`, filter)

	var count int64
	err := s.pool.QueryRow(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count dlq entries: %w", err)
	}
	return count, nil
}

func (s *PGDLQStore) UpdateStatus(ctx context.Context, id uuid.UUID, status DLQStatus) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE dlq_entries SET status = $2, updated_at = NOW() WHERE id = $1`,
		id, string(status),
	)
	if err != nil {
		return fmt.Errorf("update dlq status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGDLQStore) Retry(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE dlq_entries SET retry_count = retry_count + 1, status = $2, updated_at = NOW() WHERE id = $1`,
		id, string(DLQStatusRetrying),
	)
	if err != nil {
		return fmt.Errorf("retry dlq entry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGDLQStore) Discard(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE dlq_entries SET status = $2, updated_at = NOW() WHERE id = $1`,
		id, string(DLQStatusDiscarded),
	)
	if err != nil {
		return fmt.Errorf("discard dlq entry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGDLQStore) Resolve(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE dlq_entries SET status = $2, resolved_at = NOW(), updated_at = NOW() WHERE id = $1`,
		id, string(DLQStatusResolved),
	)
	if err != nil {
		return fmt.Errorf("resolve dlq entry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGDLQStore) Purge(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM dlq_entries WHERE status IN ($1, $2) AND updated_at < $3`,
		string(DLQStatusResolved), string(DLQStatusDiscarded), cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("purge dlq entries: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ---------------------------------------------------------------------------
// PostgreSQL helpers
// ---------------------------------------------------------------------------

// buildPGDLQQuery constructs a WHERE clause using PostgreSQL $N placeholders.
func buildPGDLQQuery(base string, filter DLQFilter) (string, []any) {
	var conditions []string
	var args []any
	idx := 1

	if filter.PipelineName != "" {
		conditions = append(conditions, fmt.Sprintf("pipeline_name = $%d", idx))
		args = append(args, filter.PipelineName)
		idx++
	}
	if filter.StepName != "" {
		conditions = append(conditions, fmt.Sprintf("step_name = $%d", idx))
		args = append(args, filter.StepName)
		idx++
	}
	if filter.Status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", idx))
		args = append(args, string(filter.Status))
		idx++
	}
	if filter.ErrorType != "" {
		conditions = append(conditions, fmt.Sprintf("error_type = $%d", idx))
		args = append(args, filter.ErrorType)
	}

	query := base
	if len(conditions) > 0 {
		query += " WHERE "
		for i, c := range conditions {
			if i > 0 {
				query += " AND "
			}
			query += c
		}
	}
	return query, args
}

// scanPGDLQEntry scans a DLQ entry from pgx.Rows.
func scanPGDLQEntry(rows pgx.Rows) (*DLQEntry, error) {
	var entry DLQEntry
	var statusStr string
	var originalEvent, metadataJSON []byte

	err := rows.Scan(
		&entry.ID, &originalEvent,
		&entry.PipelineName, &entry.StepName,
		&entry.ErrorMessage, &entry.ErrorType,
		&entry.RetryCount, &entry.MaxRetries,
		&statusStr, &entry.CreatedAt, &entry.UpdatedAt,
		&entry.ResolvedAt, &metadataJSON,
	)
	if err != nil {
		return nil, fmt.Errorf("scan dlq entry: %w", err)
	}

	entry.Status = DLQStatus(statusStr)
	if originalEvent != nil {
		entry.OriginalEvent = json.RawMessage(originalEvent)
	}
	if metadataJSON != nil {
		_ = json.Unmarshal(metadataJSON, &entry.Metadata)
	}

	return &entry, nil
}

// ---------------------------------------------------------------------------
// Compile-time interface assertion
// ---------------------------------------------------------------------------

var _ DLQStore = (*PGDLQStore)(nil)
