package main

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestInfraOutput_EnvResolvesModuleSource(t *testing.T) {
	// Staging env renames bmw-database → bmw-staging-db. State is keyed by
	// env-resolved name. Secret generation reads source "bmw-database.uri"
	// which must resolve to "bmw-staging-db" for the state lookup to succeed.

	wfCfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name:   "bmw-database",
				Type:   "infra.database",
				Config: map[string]any{"provider": "do-provider"},
				Environments: map[string]*config.InfraEnvironmentResolution{
					"staging": {Config: map[string]any{"name": "bmw-staging-db"}},
				},
			},
		},
	}

	// Simulate pre-populated state with env-resolved name.
	fakeState := map[string]map[string]any{
		"bmw-staging-db": {"uri": "postgresql://test"},
	}

	// Function under test: resolve infra_output source with envName.
	// Must transform "bmw-database.uri" → lookup "bmw-staging-db" → "uri" field.
	val, err := resolveInfraOutput(wfCfg, "bmw-database.uri", "staging", fakeState)
	if err != nil {
		t.Fatalf("resolveInfraOutput: %v", err)
	}
	if val != "postgresql://test" {
		t.Errorf("got %q, want %q (state lookup via env-resolved name)", val, "postgresql://test")
	}
}

func TestInfraOutput_NoEnvUsesBaseName(t *testing.T) {
	wfCfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name:   "bmw-database",
				Type:   "infra.database",
				Config: map[string]any{"provider": "do-provider"},
			},
		},
	}

	fakeState := map[string]map[string]any{
		"bmw-database": {"uri": "postgresql://base"},
	}

	val, err := resolveInfraOutput(wfCfg, "bmw-database.uri", "", fakeState)
	if err != nil {
		t.Fatalf("resolveInfraOutput: %v", err)
	}
	if val != "postgresql://base" {
		t.Errorf("got %q, want %q (base name when no env)", val, "postgresql://base")
	}
}

func TestInfraOutput_ExplicitlyDisabledModuleErrors(t *testing.T) {
	// A nil environments entry means the module is explicitly removed for this env.
	// resolveInfraOutput must return a clear error rather than silently falling
	// back to the base name (which would read stale/incorrect state).
	wfCfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name:   "bmw-database",
				Type:   "infra.database",
				Config: map[string]any{"provider": "do-provider"},
				Environments: map[string]*config.InfraEnvironmentResolution{
					"staging": nil, // explicitly disabled
				},
			},
		},
	}

	fakeState := map[string]map[string]any{
		"bmw-database": {"uri": "postgresql://base"},
	}

	_, err := resolveInfraOutput(wfCfg, "bmw-database.uri", "staging", fakeState)
	if err == nil {
		t.Fatal("expected error when module is explicitly disabled for env")
	}
	if !strings.Contains(err.Error(), "explicitly disabled") {
		t.Errorf("error should mention 'explicitly disabled', got: %v", err)
	}
}
