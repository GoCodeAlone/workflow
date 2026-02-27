package store

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPGConfigStore_Integration(t *testing.T) {
	pgURL := os.Getenv("PG_URL")
	if pgURL == "" {
		t.Skip("PG_URL not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgURL)
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}

	// Ensure the table exists (run migration inline for integration test).
	_, err = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS config_documents (
			key         TEXT PRIMARY KEY,
			data        BYTEA NOT NULL,
			hash        TEXT NOT NULL,
			version     INTEGER NOT NULL DEFAULT 1,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			created_by  TEXT NOT NULL DEFAULT 'system'
		);
		CREATE TABLE IF NOT EXISTS config_document_history (
			id          BIGSERIAL PRIMARY KEY,
			key         TEXT NOT NULL,
			data        BYTEA NOT NULL,
			hash        TEXT NOT NULL,
			version     INTEGER NOT NULL,
			changed_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			changed_by  TEXT NOT NULL DEFAULT 'system'
		);
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}

	// Clean up test data.
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM config_document_history WHERE key = 'test-integration'`)
		_, _ = pool.Exec(ctx, `DELETE FROM config_documents WHERE key = 'test-integration'`)
	})

	s := NewPGConfigStore(pool)
	const key = "test-integration"
	const yamlV1 = `modules:
  - name: server
    type: http.server
`
	const yamlV2 = `modules:
  - name: server
    type: http.server
  - name: router
    type: http.router
`

	// PutConfigDocument — initial insert.
	if err := s.PutConfigDocument(ctx, key, []byte(yamlV1)); err != nil {
		t.Fatalf("PutConfigDocument (v1): %v", err)
	}

	// GetConfigDocument.
	data, err := s.GetConfigDocument(ctx, key)
	if err != nil {
		t.Fatalf("GetConfigDocument: %v", err)
	}
	if string(data) != yamlV1 {
		t.Errorf("unexpected data: got %q, want %q", data, yamlV1)
	}

	// GetConfigDocumentHash.
	hash, err := s.GetConfigDocumentHash(ctx, key)
	if err != nil {
		t.Fatalf("GetConfigDocumentHash: %v", err)
	}
	if hash == "" {
		t.Error("expected non-empty hash")
	}

	// ListConfigDocuments.
	docs, err := s.ListConfigDocuments(ctx)
	if err != nil {
		t.Fatalf("ListConfigDocuments: %v", err)
	}
	found := false
	for _, d := range docs {
		if d.Key == key {
			found = true
			if d.Version != 1 {
				t.Errorf("expected version 1, got %d", d.Version)
			}
			break
		}
	}
	if !found {
		t.Error("expected to find test key in ListConfigDocuments")
	}

	// PutConfigDocument — update (upsert).
	if err := s.PutConfigDocument(ctx, key, []byte(yamlV2)); err != nil {
		t.Fatalf("PutConfigDocument (v2): %v", err)
	}

	data2, err := s.GetConfigDocument(ctx, key)
	if err != nil {
		t.Fatalf("GetConfigDocument after update: %v", err)
	}
	if string(data2) != yamlV2 {
		t.Errorf("unexpected data after update: got %q, want %q", data2, yamlV2)
	}

	// Verify version incremented.
	docs2, err := s.ListConfigDocuments(ctx)
	if err != nil {
		t.Fatalf("ListConfigDocuments after update: %v", err)
	}
	for _, d := range docs2 {
		if d.Key == key {
			if d.Version != 2 {
				t.Errorf("expected version 2 after update, got %d", d.Version)
			}
			break
		}
	}

	// Hash must have changed after update.
	hash2, err := s.GetConfigDocumentHash(ctx, key)
	if err != nil {
		t.Fatalf("GetConfigDocumentHash after update: %v", err)
	}
	if hash2 == hash {
		t.Error("expected hash to change after update")
	}
}
