package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// Event type constants
// ---------------------------------------------------------------------------

const (
	EventExecutionStarted   = "execution.started"
	EventStepStarted        = "step.started"
	EventStepInputRecorded  = "step.input_recorded"
	EventStepOutputRecorded = "step.output_recorded"
	EventStepCompleted      = "step.completed"
	EventStepFailed         = "step.failed"
	EventStepSkipped        = "step.skipped"
	EventStepCompensated    = "step.compensated"
	EventConditionalRouted  = "conditional.routed"
	EventRetryAttempted     = "retry.attempted"
	EventExecutionCompleted = "execution.completed"
	EventExecutionFailed    = "execution.failed"
	EventExecutionCancelled = "execution.cancelled"
	EventSagaCompensating   = "saga.compensating"
	EventSagaCompensated    = "saga.compensated"
)

// ---------------------------------------------------------------------------
// Core types
// ---------------------------------------------------------------------------

// ExecutionEvent represents a single immutable event in the execution log.
type ExecutionEvent struct {
	ID          uuid.UUID       `json:"id"`
	ExecutionID uuid.UUID       `json:"execution_id"`
	SequenceNum int64           `json:"sequence_num"`
	EventType   string          `json:"event_type"`
	EventData   json.RawMessage `json:"event_data"`
	CreatedAt   time.Time       `json:"created_at"`
}

