package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

// templatePattern matches {{ outputs.name.key }} and {{ secrets.key }} expressions.
var templatePattern = regexp.MustCompile(`\{\{\s*(outputs\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_]+|secrets\.[a-zA-Z0-9_]+)\s*\}\}`)

// resolveOutputTemplates replaces template expressions in spec.Config string values.
// outputs is a map of resource name → ResourceOutput. provider may be nil.
func resolveOutputTemplates(spec *interfaces.ResourceSpec, outputs map[string]*interfaces.ResourceOutput, provider secrets.Provider) error {
	if spec == nil || len(spec.Config) == 0 {
		return nil
	}
	var resolveErr error
	resolved := resolveMap(spec.Config, outputs, provider, &resolveErr)
	if resolveErr != nil {
		return resolveErr
	}
	spec.Config = resolved
	return nil
}

func resolveMap(m map[string]any, outputs map[string]*interfaces.ResourceOutput, provider secrets.Provider, errOut *error) map[string]any {
	if *errOut != nil {
		return m
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = resolveValue(v, outputs, provider, errOut)
	}
	return result
}

func resolveValue(v any, outputs map[string]*interfaces.ResourceOutput, provider secrets.Provider, errOut *error) any {
	if *errOut != nil {
		return v
	}
	switch val := v.(type) {
	case string:
		return templatePattern.ReplaceAllStringFunc(val, func(match string) string {
			if *errOut != nil {
				return match
			}
			inner := strings.TrimSpace(match[2 : len(match)-2])
			parts := strings.SplitN(inner, ".", 3)
			switch parts[0] {
			case "outputs":
				if len(parts) != 3 {
					return match
				}
				name, key := parts[1], parts[2]
				out, ok := outputs[name]
				if !ok || out == nil {
					*errOut = fmt.Errorf("template: resource %q output not found", name)
					return match
				}
				val, ok := out.Outputs[key]
				if !ok {
					*errOut = fmt.Errorf("template: output key %q not found on resource %q", key, name)
					return match
				}
				return fmt.Sprintf("%v", val)
			case "secrets":
				if len(parts) != 2 || provider == nil {
					*errOut = fmt.Errorf("template: secrets provider not configured for %q", match)
					return match
				}
				secret, err := provider.Get(context.Background(), parts[1])
				if err != nil {
					*errOut = fmt.Errorf("template: resolve secret %q: %w", parts[1], err)
					return match
				}
				return secret
			default:
				return match
			}
		})
	case map[string]any:
		return resolveMap(val, outputs, provider, errOut)
	case []any:
		result := make([]any, len(val))
		for i, elem := range val {
			result[i] = resolveValue(elem, outputs, provider, errOut)
		}
		return result
	default:
		return v
	}
}

// inferDependencies scans specs for {{ outputs.name.* }} references and adds
// implicit DependsOn entries. Returns a new slice with updated specs.
func inferDependencies(specs []interfaces.ResourceSpec) []interfaces.ResourceSpec {
	result := make([]interfaces.ResourceSpec, len(specs))
	copy(result, specs)
	for i := range result {
		deps := extractOutputDeps(result[i].Config)
		for _, dep := range deps {
			if !containsString(result[i].DependsOn, dep) {
				result[i].DependsOn = append(result[i].DependsOn, dep)
			}
		}
	}
	return result
}

func extractOutputDeps(m map[string]any) []string {
	seen := map[string]struct{}{}
	var deps []string
	walkValues(m, func(s string) {
		templatePattern.ReplaceAllStringFunc(s, func(match string) string {
			inner := strings.TrimSpace(match[2 : len(match)-2])
			parts := strings.SplitN(inner, ".", 3)
			if len(parts) == 3 && parts[0] == "outputs" {
				if _, ok := seen[parts[1]]; !ok {
					seen[parts[1]] = struct{}{}
					deps = append(deps, parts[1])
				}
			}
			return match
		})
	})
	return deps
}

func walkValues(v any, fn func(string)) {
	switch val := v.(type) {
	case string:
		fn(val)
	case map[string]any:
		for _, v2 := range val {
			walkValues(v2, fn)
		}
	case []any:
		for _, elem := range val {
			walkValues(elem, fn)
		}
	}
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// topoSort returns specs in dependency order. Returns an error on circular deps.
func topoSort(specs []interfaces.ResourceSpec) ([]interfaces.ResourceSpec, error) {
	index := make(map[string]int, len(specs))
	for i, s := range specs {
		index[s.Name] = i
	}

	const (
		stateUnvisited = 0
		stateVisiting  = 1
		stateVisited   = 2
	)
	state := make([]int, len(specs))
	var order []interfaces.ResourceSpec

	var visit func(i int) error
	visit = func(i int) error {
		if state[i] == stateVisited {
			return nil
		}
		if state[i] == stateVisiting {
			return fmt.Errorf("circular dependency detected involving %q", specs[i].Name)
		}
		state[i] = stateVisiting
		for _, dep := range specs[i].DependsOn {
			j, ok := index[dep]
			if !ok {
				continue // external dependency, skip
			}
			if err := visit(j); err != nil {
				return err
			}
		}
		state[i] = stateVisited
		order = append(order, specs[i])
		return nil
	}

	for i := range specs {
		if err := visit(i); err != nil {
			return nil, err
		}
	}
	return order, nil
}
