package config

import "os"

// ExpandEnvInMap returns a deep copy of m with every string value having
// ${VAR} and $VAR references resolved via os.ExpandEnv. Nested map[string]any
// and []any values are walked recursively. Non-string values are preserved.
// Nil input returns nil.
func ExpandEnvInMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = ExpandEnvInValue(v)
	}
	return out
}

// ExpandEnvInSlice parallels ExpandEnvInMap for []any.
func ExpandEnvInSlice(s []any) []any {
	if s == nil {
		return nil
	}
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = ExpandEnvInValue(v)
	}
	return out
}

// ExpandEnvInValue handles a single any value — used by Map and Slice variants.
func ExpandEnvInValue(v any) any {
	switch val := v.(type) {
	case string:
		return os.ExpandEnv(val)
	case map[string]any:
		return ExpandEnvInMap(val)
	case []any:
		return ExpandEnvInSlice(val)
	default:
		return v
	}
}
