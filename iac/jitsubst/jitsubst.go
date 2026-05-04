// Package jitsubst implements just-in-time substitution for an IaC
// [interfaces.ResourceSpec] at apply time.
//
// # Why JIT
//
// Plan time computes a [interfaces.IaCPlan] over modules whose configs may
// reference values that don't exist yet — e.g. a database password that a
// sub-action will populate, or the ProviderID of a sibling resource that
// the same apply will create. wfctlhelpers.ApplyPlan calls
// [ResolveSpec] right before dispatching each [interfaces.PlanAction] so
// the driver sees fully-substituted Config.
//
// # Reference forms
//
// Three substitution forms are recognized inside any string value of
// [interfaces.ResourceSpec.Config] (recursively, including nested maps and
// slices):
//
//	${VAR}             → envLookup(VAR)
//	${MODULE.id}       → replaceIDMap[MODULE], else syncedOutputs[MODULE]["id"]
//	${MODULE.field}    → syncedOutputs[MODULE][field]
//
// The discriminator between ${VAR} and ${MODULE.field} is the presence of
// a "." inside the body. Env-var names cannot contain "." per POSIX, so
// the rule is unambiguous.
//
// # Source precedence for ${MODULE.id}
//
// replaceIDMap is consulted FIRST. The map is populated by W-3b's
// doReplace [interfaces.ApplyResult.ReplaceIDMap] every time a Replace
// action successfully Delete-then-Creates a resource — its value is the
// post-Replace ProviderID. syncedOutputs holds outputs read from durable
// state (per W-5's design "JIT resolution reads from STATE … and from
// replaceIDMap"); state may have a stale ProviderID for a just-replaced
// resource until the apply loop persists the new one. Preferring
// replaceIDMap means dependents of a cascade-replaced parent see the new
// ID without depending on state-write ordering.
//
// # Strict resolution semantics
//
// Every reference MUST resolve. An unset env var, missing module, or
// missing field returns an error — matching the JIT contract that
// unresolved-at-apply-time refs are operator-actionable bugs, NOT
// silent-empty-string substitutions like [os.ExpandEnv]. Plan-time has
// already collapsed every resolvable ${VAR} via config.ExpandEnvInMap; a
// surviving env-var reference at apply time is therefore meaningful.
//
// # Error handling and partial state
//
// On error the original input spec is returned unmodified — callers MUST
// NOT use a partially-resolved spec since some fields may have substituted
// and others not. The first unresolved reference wins; resolution within a
// single string is left-to-right via Go's [regexp.Regexp.ReplaceAllStringFunc].
// Across maps, keys are walked in sorted order so that error messages are
// deterministic across Go map-iteration randomization.
//
// # Mutation contract
//
// ResolveSpec deep-copies Config (including nested maps and slices) before
// substitution. Caller-side mutation of the returned spec cannot poison
// the input.
//
// # Lifecycle
//
//   - T5.1 (this file) defines the helper.
//   - T5.2 wires it into wfctlhelpers.ApplyPlan's per-action dispatch —
//     a single ResolveSpec call per PlanAction immediately before driver
//     dispatch.
//   - T5.3 verifies cascade behavior: doReplace populates
//     ApplyResult.ReplaceIDMap when a Replace action successfully
//     Delete-then-Creates, and the existing T5.2 ResolveSpec call site
//     consumes that map on subsequent PlanActions in the same apply
//     loop. T5.3 does NOT add a second ResolveSpec call inside
//     doReplace — see ADR 008 for the rationale (single resolution
//     boundary, ReplaceIDMap-first ordering).
package jitsubst

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// refRE matches a single ${...} reference. The body may contain any
// non-} character; further parsing distinguishes ${VAR} from
// ${MODULE.field} by the presence of a ".".
//
// We intentionally do NOT support the bare $VAR form (without braces): IaC
// Config values are user-provided cloud-API knobs where adjacent characters
// are common (e.g., paths, hostnames), and unbraced $VAR has no clear
// terminator across resource-name slugs containing dots. Plan-time
// config.ExpandEnvInMap handles the simpler cases via os.ExpandEnv; JIT-time
// is the strict-form residue.
var refRE = regexp.MustCompile(`\$\{([^}]*)\}`)

// moduleRefRE matches a JIT-style ${MODULE.field} reference — any ${...}
// whose body has a "." separator AND non-empty module + field segments.
// Used by HasModuleRefs to gate plan.SchemaVersion bumping (T5.4) and the
// persisted-plan rejection (T5.5). Plain ${VAR} env-var references (no
// dot in body) do NOT match — they are NOT JIT-required at apply time
// since plan-time config.ExpandEnvInMap has already collapsed them
// (preserved-key submaps aside; those still resolve from env, not from
// other modules' outputs).
//
// Anchoring contract: the body MUST have at least one non-`.` character on
// EACH side of the first dot — `${.}`, `${.x}`, `${x.}` are NOT counted as
// JIT-required, matching jitsubst.ResolveSpec's strict rejection of those
// forms as malformed (they could never resolve at apply time anyway).
var moduleRefRE = regexp.MustCompile(`\$\{[^}.]+\.[^}]+\}`)

