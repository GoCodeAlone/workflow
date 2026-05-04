package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// writeApplyPreRefreshConfig writes an infra YAML at <dir>/infra.yaml with
// auto_bootstrap disabled (so the apply path doesn't try to run bootstrap),
// a filesystem state backend pointed at <dir>/state, a single iac.provider
// module, and one infra.vpc resource keyed to it. Returned path is the
// absolute config path.
func writeApplyPreRefreshConfig(t *testing.T, dir string) string {
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
  - name: coredump-staging-vpc
    type: infra.vpc
    config:
      provider: cloud-provider
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

// seedStaleVPCState writes a single VPC ResourceState to the configured
// filesystem state backend with no "id" output, ready for refresh to add it.
func seedStaleVPCState(t *testing.T, cfgPath string) {
	t.Helper()
	store, err := resolveStateStore(cfgPath, "")
	if err != nil {
		t.Fatalf("resolveStateStore: %v", err)
	}
	stale := interfaces.ResourceState{
		ID:          "coredump-staging-vpc",
		Name:        "coredump-staging-vpc",
		Type:        "infra.vpc",
		Provider:    "fake-provider",
		ProviderRef: "cloud-provider",
		ProviderID:  "uuid-1",
		Outputs:     map[string]any{"ip_range": "10.0.0.0/16"},
	}
	if err := store.SaveResource(context.Background(), stale); err != nil {
		t.Fatalf("seed state: %v", err)
	}
}

// loadVPCStateOutputs returns the Outputs map for coredump-staging-vpc as
// persisted on disk after the apply call returns.
func loadVPCStateOutputs(t *testing.T, cfgPath string) map[string]any {
	t.Helper()
	states, err := loadCurrentState(cfgPath, "")
	if err != nil {
		t.Fatalf("loadCurrentState: %v", err)
	}
	for _, s := range states {
		if s.Name == "coredump-staging-vpc" {
			return s.Outputs
		}
	}
	t.Fatalf("coredump-staging-vpc not in state after apply; got %d entries", len(states))
	return nil
}

// installFakeRefreshProvider swaps the global resolveIaCProvider for the
// duration of the test with a stub that handles both Read (via the embedded
// driver) and Apply (returning an empty ApplyResult so the apply path is a
// no-op). Returns the cleanup function.
func installFakeRefreshProvider(t *testing.T, liveOutputs map[string]map[string]any) func() {
	t.Helper()
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return &refreshOutputsCmdFakeProvider{readOutputs: liveOutputs}, nil, nil
	}
	return func() { resolveIaCProvider = orig }
}

// TestApply_PreStepRefresh_OptInViaEnvVar verifies that setting
// WFCTL_REFRESH_OUTPUTS=1 causes runInfraApply to read live Outputs for
// every state entry and persist new fields BEFORE the plan+apply phase.
// The end-to-end signal is the "id" field present in state on disk after
// apply returns.
func TestApply_PreStepRefresh_OptInViaEnvVar(t *testing.T) {
	t.Setenv("WFCTL_REFRESH_OUTPUTS", "1")
	dir := t.TempDir()
	cfgPath := writeApplyPreRefreshConfig(t, dir)
	seedStaleVPCState(t, cfgPath)

	cleanup := installFakeRefreshProvider(t, map[string]map[string]any{
		"uuid-1": {"ip_range": "10.0.0.0/16", "id": "uuid-1"},
	})
	defer cleanup()

	if _, err := captureStdout(t, func() error {
		return runInfraApply([]string{"--auto-approve", "-c", cfgPath})
	}); err != nil {
		t.Fatalf("runInfraApply: %v", err)
	}

	got := loadVPCStateOutputs(t, cfgPath)
	if id, _ := got["id"].(string); id != "uuid-1" {
		t.Errorf("apply pre-refresh should populate 'id'; got %v", got)
	}
}

