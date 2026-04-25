package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
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

func TestResolveStateStore_ReturnsDiscoverErrors(t *testing.T) {
	_, err := resolveStateStore(filepath.Join(t.TempDir(), "missing.yaml"), "")
	if err == nil {
		t.Fatal("expected missing config error, got nil")
	}
	if !strings.Contains(err.Error(), "discover iac.state modules") {
		t.Fatalf("error = %v, want discover context", err)
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

func TestRunInfraPlan_ReturnsStateLoadErrors(t *testing.T) {
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

  - name: site-dns
    type: infra.dns
    config:
      domain: example.com
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "bad.json"), []byte(`{`), 0o600); err != nil {
		t.Fatalf("write bad state: %v", err)
	}

	err := runInfraPlan([]string{"--config", cfgPath})
	if err == nil {
		t.Fatal("expected state load error, got nil")
	}
	if !strings.Contains(err.Error(), "load current state") {
		t.Fatalf("error = %v, want load current state context", err)
	}
}

func TestRunInfraPlan_RejectsDuplicateResourceNames(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: site-dns
    type: infra.dns
    config:
      domain: example.com

  - name: site-dns
    type: infra.dns
    config:
      domain: example.org
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	err := runInfraPlan([]string{"--config", cfgPath})
	if err == nil {
		t.Fatal("expected duplicate resource name error, got nil")
	}
	if !strings.Contains(err.Error(), "declared more than once") {
		t.Fatalf("error = %v, want duplicate declaration message", err)
	}
}

func TestApplyInfraModules_FailsOnCorruptFilesystemState(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "broken.json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}
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

	err := applyInfraModules(context.Background(), cfgPath, "")
	if err == nil {
		t.Fatal("expected corrupt state error, got nil")
	}
	if !strings.Contains(err.Error(), "load current state") || !strings.Contains(err.Error(), "parse state") {
		t.Fatalf("error = %v, want load current state parse state", err)
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
		ProviderRef:   "do-provider",
		ProviderID:    "do-domain-123",
		ConfigHash:    "live-config-hash",
		AppliedConfig: map[string]any{"domain": "example.com"},
		Outputs:       map[string]any{"domain": "example.com"},
		Dependencies:  []string{"site-app"},
	}

	moduleState := resourceStateToIaCState(state)
	if moduleState.ProviderID != "do-domain-123" {
		t.Fatalf("module ProviderID = %q, want do-domain-123", moduleState.ProviderID)
	}
	if moduleState.ProviderRef != "do-provider" {
		t.Fatalf("module ProviderRef = %q, want do-provider", moduleState.ProviderRef)
	}
	if moduleState.ConfigHash != "live-config-hash" {
		t.Fatalf("module ConfigHash = %q, want live-config-hash", moduleState.ConfigHash)
	}
	if len(moduleState.Dependencies) != 1 || moduleState.Dependencies[0] != "site-app" {
		t.Fatalf("module Dependencies = %#v, want [site-app]", moduleState.Dependencies)
	}

	roundTripped := iacStateToResourceState(&module.IaCState{
		ResourceID:   "site-dns",
		ResourceType: "infra.dns",
		Provider:     "digitalocean",
		ProviderRef:  "do-provider",
		ProviderID:   "do-domain-123",
		ConfigHash:   "live-config-hash",
		Config:       map[string]any{"domain": "example.com"},
		Outputs:      map[string]any{"domain": "example.com"},
		Dependencies: []string{"site-app"},
	})
	if roundTripped.ProviderID != "do-domain-123" {
		t.Fatalf("round-tripped ProviderID = %q, want do-domain-123", roundTripped.ProviderID)
	}
	if roundTripped.ProviderRef != "do-provider" {
		t.Fatalf("round-tripped ProviderRef = %q, want do-provider", roundTripped.ProviderRef)
	}
	if roundTripped.ConfigHash != "live-config-hash" {
		t.Fatalf("round-tripped ConfigHash = %q, want live-config-hash", roundTripped.ConfigHash)
	}
	if len(roundTripped.Dependencies) != 1 || roundTripped.Dependencies[0] != "site-app" {
		t.Fatalf("round-tripped Dependencies = %#v, want [site-app]", roundTripped.Dependencies)
	}
}

func TestFSStateStore_RoundTripsDependencies(t *testing.T) {
	store := &fsWfctlStateStore{dir: t.TempDir()}
	state := interfaces.ResourceState{
		ID:            "site-app",
		Name:          "site-app",
		Type:          "infra.container_service",
		Provider:      "digitalocean",
		ProviderID:    "app-123",
		AppliedConfig: map[string]any{"image": "example/app:latest"},
		Dependencies:  []string{"site-db", "site-dns"},
	}
	if err := store.SaveResource(t.Context(), state); err != nil {
		t.Fatalf("SaveResource: %v", err)
	}
	states, err := store.ListResources(t.Context())
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("states = %d, want 1", len(states))
	}
	if len(states[0].Dependencies) != 2 || states[0].Dependencies[0] != "site-db" || states[0].Dependencies[1] != "site-dns" {
		t.Fatalf("Dependencies = %#v, want [site-db site-dns]", states[0].Dependencies)
	}
}
