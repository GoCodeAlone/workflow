# Engine-side sensitive-output routing Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Engine routes per-call `Sensitive`-flagged outputs from `ResourceDriver` calls through the configured `secrets.Provider` before state persistence, keeping plugins platform-agnostic and never letting sensitive values reach the state backend.

**Architecture:** New `iac/sensitive` package exposes free functions (`Route`, `Revoke`, `IsPlaceholder`, `MaskSensitiveForDiff`). A new helper `persistResourceWithSecretRouting` in `cmd/wfctl/infra_apply.go` is the single funnel for all five state-write call sites. `Route` (Create/Update only) sets sensitive values into `secrets.Provider` and returns sanitized `secret_ref://...` placeholders for state plus a hydrated in-memory map for same-process consumers. Read/Adoption/Refresh paths are sanitize-only (no provider writes — prevents cache pollution). On `SaveResource` failure after `Set` succeeded, the helper compensates with `driver.Delete` + `provider.Delete` to avoid orphan cloud resources. New `wfctl infra audit-state-secrets` command is the recovery surface for orphans, legacy plaintext, and missing routed values.

**Tech Stack:** Go 1.22+, stdlib only (no new deps). Existing packages: `github.com/GoCodeAlone/workflow/interfaces`, `.../secrets`, `.../iac/wfctlhelpers`. New package: `.../iac/sensitive`.

**Base branch:** `design/engine-sensitive-output-routing` (worktree `_worktrees/engine-sensitive-output-routing`)

---

## Scope Manifest

**PR Count:** 1
**Tasks:** 6
**Estimated Lines of Change:** ~1.7k (informational; not enforced)

**Out of scope:**
- DO plugin update to set `Sensitive: {"secret_key": true}` on `SpacesKeyDriver.Create` — separate PR after v0.27.0 tag.
- AWS / GCP / Azure plugin updates — separate per-plugin PRs.
- workflow-plugin-reviewer skill update for "plugin-sensitive-keys-declared" lint.
- State-record migration tool to retroactively route legacy plaintext secrets.
- UI display of `secret_ref://...` placeholders.
- Per-provider `DetectDrift` masking updates (per-plugin follow-up).
- Schema change to `interfaces.ResourceState` to add explicit `RoutedSecrets` map (deferred per design §10 doubt 3).
- Wiring `MaskSensitiveForDiff` into engine-side Diff call sites (helper ships in Task 1; in-tree consumers ride a follow-up PR if needed — currently no `cmd/wfctl` site sees state.Outputs flow into `driver.Diff`; per-provider Diffs receive desired/current via gRPC and are out of scope).
- Import path (`infra.go:1101` `Outputs: cloneMap(imported.Outputs)`) — accepted as legacy-plaintext on import; operator runs `wfctl infra audit-state-secrets` post-import to triage. Routing imported state requires a behavioural decision (ad-hoc rotate vs. preserve-as-is) that is out of scope here.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | feat(iac): engine-side sensitive-output routing through secrets.Provider | Task 1, Task 2, Task 3, Task 4, Task 5, Task 6 | `feat/engine-sensitive-output-routing` |

**Status:** Draft

## Public API contract (locked at Task 1)

After Task 1 lands, the `iac/sensitive` package exports the following names. Tasks 2-6 depend on these being **exported** (capital initial). Implementer MUST NOT lowercase any of these:

- `Route(ctx, provider, resourceName, out) (sanitized, hydrated, error)`
- `Revoke(ctx, provider, resourceName, mergedKeys) error`
- `IsPlaceholder(v any) bool`
- `MaskSensitiveForDiff(driverKeys, desired, current) (map, map)`
- `Placeholder(resourceName, outputKey string) string`
- `PlaceholderPrefix string` (constant)
- `SecretKey(resourceName, outputKey string) string`

---

## Pre-flight

Before starting Task 1, the implementer:

1. Confirms working directory: `/Users/jon/workspace/workflow/_worktrees/engine-sensitive-output-routing`.
2. Confirms branch: `git branch --show-current` → `design/engine-sensitive-output-routing`.
3. Creates the implementation branch: `git checkout -b feat/engine-sensitive-output-routing`.
4. Reads the design doc: `docs/plans/2026-05-09-engine-sensitive-output-routing-design.md`.
5. Reads the cited source files end-to-end: `interfaces/iac_resource_driver.go`, `interfaces/iac_state.go`, `secrets/secrets.go`, `secrets/github_provider.go`, `iac/wfctlhelpers/apply.go`, `cmd/wfctl/infra_apply.go`, `cmd/wfctl/infra_secrets.go`, `cmd/wfctl/infra_output_secrets.go`, `cmd/wfctl/infra_audit_secrets.go`.

---

## Task 1: New `iac/sensitive` package — Route, Revoke, IsPlaceholder, MaskSensitiveForDiff

**Files:**
- Create: `iac/sensitive/route.go`
- Create: `iac/sensitive/route_test.go`

**Change class:** Internal logic (new package, free functions). Verification: unit tests.

**Step 1.1: Write the failing tests for `Route`**

Create `iac/sensitive/route_test.go`:

