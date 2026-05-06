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

// ExpandEnvInMapPreservingKeys is like ExpandEnvInMap but when a key in
// preserveKeys is encountered, the corresponding value (and any nested
// content inside it) is left untouched — ${VAR} / $VAR references are
// preserved literally instead of being substituted from the process env.
//
// Use case: plan-time serialization of resource specs where certain
// submaps (env_vars, env_vars_secret, secret_env_vars) carry secret
// references that should resolve only at apply time. Without this,
// security-check rules see resolved literals and incorrectly flag them
// as accidentally-pasted secret values.
//
// preserveKeys is matched case-sensitively against the immediate map key
// at every depth. An empty or nil preserveKeys list makes this function
// behave identically to ExpandEnvInMap.
func ExpandEnvInMapPreservingKeys(m map[string]any, preserveKeys []string) map[string]any {
	if m == nil {
		return nil
	}
	preserve := make(map[string]struct{}, len(preserveKeys))
	for _, k := range preserveKeys {
		preserve[k] = struct{}{}
	}
	return expandEnvInMapWithPreserve(m, preserve)
}

func expandEnvInMapWithPreserve(m map[string]any, preserve map[string]struct{}) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if _, isPreserved := preserve[k]; isPreserved {
			// Copy the value verbatim. For maps/slices, deep-copy so callers
			// can mutate without aliasing back into the source. Strings and
			// scalars are immutable; pass through.
			out[k] = deepCopyValue(v)
			continue
		}
		out[k] = expandEnvInValueWithPreserve(v, preserve)
	}
	return out
}

func expandEnvInValueWithPreserve(v any, preserve map[string]struct{}) any {
	switch val := v.(type) {
	case string:
		return os.ExpandEnv(val)
	case map[string]any:
		return expandEnvInMapWithPreserve(val, preserve)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = expandEnvInValueWithPreserve(item, preserve)
		}
		return out
	default:
		return v
	}
}

// ExpandEnvInMapPreservingVars is like ExpandEnvInMapPreservingKeys but adds
// a second dimension of preservation: individual ${VAR} / $VAR references
// whose variable name appears in preserveVarNames are emitted as the literal
// "${name}" instead of being substituted from the process environment.
//
// Use case: plan-time serialisation of resource specs where a known set of
// secret variable names (e.g. cfg.Secrets.Generate keys) must produce
// hash-identical output regardless of whether the variable is present in the
// current environment.  Without this, fields such as user_data that contain
// ${SECRET_VAR} produce different hashes at plan time (var unset → empty
// substitution) and apply time (var set → actual value), causing a spurious
// "plan stale: config hash mismatch".
//
// Precedence: preserveKeys takes priority — if a map key is in preserveKeys
// the entire subtree is deep-copied as-is (no expansion at all, matching
// ExpandEnvInMapPreservingKeys semantics). preserveVarNames only affects
// string values in portions of the tree that are NOT inside a preserved-key
// subtree.
func ExpandEnvInMapPreservingVars(m map[string]any, preserveKeys []string, preserveVarNames []string) map[string]any {
	if m == nil {
		return nil
	}
	preserveK := make(map[string]struct{}, len(preserveKeys))
	for _, k := range preserveKeys {
		preserveK[k] = struct{}{}
	}
	preserveV := make(map[string]struct{}, len(preserveVarNames))
	for _, v := range preserveVarNames {
		preserveV[v] = struct{}{}
	}
	return expandEnvInMapWithPreserveVars(m, preserveK, preserveV)
}

func expandEnvInMapWithPreserveVars(m map[string]any, preserveK, preserveV map[string]struct{}) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if _, isPreservedKey := preserveK[k]; isPreservedKey {
			out[k] = deepCopyValue(v)
			continue
		}
		out[k] = expandEnvInValueWithPreserveVars(v, preserveK, preserveV)
	}
	return out
}

func expandEnvInValueWithPreserveVars(v any, preserveK, preserveV map[string]struct{}) any {
	switch val := v.(type) {
	case string:
		if len(preserveV) == 0 {
			return os.ExpandEnv(val)
		}
		return os.Expand(val, func(name string) string {
			if _, ok := preserveV[name]; ok {
				return "${" + name + "}"
			}
			return os.Getenv(name)
		})
	case map[string]any:
		return expandEnvInMapWithPreserveVars(val, preserveK, preserveV)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = expandEnvInValueWithPreserveVars(item, preserveK, preserveV)
		}
		return out
	default:
		return v
	}
}

// deepCopyValue copies a value preserving its structure. Maps and slices
// are recursively copied; scalars (string, int, bool, nil) are returned
// as-is. Used by ExpandEnvInMapPreservingKeys to insulate preserved
// subtrees from caller mutation.
func deepCopyValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, vv := range val {
			out[k] = deepCopyValue(vv)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = deepCopyValue(item)
		}
		return out
	default:
		return v
	}
}
