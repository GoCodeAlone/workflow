// Package platform defines the core types and interfaces for the platform
// abstraction layer. It provides a three-tier model (Infrastructure, Shared
// Primitives, Application) for declarative infrastructure management through
// capability-based abstractions that are provider-agnostic.
package platform

import "time"

// Tier represents the infrastructure tier a resource belongs to.
// The three tiers form a hierarchy where each tier's outputs constrain
// the tier below it.
type Tier int

const (
	// TierInfrastructure represents Tier 1: compute, networking, IAM.
	// Changes are infrequent and approval-gated.
	TierInfrastructure Tier = 1

	// TierSharedPrimitive represents Tier 2: namespaces, queues, shared DBs.
	// Changes are moderate frequency.
	TierSharedPrimitive Tier = 2

	// TierApplication represents Tier 3: app deployments, scaling policies.
	// Changes are frequent and CI/CD-driven.
	TierApplication Tier = 3
)

// String returns the human-readable name of the tier.
func (t Tier) String() string {
	switch t {
	case TierInfrastructure:
		return "infrastructure"
	case TierSharedPrimitive:
		return "shared_primitive"
	case TierApplication:
		return "application"
	default:
		return "unknown"
	}
}

// Valid returns true if the tier is a recognized value.
func (t Tier) Valid() bool {
	return t >= TierInfrastructure && t <= TierApplication
}

// ResourceStatus represents the lifecycle state of a managed resource.
type ResourceStatus string

const (
	// ResourceStatusPending indicates the resource has been declared but not yet provisioned.
	ResourceStatusPending ResourceStatus = "pending"

	// ResourceStatusCreating indicates the resource is being provisioned.
	ResourceStatusCreating ResourceStatus = "creating"

	// ResourceStatusActive indicates the resource is provisioned and healthy.
	ResourceStatusActive ResourceStatus = "active"

	// ResourceStatusUpdating indicates the resource is being modified.
	ResourceStatusUpdating ResourceStatus = "updating"

	// ResourceStatusDeleting indicates the resource is being torn down.
	ResourceStatusDeleting ResourceStatus = "deleting"

	// ResourceStatusDeleted indicates the resource has been removed.
	ResourceStatusDeleted ResourceStatus = "deleted"

	// ResourceStatusFailed indicates provisioning or update failed.
	ResourceStatusFailed ResourceStatus = "failed"

	// ResourceStatusDegraded indicates the resource is running but not fully healthy.
	ResourceStatusDegraded ResourceStatus = "degraded"

	// ResourceStatusDrifted indicates the actual state diverges from declared state.
	ResourceStatusDrifted ResourceStatus = "drifted"
)

// CapabilityDeclaration is a provider-agnostic resource requirement.
// This is what users write in YAML configuration files. The platform
// abstraction layer maps these to provider-specific resources.
type CapabilityDeclaration struct {
	// Name is the unique identifier for this capability within a tier.
	Name string `yaml:"name" json:"name"`

	// Type is the abstract capability type (e.g., "container_runtime", "database", "message_queue").
	Type string `yaml:"type" json:"type"`

	// Tier indicates which infrastructure tier this capability belongs to.
	Tier Tier `yaml:"tier" json:"tier"`

	// Properties are abstract, provider-agnostic configuration values
	// (e.g., replicas, memory, ports).
	Properties map[string]any `yaml:"properties" json:"properties"`

	// Constraints are hard limits imposed by parent tiers.
	Constraints []Constraint `yaml:"constraints" json:"constraints"`

	// DependsOn lists other capability names that must be provisioned first.
	DependsOn []string `yaml:"dependsOn" json:"dependsOn"`
}

