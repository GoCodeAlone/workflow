package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// testAPIKeyStores returns both in-memory and SQLite stores for table-driven testing.
func testAPIKeyStores(t *testing.T) map[string]APIKeyStore {
	t.Helper()

	mem := NewInMemoryAPIKeyStore()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_api_keys.db")
	sqlite, err := NewSQLiteAPIKeyStore(dbPath)
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	t.Cleanup(func() { sqlite.Close() })

	return map[string]APIKeyStore{
		"InMemory": mem,
		"SQLite":   sqlite,
	}
}

func TestCreateAndValidate(t *testing.T) {
	for name, s := range testAPIKeyStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			companyID := uuid.New()
			createdBy := uuid.New()

			key := &APIKey{
				Name:        "test-key",
				CompanyID:   companyID,
				Permissions: []string{"read", "write"},
				CreatedBy:   createdBy,
				IsActive:    true,
			}

			rawKey, err := s.Create(ctx, key)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}

			// Raw key should have the correct format.
			if !strings.HasPrefix(rawKey, "wf_") {
				t.Errorf("raw key should start with 'wf_', got %q", rawKey)
			}
			if len(rawKey) != 3+32 { // "wf_" + 32 hex chars
				t.Errorf("raw key length: want 35, got %d", len(rawKey))
			}

			// Should be able to validate with the raw key.
			validated, err := s.Validate(ctx, rawKey)
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}

			if validated.ID != key.ID {
				t.Errorf("validated ID: got %v, want %v", validated.ID, key.ID)
			}
			if validated.Name != "test-key" {
				t.Errorf("validated Name: got %q, want %q", validated.Name, "test-key")
			}
			if validated.CompanyID != companyID {
				t.Errorf("validated CompanyID: got %v, want %v", validated.CompanyID, companyID)
			}
			if len(validated.Permissions) != 2 || validated.Permissions[0] != "read" || validated.Permissions[1] != "write" {
				t.Errorf("validated Permissions: got %v, want [read write]", validated.Permissions)
			}

			// Should NOT validate with a wrong key.
			_, err = s.Validate(ctx, "wf_0000000000000000000000000000dead")
			if err != ErrNotFound {
				t.Errorf("Validate wrong key: got %v, want ErrNotFound", err)
			}
		})
	}
}

func TestKeyPrefix(t *testing.T) {
	for name, s := range testAPIKeyStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			key := &APIKey{
				Name:        "prefix-test",
				CompanyID:   uuid.New(),
				Permissions: []string{"read"},
				CreatedBy:   uuid.New(),
				IsActive:    true,
			}

			rawKey, err := s.Create(ctx, key)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}

			// Prefix should be the first 11 characters of the raw key ("wf_" + 8 hex).
			expectedPrefix := rawKey[:11]

			// Re-fetch to verify stored prefix.
			fetched, err := s.Get(ctx, key.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}

			if fetched.KeyPrefix != expectedPrefix {
				t.Errorf("KeyPrefix: got %q, want %q", fetched.KeyPrefix, expectedPrefix)
			}
		})
	}
}

func TestDeleteKey(t *testing.T) {
	for name, s := range testAPIKeyStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			key := &APIKey{
				Name:        "delete-test",
				CompanyID:   uuid.New(),
				Permissions: []string{"admin"},
				CreatedBy:   uuid.New(),
				IsActive:    true,
			}

			rawKey, err := s.Create(ctx, key)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}

			// Validate before deletion should succeed.
			if _, err := s.Validate(ctx, rawKey); err != nil {
				t.Fatalf("Validate before delete: %v", err)
			}

			// Delete the key.
			if err := s.Delete(ctx, key.ID); err != nil {
				t.Fatalf("Delete: %v", err)
			}

			// Validate after deletion should fail.
			_, err = s.Validate(ctx, rawKey)
			if err != ErrNotFound {
				t.Errorf("Validate after delete: got %v, want ErrNotFound", err)
			}

			// Get should also fail.
			_, err = s.Get(ctx, key.ID)
			if err != ErrNotFound {
				t.Errorf("Get after delete: got %v, want ErrNotFound", err)
			}

			// Delete non-existent should return ErrNotFound.
			err = s.Delete(ctx, uuid.New())
			if err != ErrNotFound {
				t.Errorf("Delete non-existent: got %v, want ErrNotFound", err)
			}
		})
	}
}

