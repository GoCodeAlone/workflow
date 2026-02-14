package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGCompanyStore implements CompanyStore backed by PostgreSQL.
type PGCompanyStore struct {
	pool *pgxpool.Pool
}

func (s *PGCompanyStore) Create(ctx context.Context, c *Company) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO companies (id, name, slug, owner_id, metadata, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,NOW(),NOW())`,
		c.ID, c.Name, c.Slug, c.OwnerID, c.Metadata)
	if err != nil {
		if isDuplicateError(err) {
			return fmt.Errorf("%w: company slug %s", ErrDuplicate, c.Slug)
		}
		return fmt.Errorf("insert company: %w", err)
	}
	return nil
}

func (s *PGCompanyStore) Get(ctx context.Context, id uuid.UUID) (*Company, error) {
	return s.scanOne(ctx, `SELECT * FROM companies WHERE id = $1`, id)
}

func (s *PGCompanyStore) GetBySlug(ctx context.Context, slug string) (*Company, error) {
	return s.scanOne(ctx, `SELECT * FROM companies WHERE slug = $1`, slug)
}

func (s *PGCompanyStore) Update(ctx context.Context, c *Company) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE companies SET name=$2, slug=$3, owner_id=$4, metadata=$5, updated_at=NOW()
		WHERE id=$1`,
		c.ID, c.Name, c.Slug, c.OwnerID, c.Metadata)
	if err != nil {
		if isDuplicateError(err) {
			return fmt.Errorf("%w: company slug %s", ErrDuplicate, c.Slug)
		}
		return fmt.Errorf("update company: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGCompanyStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM companies WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete company: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGCompanyStore) List(ctx context.Context, f CompanyFilter) ([]*Company, error) {
	query := `SELECT * FROM companies WHERE 1=1`
	args := []any{}
	idx := 1

	if f.OwnerID != nil {
		query += fmt.Sprintf(` AND owner_id = $%d`, idx)
		args = append(args, *f.OwnerID)
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
		return nil, fmt.Errorf("list companies: %w", err)
	}
	defer rows.Close()

	var companies []*Company
	for rows.Next() {
		c, err := scanCompany(rows)
		if err != nil {
			return nil, err
		}
		companies = append(companies, c)
	}
	return companies, rows.Err()
}

func (s *PGCompanyStore) ListForUser(ctx context.Context, userID uuid.UUID) ([]*Company, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT c.* FROM companies c
		JOIN memberships m ON m.company_id = c.id
		WHERE m.user_id = $1
		ORDER BY c.created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list companies for user: %w", err)
	}
	defer rows.Close()

	var companies []*Company
	for rows.Next() {
		c, err := scanCompany(rows)
		if err != nil {
			return nil, err
		}
		companies = append(companies, c)
	}
	return companies, rows.Err()
}

func (s *PGCompanyStore) scanOne(ctx context.Context, query string, args ...any) (*Company, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query company: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("query company: %w", err)
		}
		return nil, ErrNotFound
	}
	return scanCompany(rows)
}

func scanCompany(rows pgx.Rows) (*Company, error) {
	var c Company
	err := rows.Scan(&c.ID, &c.Name, &c.Slug, &c.OwnerID, &c.Metadata, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("scan company: %w", err)
	}
	return &c, nil
}
