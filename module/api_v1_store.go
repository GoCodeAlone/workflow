package module

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// V1Store is a SQLite-backed data store for the v1 API.
type V1Store struct {
	db *sql.DB
}

// OpenV1Store opens (or creates) a SQLite database at dbPath and initializes the schema.
func OpenV1Store(dbPath string) (*V1Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	s := &V1Store{db: db}
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return s, nil
}

// Close closes the database connection.
func (s *V1Store) Close() error {
	return s.db.Close()
}

func (s *V1Store) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS companies (
		id         TEXT PRIMARY KEY,
		name       TEXT NOT NULL,
		slug       TEXT NOT NULL,
		owner_id   TEXT NOT NULL DEFAULT '',
		parent_id  TEXT,
		is_system  INTEGER NOT NULL DEFAULT 0,
		metadata   TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		FOREIGN KEY (parent_id) REFERENCES companies(id)
	);

	CREATE TABLE IF NOT EXISTS projects (
		id         TEXT PRIMARY KEY,
		company_id TEXT NOT NULL,
		name       TEXT NOT NULL,
		slug       TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		is_system  INTEGER NOT NULL DEFAULT 0,
		metadata   TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		FOREIGN KEY (company_id) REFERENCES companies(id)
	);

	CREATE TABLE IF NOT EXISTS workflows (
		id          TEXT PRIMARY KEY,
		project_id  TEXT NOT NULL,
		name        TEXT NOT NULL,
		slug        TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		config_yaml TEXT NOT NULL DEFAULT '',
		version     INTEGER NOT NULL DEFAULT 1,
		status      TEXT NOT NULL DEFAULT 'draft',
		is_system   INTEGER NOT NULL DEFAULT 0,
		created_by  TEXT NOT NULL DEFAULT '',
		updated_by  TEXT NOT NULL DEFAULT '',
		created_at  TEXT NOT NULL,
		updated_at  TEXT NOT NULL,
		FOREIGN KEY (project_id) REFERENCES projects(id)
	);

	CREATE TABLE IF NOT EXISTS workflow_versions (
		id          TEXT PRIMARY KEY,
		workflow_id TEXT NOT NULL,
		version     INTEGER NOT NULL,
		config_yaml TEXT NOT NULL,
		created_by  TEXT NOT NULL DEFAULT '',
		created_at  TEXT NOT NULL,
		FOREIGN KEY (workflow_id) REFERENCES workflows(id) ON DELETE CASCADE
	);
	`
	_, err := s.db.Exec(schema)
	return err
}

// --- Types ---

// V1Company represents a company or organization.
type V1Company struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	OwnerID   string `json:"owner_id"`
	ParentID  string `json:"parent_id,omitempty"`
	IsSystem  bool   `json:"is_system,omitempty"`
	Metadata  string `json:"metadata,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// V1Project represents a project.
