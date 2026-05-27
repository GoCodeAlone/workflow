package wfctlhelpers_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestOutOfSubsetMethods_Panic guards design-doc cycle-5 row 4: the
// handler library and host module use only the
// {SaveResource, GetResource, ListResources, DeleteResource, Close}
// subset. Any call to Lock / SavePlan / GetPlan on the resolved store
// MUST panic with a `wfctlhelpers:` prefix so an accidental future
// refactor (e.g. returning nil-error stubs) is loud rather than silent.
// Coverage: 3 concrete stores × 3 methods = 9 cases, table-driven.
//
// Per code-reviewer I-2.1 on commit 7a064b824.
func TestOutOfSubsetMethods_Panic(t *testing.T) {
	// Build one store of each concrete shape. *moduleStoreAdapter is
	// reachable from any non-filesystem, non-noop backend; we use the
	// memory backend via ResolveStateStore so the test exercises the
	// adapter shape that the production code path actually returns.
	memCfg := writeStateCfg(t, `modules:
  - name: iac-state
    type: iac.state
    config:
      backend: memory
`)
	memStore, err := wfctlhelpers.ResolveStateStore(memCfg, "", "")
	if err != nil {
		t.Fatalf("ResolveStateStore(memory): %v", err)
	}

	fsStore := &wfctlhelpers.FSStateStore{}
	noopStore := &wfctlhelpers.NoopStateStore{}

	stores := []struct {
		name  string
		store interfaces.IaCStateStore
	}{
		{"NoopStateStore", noopStore},
		{"FSStateStore", fsStore},
		{"moduleStoreAdapter(memory)", memStore},
	}

	cases := []struct {
		method string
		call   func(s interfaces.IaCStateStore)
	}{
		{"SavePlan", func(s interfaces.IaCStateStore) {
			_ = s.SavePlan(context.Background(), interfaces.IaCPlan{ID: "p1"})
		}},
		{"GetPlan", func(s interfaces.IaCStateStore) {
			_, _ = s.GetPlan(context.Background(), "p1")
		}},
		{"Lock", func(s interfaces.IaCStateStore) {
			_, _ = s.Lock(context.Background(), "r1", time.Second)
		}},
	}

	for _, st := range stores {
		for _, c := range cases {
			t.Run(st.name+"/"+c.method, func(t *testing.T) {
				defer func() {
					r := recover()
					if r == nil {
						t.Fatalf("expected panic from %s.%s, got nil", st.name, c.method)
					}
					msg, ok := r.(string)
					if !ok {
						t.Fatalf("expected string panic message, got %T(%v)", r, r)
					}
					if !strings.HasPrefix(msg, "wfctlhelpers: ") {
						t.Errorf("panic message %q missing `wfctlhelpers:` prefix", msg)
					}
					if !strings.Contains(msg, c.method) {
						t.Errorf("panic message %q does not name method %q", msg, c.method)
					}
					if !strings.Contains(msg, "out-of-subset") {
						t.Errorf("panic message %q missing `out-of-subset` rationale", msg)
					}
				}()
				c.call(st.store)
			})
		}
	}
}

// TestResolveStateStore_EnvOverride exercises the envName != "" branch
// (lines 47-54 of state.go) which routes through WriteEnvResolvedConfig
// and the temp-file path. The branch is the reason the host-side module
// can target per-env state backends and was untested before
// code-reviewer I-3 on commit 7a064b824.
func TestResolveStateStore_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	baseStateDir := filepath.Join(dir, "base-state")
	stagingStateDir := filepath.Join(dir, "staging-state")
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`modules:
  - name: iac-state
    type: iac.state
    config:
      backend: filesystem
      directory: `+baseStateDir+`
    environments:
      staging:
        config:
          directory: `+stagingStateDir+`
`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Pre-stage a fixture in the staging directory so we can confirm the
	// env-resolved backend really targets it (not the base directory).
	if err := os.MkdirAll(stagingStateDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagingStateDir, "vpc-staging.json"),
		[]byte(`{"resource_id":"vpc-staging","resource_type":"infra.vpc","provider":"stub","status":"active","config":{},"outputs":{},"created_at":"2026-05-27T00:00:00Z","updated_at":"2026-05-27T00:00:00Z"}`),
		0o600); err != nil {
		t.Fatal(err)
	}

	store, err := wfctlhelpers.ResolveStateStore(cfgPath, "staging", "")
	if err != nil {
		t.Fatalf("ResolveStateStore(envName=staging): %v", err)
	}
	list, err := store.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(list) != 1 || list[0].Name != "vpc-staging" {
		t.Fatalf("got %+v, want one vpc-staging resource — env-resolved backend should target stagingStateDir", list)
	}

	// Confirm the env-resolve temp file was cleaned up. The temp file lives
	// in the same dir as cfgPath with prefix `.wfctl-env-resolved-`.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".wfctl-env-resolved-") {
			t.Errorf("env-resolve temp file %q leaked — defer os.Remove failed", e.Name())
		}
	}
}

// TestResolveStateStore_EnvOverride_PropagatesError verifies the
// envName != "" branch's error-wrap context survives the lift: when
// the underlying config cannot be loaded for env resolution, the
// returned error mentions the env name so the operator can diagnose
// which environment triggered the failure.
func TestResolveStateStore_EnvOverride_PropagatesError(t *testing.T) {
	_, err := wfctlhelpers.ResolveStateStore(filepath.Join(t.TempDir(), "missing.yaml"), "staging", "")
	if err == nil {
		t.Fatal("expected error for missing config + non-empty envName, got nil")
	}
	if !strings.Contains(err.Error(), "staging") {
		t.Errorf("error %v does not mention envName 'staging' — context lost in lift", err)
	}
}

// writeStateCfg writes the supplied YAML body to a temp file and returns
// the path. Helper for invariant tests that don't need per-test config
// variation.
func writeStateCfg(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
	return path
}