// MaterializedStep is a read-optimized view of a single step within an execution.
type MaterializedStep struct {
	StepName    string          `json:"step_name"`
	StepType    string          `json:"step_type,omitempty"`
	Status      string          `json:"status"`
	InputData   json.RawMessage `json:"input_data,omitempty"`
	OutputData  json.RawMessage `json:"output_data,omitempty"`
	Error       string          `json:"error,omitempty"`
	Route       string          `json:"route,omitempty"`
	Retries     int             `json:"retries,omitempty"`
	StartedAt   *time.Time      `json:"started_at,omitempty"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}

// MaterializedExecution is a read-optimized view of a complete execution,
// materialized from the event stream.
type MaterializedExecution struct {
	ExecutionID uuid.UUID          `json:"execution_id"`
	Pipeline    string             `json:"pipeline,omitempty"`
	TenantID    string             `json:"tenant_id,omitempty"`
	Status      string             `json:"status"`
	Steps       []MaterializedStep `json:"steps,omitempty"`
	Error       string             `json:"error,omitempty"`
	StartedAt   *time.Time         `json:"started_at,omitempty"`
	CompletedAt *time.Time         `json:"completed_at,omitempty"`
	EventCount  int                `json:"event_count"`
}

// ExecutionEventFilter specifies criteria for listing materialized executions.
type ExecutionEventFilter struct {
	Pipeline string
	TenantID string
	Status   string
	Since    *time.Time
	Until    *time.Time
	Limit    int
	Offset   int
}

// ---------------------------------------------------------------------------
// EventStore interface
// ---------------------------------------------------------------------------

// EventStore defines persistence operations for execution events using an
// append-only event sourcing pattern.
type EventStore interface {
	// Append adds a new event to the log for a given execution.
	Append(ctx context.Context, executionID uuid.UUID, eventType string, data map[string]any) error
	// GetEvents returns all events for an execution ordered by sequence number.
	GetEvents(ctx context.Context, executionID uuid.UUID) ([]ExecutionEvent, error)
	// GetTimeline materializes a complete execution view from its event stream.
	GetTimeline(ctx context.Context, executionID uuid.UUID) (*MaterializedExecution, error)
	// ListExecutions returns materialized executions matching the given filter.
	ListExecutions(ctx context.Context, filter ExecutionEventFilter) ([]MaterializedExecution, error)
}

// ---------------------------------------------------------------------------
// Materialization helper
// ---------------------------------------------------------------------------

// materialize replays a sequence of events into a MaterializedExecution.
func materialize(events []ExecutionEvent) *MaterializedExecution {
	if len(events) == 0 {
		return nil
	}

	m := &MaterializedExecution{
		ExecutionID: events[0].ExecutionID,
		Status:      "unknown",
		EventCount:  len(events),
	}

	stepIndex := make(map[string]int) // stepName -> index in m.Steps

	for i := range events {
		ev := &events[i]
		var data map[string]any
		if len(ev.EventData) > 0 {
			_ = json.Unmarshal(ev.EventData, &data)
		}
		if data == nil {
			data = map[string]any{}
		}

		switch ev.EventType {
		case EventExecutionStarted:
			m.Status = "running"
			t := ev.CreatedAt
			m.StartedAt = &t
			if v, ok := data["pipeline"].(string); ok {
				m.Pipeline = v
			}
			if v, ok := data["tenant_id"].(string); ok {
				m.TenantID = v
			}

		case EventStepStarted:
			stepName, _ := data["step_name"].(string)
			if stepName == "" {
				continue
			}
			t := ev.CreatedAt
			step := MaterializedStep{
				StepName:  stepName,
				Status:    "running",
				StartedAt: &t,
			}
			if v, ok := data["step_type"].(string); ok {
				step.StepType = v
			}
			stepIndex[stepName] = len(m.Steps)
			m.Steps = append(m.Steps, step)

		case EventStepInputRecorded:
			stepName, _ := data["step_name"].(string)
			if idx, ok := stepIndex[stepName]; ok {
				if inputRaw, err := json.Marshal(data["input"]); err == nil {
					m.Steps[idx].InputData = inputRaw
				}
			}

		case EventStepOutputRecorded:
			stepName, _ := data["step_name"].(string)
			if idx, ok := stepIndex[stepName]; ok {
				if outputRaw, err := json.Marshal(data["output"]); err == nil {
					m.Steps[idx].OutputData = outputRaw
				}
			}

		case EventStepCompleted:
			stepName, _ := data["step_name"].(string)
			if idx, ok := stepIndex[stepName]; ok {
				m.Steps[idx].Status = "completed"
				t := ev.CreatedAt
				m.Steps[idx].CompletedAt = &t
			}

		case EventStepFailed:
			stepName, _ := data["step_name"].(string)
			if idx, ok := stepIndex[stepName]; ok {
				m.Steps[idx].Status = "failed"
				t := ev.CreatedAt
				m.Steps[idx].CompletedAt = &t
				if v, ok := data["error"].(string); ok {
					m.Steps[idx].Error = v
				}
			}

		case EventStepSkipped:
			stepName, _ := data["step_name"].(string)
			if stepName == "" {
				continue
			}
			step := MaterializedStep{
				StepName: stepName,
				Status:   "skipped",
			}
			if v, ok := data["reason"].(string); ok {
				step.Error = v
			}
			stepIndex[stepName] = len(m.Steps)
			m.Steps = append(m.Steps, step)

		case EventStepCompensated:
			stepName, _ := data["step_name"].(string)
			if idx, ok := stepIndex[stepName]; ok {
				m.Steps[idx].Status = "compensated"
			}

		case EventConditionalRouted:
			stepName, _ := data["step_name"].(string)
			if idx, ok := stepIndex[stepName]; ok {
				if v, ok := data["route"].(string); ok {
					m.Steps[idx].Route = v
				}
			}

		case EventRetryAttempted:
			stepName, _ := data["step_name"].(string)
			if idx, ok := stepIndex[stepName]; ok {
				m.Steps[idx].Retries++
				m.Steps[idx].Status = "running"
			}

		case EventExecutionCompleted:
			m.Status = "completed"
			t := ev.CreatedAt
			m.CompletedAt = &t

		case EventExecutionFailed:
			m.Status = "failed"
			t := ev.CreatedAt
			m.CompletedAt = &t
			if v, ok := data["error"].(string); ok {
				m.Error = v
			}

		case EventExecutionCancelled:
			m.Status = "cancelled"
			t := ev.CreatedAt
			m.CompletedAt = &t

		case EventSagaCompensating:
			m.Status = "compensating"

		case EventSagaCompensated:
			m.Status = "compensated"
			t := ev.CreatedAt
			m.CompletedAt = &t
		}
	}

	return m
}

// ===========================================================================
// InMemoryEventStore
// ===========================================================================

// InMemoryEventStore is a thread-safe in-memory implementation of EventStore.
// Suitable for testing and single-server use.
type InMemoryEventStore struct {
	mu     sync.RWMutex
	events map[uuid.UUID][]ExecutionEvent // executionID -> events
	seqs   map[uuid.UUID]int64            // executionID -> last sequence number
}

// NewInMemoryEventStore creates a new InMemoryEventStore.
func NewInMemoryEventStore() *InMemoryEventStore {
	return &InMemoryEventStore{
		events: make(map[uuid.UUID][]ExecutionEvent),
		seqs:   make(map[uuid.UUID]int64),
	}
}

func (s *InMemoryEventStore) Append(_ context.Context, executionID uuid.UUID, eventType string, data map[string]any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal event data: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.seqs[executionID]++
	seq := s.seqs[executionID]

	ev := ExecutionEvent{
		ID:          uuid.New(),
		ExecutionID: executionID,
		SequenceNum: seq,
		EventType:   eventType,
		EventData:   raw,
		CreatedAt:   time.Now(),
	}

	s.events[executionID] = append(s.events[executionID], ev)
	return nil
}

func (s *InMemoryEventStore) GetEvents(_ context.Context, executionID uuid.UUID) ([]ExecutionEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events, ok := s.events[executionID]
	if !ok {
		return nil, nil
	}

	// Return a copy to avoid data races.
	result := make([]ExecutionEvent, len(events))
	copy(result, events)
	return result, nil
}

func (s *InMemoryEventStore) GetTimeline(_ context.Context, executionID uuid.UUID) (*MaterializedExecution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events, ok := s.events[executionID]
	if !ok {
		return nil, ErrNotFound
	}

	// Work on a copy.
	cp := make([]ExecutionEvent, len(events))
	copy(cp, events)

	m := materialize(cp)
	if m == nil {
		return nil, ErrNotFound
	}
	return m, nil
}

func (s *InMemoryEventStore) ListExecutions(_ context.Context, filter ExecutionEventFilter) ([]MaterializedExecution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []MaterializedExecution

	for _, events := range s.events {
		if len(events) == 0 {
			continue
		}
		cp := make([]ExecutionEvent, len(events))
		copy(cp, events)
		m := materialize(cp)
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

	// Sort by started time descending (most recent first).
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

// ===========================================================================
// SQLiteEventStore
// ===========================================================================

// SQLiteEventStore implements EventStore backed by SQLite using database/sql.
// Writes are serialized with a mutex to avoid SQLITE_BUSY errors under
// concurrent load, which is the standard approach for SQLite.
type SQLiteEventStore struct {
	mu sync.Mutex // serializes writes
	db *sql.DB
}

// NewSQLiteEventStore creates a new SQLiteEventStore using the given database path.
// It opens the database and creates the required table if it does not exist.
func NewSQLiteEventStore(dbPath string) (*SQLiteEventStore, error) {
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(5)

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	s := &SQLiteEventStore{db: db}
	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// NewSQLiteEventStoreFromDB wraps an existing *sql.DB connection.
// It creates the required table if it does not exist.
func NewSQLiteEventStoreFromDB(db *sql.DB) (*SQLiteEventStore, error) {
	s := &SQLiteEventStore{db: db}
	if err := s.init(); err != nil {
		return nil, err
	}
	return s, nil
}

// init creates the execution_events table and indexes.
func (s *SQLiteEventStore) init() error {
	schema := `
	CREATE TABLE IF NOT EXISTS execution_events (
		id            TEXT PRIMARY KEY,
		execution_id  TEXT NOT NULL,
		sequence_num  INTEGER NOT NULL,
		event_type    TEXT NOT NULL,
		event_data    TEXT,
		created_at    TEXT NOT NULL,
		UNIQUE(execution_id, sequence_num)
	);
	CREATE INDEX IF NOT EXISTS idx_execution_events_execution_id ON execution_events(execution_id);
	CREATE INDEX IF NOT EXISTS idx_execution_events_event_type ON execution_events(event_type);
	CREATE INDEX IF NOT EXISTS idx_execution_events_created_at ON execution_events(created_at);
	`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("create execution_events table: %w", err)
	}
	return nil
}

// Close closes the underlying database connection.
func (s *SQLiteEventStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteEventStore) Append(ctx context.Context, executionID uuid.UUID, eventType string, data map[string]any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal event data: %w", err)
	}

	// Serialize writes to avoid SQLITE_BUSY under concurrent load.
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Get next sequence number for this execution.
	var maxSeq sql.NullInt64
	err = tx.QueryRowContext(ctx,
		`SELECT MAX(sequence_num) FROM execution_events WHERE execution_id = ?`,
		executionID.String(),
	).Scan(&maxSeq)
	if err != nil {
		return fmt.Errorf("get max sequence: %w", err)
	}

	seq := int64(1)
	if maxSeq.Valid {
		seq = maxSeq.Int64 + 1
	}

	now := time.Now().UTC()
	id := uuid.New()

	_, err = tx.ExecContext(ctx,
		`INSERT INTO execution_events (id, execution_id, sequence_num, event_type, event_data, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id.String(), executionID.String(), seq, eventType, string(raw), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	return tx.Commit()
}

func (s *SQLiteEventStore) GetEvents(ctx context.Context, executionID uuid.UUID) ([]ExecutionEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, execution_id, sequence_num, event_type, event_data, created_at
		 FROM execution_events
		 WHERE execution_id = ?
		 ORDER BY sequence_num ASC`,
		executionID.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []ExecutionEvent
	for rows.Next() {
		var ev ExecutionEvent
		var idStr, execIDStr, dataStr, createdStr string

		if err := rows.Scan(&idStr, &execIDStr, &ev.SequenceNum, &ev.EventType, &dataStr, &createdStr); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}

		ev.ID, _ = uuid.Parse(idStr)
		ev.ExecutionID, _ = uuid.Parse(execIDStr)
		ev.EventData = json.RawMessage(dataStr)
		ev.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)

		events = append(events, ev)
	}
	return events, rows.Err()
}

func (s *SQLiteEventStore) GetTimeline(ctx context.Context, executionID uuid.UUID) (*MaterializedExecution, error) {
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

func (s *SQLiteEventStore) ListExecutions(ctx context.Context, filter ExecutionEventFilter) ([]MaterializedExecution, error) {
	// Get distinct execution IDs.
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT execution_id FROM execution_events ORDER BY execution_id`)
	if err != nil {
		return nil, fmt.Errorf("query execution IDs: %w", err)
	}
	defer rows.Close()

	var execIDs []uuid.UUID
	for rows.Next() {
		var idStr string
		if err := rows.Scan(&idStr); err != nil {
			return nil, fmt.Errorf("scan execution ID: %w", err)
		}
		id, _ := uuid.Parse(idStr)
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

// ---------------------------------------------------------------------------
// Compile-time interface assertions
// ---------------------------------------------------------------------------

var (
	_ EventStore = (*InMemoryEventStore)(nil)
	_ EventStore = (*SQLiteEventStore)(nil)
)
