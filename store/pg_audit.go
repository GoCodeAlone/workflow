package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGAuditStore implements AuditStore backed by PostgreSQL.
type PGAuditStore struct {
	pool *pgxpool.Pool
}

func (s *PGAuditStore) Record(ctx context.Context, e *AuditEntry) error {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO audit_log (user_id, action, resource_type, resource_id, details, ip_address, user_agent, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())
		RETURNING id, created_at`,
		e.UserID, e.Action, e.ResourceType, e.ResourceID, e.Details, e.IPAddress, e.UserAgent).Scan(&e.ID, &e.CreatedAt)
	if err != nil {
		return fmt.Errorf("record audit: %w", err)
	}
	return nil
}

func (s *PGAuditStore) Query(ctx context.Context, f AuditFilter) ([]*AuditEntry, error) {
	query := `SELECT id, user_id, action, resource_type, resource_id, details, ip_address, user_agent, created_at
		FROM audit_log WHERE 1=1`
	args := []interface{}{}
	idx := 1

	if f.UserID != nil {
		query += fmt.Sprintf(` AND user_id = $%d`, idx)
		args = append(args, *f.UserID)
		idx++
	}
	if f.Action != "" {
		query += fmt.Sprintf(` AND action = $%d`, idx)
		args = append(args, f.Action)
		idx++
	}
	if f.ResourceType != "" {
		query += fmt.Sprintf(` AND resource_type = $%d`, idx)
		args = append(args, f.ResourceType)
		idx++
	}
	if f.ResourceID != nil {
		query += fmt.Sprintf(` AND resource_id = $%d`, idx)
		args = append(args, *f.ResourceID)
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
		return nil, fmt.Errorf("query audit: %w", err)
	}
	defer rows.Close()

	var entries []*AuditEntry
	for rows.Next() {
		var e AuditEntry
		err := rows.Scan(&e.ID, &e.UserID, &e.Action, &e.ResourceType, &e.ResourceID,
			&e.Details, &e.IPAddress, &e.UserAgent, &e.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan audit: %w", err)
		}
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}
