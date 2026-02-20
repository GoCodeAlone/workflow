package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	// SQLite driver
	_ "modernc.org/sqlite"
)

// APIKey represents an API key for programmatic access to the platform.
type APIKey struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	KeyHash     string     `json:"-"`          // SHA-256 hash, never exposed
	KeyPrefix   string     `json:"key_prefix"` // first 8 chars for identification
	CompanyID   uuid.UUID  `json:"company_id"`
	OrgID       *uuid.UUID `json:"org_id,omitempty"`     // optional scoping
	ProjectID   *uuid.UUID `json:"project_id,omitempty"` // optional scoping
	Permissions []string   `json:"permissions"`          // e.g., ["read", "write", "admin"]
	CreatedBy   uuid.UUID  `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	IsActive    bool       `json:"is_active"`
}

// APIKeyStore defines persistence operations for API keys.
type APIKeyStore interface {
	// Create creates a new API key and returns the raw key (only available at creation time).
	Create(ctx context.Context, key *APIKey) (rawKey string, err error)
	// Get retrieves an API key by ID.
	Get(ctx context.Context, id uuid.UUID) (*APIKey, error)
	// GetByHash retrieves an API key by its hash.
	GetByHash(ctx context.Context, keyHash string) (*APIKey, error)
	// List returns all API keys for a company.
	List(ctx context.Context, companyID uuid.UUID) ([]*APIKey, error)
	// Delete removes an API key by ID.
	Delete(ctx context.Context, id uuid.UUID) error
	// UpdateLastUsed updates the last_used_at timestamp for an API key.
	UpdateLastUsed(ctx context.Context, id uuid.UUID) error
	// Validate hashes the raw key and looks up the corresponding API key.
	// Returns ErrNotFound if no matching key exists, or ErrKeyExpired / ErrKeyInactive
	// for keys that exist but cannot be used.
	Validate(ctx context.Context, rawKey string) (*APIKey, error)
}

// Sentinel errors for API key operations.
var (
	ErrKeyExpired  = fmt.Errorf("api key expired")
	ErrKeyInactive = fmt.Errorf("api key inactive")
)

// apiKeyPrefix is the prefix for all generated API keys.
const apiKeyPrefix = "wf_"

// generateRawKey creates a new raw API key: "wf_" + 32 random hex chars.
func generateRawKey() (string, error) {
	b := make([]byte, 16) // 16 bytes = 32 hex chars
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return apiKeyPrefix + hex.EncodeToString(b), nil
}

// hashKey returns the SHA-256 hex digest of a raw API key.
func hashKey(rawKey string) string {
	h := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(h[:])
}

// constantTimeHashCompare compares two hex-encoded hashes in constant time.
func constantTimeHashCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// ---------------------------------------------------------------------------
// InMemoryAPIKeyStore
// ---------------------------------------------------------------------------

// InMemoryAPIKeyStore is a thread-safe in-memory implementation of APIKeyStore.
type InMemoryAPIKeyStore struct {
	mu   sync.Mutex
	keys map[uuid.UUID]*APIKey
}

// NewInMemoryAPIKeyStore creates a new InMemoryAPIKeyStore.
func NewInMemoryAPIKeyStore() *InMemoryAPIKeyStore {
	return &InMemoryAPIKeyStore{keys: make(map[uuid.UUID]*APIKey)}
}

func (s *InMemoryAPIKeyStore) Create(_ context.Context, key *APIKey) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rawKey, err := generateRawKey()
	if err != nil {
		return "", err
	}

	if key.ID == uuid.Nil {
		key.ID = uuid.New()
	}
	key.KeyHash = hashKey(rawKey)
	key.KeyPrefix = rawKey[:len(apiKeyPrefix)+8] // "wf_" + first 8 hex chars
	key.CreatedAt = time.Now()
	if key.Permissions == nil {
		key.Permissions = []string{}
	}

	cp := copyAPIKey(key)
	s.keys[key.ID] = cp
	return rawKey, nil
}

func (s *InMemoryAPIKeyStore) Get(_ context.Context, id uuid.UUID) (*APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.keys[id]
	if !ok {
		return nil, ErrNotFound
	}
	return copyAPIKey(k), nil
}

func (s *InMemoryAPIKeyStore) GetByHash(_ context.Context, keyHash string) (*APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range s.keys {
		if constantTimeHashCompare(k.KeyHash, keyHash) {
			return copyAPIKey(k), nil
		}
	}
	return nil, ErrNotFound
}

func (s *InMemoryAPIKeyStore) List(_ context.Context, companyID uuid.UUID) ([]*APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var results []*APIKey
	for _, k := range s.keys {
		if k.CompanyID == companyID {
			results = append(results, copyAPIKey(k))
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.Before(results[j].CreatedAt)
	})
	return results, nil
}

func (s *InMemoryAPIKeyStore) Delete(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.keys[id]; !ok {
		return ErrNotFound
	}
	delete(s.keys, id)
	return nil
}

func (s *InMemoryAPIKeyStore) UpdateLastUsed(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.keys[id]
	if !ok {
		return ErrNotFound
	}
	now := time.Now()
	k.LastUsedAt = &now
	return nil
}

func (s *InMemoryAPIKeyStore) Validate(_ context.Context, rawKey string) (*APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	h := hashKey(rawKey)
	for _, k := range s.keys {
		if constantTimeHashCompare(k.KeyHash, h) {
			if !k.IsActive {
				return nil, ErrKeyInactive
			}
			if k.ExpiresAt != nil && k.ExpiresAt.Before(time.Now()) {
				return nil, ErrKeyExpired
			}
			return copyAPIKey(k), nil
		}
	}
	return nil, ErrNotFound
}

// copyAPIKey returns a deep copy of an APIKey.
func copyAPIKey(k *APIKey) *APIKey {
	cp := *k
	if k.OrgID != nil {
		oid := *k.OrgID
		cp.OrgID = &oid
	}
	if k.ProjectID != nil {
		pid := *k.ProjectID
		cp.ProjectID = &pid
	}
	if k.ExpiresAt != nil {
		t := *k.ExpiresAt
		cp.ExpiresAt = &t
	}
	if k.LastUsedAt != nil {
		t := *k.LastUsedAt
		cp.LastUsedAt = &t
	}
	if k.Permissions != nil {
		cp.Permissions = make([]string, len(k.Permissions))
		copy(cp.Permissions, k.Permissions)
	}
	return &cp
}

// ---------------------------------------------------------------------------
// SQLiteAPIKeyStore
// ---------------------------------------------------------------------------

// SQLiteAPIKeyStore is a SQLite-backed implementation of APIKeyStore.
type SQLiteAPIKeyStore struct {
	db *sql.DB
}

// NewSQLiteAPIKeyStore creates a new SQLiteAPIKeyStore and initializes the schema.
func NewSQLiteAPIKeyStore(dbPath string) (*SQLiteAPIKeyStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	// Enable WAL mode for better concurrent performance.
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	s := &SQLiteAPIKeyStore{db: db}
	if err := s.createTable(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// NewSQLiteAPIKeyStoreFromDB creates a SQLiteAPIKeyStore from an existing *sql.DB.
func NewSQLiteAPIKeyStoreFromDB(db *sql.DB) (*SQLiteAPIKeyStore, error) {
	s := &SQLiteAPIKeyStore{db: db}
	if err := s.createTable(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SQLiteAPIKeyStore) createTable() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS api_keys (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			key_prefix TEXT NOT NULL,
			company_id TEXT NOT NULL,
			org_id TEXT,
			project_id TEXT,
			permissions TEXT NOT NULL DEFAULT '[]',
			created_by TEXT NOT NULL,
			created_at TEXT NOT NULL,
			expires_at TEXT,
			last_used_at TEXT,
			is_active INTEGER NOT NULL DEFAULT 1
		)`)
	if err != nil {
		return fmt.Errorf("create api_keys table: %w", err)
	}

	// Index on key_hash for fast lookups during validation.
	_, err = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys(key_hash)`)
	if err != nil {
		return fmt.Errorf("create key_hash index: %w", err)
	}

	// Index on company_id for listing.
	_, err = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_api_keys_company_id ON api_keys(company_id)`)
	if err != nil {
		return fmt.Errorf("create company_id index: %w", err)
	}

	return nil
}

