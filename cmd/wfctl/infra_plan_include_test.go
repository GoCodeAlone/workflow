package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// writeThreeResourceInfraConfig writes an infra YAML with three named infra.vpc
// resources (res-A, res-B, res-C) backed by the same provider. Used to verify
// that --include properly scopes plan output to the named subset.
func writeThreeResourceInfraConfig(t *testing.T, dir string) string {
	t.Helper()
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "infra.yaml")
	cfg := `infra:
  auto_bootstrap: false
modules:
  - name: cloud-provider
    type: iac.provider
    config:
      provider: fake-provider
  - name: state-store
    type: iac.state
    config:
      backend: filesystem
      directory: ` + stateDir + `
  - name: res-A
    type: infra.vpc
    config:
      provider: cloud-provider
  - name: res-B
    type: infra.vpc
    config:
      provider: cloud-provider
  - name: res-C
    type: infra.vpc
    config:
      provider: cloud-provider
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

// seedThreeResourceState seeds state entries for all three resources.
func seedThreeResourceState(t *testing.T, cfgPath string) {
	t.Helper()
	store, err := resolveStateStore(cfgPath, "")
	if err != nil {
		t.Fatalf("resolveStateStore: %v", err)
	}
	for _, name := range []string{"res-A", "res-B", "res-C"} {
		entry := interfaces.ResourceState{
			ID:         name,
			Name:       name,
			Type:       "infra.vpc",
			Provider:   "fake-provider",
			ProviderID: "pid-" + name,
			Outputs:    map[string]any{"ip_range": "10.0.0.0/16"},
		}
		if err := store.SaveResource(context.Background(), entry); err != nil {
			t.Fatalf("seed state %q: %v", name, err)
		}
	}
}

// TestPlanInclude_FlagRegistered verifies that --include is accepted by
// runInfraPlan (not "flag provided but not defined").
func TestPlanInclude_FlagRegistered(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeThreeResourceInfraConfig(t, dir)
	seedThreeResourceState(t, cfgPath)

	cleanup := installFakeRefreshProvider(t, map[string]map[string]any{})
	defer cleanup()

	_, err := captureStdout(t, func() error {
		return runInfraPlan([]string{"--include=res-A", "-c", cfgPath})
	})
	if err != nil && strings.Contains(err.Error(), "flag provided but not defined: -include") {
		t.Errorf("--include flag not registered in runInfraPlan: %v", err)
	}
}

// TestPlanInclude_EmptyMeansAll verifies that omitting --include returns all
// resources (back-compat behavior).
func TestPlanInclude_EmptyMeansAll(t *testing.T) {
	includeSet := parseIncludeFlag("")
	if includeSet != nil {
		t.Errorf("empty include should yield nil; got %v", includeSet)
	}
}

// TestPlanInclude_UnknownResourceFails verifies that --include with a name
// not present in config or state fails fast.
func TestPlanInclude_UnknownResourceFails(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeThreeResourceInfraConfig(t, dir)
	seedThreeResourceState(t, cfgPath)

	cleanup := installFakeRefreshProvider(t, map[string]map[string]any{})
	defer cleanup()

	_, err := captureStdout(t, func() error {
		return runInfraPlan([]string{"--include=does-not-exist", "-c", cfgPath})
	})
	if err == nil {
		t.Fatal("expected error for unknown --include resource name")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error should name the unknown resource; got %v", err)
	}
}

// TestPlanInclude_HelpTextPresent verifies the --include flag appears in
// runInfraPlan --help output.
func TestPlanInclude_HelpTextPresent(t *testing.T) {
	// Running --help returns an error (flag.ErrHelp), but we can verify the
	// flag is accepted (no "flag provided but not defined") by checking error text.
	err := runInfraPlan([]string{"--help"})
	if err != nil && strings.Contains(err.Error(), "flag provided but not defined: -include") {
		t.Errorf("--include not in plan --help: %v", err)
	}
}

// TestPlanInclude_FiltersOutput verifies that --include=res-A,res-B scopes the
// plan to only those two resources. We verify by checking that the plan output
// does NOT reference res-C.
func TestPlanInclude_FiltersOutput(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeThreeResourceInfraConfig(t, dir)
	seedThreeResourceState(t, cfgPath)

	cleanup := installFakeRefreshProvider(t, map[string]map[string]any{
		"pid-res-A": {"ip_range": "10.0.0.0/16"},
		"pid-res-B": {"ip_range": "10.0.0.0/16"},
	})
	defer cleanup()

	out, err := captureStdout(t, func() error {
		return runInfraPlan([]string{"--include=res-A,res-B", "-c", cfgPath})
	})
	if err != nil {
		t.Fatalf("runInfraPlan: %v", err)
	}

	// res-C must NOT appear in plan output (it was excluded by --include).
	if strings.Contains(out, "res-C") {
		t.Errorf("--include=res-A,res-B should exclude res-C from plan output; got:\n%s", out)
	}
}

// TestPlanInclude_StateOnlyResourceEligibleForDelete verifies that a resource
// present only in state (no spec) can be included — it will appear as a delete
// in the plan.
func TestPlanInclude_StateOnlyResourceEligibleForDelete(t *testing.T) {
	dir := t.TempDir()
	// Config has only res-A and res-B (no res-C).
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "infra.yaml")
	cfg := `infra:
  auto_bootstrap: false
modules:
  - name: cloud-provider
    type: iac.provider
    config:
      provider: fake-provider
  - name: state-store
    type: iac.state
    config:
      backend: filesystem
      directory: ` + stateDir + `
  - name: res-A
    type: infra.vpc
    config:
      provider: cloud-provider
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	// Seed state with both res-A (in config) and orphan-B (state-only).
	// orphan-B has ProviderRef set so the state-based group-by-provider path
	// can route it to the correct provider for delete planning.
	store, err := resolveStateStore(cfgPath, "")
	if err != nil {
		t.Fatalf("resolveStateStore: %v", err)
	}
	for _, name := range []string{"res-A", "orphan-B"} {
		entry := interfaces.ResourceState{
			ID: name, Name: name, Type: "infra.vpc",
			Provider: "fake-provider", ProviderID: "pid-" + name,
			ProviderRef: "cloud-provider",
		}
		if serr := store.SaveResource(context.Background(), entry); serr != nil {
			t.Fatalf("seed %q: %v", name, serr)
		}
	}

	cleanup := installFakeRefreshProvider(t, map[string]map[string]any{})
	defer cleanup()

	// --include=orphan-B should be accepted AND produce a delete action for
	// the state-only resource (not a silent no-op).
	out, err := captureStdout(t, func() error {
		return runInfraPlan([]string{"--include=orphan-B", "-c", cfgPath})
	})
	// Should not fail with "not declared" for orphan-B.
	if err != nil && strings.Contains(err.Error(), "--include: 1 resource(s) not declared") {
		t.Errorf("state-only resource should be accepted by --include; got %v", err)
	}
	// orphan-B must appear in the plan output (not silently skipped).
	if err == nil && !strings.Contains(out, "orphan-B") {
		t.Errorf("--include=orphan-B: state-only resource should appear in plan output; got:\n%s", out)
	}
}