// Constraint represents a limit or requirement imposed by a parent tier
// on downstream tier resources.
type Constraint struct {
	// Field is the property being constrained (e.g., "memory", "replicas", "cpu").
	Field string `yaml:"field" json:"field"`

	// Operator is the comparison operator: "<=", ">=", "==", "in", "not_in".
	Operator string `yaml:"operator" json:"operator"`

	// Value is the constraint limit value.
	Value any `yaml:"value" json:"value"`

	// Source identifies which context or tier imposed this constraint.
	Source string `yaml:"source" json:"source"`
}

// ResourceOutput represents the concrete output of a provisioned resource.
// These become inputs and constraints for downstream tiers.
type ResourceOutput struct {
	// Name is the resource identifier matching the CapabilityDeclaration name.
	Name string `json:"name"`

	// Type is the abstract capability type.
	Type string `json:"type"`

	// ProviderType is the provider-specific resource type (e.g., "aws.eks_cluster").
	ProviderType string `json:"providerType"`

	// Endpoint is the primary access endpoint for the resource, if applicable.
	Endpoint string `json:"endpoint,omitempty"`

	// ConnectionStr is a connection string for database or messaging resources.
	ConnectionStr string `json:"connectionString,omitempty"`

	// CredentialRef is a reference to a credential in the credential broker.
	CredentialRef string `json:"credentialRef,omitempty"`

	// Properties are provider-specific output properties.
	Properties map[string]any `json:"properties"`

	// Status is the current lifecycle state of the resource.
	Status ResourceStatus `json:"status"`

	// LastSynced is the last time the resource state was read from the provider.
	LastSynced time.Time `json:"lastSynced"`
}

// PlanAction represents a single planned change to infrastructure.
type PlanAction struct {
	// Action is the operation type: "create", "update", "delete", or "no-op".
	Action string `json:"action"`

	// ResourceName is the name of the resource being changed.
	ResourceName string `json:"resourceName"`

	// ResourceType is the provider-specific resource type.
	ResourceType string `json:"resourceType"`

	// Provider is the name of the provider executing the action.
	Provider string `json:"provider"`

	// Before is the current state properties (nil for create actions).
	Before map[string]any `json:"before,omitempty"`

	// After is the desired state properties (nil for delete actions).
	After map[string]any `json:"after,omitempty"`

	// Diff contains the individual field differences for update actions.
	Diff []DiffEntry `json:"diff,omitempty"`
}

// DiffEntry represents a single field difference in a plan action.
type DiffEntry struct {
	// Path is the dot-separated field path (e.g., "properties.replicas").
	Path string `json:"path"`

	// OldValue is the current value of the field.
	OldValue any `json:"oldValue"`

	// NewValue is the desired value of the field.
	NewValue any `json:"newValue"`
}

// Plan is the complete execution plan for a set of infrastructure changes.
// Plans must be approved before they can be applied for Tier 1 and Tier 2.
type Plan struct {
	// ID is a unique identifier for this plan.
	ID string `json:"id"`

	// Tier indicates which infrastructure tier this plan operates on.
	Tier Tier `json:"tier"`

	// Context is the hierarchical context path (e.g., "acme/production/api-service").
	Context string `json:"context"`

	// Actions is the ordered list of changes to execute.
	Actions []PlanAction `json:"actions"`

	// CreatedAt is when the plan was generated.
	CreatedAt time.Time `json:"createdAt"`

	// ApprovedAt is when the plan was approved (nil if pending).
	ApprovedAt *time.Time `json:"approvedAt,omitempty"`

	// ApprovedBy is the principal who approved the plan.
	ApprovedBy string `json:"approvedBy,omitempty"`

	// Status is the plan lifecycle state: "pending", "approved", "applying", "applied", "failed".
	Status string `json:"status"`

	// Provider is the name of the provider that generated this plan.
	Provider string `json:"provider"`

	// DryRun indicates whether this plan was generated as a dry-run.
	DryRun bool `json:"dryRun,omitempty"`

	// FidelityReports contains any fidelity gap warnings from the provider.
	FidelityReports []FidelityReport `json:"fidelityReports,omitempty"`
}
