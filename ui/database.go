package ui

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// DatabaseService handles database operations for the UI
type DatabaseService struct {
	db *sql.DB
}

// NewDatabaseService creates a new database service
func NewDatabaseService(db *sql.DB) *DatabaseService {
	return &DatabaseService{db: db}
}

// InitializeSchema creates the required database tables
func (s *DatabaseService) InitializeSchema(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS tenants (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			description TEXT,
			active BOOLEAN DEFAULT TRUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			username TEXT NOT NULL,
			email TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'user',
			active BOOLEAN DEFAULT TRUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (tenant_id) REFERENCES tenants(id),
			UNIQUE(tenant_id, username)
		)`,
		`CREATE TABLE IF NOT EXISTS workflows (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT,
			config_yaml TEXT NOT NULL,
			status TEXT DEFAULT 'stopped',
			active BOOLEAN DEFAULT TRUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (tenant_id) REFERENCES tenants(id),
			FOREIGN KEY (user_id) REFERENCES users(id),
			UNIQUE(tenant_id, name)
		)`,
		`CREATE TABLE IF NOT EXISTS workflow_executions (
			id TEXT PRIMARY KEY,
			workflow_id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			status TEXT DEFAULT 'running',
			input_data TEXT,
			output_data TEXT,
			logs TEXT,
			error_message TEXT,
			started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			ended_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (workflow_id) REFERENCES workflows(id),
			FOREIGN KEY (tenant_id) REFERENCES tenants(id),
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_users_tenant_id ON users(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_workflows_tenant_id ON workflows(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_executions_workflow_id ON workflow_executions(workflow_id)`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_executions_tenant_id ON workflow_executions(tenant_id)`,
	}

	for _, query := range queries {
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to execute schema query: %w", err)
		}
	}

	// Create default tenant if none exists
	if err := s.createDefaultTenant(ctx); err != nil {
		return fmt.Errorf("failed to create default tenant: %w", err)
	}

	return nil
}

// createDefaultTenant creates a default tenant if none exists
func (s *DatabaseService) createDefaultTenant(ctx context.Context) error {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tenants").Scan(&count)
	if err != nil {
		return err
	}

	if count == 0 {
		defaultTenant := &Tenant{
			ID:          uuid.New(),
			Name:        "default",
			Description: "Default tenant",
			Active:      true,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO tenants (id, name, description, active, created_at, updated_at) 
			 VALUES (?, ?, ?, ?, ?, ?)`,
			defaultTenant.ID.String(), defaultTenant.Name, defaultTenant.Description,
			defaultTenant.Active, defaultTenant.CreatedAt, defaultTenant.UpdatedAt)
		if err != nil {
			return err
		}

		// Create default admin user
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
		if err != nil {
			return err
		}

		defaultUser := &User{
			ID:        uuid.New(),
			TenantID:  defaultTenant.ID,
			Username:  "admin",
			Email:     "admin@example.com",
			Password:  string(hashedPassword),
			Role:      "admin",
			Active:    true,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		_, err = s.db.ExecContext(ctx,
			`INSERT INTO users (id, tenant_id, username, email, password_hash, role, active, created_at, updated_at) 
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			defaultUser.ID.String(), defaultUser.TenantID.String(), defaultUser.Username,
			defaultUser.Email, defaultUser.Password, defaultUser.Role, defaultUser.Active,
			defaultUser.CreatedAt, defaultUser.UpdatedAt)
		return err
	}

	return nil
}

// CreateUser creates a new user
func (s *DatabaseService) CreateUser(ctx context.Context, req *CreateUserRequest) (*User, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		return nil, fmt.Errorf("invalid tenant ID: %w", err)
	}

	user := &User{
		ID:        uuid.New(),
		TenantID:  tenantID,
		Username:  req.Username,
		Email:     req.Email,
		Password:  string(hashedPassword),
		Role:      req.Role,
		Active:    true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO users (id, tenant_id, username, email, password_hash, role, active, created_at, updated_at) 
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID.String(), user.TenantID.String(), user.Username, user.Email,
		user.Password, user.Role, user.Active, user.CreatedAt, user.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return user, nil
}

// AuthenticateUser validates user credentials and returns user info
func (s *DatabaseService) AuthenticateUser(ctx context.Context, username, password string) (*User, *Tenant, error) {
	var user User
	var tenant Tenant

	query := `
		SELECT u.id, u.tenant_id, u.username, u.email, u.password_hash, u.role, u.active, u.created_at, u.updated_at,
		       t.id, t.name, t.description, t.active, t.created_at, t.updated_at
		FROM users u
		JOIN tenants t ON u.tenant_id = t.id
		WHERE u.username = ? AND u.active = 1 AND t.active = 1`

	row := s.db.QueryRowContext(ctx, query, username)
	err := row.Scan(
		&user.ID, &user.TenantID, &user.Username, &user.Email, &user.Password, &user.Role, &user.Active, &user.CreatedAt, &user.UpdatedAt,
		&tenant.ID, &tenant.Name, &tenant.Description, &tenant.Active, &tenant.CreatedAt, &tenant.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, fmt.Errorf("invalid credentials")
		}
		return nil, nil, fmt.Errorf("failed to query user: %w", err)
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil, nil, fmt.Errorf("invalid credentials")
	}

	return &user, &tenant, nil
}

