package module

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// resolveIaCStore looks up an IaCStateStore from the service registry.
func resolveIaCStore(app modular.Application, storeName, stepName string) (IaCStateStore, error) {
	if app == nil {
		return nil, fmt.Errorf("step %q: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[storeName]
	if !ok {
		return nil, fmt.Errorf("step %q: iac state store %q not found in registry", stepName, storeName)
	}
	store, ok := svc.(IaCStateStore)
	if !ok {
		return nil, fmt.Errorf("step %q: service %q does not implement IaCStateStore (got %T)", stepName, storeName, svc)
	}
	return store, nil
}

// resolvePlatformProvider looks up a PlatformProvider from the service registry.
func resolvePlatformProvider(app modular.Application, platform, stepName string) (PlatformProvider, error) {
	if app == nil {
		return nil, fmt.Errorf("step %q: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[platform]
	if !ok {
		return nil, fmt.Errorf("step %q: platform provider %q not found in registry", stepName, platform)
	}
	provider, ok := svc.(PlatformProvider)
	if !ok {
		return nil, fmt.Errorf("step %q: service %q does not implement PlatformProvider (got %T)", stepName, platform, svc)
	}
	return provider, nil
}

// nowUTC returns the current time as an RFC3339 UTC string.
func nowUTC() string { return time.Now().UTC().Format(time.RFC3339) }

// configSnapshot deep-copies a map[string]any via JSON round-trip.
func configSnapshot(cfg map[string]any) map[string]any {
	if cfg == nil {
		return nil
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

// ─── step.iac_plan ────────────────────────────────────────────────────────────

// IaCPlanStep resolves a PlatformProvider, calls Plan(), and saves a "planned" state.
type IaCPlanStep struct {
	name       string
	platform   string
	resourceID string
	storeName  string
	app        modular.Application
}

// NewIaCPlanStepFactory returns a StepFactory for step.iac_plan.
func NewIaCPlanStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		platform, _ := cfg["platform"].(string)
		if platform == "" {
			return nil, fmt.Errorf("iac_plan step %q: 'platform' is required", name)
		}
		resourceID, _ := cfg["resource_id"].(string)
		if resourceID == "" {
			resourceID = platform
		}
		storeName, _ := cfg["state_store"].(string)
		if storeName == "" {
			return nil, fmt.Errorf("iac_plan step %q: 'state_store' is required", name)
		}
		return &IaCPlanStep{
			name:       name,
			platform:   platform,
			resourceID: resourceID,
			storeName:  storeName,
			app:        app,
		}, nil
	}
}

func (s *IaCPlanStep) Name() string { return s.name }

func (s *IaCPlanStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	provider, err := resolvePlatformProvider(s.app, s.platform, s.name)
	if err != nil {
		return nil, err
	}
	store, err := resolveIaCStore(s.app, s.storeName, s.name)
	if err != nil {
		return nil, err
	}

	plan, err := provider.Plan()
	if err != nil {
		return nil, fmt.Errorf("iac_plan step %q: Plan: %w", s.name, err)
	}

	// Persist planned state.
	existing, _ := store.GetState(s.resourceID)
	now := nowUTC()
	st := &IaCState{
		ResourceID:   s.resourceID,
		ResourceType: plan.Resource,
		Provider:     plan.Provider,
		Status:       "planned",
		Config:       configSnapshot(nil),
		Outputs:      nil,
		UpdatedAt:    now,
	}
	if existing != nil {
		st.CreatedAt = existing.CreatedAt
		st.Config = existing.Config
	} else {
		st.CreatedAt = now
	}

	if err := store.SaveState(st); err != nil {
		return nil, fmt.Errorf("iac_plan step %q: save state: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"plan":        plan,
		"resource_id": s.resourceID,
		"provider":    plan.Provider,
		"actions":     plan.Actions,
		"status":      "planned",
	}}, nil
}

// ─── step.iac_apply ───────────────────────────────────────────────────────────

// IaCApplyStep calls Apply() on a PlatformProvider and updates state to "active".
type IaCApplyStep struct {
	name       string
	platform   string
	resourceID string
	storeName  string
	app        modular.Application
}

// NewIaCApplyStepFactory returns a StepFactory for step.iac_apply.
func NewIaCApplyStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		platform, _ := cfg["platform"].(string)
		if platform == "" {
			return nil, fmt.Errorf("iac_apply step %q: 'platform' is required", name)
		}
		resourceID, _ := cfg["resource_id"].(string)
		if resourceID == "" {
			resourceID = platform
		}
		storeName, _ := cfg["state_store"].(string)
		if storeName == "" {
			return nil, fmt.Errorf("iac_apply step %q: 'state_store' is required", name)
		}
		return &IaCApplyStep{
			name:       name,
			platform:   platform,
			resourceID: resourceID,
			storeName:  storeName,
			app:        app,
		}, nil
	}
}

func (s *IaCApplyStep) Name() string { return s.name }

func (s *IaCApplyStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	provider, err := resolvePlatformProvider(s.app, s.platform, s.name)
	if err != nil {
		return nil, err
	}
	store, err := resolveIaCStore(s.app, s.storeName, s.name)
	if err != nil {
		return nil, err
	}

	now := nowUTC()

	// Transition state to provisioning before calling Apply.
	existing, _ := store.GetState(s.resourceID)
	if existing != nil {
		existing.Status = "provisioning"
		existing.UpdatedAt = now
		_ = store.SaveState(existing)
	}

	result, err := provider.Apply()
	if err != nil {
		// Record error state.
		if existing != nil {
			existing.Status = "error"
			existing.Error = err.Error()
			existing.UpdatedAt = nowUTC()
			_ = store.SaveState(existing)
		}
		return nil, fmt.Errorf("iac_apply step %q: Apply: %w", s.name, err)
	}

	// Extract outputs from apply result.
	var outputs map[string]any
	if result.State != nil {
		if m, ok := result.State.(map[string]any); ok {
			outputs = m
		} else {
			// Store arbitrary state as a JSON-encoded snapshot.
			data, _ := json.Marshal(result.State)
			var m map[string]any
			if json.Unmarshal(data, &m) == nil {
				outputs = m
			}
		}
	}

	// Determine resource type and provider from existing state or result.
	resourceType := ""
	providerName := ""
	if existing != nil {
		resourceType = existing.ResourceType
		providerName = existing.Provider
	}

	st := &IaCState{
		ResourceID:   s.resourceID,
		ResourceType: resourceType,
		Provider:     providerName,
		Status:       "active",
		Outputs:      outputs,
		UpdatedAt:    nowUTC(),
	}
	if existing != nil {
		st.CreatedAt = existing.CreatedAt
		st.Config = existing.Config
	} else {
		st.CreatedAt = st.UpdatedAt
	}

	if err := store.SaveState(st); err != nil {
		return nil, fmt.Errorf("iac_apply step %q: save state: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"result":      result,
		"resource_id": s.resourceID,
		"success":     result.Success,
		"message":     result.Message,
		"state":       result.State,
		"outputs":     outputs,
		"status":      "active",
	}}, nil
}

// ─── step.iac_status ──────────────────────────────────────────────────────────

// IaCStatusStep reads stored state and calls Status() on the PlatformProvider.
type IaCStatusStep struct {
	name       string
	platform   string
	resourceID string
	storeName  string
	app        modular.Application
}

// NewIaCStatusStepFactory returns a StepFactory for step.iac_status.
func NewIaCStatusStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		platform, _ := cfg["platform"].(string)
		if platform == "" {
			return nil, fmt.Errorf("iac_status step %q: 'platform' is required", name)
		}
		resourceID, _ := cfg["resource_id"].(string)
		if resourceID == "" {
			resourceID = platform
		}
		storeName, _ := cfg["state_store"].(string)
		if storeName == "" {
			return nil, fmt.Errorf("iac_status step %q: 'state_store' is required", name)
		}
		return &IaCStatusStep{
			name:       name,
			platform:   platform,
			resourceID: resourceID,
			storeName:  storeName,
			app:        app,
		}, nil
	}
}

