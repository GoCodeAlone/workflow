// Package sensitive routes ResourceOutput fields flagged as Sensitive
// through a secrets.Provider, returning sanitized placeholders for state
// persistence and a hydrated map for in-process consumers.
//
// Per the engine-sensitive-output-routing design (workflow v0.27.0):
//   - Route is invoked on Create/Update only. Read/Adoption/Refresh paths
//     use Sanitize-only logic (not in this package — see
//     cmd/wfctl/infra_apply.go) to prevent cache pollution.
//   - The placeholder format "secret_ref://<SecretKey(resource,key)>" is
//     distinct from the user-supplied "secret://<key>" config-reference
//     convention.
//   - Routing trigger is exclusively out.Sensitive[k]==true (per-call
//     dynamic). ResourceDriver.SensitiveKeys() is NOT consulted here;
//     it remains a display-masking signal.
//
// Limitation (v0.27.0): only string-typed sensitive output values are
// supported. Non-string sensitive outputs (e.g., []byte, int) yield an
// error from Route. Future expansion via a MarshalSensitive interface
// is out of scope.
package sensitive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

const (
	// PlaceholderPrefix is the URI scheme used in state.Outputs values to
	// reference a routed secret stored in the configured secrets.Provider.
	// Distinct from secrets.SecretPrefix ("secret://") which is for
	// user-supplied config references.
	PlaceholderPrefix = "secret_ref://"

	// secretKeyMaxLength follows the lowest common provider limit currently
	// targeted by routed output secrets: GitHub Actions secret names.
	secretKeyMaxLength = 100
	secretKeyHashBytes = 16
)

// SecretKey returns the canonical secrets.Provider key for a resource's
// output. The key is provider-safe and collision-resistant for distinct
// (resourceName, outputKey) pairs. Exported so audit-state-secrets and other
// consumers can recompute routed-secret names from known resource/output pairs.
func SecretKey(resourceName, outputKey string) string {
	raw := resourceName + "\x00" + outputKey
	resourcePart := sanitizeSecretKeyPart(resourceName)
	outputPart := sanitizeSecretKeyPart(outputKey)
	hash := shortHash(raw)

	prefix := ""
	if strings.HasPrefix(strings.ToUpper(resourcePart+"__"+outputPart), "GITHUB_") {
		prefix = "WF_"
	}
	maxPartsLength := secretKeyMaxLength - len(prefix) - len(hash) - len("__") - len("_")
	resourcePart, outputPart = truncateSecretKeyParts(resourcePart, outputPart, maxPartsLength)
	return prefix + resourcePart + "__" + outputPart + "_" + hash
}

// Placeholder returns PlaceholderPrefix + SecretKey(resourceName, outputKey),
// replacing a routed value in state.Outputs.
func Placeholder(resourceName, outputKey string) string {
	return PlaceholderPrefix + SecretKey(resourceName, outputKey)
}

// IsPlaceholder reports whether v is a string with the PlaceholderPrefix.
// Non-string values return false.
func IsPlaceholder(v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	return strings.HasPrefix(s, PlaceholderPrefix)
}

// Route routes sensitive fields from out through provider, keying each
// secret via SecretKey(resourceName, outputKey). Returns:
//
//   - sanitized: a copy of out.Outputs with sensitive values replaced by
//     PlaceholderPrefix + SecretKey(resourceName, k). Suitable for
//     persistence to interfaces.IaCStateStore.
//   - hydrated: a flat map keyed by SecretKey of values that were routed.
//     Suitable for in-process hand-off to post-apply consumers in the
//     same wfctl invocation. Empty when no fields were routed.
//
// Routing trigger is out.Sensitive[k] == true with out.Outputs[k] present
// (any value, including empty string). When the sensitive key's value is
// absent from out.Outputs the key is silently SKIPPED — neither
// provider.Set is called nor a placeholder inserted (the engine has no
// value to route; existing routed-secret in provider stays as-is).
//
// Errors:
//   - resourceName == "" with non-empty Sensitive map → error (defensive;
//     out.Name is intentionally NOT consulted, since Read/Adoption paths
//     may have empty out.Name).
//   - provider == nil with non-empty Sensitive map AND any sensitive key
//     present in out.Outputs → error naming the resource and keys.
//   - provider.Set returns an error → error wrapping the failed key. Set
//     is invoked in sorted order by key for determinism; on first error
//     the loop stops and hydrated contains values already routed so the
//     caller can compensate partial writes.
//
// Out is not mutated.
func Route(
	ctx context.Context,
	provider secrets.Provider,
	resourceName string,
	out *interfaces.ResourceOutput,
) (sanitized map[string]any, hydrated map[string]string, err error) {
	if out == nil {
		return nil, nil, fmt.Errorf("sensitive.Route: out is nil")
	}
	// Build sanitized as a copy of Outputs (or empty map). Hydrated is
	// allocated lazily — kept nil-or-empty when no routing happens.
	sanitized = make(map[string]any, len(out.Outputs))
	for k, v := range out.Outputs {
		sanitized[k] = v
	}

	// Collect the sensitive keys whose value is present in Outputs.
	// Sort for deterministic Set order.
	var routableKeys []string
	for k, flag := range out.Sensitive {
		if !flag {
			continue
		}
		if _, present := out.Outputs[k]; !present {
			continue
		}
		routableKeys = append(routableKeys, k)
	}
	sort.Strings(routableKeys)

	if len(routableKeys) == 0 {
		return sanitized, nil, nil
	}
	if resourceName == "" {
		return nil, nil, fmt.Errorf("sensitive.Route: resourceName is empty (sensitive keys: %v)", routableKeys)
	}
	if provider == nil {
		return nil, nil, fmt.Errorf("sensitive.Route: no secrets.Provider configured but resource %q has sensitive output keys %v", resourceName, routableKeys)
	}

	hydrated = make(map[string]string, len(routableKeys))
	for _, k := range routableKeys {
		val, sErr := stringifyOutput(out.Outputs[k])
		if sErr != nil {
			return nil, hydrated, fmt.Errorf("sensitive.Route: resource %q key %q: %w", resourceName, k, sErr)
		}
		secretName := SecretKey(resourceName, k)
		if setErr := provider.Set(ctx, secretName, val); setErr != nil {
			return nil, hydrated, fmt.Errorf("sensitive.Route: provider.Set(%q): %w", secretName, setErr)
		}
		sanitized[k] = Placeholder(resourceName, k)
		hydrated[secretName] = val
	}
	return sanitized, hydrated, nil
}

