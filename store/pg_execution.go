package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGExecutionStore implements ExecutionStore backed by PostgreSQL.
type PGExecutionStore struct {
	pool *pgxpool.Pool
}

func (s *PGExecutionStore) CreateExecution(ctx context.Context, e *WorkflowExecution) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.Status == "" {
		e.Status = ExecutionStatusPending
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO workflow_executions (id, workflow_id, trigger_type, trigger_data, status,
			output_data, error_message, error_stack, started_at, completed_at, duration_ms, metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		e.ID, e.WorkflowID, e.TriggerType, e.TriggerData, e.Status,
		e.OutputData, e.ErrorMessage, e.ErrorStack, e.StartedAt, e.CompletedAt, e.DurationMs, e.Metadata)
	if err != nil {
		return fmt.Errorf("insert execution: %w", err)
	}
	return nil
}

func (s *PGExecutionStore) GetExecution(ctx context.Context, id uuid.UUID) (*WorkflowExecution, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, workflow_id, trigger_type, trigger_data, status,
			output_data, error_message, error_stack, started_at, completed_at, duration_ms, metadata
		FROM workflow_executions WHERE id = $1`, id)

	var e WorkflowExecution
	err := row.Scan(&e.ID, &e.WorkflowID, &e.TriggerType, &e.TriggerData, &e.Status,
		&e.OutputData, &e.ErrorMessage, &e.ErrorStack, &e.StartedAt, &e.CompletedAt, &e.DurationMs, &e.Metadata)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get execution: %w", err)
	}
	return &e, nil
}

func (s *PGExecutionStore) UpdateExecution(ctx context.Context, e *WorkflowExecution) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE workflow_executions SET status=$2, output_data=$3, error_message=$4,
			error_stack=$5, completed_at=$6, duration_ms=$7, metadata=$8
		WHERE id=$1`,
		e.ID, e.Status, e.OutputData, e.ErrorMessage,
		e.ErrorStack, e.CompletedAt, e.DurationMs, e.Metadata)
	if err != nil {
		return fmt.Errorf("update execution: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGExecutionStore) ListExecutions(ctx context.Context, f ExecutionFilter) ([]*WorkflowExecution, error) {
	query := `SELECT id, workflow_id, trigger_type, trigger_data, status,
		output_data, error_message, error_stack, started_at, completed_at, duration_ms, metadata
		FROM workflow_executions WHERE 1=1`
	args := []interface{}{}
	idx := 1

	if f.WorkflowID != nil {
		query += fmt.Sprintf(` AND workflow_id = $%d`, idx)
		args = append(args, *f.WorkflowID)
		idx++
	}
	if f.Status != "" {
		query += fmt.Sprintf(` AND status = $%d`, idx)
		args = append(args, f.Status)
		idx++
	}
	if f.Since != nil {
		query += fmt.Sprintf(` AND started_at >= $%d`, idx)
		args = append(args, *f.Since)
		idx++
	}
	if f.Until != nil {
		query += fmt.Sprintf(` AND started_at <= $%d`, idx)
		args = append(args, *f.Until)
		idx++
	}

	query += fmt.Sprintf(` ORDER BY started_at DESC LIMIT $%d OFFSET $%d`, idx, idx+1)
	limit := f.Pagination.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, f.Pagination.Offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list executions: %w", err)
	}
	defer rows.Close()

	var executions []*WorkflowExecution
	for rows.Next() {
		var e WorkflowExecution
		err := rows.Scan(&e.ID, &e.WorkflowID, &e.TriggerType, &e.TriggerData, &e.Status,
			&e.OutputData, &e.ErrorMessage, &e.ErrorStack, &e.StartedAt, &e.CompletedAt, &e.DurationMs, &e.Metadata)
		if err != nil {
			return nil, fmt.Errorf("scan execution: %w", err)
		}
		executions = append(executions, &e)
	}
	return executions, rows.Err()
}

func (s *PGExecutionStore) CreateStep(ctx context.Context, step *ExecutionStep) error {
	if step.ID == uuid.Nil {
		step.ID = uuid.New()
	}
	if step.Status == "" {
		step.Status = StepStatusPending
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO execution_steps (id, execution_id, step_name, step_type, input_data,
			output_data, status, error_message, started_at, completed_at, duration_ms, sequence_num, metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		step.ID, step.ExecutionID, step.StepName, step.StepType, step.InputData,
		step.OutputData, step.Status, step.ErrorMessage, step.StartedAt, step.CompletedAt,
		step.DurationMs, step.SequenceNum, step.Metadata)
	if err != nil {
		return fmt.Errorf("insert step: %w", err)
	}
	return nil
}

func (s *PGExecutionStore) UpdateStep(ctx context.Context, step *ExecutionStep) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE execution_steps SET status=$2, output_data=$3, error_message=$4,
			completed_at=$5, duration_ms=$6, metadata=$7
		WHERE id=$1`,
		step.ID, step.Status, step.OutputData, step.ErrorMessage,
		step.CompletedAt, step.DurationMs, step.Metadata)
	if err != nil {
		return fmt.Errorf("update step: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGExecutionStore) ListSteps(ctx context.Context, executionID uuid.UUID) ([]*ExecutionStep, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, execution_id, step_name, step_type, input_data, output_data, status,
			error_message, started_at, completed_at, duration_ms, sequence_num, metadata
		FROM execution_steps WHERE execution_id = $1
		ORDER BY sequence_num ASC`, executionID)
	if err != nil {
		return nil, fmt.Errorf("list steps: %w", err)
	}
	defer rows.Close()

	var steps []*ExecutionStep
	for rows.Next() {
		var step ExecutionStep
		err := rows.Scan(&step.ID, &step.ExecutionID, &step.StepName, &step.StepType,
			&step.InputData, &step.OutputData, &step.Status, &step.ErrorMessage,
			&step.StartedAt, &step.CompletedAt, &step.DurationMs, &step.SequenceNum, &step.Metadata)
		if err != nil {
			return nil, fmt.Errorf("scan step: %w", err)
		}
		steps = append(steps, &step)
	}
	return steps, rows.Err()
}

func (s *PGExecutionStore) CountByStatus(ctx context.Context, workflowID uuid.UUID) (map[ExecutionStatus]int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT status, COUNT(*) FROM workflow_executions
		WHERE workflow_id = $1 GROUP BY status`, workflowID)
	if err != nil {
		return nil, fmt.Errorf("count by status: %w", err)
	}
	defer rows.Close()

	result := make(map[ExecutionStatus]int)
	for rows.Next() {
		var status ExecutionStatus
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scan count: %w", err)
		}
		result[status] = count
	}
	return result, rows.Err()
}