// Close closes the underlying database connection.
func (s *SQLiteAPIKeyStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteAPIKeyStore) Create(ctx context.Context, key *APIKey) (string, error) {
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

	permsJSON := marshalPermissions(key.Permissions)

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO api_keys (id, name, key_hash, key_prefix, company_id, org_id, project_id,
			permissions, created_by, created_at, expires_at, last_used_at, is_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		key.ID.String(),
		key.Name,
		key.KeyHash,
		key.KeyPrefix,
		key.CompanyID.String(),
		nullableUUIDStr(key.OrgID),
		nullableUUIDStr(key.ProjectID),
		permsJSON,
		key.CreatedBy.String(),
		key.CreatedAt.Format(time.RFC3339Nano),
		nullableTimeStr(key.ExpiresAt),
		nullableTimeStr(key.LastUsedAt),
		boolToInt(key.IsActive),
	)
	if err != nil {
		return "", fmt.Errorf("insert api key: %w", err)
	}
	return rawKey, nil
}

func (s *SQLiteAPIKeyStore) Get(ctx context.Context, id uuid.UUID) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, key_hash, key_prefix, company_id, org_id, project_id,
			permissions, created_by, created_at, expires_at, last_used_at, is_active
		FROM api_keys WHERE id = ?`, id.String())
	return scanAPIKey(row)
}

func (s *SQLiteAPIKeyStore) GetByHash(ctx context.Context, keyHash string) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, key_hash, key_prefix, company_id, org_id, project_id,
			permissions, created_by, created_at, expires_at, last_used_at, is_active
		FROM api_keys WHERE key_hash = ?`, keyHash)
	return scanAPIKey(row)
}