```go
package sensitive

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

// fakeProvider records Set/Delete/Get/List calls for assertions.
type fakeProvider struct {
	values  map[string]string
	setErr  map[string]error // per-key Set override
	delErr  map[string]error
	setLog  []string
	delLog  []string
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{values: map[string]string{}, setErr: map[string]error{}, delErr: map[string]error{}}
}

func (p *fakeProvider) Name() string { return "fake" }
func (p *fakeProvider) Get(_ context.Context, k string) (string, error) {
	v, ok := p.values[k]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return v, nil
}
func (p *fakeProvider) Set(_ context.Context, k, v string) error {
	if err, ok := p.setErr[k]; ok {
		return err
	}
	p.setLog = append(p.setLog, k)
	p.values[k] = v
	return nil
}
func (p *fakeProvider) Delete(_ context.Context, k string) error {
	if err, ok := p.delErr[k]; ok {
		return err
	}
	p.delLog = append(p.delLog, k)
	delete(p.values, k)
	return nil
}
func (p *fakeProvider) List(_ context.Context) ([]string, error) {
	out := make([]string, 0, len(p.values))
	for k := range p.values {
		out = append(out, k)
	}
	return out, nil
}

func TestRoute_NoSensitive_PassesThrough(t *testing.T) {
	p := newFakeProvider()
	out := &interfaces.ResourceOutput{
		Name: "x", Type: "infra.spaces_key",
		Outputs: map[string]any{"bucket": "b", "endpoint": "e"},
	}
	sanitized, hydrated, err := Route(context.Background(), p, "myres", out)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if len(p.setLog) != 0 {
		t.Errorf("expected no Set calls, got %v", p.setLog)
	}
	if sanitized["bucket"] != "b" || sanitized["endpoint"] != "e" {
		t.Errorf("non-sensitive outputs corrupted: %v", sanitized)
	}
	if len(hydrated) != 0 {
		t.Errorf("expected empty hydrated, got %v", hydrated)
	}
}

func TestRoute_SensitiveValuePresent_RoutesAndSanitizes(t *testing.T) {
	p := newFakeProvider()
	out := &interfaces.ResourceOutput{
		Outputs:   map[string]any{"access_key": "AK", "secret_key": "SECRET", "bucket": "b"},
		Sensitive: map[string]bool{"secret_key": true, "access_key": true},
	}
	sanitized, hydrated, err := Route(context.Background(), p, "myres", out)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if p.values["myres_secret_key"] != "SECRET" {
		t.Errorf("provider did not receive secret_key; got %v", p.values)
	}
	if p.values["myres_access_key"] != "AK" {
		t.Errorf("provider did not receive access_key; got %v", p.values)
	}
	if sanitized["secret_key"] != "secret_ref://myres_secret_key" {
		t.Errorf("sanitized[secret_key] = %v, want placeholder", sanitized["secret_key"])
	}
	if sanitized["access_key"] != "secret_ref://myres_access_key" {
		t.Errorf("sanitized[access_key] = %v, want placeholder", sanitized["access_key"])
	}
	if sanitized["bucket"] != "b" {
		t.Errorf("non-sensitive bucket lost: %v", sanitized["bucket"])
	}
	if hydrated["myres_secret_key"] != "SECRET" {
		t.Errorf("hydrated missing secret_key: %v", hydrated)
	}
}

func TestRoute_SensitiveKeyAbsentFromOutputs_Skipped(t *testing.T) {
	p := newFakeProvider()
	out := &interfaces.ResourceOutput{
		Outputs:   map[string]any{"access_key": "AK"},
		Sensitive: map[string]bool{"secret_key": true, "access_key": true},
	}
	sanitized, hydrated, err := Route(context.Background(), p, "myres", out)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if _, ok := sanitized["secret_key"]; ok {
		t.Errorf("absent sensitive key should NOT yield placeholder, got %v", sanitized["secret_key"])
	}
	if _, ok := p.values["myres_secret_key"]; ok {
		t.Errorf("provider should not have received secret_key (absent value)")
	}
	if hydrated["myres_secret_key"] != "" {
		t.Errorf("hydrated should not contain absent key")
	}
	// access_key was present, should be routed
	if sanitized["access_key"] != "secret_ref://myres_access_key" {
		t.Errorf("access_key routing failed: %v", sanitized["access_key"])
	}
}

func TestRoute_SensitiveFalseValue_NotRouted(t *testing.T) {
	p := newFakeProvider()
	out := &interfaces.ResourceOutput{
		Outputs:   map[string]any{"bucket_uri": "https://b.example/"},
		Sensitive: map[string]bool{"bucket_uri": false},
	}
	sanitized, _, err := Route(context.Background(), p, "myres", out)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if sanitized["bucket_uri"] != "https://b.example/" {
		t.Errorf("Sensitive=false should not route, got %v", sanitized["bucket_uri"])
	}
	if len(p.setLog) != 0 {
		t.Errorf("Sensitive=false triggered Set: %v", p.setLog)
	}
}

func TestRoute_ProviderSetError_ReturnsError(t *testing.T) {
	p := newFakeProvider()
	p.setErr["myres_secret_key"] = errors.New("boom")
	out := &interfaces.ResourceOutput{
		Outputs:   map[string]any{"secret_key": "S"},
		Sensitive: map[string]bool{"secret_key": true},
	}
	_, _, err := Route(context.Background(), p, "myres", out)
	if err == nil {
		t.Fatal("expected error from Set")
	}
}

func TestRoute_NilProviderWithSensitive_Errors(t *testing.T) {
	out := &interfaces.ResourceOutput{
		Outputs:   map[string]any{"secret_key": "S"},
		Sensitive: map[string]bool{"secret_key": true},
	}
	_, _, err := Route(context.Background(), nil, "myres", out)
	if err == nil {
		t.Fatal("expected error when provider nil and Sensitive non-empty")
	}
}

func TestRoute_NilProviderWithoutSensitive_OK(t *testing.T) {
	out := &interfaces.ResourceOutput{
		Outputs: map[string]any{"bucket": "b"},
	}
	sanitized, hydrated, err := Route(context.Background(), nil, "myres", out)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if sanitized["bucket"] != "b" {
		t.Errorf("nil-provider non-sensitive corrupted: %v", sanitized)
	}
	if len(hydrated) != 0 {
		t.Errorf("expected empty hydrated, got %v", hydrated)
	}
}

func TestRoute_EmptyResourceName_Errors(t *testing.T) {
	p := newFakeProvider()
	out := &interfaces.ResourceOutput{
		Outputs:   map[string]any{"secret_key": "S"},
		Sensitive: map[string]bool{"secret_key": true},
	}
	_, _, err := Route(context.Background(), p, "", out)
	if err == nil {
		t.Fatal("expected error on empty resourceName")
	}
}

func TestRoute_DeterministicSetOrder(t *testing.T) {
	// Multiple sensitive keys: ensure Set order is sorted by key (deterministic).
	p := newFakeProvider()
	out := &interfaces.ResourceOutput{
		Outputs:   map[string]any{"a_key": "A", "b_key": "B", "c_key": "C"},
		Sensitive: map[string]bool{"a_key": true, "b_key": true, "c_key": true},
	}
	_, _, err := Route(context.Background(), p, "r", out)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	want := []string{"r_a_key", "r_b_key", "r_c_key"}
	if len(p.setLog) != len(want) {
		t.Fatalf("setLog len: got %v want %v", p.setLog, want)
	}
	for i, w := range want {
		if p.setLog[i] != w {
			t.Errorf("setLog[%d] = %v want %v", i, p.setLog[i], w)
		}
	}
}

func TestRevoke_DeletesAllKeys(t *testing.T) {
	p := newFakeProvider()
	p.values["r_secret_key"] = "S"
	p.values["r_access_key"] = "A"
	if err := Revoke(context.Background(), p, "r", []string{"secret_key", "access_key"}); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, ok := p.values["r_secret_key"]; ok {
		t.Errorf("secret_key not deleted")
	}
	if _, ok := p.values["r_access_key"]; ok {
		t.Errorf("access_key not deleted")
	}
}

func TestRevoke_AggregatesErrors(t *testing.T) {
	p := newFakeProvider()
	p.values["r_secret_key"] = "S"
	p.values["r_access_key"] = "A"
	p.delErr["r_secret_key"] = errors.New("boom1")
	p.delErr["r_access_key"] = errors.New("boom2")
	err := Revoke(context.Background(), p, "r", []string{"secret_key", "access_key"})
	if err == nil {
		t.Fatal("expected aggregated error")
	}
	// both should appear; order is sorted by key
	msg := err.Error()
	if !contains(msg, "boom1") || !contains(msg, "boom2") {
		t.Errorf("aggregated error missing one or both: %q", msg)
	}
}

func TestRevoke_ContinuesAfterError(t *testing.T) {
	// First key errors; second key should still be Deleted.
	p := newFakeProvider()
	p.values["r_secret_key"] = "S"
	p.values["r_access_key"] = "A"
	p.delErr["r_secret_key"] = errors.New("boom")
	_ = Revoke(context.Background(), p, "r", []string{"secret_key", "access_key"})
	if _, ok := p.values["r_access_key"]; ok {
		t.Error("access_key not deleted (Revoke should continue past first error)")
	}
}

func TestIsPlaceholder(t *testing.T) {
	cases := map[string]bool{
		"secret_ref://x":         true,
		"secret_ref://r_key":     true,
		"secret_ref://":          true, // edge: empty key after prefix; still has prefix
		"secret://x":             false,
		"plain":                  false,
		"":                       false,
	}
	for in, want := range cases {
		if got := IsPlaceholder(in); got != want {
			t.Errorf("IsPlaceholder(%q) = %v, want %v", in, got, want)
		}
	}
	// non-string input
	if IsPlaceholder(42) {
		t.Error("IsPlaceholder(int) should be false")
	}
	if IsPlaceholder(nil) {
		t.Error("IsPlaceholder(nil) should be false")
	}
}

func TestMaskSensitiveForDiff_MasksPlaceholdersAndDriverKeys(t *testing.T) {
	desired := map[string]any{"region": "nyc3", "secret_key": "should-mask", "bucket": "b"}
	current := map[string]any{"region": "nyc3", "secret_key": "secret_ref://r_secret_key", "bucket": "b"}
	d2, c2 := MaskSensitiveForDiff([]string{"secret_key"}, desired, current)
	if _, ok := d2["secret_key"]; ok {
		t.Errorf("desired should have secret_key elided, got %v", d2["secret_key"])
	}
	if _, ok := c2["secret_key"]; ok {
		t.Errorf("current should have secret_key elided, got %v", c2["secret_key"])
	}
	if d2["region"] != "nyc3" || c2["region"] != "nyc3" {
		t.Errorf("non-sensitive keys must survive: d=%v c=%v", d2["region"], c2["region"])
	}
}

func TestMaskSensitiveForDiff_PlaceholderInDesired(t *testing.T) {
	// Edge: a desired map carrying a placeholder shouldn't leak it into Diff.
	desired := map[string]any{"k": "secret_ref://r_k"}
	current := map[string]any{"k": "secret_ref://r_k"}
	d2, c2 := MaskSensitiveForDiff(nil, desired, current)
	if _, ok := d2["k"]; ok {
		t.Errorf("placeholder in desired should be elided")
	}
	if _, ok := c2["k"]; ok {
		t.Errorf("placeholder in current should be elided")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

**Step 1.2: Run tests, confirm fail**

Run: `cd iac/sensitive && go test ./...`
Expected: FAIL (package does not exist; `Route` / `Revoke` / `IsPlaceholder` / `MaskSensitiveForDiff` undefined).

**Step 1.3: Implement `iac/sensitive/route.go`**

```go
// Package sensitive routes ResourceOutput fields flagged as Sensitive
// through a secrets.Provider, returning sanitized placeholders for state
// persistence and a hydrated map for in-process consumers.
//
// Per the engine-sensitive-output-routing design (workflow v0.27.0):
//   - Route is invoked on Create/Update only. Read/Adoption/Refresh paths
//     use Sanitize-only logic (not in this package — see
//     cmd/wfctl/infra_apply.go) to prevent cache pollution.
//   - The placeholder format "secret_ref://<resource>_<key>" is distinct
//     from the user-supplied "secret://<key>" config-reference convention.
//   - Routing trigger is exclusively out.Sensitive[k]==true (per-call
//     dynamic). ResourceDriver.SensitiveKeys() is NOT consulted here;
//     it remains a display-masking signal.
package sensitive

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

// PlaceholderPrefix is the URI scheme used in state.Outputs values to
// reference a routed secret stored in the configured secrets.Provider.
// Distinct from secrets.SecretPrefix ("secret://") which is for
// user-supplied config references.
const PlaceholderPrefix = "secret_ref://"

// SecretKey returns the canonical secrets.Provider key for a resource's
// output: "<resourceName>_<outputKey>". Exported so audit-state-secrets
// and other consumers can reverse-engineer routed-secret names.
func SecretKey(resourceName, outputKey string) string {
	return resourceName + "_" + outputKey
}

