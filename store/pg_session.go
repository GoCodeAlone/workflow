package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGSessionStore implements SessionStore backed by PostgreSQL.
type PGSessionStore struct {
	pool *pgxpool.Pool
}

func (s *PGSessionStore) Create(ctx context.Context, sess *Session) error {
	if sess.ID == uuid.Nil {
		sess.ID = uuid.New()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sessions (id, user_id, token, ip_address, user_agent,
			metadata, active, created_at, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NOW(),$8)`,
		sess.ID, sess.UserID, sess.Token, sess.IPAddress, sess.UserAgent,
		sess.Metadata, sess.Active, sess.ExpiresAt)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (s *PGSessionStore) Get(ctx context.Context, id uuid.UUID) (*Session, error) {
	return s.scanOne(ctx, `SELECT * FROM sessions WHERE id = $1`, id)
}

func (s *PGSessionStore) GetByToken(ctx context.Context, token string) (*Session, error) {
	return s.scanOne(ctx, `SELECT * FROM sessions WHERE token = $1 AND active = true`, token)
}

func (s *PGSessionStore) Update(ctx context.Context, sess *Session) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE sessions SET token=$2, ip_address=$3, user_agent=$4,
			metadata=$5, active=$6, expires_at=$7
		WHERE id=$1`,
		sess.ID, sess.Token, sess.IPAddress, sess.UserAgent,
		sess.Metadata, sess.Active, sess.ExpiresAt)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGSessionStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGSessionStore) List(ctx context.Context, f SessionFilter) ([]*Session, error) {
	query := `SELECT * FROM sessions WHERE 1=1`
	args := []interface{}{}
	idx := 1

	if f.UserID != nil {
		query += fmt.Sprintf(` AND user_id = $%d`, idx)
		args = append(args, *f.UserID)
		idx++
	}
	if f.Active != nil {
		query += fmt.Sprintf(` AND active = $%d`, idx)
		args = append(args, *f.Active)
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
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func (s *PGSessionStore) DeleteExpired(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < NOW() OR active = false`)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *PGSessionStore) scanOne(ctx context.Context, query string, args ...interface{}) (*Session, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query session: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("query session: %w", err)
		}
		return nil, ErrNotFound
	}
	return scanSession(rows)
}

func scanSession(rows pgx.Rows) (*Session, error) {
	var sess Session
	err := rows.Scan(&sess.ID, &sess.UserID, &sess.Token, &sess.IPAddress,
		&sess.UserAgent, &sess.Metadata, &sess.Active, &sess.CreatedAt, &sess.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("scan session: %w", err)
	}
	return &sess, nil
}