// CreateWorkflow creates a new workflow
func (s *DatabaseService) CreateWorkflow(ctx context.Context, userID, tenantID uuid.UUID, req *CreateWorkflowRequest) (*StoredWorkflow, error) {
	workflow := &StoredWorkflow{
		ID:          uuid.New(),
		TenantID:    tenantID,
		UserID:      userID,
		Name:        req.Name,
		Description: req.Description,
		Config:      req.Config,
		Status:      "stopped",
		Active:      true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO workflows (id, tenant_id, user_id, name, description, config_yaml, status, active, created_at, updated_at) 
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		workflow.ID.String(), workflow.TenantID.String(), workflow.UserID.String(),
		workflow.Name, workflow.Description, workflow.Config, workflow.Status,
		workflow.Active, workflow.CreatedAt, workflow.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow: %w", err)
	}

	return workflow, nil
}

// GetWorkflows retrieves workflows for a tenant
func (s *DatabaseService) GetWorkflows(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*StoredWorkflow, error) {
	query := `
		SELECT id, tenant_id, user_id, name, description, config_yaml, status, active, created_at, updated_at
		FROM workflows
		WHERE tenant_id = ? AND active = 1
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?`

	rows, err := s.db.QueryContext(ctx, query, tenantID.String(), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query workflows: %w", err)
	}
	defer rows.Close()

	var workflows []*StoredWorkflow
	for rows.Next() {
		var workflow StoredWorkflow
		err := rows.Scan(
			&workflow.ID, &workflow.TenantID, &workflow.UserID, &workflow.Name,
			&workflow.Description, &workflow.Config, &workflow.Status,
			&workflow.Active, &workflow.CreatedAt, &workflow.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan workflow: %w", err)
		}
		workflows = append(workflows, &workflow)
	}

	return workflows, nil
}

// GetWorkflow retrieves a specific workflow
func (s *DatabaseService) GetWorkflow(ctx context.Context, workflowID, tenantID uuid.UUID) (*StoredWorkflow, error) {
	var workflow StoredWorkflow
	query := `
		SELECT id, tenant_id, user_id, name, description, config_yaml, status, active, created_at, updated_at
		FROM workflows
		WHERE id = ? AND tenant_id = ? AND active = 1`

	row := s.db.QueryRowContext(ctx, query, workflowID.String(), tenantID.String())
	err := row.Scan(
		&workflow.ID, &workflow.TenantID, &workflow.UserID, &workflow.Name,
		&workflow.Description, &workflow.Config, &workflow.Status,
		&workflow.Active, &workflow.CreatedAt, &workflow.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("workflow not found")
		}
		return nil, fmt.Errorf("failed to query workflow: %w", err)
	}

	return &workflow, nil
}

// UpdateWorkflow updates a workflow
func (s *DatabaseService) UpdateWorkflow(ctx context.Context, workflowID, tenantID uuid.UUID, req *UpdateWorkflowRequest) (*StoredWorkflow, error) {
	// First get the existing workflow
	workflow, err := s.GetWorkflow(ctx, workflowID, tenantID)
	if err != nil {
		return nil, err
	}

	// Update fields that are provided
	if req.Name != "" {
		workflow.Name = req.Name
	}
	if req.Description != "" {
		workflow.Description = req.Description
	}
	if req.Config != "" {
		workflow.Config = req.Config
	}
	if req.Status != "" {
		workflow.Status = req.Status
	}
	workflow.UpdatedAt = time.Now()

	_, err = s.db.ExecContext(ctx,
		`UPDATE workflows SET name = ?, description = ?, config_yaml = ?, status = ?, updated_at = ?
		 WHERE id = ? AND tenant_id = ?`,
		workflow.Name, workflow.Description, workflow.Config, workflow.Status, workflow.UpdatedAt,
		workflowID.String(), tenantID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to update workflow: %w", err)
	}

	return workflow, nil
}

