package store

import (
	"context"

	"github.com/google/uuid"
)

// Pagination holds common pagination parameters.
type Pagination struct {
	Offset int
	Limit  int
}

// DefaultPagination returns a Pagination with sensible defaults.
func DefaultPagination() Pagination {
	return Pagination{Offset: 0, Limit: 50}
}

// --- User ---

// UserFilter specifies criteria for listing users.
type UserFilter struct {
	Email         string
	Active        *bool
	OAuthProvider OAuthProvider
	Pagination    Pagination
}

// UserStore defines persistence operations for users.
type UserStore interface {
	Create(ctx context.Context, u *User) error
	Get(ctx context.Context, id uuid.UUID) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetByOAuth(ctx context.Context, provider OAuthProvider, oauthID string) (*User, error)
	Update(ctx context.Context, u *User) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, f UserFilter) ([]*User, error)
}

// --- Company / Organization ---

// CompanyFilter specifies criteria for listing companies.
type CompanyFilter struct {
	OwnerID    *uuid.UUID
	Slug       string
	Pagination Pagination
}

// CompanyStore defines persistence operations for companies.
type CompanyStore interface {
	Create(ctx context.Context, c *Company) error
	Get(ctx context.Context, id uuid.UUID) (*Company, error)
	GetBySlug(ctx context.Context, slug string) (*Company, error)
	Update(ctx context.Context, c *Company) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, f CompanyFilter) ([]*Company, error)
	ListForUser(ctx context.Context, userID uuid.UUID) ([]*Company, error)
}

// OrganizationStore is an alias for CompanyStore.
type OrganizationStore = CompanyStore

// --- Project ---

// ProjectFilter specifies criteria for listing projects.
type ProjectFilter struct {
	CompanyID  *uuid.UUID
	Slug       string
	Pagination Pagination
}

// ProjectStore defines persistence operations for projects.
type ProjectStore interface {
	Create(ctx context.Context, p *Project) error
	Get(ctx context.Context, id uuid.UUID) (*Project, error)
	GetBySlug(ctx context.Context, companyID uuid.UUID, slug string) (*Project, error)
	Update(ctx context.Context, p *Project) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, f ProjectFilter) ([]*Project, error)
	ListForUser(ctx context.Context, userID uuid.UUID) ([]*Project, error)
}

// --- Workflow ---

// WorkflowFilter specifies criteria for listing workflow records.
type WorkflowFilter struct {
	ProjectID  *uuid.UUID
	Status     WorkflowStatus
	Slug       string
	Pagination Pagination
}

// WorkflowStore defines persistence operations for workflow records.
type WorkflowStore interface {
	Create(ctx context.Context, w *WorkflowRecord) error
	Get(ctx context.Context, id uuid.UUID) (*WorkflowRecord, error)
	GetBySlug(ctx context.Context, projectID uuid.UUID, slug string) (*WorkflowRecord, error)
	Update(ctx context.Context, w *WorkflowRecord) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, f WorkflowFilter) ([]*WorkflowRecord, error)
	// GetVersion retrieves a specific version of a workflow.
	GetVersion(ctx context.Context, id uuid.UUID, version int) (*WorkflowRecord, error)
	// ListVersions returns all versions for a given workflow ID.
	ListVersions(ctx context.Context, id uuid.UUID) ([]*WorkflowRecord, error)
}

// --- Membership ---

// MembershipFilter specifies criteria for listing memberships.
type MembershipFilter struct {
	UserID     *uuid.UUID
	CompanyID  *uuid.UUID
	ProjectID  *uuid.UUID
	Role       Role
	Pagination Pagination
}

// MembershipStore defines persistence operations for memberships.
type MembershipStore interface {
	Create(ctx context.Context, m *Membership) error
	Get(ctx context.Context, id uuid.UUID) (*Membership, error)
	Update(ctx context.Context, m *Membership) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, f MembershipFilter) ([]*Membership, error)
	// GetEffectiveRole resolves the effective role for a user in a project,
	// cascading from company-level if no project-level membership exists.
	GetEffectiveRole(ctx context.Context, userID, companyID uuid.UUID, projectID *uuid.UUID) (Role, error)
}

// --- CrossWorkflowLink ---

// CrossWorkflowLinkFilter specifies criteria for listing cross-workflow links.
type CrossWorkflowLinkFilter struct {
	SourceWorkflowID *uuid.UUID
	TargetWorkflowID *uuid.UUID
	LinkType         string
	Pagination       Pagination
}

