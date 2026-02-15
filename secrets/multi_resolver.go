package secrets

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// secretRefPattern matches ${scheme:path} or ${VAR_NAME} patterns.
// Examples: ${vault:secret/data/myapp#password}, ${aws-sm:my-secret}, ${env:DB_HOST}, ${DB_HOST}
var secretRefPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// MultiResolver resolves secret references in configuration values using
// multiple providers identified by URI scheme. It is backward-compatible:
// bare ${VAR_NAME} references (without a scheme) default to env resolution.
type MultiResolver struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewMultiResolver creates a new MultiResolver.
// An EnvProvider is registered by default under the "env" scheme.
func NewMultiResolver() *MultiResolver {
	m := &MultiResolver{
		providers: make(map[string]Provider),
	}
	m.providers["env"] = NewEnvProvider("")
	return m
}

// Register adds or replaces a provider for a given scheme.
func (m *MultiResolver) Register(scheme string, provider Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers[scheme] = provider
}

// Unregister removes a provider for the given scheme.
func (m *MultiResolver) Unregister(scheme string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.providers, scheme)
}

// Provider returns the provider for a given scheme, or nil if not found.
func (m *MultiResolver) Provider(scheme string) Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.providers[scheme]
}

// Schemes returns the list of registered provider schemes.
func (m *MultiResolver) Schemes() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	schemes := make([]string, 0, len(m.providers))
	for s := range m.providers {
		schemes = append(schemes, s)
	}
	return schemes
}

// Expand replaces all ${...} patterns in input with resolved values.
//
// Supported formats:
//   - ${vault:secret/path#field} — uses "vault" provider with key "secret/path#field"
//   - ${aws-sm:secret-name} — uses "aws-sm" provider with key "secret-name"
//   - ${env:VAR_NAME} — uses "env" provider with key "VAR_NAME"
//   - ${VAR_NAME} — backward-compatible, uses "env" provider (os.LookupEnv via EnvProvider)
func (m *MultiResolver) Expand(ctx context.Context, input string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var expandErr error

	result := secretRefPattern.ReplaceAllStringFunc(input, func(match string) string {
		if expandErr != nil {
			return match
		}

		// Strip ${ and }
		inner := match[2 : len(match)-1]

		scheme, key := parseReference(inner)

		provider, ok := m.providers[scheme]
		if !ok {
			expandErr = fmt.Errorf("secrets: unknown provider scheme %q in reference %s", scheme, match)
			return match
		}

		val, err := provider.Get(ctx, key)
		if err != nil {
			expandErr = fmt.Errorf("secrets: failed to resolve %s: %w", match, err)
			return match
		}

		return val
	})

	if expandErr != nil {
		return "", expandErr
	}
	return result, nil
}

// parseReference splits an inner reference (without ${}) into scheme and key.
// "vault:secret/path#field" → ("vault", "secret/path#field")
// "aws-sm:my-secret"        → ("aws-sm", "my-secret")
// "env:DB_HOST"              → ("env", "DB_HOST")
// "DB_HOST"                  → ("env", "DB_HOST")  (backward-compatible)
func parseReference(inner string) (scheme, key string) {
	// Look for scheme:key pattern. The scheme must not contain
	// slashes, dots, or hash characters (those are part of the key).
	idx := strings.IndexByte(inner, ':')
	if idx > 0 {
		candidate := inner[:idx]
		// A valid scheme is alphanumeric plus hyphens
		if isValidScheme(candidate) {
			return candidate, inner[idx+1:]
		}
	}
	// No scheme found — treat entire inner as an env var name
	return "env", inner
}

// isValidScheme checks whether s looks like a provider scheme (alphanumeric + hyphens).
func isValidScheme(s string) bool {
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return len(s) > 0
}
