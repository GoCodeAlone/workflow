//go:build aws

package drivers

import (
	"fmt"

	"github.com/GoCodeAlone/workflow/platform"
)

// diffProperties compares current and desired property maps, returning entries
// for any fields that differ.
func diffProperties(current map[string]any, desired map[string]any) []platform.DiffEntry {
	var diffs []platform.DiffEntry
	for k, desiredVal := range desired {
		currentVal, exists := current[k]
		if !exists || fmt.Sprintf("%v", currentVal) != fmt.Sprintf("%v", desiredVal) {
			diffs = append(diffs, platform.DiffEntry{
				Path:     k,
				OldValue: currentVal,
				NewValue: desiredVal,
			})
		}
	}
	return diffs
}

// stringSliceProp extracts a string slice from a properties map.
func stringSliceProp(props map[string]any, key string) []string {
	v, ok := props[key]
	if !ok {
		return nil
	}
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		var result []string
		for _, item := range s {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	default:
		return nil
	}
}

// intPropDrivers extracts an int property with a default value.
func intPropDrivers(props map[string]any, key string, def int) int {
	v, ok := props[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return def
	}
}

// boolPropDrivers extracts a bool property with a default value.
func boolPropDrivers(props map[string]any, key string, def bool) bool {
	v, ok := props[key]
	if !ok {
		return def
	}
	b, ok := v.(bool)
	if !ok {
		return def
	}
	return b
}