// CrossWorkflowLinkStore defines persistence operations for cross-workflow links.
type CrossWorkflowLinkStore interface {
	Create(ctx context.Context, l *CrossWorkflowLink) error
	Get(ctx context.Context, id uuid.UUID) (*CrossWorkflowLink, error)
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, f CrossWorkflowLinkFilter) ([]*CrossWorkflowLink, error)
}

// --- Session ---

// SessionFilter specifies criteria for listing sessions.
type SessionFilter struct {
	UserID     *uuid.UUID
	Active     *bool
	Pagination Pagination
}

// SessionStore defines persistence operations for sessions.
type SessionStore interface {
	Create(ctx context.Context, s *Session) error
	Get(ctx context.Context, id uuid.UUID) (*Session, error)
	GetByToken(ctx context.Context, token string) (*Session, error)
	Update(ctx context.Context, s *Session) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, f SessionFilter) ([]*Session, error)
	DeleteExpired(ctx context.Context) (int64, error)
}

// --- ExecutionStore ---

// ExecutionStore defines persistence operations for workflow executions.
type ExecutionStore interface {
	// CreateExecution creates a new workflow execution record.
	CreateExecution(ctx context.Context, e *WorkflowExecution) error
	// GetExecution retrieves an execution by ID.
	GetExecution(ctx context.Context, id uuid.UUID) (*WorkflowExecution, error)
	// UpdateExecution updates an execution record.
	UpdateExecution(ctx context.Context, e *WorkflowExecution) error
	// ListExecutions lists executions matching the filter.
	ListExecutions(ctx context.Context, f ExecutionFilter) ([]*WorkflowExecution, error)

	// CreateStep creates a new execution step.
	CreateStep(ctx context.Context, s *ExecutionStep) error
	// UpdateStep updates an execution step.
	UpdateStep(ctx context.Context, s *ExecutionStep) error
	// ListSteps lists steps for an execution.
	ListSteps(ctx context.Context, executionID uuid.UUID) ([]*ExecutionStep, error)

	// CountByStatus returns execution counts grouped by status for a workflow.
	CountByStatus(ctx context.Context, workflowID uuid.UUID) (map[ExecutionStatus]int, error)
}

// --- LogStore ---

// LogStore defines persistence operations for execution logs.
type LogStore interface {
	// Append adds a log entry.
	Append(ctx context.Context, l *ExecutionLog) error
	// Query returns log entries matching the filter.
	Query(ctx context.Context, f LogFilter) ([]*ExecutionLog, error)
	// CountByLevel returns log counts grouped by level for a workflow.
	CountByLevel(ctx context.Context, workflowID uuid.UUID) (map[LogLevel]int, error)
}

// --- AuditStore ---

// AuditStore defines persistence operations for audit log entries.
type AuditStore interface {
	// Record adds an audit entry.
	Record(ctx context.Context, e *AuditEntry) error
	// Query returns audit entries matching the filter.
	Query(ctx context.Context, f AuditFilter) ([]*AuditEntry, error)
}

// --- IAMStore ---

// IAMStore defines persistence operations for IAM providers and role mappings.
type IAMStore interface {
	// CreateProvider creates a new IAM provider config.
	CreateProvider(ctx context.Context, p *IAMProviderConfig) error
	// GetProvider retrieves an IAM provider by ID.
	GetProvider(ctx context.Context, id uuid.UUID) (*IAMProviderConfig, error)
	// UpdateProvider updates an IAM provider config.
	UpdateProvider(ctx context.Context, p *IAMProviderConfig) error
	// DeleteProvider deletes an IAM provider config.
	DeleteProvider(ctx context.Context, id uuid.UUID) error
	// ListProviders lists IAM providers matching the filter.
	ListProviders(ctx context.Context, f IAMProviderFilter) ([]*IAMProviderConfig, error)

	// CreateMapping creates a new IAM role mapping.
	CreateMapping(ctx context.Context, m *IAMRoleMapping) error
	// GetMapping retrieves an IAM role mapping by ID.
	GetMapping(ctx context.Context, id uuid.UUID) (*IAMRoleMapping, error)
	// DeleteMapping deletes an IAM role mapping.
	DeleteMapping(ctx context.Context, id uuid.UUID) error
	// ListMappings lists IAM role mappings matching the filter.
	ListMappings(ctx context.Context, f IAMRoleMappingFilter) ([]*IAMRoleMapping, error)

	// ResolveRole resolves the role for an external identifier on a resource.
	ResolveRole(ctx context.Context, providerID uuid.UUID, externalID string, resourceType string, resourceID uuid.UUID) (Role, error)
}
