// Package sensitive routes ResourceOutput fields flagged as Sensitive through a
// secrets.Provider, returning sanitized placeholders for state persistence and
// hydrated values for same-process consumers.
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

// PlaceholderPrefix marks state values that reference a routed secret.
const PlaceholderPrefix = "secret_ref://"

// SecretKey returns the provider key used for one resource output.
func SecretKey(resourceName, outputKey string) string {
	raw := resourceName + "_" + outputKey
	key := sanitizeSecretKeyPart(resourceName) + "_" + sanitizeSecretKeyPart(outputKey)
	if strings.HasPrefix(strings.ToUpper(key), "GITHUB_") {
		key = "WF_" + key + "_" + shortHash(raw)
	}
	return key
}

// Placeholder returns the sanitized state value for one routed output.
func Placeholder(resourceName, outputKey string) string {
	return PlaceholderPrefix + SecretKey(resourceName, outputKey)
}

// IsPlaceholder reports whether v is a routed-secret placeholder.
func IsPlaceholder(v any) bool {
	s, ok := v.(string)
	return ok && strings.HasPrefix(s, PlaceholderPrefix)
}

// Route stores sensitive output values in provider and replaces them with
// placeholders in the returned sanitized output map. Out is not mutated.
func Route(ctx context.Context, provider secrets.Provider, resourceName string, out *interfaces.ResourceOutput) (map[string]any, map[string]string, error) {
	if out == nil {
		return nil, nil, fmt.Errorf("sensitive.Route: out is nil")
	}

	sanitized := make(map[string]any, len(out.Outputs))
	for k, v := range out.Outputs {
		sanitized[k] = v
	}

	var routable []string
	for k, flag := range out.Sensitive {
		if !flag {
			continue
		}
		if _, ok := out.Outputs[k]; ok {
			routable = append(routable, k)
		}
	}
	sort.Strings(routable)

	if len(routable) == 0 {
		return sanitized, nil, nil
	}
	if resourceName == "" {
		return nil, nil, fmt.Errorf("sensitive.Route: resourceName is empty (sensitive keys: %v)", routable)
	}
	if provider == nil {
		return nil, nil, fmt.Errorf("sensitive.Route: no secrets.Provider configured but resource %q has sensitive output keys %v", resourceName, routable)
	}

	hydrated := make(map[string]string, len(routable))
	for _, k := range routable {
		value, err := stringifyOutput(out.Outputs[k])
		if err != nil {
			return nil, nil, fmt.Errorf("sensitive.Route: resource %q key %q: %w", resourceName, k, err)
		}
		secretName := SecretKey(resourceName, k)
		if err := provider.Set(ctx, secretName, value); err != nil {
			return nil, nil, fmt.Errorf("sensitive.Route: provider.Set(%q): %w", secretName, err)
		}
		sanitized[k] = Placeholder(resourceName, k)
		hydrated[secretName] = value
	}
	return sanitized, hydrated, nil
}

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
	changed := false
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
			changed = true
		}
	}
	out := b.String()
	if out == "" {
		out = "SECRET"
		changed = true
	}
	if out[0] >= '0' && out[0] <= '9' {
		out = "_" + out
		changed = true
	}
	if changed {
		out += "_" + shortHash(part)
	}
	return out
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return strings.ToUpper(hex.EncodeToString(sum[:4]))
}

// Revoke deletes routed secrets for resourceName. Missing secrets are ignored.
func Revoke(ctx context.Context, provider secrets.Provider, resourceName string, keys []string) error {
	if provider == nil {
		return nil
	}
	if resourceName == "" {
		return fmt.Errorf("sensitive.Revoke: resourceName is empty")
	}
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)

	var errs []error
	for _, k := range sorted {
		secretName := SecretKey(resourceName, k)
		if err := provider.Delete(ctx, secretName); err != nil {
			if errors.Is(err, secrets.ErrNotFound) {
				continue
			}
			errs = append(errs, fmt.Errorf("delete %q: %w", secretName, err))
		}
	}
	return errors.Join(errs...)
}

// MaskSensitiveForDiff returns copies of desired/current with sensitive keys
// removed from both sides.
func MaskSensitiveForDiff(driverKeys []string, desired, current map[string]any) (map[string]any, map[string]any) {
	mask := make(map[string]struct{}, len(driverKeys))
	for _, k := range driverKeys {
		mask[k] = struct{}{}
	}
	for k, v := range current {
		if IsPlaceholder(v) {
			mask[k] = struct{}{}
		}
	}
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
