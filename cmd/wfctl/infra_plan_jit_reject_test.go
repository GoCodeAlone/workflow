package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// expectedJITRejectError is the EXACT error string runInfraPlan must
// emit when -o is passed alongside a config that produces a
// SchemaVersion=2 (JIT-required) plan. Verbatim from the plan literal
// at docs/plans/2026-05-03-iac-conformance-and-replace.md §T5.5
// line 2104. The string is kept literal here (not built via fmt
// formatting) so any drift between the implementation and the
// operator-facing diagnostic fails this test loudly. Runbooks and
// operator search-engines may match on this exact phrase.
//
// Note: the operator's displayed error includes a leading "error: "
// prefix from cmd/wfctl/main.go's top-level error wrapper; the value
// returned by runInfraPlan (and asserted here) does NOT include that
// prefix.
const expectedJITRejectError = "this plan requires JIT resolution; persisted plan.json is not supported. Run 'wfctl infra apply' directly without -o/--plan"

// TestInfraPlan_RejectsPersistedJITPlan_WithExactErrorString is the
// canonical T5.5 scenario: a config whose env_vars carry a
// ${MODULE.field} ref (preserved through ExpandEnvInMapPreservingKeys
// and surviving into plan.Actions[*].Resource.Config) is requested to
// be persisted via -o. runInfraPlan must REJECT before writing the
// plan file, with the exact T5.5 error string. The plan file MUST NOT
// be created — operators should not have a half-state plan.json on disk.
func TestInfraPlan_RejectsPersistedJITPlan_WithExactErrorString(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: app
    type: infra.container_service
    config:
      env_vars:
        VPC_UUID: "${vpc.id}"
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	planFile := filepath.Join(dir, "plan.json")
	err := runInfraPlan([]string{"--config", cfgPath, "--output", planFile})
	if err == nil {
		t.Fatalf("expected runInfraPlan to reject persistence of JIT-style plan; got nil error")
	}
	if got := err.Error(); got != expectedJITRejectError {
		t.Errorf("error string mismatch:\n got: %q\nwant: %q", got, expectedJITRejectError)
	}
	// The plan file must NOT be present after rejection.
	if _, statErr := os.Stat(planFile); statErr == nil {
		t.Errorf("plan file was written despite rejection: %s", planFile)
	}
}

// TestInfraPlan_PermitsStdoutOnlyJITPlan verifies the inverse contract:
// a JIT-style plan WITHOUT -o is fine — the operator gets the plan
// preview on stdout and the rejection guard does NOT fire. This locks
// the design's "stdout-only is fine (operator can preview)" clause.
func TestInfraPlan_PermitsStdoutOnlyJITPlan(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: app
    type: infra.container_service
    config:
      env_vars:
        VPC_UUID: "${vpc.id}"
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := runInfraPlan([]string{"--config", cfgPath}); err != nil {
		t.Errorf("stdout-only JIT plan should not be rejected; got error: %v", err)
	}
}

// TestInfraPlan_PermitsPersistedNonJITPlan is the negative-control:
// a non-JIT plan persisted via -o still works exactly as before T5.5.
// Belt-and-suspenders that the rejection guard didn't break the V1
// happy path.
func TestInfraPlan_PermitsPersistedNonJITPlan(t *testing.T) {
	t.Setenv("STAGING_DB_PASSWORD", "secret")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: app
    type: infra.container_service
    config:
      env_vars:
        DATABASE_URL: "postgres://user:${STAGING_DB_PASSWORD}@host:5432/db"
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	planFile := filepath.Join(dir, "plan.json")
	if err := runInfraPlan([]string{"--config", cfgPath, "--output", planFile}); err != nil {
		t.Fatalf("non-JIT plan with -o should succeed; got: %v", err)
	}
	if _, statErr := os.Stat(planFile); statErr != nil {
		t.Errorf("non-JIT plan file should exist after -o; stat err: %v", statErr)
	}
}

// TestInfraPlan_RejectionErrorContainsCanonicalKeywords is a defensive
// substring assertion. If a future refactor reformats the error string
// for any reason (cycle-N reviewer feedback, e.g.) but keeps the
// keywords stable, this test still catches the canonical-vocab
// expectations. The exact-match test above is the strict contract;
// this one is the operator-search-engine safety net.
func TestInfraPlan_RejectionErrorContainsCanonicalKeywords(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: app
    type: infra.container_service
    config:
      env_vars:
        DEP: "${vpc.id}"
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	planFile := filepath.Join(dir, "plan.json")
	err := runInfraPlan([]string{"--config", cfgPath, "--output", planFile})
	if err == nil {
		t.Fatal("expected error")
	}
	keywords := []string{
		"JIT resolution",
		"persisted plan.json",
		"wfctl infra apply",
		"-o/--plan",
	}
	for _, kw := range keywords {
		if !strings.Contains(err.Error(), kw) {
			t.Errorf("error missing canonical keyword %q; got: %q", kw, err.Error())
		}
	}
}
