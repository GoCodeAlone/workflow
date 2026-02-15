package operator

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/GoCodeAlone/workflow/config"
)

// ReconcileResult describes the outcome of a single reconciliation.
type ReconcileResult struct {
	Action  string // "created", "updated", "deleted", "unchanged", "error"
	Message string
}

// DeployedWorkflow tracks runtime state for a reconciled workflow.
type DeployedWorkflow struct {
	Definition *WorkflowDefinition
	Status     string
	StartedAt  time.Time
	StoppedAt  *time.Time
	Error      string
}

// Reconciler handles the reconciliation loop for WorkflowDefinition CRDs.
// It compares the desired state (spec) with the actual state (deployed) and
// takes corrective action to converge the two.
type Reconciler struct {
	mu          sync.RWMutex
	definitions map[string]*WorkflowDefinition // key: namespace/name
	deployed    map[string]*DeployedWorkflow
	logger      *slog.Logger
}

// NewReconciler creates a new Reconciler.
func NewReconciler(logger *slog.Logger) *Reconciler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Reconciler{
		definitions: make(map[string]*WorkflowDefinition),
		deployed:    make(map[string]*DeployedWorkflow),
		logger:      logger,
	}
}

// Reconcile is the main reconciliation entry point. It compares the desired
// definition against the currently deployed state and takes the appropriate
// action: create, update, or mark unchanged.
func (r *Reconciler) Reconcile(ctx context.Context, def *WorkflowDefinition) (*ReconcileResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	key := definitionKey(def.Metadata.Namespace, def.Metadata.Name)
	r.logger.Info("Reconciling workflow definition", "key", key, "version", def.Spec.Version)

	r.mu.RLock()
	existing, exists := r.deployed[key]
	r.mu.RUnlock()

	// If not deployed yet, create it.
	if !exists {
		if err := r.applyInternal(ctx, def, key); err != nil {
			return &ReconcileResult{Action: "error", Message: err.Error()}, err
		}
		return &ReconcileResult{Action: "created", Message: fmt.Sprintf("workflow %s created", key)}, nil
	}

	// If the version has changed, update.
	if existing.Definition.Spec.Version != def.Spec.Version ||
		existing.Definition.Spec.ConfigYAML != def.Spec.ConfigYAML ||
		existing.Definition.Spec.Replicas != def.Spec.Replicas {

		if err := r.applyInternal(ctx, def, key); err != nil {
			return &ReconcileResult{Action: "error", Message: err.Error()}, err
		}
		return &ReconcileResult{Action: "updated", Message: fmt.Sprintf("workflow %s updated to version %d", key, def.Spec.Version)}, nil
	}

	// No changes needed.
	return &ReconcileResult{Action: "unchanged", Message: fmt.Sprintf("workflow %s is up to date", key)}, nil
}

// Apply creates or updates a WorkflowDefinition. This is the public entry
// point for applying a definition outside the reconciliation loop.
func (r *Reconciler) Apply(ctx context.Context, def *WorkflowDefinition) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	key := definitionKey(def.Metadata.Namespace, def.Metadata.Name)
	return r.applyInternal(ctx, def, key)
}

// applyInternal handles the actual creation or update of a workflow definition.
func (r *Reconciler) applyInternal(_ context.Context, def *WorkflowDefinition, key string) error {
	// Validate the config YAML can be parsed.
	if def.Spec.ConfigYAML == "" {
		return r.failDefinition(def, key, "configYAML is empty")
	}

	_, err := config.LoadFromString(def.Spec.ConfigYAML)
	if err != nil {
		return r.failDefinition(def, key, fmt.Sprintf("invalid config YAML: %v", err))
	}

	// Set defaults.
	if def.Spec.Replicas <= 0 {
		def.Spec.Replicas = 1
	}

	if def.APIVersion == "" {
		def.APIVersion = "workflow.gocodalone.com/v1alpha1"
	}
	if def.Kind == "" {
		def.Kind = "WorkflowDefinition"
	}

	now := time.Now()

	// Update status to Running.
	def.Status = WorkflowDefinitionStatus{
		Phase:           PhaseRunning,
		Replicas:        def.Spec.Replicas,
		ReadyReplicas:   def.Spec.Replicas,
		Message:         "workflow deployed successfully",
		LastTransition:  now,
		ObservedVersion: def.Spec.Version,
	}

	r.mu.Lock()
	r.definitions[key] = def
	r.deployed[key] = &DeployedWorkflow{
		Definition: def,
		Status:     PhaseRunning,
		StartedAt:  now,
	}
	r.mu.Unlock()

	r.logger.Info("Applied workflow definition", "key", key, "version", def.Spec.Version, "replicas", def.Spec.Replicas)
	return nil
}

// failDefinition records a failed deployment and returns the error.
func (r *Reconciler) failDefinition(def *WorkflowDefinition, key string, message string) error {
	now := time.Now()
	def.Status = WorkflowDefinitionStatus{
		Phase:           PhaseFailed,
		Replicas:        def.Spec.Replicas,
		ReadyReplicas:   0,
		Message:         message,
		LastTransition:  now,
		ObservedVersion: def.Spec.Version,
	}

	r.mu.Lock()
	r.definitions[key] = def
	r.deployed[key] = &DeployedWorkflow{
		Definition: def,
		Status:     PhaseFailed,
		StartedAt:  now,
		Error:      message,
	}
	r.mu.Unlock()

	r.logger.Error("Failed to apply workflow definition", "key", key, "error", message)
	return fmt.Errorf("%s", message)
}

// Delete removes a WorkflowDefinition and its deployed state.
func (r *Reconciler) Delete(_ context.Context, name, namespace string) error {
	key := definitionKey(namespace, name)

	r.mu.Lock()
	defer r.mu.Unlock()

	deployed, exists := r.deployed[key]
	if !exists {
		return fmt.Errorf("workflow definition %s not found", key)
	}

	now := time.Now()
	deployed.Status = PhaseTerminated
	deployed.StoppedAt = &now
	if deployed.Definition != nil {
		deployed.Definition.Status.Phase = PhaseTerminated
		deployed.Definition.Status.ReadyReplicas = 0
		deployed.Definition.Status.Message = "workflow terminated"
		deployed.Definition.Status.LastTransition = now
	}

	delete(r.definitions, key)
	delete(r.deployed, key)

	r.logger.Info("Deleted workflow definition", "key", key)
	return nil
}

// Get returns the current WorkflowDefinition for the given name and namespace.
func (r *Reconciler) Get(name, namespace string) (*WorkflowDefinition, error) {
	key := definitionKey(namespace, name)

	r.mu.RLock()
	defer r.mu.RUnlock()

	def, exists := r.definitions[key]
	if !exists {
		return nil, fmt.Errorf("workflow definition %s not found", key)
	}

	return def, nil
}

// List returns all WorkflowDefinitions in the given namespace.
// If namespace is empty, all definitions across all namespaces are returned.
func (r *Reconciler) List(namespace string) []*WorkflowDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*WorkflowDefinition
	for key, def := range r.definitions {
		if namespace == "" {
			result = append(result, def)
			continue
		}
		ns := def.Metadata.Namespace
		if ns == "" {
			ns = "default"
		}
		_ = key // key used for iteration
		if ns == namespace {
			result = append(result, def)
		}
	}
	return result
}
