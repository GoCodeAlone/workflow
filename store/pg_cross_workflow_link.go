package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGCrossWorkflowLinkStore implements CrossWorkflowLinkStore backed by PostgreSQL.
type PGCrossWorkflowLinkStore struct {
	pool *pgxpool.Pool
}

func (s *PGCrossWorkflowLinkStore) Create(ctx context.Context, l *CrossWorkflowLink) error {
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO cross_workflow_links (id, source_workflow_id, target_workflow_id,
			link_type, config, created_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,NOW())`,
		l.ID, l.SourceWorkflowID, l.TargetWorkflowID,
		l.LinkType, l.Config, l.CreatedBy)
	if err != nil {
		if isDuplicateError(err) {
			return fmt.Errorf("%w: cross-workflow link already exists", ErrDuplicate)
		}
		return fmt.Errorf("insert cross-workflow link: %w", err)
	}
	return nil
}

func (s *PGCrossWorkflowLinkStore) Get(ctx context.Context, id uuid.UUID) (*CrossWorkflowLink, error) {
	rows, err := s.pool.Query(ctx, `SELECT * FROM cross_workflow_links WHERE id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("query cross-workflow link: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("query cross-workflow link: %w", err)
		}
		return nil, ErrNotFound
	}
	return scanCrossWorkflowLink(rows)
}

func (s *PGCrossWorkflowLinkStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM cross_workflow_links WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete cross-workflow link: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGCrossWorkflowLinkStore) List(ctx context.Context, f CrossWorkflowLinkFilter) ([]*CrossWorkflowLink, error) {
	query := `SELECT * FROM cross_workflow_links WHERE 1=1`
	args := []interface{}{}
	idx := 1

	if f.SourceWorkflowID != nil {
		query += fmt.Sprintf(` AND source_workflow_id = $%d`, idx)
		args = append(args, *f.SourceWorkflowID)
		idx++
	}
	if f.TargetWorkflowID != nil {
		query += fmt.Sprintf(` AND target_workflow_id = $%d`, idx)
		args = append(args, *f.TargetWorkflowID)
		idx++
	}
	if f.LinkType != "" {
		query += fmt.Sprintf(` AND link_type = $%d`, idx)
		args = append(args, f.LinkType)
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
		return nil, fmt.Errorf("list cross-workflow links: %w", err)
	}
	defer rows.Close()

	var links []*CrossWorkflowLink
	for rows.Next() {
		l, err := scanCrossWorkflowLink(rows)
		if err != nil {
			return nil, err
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

func scanCrossWorkflowLink(rows pgx.Rows) (*CrossWorkflowLink, error) {
	var l CrossWorkflowLink
	err := rows.Scan(&l.ID, &l.SourceWorkflowID, &l.TargetWorkflowID,
		&l.LinkType, &l.Config, &l.CreatedBy, &l.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("scan cross-workflow link: %w", err)
	}
	return &l, nil
}