// stringifyOutput coerces an output value to string. The secrets.Provider
// API takes string values; non-string sensitive outputs are not supported
// in v0.27.0 (would need encoding decisions out of scope here).
func stringifyOutput(v any) (string, error) {
	switch s := v.(type) {
	case string:
		return s, nil
	default:
		return "", fmt.Errorf("sensitive output value type %T not supported (must be string)", v)
	}
}

func sanitizeSecretKeyPart(part string) string {
	var b strings.Builder
	b.Grow(len(part))
	for _, r := range part {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		out = "SECRET"
	}
	if out[0] >= '0' && out[0] <= '9' {
		out = "_" + out
	}
	return out
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return strings.ToUpper(hex.EncodeToString(sum[:secretKeyHashBytes]))
}

func truncateSecretKeyParts(resourcePart, outputPart string, maxLength int) (string, string) {
	if len(resourcePart)+len(outputPart) <= maxLength {
		return resourcePart, outputPart
	}
	resourceBudget := maxLength / 2
	outputBudget := maxLength - resourceBudget
	if len(resourcePart) < resourceBudget {
		outputBudget += resourceBudget - len(resourcePart)
		resourceBudget = len(resourcePart)
	}
	if len(outputPart) < outputBudget {
		resourceBudget += outputBudget - len(outputPart)
		outputBudget = len(outputPart)
	}
	if len(resourcePart) > resourceBudget {
		resourcePart = resourcePart[:resourceBudget]
	}
	if len(outputPart) > outputBudget {
		outputPart = outputPart[:outputBudget]
	}
	return resourcePart, outputPart
}

// Revoke deletes routed secrets for resourceName. mergedKeys is the union
// of placeholder-derived keys (caller extracts from pre-delete
// state.Outputs) and any legacy heuristic keys. Errors from
// provider.Delete are aggregated via errors.Join — Revoke does NOT stop
// on the first error so partial cleanup proceeds. Keys that were never
// stored (provider returns secrets.ErrNotFound) are silently treated as
// success.
func Revoke(
	ctx context.Context,
	provider secrets.Provider,
	resourceName string,
	mergedKeys []string,
) error {
	if provider == nil {
		return nil // no-op when no provider configured
	}
	if resourceName == "" {
		return fmt.Errorf("sensitive.Revoke: resourceName is empty")
	}
	// Sort for determinism (test stability + log readability).
	sorted := append([]string(nil), mergedKeys...)
	sort.Strings(sorted)

	var errs []error
	for _, k := range sorted {
		secretName := SecretKey(resourceName, k)
		if delErr := provider.Delete(ctx, secretName); delErr != nil {
			if errors.Is(delErr, secrets.ErrNotFound) {
				continue
			}
			errs = append(errs, fmt.Errorf("delete %q: %w", secretName, delErr))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// MaskSensitiveForDiff returns copies of desired and current with sensitive
// keys elided from BOTH sides. A key is considered sensitive when:
//
//   - it is named in driverKeys (i.e., ResourceDriver.SensitiveKeys()), OR
//   - its value in current matches the PlaceholderPrefix.
//
// Eliding from both sides ensures driver.Diff or other field-by-field
// comparators don't report drift when state has a placeholder and live
// has a different (or absent) value. Non-sensitive keys are passed
// through unchanged.
//
// Either input may be nil; the corresponding output is also nil.
func MaskSensitiveForDiff(driverKeys []string, desired, current map[string]any) (map[string]any, map[string]any) {
	mask := make(map[string]struct{}, len(driverKeys))
	for _, k := range driverKeys {
		mask[k] = struct{}{}
	}
	// Augment with placeholder-derived keys from current.
	for k, v := range current {
		if IsPlaceholder(v) {
			mask[k] = struct{}{}
		}
	}
	// Also augment from desired in case a desired-side placeholder leaked in
	// (unusual but defensive).
	for k, v := range desired {
		if IsPlaceholder(v) {
			mask[k] = struct{}{}
		}
	}
	return copyExcept(desired, mask), copyExcept(current, mask)
}

func copyExcept(in map[string]any, exclude map[string]struct{}) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if _, skip := exclude[k]; skip {
			continue
		}
		out[k] = v
	}
	return out
}
