package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGEventStore implements EventStore backed by PostgreSQL using pgxpool.
type PGEventStore struct {
	pool *pgxpool.Pool
}

// NewPGEventStore creates a new PGEventStore backed by the given connection pool
// and ensures the required schema exists.
func NewPGEventStore(pool *pgxpool.Pool) (*PGEventStore, error) {
	s := &PGEventStore{pool: pool}
	if err := s.init(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

// init creates the execution_events table and indexes.
func (s *PGEventStore) init(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS execution_events (
			id            UUID        PRIMARY KEY,
			execution_id  UUID        NOT NULL,
			sequence_num  BIGINT      NOT NULL,
			event_type    TEXT        NOT NULL,
			event_data    JSONB,
			created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(execution_id, sequence_num)
		);
		CREATE INDEX IF NOT EXISTS idx_execution_events_execution_id ON execution_events(execution_id);
		CREATE INDEX IF NOT EXISTS idx_execution_events_event_type   ON execution_events(event_type);
		CREATE INDEX IF NOT EXISTS idx_execution_events_created_at   ON execution_events(created_at);
	`)
	if err != nil {
		return fmt.Errorf("create execution_events table: %w", err)
	}
	return nil
}

func (s *PGEventStore) Append(ctx context.Context, executionID uuid.UUID, eventType string, data map[string]any) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Get next sequence number for this execution.
	var maxSeq *int64
	err = tx.QueryRow(ctx,
		`SELECT MAX(sequence_num) FROM execution_events WHERE execution_id = $1`,
		executionID,
	).Scan(&maxSeq)
	if err != nil {
		return fmt.Errorf("get max sequence: %w", err)
	}

	seq := int64(1)
	if maxSeq != nil {
		seq = *maxSeq + 1
	}

	id := uuid.New()
	now := time.Now().UTC()

	_, err = tx.Exec(ctx,
		`INSERT INTO execution_events (id, execution_id, sequence_num, event_type, event_data, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		id, executionID, seq, eventType, data, now,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *PGEventStore) GetEvents(ctx context.Context, executionID uuid.UUID) ([]ExecutionEvent, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, execution_id, sequence_num, event_type, event_data, created_at
		 FROM execution_events
		 WHERE execution_id = $1
		 ORDER BY sequence_num ASC`,
		executionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []ExecutionEvent
	for rows.Next() {
		var ev ExecutionEvent
		var data []byte
		if err := rows.Scan(&ev.ID, &ev.ExecutionID, &ev.SequenceNum, &ev.EventType, &data, &ev.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		if data != nil {
			ev.EventData = data
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}

func (s *PGEventStore) GetTimeline(ctx context.Context, executionID uuid.UUID) (*MaterializedExecution, error) {
	events, err := s.GetEvents(ctx, executionID)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, ErrNotFound
	}
	m := materialize(events)
	if m == nil {
		return nil, ErrNotFound
	}
	return m, nil
}

func (s *PGEventStore) ListExecutions(ctx context.Context, filter ExecutionEventFilter) ([]MaterializedExecution, error) {
	// Get distinct execution IDs.
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT execution_id FROM execution_events ORDER BY execution_id`)
	if err != nil {
		return nil, fmt.Errorf("query execution IDs: %w", err)
	}
	defer rows.Close()

	var execIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan execution ID: %w", err)
		}
		execIDs = append(execIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var results []MaterializedExecution
	for _, eid := range execIDs {
		events, err := s.GetEvents(ctx, eid)
		if err != nil {
			return nil, err
		}
		m := materialize(events)
		if m == nil {
			continue
		}

		// Apply filters.
		if filter.Pipeline != "" && m.Pipeline != filter.Pipeline {
			continue
		}
		if filter.TenantID != "" && m.TenantID != filter.TenantID {
			continue
		}
		if filter.Status != "" && m.Status != filter.Status {
			continue
		}
		if filter.Since != nil && (m.StartedAt == nil || m.StartedAt.Before(*filter.Since)) {
			continue
		}
		if filter.Until != nil && (m.StartedAt == nil || m.StartedAt.After(*filter.Until)) {
			continue
		}

		results = append(results, *m)
	}

	// Sort by started time descending.
	sortExecutions(results)

	// Apply offset/limit.
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

// sortExecutions sorts MaterializedExecution slice by StartedAt descending.
func sortExecutions(results []MaterializedExecution) {
	sort.Slice(results, func(i, j int) bool {
		ti := results[i].StartedAt
		tj := results[j].StartedAt
		if ti == nil && tj == nil {
			return false
		}
		if ti == nil {
			return false
		}
		if tj == nil {
			return true
		}
		return ti.After(*tj)
	})
}

// ---------------------------------------------------------------------------
// Compile-time interface assertion
// ---------------------------------------------------------------------------

var _ EventStore = (*PGEventStore)(nil)
