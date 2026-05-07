package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// parseIncludeFlag turns a comma-separated --include=<csv> value into a
// name-set. Empty input → nil (back-compat all-resources behavior;
// indistinguishable from the flag never being passed). Whitespace around
// names is trimmed.
func parseIncludeFlag(raw string) map[string]struct{} {
	if raw == "" {
		return nil
	}
	out := map[string]struct{}{}
	for _, name := range strings.Split(raw, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out[name] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// validateIncludeSet returns an error if any name in the include set is not
// declared in either specs or states. Resources can be in either side
// (state-only resource is eligible for delete; spec-only resource is
// eligible for create); a name in NEITHER is an operator typo.
func validateIncludeSet(include map[string]struct{}, specs []interfaces.ResourceSpec, states []interfaces.ResourceState) error {
	if len(include) == 0 {
		return nil
	}
	known := map[string]struct{}{}
	for i := range specs {
		known[specs[i].Name] = struct{}{}
	}
	for i := range states {
		known[states[i].Name] = struct{}{}
	}
	var missing []string
	for name := range include {
		if _, ok := known[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing) // deterministic order for stable CLI output
		return fmt.Errorf("--include: %d resource(s) not declared in config or state: %s",
			len(missing), strings.Join(missing, ", "))
	}
	return nil
}

// filterSpecsByInclude returns the subset of specs whose names are in
// include. Pass nil include to return inputs unchanged (back-compat).
func filterSpecsByInclude(specs []interfaces.ResourceSpec, include map[string]struct{}) []interfaces.ResourceSpec {
	if include == nil {
		return specs
	}
	out := make([]interfaces.ResourceSpec, 0, len(include))
	for i := range specs {
		if _, ok := include[specs[i].Name]; ok {
			out = append(out, specs[i])
		}
	}
	return out
}

// filterStatesByInclude returns the subset of states whose names are in
// include. Pass nil include to return inputs unchanged (back-compat).
func filterStatesByInclude(states []interfaces.ResourceState, include map[string]struct{}) []interfaces.ResourceState {
	if include == nil {
		return states
	}
	out := make([]interfaces.ResourceState, 0, len(include))
	for i := range states {
		if _, ok := include[states[i].Name]; ok {
			out = append(out, states[i])
		}
	}
	return out
}

// currentApplyIncludeFlag is the per-invocation --include flag value.
// Set by runInfraApply from the flag value; reset to "" after each
// invocation so the filter fails open (all-resources) on subsequent
// invocations that do not pass the flag. Same pattern as applyAllowReplaceSet.
var currentApplyIncludeFlag string
