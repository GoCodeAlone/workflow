package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/platform"
)

// --- parseInfraResourceSpecs tests ---

func TestParseInfraResourceSpecs_ExtractsInfraModules(t *testing.T) {
	yaml := `
modules:
  - name: cloud
    type: iac.provider
    config:
      provider: aws
  - name: state
    type: iac.state
    config:
      backend: filesystem
  - name: network
    type: infra.vpc
    config:
      cidr: "10.0.0.0/16"
  - name: db
    type: infra.database
    config:
      engine: postgres
      size: m
`
	f, err := writeTempYAML(t, yaml)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f)

	specs, err := parseInfraResourceSpecs(f)
	if err != nil {
		t.Fatalf("parseInfraResourceSpecs: %v", err)
	}

	if len(specs) != 2 {
		t.Fatalf("expected 2 infra.* specs, got %d", len(specs))
	}

	names := make(map[string]bool)
	for _, s := range specs {
		names[s.Name] = true
		if !strings.HasPrefix(s.Type, "infra.") {
			t.Errorf("spec %q has unexpected type %q", s.Name, s.Type)
		}
	}
	if !names["network"] {
		t.Error("expected spec for 'network'")
	}
	if !names["db"] {
		t.Error("expected spec for 'db'")
	}
}

func TestParseInfraResourceSpecs_ExtractsSizeFromConfig(t *testing.T) {
	yaml := `
modules:
  - name: db
    type: infra.database
    config:
      size: l
      engine: postgres
`
	f, err := writeTempYAML(t, yaml)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f)

	specs, err := parseInfraResourceSpecs(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	if specs[0].Size != interfaces.SizeL {
		t.Errorf("size = %q, want %q", specs[0].Size, interfaces.SizeL)
	}
}

func TestParseInfraResourceSpecs_ExtractsDependsOn(t *testing.T) {
	yaml := `
modules:
  - name: db
    type: infra.database
    config:
      depends_on: [network, cache]
      engine: postgres
`
	f, err := writeTempYAML(t, yaml)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f)

	specs, err := parseInfraResourceSpecs(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs[0].DependsOn) != 2 {
		t.Errorf("depends_on len = %d, want 2", len(specs[0].DependsOn))
	}
}

// --- formatPlanTable tests ---

func TestFormatPlanTable_ShowsAllActions(t *testing.T) {
	plan := makeMixedPlan()
	out := formatPlanTable(plan)

	if !strings.Contains(out, "create") {
		t.Error("expected 'create' in table output")
	}
	if !strings.Contains(out, "update") {
		t.Error("expected 'update' in table output")
	}
	if !strings.Contains(out, "delete") {
		t.Error("expected 'delete' in table output")
	}
}

func TestFormatPlanTable_EmptyPlanMessage(t *testing.T) {
	plan := interfaces.IaCPlan{Actions: nil}
	out := formatPlanTable(plan)
	if !strings.Contains(out, "No changes") {
		t.Errorf("expected 'No changes' for empty plan, got: %q", out)
	}
}

// --- formatPlanMarkdown tests ---

func TestFormatPlanMarkdown_ContainsTable(t *testing.T) {
	plan := makeMixedPlan()
	out := formatPlanMarkdown(plan)

	if !strings.Contains(out, "|") {
		t.Error("expected markdown table pipes")
	}
	if !strings.Contains(out, "create") || !strings.Contains(out, "update") || !strings.Contains(out, "delete") {
		t.Error("expected all action types in markdown output")
	}
}

func TestFormatPlanMarkdown_EmptyPlanMessage(t *testing.T) {
	plan := interfaces.IaCPlan{Actions: nil}
	out := formatPlanMarkdown(plan)
	if !strings.Contains(out, "No changes") {
		t.Errorf("expected 'No changes' for empty plan, got: %q", out)
	}
}

// --- plan JSON output tests ---

func TestWritePlanJSON_RoundTrip(t *testing.T) {
	plan := makeMixedPlan()

	f, err := os.CreateTemp(t.TempDir(), "plan-*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	if err := writePlanJSON(plan, f.Name()); err != nil {
		t.Fatalf("writePlanJSON: %v", err)
	}

	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	var restored interfaces.IaCPlan
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(restored.Actions) != len(plan.Actions) {
		t.Errorf("actions count: got %d, want %d", len(restored.Actions), len(plan.Actions))
	}
}

// --- ComputePlan integration via the differ ---

func TestRunInfraPlan_EmptyState_AllCreates(t *testing.T) {
	yaml := `
modules:
  - name: network
    type: infra.vpc
    config:
      cidr: "10.0.0.0/16"
  - name: db
    type: infra.database
    config:
      engine: postgres
`
	f, err := writeTempYAML(t, yaml)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f)

	specs, err := parseInfraResourceSpecs(f)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := platform.ComputePlan(specs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 2 {
		t.Fatalf("expected 2 create actions, got %d", len(plan.Actions))
	}
	for _, a := range plan.Actions {
		if a.Action != "create" {
			t.Errorf("action = %q, want 'create'", a.Action)
		}
	}
}

// --- helpers ---

func writeTempYAML(t *testing.T, content string) (string, error) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "infra-*.yaml")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return "", err
	}
	return f.Name(), f.Close()
}

func makeMixedPlan() interfaces.IaCPlan {
	return interfaces.IaCPlan{
		ID:        "plan-test",
		CreatedAt: time.Now(),
		Actions: []interfaces.PlanAction{
			{Action: "create", Resource: interfaces.ResourceSpec{Name: "network", Type: "infra.vpc"}},
			{Action: "update", Resource: interfaces.ResourceSpec{Name: "db", Type: "infra.database"}},
			{Action: "delete", Resource: interfaces.ResourceSpec{Name: "old-cache", Type: "infra.cache"}},
		},
	}
}
