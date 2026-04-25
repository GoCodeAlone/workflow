package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
)

// ── TestResolveStateStore_NoEnv_FallsBackToBase ────────────────────────────────

// TestResolveStateStore_NoEnv_FallsBackToBase verifies that when envName is
// empty, resolveStateStore uses the base config directly and initialises a
// filesystem backend from the base directory field.
func TestResolveStateStore_NoEnv_FallsBackToBase(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: iac-state
    type: iac.state
    config:
      backend: filesystem
      directory: `+stateDir+`
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	store, err := resolveStateStore(cfgPath, "")
	if err != nil {
		t.Fatalf("resolveStateStore: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	// Verify it is functional: ListResources on empty dir returns nil, no error.
	states, err := store.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(states) != 0 {
		t.Errorf("expected empty state, got %d records", len(states))
	}
}

// ── TestResolveStateStore_EnvOverride_UsesEnvConfig ───────────────────────────

// TestResolveStateStore_EnvOverride_UsesEnvConfig verifies that when envName
// is set and the iac.state module has an environments section, the
// env-resolved config is used to initialise the backend. Specifically it
// checks that a filesystem backend declared only under environments.staging
// is initialised with the staging directory, not the base directory.
func TestResolveStateStore_EnvOverride_UsesEnvConfig(t *testing.T) {
	dir := t.TempDir()
	baseDir := filepath.Join(dir, "base-state")
	stagingDir := filepath.Join(dir, "staging-state")
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: iac-state
    type: iac.state
    config:
      backend: filesystem
      directory: `+baseDir+`
    environments:
      staging:
        config:
          directory: `+stagingDir+`
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	store, err := resolveStateStore(cfgPath, "staging")
	if err != nil {
		t.Fatalf("resolveStateStore(staging): %v", err)
	}

	// Write a resource through the store — it should land in stagingDir.
	if err := os.MkdirAll(stagingDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rs := interfaces.ResourceState{
		ID:   "test-vpc",
		Name: "test-vpc",
		Type: "infra.vpc",
	}
	if err := store.SaveResource(context.Background(), rs); err != nil {
		t.Fatalf("SaveResource: %v", err)
	}
	// Verify file is in stagingDir, not baseDir.
	entries, err := os.ReadDir(stagingDir)
	if err != nil {
		t.Fatalf("ReadDir stagingDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("staging state dir: want 1 file, got %d", len(entries))
	}
	baseEntries, _ := os.ReadDir(baseDir)
	if len(baseEntries) != 0 {
		t.Errorf("base state dir should be empty (env override applied), got %d files", len(baseEntries))
	}
}

// ── TestApplyInfraModules_PersistsResourceState ────────────────────────────────

// TestApplyInfraModules_PersistsResourceState is an end-to-end regression gate
// verifying that applyInfraModules actually calls store.SaveResource for each
// resource returned by provider.Apply. This test would have caught the silent
// state-drop caused by the missing envName propagation to resolveStateStore.
func TestApplyInfraModules_PersistsResourceState(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: my-provider
    type: iac.provider
    config:
      provider: fake-cloud

  - name: iac-state
    type: iac.state
    config:
      backend: filesystem
      directory: `+stateDir+`

  - name: my-vpc
    type: infra.vpc
    config:
      provider: my-provider
      region: nyc3
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Fake provider that returns one provisioned resource.
	fakeResult := &interfaces.ApplyResult{
		Resources: []interfaces.ResourceOutput{
			{Name: "my-vpc", Type: "infra.vpc", ProviderID: "vpc-abc123", Outputs: map[string]any{"id": "vpc-abc123"}},
		},
	}
	fake := &stateReturningProvider{applyResult: fakeResult}

	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}
	t.Cleanup(func() { resolveIaCProvider = orig })

	if err := applyInfraModules(context.Background(), cfgPath, ""); err != nil {
		t.Fatalf("applyInfraModules: %v", err)
	}

	// State should now contain the provisioned resource.
	store, err := resolveStateStore(cfgPath, "")
	if err != nil {
		t.Fatalf("resolveStateStore after apply: %v", err)
	}
	states, err := store.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources after apply: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("expected 1 persisted resource, got %d", len(states))
	}
	if states[0].Name != "my-vpc" {
		t.Errorf("persisted resource name = %q, want my-vpc", states[0].Name)
	}
	if states[0].Type != "infra.vpc" {
		t.Errorf("persisted resource type = %q, want infra.vpc", states[0].Type)
	}
}

func TestResourceStateModuleConversion_PreservesProviderMetadata(t *testing.T) {
	state := interfaces.ResourceState{
		ID:            "site-dns",
		Name:          "site-dns",
		Type:          "infra.dns",
		Provider:      "digitalocean",
		ProviderID:    "do-domain-123",
		ConfigHash:    "live-config-hash",
		AppliedConfig: map[string]any{"domain": "example.com"},
		Outputs:       map[string]any{"domain": "example.com"},
	}

	moduleState := resourceStateToIaCState(state)
	if moduleState.ProviderID != "do-domain-123" {
		t.Fatalf("module ProviderID = %q, want do-domain-123", moduleState.ProviderID)
	}
	if moduleState.ConfigHash != "live-config-hash" {
		t.Fatalf("module ConfigHash = %q, want live-config-hash", moduleState.ConfigHash)
	}

	roundTripped := iacStateToResourceState(&module.IaCState{
		ResourceID:   "site-dns",
		ResourceType: "infra.dns",
		Provider:     "digitalocean",
		ProviderID:   "do-domain-123",
		ConfigHash:   "live-config-hash",
		Config:       map[string]any{"domain": "example.com"},
		Outputs:      map[string]any{"domain": "example.com"},
	})
	if roundTripped.ProviderID != "do-domain-123" {
		t.Fatalf("round-tripped ProviderID = %q, want do-domain-123", roundTripped.ProviderID)
	}
	if roundTripped.ConfigHash != "live-config-hash" {
		t.Fatalf("round-tripped ConfigHash = %q, want live-config-hash", roundTripped.ConfigHash)
	}
}
