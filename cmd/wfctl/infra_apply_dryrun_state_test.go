package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInfraApplyDryRun_RejectsStateLoadErrors(t *testing.T) {
	for _, format := range []string{"table", "json"} {
		for _, tc := range []struct {
			name string
			want string
		}{
			{name: "malformed-record", want: "parse state"},
			{name: "directory-is-file", want: "list state"},
			{name: "unknown-backend", want: "unknown iac.state backend"},
		} {
			t.Run(format+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				backend := "filesystem"
				switch tc.name {
				case "malformed-record":
					if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0o600); err != nil {
						t.Fatal(err)
					}
				case "directory-is-file":
					dir = filepath.Join(dir, "not-a-directory")
					if err := os.WriteFile(dir, []byte("ordinary file"), 0o600); err != nil {
						t.Fatal(err)
					}
				case "unknown-backend":
					backend = "unknown"
				}
				cfgPath := writeDryRunStateConfig(t, fmt.Sprintf("  - name: local-state\n    type: iac.state\n    config:\n      backend: %s\n      directory: %q\n", backend, dir))
				pluginDir := t.TempDir()
				out, err := captureStdout(t, func() error {
					return runInfraApply([]string{"--dry-run", "--format", format, "--config", cfgPath, "--plugin-dir", pluginDir})
				})
				if err == nil {
					t.Error("expected state-load failure, got success")
				} else if !strings.Contains(err.Error(), "load current state") || !strings.Contains(err.Error(), tc.want) {
					t.Errorf("error = %v, want load current state context and %q", err, tc.want)
				}
				if out != "" {
					t.Errorf("state-load failure emitted a plan: %s", out)
				}
			})
		}
	}
}

func TestInfraApplyDryRun_EmptyStatePlansCreates(t *testing.T) {
	for _, format := range []string{"table", "json"} {
		for _, state := range []string{"empty-directory", "absent-directory", "memory", "no-state-module"} {
			t.Run(format+"/"+state, func(t *testing.T) {
				dir := t.TempDir()
				backend := "filesystem"
				if state == "absent-directory" {
					dir = filepath.Join(dir, "absent")
				} else if state == "memory" {
					backend = "memory"
				}
				stateModule := fmt.Sprintf("  - name: local-state\n    type: iac.state\n    config:\n      backend: %s\n      directory: %q\n", backend, dir)
				if state == "no-state-module" {
					stateModule = ""
				}
				cfgPath := writeDryRunStateConfig(t, stateModule)
				pluginDir := t.TempDir()
				out, err := captureStdout(t, func() error {
					return runInfraApply([]string{"--dry-run", "--format", format, "--config", cfgPath, "--plugin-dir", pluginDir})
				})
				if err != nil {
					t.Fatalf("dry-run on empty state: %v", err)
				}
				if format == "json" {
					var plan DryRunApplyPlan
					if err := json.Unmarshal([]byte(out), &plan); err != nil {
						t.Fatalf("parse dry-run output: %v", err)
					}
					if plan.Summary.Creates != 1 || len(plan.Actions) != 1 || plan.Actions[0].Action != "create" || plan.Actions[0].ResourceName != "synthetic-network" {
						t.Errorf("empty-state plan = %+v, want one create for synthetic-network", plan)
					}
				} else if !strings.Contains(out, "Plan: 1 to create, 0 to update, 0 to replace, 0 to destroy.") || !strings.Contains(out, "synthetic-network") {
					t.Errorf("empty-state output = %q, want one create for synthetic-network", out)
				}
			})
		}
	}
}

// No iac.provider is declared: these tests exercise the supported local
// ConfigHash planner without loading or starting any provider plugin.
func writeDryRunStateConfig(t *testing.T, stateModule string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "infra.yaml")
	contents := "modules:\n" + stateModule + `  - name: synthetic-network
    type: infra.vpc
    config:
      cidr: "10.0.0.0/16"
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