func (s *IaCStatusStep) Name() string { return s.name }

func (s *IaCStatusStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	provider, err := resolvePlatformProvider(s.app, s.platform, s.name)
	if err != nil {
		return nil, err
	}
	store, err := resolveIaCStore(s.app, s.storeName, s.name)
	if err != nil {
		return nil, err
	}

	liveStatus, err := provider.Status()
	if err != nil {
		return nil, fmt.Errorf("iac_status step %q: Status: %w", s.name, err)
	}

	st, err := store.GetState(s.resourceID)
	if err != nil {
		return nil, fmt.Errorf("iac_status step %q: get state: %w", s.name, err)
	}

	var storedStatus string
	if st != nil {
		storedStatus = st.Status
	}

	return &StepResult{Output: map[string]any{
		"resource_id":   s.resourceID,
		"live_status":   liveStatus,
		"stored_status": storedStatus,
		"state":         st,
	}}, nil
}

// ─── step.iac_destroy ─────────────────────────────────────────────────────────

// IaCDestroyStep calls Destroy() and marks state as "destroyed".
type IaCDestroyStep struct {
	name       string
	platform   string
	resourceID string
	storeName  string
	app        modular.Application
}

// NewIaCDestroyStepFactory returns a StepFactory for step.iac_destroy.
func NewIaCDestroyStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		platform, _ := cfg["platform"].(string)
		if platform == "" {
			return nil, fmt.Errorf("iac_destroy step %q: 'platform' is required", name)
		}
		resourceID, _ := cfg["resource_id"].(string)
		if resourceID == "" {
			resourceID = platform
		}
		storeName, _ := cfg["state_store"].(string)
		if storeName == "" {
			return nil, fmt.Errorf("iac_destroy step %q: 'state_store' is required", name)
		}
		return &IaCDestroyStep{
			name:       name,
			platform:   platform,
			resourceID: resourceID,
			storeName:  storeName,
			app:        app,
		}, nil
	}
}

