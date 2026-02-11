package store

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Role represents a membership role within a company or project.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleEditor Role = "editor"
	RoleViewer Role = "viewer"
)

// ValidRoles is the set of valid role values.
var ValidRoles = map[Role]bool{
	RoleOwner:  true,
	RoleAdmin:  true,
	RoleEditor: true,
	RoleViewer: true,
}

// WorkflowStatus represents the lifecycle status of a workflow record.
type WorkflowStatus string

const (
	WorkflowStatusDraft   WorkflowStatus = "draft"
	WorkflowStatusActive  WorkflowStatus = "active"
	WorkflowStatusStopped WorkflowStatus = "stopped"
	WorkflowStatusError   WorkflowStatus = "error"
)

// ValidWorkflowStatuses is the set of valid workflow status values.
var ValidWorkflowStatuses = map[WorkflowStatus]bool{
	WorkflowStatusDraft:   true,
	WorkflowStatusActive:  true,
	WorkflowStatusStopped: true,
	WorkflowStatusError:   true,
}

// OAuthProvider represents the provider for an OAuth connection.
type OAuthProvider string

const (
	OAuthProviderGitHub OAuthProvider = "github"
	OAuthProviderGoogle OAuthProvider = "google"
)

// User represents a platform user.
type User struct {
	ID            uuid.UUID       `json:"id"`
	Email         string          `json:"email"`
	PasswordHash  string          `json:"-"`
	DisplayName   string          `json:"display_name"`
	AvatarURL     string          `json:"avatar_url,omitempty"`
	OAuthProvider OAuthProvider   `json:"oauth_provider,omitempty"`
	OAuthID       string          `json:"oauth_id,omitempty"`
	Active        bool            `json:"active"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	LastLoginAt   *time.Time      `json:"last_login_at,omitempty"`
}

// Company represents a top-level organization or company.
type Company struct {
	ID        uuid.UUID       `json:"id"`
	Name      string          `json:"name"`
	Slug      string          `json:"slug"`
	OwnerID   uuid.UUID       `json:"owner_id"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Organization is an alias for Company for code clarity where "organization"
// terminology is preferred.
type Organization = Company

// Project represents a project within a company.
type Project struct {
	ID          uuid.UUID       `json:"id"`
	CompanyID   uuid.UUID       `json:"company_id"`
	Name        string          `json:"name"`
	Slug        string          `json:"slug"`
	Description string          `json:"description,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// Membership represents a user's role within a company or project.
type Membership struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	CompanyID uuid.UUID  `json:"company_id"`
	ProjectID *uuid.UUID `json:"project_id,omitempty"` // nil means company-level membership
	Role      Role       `json:"role"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// WorkflowRecord represents a stored workflow configuration with version tracking.
type WorkflowRecord struct {
	ID          uuid.UUID      `json:"id"`
	ProjectID   uuid.UUID      `json:"project_id"`
	Name        string         `json:"name"`
	Slug        string         `json:"slug"`
	Description string         `json:"description,omitempty"`
	ConfigYAML  string         `json:"config_yaml"`
	Version     int            `json:"version"`
	Status      WorkflowStatus `json:"status"`
	CreatedBy   uuid.UUID      `json:"created_by"`
	UpdatedBy   uuid.UUID      `json:"updated_by"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// CrossWorkflowLink represents a directed link between two workflows.
type CrossWorkflowLink struct {
	ID               uuid.UUID       `json:"id"`
	SourceWorkflowID uuid.UUID       `json:"source_workflow_id"`
	TargetWorkflowID uuid.UUID       `json:"target_workflow_id"`
	LinkType         string          `json:"link_type"`
	Config           json.RawMessage `json:"config,omitempty"`
	CreatedBy        uuid.UUID       `json:"created_by"`
	CreatedAt        time.Time       `json:"created_at"`
}

// Session represents an active user session.
type Session struct {
	ID        uuid.UUID       `json:"id"`
	UserID    uuid.UUID       `json:"user_id"`
	Token     string          `json:"-"`
	IPAddress string          `json:"ip_address"`
	UserAgent string          `json:"user_agent"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	Active    bool            `json:"active"`
	CreatedAt time.Time       `json:"created_at"`
	ExpiresAt time.Time       `json:"expires_at"`
}

// --- Execution Tracking ---

// ExecutionStatus represents the status of a workflow execution.
type ExecutionStatus string

const (
	ExecutionStatusPending   ExecutionStatus = "pending"
	ExecutionStatusRunning   ExecutionStatus = "running"
	ExecutionStatusCompleted ExecutionStatus = "completed"
	ExecutionStatusFailed    ExecutionStatus = "failed"
	ExecutionStatusCancelled ExecutionStatus = "cancelled"
)

// StepStatus represents the status of an execution step.
type StepStatus string

const (
	StepStatusPending   StepStatus = "pending"
	StepStatusRunning   StepStatus = "running"
	StepStatusCompleted StepStatus = "completed"
	StepStatusFailed    StepStatus = "failed"
	StepStatusSkipped   StepStatus = "skipped"
)

// WorkflowExecution represents a single execution of a workflow.
type WorkflowExecution struct {
	ID           uuid.UUID       `json:"id"`
	WorkflowID   uuid.UUID       `json:"workflow_id"`
	TriggerType  string          `json:"trigger_type"`
	TriggerData  json.RawMessage `json:"trigger_data,omitempty"`
	Status       ExecutionStatus `json:"status"`
	OutputData   json.RawMessage `json:"output_data,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
	ErrorStack   string          `json:"error_stack,omitempty"`
	StartedAt    time.Time       `json:"started_at"`
	CompletedAt  *time.Time      `json:"completed_at,omitempty"`
	DurationMs   *int64          `json:"duration_ms,omitempty"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
}

// ExecutionStep represents a single step within a workflow execution.
type ExecutionStep struct {
	ID           uuid.UUID       `json:"id"`
	ExecutionID  uuid.UUID       `json:"execution_id"`
	StepName     string          `json:"step_name"`
	StepType     string          `json:"step_type"`
	InputData    json.RawMessage `json:"input_data,omitempty"`
	OutputData   json.RawMessage `json:"output_data,omitempty"`
	Status       StepStatus      `json:"status"`
	ErrorMessage string          `json:"error_message,omitempty"`
	StartedAt    *time.Time      `json:"started_at,omitempty"`
	CompletedAt  *time.Time      `json:"completed_at,omitempty"`
	DurationMs   *int64          `json:"duration_ms,omitempty"`
	SequenceNum  int             `json:"sequence_num"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
}

// --- Logging ---

// LogLevel represents a log severity level.
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
	LogLevelFatal LogLevel = "fatal"
)

// ExecutionLog represents a log entry for a workflow execution.
type ExecutionLog struct {
	ID          int64           `json:"id"`
	WorkflowID  uuid.UUID       `json:"workflow_id"`
	ExecutionID *uuid.UUID      `json:"execution_id,omitempty"`
	Level       LogLevel        `json:"level"`
	Message     string          `json:"message"`
	ModuleName  string          `json:"module_name,omitempty"`
	Fields      json.RawMessage `json:"fields,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

// --- Audit ---

// AuditEntry represents an entry in the audit log.
type AuditEntry struct {
	ID           int64           `json:"id"`
	UserID       *uuid.UUID      `json:"user_id,omitempty"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   *uuid.UUID      `json:"resource_id,omitempty"`
	Details      json.RawMessage `json:"details,omitempty"`
	IPAddress    string          `json:"ip_address,omitempty"`
	UserAgent    string          `json:"user_agent,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

// --- IAM ---

// IAMProviderType represents the type of an IAM provider.
type IAMProviderType string

const (
	IAMProviderAWS        IAMProviderType = "aws_iam"
	IAMProviderKubernetes IAMProviderType = "kubernetes"
	IAMProviderOIDC       IAMProviderType = "oidc"
	IAMProviderSAML       IAMProviderType = "saml"
	IAMProviderLDAP       IAMProviderType = "ldap"
	IAMProviderCustom     IAMProviderType = "custom"
)

// IAMProviderConfig represents a configured IAM provider for a company.
type IAMProviderConfig struct {
	ID           uuid.UUID       `json:"id"`
	CompanyID    uuid.UUID       `json:"company_id"`
	ProviderType IAMProviderType `json:"provider_type"`
	Name         string          `json:"name"`
	Config       json.RawMessage `json:"config"`
	Enabled      bool            `json:"enabled"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// IAMRoleMapping maps an external identity to a role on a resource.
type IAMRoleMapping struct {
	ID                 uuid.UUID `json:"id"`
	ProviderID         uuid.UUID `json:"provider_id"`
	ExternalIdentifier string    `json:"external_identifier"`
	ResourceType       string    `json:"resource_type"`
	ResourceID         uuid.UUID `json:"resource_id"`
	Role               Role      `json:"role"`
	CreatedAt          time.Time `json:"created_at"`
}

// --- Filters ---

// ExecutionFilter specifies criteria for listing executions.
type ExecutionFilter struct {
	WorkflowID *uuid.UUID
	Status     ExecutionStatus
	Since      *time.Time
	Until      *time.Time
	Pagination Pagination
}

// LogFilter specifies criteria for querying logs.
type LogFilter struct {
	WorkflowID  *uuid.UUID
	ExecutionID *uuid.UUID
	Level       LogLevel
	ModuleName  string
	Since       *time.Time
	Until       *time.Time
	Pagination  Pagination
}

// AuditFilter specifies criteria for querying audit entries.
type AuditFilter struct {
	UserID       *uuid.UUID
	Action       string
	ResourceType string
	ResourceID   *uuid.UUID
	Since        *time.Time
	Until        *time.Time
	Pagination   Pagination
}

// IAMProviderFilter specifies criteria for listing IAM providers.
type IAMProviderFilter struct {
	CompanyID    *uuid.UUID
	ProviderType IAMProviderType
	Enabled      *bool
	Pagination   Pagination
}

// IAMRoleMappingFilter specifies criteria for listing IAM role mappings.
type IAMRoleMappingFilter struct {
	ProviderID         *uuid.UUID
	ExternalIdentifier string
	ResourceType       string
	ResourceID         *uuid.UUID
	Pagination         Pagination
}
