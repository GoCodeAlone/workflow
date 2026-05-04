// Package diffcache caches per-resource Diff results so a wfctl invocation
// that re-runs a Plan against unchanged inputs can skip the (sometimes
// network-expensive) provider-side Diff call. The cache is purely an
// amortization optimization, NOT a correctness mechanism — apply paths
// remain correct on a 100% miss rate (which is exactly what CI sees on
// every fresh runner).
//
// # Storage backends
//
// Cache selection is driven by the WFCTL_DIFFCACHE env var, resolved by
// [New]:
//
//   - `disabled` → noop cache (every Get misses; Put is a no-op). Use
//     this when an operator wants fully-deterministic Plan/Apply timing
//     with no shared state across invocations.
//   - `:memory:` → in-memory cache that lives only for the current
//     process. CI workflows in this repo set WFCTL_DIFFCACHE=:memory:
//     explicitly so containerized runners never write to disk.
//   - any other value (or unset) → filesystem cache rooted at
//     `~/.cache/wfctl/diff/`.
//
// # CI ephemerality (load-bearing)
//
// CI runners are ephemeral — each job starts with an empty cache.
// Workflow correctness MUST NOT depend on a cache hit. The diff cache
// is an operator-local performance optimization for repeated
// `wfctl infra plan` invocations against the same checkout.
//
// # Cache key
//
// The cache key is a [Key] tuple of (PluginVersion, Type, ProviderID,
// SHAConfig, SHAOutputs). Plugin downgrades naturally invalidate
// entries since PluginVersion is part of the key — old entries persist
// on disk until the LRU eviction reclaims them; the size cap (1024
// entries / 64 MiB) bounds the disk waste.
//
// # Schema versioning
//
// Each cache file embeds [cacheSchemaVersion] in a JSON envelope. On
// Get, a mismatched version is treated identically to file corruption:
// the entry is silently evicted and the caller re-Diffs.
//
// # Known limitations
//
// Windows: the filesystem cache uses [os.Rename] for the atomic
// publish step on Put. On Windows, [os.Rename] fails when the
// destination already exists, so updating an existing cache entry
// will fail (the entry is treated as a write failure and the
// operator gets a cache miss on the next Get — correct, since
// apply does not depend on cache hits). A future improvement is to
// vendor github.com/google/renameio for cross-platform atomic
// rename; deferred until there's a Windows-supported wfctl use
// case.
//
// # T3.5 / W-3a status
//
// This package ships in W-3a. The consumer that wires it into
// platform.ComputePlan lands in W-3b/T3.6f. Until then, the cache is
// callable but never invoked from production code paths.
package diffcache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// cacheSchemaVersion is the on-disk envelope version. Bump on any
// breaking change to the JSON shape; old files will be silently
// evicted on the next Get (same code path as corruption recovery).
const cacheSchemaVersion = 1

// defaultMaxEntries is the LRU eviction trigger by count.
const defaultMaxEntries = 1024

// defaultMaxBytes is the LRU eviction trigger by total on-disk size
// (64 MiB). Whichever cap (entries or bytes) is exceeded first triggers
// the eviction pass.
const defaultMaxBytes = 64 * 1024 * 1024

// evictionFraction is the fraction of cache entries to evict in one
// over-cap pass. 0.10 = oldest 10%; matches rev2 lifecycle constraint.
const evictionFraction = 0.10

// Key tuples the inputs to a single Diff. Two Keys are equal iff every
// field matches; the canonical sha256 fingerprint of the key
// determines the on-disk filename.
//
// JSON tags on the fields are for log / transcript serialization
// only — cache keying uses NUL-separated string concatenation in
// [keyFingerprint], not JSON marshaling. A reader checking the
// fingerprint shape should follow the keyFingerprint code, not the
// tags.
type Key struct {
	// PluginVersion is the plugin's name@version string, e.g.,
	// "do@v0.10.0". Plugin downgrades naturally invalidate cache
	// entries via this field.
	PluginVersion string `json:"plugin_version"`
	// Type is the canonical resource type, e.g., "infra.vpc".
	Type string `json:"type"`
	// ProviderID is the resource's cloud-side identifier; empty for
	// net-new resources.
	ProviderID string `json:"provider_id"`
	// SHAConfig is the sha256-hex of canonical-marshal(spec.Config).
	SHAConfig string `json:"sha_config"`
	// SHAOutputs is the sha256-hex of canonical-marshal(currentState.Outputs);
	// empty for net-new resources.
	SHAOutputs string `json:"sha_outputs"`
}

