package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGAPIKeyStore implements APIKeyStore backed by PostgreSQL using pgxpool.
type PGAPIKeyStore struct {
	pool *pgxpool.Pool
}

// NewPGAPIKeyStore creates a new PGAPIKeyStore backed by the given connection pool
// and ensures the required schema exists.
func NewPGAPIKeyStore(pool *pgxpool.Pool) (*PGAPIKeyStore, error) {
	s := &PGAPIKeyStore{pool: pool}
	if err := s.createTable(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *PGAPIKeyStore) createTable(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS api_keys (
			id          UUID        PRIMARY KEY,
			name        TEXT        NOT NULL,
			key_hash    TEXT        NOT NULL UNIQUE,
			key_prefix  TEXT        NOT NULL,
			company_id  UUID        NOT NULL,
			org_id      UUID,
			project_id  UUID,
			permissions JSONB       NOT NULL DEFAULT '[]',
			created_by  UUID        NOT NULL,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			expires_at  TIMESTAMPTZ,
			last_used_at TIMESTAMPTZ,
			is_active   BOOLEAN     NOT NULL DEFAULT TRUE
		);
		CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash   ON api_keys(key_hash);
		CREATE INDEX IF NOT EXISTS idx_api_keys_company_id ON api_keys(company_id);
	`)
	if err != nil {
		return fmt.Errorf("create api_keys table: %w", err)
	}
	return nil
}

func (s *PGAPIKeyStore) Create(ctx context.Context, key *APIKey) (string, error) {
	rawKey, err := generateRawKey()
	if err != nil {
		return "", err
	}

	if key.ID == uuid.Nil {
		key.ID = uuid.New()
	}
	key.KeyHash = hashKey(rawKey)
	key.KeyPrefix = rawKey[:len(apiKeyPrefix)+8]
	key.CreatedAt = time.Now()
	if key.Permissions == nil {
		key.Permissions = []string{}
	}

	permsJSON, err := json.Marshal(key.Permissions)
	if err != nil {
		return "", fmt.Errorf("marshal permissions: %w", err)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO api_keys (id, name, key_hash, key_prefix, company_id, org_id, project_id,
			permissions, created_by, created_at, expires_at, last_used_at, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		key.ID,
		key.Name,
		key.KeyHash,
		key.KeyPrefix,
		key.CompanyID,
		key.OrgID,
		key.ProjectID,
		permsJSON,
		key.CreatedBy,
		key.CreatedAt,
		key.ExpiresAt,
		key.LastUsedAt,
		key.IsActive,
	)
	if err != nil {
		return "", fmt.Errorf("insert api key: %w", err)
	}
	return rawKey, nil
}

func (s *PGAPIKeyStore) Get(ctx context.Context, id uuid.UUID) (*APIKey, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, key_hash, key_prefix, company_id, org_id, project_id,
			permissions, created_by, created_at, expires_at, last_used_at, is_active
		FROM api_keys WHERE id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("query api key: %w", err)
	}
	defer rows.Close()
	return scanPGAPIKeyOne(rows)
}

func (s *PGAPIKeyStore) GetByHash(ctx context.Context, keyHash string) (*APIKey, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, key_hash, key_prefix, company_id, org_id, project_id,
			permissions, created_by, created_at, expires_at, last_used_at, is_active
		FROM api_keys WHERE key_hash = $1`, keyHash)
	if err != nil {
		return nil, fmt.Errorf("query api key by hash: %w", err)
	}
	defer rows.Close()
	return scanPGAPIKeyOne(rows)
}

func (s *PGAPIKeyStore) List(ctx context.Context, companyID uuid.UUID) ([]*APIKey, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, key_hash, key_prefix, company_id, org_id, project_id,
			permissions, created_by, created_at, expires_at, last_used_at, is_active
		FROM api_keys WHERE company_id = $1 ORDER BY created_at ASC`, companyID)
	if err != nil {
		return nil, fmt.Errorf("query api keys: %w", err)
	}
	defer rows.Close()

	var results []*APIKey
	for rows.Next() {
		k, err := scanPGAPIKey(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api keys: %w", err)
	}
	return results, nil
}

func (s *PGAPIKeyStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGAPIKeyStore) UpdateLastUsed(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("update last_used_at: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PGAPIKeyStore) Validate(ctx context.Context, rawKey string) (*APIKey, error) {
	h := hashKey(rawKey)
	k, err := s.GetByHash(ctx, h)
	if err != nil {
		return nil, ErrNotFound
	}
	if !k.IsActive {
		return nil, ErrKeyInactive
	}
	if k.ExpiresAt != nil && k.ExpiresAt.Before(time.Now()) {
		return nil, ErrKeyExpired
	}
	return k, nil
}

// ---------------------------------------------------------------------------
// PostgreSQL scan helpers
// ---------------------------------------------------------------------------

func scanPGAPIKeyOne(rows pgx.Rows) (*APIKey, error) {
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("query api key: %w", err)
		}
		return nil, ErrNotFound
	}
	return scanPGAPIKey(rows)
}

func scanPGAPIKey(rows pgx.Rows) (*APIKey, error) {
	var k APIKey
	var permsJSON []byte
	err := rows.Scan(
		&k.ID, &k.Name, &k.KeyHash, &k.KeyPrefix,
		&k.CompanyID, &k.OrgID, &k.ProjectID,
		&permsJSON, &k.CreatedBy, &k.CreatedAt,
		&k.ExpiresAt, &k.LastUsedAt, &k.IsActive,
	)
	if err != nil {
		return nil, fmt.Errorf("scan api key: %w", err)
	}
	if permsJSON != nil {
		if err := json.Unmarshal(permsJSON, &k.Permissions); err != nil {
			return nil, fmt.Errorf("unmarshal permissions: %w", err)
		}
	}
	if k.Permissions == nil {
		k.Permissions = []string{}
	}
	return &k, nil
}

// ---------------------------------------------------------------------------
// Compile-time interface assertion
// ---------------------------------------------------------------------------

var _ APIKeyStore = (*PGAPIKeyStore)(nil)
