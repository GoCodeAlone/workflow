package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/platform"
)

func TestRunInfraTestFile_HermeticWithProviderModule(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	writeInfraTestFile(t, cfgPath, `
modules:
  - name: do
    type: iac.provider
    config:
      provider: digitalocean
      token: ${DO_TOKEN}
  - name: network
    type: infra.vpc
    config:
      provider: do
      cidr: 10.10.0.0/16
  - name: subnet-a
    type: infra.subnet
    config:
      provider: do
      cidr: 10.10.1.0/24
      depends_on: [network]
  - name: subnet-b
    type: infra.subnet
    config:
      provider: do
      cidr: 10.10.2.0/24
      depends_on: [network]
`)
	testPath := filepath.Join(dir, "infra_test.yaml")
	writeInfraTestFile(t, testPath, `
config: infra.yaml
expect:
  resources_count: 3
  resources:
    - name: network
      type: infra.vpc
      config:
        cidr: 10.10.0.0/16
    - name: subnet-a
      type: infra.subnet
      depends_on: [network]
  provider_inputs:
    resources:
      - name: subnet-b
        config:
          provider: do
          cidr: 10.10.2.0/24
  plan:
    action_counts:
      create: 3
    actions:
      - action: create
        resource:
          name: network
          type: infra.vpc
      - action: create
        resource:
          name: subnet-a
          config:
            cidr: 10.10.1.0/24
`)

	result, err := runInfraTestFile(testPath)
	if err != nil {
		t.Fatalf("runInfraTestFile: %v", err)
	}
	if result.Resources != 3 || result.Actions != 3 {
		t.Fatalf("result = %+v, want 3 resources and 3 actions", result)
	}
}

func TestRunInfraTestFile_FailsOnPlanMismatch(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	hash := platform.ConfigHash(map[string]any{"engine": "postgres"})
	writeInfraTestFile(t, cfgPath, `
modules:
  - name: db
    type: infra.database
    config:
      engine: mysql
`)
	testPath := filepath.Join(dir, "infra_test.yaml")
	writeInfraTestFile(t, testPath, `
config: infra.yaml
current_state:
  - name: db
    type: infra.database
    config_hash: `+hash+`
    applied_config:
      engine: postgres
expect:
  plan:
    action_counts:
      create: 1
`)

	_, err := runInfraTestFile(testPath)
	if err == nil {
		t.Fatal("expected plan mismatch error")
	}
	if !strings.Contains(err.Error(), "plan action count for create") {
		t.Fatalf("error = %v, want action count mismatch", err)
	}
}

func writeInfraTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.TrimPrefix(content, "\n")), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