func (s *SQLiteAPIKeyStore) List(ctx context.Context, companyID uuid.UUID) ([]*APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, key_hash, key_prefix, company_id, org_id, project_id,
			permissions, created_by, created_at, expires_at, last_used_at, is_active
		FROM api_keys WHERE company_id = ? ORDER BY created_at ASC`, companyID.String())
	if err != nil {
		return nil, fmt.Errorf("query api keys: %w", err)
	}
	defer rows.Close()

	var results []*APIKey
	for rows.Next() {
		k, err := scanAPIKeyFromRows(rows)
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

func (s *SQLiteAPIKeyStore) Delete(ctx context.Context, id uuid.UUID) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ?`, id.String())
	if err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteAPIKeyStore) UpdateLastUsed(ctx context.Context, id uuid.UUID) error {
	now := time.Now().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`, now, id.String())
	if err != nil {
		return fmt.Errorf("update last_used_at: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteAPIKeyStore) Validate(ctx context.Context, rawKey string) (*APIKey, error) {
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

// --- SQLite helpers ---

func scanAPIKey(row *sql.Row) (*APIKey, error) {
	var (
		k                                 APIKey
		idStr, companyIDStr, createdByStr string
		orgIDStr, projectIDStr            sql.NullString
		permsJSON, createdAtStr           string
		expiresAtStr, lastUsedAtStr       sql.NullString
		isActiveInt                       int
	)

	err := row.Scan(
		&idStr, &k.Name, &k.KeyHash, &k.KeyPrefix,
		&companyIDStr, &orgIDStr, &projectIDStr,
		&permsJSON, &createdByStr, &createdAtStr,
		&expiresAtStr, &lastUsedAtStr, &isActiveInt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan api key: %w", err)
	}

	return populateAPIKey(&k, idStr, companyIDStr, createdByStr, orgIDStr, projectIDStr,
		permsJSON, createdAtStr, expiresAtStr, lastUsedAtStr, isActiveInt)
}

func scanAPIKeyFromRows(rows *sql.Rows) (*APIKey, error) {
	var (
		k                                 APIKey
		idStr, companyIDStr, createdByStr string
		orgIDStr, projectIDStr            sql.NullString
		permsJSON, createdAtStr           string
		expiresAtStr, lastUsedAtStr       sql.NullString
		isActiveInt                       int
	)

	err := rows.Scan(
		&idStr, &k.Name, &k.KeyHash, &k.KeyPrefix,
		&companyIDStr, &orgIDStr, &projectIDStr,
		&permsJSON, &createdByStr, &createdAtStr,
		&expiresAtStr, &lastUsedAtStr, &isActiveInt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan api key row: %w", err)
	}

	return populateAPIKey(&k, idStr, companyIDStr, createdByStr, orgIDStr, projectIDStr,
		permsJSON, createdAtStr, expiresAtStr, lastUsedAtStr, isActiveInt)
}

func populateAPIKey(k *APIKey, idStr, companyIDStr, createdByStr string,
	orgIDStr, projectIDStr sql.NullString,
	permsJSON, createdAtStr string,
	expiresAtStr, lastUsedAtStr sql.NullString,
	isActiveInt int) (*APIKey, error) {

	var err error
	k.ID, err = uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("parse id: %w", err)
	}
	k.CompanyID, err = uuid.Parse(companyIDStr)
	if err != nil {
		return nil, fmt.Errorf("parse company_id: %w", err)
	}
	k.CreatedBy, err = uuid.Parse(createdByStr)
	if err != nil {
		return nil, fmt.Errorf("parse created_by: %w", err)
	}

	if orgIDStr.Valid {
		oid, err := uuid.Parse(orgIDStr.String)
		if err != nil {
			return nil, fmt.Errorf("parse org_id: %w", err)
		}
		k.OrgID = &oid
	}
	if projectIDStr.Valid {
		pid, err := uuid.Parse(projectIDStr.String)
		if err != nil {
			return nil, fmt.Errorf("parse project_id: %w", err)
		}
		k.ProjectID = &pid
	}

	k.Permissions = unmarshalPermissions(permsJSON)

	k.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	if expiresAtStr.Valid {
		t, err := time.Parse(time.RFC3339Nano, expiresAtStr.String)
		if err != nil {
			return nil, fmt.Errorf("parse expires_at: %w", err)
		}
		k.ExpiresAt = &t
	}
	if lastUsedAtStr.Valid {
		t, err := time.Parse(time.RFC3339Nano, lastUsedAtStr.String)
		if err != nil {
			return nil, fmt.Errorf("parse last_used_at: %w", err)
		}
		k.LastUsedAt = &t
	}

	k.IsActive = isActiveInt != 0
	return k, nil
}

func nullableUUIDStr(id *uuid.UUID) sql.NullString {
	if id == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: id.String(), Valid: true}
}

func nullableTimeStr(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: t.Format(time.RFC3339Nano), Valid: true}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func marshalPermissions(perms []string) string {
	if len(perms) == 0 {
		return "[]"
	}
	// Simple JSON array encoding to avoid import cycle with encoding/json in tests.
	result := "["
	for i, p := range perms {
		if i > 0 {
			result += ","
		}
		result += `"` + p + `"`
	}
	result += "]"
	return result
}

func unmarshalPermissions(s string) []string {
	if s == "" || s == "[]" {
		return []string{}
	}
	// Simple parser for JSON string arrays ["a","b","c"]
	s = s[1 : len(s)-1] // trim brackets
	if s == "" {
		return []string{}
	}
	var result []string
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			if start == -1 {
				start = i + 1
			} else {
				result = append(result, s[start:i])
				start = -1
			}
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Compile-time interface assertions
// ---------------------------------------------------------------------------

var (
	_ APIKeyStore = (*InMemoryAPIKeyStore)(nil)
	_ APIKeyStore = (*SQLiteAPIKeyStore)(nil)
)