// Cache is the diff-result cache. Implementations are safe for
// concurrent use (the backing fs cache uses os-level atomic file ops;
// the in-memory cache locks internally).
type Cache interface {
	// Get returns the cached DiffResult for key. The boolean is true
	// iff the entry was found and successfully decoded; corruption,
	// schema-version mismatch, and missing entries all yield
	// (zero-value, false).
	Get(key Key) (interfaces.DiffResult, bool)
	// Put stores result under key. Errors during Put (e.g., disk
	// full, serialization failure) are silently swallowed because
	// cache misses are correct — apply behavior must not depend on
	// Put success.
	Put(key Key, result interfaces.DiffResult)
}

// New returns a Cache configured by the WFCTL_DIFFCACHE env var.
//
//   - "disabled"  → [NewNoop]
//   - ":memory:"  → [NewMemory]
//   - default     → [NewFilesystem] rooted at the user cache directory
//     (typically `~/.cache/wfctl/diff/`).
//
// When the user cache directory cannot be resolved, falls back to the
// in-memory cache so the caller still gets a working Cache (the
// filesystem path being unavailable is the operator's hint that disk
// caching is off).
func New() Cache {
	switch strings.TrimSpace(os.Getenv("WFCTL_DIFFCACHE")) {
	case "disabled":
		return NewNoop()
	case ":memory:":
		return NewMemory()
	}
	dir, err := userCacheDir()
	if err != nil {
		return NewMemory()
	}
	return NewFilesystem(dir)
}

// NewNoop returns a Cache that always misses. Put is a no-op.
func NewNoop() Cache { return &noopCache{} }

// noopCache is the disabled-cache implementation.
type noopCache struct{}

func (noopCache) Get(_ Key) (interfaces.DiffResult, bool) { return interfaces.DiffResult{}, false }
func (noopCache) Put(_ Key, _ interfaces.DiffResult)      { /* no-op */ }

// userCacheDir returns the diff-cache root under the user cache
// directory. Resolution follows os.UserCacheDir conventions per
// platform:
//
//   - Linux:   $XDG_CACHE_HOME or $HOME/.cache (e.g., ~/.cache/wfctl/diff/).
//   - macOS:   $HOME/Library/Caches (e.g., ~/Library/Caches/wfctl/diff/).
//   - Windows: %LocalAppData% (e.g., C:\Users\<user>\AppData\Local\wfctl\diff\).
func userCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}
	return filepath.Join(base, "wfctl", "diff"), nil
}

// keyFingerprint returns the sha256-hex fingerprint of the canonical
// key serialization. Exported as a package-level function so tests can
// assert key-stability without coupling to the filesystem cache.
func keyFingerprint(k Key) string {
	// Concatenate fields with a NUL separator so per-field collisions
	// are impossible (any field containing NUL would have to be
	// deliberately injected; cloud IDs and resource types don't).
	var b strings.Builder
	b.WriteString(k.PluginVersion)
	b.WriteByte(0)
	b.WriteString(k.Type)
	b.WriteByte(0)
	b.WriteString(k.ProviderID)
	b.WriteByte(0)
	b.WriteString(k.SHAConfig)
	b.WriteByte(0)
	b.WriteString(k.SHAOutputs)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// envelope is the JSON shape persisted to disk and used by the
// in-memory cache to validate on Get. The schemaVersion field gates
// silent eviction on a future schema bump.
type envelope struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Result        interfaces.DiffResult `json:"result"`
}