func TestExpiredKey(t *testing.T) {
	for name, s := range testAPIKeyStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			pastTime := time.Now().Add(-1 * time.Hour)
			key := &APIKey{
				Name:        "expired-key",
				CompanyID:   uuid.New(),
				Permissions: []string{"read"},
				CreatedBy:   uuid.New(),
				IsActive:    true,
				ExpiresAt:   &pastTime,
			}

			rawKey, err := s.Create(ctx, key)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}

			// Validation should fail with ErrKeyExpired.
			_, err = s.Validate(ctx, rawKey)
			if err != ErrKeyExpired {
				t.Errorf("Validate expired key: got %v, want ErrKeyExpired", err)
			}
		})
	}
}

func TestInactiveKey(t *testing.T) {
	for name, s := range testAPIKeyStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			key := &APIKey{
				Name:        "inactive-key",
				CompanyID:   uuid.New(),
				Permissions: []string{"read"},
				CreatedBy:   uuid.New(),
				IsActive:    false,
			}

			rawKey, err := s.Create(ctx, key)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}

			// Validation should fail with ErrKeyInactive.
			_, err = s.Validate(ctx, rawKey)
			if err != ErrKeyInactive {
				t.Errorf("Validate inactive key: got %v, want ErrKeyInactive", err)
			}
		})
	}
}

func TestListKeys(t *testing.T) {
	for name, s := range testAPIKeyStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			companyA := uuid.New()
			companyB := uuid.New()
			createdBy := uuid.New()

			// Create keys for company A.
			for i := 0; i < 3; i++ {
				key := &APIKey{
					Name:        "key-a",
					CompanyID:   companyA,
					Permissions: []string{"read"},
					CreatedBy:   createdBy,
					IsActive:    true,
				}
				if _, err := s.Create(ctx, key); err != nil {
					t.Fatalf("Create key-a[%d]: %v", i, err)
				}
				// Small sleep to ensure distinct timestamps for ordering.
				time.Sleep(time.Millisecond)
			}

			// Create keys for company B.
			for i := 0; i < 2; i++ {
				key := &APIKey{
					Name:        "key-b",
					CompanyID:   companyB,
					Permissions: []string{"write"},
					CreatedBy:   createdBy,
					IsActive:    true,
				}
				if _, err := s.Create(ctx, key); err != nil {
					t.Fatalf("Create key-b[%d]: %v", i, err)
				}
				time.Sleep(time.Millisecond)
			}

			// List company A keys.
			keysA, err := s.List(ctx, companyA)
			if err != nil {
				t.Fatalf("List companyA: %v", err)
			}
			if len(keysA) != 3 {
				t.Errorf("List companyA: got %d keys, want 3", len(keysA))
			}

			// List company B keys.
			keysB, err := s.List(ctx, companyB)
			if err != nil {
				t.Fatalf("List companyB: %v", err)
			}
			if len(keysB) != 2 {
				t.Errorf("List companyB: got %d keys, want 2", len(keysB))
			}

			// List non-existent company.
			keysNone, err := s.List(ctx, uuid.New())
			if err != nil {
				t.Fatalf("List unknown: %v", err)
			}
			if len(keysNone) != 0 {
				t.Errorf("List unknown: got %d keys, want 0", len(keysNone))
			}
		})
	}
}

func TestUpdateLastUsed(t *testing.T) {
	for name, s := range testAPIKeyStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			key := &APIKey{
				Name:        "last-used-test",
				CompanyID:   uuid.New(),
				Permissions: []string{"read"},
				CreatedBy:   uuid.New(),
				IsActive:    true,
			}

			if _, err := s.Create(ctx, key); err != nil {
				t.Fatalf("Create: %v", err)
			}

			// Initially, LastUsedAt should be nil.
			fetched, err := s.Get(ctx, key.ID)
			if err != nil {
				t.Fatalf("Get before update: %v", err)
			}
			if fetched.LastUsedAt != nil {
				t.Errorf("LastUsedAt before update: got %v, want nil", fetched.LastUsedAt)
			}

			// Update last used.
			before := time.Now()
			if err := s.UpdateLastUsed(ctx, key.ID); err != nil {
				t.Fatalf("UpdateLastUsed: %v", err)
			}

			// Fetch again and verify.
			fetched, err = s.Get(ctx, key.ID)
			if err != nil {
				t.Fatalf("Get after update: %v", err)
			}
			if fetched.LastUsedAt == nil {
				t.Fatal("LastUsedAt after update: got nil, want non-nil")
			}
			if fetched.LastUsedAt.Before(before.Add(-time.Second)) {
				t.Errorf("LastUsedAt too early: %v, expected around %v", *fetched.LastUsedAt, before)
			}

			// Update non-existent key should return ErrNotFound.
			err = s.UpdateLastUsed(ctx, uuid.New())
			if err != ErrNotFound {
				t.Errorf("UpdateLastUsed non-existent: got %v, want ErrNotFound", err)
			}
		})
	}
}

