package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestInfraPlan_MergesTopLevelEnvVars(t *testing.T) {
	dir := t.TempDir()
	cfg := `environments:
  staging:
    provider: digitalocean
    region: nyc3
    envVars:
      LOG_LEVEL: debug
  prod:
    provider: digitalocean
    region: nyc1
    envVars:
      LOG_LEVEL: info
modules:
  - name: app
    type: infra.container_service
    config:
      image: example/app:latest
      env_vars:
        PORT: "8080"
`
	path := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := planResourcesForEnv(path, "prod")
	if err != nil {
		t.Fatal(err)
	}
	var app *config.ResolvedModule
	for _, r := range resolved {
		if r.Name == "app" {
			app = r
			break
		}
	}
	if app == nil {
		t.Fatal("app resource not found")
	}
	envVars := app.Config["env_vars"].(map[string]any)
	if envVars["LOG_LEVEL"] != "info" {
		t.Fatalf("want LOG_LEVEL=info merged from top-level, got %v", envVars["LOG_LEVEL"])
	}
	if envVars["PORT"] != "8080" {
		t.Fatalf("want PORT=8080 preserved from module, got %v", envVars["PORT"])
	}
}

func TestInfraPlan_DefaultsRegionFromTopLevel(t *testing.T) {
	dir := t.TempDir()
	cfg := `environments:
  prod:
    provider: digitalocean
    region: nyc1
modules:
  - name: db
    type: infra.database
    config:
      size: large
`
	path := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := planResourcesForEnv(path, "prod")
	if err != nil {
		t.Fatal(err)
	}
	if resolved[0].Region != "nyc1" {
		t.Fatalf("want region=nyc1 defaulted from top-level environments[prod], got %q", resolved[0].Region)
	}
}

func TestInfraPlan_ModuleRegionWinsOverTopLevel(t *testing.T) {
	dir := t.TempDir()
	cfg := `environments:
  prod:
    provider: digitalocean
    region: nyc1
modules:
  - name: db
    type: infra.database
    config:
      size: large
      region: sfo3
`
	path := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := planResourcesForEnv(path, "prod")
	if err != nil {
		t.Fatal(err)
	}
	if resolved[0].Region != "sfo3" {
		t.Fatalf("want module's own region=sfo3 to win, got %q", resolved[0].Region)
	}
}
