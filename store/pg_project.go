package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGProjectStore implements ProjectStore backed by PostgreSQL.
type PGProjectStore struct {
	pool *pgxpool.Pool
}

func (s *PGProjectStore) Create(ctx context.Context, p *Project) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO projects (id, company_id, name, slug, description, metadata, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,NOW(),NOW())`,
		p.ID, p.CompanyID, p.Name, p.Slug, p.Description, p.Metadata)
	if err != nil {
		if isDuplicateError(err) {
			return fmt.Errorf("%w: project slug %s in company", ErrDuplicate, p.Slug)
		}
		return fmt.Errorf("insert project: %w", err)
	}
	return nil
}

func (s *PGProjectStore) Get(ctx context.Context, id uuid.UUID) (*Project, error) {
	return s.scanOne(ctx, `SELECT * FROM projects WHERE id = $1`, id)
}

func (s *PGProjectStore) GetBySlug(ctx context.Context, companyID uuid.UUID, slug string) (*Project, error) {
	return s.scanOne(ctx, `SELECT * FROM projects WHERE company_id = $1 AND slug = $2`, companyID, slug)
}

func (s *PGProjectStore) Update(ctx context.Context, p *Project) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE projects SET company_id=$2, name=$3, slug=$4, description=$5, metadata=$6, updated_at=NOW()
		WHERE id=$1`,
		p.ID, p.CompanyID, p.Name, p.Slug, p.Description, p.Metadata)
	if err != nil {
		if isDuplicateError(err) {
			return fmt.Errorf("%w: project slug %s in company", ErrDuplicate, p.Slug)
		}
		return fmt.Errorf("update project: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGProjectStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGProjectStore) List(ctx context.Context, f ProjectFilter) ([]*Project, error) {
	query := `SELECT * FROM projects WHERE 1=1`
	args := []any{}
	idx := 1

	if f.CompanyID != nil {
		query += fmt.Sprintf(` AND company_id = $%d`, idx)
		args = append(args, *f.CompanyID)
		idx++
	}
	if f.Slug != "" {
		query += fmt.Sprintf(` AND slug = $%d`, idx)
		args = append(args, f.Slug)
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
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var projects []*Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (s *PGProjectStore) ListForUser(ctx context.Context, userID uuid.UUID) ([]*Project, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT p.* FROM projects p
		JOIN memberships m ON (m.company_id = p.company_id AND (m.project_id IS NULL OR m.project_id = p.id))
		WHERE m.user_id = $1
		ORDER BY p.created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list projects for user: %w", err)
	}
	defer rows.Close()

	var projects []*Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (s *PGProjectStore) scanOne(ctx context.Context, query string, args ...any) (*Project, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query project: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("query project: %w", err)
		}
		return nil, ErrNotFound
	}
	return scanProject(rows)
}

func scanProject(rows pgx.Rows) (*Project, error) {
	var p Project
	err := rows.Scan(&p.ID, &p.CompanyID, &p.Name, &p.Slug, &p.Description, &p.Metadata, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("scan project: %w", err)
	}
	return &p, nil
}