func TestGetByHash(t *testing.T) {
	for name, s := range testAPIKeyStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			key := &APIKey{
				Name:        "hash-lookup-test",
				CompanyID:   uuid.New(),
				Permissions: []string{"read"},
				CreatedBy:   uuid.New(),
				IsActive:    true,
			}

			rawKey, err := s.Create(ctx, key)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}

			h := hashKey(rawKey)
			fetched, err := s.GetByHash(ctx, h)
			if err != nil {
				t.Fatalf("GetByHash: %v", err)
			}
			if fetched.ID != key.ID {
				t.Errorf("GetByHash ID: got %v, want %v", fetched.ID, key.ID)
			}

			// Non-existent hash.
			_, err = s.GetByHash(ctx, hashKey("wf_nonexistent00000000000000000000"))
			if err != ErrNotFound {
				t.Errorf("GetByHash non-existent: got %v, want ErrNotFound", err)
			}
		})
	}
}

func TestOptionalScoping(t *testing.T) {
	for name, s := range testAPIKeyStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			orgID := uuid.New()
			projectID := uuid.New()

			key := &APIKey{
				Name:        "scoped-key",
				CompanyID:   uuid.New(),
				OrgID:       &orgID,
				ProjectID:   &projectID,
				Permissions: []string{"read", "write", "admin"},
				CreatedBy:   uuid.New(),
				IsActive:    true,
			}

			rawKey, err := s.Create(ctx, key)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}

			validated, err := s.Validate(ctx, rawKey)
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}

			if validated.OrgID == nil || *validated.OrgID != orgID {
				t.Errorf("OrgID: got %v, want %v", validated.OrgID, orgID)
			}
			if validated.ProjectID == nil || *validated.ProjectID != projectID {
				t.Errorf("ProjectID: got %v, want %v", validated.ProjectID, projectID)
			}
			if len(validated.Permissions) != 3 {
				t.Errorf("Permissions count: got %d, want 3", len(validated.Permissions))
			}
		})
	}
}

func TestSQLiteAPIKeyStore_PersistsAcrossReopen(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "persist_test.db")

	// Create store and add a key.
	s1, err := NewSQLiteAPIKeyStore(dbPath)
	if err != nil {
		t.Fatalf("create store 1: %v", err)
	}

	ctx := context.Background()
	key := &APIKey{
		Name:        "persist-key",
		CompanyID:   uuid.New(),
		Permissions: []string{"read"},
		CreatedBy:   uuid.New(),
		IsActive:    true,
	}
	rawKey, err := s1.Create(ctx, key)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	s1.Close()

	// Reopen and validate.
	s2, err := NewSQLiteAPIKeyStore(dbPath)
	if err != nil {
		t.Fatalf("create store 2: %v", err)
	}
	defer s2.Close()

	validated, err := s2.Validate(ctx, rawKey)
	if err != nil {
		t.Fatalf("Validate after reopen: %v", err)
	}
	if validated.ID != key.ID {
		t.Errorf("ID after reopen: got %v, want %v", validated.ID, key.ID)
	}
}

func TestSQLiteAPIKeyStoreFromDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "from_db_test.db")

	// Ensure the file doesn't exist yet (sql.Open with sqlite creates it).
	os.Remove(dbPath)

	s, err := NewSQLiteAPIKeyStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteAPIKeyStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	key := &APIKey{
		Name:        "from-db-test",
		CompanyID:   uuid.New(),
		Permissions: []string{"write"},
		CreatedBy:   uuid.New(),
		IsActive:    true,
	}
	rawKey, err := s.Create(ctx, key)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	validated, err := s.Validate(ctx, rawKey)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if validated.Name != "from-db-test" {
		t.Errorf("Name: got %q, want %q", validated.Name, "from-db-test")
	}
}
