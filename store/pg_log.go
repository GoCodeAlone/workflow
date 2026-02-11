package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGLogStore implements LogStore backed by PostgreSQL.
type PGLogStore struct {
	pool *pgxpool.Pool
}

func (s *PGLogStore) Append(ctx context.Context, l *ExecutionLog) error {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO execution_logs (workflow_id, execution_id, level, message, module_name, fields, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,NOW())
		RETURNING id, created_at`,
		l.WorkflowID, l.ExecutionID, l.Level, l.Message, l.ModuleName, l.Fields).Scan(&l.ID, &l.CreatedAt)
	if err != nil {
		return fmt.Errorf("append log: %w", err)
	}
	return nil
}

func (s *PGLogStore) Query(ctx context.Context, f LogFilter) ([]*ExecutionLog, error) {
	query := `SELECT id, workflow_id, execution_id, level, message, module_name, fields, created_at
		FROM execution_logs WHERE 1=1`
	args := []interface{}{}
	idx := 1

	if f.WorkflowID != nil {
		query += fmt.Sprintf(` AND workflow_id = $%d`, idx)
		args = append(args, *f.WorkflowID)
		idx++
	}
	if f.ExecutionID != nil {
		query += fmt.Sprintf(` AND execution_id = $%d`, idx)
		args = append(args, *f.ExecutionID)
		idx++
	}
	if f.Level != "" {
		query += fmt.Sprintf(` AND level = $%d`, idx)
		args = append(args, f.Level)
		idx++
	}
	if f.ModuleName != "" {
		query += fmt.Sprintf(` AND module_name = $%d`, idx)
		args = append(args, f.ModuleName)
		idx++
	}
	if f.Since != nil {
		query += fmt.Sprintf(` AND created_at >= $%d`, idx)
		args = append(args, *f.Since)
		idx++
	}
	if f.Until != nil {
		query += fmt.Sprintf(` AND created_at <= $%d`, idx)
		args = append(args, *f.Until)
		idx++
	}

	query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, idx, idx+1)
	limit := f.Pagination.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, f.Pagination.Offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query logs: %w", err)
	}
	defer rows.Close()

	var logs []*ExecutionLog
	for rows.Next() {
		var l ExecutionLog
		err := rows.Scan(&l.ID, &l.WorkflowID, &l.ExecutionID, &l.Level,
			&l.Message, &l.ModuleName, &l.Fields, &l.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan log: %w", err)
		}
		logs = append(logs, &l)
	}
	return logs, rows.Err()
}

func (s *PGLogStore) CountByLevel(ctx context.Context, workflowID uuid.UUID) (map[LogLevel]int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT level, COUNT(*) FROM execution_logs
		WHERE workflow_id = $1 GROUP BY level`, workflowID)
	if err != nil {
		return nil, fmt.Errorf("count by level: %w", err)
	}
	defer rows.Close()

	result := make(map[LogLevel]int)
	for rows.Next() {
		var level LogLevel
		var count int
		if err := rows.Scan(&level, &count); err != nil {
			return nil, fmt.Errorf("scan count: %w", err)
		}
		result[level] = count
	}
	return result, rows.Err()
}
