package platform_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/platform"
)

// hashConfig produces a deterministic SHA-256 hex hash of a config map for test setup.
func hashConfig(config map[string]any) string {
	data, _ := json.Marshal(config)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func TestDiffer_NewResource(t *testing.T) {
	desired := []interfaces.ResourceSpec{
		{Name: "db", Type: "infra.database", Config: map[string]any{"engine": "postgres"}},
	}
	current := []interfaces.ResourceState{}

	plan := platform.ComputePlan(desired, current)

	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(plan.Actions))
	}
	if plan.Actions[0].Action != "create" {
		t.Errorf("action = %q, want %q", plan.Actions[0].Action, "create")
	}
	if plan.Actions[0].Resource.Name != "db" {
		t.Errorf("resource name = %q, want %q", plan.Actions[0].Resource.Name, "db")
	}
}

func TestDiffer_DeletedResource(t *testing.T) {
	desired := []interfaces.ResourceSpec{}
	current := []interfaces.ResourceState{
		{Name: "old-db", Type: "infra.database", ConfigHash: "abc123"},
	}

	plan := platform.ComputePlan(desired, current)

	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(plan.Actions))
	}
	if plan.Actions[0].Action != "delete" {
		t.Errorf("action = %q, want %q", plan.Actions[0].Action, "delete")
	}
	if plan.Actions[0].Resource.Name != "old-db" {
		t.Errorf("resource name = %q, want %q", plan.Actions[0].Resource.Name, "old-db")
	}
}

func TestDiffer_UpdatedResource(t *testing.T) {
	config := map[string]any{"engine": "postgres", "version": "15"}
	newConfig := map[string]any{"engine": "postgres", "version": "16"}

	desired := []interfaces.ResourceSpec{
		{Name: "db", Type: "infra.database", Config: newConfig},
	}
	current := []interfaces.ResourceState{
		{Name: "db", Type: "infra.database", ConfigHash: hashConfig(config)},
	}

	plan := platform.ComputePlan(desired, current)

	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(plan.Actions))
	}
	if plan.Actions[0].Action != "update" {
		t.Errorf("action = %q, want %q", plan.Actions[0].Action, "update")
	}
}

func TestDiffer_NoChanges(t *testing.T) {
	config := map[string]any{"engine": "postgres"}

	desired := []interfaces.ResourceSpec{
		{Name: "db", Type: "infra.database", Config: config},
	}
	current := []interfaces.ResourceState{
		{Name: "db", Type: "infra.database", ConfigHash: hashConfig(config)},
	}

	plan := platform.ComputePlan(desired, current)

	if len(plan.Actions) != 0 {
		t.Fatalf("expected 0 actions, got %d: %+v", len(plan.Actions), plan.Actions)
	}
}

func TestDiffer_DependencyOrdering(t *testing.T) {
	// app depends on db, db depends on network
	// Creates should be ordered: network → db → app
	desired := []interfaces.ResourceSpec{
		{Name: "app", Type: "infra.container_service", DependsOn: []string{"db"}},
		{Name: "db", Type: "infra.database", DependsOn: []string{"network"}},
		{Name: "network", Type: "infra.vpc"},
	}
	current := []interfaces.ResourceState{}

	plan := platform.ComputePlan(desired, current)

	if len(plan.Actions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(plan.Actions))
	}

	// Build a position map
	pos := make(map[string]int)
	for i, a := range plan.Actions {
		pos[a.Resource.Name] = i
	}

	if pos["network"] >= pos["db"] {
		t.Errorf("network (pos %d) must come before db (pos %d)", pos["network"], pos["db"])
	}
	if pos["db"] >= pos["app"] {
		t.Errorf("db (pos %d) must come before app (pos %d)", pos["db"], pos["app"])
	}
}

func TestDiffer_MixedActions(t *testing.T) {
	existingConfig := map[string]any{"size": "m"}
	updatedConfig := map[string]any{"size": "l"}

	desired := []interfaces.ResourceSpec{
		{Name: "new-svc", Type: "infra.container_service"},
		{Name: "existing-db", Type: "infra.database", Config: updatedConfig},
	}
	current := []interfaces.ResourceState{
		{Name: "existing-db", Type: "infra.database", ConfigHash: hashConfig(existingConfig)},
		{Name: "old-cache", Type: "infra.cache"},
	}

	plan := platform.ComputePlan(desired, current)

	actions := make(map[string]string) // name → action
	for _, a := range plan.Actions {
		actions[a.Resource.Name] = a.Action
	}

	if actions["new-svc"] != "create" {
		t.Errorf("new-svc action = %q, want %q", actions["new-svc"], "create")
	}
	if actions["existing-db"] != "update" {
		t.Errorf("existing-db action = %q, want %q", actions["existing-db"], "update")
	}
	if actions["old-cache"] != "delete" {
		t.Errorf("old-cache action = %q, want %q", actions["old-cache"], "delete")
	}
}