// DeleteWorkflow soft deletes a workflow
func (s *DatabaseService) DeleteWorkflow(ctx context.Context, workflowID, tenantID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE workflows SET active = 0, updated_at = ? WHERE id = ? AND tenant_id = ?`,
		time.Now(), workflowID.String(), tenantID.String())
	if err != nil {
		return fmt.Errorf("failed to delete workflow: %w", err)
	}
	return nil
}

// CreateExecution creates a new workflow execution
func (s *DatabaseService) CreateExecution(ctx context.Context, workflowID, tenantID, userID uuid.UUID, input map[string]interface{}) (*WorkflowExecution, error) {
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal input: %w", err)
	}

	execution := &WorkflowExecution{
		ID:         uuid.New(),
		WorkflowID: workflowID,
		TenantID:   tenantID,
		UserID:     userID,
		Status:     "running",
		Input:      input,
		StartedAt:  time.Now(),
		CreatedAt:  time.Now(),
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO workflow_executions (id, workflow_id, tenant_id, user_id, status, input_data, started_at, created_at) 
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		execution.ID.String(), execution.WorkflowID.String(), execution.TenantID.String(),
		execution.UserID.String(), execution.Status, string(inputJSON),
		execution.StartedAt, execution.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create execution: %w", err)
	}

	return execution, nil
}

// GetExecutions retrieves executions for a workflow
func (s *DatabaseService) GetExecutions(ctx context.Context, workflowID, tenantID uuid.UUID, limit, offset int) ([]*WorkflowExecution, error) {
	query := `
		SELECT id, workflow_id, tenant_id, user_id, status, input_data, output_data, logs, error_message, started_at, ended_at, created_at
		FROM workflow_executions
		WHERE workflow_id = ? AND tenant_id = ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?`

	rows, err := s.db.QueryContext(ctx, query, workflowID.String(), tenantID.String(), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query executions: %w", err)
	}
	defer rows.Close()

	var executions []*WorkflowExecution
	for rows.Next() {
		var execution WorkflowExecution
		var inputJSON, outputJSON, logsJSON sql.NullString
		var endedAt sql.NullTime

		err := rows.Scan(
			&execution.ID, &execution.WorkflowID, &execution.TenantID, &execution.UserID,
			&execution.Status, &inputJSON, &outputJSON, &logsJSON, &execution.Error,
			&execution.StartedAt, &endedAt, &execution.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan execution: %w", err)
		}

		if inputJSON.Valid {
			json.Unmarshal([]byte(inputJSON.String), &execution.Input)
		}
		if outputJSON.Valid {
			json.Unmarshal([]byte(outputJSON.String), &execution.Output)
		}
		if logsJSON.Valid {
			json.Unmarshal([]byte(logsJSON.String), &execution.Logs)
		}
		if endedAt.Valid {
			execution.EndedAt = &endedAt.Time
		}

		executions = append(executions, &execution)
	}

	return executions, nil
}

// UpdateExecution updates a workflow execution
func (s *DatabaseService) UpdateExecution(ctx context.Context, executionID uuid.UUID, status string, output map[string]interface{}, logs []string, errMsg string) error {
	outputJSON, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("failed to marshal output: %w", err)
	}

	logsJSON, err := json.Marshal(logs)
	if err != nil {
		return fmt.Errorf("failed to marshal logs: %w", err)
	}

	var endedAt *time.Time
	if status == "completed" || status == "failed" || status == "stopped" {
		now := time.Now()
		endedAt = &now
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE workflow_executions SET status = ?, output_data = ?, logs = ?, error_message = ?, ended_at = ?
		 WHERE id = ?`,
		status, string(outputJSON), string(logsJSON), errMsg, endedAt, executionID.String())
	if err != nil {
		return fmt.Errorf("failed to update execution: %w", err)
	}

	return nil
}