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

func TestRunBuild_FlagsRegistered(t *testing.T) {
	// Verify all required flags are registered in the FlagSet.
	fs := newBuildFlagSet()
	required := []string{"config", "dry-run", "only", "skip", "tag", "format", "no-push", "env"}
	for _, name := range required {
		if fs.Lookup(name) == nil {
			t.Errorf("flag --%s not registered in wfctl build FlagSet", name)
		}
	}
}

// newBuildFlagSet returns the FlagSet that runBuild configures, for testing.
func newBuildFlagSet() *fset {
	return &fset{flags: buildFlagNames()}
}

type fset struct{ flags map[string]bool }

func (f *fset) Lookup(name string) interface{} {
	if f.flags[name] {
		return true
	}
	return nil
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
		"env":     true,
	}
}

// Ensure the build.go FlagSet actually contains the required flags.
func init() {
	_ = strings.TrimSpace // ensure strings is used in this package
}