type V1Project struct {
	ID          string `json:"id"`
	CompanyID   string `json:"company_id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`
	IsSystem    bool   `json:"is_system,omitempty"`
	Metadata    string `json:"metadata,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// V1Workflow represents a workflow record.
type V1Workflow struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`
	ConfigYAML  string `json:"config_yaml"`
	Version     int    `json:"version"`
	Status      string `json:"status"`
	IsSystem    bool   `json:"is_system,omitempty"`
	CreatedBy   string `json:"created_by"`
	UpdatedBy   string `json:"updated_by"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// V1WorkflowVersion represents a snapshot of a workflow at a specific version.
type V1WorkflowVersion struct {
	ID         string `json:"id"`
	WorkflowID string `json:"workflow_id"`
	Version    int    `json:"version"`
	ConfigYAML string `json:"config_yaml"`
	CreatedBy  string `json:"created_by"`
	CreatedAt  string `json:"created_at"`
}

// --- Helpers ---

func newID() string {
	return uuid.New().String()
}

func nowStr() string {
	return time.Now().UTC().Format(time.RFC3339)
}

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

func toSlug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "untitled"
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- Companies ---

// CreateCompany inserts a new top-level company.
func (s *V1Store) CreateCompany(name, slug, ownerID string) (*V1Company, error) {
	if slug == "" {
		slug = toSlug(name)
	}
	now := nowStr()
	c := &V1Company{
		ID:        newID(),
		Name:      name,
		Slug:      slug,
		OwnerID:   ownerID,
		IsSystem:  false,
		Metadata:  "{}",
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.db.Exec(
		`INSERT INTO companies (id, name, slug, owner_id, parent_id, is_system, metadata, created_at, updated_at)
		 VALUES (?, ?, ?, ?, NULL, 0, '{}', ?, ?)`,
		c.ID, c.Name, c.Slug, c.OwnerID, c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// GetCompany retrieves a company by ID.
func (s *V1Store) GetCompany(id string) (*V1Company, error) {
	c := &V1Company{}
	var parentID sql.NullString
	var isSys int
	err := s.db.QueryRow(
		`SELECT id, name, slug, owner_id, parent_id, is_system, metadata, created_at, updated_at
		 FROM companies WHERE id = ?`, id,
	).Scan(&c.ID, &c.Name, &c.Slug, &c.OwnerID, &parentID, &isSys, &c.Metadata, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.ParentID = parentID.String
	c.IsSystem = isSys == 1
	return c, nil
}

// ListCompanies lists top-level companies (parent_id IS NULL).
func (s *V1Store) ListCompanies(ownerID string) ([]V1Company, error) {
	rows, err := s.db.Query(
		`SELECT id, name, slug, owner_id, parent_id, is_system, metadata, created_at, updated_at
		 FROM companies WHERE parent_id IS NULL ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []V1Company
	for rows.Next() {
		var c V1Company
		var parentID sql.NullString
		var isSys int
		if err := rows.Scan(&c.ID, &c.Name, &c.Slug, &c.OwnerID, &parentID, &isSys, &c.Metadata, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.ParentID = parentID.String
		c.IsSystem = isSys == 1
		result = append(result, c)
	}
	return result, rows.Err()
}

// --- Organizations (companies with a parent_id) ---

// CreateOrganization inserts a child company under a parent company.
func (s *V1Store) CreateOrganization(parentID, name, slug, ownerID string) (*V1Company, error) {
	if slug == "" {
		slug = toSlug(name)
	}
	now := nowStr()
	c := &V1Company{
		ID:        newID(),
		Name:      name,
		Slug:      slug,
		OwnerID:   ownerID,
		ParentID:  parentID,
		IsSystem:  false,
		Metadata:  "{}",
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.db.Exec(
		`INSERT INTO companies (id, name, slug, owner_id, parent_id, is_system, metadata, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 0, '{}', ?, ?)`,
		c.ID, c.Name, c.Slug, c.OwnerID, c.ParentID, c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// ListOrganizations lists child companies under a parent.
func (s *V1Store) ListOrganizations(parentID string) ([]V1Company, error) {
	rows, err := s.db.Query(
		`SELECT id, name, slug, owner_id, parent_id, is_system, metadata, created_at, updated_at
		 FROM companies WHERE parent_id = ? ORDER BY created_at ASC`, parentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []V1Company
	for rows.Next() {
		var c V1Company
		var pid sql.NullString
		var isSys int
		if err := rows.Scan(&c.ID, &c.Name, &c.Slug, &c.OwnerID, &pid, &isSys, &c.Metadata, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.ParentID = pid.String
		c.IsSystem = isSys == 1
		result = append(result, c)
	}
	return result, rows.Err()
}

// --- Projects ---

// CreateProject creates a project under an organization.
func (s *V1Store) CreateProject(companyID, name, slug, description string) (*V1Project, error) {
	if slug == "" {
		slug = toSlug(name)
	}
	now := nowStr()
	p := &V1Project{
		ID:          newID(),
		CompanyID:   companyID,
		Name:        name,
		Slug:        slug,
		Description: description,
		IsSystem:    false,
		Metadata:    "{}",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err := s.db.Exec(
		`INSERT INTO projects (id, company_id, name, slug, description, is_system, metadata, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 0, '{}', ?, ?)`,
		p.ID, p.CompanyID, p.Name, p.Slug, p.Description, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// GetProject retrieves a project by ID.
func (s *V1Store) GetProject(id string) (*V1Project, error) {
	p := &V1Project{}
	var isSys int
	err := s.db.QueryRow(
		`SELECT id, company_id, name, slug, description, is_system, metadata, created_at, updated_at
		 FROM projects WHERE id = ?`, id,
	).Scan(&p.ID, &p.CompanyID, &p.Name, &p.Slug, &p.Description, &isSys, &p.Metadata, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	p.IsSystem = isSys == 1
	return p, nil
}

// ListProjects lists projects for a given organization (company_id).
func (s *V1Store) ListProjects(companyID string) ([]V1Project, error) {
	rows, err := s.db.Query(
		`SELECT id, company_id, name, slug, description, is_system, metadata, created_at, updated_at
		 FROM projects WHERE company_id = ? ORDER BY created_at ASC`, companyID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []V1Project
	for rows.Next() {
		var p V1Project
		var isSys int
		if err := rows.Scan(&p.ID, &p.CompanyID, &p.Name, &p.Slug, &p.Description, &isSys, &p.Metadata, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.IsSystem = isSys == 1
		result = append(result, p)
	}
	return result, rows.Err()
}

// --- Workflows ---

// CreateWorkflow creates a workflow under a project.
func (s *V1Store) CreateWorkflow(projectID, name, slug, description, configYAML, createdBy string) (*V1Workflow, error) {
	if slug == "" {
		slug = toSlug(name)
	}
	now := nowStr()
	w := &V1Workflow{
		ID:          newID(),
		ProjectID:   projectID,
		Name:        name,
		Slug:        slug,
		Description: description,
		ConfigYAML:  configYAML,
		Version:     1,
		Status:      "draft",
		IsSystem:    false,
		CreatedBy:   createdBy,
		UpdatedBy:   createdBy,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err := s.db.Exec(
		`INSERT INTO workflows (id, project_id, name, slug, description, config_yaml, version, status, is_system, created_by, updated_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?)`,
		w.ID, w.ProjectID, w.Name, w.Slug, w.Description, w.ConfigYAML, w.Version, w.Status, w.CreatedBy, w.UpdatedBy, w.CreatedAt, w.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return w, nil
}

// GetWorkflow retrieves a workflow by ID.
func (s *V1Store) GetWorkflow(id string) (*V1Workflow, error) {
	w := &V1Workflow{}
	var isSys int
	err := s.db.QueryRow(
		`SELECT id, project_id, name, slug, description, config_yaml, version, status, is_system, created_by, updated_by, created_at, updated_at
		 FROM workflows WHERE id = ?`, id,
	).Scan(&w.ID, &w.ProjectID, &w.Name, &w.Slug, &w.Description, &w.ConfigYAML, &w.Version, &w.Status, &isSys, &w.CreatedBy, &w.UpdatedBy, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return nil, err
	}
	w.IsSystem = isSys == 1
	return w, nil
}

// UpdateWorkflow updates a workflow's fields and auto-increments version.
// If config_yaml changed, a version snapshot is saved.
func (s *V1Store) UpdateWorkflow(id string, name, description, configYAML, updatedBy string) (*V1Workflow, error) {
	w, err := s.GetWorkflow(id)
	if err != nil {
		return nil, err
	}

	configChanged := configYAML != "" && configYAML != w.ConfigYAML
	if name != "" {
		w.Name = name
		w.Slug = toSlug(name)
	}
	if description != "" {
		w.Description = description
	}
	if configYAML != "" {
		w.ConfigYAML = configYAML
	}
	if configChanged {
		w.Version++
	}
	w.UpdatedBy = updatedBy
	w.UpdatedAt = nowStr()

	_, err = s.db.Exec(
		`UPDATE workflows SET name=?, slug=?, description=?, config_yaml=?, version=?, updated_by=?, updated_at=?
		 WHERE id=?`,
		w.Name, w.Slug, w.Description, w.ConfigYAML, w.Version, w.UpdatedBy, w.UpdatedAt, w.ID,
	)
	if err != nil {
		return nil, err
	}

	// Save version snapshot
	if configChanged {
		if err := s.SaveVersion(w.ID, w.ConfigYAML, updatedBy); err != nil {
			return nil, err
		}
	}

	return w, nil
}

// DeleteWorkflow deletes a workflow by ID. Returns an error if the workflow is a system workflow.
func (s *V1Store) DeleteWorkflow(id string) error {
	w, err := s.GetWorkflow(id)
	if err != nil {
		return err
	}
	if w.IsSystem {
		return fmt.Errorf("cannot delete system workflow")
	}
	_, err = s.db.Exec(`DELETE FROM workflows WHERE id = ?`, id)
	return err
}

// ListWorkflows lists workflows for a project. If projectID is empty, lists all.
func (s *V1Store) ListWorkflows(projectID string) ([]V1Workflow, error) {
	var rows *sql.Rows
	var err error
	if projectID != "" {
		rows, err = s.db.Query(
			`SELECT id, project_id, name, slug, description, config_yaml, version, status, is_system, created_by, updated_by, created_at, updated_at
			 FROM workflows WHERE project_id = ? ORDER BY created_at DESC`, projectID,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, project_id, name, slug, description, config_yaml, version, status, is_system, created_by, updated_by, created_at, updated_at
			 FROM workflows ORDER BY created_at DESC`,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []V1Workflow
	for rows.Next() {
		var w V1Workflow
		var isSys int
		if err := rows.Scan(&w.ID, &w.ProjectID, &w.Name, &w.Slug, &w.Description, &w.ConfigYAML, &w.Version, &w.Status, &isSys, &w.CreatedBy, &w.UpdatedBy, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		w.IsSystem = isSys == 1
		result = append(result, w)
	}
	return result, rows.Err()
}

// SetWorkflowStatus updates a workflow's status field.
func (s *V1Store) SetWorkflowStatus(id, status string) (*V1Workflow, error) {
	now := nowStr()
	_, err := s.db.Exec(`UPDATE workflows SET status=?, updated_at=? WHERE id=?`, status, now, id)
	if err != nil {
		return nil, err
	}
	return s.GetWorkflow(id)
}

// --- Versions ---

// SaveVersion stores a version snapshot.
func (s *V1Store) SaveVersion(workflowID, configYAML, createdBy string) error {
	// Get the current version number from the workflow
	var version int
	err := s.db.QueryRow(`SELECT version FROM workflows WHERE id = ?`, workflowID).Scan(&version)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO workflow_versions (id, workflow_id, version, config_yaml, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		newID(), workflowID, version, configYAML, createdBy, nowStr(),
	)
	return err
}

// ListVersions returns version history for a workflow.
func (s *V1Store) ListVersions(workflowID string) ([]V1WorkflowVersion, error) {
	rows, err := s.db.Query(
		`SELECT id, workflow_id, version, config_yaml, created_by, created_at
		 FROM workflow_versions WHERE workflow_id = ? ORDER BY version DESC`, workflowID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []V1WorkflowVersion
	for rows.Next() {
		var v V1WorkflowVersion
		if err := rows.Scan(&v.ID, &v.WorkflowID, &v.Version, &v.ConfigYAML, &v.CreatedBy, &v.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

// GetVersion retrieves a specific version of a workflow.
func (s *V1Store) GetVersion(workflowID string, version int) (*V1WorkflowVersion, error) {
	v := &V1WorkflowVersion{}
	err := s.db.QueryRow(
		`SELECT id, workflow_id, version, config_yaml, created_by, created_at
		 FROM workflow_versions WHERE workflow_id = ? AND version = ?`, workflowID, version,
	).Scan(&v.ID, &v.WorkflowID, &v.Version, &v.ConfigYAML, &v.CreatedBy, &v.CreatedAt)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// --- System Hierarchy ---

// GetSystemWorkflow returns the system workflow if it exists.
func (s *V1Store) GetSystemWorkflow() (*V1Workflow, error) {
	w := &V1Workflow{}
	var isSys int
	err := s.db.QueryRow(
		`SELECT id, project_id, name, slug, description, config_yaml, version, status, is_system, created_by, updated_by, created_at, updated_at
		 FROM workflows WHERE is_system = 1 LIMIT 1`,
	).Scan(&w.ID, &w.ProjectID, &w.Name, &w.Slug, &w.Description, &w.ConfigYAML, &w.Version, &w.Status, &isSys, &w.CreatedBy, &w.UpdatedBy, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return nil, err
	}
	w.IsSystem = true
	return w, nil
}

// ResetSystemWorkflow resets the system workflow config to the given YAML,
// incrementing the version and saving a version snapshot.
func (s *V1Store) ResetSystemWorkflow(configYAML string) error {
	w, err := s.GetSystemWorkflow()
	if err != nil {
		return fmt.Errorf("no system workflow found: %w", err)
	}
	w.Version++
	w.ConfigYAML = configYAML
	w.UpdatedAt = nowStr()
	w.UpdatedBy = "system"

	_, err = s.db.Exec(
		`UPDATE workflows SET config_yaml=?, version=?, updated_by=?, updated_at=? WHERE id=?`,
		w.ConfigYAML, w.Version, w.UpdatedBy, w.UpdatedAt, w.ID,
	)
	if err != nil {
		return err
	}

	return s.SaveVersion(w.ID, configYAML, "system")
}

// EnsureSystemHierarchy creates the system company, organization, project, and
// admin workflow if they don't already exist. Returns the IDs of all created entities.
func (s *V1Store) EnsureSystemHierarchy(ownerID, adminConfigYAML string) (companyID, orgID, projectID, workflowID string, err error) {
	// Check if system hierarchy already exists
	existing, sysErr := s.GetSystemWorkflow()
	if sysErr == nil && existing != nil {
		// Already exists — return existing IDs
		proj, _ := s.GetProject(existing.ProjectID)
		if proj != nil {
			org, _ := s.GetCompany(proj.CompanyID)
			if org != nil {
				return org.ParentID, org.ID, proj.ID, existing.ID, nil
			}
		}
		return "", "", "", existing.ID, nil
	}

	now := nowStr()

	// Create system company
	companyID = newID()
	_, err = s.db.Exec(
		`INSERT INTO companies (id, name, slug, owner_id, parent_id, is_system, metadata, created_at, updated_at)
		 VALUES (?, 'System', 'system', ?, NULL, 1, '{}', ?, ?)`,
		companyID, ownerID, now, now,
	)
	if err != nil {
		return "", "", "", "", fmt.Errorf("create system company: %w", err)
	}

	// Create system organization under system company
	orgID = newID()
	_, err = s.db.Exec(
		`INSERT INTO companies (id, name, slug, owner_id, parent_id, is_system, metadata, created_at, updated_at)
		 VALUES (?, 'Administration', 'administration', ?, ?, 1, '{}', ?, ?)`,
		orgID, ownerID, companyID, now, now,
	)
	if err != nil {
		return "", "", "", "", fmt.Errorf("create system org: %w", err)
	}

	// Create system project
	projectID = newID()
	_, err = s.db.Exec(
		`INSERT INTO projects (id, company_id, name, slug, description, is_system, metadata, created_at, updated_at)
		 VALUES (?, ?, 'System Administration', 'system-administration', 'System administration workflows', 1, '{}', ?, ?)`,
		projectID, orgID, now, now,
	)
	if err != nil {
		return "", "", "", "", fmt.Errorf("create system project: %w", err)
	}

	// Create system workflow
	workflowID = newID()
	_, err = s.db.Exec(
		`INSERT INTO workflows (id, project_id, name, slug, description, config_yaml, version, status, is_system, created_by, updated_by, created_at, updated_at)
		 VALUES (?, ?, 'Admin Configuration', 'admin-configuration', 'System administration config', ?, 1, 'active', 1, ?, ?, ?, ?)`,
		workflowID, projectID, adminConfigYAML, ownerID, ownerID, now, now,
	)
	if err != nil {
		return "", "", "", "", fmt.Errorf("create system workflow: %w", err)
	}

	// Save initial version
	_, err = s.db.Exec(
		`INSERT INTO workflow_versions (id, workflow_id, version, config_yaml, created_by, created_at)
		 VALUES (?, ?, 1, ?, ?, ?)`,
		newID(), workflowID, adminConfigYAML, ownerID, now,
	)
	if err != nil {
		return "", "", "", "", fmt.Errorf("save initial version: %w", err)
	}

	return companyID, orgID, projectID, workflowID, nil
}
