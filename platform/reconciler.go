package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// ReconcilerStateStore extends StateStore with drift report capabilities.
// This interface is satisfied by the state package implementations.
type ReconcilerStateStore interface {
	StateStore
	SaveDriftReport(ctx context.Context, report *ReconcilerDriftReport) error
}

// ReconcilerDriftReport is the drift report type used by the reconciler.
// It matches the DriftReport in the state package but is defined here
// to avoid circular imports.
type ReconcilerDriftReport struct {
	ID           int64          `json:"id"`
	ContextPath  string         `json:"contextPath"`
	ResourceName string         `json:"resourceName"`
	ResourceType string         `json:"resourceType"`
	Tier         Tier           `json:"tier"`
	DriftType    string         `json:"driftType"`
	Expected     map[string]any `json:"expected"`
	Actual       map[string]any `json:"actual"`
	Diffs        []DiffEntry    `json:"diffs"`
	DetectedAt   time.Time      `json:"detectedAt"`
	ResolvedAt   *time.Time     `json:"resolvedAt,omitempty"`
	ResolvedBy   string         `json:"resolvedBy,omitempty"`
}

// DriftResult represents the outcome of a single resource drift check.
type DriftResult struct {
	// ContextPath is the hierarchical context path of the resource.
	ContextPath string `json:"contextPath"`

	// ResourceName is the name of the resource.
	ResourceName string `json:"resourceName"`

	// ResourceType is the provider-specific resource type.
	ResourceType string `json:"resourceType"`

	// Tier is the tier the resource belongs to.
	Tier Tier `json:"tier"`

	// DriftType classifies the drift: "changed", "added", "removed".
	DriftType string `json:"driftType"`

	// Diffs contains the individual field differences (for "changed" type).
	Diffs []DiffEntry `json:"diffs,omitempty"`

	// Expected is the stored state.
	Expected map[string]any `json:"expected,omitempty"`

	// Actual is the live state from the provider.
	Actual map[string]any `json:"actual,omitempty"`
}

// CrossTierImpact describes how drift in one tier affects resources in
// another tier.
type CrossTierImpact struct {
	// SourceDrift is the drift that triggered the impact analysis.
	SourceDrift DriftResult `json:"sourceDrift"`

	// AffectedResources are the downstream resources potentially impacted.
	AffectedResources []DependencyRef `json:"affectedResources"`
}

// ReconcileResult is the complete output of a reconciliation cycle.
type ReconcileResult struct {
	// ContextPath is the context that was reconciled.
	ContextPath string `json:"contextPath"`

	// DriftResults contains all detected drifts.
	DriftResults []DriftResult `json:"driftResults"`

	// CrossTierImpacts describes downstream impacts of detected drifts.
	CrossTierImpacts []CrossTierImpact `json:"crossTierImpacts,omitempty"`

	// CheckedAt is when the reconciliation was performed.
	CheckedAt time.Time `json:"checkedAt"`

	// Duration is how long the reconciliation took.
	Duration time.Duration `json:"duration"`

	// ResourcesChecked is the number of resources that were checked.
	ResourcesChecked int `json:"resourcesChecked"`
}

// Reconciler runs periodic drift detection by comparing stored state
// with live provider state. It identifies drifted, added, and removed
// resources and tracks cross-tier impact.
type Reconciler struct {
	provider    Provider
	store       StateStore
	contextPath string
	interval    time.Duration
	logger      *log.Logger
}

// NewReconciler creates a new reconciler for the given provider and state store.
func NewReconciler(provider Provider, store StateStore, contextPath string, interval time.Duration) *Reconciler {
	return &Reconciler{
		provider:    provider,
		store:       store,
		contextPath: contextPath,
		interval:    interval,
		logger:      log.Default(),
	}
}

// SetLogger sets a custom logger for the reconciler.
func (r *Reconciler) SetLogger(logger *log.Logger) {
	r.logger = logger
}