// TestApply_PreStepRefresh_DisabledByDefault verifies the opt-in semantics:
// without WFCTL_REFRESH_OUTPUTS set, the pre-step is skipped and stale
// state remains stale.
func TestApply_PreStepRefresh_DisabledByDefault(t *testing.T) {
	// Clear any inherited value to make the test resilient under -count=N.
	t.Setenv("WFCTL_REFRESH_OUTPUTS", "")
	os.Unsetenv("WFCTL_REFRESH_OUTPUTS")
	dir := t.TempDir()
	cfgPath := writeApplyPreRefreshConfig(t, dir)
	seedStaleVPCState(t, cfgPath)

	cleanup := installFakeRefreshProvider(t, map[string]map[string]any{
		"uuid-1": {"ip_range": "10.0.0.0/16", "id": "uuid-1"},
	})
	defer cleanup()

	if _, err := captureStdout(t, func() error {
		return runInfraApply([]string{"--auto-approve", "-c", cfgPath})
	}); err != nil {
		t.Fatalf("runInfraApply: %v", err)
	}

	got := loadVPCStateOutputs(t, cfgPath)
	if _, ok := got["id"]; ok {
		t.Errorf("default-off pre-refresh should not populate 'id'; got %v", got)
	}
}

// TestApply_PreStepRefresh_EnvVarFalseyValueDisables pins the
// strconv.ParseBool semantic: "0" / "false" / "off" don't enable the
// pre-step. Operators routinely set env vars to "0" to disable a
// feature; without ParseBool that would be a foot-gun (any non-empty
// value enables, including "0"). One representative falsey value is
// enough — strconv.ParseBool's accept set is well-tested upstream.
func TestApply_PreStepRefresh_EnvVarFalseyValueDisables(t *testing.T) {
	t.Setenv("WFCTL_REFRESH_OUTPUTS", "0")
	dir := t.TempDir()
	cfgPath := writeApplyPreRefreshConfig(t, dir)
	seedStaleVPCState(t, cfgPath)

	cleanup := installFakeRefreshProvider(t, map[string]map[string]any{
		"uuid-1": {"ip_range": "10.0.0.0/16", "id": "uuid-1"},
	})
	defer cleanup()

	if _, err := captureStdout(t, func() error {
		return runInfraApply([]string{"--auto-approve", "-c", cfgPath})
	}); err != nil {
		t.Fatalf("runInfraApply: %v", err)
	}

	got := loadVPCStateOutputs(t, cfgPath)
	if _, ok := got["id"]; ok {
		t.Errorf("WFCTL_REFRESH_OUTPUTS=0 must disable pre-refresh; got %v", got)
	}
}

// TestApply_PreStepRefresh_SkipFlagOverridesEnvVar verifies that
// --skip-refresh trumps WFCTL_REFRESH_OUTPUTS. Operators need a way to
// disable the pre-step in the same invocation that has the env var set
// (for example, when a CI environment forces it on globally).
func TestApply_PreStepRefresh_SkipFlagOverridesEnvVar(t *testing.T) {
	t.Setenv("WFCTL_REFRESH_OUTPUTS", "1")
	dir := t.TempDir()
	cfgPath := writeApplyPreRefreshConfig(t, dir)
	seedStaleVPCState(t, cfgPath)

	cleanup := installFakeRefreshProvider(t, map[string]map[string]any{
		"uuid-1": {"ip_range": "10.0.0.0/16", "id": "uuid-1"},
	})
	defer cleanup()

	if _, err := captureStdout(t, func() error {
		return runInfraApply([]string{"--auto-approve", "--skip-refresh", "-c", cfgPath})
	}); err != nil {
		t.Fatalf("runInfraApply: %v", err)
	}

	got := loadVPCStateOutputs(t, cfgPath)
	if _, ok := got["id"]; ok {
		t.Errorf("--skip-refresh should suppress refresh even with env var set; got %v", got)
	}
}
