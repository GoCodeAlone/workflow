package module

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
)

// UserRecord represents a user for persistence
type UserRecord struct {
	ID           string         `json:"id"`
	Email        string         `json:"email"`
	Name         string         `json:"name"`
	PasswordHash string         `json:"-"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    time.Time      `json:"createdAt"`
}

// PersistenceStore provides SQLite-backed persistence for workflow instances,
// resources, and users.
type PersistenceStore struct {
	name          string
	dbServiceName string
	db            *sql.DB
	encryptor     *FieldEncryptor
}

// NewPersistenceStore creates a new PersistenceStore module.
func NewPersistenceStore(name, dbServiceName string) *PersistenceStore {
	return &PersistenceStore{
		name:          name,
		dbServiceName: dbServiceName,
		encryptor:     NewFieldEncryptorFromEnv(),
	}
}

// Name returns the module name.
func (p *PersistenceStore) Name() string {
	return p.name
}

// Init looks up the WorkflowDatabase service and runs schema migrations.
func (p *PersistenceStore) Init(app modular.Application) error {
	var wdb *WorkflowDatabase
	if err := app.GetService(p.dbServiceName, &wdb); err != nil {
		return fmt.Errorf("persistence: failed to get database service %q: %w", p.dbServiceName, err)
	}

	db, err := wdb.Open()
	if err != nil {
		return fmt.Errorf("persistence: failed to open database: %w", err)
	}
	p.db = db

	if err := p.migrate(); err != nil {
		return fmt.Errorf("persistence: migration failed: %w", err)
	}

	return nil
}

// Start is a no-op; data loading can be triggered explicitly.
func (p *PersistenceStore) Start(ctx context.Context) error {
	return nil
}

// Stop is a no-op; the database lifecycle is owned by WorkflowDatabase.
func (p *PersistenceStore) Stop(ctx context.Context) error {
	return nil
}

// ProvidesServices returns services provided by this module.
func (p *PersistenceStore) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        p.name,
			Description: "Persistence Store",
			Instance:    p,
		},
	}
}

// RequiresServices returns services required by this module.
func (p *PersistenceStore) RequiresServices() []modular.ServiceDependency {
	return []modular.ServiceDependency{
		{
			Name:     p.dbServiceName,
			Required: true,
		},
	}
}

// migrate creates the required tables if they don't already exist.
func (p *PersistenceStore) migrate() error {
	// Enable WAL mode for concurrent read/write performance
	if _, err := p.db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Set busy timeout to avoid SQLITE_BUSY errors during concurrent access
	if _, err := p.db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		return fmt.Errorf("failed to set busy timeout: %w", err)
	}

	statements := []string{
		`CREATE TABLE IF NOT EXISTS workflow_instances (
			id TEXT PRIMARY KEY,
			workflow_type TEXT NOT NULL,
			current_state TEXT NOT NULL,
			previous_state TEXT DEFAULT '',
			data TEXT DEFAULT '{}',
			start_time TEXT NOT NULL,
			last_updated TEXT NOT NULL,
			completed INTEGER DEFAULT 0,
			error_msg TEXT DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS resources (
			resource_type TEXT NOT NULL,
			id TEXT NOT NULL,
			data TEXT NOT NULL,
			state TEXT DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (resource_type, id)
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			name TEXT DEFAULT '',
			password_hash TEXT NOT NULL,
			metadata TEXT DEFAULT '{}',
			created_at TEXT NOT NULL
		)`,
		// Indexes for query performance
		`CREATE INDEX IF NOT EXISTS idx_instances_type ON workflow_instances(workflow_type)`,
		`CREATE INDEX IF NOT EXISTS idx_instances_state ON workflow_instances(current_state)`,
		`CREATE INDEX IF NOT EXISTS idx_instances_completed ON workflow_instances(completed)`,
		`CREATE INDEX IF NOT EXISTS idx_resources_type ON resources(resource_type)`,
	}

	for _, stmt := range statements {
		if _, err := p.db.Exec(stmt); err != nil {
			return fmt.Errorf("failed to execute migration: %w", err)
		}
	}

	// Idempotent migration: add metadata column to users if it doesn't exist
	_, _ = p.db.Exec(`ALTER TABLE users ADD COLUMN metadata TEXT DEFAULT '{}'`)

	return nil
}

// Ping verifies the database connection is alive.
func (p *PersistenceStore) Ping(ctx context.Context) error {
	if p.db == nil {
		return fmt.Errorf("database not initialized")
	}
	return p.db.PingContext(ctx)
}

// SetDB sets the underlying database connection directly (useful for testing).
func (p *PersistenceStore) SetDB(db *sql.DB) {
	p.db = db
}

// SetEncryptor sets a custom field encryptor (useful for testing).
func (p *PersistenceStore) SetEncryptor(enc *FieldEncryptor) {
	p.encryptor = enc
}

// SaveWorkflowInstance upserts a workflow instance. PII fields within instance
// data are encrypted before writing to SQLite when ENCRYPTION_KEY is set.
func (p *PersistenceStore) SaveWorkflowInstance(instance *WorkflowInstance) error {
	dataToStore := instance.Data
	if p.encryptor != nil && p.encryptor.Enabled() && dataToStore != nil {
		encrypted, err := p.encryptor.EncryptPIIFields(dataToStore)
		if err != nil {
			return fmt.Errorf("failed to encrypt instance PII: %w", err)
		}
		dataToStore = encrypted
	}

	dataJSON, err := json.Marshal(dataToStore)
	if err != nil {
		return fmt.Errorf("failed to marshal instance data: %w", err)
	}

	completed := 0
	if instance.Completed {
		completed = 1
	}

	_, err = p.db.Exec(`INSERT INTO workflow_instances (id, workflow_type, current_state, previous_state, data, start_time, last_updated, completed, error_msg)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			workflow_type = excluded.workflow_type,
			current_state = excluded.current_state,
			previous_state = excluded.previous_state,
			data = excluded.data,
			last_updated = excluded.last_updated,
			completed = excluded.completed,
			error_msg = excluded.error_msg`,
		instance.ID,
		instance.WorkflowType,
		instance.CurrentState,
		instance.PreviousState,
		string(dataJSON),
		instance.StartTime.Format(time.RFC3339Nano),
		instance.LastUpdated.Format(time.RFC3339Nano),
		completed,
		instance.Error,
	)
	return err
}

// LoadWorkflowInstances loads all instances for a given workflow type.
func (p *PersistenceStore) LoadWorkflowInstances(workflowType string) ([]*WorkflowInstance, error) {
	rows, err := p.db.Query(
		`SELECT id, workflow_type, current_state, previous_state, data, start_time, last_updated, completed, error_msg
		FROM workflow_instances WHERE workflow_type = ?`, workflowType)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var instances []*WorkflowInstance
	for rows.Next() {
		inst, err := scanWorkflowInstance(rows)
		if err != nil {
			return nil, err
		}

		// Decrypt PII fields after loading
		if p.encryptor != nil && p.encryptor.Enabled() && inst.Data != nil {
			decrypted, decErr := p.encryptor.DecryptPIIFields(inst.Data)
			if decErr != nil {
				return nil, fmt.Errorf("failed to decrypt instance PII for %s: %w", inst.ID, decErr)
			}
			inst.Data = decrypted
		}

		instances = append(instances, inst)
	}
	return instances, rows.Err()
}

func scanWorkflowInstance(rows *sql.Rows) (*WorkflowInstance, error) {
	var inst WorkflowInstance
	var dataJSON, startStr, updatedStr string
	var completed int

	if err := rows.Scan(
		&inst.ID,
		&inst.WorkflowType,
		&inst.CurrentState,
		&inst.PreviousState,
		&dataJSON,
		&startStr,
		&updatedStr,
		&completed,
		&inst.Error,
	); err != nil {
		return nil, err
	}

	inst.Completed = completed != 0

	if err := json.Unmarshal([]byte(dataJSON), &inst.Data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal instance data: %w", err)
	}

	var err error
	inst.StartTime, err = time.Parse(time.RFC3339Nano, startStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse start_time: %w", err)
	}
	inst.LastUpdated, err = time.Parse(time.RFC3339Nano, updatedStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse last_updated: %w", err)
	}

	return &inst, nil
}

// SaveResource upserts a resource. PII fields within the data map are encrypted
// before writing to SQLite when ENCRYPTION_KEY is set.
func (p *PersistenceStore) SaveResource(resourceType, id string, data map[string]any) error {
	// Encrypt PII fields before persisting
	dataToStore := data
	if p.encryptor != nil && p.encryptor.Enabled() {
		encrypted, err := p.encryptor.EncryptPIIFields(data)
		if err != nil {
			return fmt.Errorf("failed to encrypt resource PII: %w", err)
		}
		dataToStore = encrypted
	}

	dataJSON, err := json.Marshal(dataToStore)
	if err != nil {
		return fmt.Errorf("failed to marshal resource data: %w", err)
	}

	now := time.Now().Format(time.RFC3339Nano)
	state := ""
	if s, ok := data["state"].(string); ok {
		state = s
	}

	_, err = p.db.Exec(`INSERT INTO resources (resource_type, id, data, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(resource_type, id) DO UPDATE SET
			data = excluded.data,
			state = excluded.state,
			updated_at = excluded.updated_at`,
		resourceType, id, string(dataJSON), state, now, now,
	)
	return err
}

// LoadResources loads all resources for a given type, keyed by ID.
// Encrypted PII fields are decrypted transparently on read.
func (p *PersistenceStore) LoadResources(resourceType string) (map[string]map[string]any, error) {
	rows, err := p.db.Query(
		`SELECT id, data FROM resources WHERE resource_type = ?`, resourceType)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]map[string]any)
	for rows.Next() {
		var id, dataJSON string
		if err := rows.Scan(&id, &dataJSON); err != nil {
			return nil, err
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
			return nil, fmt.Errorf("failed to unmarshal resource data for %s: %w", id, err)
		}

		// Decrypt PII fields after loading
		if p.encryptor != nil && p.encryptor.Enabled() {
			decrypted, err := p.encryptor.DecryptPIIFields(data)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt resource PII for %s: %w", id, err)
			}
			data = decrypted
		}

		result[id] = data
	}
	return result, rows.Err()
}

// LoadResource loads a single resource by type and ID.
// Returns nil, nil if the resource does not exist.
func (p *PersistenceStore) LoadResource(resourceType, id string) (map[string]any, error) {
	var dataJSON string
	err := p.db.QueryRow(
		`SELECT data FROM resources WHERE resource_type = ? AND id = ?`,
		resourceType, id,
	).Scan(&dataJSON)
	if err != nil {
		return nil, nil //nolint:nilerr // treat lookup errors as absent
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resource data for %s: %w", id, err)
	}

	if p.encryptor != nil && p.encryptor.Enabled() {
		decrypted, err := p.encryptor.DecryptPIIFields(data)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt resource PII for %s: %w", id, err)
		}
		data = decrypted
	}

	return data, nil
}

// SaveUser upserts a user record. PII fields (name, email) are encrypted
// before writing to SQLite when ENCRYPTION_KEY is set.
func (p *PersistenceStore) SaveUser(user UserRecord) error {
	metadataJSON := "{}"
	if user.Metadata != nil {
		b, err := json.Marshal(user.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal user metadata: %w", err)
		}
		metadataJSON = string(b)
	}

	// Encrypt PII fields
	emailToStore := user.Email
	nameToStore := user.Name
	if p.encryptor != nil && p.encryptor.Enabled() {
		var err error
		emailToStore, err = p.encryptor.EncryptValue(user.Email)
		if err != nil {
			return fmt.Errorf("failed to encrypt user email: %w", err)
		}
		nameToStore, err = p.encryptor.EncryptValue(user.Name)
		if err != nil {
			return fmt.Errorf("failed to encrypt user name: %w", err)
		}
	}

	_, err := p.db.Exec(`INSERT INTO users (id, email, name, password_hash, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			email = excluded.email,
			name = excluded.name,
			password_hash = excluded.password_hash,
			metadata = excluded.metadata`,
		user.ID, emailToStore, nameToStore, user.PasswordHash, metadataJSON, user.CreatedAt.Format(time.RFC3339Nano),
	)
	return err
}

// DeleteResource deletes a resource by type and ID.
func (p *PersistenceStore) DeleteResource(resourceType, id string) error {
	_, err := p.db.Exec(`DELETE FROM resources WHERE resource_type = ? AND id = ?`, resourceType, id)
	return err
}

// LoadUsers loads all user records. Encrypted PII fields (name, email) are
// decrypted transparently on read.
func (p *PersistenceStore) LoadUsers() ([]UserRecord, error) {
	rows, err := p.db.Query(`SELECT id, email, name, password_hash, COALESCE(metadata, '{}'), created_at FROM users`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var users []UserRecord
	for rows.Next() {
		var u UserRecord
		var createdStr, metadataJSON string
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &metadataJSON, &createdStr); err != nil {
			return nil, err
		}
		u.CreatedAt, err = time.Parse(time.RFC3339Nano, createdStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse created_at for user %s: %w", u.ID, err)
		}
		if metadataJSON != "" && metadataJSON != "{}" {
			if err := json.Unmarshal([]byte(metadataJSON), &u.Metadata); err != nil {
				return nil, fmt.Errorf("failed to unmarshal metadata for user %s: %w", u.ID, err)
			}
		}

		// Decrypt PII fields after loading
		if p.encryptor != nil && p.encryptor.Enabled() {
			u.Email, err = p.encryptor.DecryptValue(u.Email)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt email for user %s: %w", u.ID, err)
			}
			u.Name, err = p.encryptor.DecryptValue(u.Name)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt name for user %s: %w", u.ID, err)
			}
		}

		users = append(users, u)
	}
	return users, rows.Err()
}
