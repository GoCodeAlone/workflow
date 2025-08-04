package ui

import (
	"time"

	"github.com/google/uuid"
)

// User represents a user in the system
type User struct {
	ID        uuid.UUID `json:"id" db:"id"`
	TenantID  uuid.UUID `json:"tenant_id" db:"tenant_id"`
	Username  string    `json:"username" db:"username"`
	Email     string    `json:"email" db:"email"`
	Password  string    `json:"-" db:"password_hash"` // Never expose password in JSON
	Role      string    `json:"role" db:"role"`       // admin, user, viewer
	Active    bool      `json:"active" db:"active"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// Tenant represents a tenant in the multi-tenant system
type Tenant struct {
	ID          uuid.UUID `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	Active      bool      `json:"active" db:"active"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// StoredWorkflow represents a workflow configuration stored in the database
type StoredWorkflow struct {
	ID          uuid.UUID `json:"id" db:"id"`
	TenantID    uuid.UUID `json:"tenant_id" db:"tenant_id"`
	UserID      uuid.UUID `json:"user_id" db:"user_id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	Config      string    `json:"config" db:"config_yaml"` // YAML configuration
	Status      string    `json:"status" db:"status"`      // active, stopped, error
	Active      bool      `json:"active" db:"active"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// WorkflowExecution represents an execution instance of a workflow
type WorkflowExecution struct {
	ID         uuid.UUID              `json:"id" db:"id"`
	WorkflowID uuid.UUID              `json:"workflow_id" db:"workflow_id"`
	TenantID   uuid.UUID              `json:"tenant_id" db:"tenant_id"`
	UserID     uuid.UUID              `json:"user_id" db:"user_id"`
	Status     string                 `json:"status" db:"status"` // running, completed, failed, stopped
	Input      map[string]interface{} `json:"input" db:"input_data"`
	Output     map[string]interface{} `json:"output" db:"output_data"`
	Logs       []string               `json:"logs" db:"logs"`
	Error      string                 `json:"error,omitempty" db:"error_message"`
	StartedAt  time.Time              `json:"started_at" db:"started_at"`
	EndedAt    *time.Time             `json:"ended_at,omitempty" db:"ended_at"`
	CreatedAt  time.Time              `json:"created_at" db:"created_at"`
}

// WorkflowLog represents individual log entries for workflow executions
type WorkflowLog struct {
	ID          uuid.UUID `json:"id" db:"id"`
	ExecutionID uuid.UUID `json:"execution_id" db:"execution_id"`
	Level       string    `json:"level" db:"level"` // debug, info, warn, error
	Message     string    `json:"message" db:"message"`
	Timestamp   time.Time `json:"timestamp" db:"timestamp"`
}

// CreateUserRequest represents a request to create a new user
type CreateUserRequest struct {
	TenantID string `json:"tenant_id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// LoginRequest represents a login request
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse represents a login response
type LoginResponse struct {
	Token     string    `json:"token"`
	User      User      `json:"user"`
	Tenant    Tenant    `json:"tenant"`
	ExpiresAt time.Time `json:"expires_at"`
}

// CreateWorkflowRequest represents a request to create a new workflow
type CreateWorkflowRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Config      string `json:"config"` // YAML configuration
}

// UpdateWorkflowRequest represents a request to update a workflow
type UpdateWorkflowRequest struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Config      string `json:"config,omitempty"`
	Status      string `json:"status,omitempty"`
}

// ExecuteWorkflowRequest represents a request to execute a workflow
type ExecuteWorkflowRequest struct {
	Input map[string]interface{} `json:"input,omitempty"`
}