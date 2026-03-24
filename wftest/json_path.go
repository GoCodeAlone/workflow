package wftest

import (
	"encoding/json"
	"fmt"
	"strings"
)

// JSONPath traverses a JSON body using a dot-separated path (e.g., "user.name").
// Returns the value at the path, or an error if the path cannot be traversed.
func JSONPath(body []byte, path string) (any, error) {
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("JSON path %q: invalid JSON body: %w", path, err)
	}
	parts := strings.Split(path, ".")
	current := root
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("JSON path %q: cannot traverse into non-object at %q", path, part)
		}
		current, ok = m[part]
		if !ok {
			return nil, fmt.Errorf("JSON path %q: key %q not found", path, part)
		}
	}
	return current, nil
}

// IsJSONEmpty reports whether a JSON value should be considered empty.
// A value is empty if it is nil, an empty string, an empty slice, or an empty map.
func IsJSONEmpty(val any) bool {
	if val == nil {
		return true
	}
	switch v := val.(type) {
	case string:
		return v == ""
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	}
	return false
}
