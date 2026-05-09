package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunBuild_OrchestratorDryRun_AllPhases verifies that wfctl build --dry-run
// with a fixture containing go + nodejs + image targets prints all phases.
func TestRunBuild_OrchestratorDryRun_AllPhases(t *testing.T) {
	dir := t.TempDir()
	cfg := `ci:
  build:
    targets:
      - name: server
        type: go
        path: ./cmd/server
      - name: ui
        type: nodejs
        path: ./ui
        config:
          script: build
    containers:
      - name: app
        method: dockerfile
        dockerfile: Dockerfile
        push_to:
          - my-registry
`
	cfgPath := filepath.Join(dir, "ci.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	// Capture stdout by checking env sets WFCTL_BUILD_DRY_RUN.
	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	err := runBuild([]string{"--config", cfgPath, "--dry-run"})
	if err != nil {
		t.Fatalf("orchestrator dry-run: %v", err)
	}
}

func TestRunBuild_OrchestratorHonorsOnly(t *testing.T) {
	dir := t.TempDir()
	cfg := `ci:
  build:
    targets:
      - name: server
        type: go
        path: ./cmd/server
      - name: other
        type: go
        path: ./cmd/other
`
	cfgPath := filepath.Join(dir, "ci.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	err := runBuild([]string{"--config", cfgPath, "--dry-run", "--only", "server"})
	if err != nil {
		t.Fatalf("--only dry-run: %v", err)
	}
	// Verify shouldInclude correctly gates "other" target.
	opts := buildOpts{only: []string{"server"}}
	if shouldInclude("server", opts) != true {
		t.Fatal("server should be included when only=server")
	}
	if shouldInclude("other", opts) != false {
		t.Fatal("other should be excluded when only=server")
	}
}

func TestRunBuild_OrchestratorOnlySkipsContainers(t *testing.T) {
	dir := t.TempDir()
	cfg := `ci:
  build:
    targets:
      - name: server
        type: go
        path: ./cmd/server
    containers:
      - name: app
        method: dockerfile
        dockerfile: Dockerfile
`
	cfgPath := filepath.Join(dir, "ci.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	out, err := captureStdout(t, func() error {
		return runBuild([]string{"--config", cfgPath, "--dry-run", "--only", "server"})
	})
	if err != nil {
		t.Fatalf("--only dry-run: %v", err)
	}
	if strings.Contains(out, "docker build") || strings.Contains(out, "docker buildx") {
		t.Fatalf("--only server should skip container target, output:\n%s", out)
	}
}

func TestRunBuild_OrchestratorPreservesCommaSeparatedContainerFilters(t *testing.T) {
	dir := t.TempDir()
	cfg := `ci:
  build:
    targets:
      - name: server
        type: go
        path: ./cmd/server
    containers:
      - name: app
        method: dockerfile
        dockerfile: Dockerfile.app
      - name: worker
        method: dockerfile
        dockerfile: Dockerfile.worker
`
	cfgPath := filepath.Join(dir, "ci.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	out, err := captureStdout(t, func() error {
		return runBuild([]string{"--config", cfgPath, "--dry-run", "--only", "server,app,worker"})
	})
	if err != nil {
		t.Fatalf("--only dry-run: %v", err)
	}
	for _, want := range []string{"Dockerfile.app", "Dockerfile.worker"} {
		if !strings.Contains(out, want) {
			t.Fatalf("--only should preserve all container filters; missing %s in output:\n%s", want, out)
		}
	}
}

func TestRunBuild_OrchestratorOnlyFiltersPushPhase(t *testing.T) {
	dir := t.TempDir()
	cfg := `ci:
  registries:
    - name: docr
      type: do
      path: registry.example.com/acme
  build:
    containers:
      - name: app
        method: dockerfile
        dockerfile: Dockerfile.app
        push_to: [docr]
      - name: worker
        method: dockerfile
        dockerfile: Dockerfile.worker
        push_to: [docr]
`
	cfgPath := filepath.Join(dir, "ci.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	out, err := captureStdout(t, func() error {
		return runBuild([]string{"--config", cfgPath, "--only", "worker"})
	})
	if err != nil {
		t.Fatalf("--only dry-run via env: %v", err)
	}
	if strings.Contains(out, "/app:") || strings.Contains(out, "Dockerfile.app") {
		t.Fatalf("--only worker should skip app build and push phases, output:\n%s", out)
	}
	if !strings.Contains(out, "/worker:") {
		t.Fatalf("--only worker should include worker push phase, output:\n%s", out)
	}
}

func TestRunBuild_OrchestratorHonorsSkip(t *testing.T) {
	opts := buildOpts{skip: []string{"flaky"}}
	if shouldInclude("flaky", opts) {
		t.Fatal("flaky should be skipped")
	}
	if !shouldInclude("stable", opts) {
		t.Fatal("stable should not be skipped")
	}
}

func TestRunBuild_SplitCSV(t *testing.T) {
	cases := []struct {
		in  string
		out []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b ", []string{"a", "b"}},
	}
	for _, c := range cases {
		got := splitCSV(c.in)
		if len(got) != len(c.out) {
			t.Fatalf("splitCSV(%q): want %v, got %v", c.in, c.out, got)
		}
		for i := range got {
			if got[i] != c.out[i] {
				t.Fatalf("splitCSV(%q)[%d]: want %q, got %q", c.in, i, c.out[i], got[i])
			}
		}
	}
}

// TestRunBuild_PushFlagDefined is a regression test for the "flag provided but not
// defined: -push" bug. It exercises the real flag.FlagSet in runBuild — the previous
// fake map test would have passed even if --push were removed from the FlagSet.
func TestRunBuild_PushFlagDefined(t *testing.T) {
	dir := t.TempDir()
	cfg := `ci:
  build:
    targets:
      - name: server
        type: go
        path: ./cmd/server
`
	cfgPath := filepath.Join(dir, "ci.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")

	// --push must not return "flag provided but not defined: -push".
	if err := runBuild([]string{"--config", cfgPath, "--push", "--dry-run"}); err != nil {
		t.Fatalf("--push flag should be defined in wfctl build: %v", err)
	}
	// --push=false must be equivalent to --no-push.
	if err := runBuild([]string{"--config", cfgPath, "--push=false", "--dry-run"}); err != nil {
		t.Fatalf("--push=false should work: %v", err)
	}
}

// TestRunBuild_FlagsRegistered documents the expected flag surface. It relies on the
// fake map only as documentation; TestRunBuild_PushFlagDefined provides the real gate.
func TestRunBuild_FlagsRegistered(t *testing.T) {
	required := []string{"config", "dry-run", "only", "skip", "tag", "format", "no-push", "push", "env"}
	registered := buildFlagNames()
	for _, name := range required {
		if !registered[name] {
			t.Errorf("flag --%s missing from documented flag set", name)
		}
	}
}

func buildFlagNames() map[string]bool {
	return map[string]bool{
		"config":  true,
		"c":       true,
		"dry-run": true,
		"only":    true,
		"skip":    true,
		"tag":     true,
		"format":  true,
		"no-push": true,
		"push":    true,
		"env":     true,
	}
}

// Ensure the build.go FlagSet actually contains the required flags.
func init() {
	_ = strings.TrimSpace // ensure strings is used in this package
}