// ResolveSpec returns a deep copy of spec where every ${...} reference in
// any string value of spec.Config (recursively, including nested maps and
// slices) has been resolved against the supplied substitution sources.
// See the package docstring for reference forms, source precedence, and
// strict-resolution semantics.
//
// Inputs:
//
//   - replaceIDMap: resource Name → new ProviderID for resources replaced
//     earlier in this apply (W-3b/T3.4 doReplace).
//   - syncedOutputs: resource Name → outputs map (typed via state).
//   - envLookup: env-var resolver; pass [os.LookupEnv] in production. May
//     be nil — every ${VAR} reference will then error as unset, but ${...}
//     references that don't reach the env-var path will still resolve.
//
// On error the input spec is returned unmodified; callers MUST NOT use a
// partially-resolved spec.
func ResolveSpec(
	spec interfaces.ResourceSpec,
	replaceIDMap map[string]string,
	syncedOutputs map[string]map[string]any,
	envLookup func(string) (string, bool),
) (interfaces.ResourceSpec, error) {
	if spec.Config == nil {
		return spec, nil
	}
	resolved, err := resolveAny(spec.Config, replaceIDMap, syncedOutputs, envLookup)
	if err != nil {
		return spec, err
	}
	out := spec
	out.Config = resolved.(map[string]any)
	return out, nil
}

// HasModuleRefs returns true when any string value reachable from v
// (recursively walking map[string]any and []any) contains a JIT-style
// ${MODULE.field} reference — i.e., a ${...} reference whose body has a
// "." separator with non-empty segments on both sides. Plain ${VAR}
// env-var references (no dot in body) do NOT count.
//
// Used by cmd/wfctl/infra.go::runInfraPlan (T5.4) to decide whether to
// stamp plan.SchemaVersion = 2 (JIT-required) and by the persisted-plan
// rejection in T5.5. Walking input is intentionally permissive: pass any
// value (a single map[string]any, a slice, a ResourceSpec.Config, even a
// raw string) — non-string scalars are ignored, nil yields false.
func HasModuleRefs(v any) bool {
	switch val := v.(type) {
	case nil:
		return false
	case string:
		return moduleRefRE.MatchString(val)
	case map[string]any:
		for _, vv := range val {
			if HasModuleRefs(vv) {
				return true
			}
		}
		return false
	case []any:
		return slices.ContainsFunc(val, HasModuleRefs)
	default:
		return false
	}
}

// resolveAny walks any value in a ResourceSpec.Config tree, deep-copying
// maps and slices, recursing into them, and resolving ${...} references in
// any string leaves. Non-string scalars (int, bool, float, nil, etc.) pass
// through unchanged. Map keys are walked in sorted order so the error
// surfaced on a multi-error spec is deterministic regardless of Go's
// map-iteration randomization.
func resolveAny(
	v any,
	replaceIDMap map[string]string,
	syncedOutputs map[string]map[string]any,
	envLookup func(string) (string, bool),
) (any, error) {
	switch val := v.(type) {
	case string:
		return resolveString(val, replaceIDMap, syncedOutputs, envLookup)
	case map[string]any:
		out := make(map[string]any, len(val))
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			r, err := resolveAny(val[k], replaceIDMap, syncedOutputs, envLookup)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(val))
		for i, vv := range val {
			r, err := resolveAny(vv, replaceIDMap, syncedOutputs, envLookup)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	default:
		// int/bool/float/nil/typed scalars: pass through. Callers that
		// store custom struct values in Config receive them unchanged —
		// JIT substitution is a string-value-only contract.
		return v, nil
	}
}

// resolveString substitutes every ${...} reference in s. The first
// unresolved reference wins (leftmost-first via ReplaceAllStringFunc); the
// returned error names the offending reference body so an operator can
// trace it back to the spec.
func resolveString(
	s string,
	replaceIDMap map[string]string,
	syncedOutputs map[string]map[string]any,
	envLookup func(string) (string, bool),
) (string, error) {
	var firstErr error
	out := refRE.ReplaceAllStringFunc(s, func(match string) string {
		if firstErr != nil {
			// Short-circuit subsequent matches once an error has been
			// recorded; the unmodified placeholder we return is unused
			// since resolveString returns "" + error below.
			return match
		}
		// match has the form "${...}" — strip the wrapping 2 + 1 chars.
		body := match[2 : len(match)-1]
		val, err := resolveRef(body, replaceIDMap, syncedOutputs, envLookup)
		if err != nil {
			firstErr = err
			return match
		}
		return val
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

// resolveRef resolves a single reference body (the text between ${ and }).
// Reference forms and source precedence are documented at the package level.
func resolveRef(
	body string,
	replaceIDMap map[string]string,
	syncedOutputs map[string]map[string]any,
	envLookup func(string) (string, bool),
) (string, error) {
	if body == "" {
		return "", fmt.Errorf("malformed reference ${}: empty body")
	}
	if module, field, hasDot := strings.Cut(body, "."); hasDot {
		if module == "" || field == "" {
			return "", fmt.Errorf("malformed reference ${%s}: empty module or field", body)
		}
		if field == "id" {
			if id, ok := replaceIDMap[module]; ok {
				return id, nil
			}
			if outs, ok := syncedOutputs[module]; ok {
				if v, ok := outs["id"]; ok {
					return fmt.Sprintf("%v", v), nil
				}
			}
			return "", fmt.Errorf("unresolved reference ${%s}: module %q has no .id in replaceIDMap or syncedOutputs", body, module)
		}
		outs, ok := syncedOutputs[module]
		if !ok {
			return "", fmt.Errorf("unresolved reference ${%s}: module %q not found in syncedOutputs", body, module)
		}
		v, ok := outs[field]
		if !ok {
			return "", fmt.Errorf("unresolved reference ${%s}: module %q has no field %q", body, module, field)
		}
		return fmt.Sprintf("%v", v), nil
	}
	// No dot → ${VAR} env-var lookup. A nil envLookup means "no env source
	// configured" — equivalent to an unset var, so the same error surfaces.
	if envLookup == nil {
		return "", fmt.Errorf("unresolved reference ${%s}: env var not set (envLookup not configured)", body)
	}
	val, ok := envLookup(body)
	if !ok {
		return "", fmt.Errorf("unresolved reference ${%s}: env var not set", body)
	}
	return val, nil
}
