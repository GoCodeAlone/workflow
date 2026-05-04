package platform

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/GoCodeAlone/workflow/iac/diffcache"
	"github.com/GoCodeAlone/workflow/interfaces"
	"golang.org/x/sync/errgroup"
)

// ComputePlan compares desired ResourceSpecs against current
// ResourceStates and returns a Plan with the minimal set of ordered
// actions needed to reconcile them. Creates, updates, and replaces are
// ordered by DependsOn (dependencies first); deletes are ordered in
// reverse dependency order.
//
// Returns an error if the DependsOn graph contains a cycle or if any
// per-resource provider.Diff call fails.
//
// Action classification for resources that exist in both desired and
// current state is delegated to the provider via
// p.ResourceDriver(spec.Type).Diff(ctx, spec, currentOut):
//
//   - replace, when DiffResult.NeedsReplace is true OR any
//     FieldChange.ForceNew is true (the latter closes the latent
//     bug-fix surface where ForceNew silently downgraded to update);
//   - update,  when DiffResult.NeedsUpdate is true and replace did not
//     fire;
//   - skip,    when neither flag is set (no plan action emitted for
//     that resource).
//
// Net-new resources (no current state) emit create without dispatching
// Diff; resources removed from the desired set emit delete in reverse
// dependency order.
//
// The Diff dispatch is parallelised across resources via errgroup with
// a bounded worker pool. The worker count defaults to 8 and can be
// overridden via the WFCTL_PLAN_DIFF_CONCURRENCY env var (clamped to
// 1..32). Operators tuning for high-fan-out plans (50+ resources) can
// raise the cap; constrained-network operators can lower it.
//
// Nil-tolerance contract: if p is nil, or if p.ResourceDriver(typ)
// returns (nil, nil) for a particular resource type, ComputePlan falls
// back to the legacy ConfigHash compare for the affected resource(s) —
// emit update when ConfigHash diverges, skip otherwise. Replace cannot
// be expressed via the legacy path; callers that depend on Replace
// classification must supply a provider whose drivers implement Diff.
//
// Concurrency contract: p (and the ResourceDriver instances it returns)
// MUST be safe for concurrent use across goroutines, since Diff
// dispatch fans out under errgroup. gRPC-loaded plugins satisfy this
// trivially (each call is an independent RPC); in-process providers
// must internally serialize state mutations.
//
// Per-resource Diff results are cached via iac/diffcache when the
// caller has set a non-noop backend (default: filesystem cache at
// ~/.cache/wfctl/diff/; controlled via the WFCTL_DIFFCACHE env var per
// the diffcache package godoc). Cache hits skip the provider.Diff
// roundtrip entirely; cache misses store the freshly-computed
// DiffResult under the (PluginVersion, Type, ProviderID, SHAConfig,
// SHAOutputs) tuple. Apply-time correctness does not depend on cache
// hits — fresh CI runners always miss and re-Diff.
func ComputePlan(ctx context.Context, p interfaces.IaCProvider, desired []interfaces.ResourceSpec, current []interfaces.ResourceState) (interfaces.IaCPlan, error) {
	// Index current state by resource name.
	currentMap := make(map[string]interfaces.ResourceState, len(current))
	for i := range current {
		currentMap[current[i].Name] = current[i]
	}

	// Index desired specs by name for delete detection.
	desiredMap := make(map[string]interfaces.ResourceSpec, len(desired))
	for _, spec := range desired {
		desiredMap[spec.Name] = spec
	}

	// Partition desired into creates (net-new) and modifications
	// (existing-resource updates/replaces resolved via Diff dispatch).
	// Modifications are dispatched in parallel below; creates are
	// emitted synchronously since they don't need the provider.
	var creates []interfaces.PlanAction
	type modCandidate struct {
		spec     interfaces.ResourceSpec
		rs       interfaces.ResourceState
		hash     string // precomputed configHash(spec.Config); reused by classifyModification
		hashable bool   // true when configHashErr succeeded; false → cache-bypass for this candidate
	}
	var candidates []modCandidate
	for _, spec := range desired {
		// Use configHashErr to detect non-marshalable spec.Config values
		// so we can bypass the diff-cache for those resources. For the
		// stored ResolvedConfigHash we fall back to the legacy
		// configHash (sha256-of-best-effort) when configHashErr fails;
		// that preserves byte-for-byte stability with any persisted
		// plans + ConfigHash values produced under the pre-T3.6
		// implementation, while the hashable flag gates cache
		// participation for the modification path.
		hash, hashErr := configHashErr(spec.Config)
		if hashErr != nil {
			hash = configHash(spec.Config)
		}
		if rs, exists := currentMap[spec.Name]; !exists {
			creates = append(creates, interfaces.PlanAction{
				Action:             "create",
				Resource:           spec,
				ResolvedConfigHash: hash,
			})
		} else {
			candidates = append(candidates, modCandidate{
				spec:     spec,
				rs:       rs,
				hash:     hash,
				hashable: hashErr == nil,
			})
		}
	}

	// Dispatch Diff per modification candidate. Pre-allocate the result
	// slice indexed by candidate position so workers can write
	// concurrently without a mutex; the nil entries left for skip
	// candidates are filtered out below.
	mods := make([]*interfaces.PlanAction, len(candidates))
	if len(candidates) > 0 {
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(planDiffConcurrency())
		for i := range candidates {
			g.Go(func() error {
				return classifyModification(gctx, p, candidates[i].spec, candidates[i].rs, candidates[i].hash, candidates[i].hashable, &mods[i])
			})
		}
		if err := g.Wait(); err != nil {
			return interfaces.IaCPlan{}, err
		}
	}

	// Collect non-nil modifications into a flat slice for topoSort.
	modifications := make([]interfaces.PlanAction, 0, len(mods))
	for _, m := range mods {
		if m != nil {
			modifications = append(modifications, *m)
		}
	}

	// Deletes: resources in current that are not in desired.
	var deletes []interfaces.PlanAction
	for i := range current {
		rs := &current[i]
		if _, exists := desiredMap[rs.Name]; !exists {
			rsCopy := *rs
			spec := interfaces.ResourceSpec{
				Name:      rs.Name,
				Type:      rs.Type,
				DependsOn: rs.Dependencies,
			}
			deletes = append(deletes, interfaces.PlanAction{
				Action:   "delete",
				Resource: spec,
				Current:  &rsCopy,
			})
		}
	}

	// Topological sort: creates + modifications in dependency order
	// (deps first); deletes in reverse dependency order (dependents
	// first). Reusing the same topoSort by concatenating the
	// non-delete buckets keeps the deterministic
	// desired-iteration-order seeding.
	sorted, err := topoSort(creates, modifications, desired)
	if err != nil {
		return interfaces.IaCPlan{}, err
	}
	sortedDeletes, err := reverseTopoSort(deletes)
	if err != nil {
		return interfaces.IaCPlan{}, err
	}
	sorted = append(sorted, sortedDeletes...)

	return interfaces.IaCPlan{
		ID:        planID(),
		Actions:   sorted,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// classifyModification dispatches Diff for a single existing resource
// and writes the resulting PlanAction (or nil for skip) to *out. It
// honors the nil-provider / nil-driver fallback contract documented on
// ComputePlan: when no driver is available, the resource is classified
// via the legacy ConfigHash compare. The hash argument is the
// precomputed configHash(spec.Config), threaded in by the caller so
// the per-candidate hashing happens once during candidate-bucketing
// rather than redundantly here on every Diff dispatch. The hashable
// argument is true when configHashErr succeeded for spec.Config; when
// false, the cache is bypassed for this resource (same defensive
// treatment as the empty-ProviderID path) — collapsing all
// non-marshalable inputs to the constant sha256("") hash would risk
// cache-key collisions across distinct resources.
func classifyModification(ctx context.Context, p interfaces.IaCProvider, spec interfaces.ResourceSpec, rs interfaces.ResourceState, hash string, hashable bool, out **interfaces.PlanAction) error {
	rsCopy := rs

	// Nil-provider fallback: legacy ConfigHash compare.
	if p == nil {
		if rs.ConfigHash != hash {
			*out = &interfaces.PlanAction{
				Action:             "update",
				Resource:           spec,
				Current:            &rsCopy,
				ResolvedConfigHash: hash,
			}
		}
		return nil
	}

	driver, err := p.ResourceDriver(spec.Type)
	if err != nil {
		return fmt.Errorf("provider.ResourceDriver(%q): %w", spec.Type, err)
	}
	// Nil-driver fallback: same as nil provider.
	if driver == nil {
		if rs.ConfigHash != hash {
			*out = &interfaces.PlanAction{
				Action:             "update",
				Resource:           spec,
				Current:            &rsCopy,
				ResolvedConfigHash: hash,
			}
		}
		return nil
	}

	// Consult the diff cache before dispatching to the (potentially
	// network-expensive) provider.Diff. Key shape per iac/diffcache:
	// (PluginVersion, Type, ProviderID, SHAConfig, SHAOutputs). Plugin
	// downgrades naturally invalidate via PluginVersion; outputs drift
	// invalidates via SHAOutputs. Apply-time correctness does NOT depend
	// on cache hits — every miss falls through to provider.Diff.
	//
	// Cache is bypassed when rs.ProviderID is empty: ProviderID is the
	// disambiguator across multiple resources of the same Type that
	// otherwise hash-collide on (SHAConfig, SHAOutputs). Empty ProviderID
	// occurs during state-bootstrap, broken-plugin paths, or transient
	// races; honoring those cache entries could let two newly-discovered
	// resources of the same Type with default-config / empty-outputs
	// serve each other's cached DiffResult and misclassify actions.
	// Always re-dispatch in that case; the cost is one extra Diff call,
	// not correctness.
	var diff *interfaces.DiffResult
	// cacheable = (1) ProviderID is the disambiguator across same-Type
	// resources (see the empty-ProviderID bypass docstring just above
	// the if-block) AND (2) we can deterministically hash both
	// spec.Config and rs.Outputs (configHashErr surfaces marshal
	// failures on non-YAML-derivable values so we don't collapse all
	// unmarshalable inputs to the constant sha256("") hash and serve
	// each other's cached DiffResult). Bypass cases all fall through
	// to the unconditional driver.Diff dispatch below.
	cacheable := rs.ProviderID != "" && hashable
	var (
		cache  diffcache.Cache
		key    diffcache.Key
		shaOut string
	)
	if cacheable {
		var outErr error
		shaOut, outErr = configHashErr(rs.Outputs)
		if outErr != nil {
			// Outputs contained a non-marshalable value; bypass cache
			// for this resource (same defensive treatment as the
			// empty-ProviderID and unhashable-spec.Config paths) and
			// re-Diff every time.
			cacheable = false
		}
	}
	if cacheable {
		// getDiffCache lives inside this branch so the empty-ProviderID
		// AND unhashable-inputs bypass paths are fully side-effect free
		// — no sync.Once init firing, no atomic load, and (most
		// importantly) no eager lazy-construction of the filesystem
		// cache backend creating ~/.cache/wfctl/diff/ on the operator's
		// machine for resources that won't use the cache anyway.
		cache = getDiffCache()
		key = diffcache.Key{
			PluginVersion: pluginVersionKey(p),
			Type:          spec.Type,
			ProviderID:    rs.ProviderID,
			SHAConfig:     hash,
			SHAOutputs:    shaOut,
		}
		if cached, hit := cache.Get(key); hit {
			c := cached
			diff = &c
		}
	}
	if diff == nil {
		currentOut := resourceStateToOutput(&rs)
		fresh, err := driver.Diff(ctx, spec, currentOut)
		if err != nil {
			return fmt.Errorf("provider.Diff(%q/%q): %w", spec.Type, spec.Name, err)
		}
		if cacheable {
			// Cache both the populated DiffResult and the no-op case
			// (driver returned (nil, nil) to signal "no changes"). The
			// downstream switch treats a zero-value DiffResult as no-op
			// just like nil, so caching the zero value here gives
			// providers that use the nil-as-no-op convention the same
			// cache benefit as those that return &DiffResult{} —
			// next ComputePlan against unchanged inputs gets a cache
			// hit instead of re-dispatching to the (potentially
			// network-expensive) Diff.
			toCache := interfaces.DiffResult{}
			if fresh != nil {
				toCache = *fresh
			}
			cache.Put(key, toCache)
		}
		diff = fresh
	}
	if diff == nil {
		// Driver returned no diff (and nothing was cached) — treat as
		// no change.
		return nil
	}

	replace := diff.NeedsReplace || hasForceNew(diff.Changes)
	switch {
	case replace:
		*out = &interfaces.PlanAction{
			Action:             "replace",
			Resource:           spec,
			Current:            &rsCopy,
			Changes:            diff.Changes,
			ResolvedConfigHash: hash,
		}
	case diff.NeedsUpdate:
		*out = &interfaces.PlanAction{
			Action:             "update",
			Resource:           spec,
			Current:            &rsCopy,
			Changes:            diff.Changes,
			ResolvedConfigHash: hash,
		}
	}
	return nil
}

// resourceStateToOutput converts the persisted ResourceState into the
// *interfaces.ResourceOutput shape that ResourceDriver.Diff expects.
// Sensitive map is not reconstructed here — drivers that need it should
// re-Read; this conversion preserves only the data ComputePlan has on
// hand (Outputs, ProviderID, identity).
func resourceStateToOutput(rs *interfaces.ResourceState) *interfaces.ResourceOutput {
	if rs == nil {
		return nil
	}
	return &interfaces.ResourceOutput{
		Name:       rs.Name,
		Type:       rs.Type,
		ProviderID: rs.ProviderID,
		Outputs:    rs.Outputs,
	}
}

// pluginVersionKey returns an ambiguity-free fingerprint of the
// provider's (Name, Version) tuple for use as the cache PluginVersion
// component. Concatenating with `@` would let `("foo", "bar@1.0")` and
// `("foo@bar", "1.0")` collide on the same key and serve each other's
// cached DiffResults; the sha256-hex digest of the NUL-separated
// concatenation eliminates that class of collision. Cheap (one hash
// per cached resource per ComputePlan) and matches how configHash
// already keys per-config inputs.
func pluginVersionKey(p interfaces.IaCProvider) string {
	if p == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(p.Name() + "\x00" + p.Version()))
	return fmt.Sprintf("%x", sum)
}

// hasForceNew reports whether any change in the slice has ForceNew=true.
// Used by ComputePlan to escalate update → replace when the provider
// signals a non-mutable field change but forgets to set NeedsReplace.
func hasForceNew(changes []interfaces.FieldChange) bool {
	for _, c := range changes {
		if c.ForceNew {
			return true
		}
	}
	return false
}

// planDiffConcurrencyDefault is the worker-pool size used when
// WFCTL_PLAN_DIFF_CONCURRENCY is unset or invalid. Chosen to keep gRPC
// roundtrip latency dominant over per-resource parallelism on typical
// 5–20-resource configs while staying well under provider rate limits.
const planDiffConcurrencyDefault = 8

// planDiffConcurrencyMin and Max are the clamp bounds for
// WFCTL_PLAN_DIFF_CONCURRENCY parsing. Values <= 0 are clamped UP to
// planDiffConcurrencyMin (=1), which produces effectively serial
// dispatch (one Diff in flight at a time) — operators cannot turn
// the worker pool fully off, only narrow it to one. Above 32 is
// unlikely to help on any reachable provider and can trip rate
// limits, so values >max clamp DOWN to planDiffConcurrencyMax.
const (
	planDiffConcurrencyMin = 1
	planDiffConcurrencyMax = 32
)

// planDiffConcurrencyOnce caches the parsed env-var value across the
// process lifetime. Operators changing the value mid-process need to
// restart wfctl, which matches the apply-time concurrency knob's
// established behavior.
var planDiffConcurrencyOnce sync.Once
var planDiffConcurrencyCached int

// planDiffConcurrency returns the parsed and clamped value of
// WFCTL_PLAN_DIFF_CONCURRENCY (or planDiffConcurrencyDefault when unset).
func planDiffConcurrency() int {
	planDiffConcurrencyOnce.Do(func() {
		planDiffConcurrencyCached = parseConcurrencyEnv(os.Getenv("WFCTL_PLAN_DIFF_CONCURRENCY"))
	})
	return planDiffConcurrencyCached
}

// parseConcurrencyEnv returns the clamped concurrency value for v.
// Empty, non-numeric, or out-of-bounds inputs fall back to safe values:
// empty/non-numeric → planDiffConcurrencyDefault; v<min → min; v>max →
// max. Extracted as a pure function so the clamping logic is unit
// testable without process-wide env-var mutation.
func parseConcurrencyEnv(v string) int {
	if v == "" {
		return planDiffConcurrencyDefault
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		return planDiffConcurrencyDefault
	}
	if parsed < planDiffConcurrencyMin {
		return planDiffConcurrencyMin
	}
	if parsed > planDiffConcurrencyMax {
		return planDiffConcurrencyMax
	}
	return parsed
}

// planDiffCache is the package-level diff cache used by
// classifyModification. Lazy-initialized on first call to getDiffCache
// from the WFCTL_DIFFCACHE env var via diffcache.New(), then served
// lock-free from the atomic.Pointer for the lifetime of the process.
// Tests in the same package may swap it via setDiffCacheForTest
// (defined in differ_cache_test.go) — Store on the atomic is safe
// concurrently with production Loads, and atomic.Pointer cleanly
// handles the nil case (test cleanup may restore a nil prior value
// without panicking the way atomic.Value would).
//
// Refactored from a per-call sync.Mutex to sync.Once + atomic.Pointer
// (Copilot review round 4): under ComputePlan's parallel Diff fan-out,
// the per-call mutex was contention on the hot path, especially on
// cache hits where the Get itself is cheap.
var (
	planDiffCacheOnce sync.Once
	planDiffCachePtr  atomic.Pointer[diffcache.Cache] // Load is lock-free; nil means uninitialized
)

// getDiffCache returns the package-level diff cache, initializing it
// from the environment on first call. Safe for concurrent use; the
// initialization fires exactly once via sync.Once and subsequent
// reads are lock-free atomic.Load. classifyModification only calls
// this when the resource is cacheable (non-empty ProviderID), so the
// bypass path neither acquires the Once nor eagerly initializes the
// filesystem cache backend on ~/.cache/wfctl/diff/.
func getDiffCache() diffcache.Cache {
	if p := planDiffCachePtr.Load(); p != nil {
		// Fast path: already initialized (production hot path AND any
		// test that has swapped a cache in via setDiffCacheForTest).
		return *p
	}
	planDiffCacheOnce.Do(func() {
		// Re-check under Once: a concurrent setDiffCacheForTest could
		// have Stored between our Load above and entering the Once
		// body. Only seed the default if no value is present.
		if planDiffCachePtr.Load() == nil {
			c := diffcache.New()
			planDiffCachePtr.Store(&c)
		}
	})
	if p := planDiffCachePtr.Load(); p != nil {
		return *p
	}
	// Defensive: should be unreachable. A concurrent test cleanup that
	// restored a nil prior value AFTER our Once fired could in
	// principle leave Load returning nil; fall through to a fresh
	// noop-cache rather than panic. Test code never relies on this
	// (cleanups always restore the prior concrete cache or a fresh
	// default — see setDiffCacheForTest below).
	return diffcache.NewNoop()
}

// ConfigHash is the exported counterpart of configHash. It allows callers
// outside the platform package (e.g. cmd/wfctl) to compute hashes that are
// byte-for-byte identical to those stored by ComputePlan, eliminating the
// risk of independent re-implementations diverging.
func ConfigHash(config map[string]any) string {
	return configHash(config)
}

// configHash returns a deterministic SHA-256 hex hash of a config map.
// Keys are explicitly sorted before marshalling so the hash is stable across
// Go's randomised map-iteration order — matching the DO plugin's pattern.
//
// configHash IGNORES json.Marshal errors. If the config contains a value
// json.Marshal cannot encode (channels, functions, cycles, custom types
// with broken MarshalJSON), this collapses to the constant
// sha256(<nil>) = e3b0c4429... hash. That preserves the legacy "did
// anything change" comparison semantics for YAML-derived configs
// (YAML can't express those types, so the failure mode is unreachable
// in practice for those callers) and keeps the exported ConfigHash
// byte-for-byte stable against any persisted ResolvedConfigHash values
// computed under the pre-T3.6 implementation. Cache-key callers MUST
// use configHashErr instead — collapsing all unmarshalable inputs to
// the same constant hash would cause cache-key collisions and let two
// genuinely-different resources serve each other's cached DiffResult.
// See the differ.go:235 comment block for the cache-bypass plumbing.
func configHash(config map[string]any) string {
	if len(config) == 0 {
		return ""
	}
	keys := make([]string, 0, len(config))
	for k := range config {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	type kv struct {
		K string
		V any
	}
	ordered := make([]kv, len(keys))
	for i, k := range keys {
		ordered[i] = kv{K: k, V: config[k]}
	}
	data, _ := json.Marshal(ordered)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// configHashErr is the error-aware variant of configHash, used by the
// diff-cache keying path so non-marshalable inputs bypass caching for
// that resource (rather than silently collapsing to the constant
// sha256("") hash and risking cache-key collisions across distinct
// resources). When err != nil, the returned string is the empty
// hash and callers MUST NOT use it for cache keys; they should
// re-Diff every time for that resource (same defensive bypass as
// the empty-ProviderID path documented at differ.go:235).
//
// In practice IaC configs come from YAML and cannot contain
// non-marshalable values (channels, functions, cycles), so this
// path is unreachable for the common case. Defensive coverage is
// for custom-provider Outputs that could conceivably contain
// types with broken MarshalJSON, or for future code paths that
// pass non-YAML-derived configs into ComputePlan.
func configHashErr(config map[string]any) (string, error) {
	if len(config) == 0 {
		return "", nil
	}
	keys := make([]string, 0, len(config))
	for k := range config {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	type kv struct {
		K string
		V any
	}
	ordered := make([]kv, len(keys))
	for i, k := range keys {
		ordered[i] = kv{K: k, V: config[k]}
	}
	data, err := json.Marshal(ordered)
	if err != nil {
		return "", fmt.Errorf("configHash: marshal: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

// planID generates a simple unique plan ID based on current time.
func planID() string {
	return fmt.Sprintf("plan-%d", time.Now().UnixNano())
}

// topoSort returns creates and modifications ordered so that a
// resource's dependencies appear before itself. Iteration order is
// seeded from desiredSpecs to ensure deterministic output for
// independent resources. Returns an error if a dependency cycle is
// detected.
func topoSort(creates, modifications []interfaces.PlanAction, desiredSpecs []interfaces.ResourceSpec) ([]interfaces.PlanAction, error) {
	// Build a map of name → DependsOn from desired specs.
	deps := make(map[string][]string, len(desiredSpecs))
	for _, s := range desiredSpecs {
		deps[s.Name] = s.DependsOn
	}

	// Collect all actions into a map by resource name.
	actionMap := make(map[string]interfaces.PlanAction)
	for i := range creates {
		actionMap[creates[i].Resource.Name] = creates[i]
	}
	for i := range modifications {
		actionMap[modifications[i].Resource.Name] = modifications[i]
	}

	visited := make(map[string]bool)
	inStack := make(map[string]bool) // cycle detection
	var result []interfaces.PlanAction

	var visit func(name string) error
	visit = func(name string) error {
		if inStack[name] {
			return fmt.Errorf("dependency cycle detected involving resource %q", name)
		}
		if visited[name] {
			return nil
		}
		inStack[name] = true
		for _, dep := range deps[name] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		inStack[name] = false
		visited[name] = true
		if action, ok := actionMap[name]; ok {
			result = append(result, action)
		}
		return nil
	}

	// Seed DFS from desiredSpecs to guarantee deterministic ordering.
	for _, s := range desiredSpecs {
		if _, ok := actionMap[s.Name]; ok {
			if err := visit(s.Name); err != nil {
				return nil, err
			}
		}
	}

	return result, nil
}

// reverseTopoSort returns deletes in reverse dependency order so that
// dependent resources are deleted before the resources they depend on.
// Returns an error if a dependency cycle is detected.
func reverseTopoSort(deletes []interfaces.PlanAction) ([]interfaces.PlanAction, error) {
	if len(deletes) == 0 {
		return nil, nil
	}

	// Build deps map from DependsOn on the resource spec.
	deps := make(map[string][]string, len(deletes))
	actionMap := make(map[string]interfaces.PlanAction, len(deletes))
	for i := range deletes {
		a := &deletes[i]
		deps[a.Resource.Name] = a.Resource.DependsOn
		actionMap[a.Resource.Name] = *a
	}

	visited := make(map[string]bool)
	inStack := make(map[string]bool) // cycle detection
	var forward []interfaces.PlanAction

	var visit func(name string) error
	visit = func(name string) error {
		if inStack[name] {
			return fmt.Errorf("dependency cycle detected involving resource %q", name)
		}
		if visited[name] {
			return nil
		}
		inStack[name] = true
		for _, dep := range deps[name] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		inStack[name] = false
		visited[name] = true
		if action, ok := actionMap[name]; ok {
			forward = append(forward, action)
		}
		return nil
	}

	// Seed DFS from the stable delete-action order.
	for i := range deletes {
		if err := visit(deletes[i].Resource.Name); err != nil {
			return nil, err
		}
	}

	// Reverse the order: deps-first → dependents-first for deletion.
	result := make([]interfaces.PlanAction, len(forward))
	for i := range forward {
		result[len(forward)-1-i] = forward[i]
	}
	return result, nil
}
