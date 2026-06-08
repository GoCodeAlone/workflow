package secrets

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// SecretPrefix is the URI scheme used in config values to reference secrets.
const SecretPrefix = "secret://"

// Common errors.
var (
	ErrNotFound     = errors.New("secrets: secret not found")
	ErrUnsupported  = errors.New("secrets: operation not supported")
	ErrInvalidKey   = errors.New("secrets: invalid key")
	ErrProviderInit = errors.New("secrets: provider initialization failed")
)

// Provider defines the interface for secret storage backends.
type Provider interface {
	// Name returns the provider identifier.
	Name() string
	// Get retrieves a secret value by key.
	Get(ctx context.Context, key string) (string, error)
	// Set stores a secret. Returns ErrUnsupported if read-only.
	Set(ctx context.Context, key, value string) error
	// Delete removes a secret. Returns ErrUnsupported if read-only.
	Delete(ctx context.Context, key string) error
	// List returns all available secret keys. Returns ErrUnsupported if not supported.
	List(ctx context.Context) ([]string, error)
}

// ProviderTarget describes the concrete provider namespace a Provider instance
// reads or writes. It is intentionally value-only and must not include secret
// values or credential material.
type ProviderTarget struct {
	Provider string
	Scope    string
	Subject  string
	Label    string
}

// ProviderEnvironment describes a provider-owned environment or namespace used
// as a secret target. It is intentionally metadata-only and must never include
// secret values or credential material.
type ProviderEnvironment struct {
	Provider string
	Name     string
	Label    string
	Exists   bool
	Source   string
}

// EnvironmentManager is optional: providers implement it when they can inspect
// and create environment-like namespaces used by scoped secrets.
type EnvironmentManager interface {
	ListEnvironments(ctx context.Context) ([]ProviderEnvironment, error)
	ValidateEnvironment(ctx context.Context, name string) (ProviderEnvironment, error)
	EnsureEnvironment(ctx context.Context, name string) (ProviderEnvironment, error)
}

// TargetDescriber is optional: providers implement it when they can describe
// their concrete namespace, such as GitHub repo/env/org or AWS region.
type TargetDescriber interface {
	SecretTarget() ProviderTarget
}

// DescribeTarget returns a safe provider-owned target description. Providers
// that do not implement TargetDescriber fall back to their Name.
func DescribeTarget(provider Provider) ProviderTarget {
	if provider == nil {
		return ProviderTarget{}
	}
	if describer, ok := provider.(TargetDescriber); ok {
		target := describer.SecretTarget()
		if target.Provider == "" {
			target.Provider = provider.Name()
		}
		if target.Label == "" {
			target.Label = strings.TrimSpace(target.Provider + " " + target.Subject)
		}
		return target
	}
	name := provider.Name()
	return ProviderTarget{Provider: name, Scope: "default", Label: name}
}

// SecretMeta is presence + freshness for one key. Never carries a value.
type SecretMeta struct {
	Name      string
	Exists    bool
	UpdatedAt time.Time // zero when the store doesn't expose it
}

// MetadataProvider is optional: stores that can report which keys exist and when they changed.
type MetadataProvider interface {
	Provider
	StatAll(ctx context.Context) ([]SecretMeta, error)
}

// AccessChecker is optional: verify the store is reachable + usable for setup.
// CheckAccess MUST NOT leak credential material in its error.
type AccessChecker interface {
	CheckAccess(ctx context.Context) error
}

// RotationProvider extends Provider with key rotation capabilities.
type RotationProvider interface {
	Provider
	// Rotate generates a new secret value and stores it, returning the new value.
	Rotate(ctx context.Context, key string) (string, error)
	// GetPrevious retrieves the previous version of a rotated secret (for grace periods).
	GetPrevious(ctx context.Context, key string) (string, error)
}

// --- Environment Variable Provider ---

// EnvProvider reads secrets from environment variables.
// Keys are converted to uppercase with dots replaced by underscores.
// For example, "database.password" becomes "DATABASE_PASSWORD".
type EnvProvider struct {
	prefix string
}

// NewEnvProvider creates a new environment variable secret provider.
// If prefix is non-empty, it is prepended to all key lookups (e.g., prefix "APP_" + key "db_pass" -> "APP_DB_PASS").
func NewEnvProvider(prefix string) *EnvProvider {
	return &EnvProvider{prefix: prefix}
}

func (p *EnvProvider) Name() string { return "env" }

// SecretTarget describes the current process environment namespace.
func (p *EnvProvider) SecretTarget() ProviderTarget {
	label := "env"
	subject := p.prefix
	if subject != "" {
		label = "env prefix " + subject
	}
	return ProviderTarget{
		Provider: "env",
		Scope:    "process",
		Subject:  subject,
		Label:    label,
	}
}

func (p *EnvProvider) Get(_ context.Context, key string) (string, error) {
	if key == "" {
		return "", ErrInvalidKey
	}
	envKey := p.envKey(key)
	val, ok := os.LookupEnv(envKey)
	if !ok {
		return "", fmt.Errorf("%w: env var %s", ErrNotFound, envKey)
	}
	return val, nil
}

func (p *EnvProvider) Set(_ context.Context, key, value string) error {
	if key == "" {
		return ErrInvalidKey
	}
	return os.Setenv(p.envKey(key), value)
}

func (p *EnvProvider) Delete(_ context.Context, key string) error {
	if key == "" {
		return ErrInvalidKey
	}
	return os.Unsetenv(p.envKey(key))
}

func (p *EnvProvider) List(_ context.Context) ([]string, error) {
	var keys []string
	prefix := strings.ToUpper(p.prefix)
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if prefix != "" && strings.HasPrefix(parts[0], prefix) {
			keys = append(keys, parts[0])
		}
	}
	if prefix == "" {
		return nil, ErrUnsupported
	}
	return keys, nil
}

