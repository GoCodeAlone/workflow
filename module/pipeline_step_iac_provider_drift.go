package module

import (
	"context"
	"errors"
	"fmt"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/iac/providerclient"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// ─── step.iac_provider_drift ─────────────────────────────────────────────────

// IaCProviderDriftStep resolves an IaCProvider and checks for configuration
// drift. It type-asserts to providerclient.DriftDetectorProvider for the
// config-aware drift path; if the provider does not advertise the optional
// IaCProviderDriftDetector service, it returns {supported:false}.
type IaCProviderDriftStep struct {
	name     string
	provider string
	refs     []interfaces.ResourceRef
	app      modular.Application
}

// NewIaCProviderDriftStepFactory returns a StepFactory for step.iac_provider_drift.
func NewIaCProviderDriftStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		providerName, _ := cfg["provider"].(string)
		if providerName == "" {
			return nil, fmt.Errorf("iac_provider_drift step %q: 'provider' is required", name)
		}
		refs, err := parseResourceRefs(cfg["refs"])
		if err != nil {
			return nil, fmt.Errorf("iac_provider_drift step %q: parse refs: %w", name, err)
		}
		return &IaCProviderDriftStep{
			name:     name,
			provider: providerName,
			refs:     refs,
			app:      app,
		}, nil
	}
}

func (s *IaCProviderDriftStep) Name() string { return s.name }

func (s *IaCProviderDriftStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	provider, err := resolveIaCProvider(s.app, s.provider, s.name, "iac_provider_drift")
	if err != nil {
		return nil, err
	}

	// Attempt config-aware drift detection via the optional DriftDetectorProvider
	// accessor (PR-1 pattern). If the accessor returns nil, fall back to the
	// existence-only DetectDrift on the required interface.
	if ddp, ok := provider.(providerclient.DriftDetectorProvider); ok {
		if dd := ddp.DriftDetector(); dd != nil {
			// Config-aware drift: build a per-ref spec map from refs (no specs
			// provided here — providers fall back to existence-only for missing entries).
			drifts, err := dd.DetectDriftWithSpecs(ctx, s.refs, nil)
			if err != nil {
				return nil, fmt.Errorf("iac_provider_drift step %q: DetectDriftWithSpecs: %w", s.name, err)
			}
			return driftResult(s.provider, drifts, true), nil
		}
	}

	// Existence-only drift via the required DetectDrift method.
	drifts, driftErr := provider.DetectDrift(ctx, s.refs)
	if driftErr != nil {
		// Only ErrProviderMethodUnimplemented means the plugin wired neither drift
		// path — surface as {supported:false} so callers can gate without pipeline
		// failure. Any other error (network, provider failure, etc.) is a real error
		// and MUST be returned so it propagates as HTTP 5xx rather than being masked
		// as an unsupported-feature response.
		if !errors.Is(driftErr, interfaces.ErrProviderMethodUnimplemented) {
			return nil, fmt.Errorf("iac_provider_drift step %q: DetectDrift: %w", s.name, driftErr)
		}
		// Plugin declared DetectDrift is unimplemented — treat as unsupported.
		// Zero-value fields are included so downstream type-assertions on
		// result.Output["any_drifted"].(bool) do not panic.
		return &StepResult{Output: map[string]any{
			"provider":    s.provider,
			"supported":   false,
			"reason":      driftErr.Error(),
			"any_drifted": false,
			"drifts":      []map[string]any{},
			"count":       0,
		}}, nil
	}
	return driftResult(s.provider, drifts, true), nil
}

// driftResult builds the step output map from a drift detection result.
func driftResult(providerName string, drifts []interfaces.DriftResult, supported bool) *StepResult {
	results := make([]map[string]any, 0, len(drifts))
	for _, d := range drifts {
		results = append(results, map[string]any{
			"name":     d.Name,
			"type":     d.Type,
			"drifted":  d.Drifted,
			"class":    string(d.Class),
			"fields":   d.Fields,
			"expected": d.Expected,
			"actual":   d.Actual,
		})
	}
	anyDrifted := false
	for _, d := range drifts {
		if d.Drifted {
			anyDrifted = true
			break
		}
	}
	return &StepResult{Output: map[string]any{
		"provider":    providerName,
		"supported":   supported,
		"any_drifted": anyDrifted,
		"drifts":      results,
		"count":       len(results),
	}}
}
