package platform

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// ComputePlan compares desired ResourceSpecs against current ResourceStates
// and returns a Plan with the minimal set of ordered actions needed to
// reconcile them. Creates and updates are ordered by DependsOn (dependencies
// first); deletes are ordered in reverse dependency order.
//
// Returns an error if the DependsOn graph contains a cycle.
func ComputePlan(desired []interfaces.ResourceSpec, current []interfaces.ResourceState) (interfaces.IaCPlan, error) {
	// Index current state by resource name.
	currentMap := make(map[string]interfaces.ResourceState, len(current))
	for i := range current {
		currentMap[current[i].Name] = current[i]
	}

	// Index desired specs by name for delete detection.
	desiredMap := make(map[string]interfaces.ResourceSpec, len(desired))
	for _, spec := range desired {
		desiredMap[spec.Name] = spec
	}

	var creates, updates, deletes []interfaces.PlanAction

	// Creates and updates: iterate desired in stable order.
	for _, spec := range desired {
		hash := configHash(spec.Config)
		if rs, exists := currentMap[spec.Name]; !exists {
			creates = append(creates, interfaces.PlanAction{
				Action:   "create",
				Resource: spec,
			})
		} else if rs.ConfigHash != hash {
			rsCopy := rs
			updates = append(updates, interfaces.PlanAction{
				Action:   "update",
				Resource: spec,
				Current:  &rsCopy,
			})
		}
		// No change: skip.
	}

	// Deletes: resources in current that are not in desired.
	for i := range current {
		rs := &current[i]
		if _, exists := desiredMap[rs.Name]; !exists {
			rsCopy := *rs
			spec := interfaces.ResourceSpec{
				Name:      rs.Name,
				Type:      rs.Type,
				DependsOn: rs.Dependencies,
			}
			deletes = append(deletes, interfaces.PlanAction{
				Action:   "delete",
				Resource: spec,
				Current:  &rsCopy,
			})
		}
	}

	// Topological sort: creates and updates in dependency order (deps first).
	sorted, err := topoSort(creates, updates, desired)
	if err != nil {
		return interfaces.IaCPlan{}, err
	}

	// Deletes in reverse dependency order (dependents deleted before deps).
	sortedDeletes, err := reverseTopoSort(deletes)
	if err != nil {
		return interfaces.IaCPlan{}, err
	}

	sorted = append(sorted, sortedDeletes...)

	return interfaces.IaCPlan{
		ID:        planID(),
		Actions:   sorted,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// ConfigHash is the exported counterpart of configHash. It allows callers
// outside the platform package (e.g. cmd/wfctl) to compute hashes that are
// byte-for-byte identical to those stored by ComputePlan, eliminating the
// risk of independent re-implementations diverging.
func ConfigHash(config map[string]any) string {
	return configHash(config)
}

// configHash returns a deterministic SHA-256 hex hash of a config map.
// Keys are explicitly sorted before marshalling so the hash is stable across
// Go's randomised map-iteration order — matching the DO plugin's pattern.
func configHash(config map[string]any) string {
	if len(config) == 0 {
		return ""
	}
	keys := make([]string, 0, len(config))
	for k := range config {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	type kv struct {
		K string
		V any
	}
	ordered := make([]kv, len(keys))
	for i, k := range keys {
		ordered[i] = kv{K: k, V: config[k]}
	}
	data, _ := json.Marshal(ordered)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// planID generates a simple unique plan ID based on current time.
func planID() string {
	return fmt.Sprintf("plan-%d", time.Now().UnixNano())
}

// topoSort returns creates and updates ordered so that a resource's
// dependencies appear before itself. Iteration order is seeded from
// desiredSpecs to ensure deterministic output for independent resources.
// Returns an error if a dependency cycle is detected.
func topoSort(creates, updates []interfaces.PlanAction, desiredSpecs []interfaces.ResourceSpec) ([]interfaces.PlanAction, error) {
	// Build a map of name → DependsOn from desired specs.
	deps := make(map[string][]string, len(desiredSpecs))
	for _, s := range desiredSpecs {
		deps[s.Name] = s.DependsOn
	}

	// Collect all actions into a map by resource name.
	actionMap := make(map[string]interfaces.PlanAction)
	for i := range creates {
		actionMap[creates[i].Resource.Name] = creates[i]
	}
	for i := range updates {
		actionMap[updates[i].Resource.Name] = updates[i]
	}

	visited := make(map[string]bool)
	inStack := make(map[string]bool) // cycle detection
	var result []interfaces.PlanAction

	var visit func(name string) error
	visit = func(name string) error {
		if inStack[name] {
			return fmt.Errorf("dependency cycle detected involving resource %q", name)
		}
		if visited[name] {
			return nil
		}
		inStack[name] = true
		for _, dep := range deps[name] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		inStack[name] = false
		visited[name] = true
		if action, ok := actionMap[name]; ok {
			result = append(result, action)
		}
		return nil
	}

	// Seed DFS from desiredSpecs to guarantee deterministic ordering.
	for _, s := range desiredSpecs {
		if _, ok := actionMap[s.Name]; ok {
			if err := visit(s.Name); err != nil {
				return nil, err
			}
		}
	}

	return result, nil
}

// reverseTopoSort returns deletes in reverse dependency order so that
// dependent resources are deleted before the resources they depend on.
// Returns an error if a dependency cycle is detected.
func reverseTopoSort(deletes []interfaces.PlanAction) ([]interfaces.PlanAction, error) {
	if len(deletes) == 0 {
		return nil, nil
	}

	// Build deps map from DependsOn on the resource spec.
	deps := make(map[string][]string, len(deletes))
	actionMap := make(map[string]interfaces.PlanAction, len(deletes))
	for i := range deletes {
		a := &deletes[i]
		deps[a.Resource.Name] = a.Resource.DependsOn
		actionMap[a.Resource.Name] = *a
	}

	visited := make(map[string]bool)
	inStack := make(map[string]bool) // cycle detection
	var forward []interfaces.PlanAction

	var visit func(name string) error
	visit = func(name string) error {
		if inStack[name] {
			return fmt.Errorf("dependency cycle detected involving resource %q", name)
		}
		if visited[name] {
			return nil
		}
		inStack[name] = true
		for _, dep := range deps[name] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		inStack[name] = false
		visited[name] = true
		if action, ok := actionMap[name]; ok {
			forward = append(forward, action)
		}
		return nil
	}

	// Seed DFS from the stable delete-action order.
	for i := range deletes {
		if err := visit(deletes[i].Resource.Name); err != nil {
			return nil, err
		}
	}

	// Reverse the order: deps-first → dependents-first for deletion.
	result := make([]interfaces.PlanAction, len(forward))
	for i := range forward {
		result[len(forward)-1-i] = forward[i]
	}
	return result, nil
}
