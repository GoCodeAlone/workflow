package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGUserStore implements UserStore backed by PostgreSQL.
type PGUserStore struct {
	pool *pgxpool.Pool
}

func (s *PGUserStore) Create(ctx context.Context, u *User) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, display_name, avatar_url,
			oauth_provider, oauth_id, active, metadata, created_at, updated_at, last_login_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW(),NOW(),$10)`,
		u.ID, u.Email, u.PasswordHash, u.DisplayName, u.AvatarURL,
		u.OAuthProvider, u.OAuthID, u.Active, u.Metadata, u.LastLoginAt)
	if err != nil {
		if isDuplicateError(err) {
			return fmt.Errorf("%w: user with email %s", ErrDuplicate, u.Email)
		}
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

func (s *PGUserStore) Get(ctx context.Context, id uuid.UUID) (*User, error) {
	return s.scanOne(ctx, `SELECT * FROM users WHERE id = $1`, id)
}

func (s *PGUserStore) GetByEmail(ctx context.Context, email string) (*User, error) {
	return s.scanOne(ctx, `SELECT * FROM users WHERE email = $1`, email)
}

func (s *PGUserStore) GetByOAuth(ctx context.Context, provider OAuthProvider, oauthID string) (*User, error) {
	return s.scanOne(ctx, `SELECT * FROM users WHERE oauth_provider = $1 AND oauth_id = $2`, provider, oauthID)
}

func (s *PGUserStore) Update(ctx context.Context, u *User) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE users SET email=$2, password_hash=$3, display_name=$4, avatar_url=$5,
			oauth_provider=$6, oauth_id=$7, active=$8, metadata=$9,
			updated_at=NOW(), last_login_at=$10
		WHERE id=$1`,
		u.ID, u.Email, u.PasswordHash, u.DisplayName, u.AvatarURL,
		u.OAuthProvider, u.OAuthID, u.Active, u.Metadata, u.LastLoginAt)
	if err != nil {
		if isDuplicateError(err) {
			return fmt.Errorf("%w: user email %s", ErrDuplicate, u.Email)
		}
		return fmt.Errorf("update user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGUserStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGUserStore) List(ctx context.Context, f UserFilter) ([]*User, error) {
	query := `SELECT * FROM users WHERE 1=1`
	args := []any{}
	idx := 1

	if f.Email != "" {
		query += fmt.Sprintf(` AND email = $%d`, idx)
		args = append(args, f.Email)
		idx++
	}
	if f.Active != nil {
		query += fmt.Sprintf(` AND active = $%d`, idx)
		args = append(args, *f.Active)
		idx++
	}
	if f.OAuthProvider != "" {
		query += fmt.Sprintf(` AND oauth_provider = $%d`, idx)
		args = append(args, f.OAuthProvider)
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
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *PGUserStore) scanOne(ctx context.Context, query string, args ...any) (*User, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query user: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("query user: %w", err)
		}
		return nil, ErrNotFound
	}
	return scanUser(rows)
}

func scanUser(rows pgx.Rows) (*User, error) {
	var u User
	err := rows.Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.AvatarURL,
		&u.OAuthProvider, &u.OAuthID, &u.Active, &u.Metadata,
		&u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}
	return &u, nil
}

// isDuplicateError checks for PostgreSQL unique-violation (23505).
func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}