// Placeholder returns the canonical "secret_ref://<resourceName>_<outputKey>"
// string that replaces a routed value in state.Outputs.
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
// secret as "<resourceName>_<outputKey>". Returns:
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
//     the loop stops and previously-Set values are NOT cleaned up
//     (idempotent overwrite on Apply rerun is the recovery path).
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
		val, err := stringifyOutput(out.Outputs[k])
		if err != nil {
			return nil, nil, fmt.Errorf("sensitive.Route: resource %q key %q: %w", resourceName, k, err)
		}
		secretName := SecretKey(resourceName, k)
		if setErr := provider.Set(ctx, secretName, val); setErr != nil {
			return nil, nil, fmt.Errorf("sensitive.Route: provider.Set(%q): %w", secretName, setErr)
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
```

**Step 1.4: Run tests, confirm pass**

Run: `cd iac/sensitive && go test ./... -v`
Expected: All tests PASS. Specifically these names: `TestRoute_NoSensitive_PassesThrough`, `TestRoute_SensitiveValuePresent_RoutesAndSanitizes`, `TestRoute_SensitiveKeyAbsentFromOutputs_Skipped`, `TestRoute_SensitiveFalseValue_NotRouted`, `TestRoute_ProviderSetError_ReturnsError`, `TestRoute_NilProviderWithSensitive_Errors`, `TestRoute_NilProviderWithoutSensitive_OK`, `TestRoute_EmptyResourceName_Errors`, `TestRoute_DeterministicSetOrder`, `TestRevoke_DeletesAllKeys`, `TestRevoke_AggregatesErrors`, `TestRevoke_ContinuesAfterError`, `TestIsPlaceholder`, `TestMaskSensitiveForDiff_MasksPlaceholdersAndDriverKeys`, `TestMaskSensitiveForDiff_PlaceholderInDesired`.

**Step 1.5: Run race + vet + lint locally**

Run:
- `go test -race ./iac/sensitive/...` → PASS
- `go vet ./iac/sensitive/...` → no output
- `golangci-lint run ./iac/sensitive/...` → no findings

**Step 1.6: Commit**

```bash
git add iac/sensitive/route.go iac/sensitive/route_test.go
git commit -m "$(cat <<'EOF'
feat(iac/sensitive): Route/Revoke/IsPlaceholder/MaskSensitiveForDiff helpers

New iac/sensitive package for engine-side routing of ResourceOutput
sensitive fields through secrets.Provider. Free functions, no struct.
Placeholder format "secret_ref://<resource>_<key>" is distinct from
the existing user-supplied "secret://<key>" config-reference convention.

Routing trigger is exclusively out.Sensitive[k]==true; SensitiveKeys()
remains a display-masking-only signal per design rev1.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: `persistResourceWithSecretRouting` helper + Apply call-site rewires (sites 1 & 2)

**Files:**
- Modify: `cmd/wfctl/infra_apply.go` (add helper + rewire two state-write sites at :550-557 and :1032-1040)
- Create: `cmd/wfctl/infra_apply_sensitive_routing_test.go`

**Change class:** Internal logic + apply path (runtime-affecting). Verification: unit + integration tests; **rollback note required**.

**Rollback:** Revert this commit; the helper is additive and the call sites' literal `Outputs: r.Outputs` shape is one-revert away.

**Step 2.1: Write the failing integration tests**

Create `cmd/wfctl/infra_apply_sensitive_routing_test.go`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/iac/sensitive"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

// envProvider is the test secret store: an in-memory env-like map.
type envProvider struct{ values map[string]string }

func newEnvProvider() *envProvider              { return &envProvider{values: map[string]string{}} }
func (p *envProvider) Name() string             { return "env" }
func (p *envProvider) Get(_ context.Context, k string) (string, error) {
	v, ok := p.values[k]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return v, nil
}
func (p *envProvider) Set(_ context.Context, k, v string) error    { p.values[k] = v; return nil }
func (p *envProvider) Delete(_ context.Context, k string) error    { delete(p.values, k); return nil }
func (p *envProvider) List(_ context.Context) ([]string, error) {
	out := make([]string, 0, len(p.values))
	for k := range p.values {
		out = append(out, k)
	}
	return out, nil
}

// stubStore captures SaveResource calls for assertions.
type stubStore struct {
	saved   []interfaces.ResourceState
	saveErr error
	deleted []string
}

func (s *stubStore) SaveResource(_ context.Context, st interfaces.ResourceState) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved = append(s.saved, st)
	return nil
}
func (s *stubStore) GetResource(_ context.Context, name string) (*interfaces.ResourceState, error) {
	for i := range s.saved {
		if s.saved[i].Name == name {
			return &s.saved[i], nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (s *stubStore) ListResources(_ context.Context) ([]interfaces.ResourceState, error) {
	return s.saved, nil
}
func (s *stubStore) DeleteResource(_ context.Context, n string) error { s.deleted = append(s.deleted, n); return nil }
func (s *stubStore) SavePlan(_ context.Context, _ interfaces.IaCPlan) error { return nil }
func (s *stubStore) GetPlan(_ context.Context, _ string) (*interfaces.IaCPlan, error) { return nil, nil }
func (s *stubStore) Lock(_ context.Context, _ string, _ time.Duration) (interfaces.IaCLockHandle, error) { return nil, nil }
func (s *stubStore) Close() error { return nil }

// stubDriver captures Delete calls (for compensating-Delete tests).
type stubSensitiveDriver struct {
	deleteCalls []interfaces.ResourceRef
	deleteErr   error
}

func (d *stubSensitiveDriver) Create(_ context.Context, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) { return nil, nil }
func (d *stubSensitiveDriver) Read(_ context.Context, _ interfaces.ResourceRef) (*interfaces.ResourceOutput, error) { return nil, nil }
func (d *stubSensitiveDriver) Update(_ context.Context, _ interfaces.ResourceRef, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) { return nil, nil }
func (d *stubSensitiveDriver) Delete(_ context.Context, ref interfaces.ResourceRef) error {
	d.deleteCalls = append(d.deleteCalls, ref)
	return d.deleteErr
}
func (d *stubSensitiveDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, _ *interfaces.ResourceOutput) (*interfaces.DiffResult, error) { return nil, nil }
func (d *stubSensitiveDriver) HealthCheck(_ context.Context, _ interfaces.ResourceRef) (*interfaces.HealthResult, error) { return nil, nil }
func (d *stubSensitiveDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) { return nil, nil }
func (d *stubSensitiveDriver) SensitiveKeys() []string { return nil }

func TestPersistResourceWithSecretRouting_RoutesSensitiveAndSanitizesState(t *testing.T) {
	prov := newEnvProvider()
	store := &stubStore{}
	drv := &stubSensitiveDriver{}
	out := interfaces.ResourceOutput{
		Name: "myres", Type: "infra.spaces_key", ProviderID: "AKIA",
		Outputs:   map[string]any{"access_key": "AK", "secret_key": "SK", "bucket": "b"},
		Sensitive: map[string]bool{"access_key": true, "secret_key": true},
	}
	rs := interfaces.ResourceState{
		ID: "myres", Name: "myres", Type: "infra.spaces_key",
		Provider: "digitalocean", ProviderID: "AKIA",
	}
	hydrated, err := persistResourceWithSecretRouting(context.Background(), store, prov, drv, rs, out, persistModeApply)
	if err != nil {
		t.Fatalf("persist: %v", err)
	}
	if len(store.saved) != 1 {
		t.Fatalf("expected 1 saved, got %d", len(store.saved))
	}
	state := store.saved[0]
	if state.Outputs["secret_key"] != "secret_ref://myres_secret_key" {
		t.Errorf("state secret_key not sanitized: %v", state.Outputs["secret_key"])
	}
	if state.Outputs["access_key"] != "secret_ref://myres_access_key" {
		t.Errorf("state access_key not sanitized: %v", state.Outputs["access_key"])
	}
	if state.Outputs["bucket"] != "b" {
		t.Errorf("state bucket lost: %v", state.Outputs["bucket"])
	}
	if prov.values["myres_secret_key"] != "SK" {
		t.Errorf("provider missing secret_key value")
	}
	if hydrated["myres_secret_key"] != "SK" {
		t.Errorf("hydrated missing secret_key: %v", hydrated)
	}
}

func TestPersistResourceWithSecretRouting_NoProviderHardFails(t *testing.T) {
	store := &stubStore{}
	drv := &stubSensitiveDriver{}
	out := interfaces.ResourceOutput{
		Outputs:   map[string]any{"secret_key": "SK"},
		Sensitive: map[string]bool{"secret_key": true},
	}
	rs := interfaces.ResourceState{Name: "myres"}
	_, err := persistResourceWithSecretRouting(context.Background(), store, nil, drv, rs, out, persistModeApply)
	if err == nil {
		t.Fatal("expected error when provider nil and sensitive non-empty")
	}
	if !strings.Contains(err.Error(), "myres") {
		t.Errorf("error should name resource, got %q", err.Error())
	}
	if len(store.saved) != 0 {
		t.Error("state should NOT be saved when routing fails")
	}
}

func TestPersistResourceWithSecretRouting_SaveFailureCompensatesWithDelete(t *testing.T) {
	prov := newEnvProvider()
	store := &stubStore{saveErr: errors.New("disk full")}
	drv := &stubSensitiveDriver{}
	out := interfaces.ResourceOutput{
		Name: "myres", ProviderID: "AKIA",
		Outputs:   map[string]any{"secret_key": "SK"},
		Sensitive: map[string]bool{"secret_key": true},
	}
	rs := interfaces.ResourceState{Name: "myres", Type: "infra.spaces_key", ProviderID: "AKIA"}
	_, err := persistResourceWithSecretRouting(context.Background(), store, prov, drv, rs, out, persistModeApply)
	if err == nil {
		t.Fatal("expected error from SaveResource")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("error should wrap original SaveResource err, got %q", err.Error())
	}
	if len(drv.deleteCalls) != 1 {
		t.Errorf("expected 1 compensating Delete call, got %d", len(drv.deleteCalls))
	}
	if _, ok := prov.values["myres_secret_key"]; ok {
		t.Errorf("compensating Delete should have removed routed secret; got %v", prov.values)
	}
}

func TestPersistResourceWithSecretRouting_NoSensitivePassesThrough(t *testing.T) {
	store := &stubStore{}
	drv := &stubSensitiveDriver{}
	out := interfaces.ResourceOutput{
		Outputs: map[string]any{"bucket": "b"},
	}
	rs := interfaces.ResourceState{Name: "myres"}
	hydrated, err := persistResourceWithSecretRouting(context.Background(), store, nil, drv, rs, out, persistModeApply)
	if err != nil {
		t.Fatalf("persist: %v", err)
	}
	if len(hydrated) != 0 {
		t.Errorf("hydrated should be empty: %v", hydrated)
	}
	if len(store.saved) != 1 {
		t.Fatalf("expected 1 saved, got %d", len(store.saved))
	}
	if store.saved[0].Outputs["bucket"] != "b" {
		t.Errorf("non-sensitive output corrupted: %v", store.saved[0].Outputs)
	}
}

// Idempotent re-Apply: routing twice with same value is safe.
func TestPersistResourceWithSecretRouting_Idempotent(t *testing.T) {
	prov := newEnvProvider()
	store := &stubStore{}
	drv := &stubSensitiveDriver{}
	out := interfaces.ResourceOutput{
		Outputs:   map[string]any{"secret_key": "SK"},
		Sensitive: map[string]bool{"secret_key": true},
	}
	rs := interfaces.ResourceState{Name: "myres"}
	for i := 0; i < 2; i++ {
		_, err := persistResourceWithSecretRouting(context.Background(), store, prov, drv, rs, out, persistModeApply)
		if err != nil {
			t.Fatalf("persist iter %d: %v", i, err)
		}
	}
	if prov.values["myres_secret_key"] != "SK" {
		t.Errorf("provider value lost on re-Apply: %v", prov.values)
	}
	if len(store.saved) != 2 {
		t.Errorf("expected 2 saved, got %d", len(store.saved))
	}
}
```

Note: `infraStateStore` interface in `cmd/wfctl/infra_state_store.go:22` matches the methods defined here on `stubStore`. The test file uses `interfaces.IaCStateStore` directly via the helper signature; the helper takes the broader `interfaces.IaCStateStore` so test-stub coupling is minimal.

**Step 2.2: Run tests, confirm fail**

Run: `cd cmd/wfctl && go test -run TestPersistResourceWithSecretRouting -v`
Expected: FAIL — `persistResourceWithSecretRouting` undefined; `persistModeApply` undefined.

**Step 2.3: Implement the helper in `cmd/wfctl/infra_apply.go`**

Add near the bottom of `cmd/wfctl/infra_apply.go` (after `cloneMap` at ~line 736):

```go
// persistMode controls how persistResourceWithSecretRouting handles a
// driver output: persistModeApply routes sensitive fields through the
// provider; persistModeRead is sanitize-only (no provider writes — used
// by adoption / refresh paths to avoid cache pollution).
type persistMode int

const (
	persistModeApply persistMode = iota
	persistModeRead
)

// persistResourceWithSecretRouting builds rs.Outputs from out (routing
// sensitive fields through provider in apply mode), calls
// store.SaveResource, and returns the hydrated routed-secret map for
// in-process hand-off. On SaveResource failure after provider.Set
// succeeded (apply mode only), invokes driver.Delete + provider.Delete
// to compensate the partial cloud-resource creation. Returns a wrapped
// error naming both the original SaveResource failure and the
// compensating-Delete outcome.
//
// In read mode, the helper does NOT call provider.Set; instead it consults
// the prior state via store.GetResource and inherits any
// PlaceholderPrefix entries; new sensitive keys (declared by the driver
// at Read time but not previously routed) are dropped from sanitized.
//
// Returns nil hydrated map in read mode (consumers don't need post-apply
// hand-off for a Read).
//
// Note: store is the wfctl-internal infraStateStore interface (defined in
// cmd/wfctl/infra_state_store.go) — NOT interfaces.IaCStateStore. The
// helper lives in the cmd/wfctl package; using the package-private
// interface keeps the boundary clean and matches existing callers
// (applyWithProviderAndStore, adoptExistingResources). The fakes in
// _test.go implement infraStateStore (a smaller surface than the engine
// IaCStateStore: SaveResource / GetResource / ListResources /
// DeleteResource / Close).
func persistResourceWithSecretRouting(
	ctx context.Context,
	store infraStateStore,
	provider secrets.Provider,
	driver interfaces.ResourceDriver,
	rs interfaces.ResourceState,
	out interfaces.ResourceOutput,
	mode persistMode,
) (map[string]string, error) {
	switch mode {
	case persistModeApply:
		return persistApplyMode(ctx, store, provider, driver, rs, out)
	case persistModeRead:
		return nil, persistReadMode(ctx, store, rs, out)
	default:
		return nil, fmt.Errorf("persistResourceWithSecretRouting: unknown mode %d", mode)
	}
}

func persistApplyMode(
	ctx context.Context,
	store infraStateStore,
	provider secrets.Provider,
	driver interfaces.ResourceDriver,
	rs interfaces.ResourceState,
	out interfaces.ResourceOutput,
) (map[string]string, error) {
	sanitized, hydrated, err := sensitive.Route(ctx, provider, rs.Name, &out)
	if err != nil {
		return nil, fmt.Errorf("%s/%s: route sensitive outputs: %w", rs.Type, rs.Name, err)
	}
	rs.Outputs = sanitized
	if saveErr := store.SaveResource(ctx, rs); saveErr != nil {
		// Compensating Delete: if we routed secrets, the matching cloud
		// resource is real but the state record didn't land. Roll back so
		// a re-Apply doesn't double-create.
		if len(hydrated) > 0 {
			compErr := compensateAfterSaveFailure(ctx, provider, driver, rs, hydrated)
			if compErr != nil {
				return nil, fmt.Errorf("%s/%s: persist state after apply: %w (compensating delete failed: %v)", rs.Type, rs.Name, saveErr, compErr)
			}
			return nil, fmt.Errorf("%s/%s: persist state after apply: %w (compensating delete succeeded)", rs.Type, rs.Name, saveErr)
		}
		return nil, fmt.Errorf("%s/%s: persist state after apply: %w", rs.Type, rs.Name, saveErr)
	}
	return hydrated, nil
}

func persistReadMode(
	ctx context.Context,
	store infraStateStore,
	rs interfaces.ResourceState,
	out interfaces.ResourceOutput,
) error {
	// Sanitize-only: inherit placeholders from prior state for sensitive
	// keys; drop newly-declared sensitive keys. Do NOT call provider.Set.
	prior, _ := store.GetResource(ctx, rs.Name) // best-effort; nil-safe
	sanitized := make(map[string]any, len(out.Outputs))
	for k, v := range out.Outputs {
		sanitized[k] = v
	}
	for k, flag := range out.Sensitive {
		if !flag {
			continue
		}
		// If prior state has a placeholder for this key, preserve it.
		if prior != nil {
			if pv, ok := prior.Outputs[k]; ok && sensitive.IsPlaceholder(pv) {
				sanitized[k] = pv
				continue
			}
		}
		// Otherwise drop the field from sanitized — we don't have a
		// previously-routed secret and we won't pollute the provider
		// from a Read.
		delete(sanitized, k)
	}
	rs.Outputs = sanitized
	if err := store.SaveResource(ctx, rs); err != nil {
		return fmt.Errorf("%s/%s: persist state after read: %w", rs.Type, rs.Name, err)
	}
	return nil
}

func compensateAfterSaveFailure(
	ctx context.Context,
	provider secrets.Provider,
	driver interfaces.ResourceDriver,
	rs interfaces.ResourceState,
	hydrated map[string]string,
) error {
	var errs []error
	if driver != nil {
		ref := interfaces.ResourceRef{Name: rs.Name, Type: rs.Type, ProviderID: rs.ProviderID}
		if delErr := driver.Delete(ctx, ref); delErr != nil {
			errs = append(errs, fmt.Errorf("driver.Delete: %w", delErr))
		}
	}
	if provider != nil {
		// Reverse-engineer the original output keys from the secret names.
		// Format: "<rs.Name>_<output_key>"; strip the prefix.
		for secretName := range hydrated {
			if delErr := provider.Delete(ctx, secretName); delErr != nil && !errors.Is(delErr, secrets.ErrNotFound) {
				errs = append(errs, fmt.Errorf("provider.Delete(%s): %w", secretName, delErr))
			}
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
```

Add the imports if absent (Go fmt will hoist them; explicit list for clarity):

```go
import (
	"errors" // already present
	// ... existing imports ...
	"github.com/GoCodeAlone/workflow/iac/sensitive"
	"github.com/GoCodeAlone/workflow/secrets"
)
```

**Step 2.4: Rewire state-write call sites 1 & 2 in `cmd/wfctl/infra_apply.go`**

Site 1 — `applyWithProviderAndStore` at lines 540-557. Replace the literal `ResourceState{...Outputs: r.Outputs ...}` block:

Find the existing block:
```go
				now := time.Now().UTC()
				rs := interfaces.ResourceState{
					ID:                  r.Name,
					Name:                r.Name,
					Type:                r.Type,
					Provider:            providerType,
					ProviderRef:         providerRef,
					ProviderID:          r.ProviderID,
					ConfigHash:          configHashMap(appliedCfg),
					AppliedConfig:       appliedCfg,
					AppliedConfigSource: "apply",
					Outputs:             r.Outputs,
					Dependencies:        dependencies,
					CreatedAt:           now,
					UpdatedAt:           now,
				}
				if saveErr := store.SaveResource(ctx, rs); saveErr != nil {
					return fmt.Errorf("%s/%s: persist state after apply: %w", r.Type, r.Name, saveErr)
				}
```

Replace with:
```go
				now := time.Now().UTC()
				rs := interfaces.ResourceState{
					ID:                  r.Name,
					Name:                r.Name,
					Type:                r.Type,
					Provider:            providerType,
					ProviderRef:         providerRef,
					ProviderID:          r.ProviderID,
					ConfigHash:          configHashMap(appliedCfg),
					AppliedConfig:       appliedCfg,
					AppliedConfigSource: "apply",
					// Outputs is set by persistResourceWithSecretRouting after Route.
					Dependencies: dependencies,
					CreatedAt:    now,
					UpdatedAt:    now,
				}
				driver, _ := provider.ResourceDriver(r.Type) // best-effort for compensating Delete; nil-safe in helper
				h, persistErr := persistResourceWithSecretRouting(ctx, store, secretsProvider, driver, rs, r, persistModeApply)
				if persistErr != nil {
					return persistErr
				}
				for k, v := range h {
					hydratedAll[k] = v
				}
```

Site 2 — In-process apply path at lines 1022-1040. Same replacement pattern; identical helper invocation.

**Required signature changes** (apply both functions):

1. `applyWithProviderAndStore` (`cmd/wfctl/infra_apply.go:360`) — add `cfgFile string` parameter, change return to `(map[string]string, error)`:

```go
func applyWithProviderAndStore(
    ctx context.Context,
    provider interfaces.IaCProvider,
    providerType string,
    cfgFile string, // NEW
    specs []interfaces.ResourceSpec,
    current []interfaces.ResourceState,
    store infraStateStore,
    w io.Writer,
    envName string,
) (map[string]string, error) {
    hydratedAll := make(map[string]string)
    secretsProvider, err := loadSecretsProviderForRouting(cfgFile)
    if err != nil {
        return nil, err
    }
    // ... existing body ...
    return hydratedAll, nil // single trailing return at every existing return-nil site
}
```

2. `applyPrecomputedPlanWithStore` (`cmd/wfctl/infra_apply.go:910`) — same parameter + return changes:

```go
func applyPrecomputedPlanWithStore(
    ctx context.Context,
    plan interfaces.IaCPlan,
    provider interfaces.IaCProvider,
    providerType string,
    cfgFile string, // NEW
    store infraStateStore,
    w io.Writer,
    envName string,
) (map[string]string, error)
```

Loader helper (in `infra_apply.go`, near the bottom):

```go
// loadSecretsProviderForRouting returns the configured secrets.Provider for
// this apply run, or (nil, nil) when secretsCfg is absent. The caller's
// downstream sensitive.Route will hard-fail if any driver emits sensitive
// outputs and provider is nil. cfgFile is the same resolved infra.yaml path
// the rest of the apply pipeline uses.
func loadSecretsProviderForRouting(cfgFile string) (secrets.Provider, error) {
    cfg, err := parseSecretsConfig(cfgFile)
    if err != nil {
        return nil, fmt.Errorf("parse secrets config for sensitive routing: %w", err)
    }
    if cfg == nil {
        return nil, nil
    }
    return resolveSecretsProvider(cfg)
}
```

3. Update the **caller** at `cmd/wfctl/infra_apply.go:267` (and the precomputed-plan caller at `:886`) to pass `cfgFile`:

```go
// Was:
return applyWithProviderAndStore(ctx, provider, g.provType, g.specs, scopedCurrent, store, os.Stderr, envName)
// Becomes:
hyd, err := applyWithProviderAndStore(ctx, provider, g.provType, cfgFile, g.specs, scopedCurrent, store, os.Stderr, envName)
if err != nil {
    return err
}
for k, v := range hyd {
    runHydrated[k] = v
}
return nil
```

`runHydrated` is a function-scope `map[string]string` declared at the top of `applyInfraModules` (or the equivalent caller). After the dispatch loop completes, `runHydrated` is passed into `syncInfraOutputSecrets` (Step 2.5).

`cfgFile` is already in scope at `infra_apply.go:244` (passed as parameter to `applyInfraModules`).

4. Update existing tests at `cmd/wfctl/infra_apply_allow_replace_test.go:223,267` (and any other test calling `applyWithProviderAndStore`) to:
   - pass empty string for the new `cfgFile` parameter.
   - update return-value handling: `_, err := applyWithProviderAndStore(...)`.

Pre-flight grep to enumerate every call site that must update:

```
grep -rn "applyWithProviderAndStore(\|applyPrecomputedPlanWithStore(" cmd/wfctl/
```

5. **Preserve existing pre-save validation.** The original code at `infra_apply.go:520-524` calls `validateOutputProviderID(provider, providerType, &r)` BEFORE state save. The replacement MUST keep this call unchanged, BEFORE invoking `persistResourceWithSecretRouting`:

```go
// Hard-fail when the driver returns a malformed ProviderID for a strict format.
if err := validateOutputProviderID(provider, providerType, &r); err != nil {
    return nil, fmt.Errorf("state write rejected: %w", err)
}
// THEN call persistResourceWithSecretRouting (replaces the old SaveResource call)
```

Same preservation requirement at the in-process apply path (`:1004` `validateOutputProviderID` call).

6. **Pre-flight check: detect sensitive-emitting drivers before the persistence loop.** When `secretsProvider == nil` AND `result.Resources` contains any `*ResourceOutput` with non-empty `Sensitive` map, surface a single clear error BEFORE entering the per-resource persistence loop. This prevents partial apply (one resource erroring mid-loop while previous ones already persisted plaintext-but-sanitized state).

```go
if secretsProvider == nil {
    for i := range result.Resources {
        if hasSensitiveOutputs(&result.Resources[i]) {
            return nil, fmt.Errorf(
                "secrets.Provider not configured but driver emitted sensitive outputs (resource %q has Sensitive map %v); add `secrets:` block to your config or use `secrets: { provider: env }`",
                result.Resources[i].Name, sensitiveKeysFor(&result.Resources[i]))
        }
    }
}
// ... persistence loop unchanged ...

func hasSensitiveOutputs(r *interfaces.ResourceOutput) bool {
    for _, v := range r.Sensitive {
        if v {
            return true
        }
    }
    return false
}

func sensitiveKeysFor(r *interfaces.ResourceOutput) []string {
    var keys []string
    for k, v := range r.Sensitive {
        if v {
            keys = append(keys, k)
        }
    }
    sort.Strings(keys)
    return keys
}
```

Same pre-flight in `applyPrecomputedPlanWithStore`.

7. **Compensation context isolation.** The `compensateAfterSaveFailure` helper inherits the apply ctx; if the apply was canceled, Delete may also fail. Use a fresh 30-second timeout context for compensation specifically:

```go
func compensateAfterSaveFailure(
    parentCtx context.Context, /* logging only */
    provider secrets.Provider,
    driver interfaces.ResourceDriver,
    rs interfaces.ResourceState,
    hydrated map[string]string,
) error {
    // Fresh context — compensation must run even on parent ctx cancel.
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    // ... existing body uses ctx, not parentCtx ...
}
```

8. **String-only sensitive values (v0.27.0 limitation).** `stringifyOutput` errors on non-string values. Document this as the v0.27.0 limitation in `iac/sensitive/route.go`'s package comment AND in DOCUMENTATION.md (Task 6 §6.1 "Sensitive output routing" section). Future expansion (e.g., `MarshalSensitive` interface) is out of scope.

**Step 2.5: Rewire `runInfraApply` to thread `hydratedAll` to `syncInfraOutputSecrets`**

In `cmd/wfctl/infra.go`, around line 1414-1450, modify `applyInfraModules` (or wherever `applyWithProviderAndStore` is invoked) to capture the returned `hydratedAll` map and pass it to `syncInfraOutputSecrets`.

Update `syncInfraOutputSecrets` signature in `cmd/wfctl/infra_output_secrets.go:100`:

```go
func syncInfraOutputSecrets(
	ctx context.Context,
	secretsCfg *SecretsConfig,
	provider secrets.Provider,
	states []interfaces.ResourceState,
	wfCfg *config.WorkflowConfig,
	envName string,
	hydrated map[string]string, // NEW: same-process routed-secret hand-off
) error {
	// ... existing body ...
	// Modify resolveInfraOutput call site to consult hydrated FIRST.
}
```

Modify `resolveInfraOutput` (`cmd/wfctl/infra_output_secrets.go:37`):

```go
func resolveInfraOutput(wfCfg *config.WorkflowConfig, source, envName string, stateOutputs map[string]map[string]any, hydrated map[string]string) (string, error) {
	// ... existing module-name resolution ...

	val, ok := outputs[field]
	if !ok {
		return "", fmt.Errorf("infra_output: field %q not found in outputs of module %q", field, moduleName)
	}
	// If state has a placeholder, prefer the hydrated map.
	if sensitive.IsPlaceholder(val) {
		secretName := strings.TrimPrefix(val.(string), sensitive.PlaceholderPrefix)
		if hv, hok := hydrated[secretName]; hok {
			return hv, nil
		}
		// Fall back to provider.Get (nil-safe; tests cover write-only-host case).
		// The provider is reachable through secretsCfg already loaded by caller.
		return "", fmt.Errorf("infra_output: field %q is a routed-secret placeholder %q; not in same-process hydrated map (write-only providers cannot rehydrate cold)", field, val)
	}
	s, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("infra_output: output field %q of module %q is %T, expected string", field, moduleName, val)
	}
	return s, nil
}
```

Update all call sites of `resolveInfraOutput` (currently at `infra_output_secrets.go:163` and `infra_resolve_state.go:123`) to pass the `hydrated` map. For the `infra_resolve_state.go` site (drift detection / state preview), pass `nil` — that path doesn't have a hydrated map. The error path on placeholder + nil hydrated is the documented constraint.

**Step 2.5.1: Update existing tests for the new `hydrated` parameter**

Update every test file that calls `syncInfraOutputSecrets` or `resolveInfraOutput` to pass `nil` (or appropriate map) for the new parameter:

```
grep -rn "syncInfraOutputSecrets(\|resolveInfraOutput(" cmd/wfctl/
```

Expected hits include:
- `cmd/wfctl/infra_output_secrets_test.go:108-146` — multiple `syncInfraOutputSecrets` calls; add `nil` as the trailing argument.
- Any other test at the same call sites.

For each existing call, append `nil` (or the appropriate map for tests that exercise the hydrated path).

**Step 2.5.2: Add `nil`-hydrated-map fallback test in `infra_output_secrets_test.go`**

Add a test that verifies: state has placeholder, hydrated is nil, provider has the value via Get → resolveInfraOutput succeeds. Plus the inverse: state has placeholder, hydrated is nil, provider returns ErrUnsupported → resolveInfraOutput returns the documented "write-only providers cannot rehydrate cold" error.

**Step 2.6: Run tests, confirm pass**

Run: `cd cmd/wfctl && go test -run TestPersistResourceWithSecretRouting -v`
Expected: All 5 TestPersistResource* tests PASS.

Run: `go test ./cmd/wfctl/... ./iac/sensitive/... ./secrets/...`
Expected: All PASS — the rewired call sites do not break existing apply tests.

Specifically check: `TestApplyPlan_*`, `TestRunInfra*` test names should still pass. If existing tests fail because they don't account for the new helper signature, update them to pass nil/empty for the new params.

**Step 2.7: Run race + vet + lint**

Run:
- `go test -race ./cmd/wfctl/... ./iac/sensitive/...` → PASS
- `go vet ./...` → no output
- `golangci-lint run ./cmd/wfctl/... ./iac/sensitive/...` → no findings

**Step 2.7.bis: Runtime-launch-validation smoke**

Task 2 modifies the apply runtime path. Per `superpowers:runtime-launch-validation`, build the artifact and exercise it under realistic conditions:

```sh
go build -o wfctl ./cmd/wfctl
mkdir -p /tmp/wfctl-routing-smoke && cd /tmp/wfctl-routing-smoke
```

Create `infra-no-secrets.yaml` (no `secrets:` block) and `infra-with-env.yaml` (with `secrets: { provider: env, config: { prefix: WFCTL_TEST_ } }`). Each declares an iac.state file backend + at least one stub-provider resource. (Implementer borrows from `cmd/wfctl/testdata/` if a sample exists; otherwise constructs minimally.)

Run two scenarios — capture transcripts in PR body:

1. **No-secrets-cfg + sensitive-emitting driver** → expected: hard fail with the documented error message naming the resource and sensitive keys.
2. **env-provider configured + sensitive-emitting driver** → expected: apply succeeds; `env | grep WFCTL_TEST_` shows the routed value; `cat <state-file>.json` shows `secret_ref://...` placeholders, no plaintext.

If a stub provider is not readily available, this validation runs against `mock` provider + an additional unit test that asserts both behaviours via the helper directly. Document the substitution in the PR body.

**Rollback note for Task 2:** revert this commit; helper + call-site rewires are additive. Existing state files written under v0.27.0 retain `secret_ref://` placeholders; v0.26.x consumers see them as literal strings (rotate affected secrets via `wfctl infra bootstrap --force-rotate <name>` before downgrading).

**Step 2.8: Commit**

```bash
git add cmd/wfctl/infra_apply.go cmd/wfctl/infra_apply_sensitive_routing_test.go \
        cmd/wfctl/infra.go cmd/wfctl/infra_output_secrets.go cmd/wfctl/infra_resolve_state.go
git commit -m "$(cat <<'EOF'
feat(wfctl): persistResourceWithSecretRouting + Apply call-site routing

Introduces persistResourceWithSecretRouting helper that funnels both
state-write call sites in applyWithProviderAndStore + in-process apply.
Routes per-call Sensitive ResourceOutput fields through secrets.Provider
into "secret_ref://" placeholders before SaveResource.

On SaveResource failure post-Set, compensates with driver.Delete +
provider.Delete to prevent orphan cloud resources.

In-memory hydrated map flows from apply through to syncInfraOutputSecrets
for same-process consumers (works on write-only GitHub provider).

Rollback: revert this commit; the helper is additive and call sites
revert to the prior literal Outputs: r.Outputs shape in one diff.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Read/Adoption/Refresh sanitize-only at sites 3, 4, 5

**Files:**
- Modify: `cmd/wfctl/infra_apply.go` (`adoptExistingResources` site at :637, `resourceStateFromLiveOutput` builder at :689-705)
- Modify: `cmd/wfctl/infra_refresh_outputs.go` (site at :244)

**Change class:** Internal logic + apply path (runtime-affecting). Verification: unit tests + integration tests.

**Rollback:** Revert this commit; the sanitize-only logic is additive.

**Step 3.1: Write failing tests**

Append to `cmd/wfctl/infra_apply_sensitive_routing_test.go`:

```go
func TestPersistResourceWithSecretRouting_ReadModeSanitizeOnly_PreservesPriorPlaceholder(t *testing.T) {
	prov := newEnvProvider() // should not be touched
	// Pre-existing state has a placeholder
	store := &stubStore{
		saved: []interfaces.ResourceState{
			{Name: "myres", Outputs: map[string]any{"secret_key": "secret_ref://myres_secret_key", "bucket": "b"}},
		},
	}
	out := interfaces.ResourceOutput{
		Outputs:   map[string]any{"bucket": "b"}, // Read can't re-emit secret_key
		Sensitive: map[string]bool{"secret_key": true},
	}
	rs := interfaces.ResourceState{Name: "myres"}
	_, err := persistResourceWithSecretRouting(context.Background(), store, prov, &stubSensitiveDriver{}, rs, out, persistModeRead)
	if err != nil {
		t.Fatalf("persist read-mode: %v", err)
	}
	if len(prov.values) != 0 {
		t.Errorf("Read mode must NOT call provider.Set; got %v", prov.values)
	}
	if len(store.saved) != 2 {
		t.Fatalf("expected 2 saves (initial + this), got %d", len(store.saved))
	}
	latest := store.saved[1]
	if latest.Outputs["secret_key"] != "secret_ref://myres_secret_key" {
		t.Errorf("Read mode lost prior placeholder: %v", latest.Outputs["secret_key"])
	}
	if latest.Outputs["bucket"] != "b" {
		t.Errorf("Read mode lost bucket: %v", latest.Outputs["bucket"])
	}
}

func TestPersistResourceWithSecretRouting_ReadModeNewSensitiveKey_Dropped(t *testing.T) {
	// No prior state; driver newly declares sensitive on Read.
	prov := newEnvProvider()
	store := &stubStore{}
	out := interfaces.ResourceOutput{
		Outputs:   map[string]any{"secret_key": "FRESH-FROM-CLOUD-CACHE", "bucket": "b"},
		Sensitive: map[string]bool{"secret_key": true},
	}
	rs := interfaces.ResourceState{Name: "myres"}
	_, err := persistResourceWithSecretRouting(context.Background(), store, prov, &stubSensitiveDriver{}, rs, out, persistModeRead)
	if err != nil {
		t.Fatalf("persist read-mode: %v", err)
	}
	if len(prov.values) != 0 {
		t.Errorf("Read mode must NOT call provider.Set even for newly-declared sensitive; got %v", prov.values)
	}
	if len(store.saved) != 1 {
		t.Fatalf("expected 1 save, got %d", len(store.saved))
	}
	if _, ok := store.saved[0].Outputs["secret_key"]; ok {
		t.Errorf("newly-declared sensitive (no prior placeholder) should be dropped; got %v", store.saved[0].Outputs["secret_key"])
	}
	if store.saved[0].Outputs["bucket"] != "b" {
		t.Errorf("non-sensitive bucket lost: %v", store.saved[0].Outputs["bucket"])
	}
}
```

**Step 3.2: Run, confirm fail**

Run: `cd cmd/wfctl && go test -run TestPersistResourceWithSecretRouting_ReadMode -v`
Expected: PASS already from Task 2's persistReadMode (the helper already supports read mode). If FAIL, fix the implementation per the test expectations.

If they pass already, great — proceed to call-site rewires.

**Step 3.3: Rewire site 3 (`adoptExistingResources` at :637)**

In `cmd/wfctl/infra_apply.go`, the adopt path constructs state from `live *interfaces.ResourceOutput` via `resourceStateFromLiveOutput` and calls `store.SaveResource`. Replace with:

```go
		state, err := resourceStateFromLiveOutput(spec, providerType, live)
		if err != nil {
			return nil, err
		}
		if err := validateStateProviderID(provider, providerType, state); err != nil {
			return nil, err
		}
		// Sanitize-only via persistResourceWithSecretRouting in read mode:
		// driver may declare Sensitive on Read but we MUST NOT call
		// provider.Set from a Read path (cache-pollution risk per design §4.4).
		// Provider may be nil; helper is nil-safe in read mode.
		if _, err := persistResourceWithSecretRouting(ctx, store, nil, driver, state, *live, persistModeRead); err != nil {
			return nil, fmt.Errorf("%s/%s: persist adopted state: %w", spec.Type, spec.Name, err)
		}
```

Remove the original `if saveErr := store.SaveResource(...)` call directly above (the helper now does it).

**Step 3.4: Rewire site 4 (`resourceStateFromLiveOutput` at :689-705)**

This is the builder, not a save site. The `Outputs: cloneMap(live.Outputs)` line is correct as-is for the builder; sanitization happens in step 3.3 via the helper. No change needed at site 4 itself.

**Step 3.5: Rewire site 5 (`infra_refresh_outputs.go:244`)**

```go
		// Replace:
		// if err := store.SaveResource(ctx, fresh); err != nil {
		//     ...
		// }
		// With:
		ro := interfaces.ResourceOutput{
			Name: fresh.Name, Type: fresh.Type, ProviderID: fresh.ProviderID,
			Outputs: fresh.Outputs,
		}
		// driver.Sensitive is per-call; reconstruct from driver.SensitiveKeys
		// for backward-compat — refresh path doesn't have the per-call map.
		if drv, derr := provider.ResourceDriver(fresh.Type); derr == nil && drv != nil {
			sk := drv.SensitiveKeys()
			if len(sk) > 0 {
				ro.Sensitive = make(map[string]bool, len(sk))
				for _, k := range sk {
					ro.Sensitive[k] = true
				}
			}
		}
		if _, err := persistResourceWithSecretRouting(ctx, store, nil, nil, fresh, ro, persistModeRead); err != nil {
			return fmt.Errorf("refresh outputs %s: %w", fresh.Name, err)
		}
```

The refresh path uses `SensitiveKeys()` (the static driver declaration) as the masking source since refresh works from the state record, not from a fresh per-call `ResourceOutput.Sensitive` map. This is the **only** consumer of `SensitiveKeys()` for sanitization (still distinct from routing — Read paths NEVER route).

**Step 3.6: Run all tests**

Run: `go test ./cmd/wfctl/... ./iac/sensitive/...`
Expected: PASS. Existing adoption + refresh tests must continue to pass; if they assert on `state.Outputs[<sensitive>]` value, update to match the sanitize-only behaviour (placeholder if prior state; absent if no prior).

**Step 3.7: Run race + vet + lint**

Run:
- `go test -race ./cmd/wfctl/... ./iac/sensitive/...` → PASS
- `go vet ./...` → no output
- `golangci-lint run ./cmd/wfctl/...` → no findings

**Step 3.7.bis: Runtime-launch-validation smoke for refresh path**

Task 3 modifies the refresh + adoption runtime paths. Build wfctl and exercise:

```sh
go build -o wfctl ./cmd/wfctl
# Using the same fixture from Step 2.7.bis:
cd /tmp/wfctl-routing-smoke
# After Task 2's apply ran successfully (placeholders in state):
./wfctl infra refresh-outputs -c infra-with-env.yaml 2>&1 | tee refresh.log
```

Expected: refresh completes; state file's `secret_ref://...` placeholder for `secret_key` is preserved (verified via `cat <state-file>.json | grep secret_ref`); env vars (the routed-secret store) are NOT modified by the Read path (verify by snapshotting `env | grep WFCTL_TEST_` before and after — should be identical).

**Rollback note for Task 3:** revert this commit; sites revert to direct `store.SaveResource(ctx, fresh)`. State written under Task 3 has the same shape as Task 2 — same downgrade considerations apply.

**Step 3.8: Commit**

```bash
git add cmd/wfctl/infra_apply.go cmd/wfctl/infra_refresh_outputs.go cmd/wfctl/infra_apply_sensitive_routing_test.go
git commit -m "$(cat <<'EOF'
feat(wfctl): sanitize-only routing for adoption + refresh paths

Read paths (adoptExistingResources, runInfraRefreshOutputs) now route
state writes through persistResourceWithSecretRouting in persistModeRead.
The helper inherits placeholders from prior state and drops
newly-declared sensitive keys — never calls provider.Set from a Read
path (prevents cache pollution per design §4.4).

Rollback: revert this commit; sites revert to direct store.SaveResource.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: `wfctl infra audit-state-secrets` command

**Files:**
- Create: `cmd/wfctl/infra_audit_state_secrets.go`
- Create: `cmd/wfctl/infra_audit_state_secrets_test.go`
- Modify: `cmd/wfctl/infra.go` (subcommand wiring)

**Change class:** New CLI command. Verification: unit tests + `wfctl infra audit-state-secrets --help` + representative invocation (CLI command class).

**Rollback:** Revert this commit; command is additive.

**Step 4.1: Write failing tests for `audit-state-secrets`**

Create `cmd/wfctl/infra_audit_state_secrets_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/sensitive"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

func TestAuditStateSecrets_NoFindings_ExitZero(t *testing.T) {
	store := &stubStore{
		saved: []interfaces.ResourceState{
			{Name: "ok", Outputs: map[string]any{"bucket": "b", "region": "nyc3"}},
		},
	}
	prov := newEnvProvider()
	w := &bytes.Buffer{}
	rc := runAuditStateSecrets(context.Background(), w, store, prov)
	if rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
	if !strings.Contains(w.String(), "no findings") {
		t.Errorf("expected 'no findings', got: %s", w.String())
	}
}

func TestAuditStateSecrets_OrphanInProvider(t *testing.T) {
	// Provider has a routed-secret "ghost_secret_key" but state has no ghost resource.
	store := &stubStore{
		saved: []interfaces.ResourceState{{Name: "live", Outputs: map[string]any{"bucket": "b"}}},
	}
	prov := newEnvProvider()
	prov.values["ghost_secret_key"] = "ORPHAN"
	w := &bytes.Buffer{}
	rc := runAuditStateSecrets(context.Background(), w, store, prov)
	if rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}
	if !strings.Contains(w.String(), "orphan") || !strings.Contains(w.String(), "ghost_secret_key") {
		t.Errorf("expected orphan finding for ghost_secret_key; got: %s", w.String())
	}
}

func TestAuditStateSecrets_LegacyPlaintext(t *testing.T) {
	store := &stubStore{
		saved: []interfaces.ResourceState{
			{Name: "legacy", Outputs: map[string]any{"secret_key": "PLAINTEXT-SECRET"}},
		},
	}
	prov := newEnvProvider()
	w := &bytes.Buffer{}
	rc := runAuditStateSecrets(context.Background(), w, store, prov)
	if rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}
	if !strings.Contains(w.String(), "legacy plaintext") || !strings.Contains(w.String(), "legacy") {
		t.Errorf("expected legacy plaintext finding; got: %s", w.String())
	}
}

func TestAuditStateSecrets_PlaceholderMissingValue(t *testing.T) {
	// State has a placeholder but provider doesn't have the secret.
	store := &stubStore{
		saved: []interfaces.ResourceState{
			{Name: "broken", Outputs: map[string]any{"secret_key": "secret_ref://broken_secret_key"}},
		},
	}
	prov := newEnvProvider()
	w := &bytes.Buffer{}
	rc := runAuditStateSecrets(context.Background(), w, store, prov)
	if rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}
	if !strings.Contains(w.String(), "missing routed value") || !strings.Contains(w.String(), "broken_secret_key") {
		t.Errorf("expected missing-routed-value finding; got: %s", w.String())
	}
}

func TestAuditStateSecrets_MistakenSecretConfigRefInState(t *testing.T) {
	// State contains a "secret://" string (user-config syntax leaked into state).
	store := &stubStore{
		saved: []interfaces.ResourceState{
			{Name: "weird", Outputs: map[string]any{"token": "secret://my_token"}},
		},
	}
	prov := newEnvProvider()
	w := &bytes.Buffer{}
	rc := runAuditStateSecrets(context.Background(), w, store, prov)
	if rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}
	if !strings.Contains(w.String(), "config-reference in state") {
		t.Errorf("expected config-reference-in-state finding; got: %s", w.String())
	}
}

func TestAuditStateSecrets_Prune_DeletesOrphans(t *testing.T) {
	store := &stubStore{
		saved: []interfaces.ResourceState{{Name: "live", Outputs: map[string]any{"bucket": "b"}}},
	}
	prov := newEnvProvider()
	prov.values["ghost_secret_key"] = "ORPHAN"
	w := &bytes.Buffer{}
	rc := runAuditStateSecretsWithPrune(context.Background(), w, store, prov, true)
	if rc != 0 { // 0 because pruning resolves the issue
		t.Errorf("rc = %d, want 0 after prune", rc)
	}
	if _, ok := prov.values["ghost_secret_key"]; ok {
		t.Errorf("orphan secret not pruned: %v", prov.values)
	}
}

func TestAuditStateSecrets_ListUnsupported_ReportsAdvisory(t *testing.T) {
	// GitHub-style provider returning ErrUnsupported for Get; we should still
	// audit state-side findings and emit a structured "list unsupported" advisory.
	store := &stubStore{
		saved: []interfaces.ResourceState{
			{Name: "ok", Outputs: map[string]any{"secret_key": "secret_ref://ok_secret_key"}},
		},
	}
	prov := &writeOnlyProvider{}
	w := &bytes.Buffer{}
	rc := runAuditStateSecrets(context.Background(), w, store, prov)
	// rc 1 because we can't verify the routed value; OR rc 0 with advisory only.
	// Design choice: rc 1 (cannot verify), but the advisory is informational.
	if rc == 2 {
		t.Errorf("rc=2 reserved for hard audit errors; should not fire on write-only providers")
	}
	if !strings.Contains(w.String(), "list unsupported") && !strings.Contains(w.String(), "Get unsupported") {
		t.Errorf("expected write-only provider advisory; got: %s", w.String())
	}
}

// writeOnlyProvider mimics GitHubSecretsProvider Get/List ErrUnsupported.
type writeOnlyProvider struct{ envProvider }

func newWriteOnlyProvider() *writeOnlyProvider { return &writeOnlyProvider{envProvider{values: map[string]string{}}} }
func (p *writeOnlyProvider) Get(_ context.Context, _ string) (string, error)  { return "", secrets.ErrUnsupported }
func (p *writeOnlyProvider) List(_ context.Context) ([]string, error) { return nil, secrets.ErrUnsupported }
```

**Step 4.2: Run, confirm fail**

Run: `cd cmd/wfctl && go test -run TestAuditStateSecrets -v`
Expected: FAIL — `runAuditStateSecrets` undefined.

**Step 4.3: Implement `cmd/wfctl/infra_audit_state_secrets.go`**

```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/iac/sensitive"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

// runInfraAuditStateSecrets is the CLI entry point for
// `wfctl infra audit-state-secrets`.
//
// Walks every entry in IaCStateStore. For each Outputs[k] that is:
//   - a "secret_ref://<name>" placeholder → confirm secrets.Provider has <name>.
//   - a plaintext value matching secrets.DefaultSensitiveKeys() → flag legacy.
//   - a "secret://<key>" string → flag mistaken config-reference in state.
// Then walks secrets.Provider.List() (when supported) for any
// "<resource>_<key>" name whose <resource> is NOT in IaCStateStore →
// orphan, candidate for prune.
//
// Exit codes:
//   0  no findings
//   1  findings (legacy plaintext, missing routed values, orphan secrets,
//      mistaken config-references)
//   2  audit error (cannot read state, etc.)
func runInfraAuditStateSecrets(args []string, w io.Writer) int {
	fs := flag.NewFlagSet("infra audit-state-secrets", flag.ContinueOnError)
	fs.SetOutput(w)
	var configFile string
	fs.StringVar(&configFile, "c", "infra.yaml", "Config file")
	fs.StringVar(&configFile, "config", "infra.yaml", "Config file")
	var prune bool
	fs.BoolVar(&prune, "prune", false, "Delete confirmed orphan secrets from secrets.Provider")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := parseSecretsConfig(configFile)
	if err != nil {
		fmt.Fprintf(w, "audit-state-secrets: parse %q: %v\n", configFile, err)
		return 2
	}
	if cfg == nil {
		fmt.Fprintln(w, "audit-state-secrets: no secrets config; nothing to audit")
		return 0
	}
	prov, err := resolveSecretsProvider(cfg)
	if err != nil {
		fmt.Fprintf(w, "audit-state-secrets: resolve provider: %v\n", err)
		return 2
	}

	store, err := openInfraStateStore(configFile)
	if err != nil {
		fmt.Fprintf(w, "audit-state-secrets: open state store: %v\n", err)
		return 2
	}
	defer store.Close()

	return runAuditStateSecretsWithPrune(context.Background(), w, store, prov, prune)
}

// runAuditStateSecrets is the testable entry point (no flag parsing).
func runAuditStateSecrets(ctx context.Context, w io.Writer, store interfaces.IaCStateStore, prov secrets.Provider) int {
	return runAuditStateSecretsWithPrune(ctx, w, store, prov, false)
}

func runAuditStateSecretsWithPrune(ctx context.Context, w io.Writer, store interfaces.IaCStateStore, prov secrets.Provider, prune bool) int {
	states, err := store.ListResources(ctx)
	if err != nil {
		fmt.Fprintf(w, "audit-state-secrets: list state: %v\n", err)
		return 2
	}

	findings := 0
	stateNames := map[string]struct{}{}
	for i := range states {
		stateNames[states[i].Name] = struct{}{}
	}

	defaultSensitive := map[string]struct{}{}
	for _, k := range secrets.DefaultSensitiveKeys() {
		defaultSensitive[k] = struct{}{}
	}

	// Walk state for placeholder/plaintext/config-ref findings.
	for i := range states {
		st := &states[i]
		// Stable iteration order for output.
		keys := make([]string, 0, len(st.Outputs))
		for k := range st.Outputs {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			v := st.Outputs[k]
			s, isStr := v.(string)
			if !isStr {
				continue
			}
			switch {
			case sensitive.IsPlaceholder(v):
				secretName := strings.TrimPrefix(s, sensitive.PlaceholderPrefix)
				_, getErr := prov.Get(ctx, secretName)
				if getErr == nil {
					// All good.
					continue
				}
				if errors.Is(getErr, secrets.ErrUnsupported) {
					fmt.Fprintf(w, "ADVISORY (list unsupported / Get unsupported): cannot verify routed value for %s/%s -> %q on this provider\n", st.Name, k, secretName)
					continue
				}
				if errors.Is(getErr, secrets.ErrNotFound) {
					fmt.Fprintf(w, "FINDING (missing routed value): %s/%s expects routed secret %q but provider does not have it\n", st.Name, k, secretName)
					findings++
				}
			case strings.HasPrefix(s, secrets.SecretPrefix):
				fmt.Fprintf(w, "FINDING (config-reference in state): %s/%s contains user-config-style %q (expected resolved value or %s placeholder)\n", st.Name, k, s, sensitive.PlaceholderPrefix)
				findings++
			default:
				if _, isSensName := defaultSensitive[k]; isSensName && s != "" {
					fmt.Fprintf(w, "FINDING (legacy plaintext): %s/%s = <plaintext>; rotate via wfctl infra bootstrap --force-rotate or re-apply\n", st.Name, k)
					findings++
				}
			}
		}
	}

	// Walk provider for orphan secrets.
	names, err := prov.List(ctx)
	switch {
	case err == nil:
		sort.Strings(names)
		for _, name := range names {
			// Names follow "<resource>_<output_key>". Strip the suffix that matches
			// any sensitive output key we know; if the prefix isn't a state name,
			// it's an orphan.
			res := stripKnownSuffix(name)
			if _, ok := stateNames[res]; ok {
				continue
			}
			if prune {
				if delErr := prov.Delete(ctx, name); delErr != nil {
					fmt.Fprintf(w, "PRUNE FAILED: %q: %v\n", name, delErr)
					findings++
				} else {
					fmt.Fprintf(w, "pruned orphan secret %q\n", name)
				}
				continue
			}
			fmt.Fprintf(w, "FINDING (orphan secret): %q has no matching state resource; rerun with --prune to delete\n", name)
			findings++
		}
	case errors.Is(err, secrets.ErrUnsupported):
		fmt.Fprintln(w, "ADVISORY (list unsupported): provider does not support List(); orphan-secret detection skipped on this host")
	default:
		fmt.Fprintf(w, "audit-state-secrets: list provider secrets: %v\n", err)
		return 2
	}

	if findings > 0 {
		fmt.Fprintf(w, "\naudit-state-secrets: %d finding(s)\n", findings)
		return 1
	}
	fmt.Fprintln(w, "audit-state-secrets: no findings")
	return 0
}

// stripKnownSuffix returns the resource-name prefix of a routed-secret name.
// Tries DefaultSensitiveKeys suffixes; falls back to the original name (which
// will then fail the state-name lookup and be flagged orphan).
func stripKnownSuffix(name string) string {
	for _, k := range secrets.DefaultSensitiveKeys() {
		suf := "_" + k
		if strings.HasSuffix(name, suf) {
			return name[:len(name)-len(suf)]
		}
	}
	return name
}
```

Wire the subcommand in `cmd/wfctl/infra.go` (around the existing `case "audit-secrets":` dispatch at line 88):

```go
case "audit-state-secrets":
	if rc := runInfraAuditStateSecrets(args[1:], os.Stdout); rc != 0 {
		return fmt.Errorf("audit-state-secrets exited with code %d", rc)
	}
	return nil
```

Update help text at line 116:

```
  audit-state-secrets  Audit state.Outputs vs. secrets.Provider for orphans, legacy, missing
```

**Step 4.5: Run tests, confirm pass**

Run: `cd cmd/wfctl && go test -run TestAuditStateSecrets -v`
Expected: All 7 TestAuditStateSecrets* tests PASS.

Run: `go test ./cmd/wfctl/...`
Expected: full suite PASS.

**Step 4.6: Manual command verification**

Run:
```
cd /Users/jon/workspace/workflow/_worktrees/engine-sensitive-output-routing && go build -o wfctl ./cmd/wfctl
./wfctl infra audit-state-secrets --help
```
Expected: usage text including `--config`, `--prune`. Exit 0.

```
./wfctl infra audit-state-secrets -c testdata/empty-secrets.yaml
```
(implementer creates `testdata/empty-secrets.yaml` if needed for smoke; expected output: "no findings"; exit 0)

**Step 4.7: Run race + vet + lint**

Run:
- `go test -race ./cmd/wfctl/... ./iac/sensitive/...` → PASS
- `go vet ./...` → no output
- `golangci-lint run ./cmd/wfctl/... ./iac/sensitive/...` → no findings

**Step 4.8: Commit**

```bash
git add cmd/wfctl/infra_audit_state_secrets.go cmd/wfctl/infra_audit_state_secrets_test.go \
        cmd/wfctl/infra.go
git commit -m "$(cat <<'EOF'
feat(wfctl): infra audit-state-secrets command

Adds wfctl infra audit-state-secrets command per design §4.7:
  - placeholder-without-routed-value findings
  - legacy plaintext-in-state findings
  - mistaken secret://... config-reference findings
  - orphan secret findings (provider has it; no matching state)
  - --prune to delete confirmed orphans

Distinct from existing audit-secrets which audits secrets.generate
config block for anti-patterns.

Rollback: revert this commit; command is additive.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Drift masking enumeration + wiring (or documented no-op)

**Files:**
- Modify: `cmd/wfctl/infra_apply_refresh.go` (if Diff call sites exist)
- Possibly modify: any other `cmd/wfctl/*.go` that calls `driver.Diff` against state-Outputs

**Change class:** Internal logic; conditional on enumeration result. Verification: unit tests OR documented enumeration that no in-tree call sites exist.

**Rollback:** Revert this commit (if any code change); enumeration is documentation-only and harmless to keep.

**Step 5.1: Enumerate every in-tree Diff call site that sees state-Outputs**

Run:
```sh
grep -rn "driver\.Diff(\|d\.Diff(\|\.Diff(ctx," --include="*.go" cmd/wfctl/ iac/
```

For each match, classify:
- (a) Receives state.Outputs (potentially with placeholders) as `current` → MUST mask before Diff.
- (b) Receives only fresh cloud Outputs (live Read result) → no masking needed (Read paths are sanitize-only post-Task 3 anyway).
- (c) Test files → skip; tests pass synthetic data.

**Step 5.2a: If (a)-class sites exist, wire `sensitive.MaskSensitiveForDiff`**

For each (a)-class site:

```go
import "github.com/GoCodeAlone/workflow/iac/sensitive"

// Replace:
result, err := driver.Diff(ctx, desiredSpec, currentOutput)
// With:
maskedDesired, maskedCurrent := sensitive.MaskSensitiveForDiff(driver.SensitiveKeys(), desiredSpec.Config, currentOutput.Outputs)
maskedSpec := desiredSpec
maskedSpec.Config = maskedDesired
maskedOut := *currentOutput
maskedOut.Outputs = maskedCurrent
result, err := driver.Diff(ctx, maskedSpec, &maskedOut)
```

Add a unit test per call site that verifies the masking is in effect (state has placeholder; Diff receives a map without the sensitive key).

**Step 5.2b: If NO (a)-class sites exist, document the no-op**

Add a comment to `cmd/wfctl/infra_apply_refresh.go` (top-of-file or above the refresh logic):

```go
// Note: as of v0.27.0, no in-tree call site dispatches driver.Diff against
// state.Outputs that may contain sensitive.PlaceholderPrefix entries.
// Per-provider Diff implementations receive desired/current via gRPC and
// are out of scope for engine-side masking. iac/sensitive.MaskSensitiveForDiff
// is exported for future in-tree consumers.
```

Add `iac/sensitive/route_test.go` test `TestMaskSensitiveForDiff_*` (already in Task 1) is the validation surface.

**Step 5.3: Run tests + lint**

Run:
- `go test ./cmd/wfctl/... ./iac/sensitive/...` → PASS
- `go vet ./...` → no output
- `golangci-lint run ./cmd/wfctl/...` → no findings

**Step 5.4: Commit**

```bash
# If 5.2a path:
git add cmd/wfctl/infra_apply_refresh.go cmd/wfctl/<other-modified>.go
# If 5.2b path:
git add cmd/wfctl/infra_apply_refresh.go

git commit -m "$(cat <<'EOF'
feat(wfctl): drift-masking wiring for sensitive state outputs

[For 5.2a: cite specific files] OR
[For 5.2b: documents that no in-tree Diff call site sees state.Outputs
with placeholders; iac/sensitive.MaskSensitiveForDiff stays exported
for future consumers.]

Rollback: revert this commit; masking is additive.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Documentation update + DOCUMENTATION.md / WFCTL.md / CHANGELOG

**Files:**
- Modify: `DOCUMENTATION.md`
- Modify: `docs/WFCTL.md`
- Modify: `CHANGELOG.md`

**Change class:** Documentation. Verification: spell-check + render preview + rollback validation (Step 6.4).

**Step 6.1: Add a section to `DOCUMENTATION.md` titled "Sensitive output routing"**

Append (or insert in an appropriate place near existing IaC sections):

```markdown
### Sensitive output routing (v0.27.0+)

When a `ResourceDriver.Create` or `Update` returns a `*ResourceOutput`
with `Sensitive: {key: true}`, the engine routes those output values
through the configured `secrets.Provider` instead of writing them
plaintext to state.

**Routed secret naming:** `<resource_name>_<output_key>` (e.g., a
resource named `coredump-deploy-key` with `secret_key` output is stored
under `coredump-deploy-key_secret_key`).

**State placeholder:** Routed fields appear in `state.Outputs` as
`secret_ref://<resource>_<key>` strings. Distinct from the user-supplied
`secret://<key>` config-reference convention.

**Required configuration:** A `secrets:` block in your workflow config
(see `secrets.* providers` below). For local/ad-hoc runs, the simplest
option is environment variables:

```yaml
secrets:
  provider: env
  config:
    prefix: WORKFLOW_
```

**Failure modes:**

- `secrets.Provider` not configured AND a driver emits sensitive outputs:
  apply hard-fails with a named-resource error pointing to this section.
- `secrets.Provider.Set` fails: apply errors; rerun is idempotent.
- `state.SaveResource` fails after `Set` succeeded: the engine compensates
  by calling `driver.Delete` + `provider.Delete`, then surfaces a
  combined error.

**Recovery:** `wfctl infra audit-state-secrets` audits state for orphan
secrets, missing routed values, and legacy plaintext fields. Run it
after a failed apply to triage:

```
wfctl infra audit-state-secrets --config infra.yaml [--prune]
```

**Read paths (refresh, adoption):** never call `provider.Set`; placeholders
are inherited from prior state to avoid cache pollution.

**Drift detection:** sensitive keys are masked from both desired and
current sides before `driver.Diff` to avoid false-positive drift on
keys that the cloud refuses to re-emit (e.g., DO Spaces `secret_key`).

**Cold-start consumers:** `secret_ref://` placeholders cannot be
rehydrated cross-process on write-only providers (GitHub Actions).
Same-process consumers (`infra_output:` generators in the same
`wfctl infra apply` invocation) get the routed value via in-memory
hand-off. Cross-process consumers reference the routed secret by name
via `secret://<resource>_<key>` directly.
```

**Step 6.2: Add `audit-state-secrets` to `docs/WFCTL.md`**

In the `wfctl infra` section, add after `audit-secrets`:

```markdown
#### infra audit-state-secrets

Audit `state.Outputs` against the configured `secrets.Provider` for orphan
secrets, missing routed values, legacy plaintext, and mistaken
config-references in state.

```
wfctl infra audit-state-secrets [--config infra.yaml] [--prune]
```

**Findings:**

- **orphan secret** — provider has `<resource>_<key>` but no state
  resource named `<resource>` exists.
- **missing routed value** — state has `secret_ref://...` placeholder
  but provider does not have the secret.
- **legacy plaintext** — state has plaintext value at a key matching
  `secrets.DefaultSensitiveKeys()` (e.g., `secret_key`, `password`).
- **config-reference in state** — state contains `secret://...` (user
  config syntax leaked into a persisted state field).

**Exit codes:** 0 = no findings; 1 = findings; 2 = audit error.

**`--prune`:** delete confirmed orphan secrets from the provider.
Idempotent; safe to rerun.

Distinct from `audit-secrets` which audits the `secrets.generate` config
block for anti-patterns. Run both as part of regular hygiene.
```

**Step 6.3: Add CHANGELOG entry**

In `CHANGELOG.md`, under the next-version section (v0.27.0):

```markdown
## v0.27.0 (unreleased)

### Added

- **Engine-side sensitive-output routing**: `ResourceDriver` outputs
  flagged with `Sensitive: {key: true}` on Create/Update are routed
  through the configured `secrets.Provider` and replaced in state with
  `secret_ref://...` placeholders. Plugins remain platform-agnostic.
  See `DOCUMENTATION.md#sensitive-output-routing` and design doc
  `docs/plans/2026-05-09-engine-sensitive-output-routing-design.md`.
- New `iac/sensitive` package with `Route`, `Revoke`, `IsPlaceholder`,
  and `MaskSensitiveForDiff` free functions.
- New `wfctl infra audit-state-secrets` command (with `--prune`) to
  audit state vs. secrets.Provider for orphans, legacy plaintext,
  missing routed values, and mistaken config-references.
- Drift masking (`MaskSensitiveForDiff`) prevents false-positive
  drift on sensitive keys where the cloud refuses to re-emit.

### Changed

- `applyWithProviderAndStore` and the in-process apply path now funnel
  state writes through `persistResourceWithSecretRouting`. On
  `SaveResource` failure after `provider.Set` succeeded, the engine
  invokes a compensating `driver.Delete` + `provider.Delete` to prevent
  orphan cloud resources.
- `adoptExistingResources` and `runInfraRefreshOutputs` now use
  sanitize-only persistence (no `provider.Set` from Read paths) to
  prevent cache pollution.
- `syncInfraOutputSecrets` accepts a `hydrated` map for same-process
  routed-secret hand-off.

### Migration

- Existing plugins continue to work unchanged; sensitive-output routing
  is opt-in via `ResourceOutput.Sensitive`.
- Operators with pre-existing state records containing plaintext
  secrets: run `wfctl infra audit-state-secrets` to enumerate; rotate
  via `wfctl infra bootstrap --force-rotate <name>`.
- Apply runs on plugins that newly emit `Sensitive` outputs require a
  `secrets:` configuration block (recommend `provider: env` with a
  prefix for local runs).

### Rollback

Pin `setup-wfctl@v0.26.x` and rebuild. State records written under
v0.27.0 contain `secret_ref://` placeholders that v0.26.x does not
understand; rotate affected secrets first or manually edit state to
inline the value from `secrets.Provider`.
```

**Step 6.4: Validate rollback claim**

The CHANGELOG / design §8 claims rollback path: "pin `setup-wfctl@v0.26.x` and rebuild; rotate affected secrets first." Validate the claim manually:

1. Build the v0.27.0 wfctl (current branch) and apply the env-provider fixture from Step 2.7.bis. Confirm state file contains `secret_ref://...` placeholder strings.
2. Inspect what a v0.26.x consumer would do with the placeholder. Build a v0.26.x wfctl from the prior tagged release:

```sh
cd /tmp && git clone --branch v0.26.x https://github.com/GoCodeAlone/workflow.git wfctl-026 || true
cd wfctl-026 && go build -o /tmp/wfctl-026 ./cmd/wfctl
```

(If `v0.26.x` tag doesn't yet exist — workflow's most recent release is the prior tag — substitute the most recent stable release tag and document the substitution.)

3. Run the v0.26.x wfctl against the v0.27.0-written state:

```sh
cd /tmp/wfctl-routing-smoke
/tmp/wfctl-026 infra outputs -c infra-with-env.yaml 2>&1 | tee rollback.log
```

Expected: outputs literally include `secret_ref://...` strings (not crashes; not hangs). Document the actual behaviour in CHANGELOG §Rollback. If a downstream consumer (e.g., infra_output secret generator) processes the placeholder as a literal value, document the manual recovery step (rotate via `wfctl infra bootstrap --force-rotate <name>` running v0.27.0 first).

If a v0.26.x build is not feasible (older Go module dependencies, tag missing), document this in CHANGELOG with a one-line note: "Rollback validation deferred — see PR <N> for explicit verification once a v0.26.x branch is available."

**Step 6.5: Spell-check + verify cross-references**

Run:
- `grep -n "secret_ref://" docs/ DOCUMENTATION.md CHANGELOG.md` → all references consistent.
- `grep -n "audit-state-secrets" docs/ DOCUMENTATION.md CHANGELOG.md cmd/wfctl/*.go` → wiring matches.

Expected: cross-references resolve; no broken anchors.

**Step 6.6: Commit**

```bash
git add DOCUMENTATION.md docs/WFCTL.md CHANGELOG.md
git commit -m "$(cat <<'EOF'
docs: sensitive-output routing + audit-state-secrets command

- DOCUMENTATION.md: new "Sensitive output routing" section explaining
  Sensitive flag, secret_ref:// placeholder format, failure modes,
  recovery, and read-path semantics.
- docs/WFCTL.md: new "infra audit-state-secrets" command reference.
- CHANGELOG.md: v0.27.0 entry covering Added / Changed / Migration /
  Rollback sections.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Final verification (before PR)

After Task 6, the implementer runs the **full local CI gate**:

```sh
cd /Users/jon/workspace/workflow/_worktrees/engine-sensitive-output-routing

# 1. Full test suite
go test ./...

# 2. Race detector
go test -race ./iac/sensitive/... ./cmd/wfctl/...

# 3. Vet
go vet ./...

# 4. Lint
golangci-lint run ./...

# 5. Build wfctl
go build -o wfctl ./cmd/wfctl
./wfctl infra audit-state-secrets --help

# 6. Verify no missed Outputs literal regressions (regression guard)
grep -rn "Outputs:.*r\.Outputs\|Outputs:.*live\.Outputs\|Outputs:.*imported\.Outputs" cmd/wfctl/*.go
# Expected output:
#   infra_state_store.go:318+341 — display layer, OK
#   infra.go:1101 — Import path, OUT OF SCOPE per Scope Manifest (operator runs
#                   audit-state-secrets post-import to triage; routing imported
#                   state requires a behavioural decision out of scope here)
# Anything else: must be triaged + either rewired through
# persistResourceWithSecretRouting or documented as out-of-scope.
```

All five steps must pass. If a regression appears, fix it before opening the PR.

---

## PR checklist

When opening the PR (`feat/engine-sensitive-output-routing` → `main`):

- [ ] Title: `feat(iac): engine-side sensitive-output routing through secrets.Provider`
- [ ] Body links to design doc + plan doc.
- [ ] All 5 commits present.
- [ ] CI green (build, test, race, vet, lint, strict-contracts).
- [ ] Copilot review requested.
- [ ] CHANGELOG.md updated under v0.27.0.
- [ ] Rollback note in PR body matches §8 of design doc.
