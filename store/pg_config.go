package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ConfigDocument represents a config record in the database.
type ConfigDocument struct {
	Key       string
	Data      []byte
	Hash      string
	Version   int
	UpdatedAt time.Time
}

// PGConfigStore manages config documents in PostgreSQL.
type PGConfigStore struct {
	pool *pgxpool.Pool
}

// NewPGConfigStore creates a config store backed by the given connection pool.
func NewPGConfigStore(pool *pgxpool.Pool) *PGConfigStore {
	return &PGConfigStore{pool: pool}
}

func (s *PGConfigStore) GetConfigDocument(ctx context.Context, key string) ([]byte, error) {
	var data []byte
	err := s.pool.QueryRow(ctx,
		`SELECT data FROM config_documents WHERE key = $1`, key).Scan(&data)
	if err != nil {
		return nil, fmt.Errorf("get config document %q: %w", key, err)
	}
	return data, nil
}

func (s *PGConfigStore) GetConfigDocumentHash(ctx context.Context, key string) (string, error) {
	var hash string
	err := s.pool.QueryRow(ctx,
		`SELECT hash FROM config_documents WHERE key = $1`, key).Scan(&hash)
	if err != nil {
		return "", fmt.Errorf("get config document hash %q: %w", key, err)
	}
	return hash, nil
}

func (s *PGConfigStore) PutConfigDocument(ctx context.Context, key string, data []byte) error {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		INSERT INTO config_documents (key, data, hash, version, created_by)
		VALUES ($1, $2, $3, 1, 'system')
		ON CONFLICT (key) DO UPDATE SET
			data = EXCLUDED.data,
			hash = EXCLUDED.hash,
			version = config_documents.version + 1,
			updated_at = NOW()
	`, key, data, hash)
	if err != nil {
		return fmt.Errorf("upsert config document %q: %w", key, err)
	}

	// Record history
	_, err = tx.Exec(ctx, `
		INSERT INTO config_document_history (key, data, hash, version, changed_by)
		SELECT key, data, hash, version, 'system' FROM config_documents WHERE key = $1
	`, key)
	if err != nil {
		return fmt.Errorf("record config history %q: %w", key, err)
	}

	return tx.Commit(ctx)
}

func (s *PGConfigStore) ListConfigDocuments(ctx context.Context) ([]ConfigDocument, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT key, data, hash, version, updated_at FROM config_documents ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("list config documents: %w", err)
	}
	defer rows.Close()

	var docs []ConfigDocument
	for rows.Next() {
		var d ConfigDocument
		if err := rows.Scan(&d.Key, &d.Data, &d.Hash, &d.Version, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan config document: %w", err)
		}
		docs = append(docs, d)
	}
	return docs, rows.Err()
}
