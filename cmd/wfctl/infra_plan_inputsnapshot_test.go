package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestPlanWritesInputSnapshot(t *testing.T) {
	t.Setenv("STAGING_DB_PASSWORD", "secret-value")
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
		t.Fatalf("runInfraPlan: %v", err)
	}

	data, err := os.ReadFile(planFile)
	if err != nil {
		t.Fatalf("read plan: %v", err)
	}
	var plan interfaces.IaCPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatalf("unmarshal plan: %v", err)
	}
	if plan.InputSnapshot["STAGING_DB_PASSWORD"] == "" {
		t.Errorf("plan.InputSnapshot missing STAGING_DB_PASSWORD; got %v", plan.InputSnapshot)
	}
	if got := plan.InputSnapshot["STAGING_DB_PASSWORD"]; len(got) != 16 {
		t.Errorf("fingerprint should be 16 hex chars, got %d (%q)", len(got), got)
	}
	if plan.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", plan.SchemaVersion)
	}
}
