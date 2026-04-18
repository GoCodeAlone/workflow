package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testdataPath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", name)
}

// wfctlWithEnvSupport returns the path to a wfctl binary that supports
// --env, or skips the test if none is available.
func wfctlWithEnvSupport(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("wfctl")
	if err != nil {
		t.Skip("wfctl binary not on PATH — skipping E2E test")
	}
	// Probe for --env flag support by running --help and checking output.
	helpOut, _ := exec.Command(path, "infra", "plan", "--help").CombinedOutput()
	if !strings.Contains(string(helpOut), "env") {
		t.Skip("wfctl binary on PATH does not support --env — skipping E2E test (build and install the new binary first)")
	}
	return path
}

// TestInfraMultiEnv_E2E exercises `wfctl infra plan --env staging|prod`
// as a subprocess against the realistic two-env fixture.
// The test is skipped when the wfctl binary is not on PATH or lacks --env support.
func TestInfraMultiEnv_E2E(t *testing.T) {
	wfctlPath := wfctlWithEnvSupport(t)
	fixture := testdataPath("infra-multi-env.yaml")

	t.Run("staging plan excludes dns", func(t *testing.T) {
		out, runErr := exec.Command(wfctlPath, "infra", "plan", "--env", "staging", "--config", fixture).CombinedOutput()
		if runErr != nil {
			t.Fatalf("wfctl infra plan --env staging: %v\n%s", runErr, out)
		}
		output := string(out)
		if strings.Contains(output, "bmw-dns") {
			t.Fatalf("staging plan should not include bmw-dns (staging: null), got:\n%s", output)
		}
		if !strings.Contains(output, "bmw-database") {
			t.Fatalf("staging plan should include bmw-database, got:\n%s", output)
		}
		if !strings.Contains(output, "db-s-1vcpu-1gb") {
			t.Fatalf("staging plan should show small db size, got:\n%s", output)
		}
	})

	t.Run("prod plan includes dns with large db", func(t *testing.T) {
		out, runErr := exec.Command(wfctlPath, "infra", "plan", "--env", "prod", "--config", fixture).CombinedOutput()
		if runErr != nil {
			t.Fatalf("wfctl infra plan --env prod: %v\n%s", runErr, out)
		}
		output := string(out)
		if !strings.Contains(output, "bmw-dns") {
			t.Fatalf("prod plan should include bmw-dns, got:\n%s", output)
		}
		if !strings.Contains(output, "db-s-2vcpu-4gb") {
			t.Fatalf("prod plan should show large db size, got:\n%s", output)
		}
		if !strings.Contains(output, "bmw-app") {
			t.Fatalf("prod plan should include bmw-app, got:\n%s", output)
		}
	})
}
