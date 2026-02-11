package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGMembershipStore implements MembershipStore backed by PostgreSQL.
type PGMembershipStore struct {
	pool *pgxpool.Pool
}

func (s *PGMembershipStore) Create(ctx context.Context, m *Membership) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO memberships (id, user_id, company_id, project_id, role, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,NOW(),NOW())`,
		m.ID, m.UserID, m.CompanyID, m.ProjectID, m.Role)
	if err != nil {
		if isDuplicateError(err) {
			return fmt.Errorf("%w: membership already exists", ErrDuplicate)
		}
		return fmt.Errorf("insert membership: %w", err)
	}
	return nil
}

func (s *PGMembershipStore) Get(ctx context.Context, id uuid.UUID) (*Membership, error) {
	return s.scanOne(ctx, `SELECT * FROM memberships WHERE id = $1`, id)
}

func (s *PGMembershipStore) Update(ctx context.Context, m *Membership) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE memberships SET role=$2, updated_at=NOW()
		WHERE id=$1`,
		m.ID, m.Role)
	if err != nil {
		return fmt.Errorf("update membership: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGMembershipStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM memberships WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete membership: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGMembershipStore) List(ctx context.Context, f MembershipFilter) ([]*Membership, error) {
	query := `SELECT * FROM memberships WHERE 1=1`
	args := []interface{}{}
	idx := 1

	if f.UserID != nil {
		query += fmt.Sprintf(` AND user_id = $%d`, idx)
		args = append(args, *f.UserID)
		idx++
	}
	if f.CompanyID != nil {
		query += fmt.Sprintf(` AND company_id = $%d`, idx)
		args = append(args, *f.CompanyID)
		idx++
	}
	if f.ProjectID != nil {
		query += fmt.Sprintf(` AND project_id = $%d`, idx)
		args = append(args, *f.ProjectID)
		idx++
	}
	if f.Role != "" {
		query += fmt.Sprintf(` AND role = $%d`, idx)
		args = append(args, f.Role)
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
		return nil, fmt.Errorf("list memberships: %w", err)
	}
	defer rows.Close()

	var memberships []*Membership
	for rows.Next() {
		m, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		memberships = append(memberships, m)
	}
	return memberships, rows.Err()
}

// GetEffectiveRole resolves the effective role for a user. If projectID is non-nil,
// it first checks for a project-level membership; if none is found it falls back
// to the company-level membership.
func (s *PGMembershipStore) GetEffectiveRole(ctx context.Context, userID, companyID uuid.UUID, projectID *uuid.UUID) (Role, error) {
	if projectID != nil {
		// Try project-level first.
		var role Role
		err := s.pool.QueryRow(ctx, `
			SELECT role FROM memberships
			WHERE user_id = $1 AND company_id = $2 AND project_id = $3`,
			userID, companyID, *projectID).Scan(&role)
		if err == nil {
			return role, nil
		}
		// Fall through to company-level.
	}

	// Company-level membership (project_id IS NULL).
	var role Role
	err := s.pool.QueryRow(ctx, `
		SELECT role FROM memberships
		WHERE user_id = $1 AND company_id = $2 AND project_id IS NULL`,
		userID, companyID).Scan(&role)
	if err != nil {
		return "", ErrNotFound
	}
	return role, nil
}

func (s *PGMembershipStore) scanOne(ctx context.Context, query string, args ...interface{}) (*Membership, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query membership: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("query membership: %w", err)
		}
		return nil, ErrNotFound
	}
	return scanMembership(rows)
}

func scanMembership(rows pgx.Rows) (*Membership, error) {
	var m Membership
	err := rows.Scan(&m.ID, &m.UserID, &m.CompanyID, &m.ProjectID, &m.Role, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("scan membership: %w", err)
	}
	return &m, nil
}