// Start runs the reconciliation loop until the context is cancelled.
// It performs an immediate check on start and then at each interval.
func (r *Reconciler) Start(ctx context.Context) error {
	// Perform an immediate reconciliation.
	if result, err := r.Reconcile(ctx); err != nil {
		r.logger.Printf("[reconciler] initial check for %s failed: %v", r.contextPath, err)
	} else if len(result.DriftResults) > 0 {
		r.logger.Printf("[reconciler] initial check for %s found %d drifts", r.contextPath, len(result.DriftResults))
	}

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			result, err := r.Reconcile(ctx)
			if err != nil {
				r.logger.Printf("[reconciler] check for %s failed: %v", r.contextPath, err)
				continue
			}
			if len(result.DriftResults) > 0 {
				r.logger.Printf("[reconciler] %s: %d drifts detected across %d resources (took %v)",
					r.contextPath, len(result.DriftResults), result.ResourcesChecked, result.Duration)
				for _, drift := range result.DriftResults {
					r.logger.Printf("[reconciler]   %s %s/%s: %s",
						drift.DriftType, drift.ContextPath, drift.ResourceName, drift.ResourceType)
				}
				for i := range result.CrossTierImpacts {
					impact := &result.CrossTierImpacts[i]
					r.logger.Printf("[reconciler]   cross-tier impact: %s/%s affects %d downstream resources",
						impact.SourceDrift.ContextPath, impact.SourceDrift.ResourceName, len(impact.AffectedResources))
				}
			}
		}
	}
}

// Reconcile performs a single reconciliation cycle. It lists all resources
// in the context path from the state store, reads their live state from
// the provider, and compares to detect drift.
func (r *Reconciler) Reconcile(ctx context.Context) (*ReconcileResult, error) {
	start := time.Now()

	// Get stored resources.
	storedResources, err := r.store.ListResources(ctx, r.contextPath)
	if err != nil {
		return nil, fmt.Errorf("list stored resources: %w", err)
	}

	result := &ReconcileResult{
		ContextPath:      r.contextPath,
		CheckedAt:        start,
		ResourcesChecked: len(storedResources),
	}

	// Build a set of resource names we've checked for "added" detection.
	checkedNames := make(map[string]bool, len(storedResources))

	for _, stored := range storedResources {
		checkedNames[stored.Name] = true

		// Skip resources that are being provisioned or deleted.
		if stored.Status == ResourceStatusCreating || stored.Status == ResourceStatusDeleting ||
			stored.Status == ResourceStatusPending || stored.Status == ResourceStatusDeleted {
			continue
		}

		// Get the driver for this resource type.
		driver, err := r.provider.ResourceDriver(stored.ProviderType)
		if err != nil {
			r.logger.Printf("[reconciler] no driver for %s, skipping drift check: %v", stored.ProviderType, err)
			continue
		}

		// Read live state from the provider.
		live, err := driver.Read(ctx, stored.Name)
		if err != nil {
			// If the resource is not found in the provider, it was removed externally.
			if isNotFound(err) {
				drift := DriftResult{
					ContextPath:  r.contextPath,
					ResourceName: stored.Name,
					ResourceType: stored.ProviderType,
					DriftType:    "removed",
					Expected:     stored.Properties,
				}
				result.DriftResults = append(result.DriftResults, drift)
				continue
			}
			r.logger.Printf("[reconciler] error reading %s from provider: %v", stored.Name, err)
			continue
		}

		// Diff the stored and live properties.
		diffs, err := driver.Diff(ctx, stored.Name, stored.Properties)
		if err != nil {
			r.logger.Printf("[reconciler] error diffing %s: %v", stored.Name, err)
			continue
		}

		if len(diffs) > 0 {
			drift := DriftResult{
				ContextPath:  r.contextPath,
				ResourceName: stored.Name,
				ResourceType: stored.ProviderType,
				DriftType:    "changed",
				Diffs:        diffs,
				Expected:     stored.Properties,
				Actual:       live.Properties,
			}
			result.DriftResults = append(result.DriftResults, drift)
		}
	}

	// Identify cross-tier impacts for each drift.
	for _, drift := range result.DriftResults {
		deps, err := r.store.Dependencies(ctx, drift.ContextPath, drift.ResourceName)
		if err != nil {
			r.logger.Printf("[reconciler] error checking dependencies for %s: %v", drift.ResourceName, err)
			continue
		}
		if len(deps) > 0 {
			impact := CrossTierImpact{
				SourceDrift:       drift,
				AffectedResources: deps,
			}
			result.CrossTierImpacts = append(result.CrossTierImpacts, impact)
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

// ReconcileJSON performs a reconciliation and returns the result as JSON.
// This is a convenience method for trigger integration.
func (r *Reconciler) ReconcileJSON(ctx context.Context) ([]byte, error) {
	result, err := r.Reconcile(ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}