func (p *EnvProvider) envKey(key string) string {
	k := strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
	if p.prefix != "" {
		return strings.ToUpper(p.prefix) + k
	}
	return k
}

// StatAll implements MetadataProvider. It lists env vars that match the prefix
// (same logic as List) and returns SecretMeta with Exists=true and zero UpdatedAt
// (env vars have no last-modified timestamp).
func (p *EnvProvider) StatAll(_ context.Context) ([]SecretMeta, error) {
	prefix := strings.ToUpper(p.prefix)
	if prefix == "" {
		return nil, ErrUnsupported
	}
	var metas []SecretMeta
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.HasPrefix(parts[0], prefix) {
			metas = append(metas, SecretMeta{
				Name:   parts[0],
				Exists: true,
				// UpdatedAt intentionally zero — env vars carry no mtime.
			})
		}
	}
	return metas, nil
}

// CheckAccess implements AccessChecker. For EnvProvider, access is always available.
func (p *EnvProvider) CheckAccess(_ context.Context) error {
	return nil
}

// --- File Provider ---

// FileProvider reads secrets from files in a directory.
// Each file name is the secret key, and the file content is the value.
// This is compatible with Kubernetes secret volume mounts.
type FileProvider struct {
	dir string
}

// NewFileProvider creates a file-based secret provider rooted at dir.
func NewFileProvider(dir string) *FileProvider {
	return &FileProvider{dir: dir}
}

func (p *FileProvider) Name() string { return "file" }

// SecretTarget describes the directory-backed file secret namespace.
func (p *FileProvider) SecretTarget() ProviderTarget {
	return ProviderTarget{
		Provider: "file",
		Scope:    "directory",
		Subject:  p.dir,
		Label:    "file " + p.dir,
	}
}

func (p *FileProvider) Get(_ context.Context, key string) (string, error) {
	if key == "" {
		return "", ErrInvalidKey
	}
	path := p.dir + "/" + key
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %s", ErrNotFound, key)
		}
		return "", fmt.Errorf("secrets: failed to read %s: %w", key, err)
	}
	return strings.TrimRight(string(data), "\n\r"), nil
}

func (p *FileProvider) Set(_ context.Context, key, value string) error {
	if key == "" {
		return ErrInvalidKey
	}
	path := p.dir + "/" + key
	return os.WriteFile(path, []byte(value), 0600)
}

func (p *FileProvider) Delete(_ context.Context, key string) error {
	if key == "" {
		return ErrInvalidKey
	}
	path := p.dir + "/" + key
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	return err
}

func (p *FileProvider) List(_ context.Context) ([]string, error) {
	entries, err := os.ReadDir(p.dir)
	if err != nil {
		return nil, fmt.Errorf("secrets: failed to list directory: %w", err)
	}
	var keys []string
	for _, e := range entries {
		if !e.IsDir() {
			keys = append(keys, e.Name())
		}
	}
	return keys, nil
}

// StatAll implements MetadataProvider. It returns SecretMeta for every file in
// the directory, using the file's modification time as UpdatedAt.
func (p *FileProvider) StatAll(_ context.Context) ([]SecretMeta, error) {
	entries, err := os.ReadDir(p.dir)
	if err != nil {
		return nil, fmt.Errorf("secrets: failed to list directory: %w", err)
	}
	var metas []SecretMeta
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		metas = append(metas, SecretMeta{
			Name:      e.Name(),
			Exists:    true,
			UpdatedAt: info.ModTime(),
		})
	}
	return metas, nil
}

// CheckAccess implements AccessChecker. It verifies the directory exists and is
// writable by attempting to create (then remove) a probe file.
func (p *FileProvider) CheckAccess(_ context.Context) error {
	probe := p.dir + "/.wfctl_probe"
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("secrets: file store not accessible: %w", err)
	}
	f.Close()
	os.Remove(probe) //nolint:errcheck
	return nil
}

// --- Vault Configuration ---

// VaultConfig holds configuration for HashiCorp Vault.
type VaultConfig struct {
	Address   string `json:"address" yaml:"address"`
	Token     string `json:"token" yaml:"token"`
	MountPath string `json:"mount_path" yaml:"mount_path"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
}

// --- Secret Resolver ---

// Resolver resolves secret:// references in configuration values.
type Resolver struct {
	mu       sync.RWMutex
	provider Provider
}

// NewResolver creates a resolver backed by the given provider.
func NewResolver(provider Provider) *Resolver {
	return &Resolver{provider: provider}
}

// Resolve replaces a value containing a secret:// reference with the actual secret.
// If the value does not start with SecretPrefix, it is returned as-is.
func (r *Resolver) Resolve(ctx context.Context, value string) (string, error) {
	if !strings.HasPrefix(value, SecretPrefix) {
		return value, nil
	}
	key := strings.TrimPrefix(value, SecretPrefix)
	r.mu.RLock()
	p := r.provider
	r.mu.RUnlock()
	return p.Get(ctx, key)
}

// ResolveMap resolves all secret:// references in a string map.
func (r *Resolver) ResolveMap(ctx context.Context, m map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case string:
			resolved, err := r.Resolve(ctx, val)
			if err != nil {
				return nil, fmt.Errorf("secrets: failed to resolve %q: %w", k, err)
			}
			result[k] = resolved
		case map[string]any:
			resolved, err := r.ResolveMap(ctx, val)
			if err != nil {
				return nil, err
			}
			result[k] = resolved
		default:
			result[k] = v
		}
	}
	return result, nil
}

// Provider returns the underlying provider.
func (r *Resolver) Provider() Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.provider
}
