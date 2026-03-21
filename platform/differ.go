package platform

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// ComputePlan compares desired ResourceSpecs against current ResourceStates
// and returns a Plan with the minimal set of ordered actions needed to
// reconcile them. Creates and updates are ordered by DependsOn (dependencies
// first); deletes are ordered in reverse dependency order.
func ComputePlan(desired []interfaces.ResourceSpec, current []interfaces.ResourceState) interfaces.Plan {
	// Index current state by resource name.
	currentMap := make(map[string]interfaces.ResourceState, len(current))
	for _, rs := range current {
		currentMap[rs.Name] = rs
	}

	// Index desired specs by name for delete detection.
	desiredMap := make(map[string]interfaces.ResourceSpec, len(desired))
	for _, spec := range desired {
		desiredMap[spec.Name] = spec
	}

	var creates, updates, deletes []interfaces.PlanAction

	// Creates and updates: iterate desired.
	for _, spec := range desired {
		hash := configHash(spec.Config)
		if rs, exists := currentMap[spec.Name]; !exists {
			creates = append(creates, interfaces.PlanAction{
				Action:   "create",
				Resource: spec,
			})
		} else if rs.ConfigHash != hash {
			current := rs
			updates = append(updates, interfaces.PlanAction{
				Action:   "update",
				Resource: spec,
				Current:  &current,
			})
		}
		// No change: skip.
	}

	// Deletes: resources in current that are not in desired.
	for _, rs := range current {
		if _, exists := desiredMap[rs.Name]; !exists {
			// Convert ResourceState to a minimal ResourceSpec for the action.
			spec := interfaces.ResourceSpec{
				Name:      rs.Name,
				Type:      rs.Type,
				DependsOn: rs.Dependencies,
			}
			deletes = append(deletes, interfaces.PlanAction{
				Action:   "delete",
				Resource: spec,
				Current:  func() *interfaces.ResourceState { c := rs; return &c }(),
			})
		}
	}

	// Topological sort: creates and updates in dependency order (deps first).
	sorted := topoSort(creates, updates, desired)

	// Deletes in reverse dependency order (dependents deleted before deps).
	sortedDeletes := reverseTopoSort(deletes)

	actions := append(sorted, sortedDeletes...)

	return interfaces.Plan{
		ID:        planID(),
		Actions:   actions,
		CreatedAt: time.Now().UTC(),
	}
}

// configHash returns a deterministic SHA-256 hex hash of a config map.
func configHash(config map[string]any) string {
	if len(config) == 0 {
		return ""
	}
	data, _ := json.Marshal(config)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// planID generates a simple unique plan ID based on current time.
func planID() string {
	return fmt.Sprintf("plan-%d", time.Now().UnixNano())
}

// topoSort returns creates and updates ordered so that a resource's
// dependencies appear before itself. Resources with no DependsOn come first.
// The desiredSpecs slice is used to build the full dependency graph.
func topoSort(creates, updates []interfaces.PlanAction, desiredSpecs []interfaces.ResourceSpec) []interfaces.PlanAction {
	// Build a map of name → DependsOn from desired specs.
	deps := make(map[string][]string, len(desiredSpecs))
	for _, s := range desiredSpecs {
		deps[s.Name] = s.DependsOn
	}

	// Collect all actions into a map by resource name.
	actionMap := make(map[string]interfaces.PlanAction)
	for _, a := range creates {
		actionMap[a.Resource.Name] = a
	}
	for _, a := range updates {
		actionMap[a.Resource.Name] = a
	}

	visited := make(map[string]bool)
	var result []interfaces.PlanAction

	var visit func(name string)
	visit = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		for _, dep := range deps[name] {
			visit(dep)
		}
		if action, ok := actionMap[name]; ok {
			result = append(result, action)
		}
	}

	for name := range actionMap {
		visit(name)
	}

	return result
}

// reverseTopoSort returns deletes in reverse dependency order so that
// dependent resources are deleted before the resources they depend on.
func reverseTopoSort(deletes []interfaces.PlanAction) []interfaces.PlanAction {
	if len(deletes) == 0 {
		return nil
	}

	// Build deps map from DependsOn on the resource spec.
	deps := make(map[string][]string, len(deletes))
	actionMap := make(map[string]interfaces.PlanAction, len(deletes))
	for _, a := range deletes {
		deps[a.Resource.Name] = a.Resource.DependsOn
		actionMap[a.Resource.Name] = a
	}

	// Forward topo sort (same as creates).
	visited := make(map[string]bool)
	var forward []interfaces.PlanAction

	var visit func(name string)
	visit = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		for _, dep := range deps[name] {
			visit(dep)
		}
		if action, ok := actionMap[name]; ok {
			forward = append(forward, action)
		}
	}

	for name := range actionMap {
		visit(name)
	}

	// Reverse the order: deps-first → dependents-first for deletion.
	result := make([]interfaces.PlanAction, len(forward))
	for i, a := range forward {
		result[len(forward)-1-i] = a
	}
	return result
}