func (s *IaCDestroyStep) Name() string { return s.name }

func (s *IaCDestroyStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	provider, err := resolvePlatformProvider(s.app, s.platform, s.name)
	if err != nil {
		return nil, err
	}
	store, err := resolveIaCStore(s.app, s.storeName, s.name)
	if err != nil {
		return nil, err
	}

	now := nowUTC()

	existing, _ := store.GetState(s.resourceID)
	if existing != nil {
		existing.Status = "destroying"
		existing.UpdatedAt = now
		_ = store.SaveState(existing)
	}

	if err := provider.Destroy(); err != nil {
		if existing != nil {
			existing.Status = "error"
			existing.Error = err.Error()
			existing.UpdatedAt = nowUTC()
			_ = store.SaveState(existing)
		}
		return nil, fmt.Errorf("iac_destroy step %q: Destroy: %w", s.name, err)
	}

	st := &IaCState{
		ResourceID: s.resourceID,
		Status:     "destroyed",
		Outputs:    nil,
		UpdatedAt:  nowUTC(),
	}
	if existing != nil {
		st.ResourceType = existing.ResourceType
		st.Provider = existing.Provider
		st.Config = existing.Config
		st.CreatedAt = existing.CreatedAt
	} else {
		st.CreatedAt = st.UpdatedAt
	}

	if err := store.SaveState(st); err != nil {
		return nil, fmt.Errorf("iac_destroy step %q: save state: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"destroyed":   true,
		"resource_id": s.resourceID,
		"status":      "destroyed",
	}}, nil
}

// ─── step.iac_drift_detect ────────────────────────────────────────────────────

// IaCDriftDetectStep compares the stored config snapshot against the current
// platform provider config and reports whether drift has occurred.
type IaCDriftDetectStep struct {
	name          string
	platform      string
	resourceID    string
	storeName     string
	currentConfig map[string]any
	app           modular.Application
}

// NewIaCDriftDetectStepFactory returns a StepFactory for step.iac_drift_detect.
func NewIaCDriftDetectStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		platform, _ := cfg["platform"].(string)
		if platform == "" {
			return nil, fmt.Errorf("iac_drift_detect step %q: 'platform' is required", name)
		}
		resourceID, _ := cfg["resource_id"].(string)
		if resourceID == "" {
			resourceID = platform
		}
		storeName, _ := cfg["state_store"].(string)
		if storeName == "" {
			return nil, fmt.Errorf("iac_drift_detect step %q: 'state_store' is required", name)
		}
		currentConfig, _ := cfg["config"].(map[string]any)
		return &IaCDriftDetectStep{
			name:          name,
			platform:      platform,
			resourceID:    resourceID,
			storeName:     storeName,
			currentConfig: currentConfig,
			app:           app,
		}, nil
	}
}

func (s *IaCDriftDetectStep) Name() string { return s.name }

func (s *IaCDriftDetectStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	store, err := resolveIaCStore(s.app, s.storeName, s.name)
	if err != nil {
		return nil, err
	}

	st, err := store.GetState(s.resourceID)
	if err != nil {
		return nil, fmt.Errorf("iac_drift_detect step %q: get state: %w", s.name, err)
	}
	if st == nil {
		return nil, fmt.Errorf("iac_drift_detect step %q: no state found for resource %q", s.name, s.resourceID)
	}

	diffs := detectConfigDrift(st.Config, s.currentConfig)
	drifted := len(diffs) > 0

	return &StepResult{Output: map[string]any{
		"resource_id":    s.resourceID,
		"drifted":        drifted,
		"diffs":          diffs,
		"stored_config":  st.Config,
		"current_config": s.currentConfig,
		"stored_status":  st.Status,
	}}, nil
}

// IaCDriftDiff describes a single configuration difference.
type IaCDriftDiff struct {
	Key      string `json:"key"`
	Stored   any    `json:"stored"`
	Current  any    `json:"current"`
	DiffType string `json:"diff_type"` // added, removed, changed
}

// detectConfigDrift compares stored vs current config and returns the differences.
func detectConfigDrift(stored, current map[string]any) []IaCDriftDiff {
	var diffs []IaCDriftDiff

	// Keys in stored but not in current (removed).
	for k, sv := range stored {
		cv, ok := current[k]
		if !ok {
			diffs = append(diffs, IaCDriftDiff{Key: k, Stored: sv, Current: nil, DiffType: "removed"})
			continue
		}
		if fmt.Sprintf("%v", sv) != fmt.Sprintf("%v", cv) {
			diffs = append(diffs, IaCDriftDiff{Key: k, Stored: sv, Current: cv, DiffType: "changed"})
		}
	}

	// Keys in current but not in stored (added).
	for k, cv := range current {
		if _, ok := stored[k]; !ok {
			diffs = append(diffs, IaCDriftDiff{Key: k, Stored: nil, Current: cv, DiffType: "added"})
		}
	}

	return diffs
}
