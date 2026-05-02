# wfctl infra drift detection + recovery Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement production-safe state-vs-cloud drift detection + recovery primitives in wfctl + workflow-plugin-digitalocean, validated by recovering core-dump's current state-drift (state thinks coredump-staging-vpc + coredump-staging-db exist but DO returns 404) so the staging deploy chain reaches /healthz.

**Architecture:** Two independent upstream PRs (workflow-plugin-digitalocean DetectDrift implementation + workflow CLI/struct extensions for ghost-prune) that ship as new releases, followed by a downstream lockfile bump in core-dump (and BMW for consistency). After both upstream fixes ship, run `wfctl infra apply --refresh` against core-dump staging to prune the 2 ghost entries, observe the next plan generates create actions for VPC + DB, and verify deploy reaches healthz. Plugin-side: each ResourceDriver's existing `Read` method drives drift detection via the canonical `interfaces.ErrResourceNotFound` sentinel for safe ghost classification. CLI-side: additive `Class` field on `interfaces.DriftResult` + `--refresh`/`--allow-protected-prune` flags on `wfctl infra apply` keep the change backwards-compatible.

**Tech Stack:** Go (workflow + workflow-plugin-digitalocean), workflow IaCProvider/ResourceDriver interfaces, wfctl CLI (cobra + flag), godo for DO API, GitHub Actions for deploy, DigitalOcean App Platform + managed Postgres + Spaces.

---

### Task 1: PR-D1 — workflow-plugin-digitalocean DOProvider.DetectDrift implementation

Repo: `GoCodeAlone/workflow-plugin-digitalocean`. Branch: `feat/detect-drift-impl` (fresh, off origin/main). Worktree: `/Users/jon/workspace/workflow-plugin-digitalocean/_worktrees/detect-drift-impl/`.

Reference: `workflow/docs/plans/2026-05-02-infra-drift-recovery-design.md` Section 1.

## Scope

Replace stub `DOProvider.DetectDrift` (`internal/provider.go:295-302`) with a real implementation that:

1. Iterates `resources` (refs from state store).
2. For each ref, resolves the driver via `p.ResourceDriver(ref.Type)`.
3. Calls `driver.Read(ctx, ref)` against cloud.
4. If Read returns an error that satisfies `errors.Is(err, interfaces.ErrResourceNotFound)`, classify as `DriftClassGhost` and return `Drifted: true`.
5. If Read returns any other error, propagate it (do NOT classify as drift — could be transient API failure).
6. If Read succeeds, call `driver.Diff(ctx, desired, actual)` with the spec from `ref` (or empty spec if not available — spec preservation is wfctl-side, plugin gets ref+actual). When `Diff.Drifted == true`, classify as `DriftClassConfig` with `Expected`, `Actual`, `Fields`. Otherwise `DriftClassInSync`.

**Critical sentinel-wrapping audit:** workflow-plugin-digitalocean has a local `ErrResourceNotFound` (`internal/drivers/app_platform.go:17`) AND `interfaces.ErrResourceNotFound` (`interfaces/iac_resource_driver.go:12`). The DetectDrift logic uses `errors.Is(err, interfaces.ErrResourceNotFound)`. **Audit every driver's Read path and ensure 404s wrap (or are aliased to) the canonical interfaces sentinel.** Where drivers use the local `ErrResourceNotFound`, change them to wrap with `interfaces.ErrResourceNotFound` so cross-package `errors.Is` works.

DriftClass enum values must be referenced via `interfaces.DriftClass{Unknown,InSync,Ghost,Config}` (defined in PR-D2). Since PR-D1 + PR-D2 are independent and develop in parallel, PR-D1 needs to either:
- Wait for PR-D2 to merge first (sequential), OR
- Define the enum constants locally in PR-D1 and have PR-D2 promote them to `interfaces`.

**Pick: PR-D2 lands first.** Workflow IaCProvider interface change (DriftClass) is the one consumers depend on. PR-D1 imports `interfaces.DriftClass*`. This linearizes the chain but each PR is still independently mergeable; we just sequence them.

NOTE: This means PR-D1 must wait for PR-D2's release. Update Files + Steps below accordingly.

**Files:**
- Modify: `internal/provider.go` lines 295-302 (DetectDrift body)
- Modify: every `internal/drivers/*.go` Read method that can return 404 — wrap with `interfaces.ErrResourceNotFound` (audit list: app_platform, database, vpc, firewall, certificate, registry, droplet, k8s_cluster, cache, load_balancer, dns, storage, api_gateway)
- Test: `internal/provider_detect_drift_test.go` (new — table-driven test exercising ghost + config + in-sync paths)
- Test: `internal/drivers/*_test.go` — add tests confirming 404 paths wrap with `interfaces.ErrResourceNotFound`
- Modify: `CHANGELOG.md` Unreleased entry citing core-dump#? (this work doesn't have an open issue yet — file as part of PR or as separate issue)

**Step 1: Set up worktree**

```bash
cd /Users/jon/workspace/workflow-plugin-digitalocean
git fetch origin main
git worktree add _worktrees/detect-drift-impl origin/main -b feat/detect-drift-impl
cd _worktrees/detect-drift-impl
```

Confirm `interfaces.DriftClass*` constants exist in the plugin's go.mod-resolved workflow version. If not (PR-D2 hasn't shipped yet), wait for PR-D2 release + bump go.mod here.

**Step 2: Audit every driver Read for sentinel wrapping**

```bash
# List Read methods + their not-found error returns
grep -nE 'func.*Read\(|ErrResourceNotFound|Code: 404' internal/drivers/*.go | grep -v _test
```

For each driver Read method:
1. Find the path that returns "not found" (404 from godo, or empty list result, etc.).
2. Verify it wraps with `interfaces.ErrResourceNotFound` (e.g., `fmt.Errorf("vpc %q: %w", name, interfaces.ErrResourceNotFound)`).
3. If it uses the local `drivers.ErrResourceNotFound` instead, change the wrap target to `interfaces.ErrResourceNotFound`. Keep the local sentinel as a backwards-compat alias that points to `interfaces.ErrResourceNotFound` (so existing internal callers continue to work but external `errors.Is` matches).

```go
// internal/drivers/app_platform.go
var ErrResourceNotFound = interfaces.ErrResourceNotFound  // alias for backwards compat
```

**Step 3: Write failing test for DetectDrift ghost path**

Create `internal/provider_detect_drift_test.go`:

```go
package internal

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fakeDriver implements interfaces.ResourceDriver with configurable Read behavior
// for DetectDrift tests. Set readErr to make Read return that error; otherwise
// readOutput is returned.
type fakeDriverForDrift struct {
	readErr    error
	readOutput *interfaces.ResourceOutput
	diffResult *interfaces.DiffResult
}

func (f *fakeDriverForDrift) Read(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.readOutput, nil
}
func (f *fakeDriverForDrift) Diff(ctx context.Context, desired interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	return f.diffResult, nil
}
// minimal stubs for other ResourceDriver methods
func (f *fakeDriverForDrift) Create(context.Context, interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) { return nil, nil }
func (f *fakeDriverForDrift) Update(context.Context, interfaces.ResourceRef, interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) { return nil, nil }
func (f *fakeDriverForDrift) Delete(context.Context, interfaces.ResourceRef) error { return nil }
func (f *fakeDriverForDrift) HealthCheck(context.Context, interfaces.ResourceRef) (*interfaces.HealthResult, error) { return nil, nil }
func (f *fakeDriverForDrift) Scale(context.Context, interfaces.ResourceRef, int) (*interfaces.ResourceOutput, error) { return nil, nil }
func (f *fakeDriverForDrift) SensitiveKeys() []string { return nil }

func TestDetectDrift_GhostInState(t *testing.T) {
	// Inject a fake driver that returns ErrResourceNotFound — simulating cloud 404.
	notFound := interfaces.ErrResourceNotFound
	p := &DOProvider{
		drivers: map[string]interfaces.ResourceDriver{
			"infra.vpc": &fakeDriverForDrift{readErr: notFound},
		},
	}
	refs := []interfaces.ResourceRef{
		{Name: "test-vpc", Type: "infra.vpc"},
	}
	results, err := p.DetectDrift(context.Background(), refs)
	if err != nil {
		t.Fatalf("DetectDrift: unexpected error %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if !r.Drifted {
		t.Errorf("expected Drifted=true for ghost-in-state")
	}
	if r.Class != interfaces.DriftClassGhost {
		t.Errorf("expected Class=ghost, got %q", r.Class)
	}
}

func TestDetectDrift_ConfigDrift(t *testing.T) {
	// Read succeeds; Diff reports drift
	p := &DOProvider{
		drivers: map[string]interfaces.ResourceDriver{
			"infra.vpc": &fakeDriverForDrift{
				readOutput: &interfaces.ResourceOutput{Name: "test-vpc", Type: "infra.vpc"},
				diffResult: &interfaces.DiffResult{
					Drifted:  true,
					Expected: map[string]any{"region": "nyc3"},
					Actual:   map[string]any{"region": "nyc1"},
					Fields:   []string{"region"},
				},
			},
		},
	}
	refs := []interfaces.ResourceRef{{Name: "test-vpc", Type: "infra.vpc"}}
	results, _ := p.DetectDrift(context.Background(), refs)
	if results[0].Class != interfaces.DriftClassConfig {
		t.Errorf("expected Class=config, got %q", results[0].Class)
	}
	if results[0].Fields[0] != "region" {
		t.Errorf("expected drift field=region, got %v", results[0].Fields)
	}
}

func TestDetectDrift_InSync(t *testing.T) {
	// Read succeeds; Diff reports no drift
	p := &DOProvider{
		drivers: map[string]interfaces.ResourceDriver{
			"infra.vpc": &fakeDriverForDrift{
				readOutput: &interfaces.ResourceOutput{Name: "test-vpc", Type: "infra.vpc"},
				diffResult: &interfaces.DiffResult{Drifted: false},
			},
		},
	}
	refs := []interfaces.ResourceRef{{Name: "test-vpc", Type: "infra.vpc"}}
	results, _ := p.DetectDrift(context.Background(), refs)
	if results[0].Drifted {
		t.Errorf("expected Drifted=false for in-sync")
	}
	if results[0].Class != interfaces.DriftClassInSync {
		t.Errorf("expected Class=in-sync, got %q", results[0].Class)
	}
}

func TestDetectDrift_TransientErrorPropagates(t *testing.T) {
	// Non-404 error must propagate; do NOT classify as drift
	transient := errors.New("DO API rate limit exceeded")
	p := &DOProvider{
		drivers: map[string]interfaces.ResourceDriver{
			"infra.vpc": &fakeDriverForDrift{readErr: transient},
		},
	}
	refs := []interfaces.ResourceRef{{Name: "test-vpc", Type: "infra.vpc"}}
	_, err := p.DetectDrift(context.Background(), refs)
	if err == nil {
		t.Fatal("expected transient error to propagate")
	}
	if !errors.Is(err, transient) {
		t.Errorf("expected wrapped transient error, got %v", err)
	}
}
```

**Step 3: Run tests — confirm 4 fail (DetectDrift currently a stub)**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./internal/ -run TestDetectDrift -v
```

Expected: all 4 cases FAIL because the stub returns `Drifted: false` for everything.

**Step 4: Implement DetectDrift**

Replace `internal/provider.go:295-302`:

```go
// DetectDrift checks for drift between declared state and actual cloud state.
// For each resource ref, it calls the driver's Read; classifies the result:
//   - errors.Is(ErrResourceNotFound) → DriftClassGhost (Drifted=true; cloud says 404)
//   - other errors → propagate (transient API failure, do NOT classify)
//   - Read succeeds + Diff.Drifted=true → DriftClassConfig
//   - Read succeeds + Diff.Drifted=false → DriftClassInSync
//
// Callers (wfctl infra drift, wfctl infra apply --refresh) use the Class to
// decide actions: ghost → state-prune, config → reconcile, in-sync → no-op.
func (p *DOProvider) DetectDrift(ctx context.Context, resources []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	results := make([]interfaces.DriftResult, 0, len(resources))
	for _, ref := range resources {
		d, err := p.ResourceDriver(ref.Type)
		if err != nil {
			results = append(results, interfaces.DriftResult{
				Name:    ref.Name,
				Type:    ref.Type,
				Class:   interfaces.DriftClassUnknown,
				Drifted: true, // unknown driver — surface as drifted so operator investigates
				Fields:  []string{"driver-resolution: " + err.Error()},
			})
			continue
		}
		out, err := d.Read(ctx, ref)
		if err != nil {
			if errors.Is(err, interfaces.ErrResourceNotFound) {
				// Ghost in state — cloud says 404 / not found
				results = append(results, interfaces.DriftResult{
					Name:    ref.Name,
					Type:    ref.Type,
					Drifted: true,
					Class:   interfaces.DriftClassGhost,
				})
				continue
			}
			// Transient or unknown error — propagate; let caller retry
			return results, fmt.Errorf("detect drift for %s/%s: %w", ref.Type, ref.Name, err)
		}
		// Read succeeded — check config drift via the driver's Diff method.
		// Note: spec is empty (we don't have desired state at this layer; caller
		// supplied only refs). Drivers' Diff implementations gracefully handle
		// empty desired by reporting "in-sync" (current == nil-spec is a no-diff).
		// For richer drift detection, callers should pass refs + desired spec via
		// a future ApplyResult-style two-input call; out of scope for v1.
		var diff *interfaces.DiffResult
		if out != nil {
			diff, _ = d.Diff(ctx, interfaces.ResourceSpec{Name: ref.Name, Type: ref.Type}, out)
		}
		if diff != nil && diff.Drifted {
			results = append(results, interfaces.DriftResult{
				Name:     ref.Name,
				Type:     ref.Type,
				Drifted:  true,
				Class:    interfaces.DriftClassConfig,
				Expected: diff.Expected,
				Actual:   diff.Actual,
				Fields:   diff.Fields,
			})
		} else {
			results = append(results, interfaces.DriftResult{
				Name:    ref.Name,
				Type:    ref.Type,
				Drifted: false,
				Class:   interfaces.DriftClassInSync,
			})
		}
	}
	return results, nil
}
```

Add necessary imports: `errors`, `fmt`, `github.com/GoCodeAlone/workflow/interfaces` (already imported).

**Step 5: Run tests — confirm pass**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./internal/ -run TestDetectDrift -v
```

Expected: 4 cases PASS.

**Step 6: Audit driver Reads + add wrap-with-canonical-sentinel tests**

For each driver where Step 2 found a non-canonical not-found path:

Add a unit test that simulates the cloud-side 404 + asserts the returned error satisfies `errors.Is(err, interfaces.ErrResourceNotFound)`. Example for VPC:

```go
func TestVPCDriverRead_404WrapsCanonicalSentinel(t *testing.T) {
	mock := &mockVPCClient{getErr: do404("vpc not found")}
	d := NewVPCDriverWithClient(mock, "nyc3")
	_, err := d.Read(context.Background(), interfaces.ResourceRef{Name: "missing", ProviderID: "abc"})
	if !errors.Is(err, interfaces.ErrResourceNotFound) {
		t.Errorf("expected ErrResourceNotFound, got %v", err)
	}
}
```

Repeat for each driver. Use existing test patterns in each driver's `_test.go` file as a template.

**Step 7: Run full test suite — no regressions**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./...
```

Expected: PASS across all packages. Existing tests unchanged; new tests added.

**Step 8: Update CHANGELOG.md**

Prepend under Unreleased:

```markdown
## [Unreleased]

### Added

- `DOProvider.DetectDrift` now produces real drift classifications instead of always returning `Drifted: false`. Three classes:
  - `DriftClassGhost` (`Drifted: true`): state has the resource, but cloud Read returns `interfaces.ErrResourceNotFound`. Caller should prune state via `wfctl infra apply --refresh`.
  - `DriftClassConfig` (`Drifted: true`): state and cloud both have the resource but configs differ (per the driver's `Diff` method). Caller should reconcile via plan/apply.
  - `DriftClassInSync` (`Drifted: false`): state and cloud agree.
- All driver `Read` methods now wrap not-found errors with `interfaces.ErrResourceNotFound` (canonical workflow sentinel), enabling cross-package `errors.Is` checks. Local `drivers.ErrResourceNotFound` retained as a backwards-compat alias.

### Fixed

- core-dump#? (state drift recovery): unblocks `wfctl infra apply --refresh` from prune-orphaned-state-entries on first deploy attempts that partially succeed.
```

**Step 9: Commit + push + open PR**

```bash
git add internal/provider.go internal/provider_detect_drift_test.go internal/drivers/*.go internal/drivers/*_test.go CHANGELOG.md
git commit -m "$(cat <<'EOF'
feat(provider): implement DOProvider.DetectDrift with ghost + config classes

Replace stub DetectDrift (returning Drifted: false for everything) with a
real implementation that drives drift detection from each resource
driver's Read method. Three classes:

- DriftClassGhost: cloud Read returns interfaces.ErrResourceNotFound →
  state has the resource, but DO doesn't. Caller can safely prune state
  via wfctl infra apply --refresh.
- DriftClassConfig: Read succeeds + Diff.Drifted=true → state and cloud
  both have the resource but configs differ. Caller reconciles via plan.
- DriftClassInSync: state and cloud agree.

Transient errors (rate limit, auth, network) propagate; do NOT classify
as drift. The errors.Is(err, interfaces.ErrResourceNotFound) sentinel
gates the ghost path — only genuine 404s trigger state-prune semantics.

Audited all driver Reads. Where drivers used local drivers.ErrResourceNotFound,
re-aliased to interfaces.ErrResourceNotFound so cross-package errors.Is
matches. Added unit tests confirming the wrap.

4 unit tests for DetectDrift cover ghost, config-drift, in-sync, and
transient-error paths.

Closes the diagnostic blank that hit core-dump's first deploy attempt
on 2026-05-02 (state thinks coredump-staging-vpc + coredump-staging-db
exist; doctl databases connection returns 404; previous DetectDrift
stub couldn't surface the discrepancy).

See workflow/docs/plans/2026-05-02-infra-drift-recovery-design.md.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
git push -u origin feat/detect-drift-impl
gh pr create --title "feat(provider): implement DOProvider.DetectDrift with ghost + config classes" \
  --reviewer "Copilot" \
  --body "$(cat <<'EOF'
## Summary

Implements `DOProvider.DetectDrift` (previously a stub) to surface real drift between IaC state and DigitalOcean cloud state. Three classes:

- **Ghost** (state has it, cloud says 404)
- **Config drift** (both exist, configs differ)
- **In-sync** (state and cloud agree)

Transient errors (rate limit, auth, network) propagate without classification — only genuine `interfaces.ErrResourceNotFound` triggers ghost classification, gating the state-prune semantics in `wfctl infra apply --refresh`.

Validated against the core-dump 2026-05-02 state-drift scenario: 2 ghost entries (VPC + DB) will surface clearly once this + PR-D2 ship and lockfiles bump.

## Files

- `internal/provider.go` — DetectDrift implementation
- `internal/drivers/*.go` — audit + wrap not-found errors with canonical `interfaces.ErrResourceNotFound`
- `internal/provider_detect_drift_test.go` — 4 unit tests (ghost / config / in-sync / transient)
- `internal/drivers/*_test.go` — wrap-confirmation tests per driver
- `CHANGELOG.md` — Unreleased entry

## Sequencing

This PR depends on `interfaces.DriftClass*` constants which are defined in workflow PR-D2. PR-D2 ships first; this PR rebases its `go.mod` onto that release.

## Test plan

- [x] 4 unit tests for DetectDrift (ghost, config drift, in-sync, transient)
- [x] Per-driver tests confirming `errors.Is(err, interfaces.ErrResourceNotFound)` matches on Read 404
- [x] Existing test suite green
- [ ] After merge + lockfile bump in core-dump: `wfctl infra drift -c infra.yaml --env staging` surfaces 2 ghosts (VPC + DB)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

If `--reviewer "Copilot"` errors: retry via API per workspace memory. DM team-lead with branch + commit SHA + PR URL + summary.

**Verification class:** plugin / extension change (per matrix). Class-appropriate evidence: unit tests green + wrap-confirmation tests per driver + post-merge integration test (DetectDrift surfaces real ghost on core-dump staging).

---

### Task 2: PR-D2 — workflow DriftClass enum + apply --refresh flag + driftInfraModules CLI extension

Repo: `GoCodeAlone/workflow`. Branch: `feat/apply-refresh-flag` (fresh, off origin/main; do NOT layer on `design/infra-drift-recovery`). Worktree: `/Users/jon/workspace/workflow/_worktrees/apply-refresh-flag/`.

Reference: design Section 2 + 3 + 4 + 5.

**Files:**
- Modify: `interfaces/iac_provider.go` — add `DriftClass` type + 4 constants + `Class` field on `DriftResult` (additive, omitempty)
- Modify: `cmd/wfctl/infra_apply.go` — add `--refresh` + `--allow-protected-prune` flags; implement refresh-then-apply logic
- Modify: `cmd/wfctl/infra_status_drift.go` — extend `driftInfraModules` to print Class in output
- Test: `interfaces/iac_provider_test.go` — DriftClass JSON marshaling + omitempty
- Test: `cmd/wfctl/infra_apply_refresh_test.go` (new) — refresh flag behavior with stub provider returning ghost results
- Test: `cmd/wfctl/infra_status_drift_test.go` (extend if exists, else new) — class output formatting
- Modify: `CHANGELOG.md` Unreleased entry
- Optional: `docs/wfctl/drift-recovery.md` (new) — operator procedure for production drift recovery

**Step 1: Set up worktree**

```bash
cd /Users/jon/workspace/workflow
git fetch origin main
git worktree add _worktrees/apply-refresh-flag origin/main -b feat/apply-refresh-flag
cd _worktrees/apply-refresh-flag
```

**Step 2: Write failing test for DriftClass enum + JSON marshaling**

Edit `interfaces/iac_provider_test.go` (create if absent — search `interfaces/*_test.go` for existing patterns first):

```go
package interfaces_test

import (
	"encoding/json"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestDriftClass_Constants(t *testing.T) {
	cases := []struct {
		name     string
		c        interfaces.DriftClass
		expected string
	}{
		{"unknown", interfaces.DriftClassUnknown, ""},
		{"in-sync", interfaces.DriftClassInSync, "in-sync"},
		{"ghost", interfaces.DriftClassGhost, "ghost"},
		{"config", interfaces.DriftClassConfig, "config"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.c) != tc.expected {
				t.Errorf("got %q, want %q", string(tc.c), tc.expected)
			}
		})
	}
}

func TestDriftResult_ClassOmitEmpty(t *testing.T) {
	// Class="" (DriftClassUnknown) should be omitted from JSON for backwards compat
	r := interfaces.DriftResult{Name: "vpc", Type: "infra.vpc", Drifted: false}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); contains(got, `"class"`) {
		t.Errorf("expected no class field with empty Class, got %s", got)
	}
}

func TestDriftResult_ClassPresent(t *testing.T) {
	r := interfaces.DriftResult{Name: "vpc", Type: "infra.vpc", Drifted: true, Class: interfaces.DriftClassGhost}
	b, _ := json.Marshal(r)
	if !contains(string(b), `"class":"ghost"`) {
		t.Errorf("expected class:ghost in JSON, got %s", b)
	}
}

func contains(s, substr string) bool { return len(s) >= len(substr) && (s == substr || (len(s) > 0 && (s[:len(substr)] == substr || contains(s[1:], substr)))) }
```

**Step 3: Run tests — confirm fail (DriftClass undefined)**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./interfaces/ -run TestDriftClass -v
GOFLAGS=-mod=mod GOWORK=off go test ./interfaces/ -run TestDriftResult -v
```

Expected: compile error (undefined DriftClass).

**Step 4: Add DriftClass type + constants + Class field**

Edit `interfaces/iac_provider.go` (append to existing section near line 122):

```go
// DriftClass classifies the type of drift detected between IaC state and
// actual cloud state. Used by wfctl infra drift output and wfctl infra
// apply --refresh recovery semantics.
type DriftClass string

const (
	// DriftClassUnknown is the zero value; preserved for backwards compat
	// with consumers serialized before the Class field existed.
	DriftClassUnknown DriftClass = ""
	// DriftClassInSync — state and cloud agree.
	DriftClassInSync DriftClass = "in-sync"
	// DriftClassGhost — state has the resource; cloud Read returned
	// ErrResourceNotFound. Caller can prune via wfctl infra apply --refresh.
	DriftClassGhost DriftClass = "ghost"
	// DriftClassConfig — state and cloud both have the resource but configs
	// differ. Caller reconciles via wfctl infra apply (normal plan path).
	DriftClassConfig DriftClass = "config"
)

// DriftResult captures detected drift between declared and actual state.
type DriftResult struct {
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Drifted  bool           `json:"drifted"`
	Class    DriftClass     `json:"class,omitempty"` // additive; omitted when Unknown
	Expected map[string]any `json:"expected,omitempty"`
	Actual   map[string]any `json:"actual,omitempty"`
	Fields   []string       `json:"fields,omitempty"`
}
```

(Modify the existing DriftResult struct to add the Class field with omitempty; do NOT remove or rename existing fields.)

**Step 5: Run tests — confirm pass**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./interfaces/ -run TestDriftClass -v
GOFLAGS=-mod=mod GOWORK=off go test ./interfaces/ -run TestDriftResult -v
```

Expected: PASS.

**Step 6: Write failing test for `wfctl infra apply --refresh`**

Create `cmd/wfctl/infra_apply_refresh_test.go`:

```go
package main

import (
	"context"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fakeProviderForRefresh stubs provider.DetectDrift to return ghost results,
// letting tests verify refresh-mode prunes them from state.
type fakeProviderForRefresh struct {
	driftResults []interfaces.DriftResult
	planActions  []interfaces.PlanAction
	deletedRefs  []interfaces.ResourceRef // captured by test
}

func (f *fakeProviderForRefresh) DetectDrift(ctx context.Context, refs []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return f.driftResults, nil
}
// ... other IaCProvider methods stubbed

func TestApplyRefresh_DryRunPrintsPrunesWithoutMutating(t *testing.T) {
	// Given state with 2 entries that DetectDrift reports as ghosts
	// When wfctl infra apply --refresh runs (dry-run, no --auto-approve)
	// Then output prints "would prune ..." for each ghost
	// And state-store DeleteResource is NOT called
	// And subsequent plan still includes those resources as create actions
	// (omitted full implementation — outline; specific assertions per test)
}

func TestApplyRefresh_AutoApprovePrunesAndApplies(t *testing.T) {
	// Given same setup
	// When wfctl infra apply --refresh --auto-approve runs
	// Then state-store DeleteResource IS called for each ghost
	// And the resulting plan includes create actions for the pruned resources
	// And apply executes those creates
}

func TestApplyRefresh_ProtectedResourceBlockedWithoutFlag(t *testing.T) {
	// Given a ghost result on a protected: true resource
	// When wfctl infra apply --refresh --auto-approve runs WITHOUT --allow-protected-prune
	// Then BLOCKED message is printed
	// And state-store DeleteResource is NOT called for protected
	// And exit code is non-zero
}

func TestApplyRefresh_ProtectedResourcePrunedWithFlag(t *testing.T) {
	// Given same setup
	// When invoked WITH --allow-protected-prune
	// Then DeleteResource IS called
	// And audit log line is emitted to stderr
}

func TestApplyRefresh_TransientErrorDoesNotPrune(t *testing.T) {
	// Given DetectDrift returns an error (transient)
	// When apply --refresh runs
	// Then no prune happens
	// And error is propagated
}
```

(Provide actual implementations following the patterns in existing `cmd/wfctl/infra_apply_*_test.go`.)

**Step 7: Run tests — confirm fail**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./cmd/wfctl/ -run TestApplyRefresh -v
```

Expected: compile errors (`--refresh` flag undefined in `runInfraApply`).

**Step 8: Implement --refresh + --allow-protected-prune flags in `cmd/wfctl/infra_apply.go`**

Find `runInfraApply` (around line 70+ in infra_apply.go). Add to the flag set:

```go
var refreshFlag, allowProtectedPruneFlag bool
fs.BoolVar(&refreshFlag, "refresh", false, "Detect drift and prune ghost-in-state entries before applying")
fs.BoolVar(&allowProtectedPruneFlag, "allow-protected-prune", false, "Allow pruning state entries for resources marked protected: true (requires --refresh)")
```

After `fs.Parse(args)` and config resolution, before the existing apply logic, add:

```go
if refreshFlag {
	if err := runInfraApplyRefreshPhase(ctx, cfgFile, envName, autoApprove, allowProtectedPruneFlag); err != nil {
		return fmt.Errorf("refresh phase: %w", err)
	}
}
// ... existing plan + apply logic continues
```

Implement `runInfraApplyRefreshPhase` in the same file (or new `cmd/wfctl/infra_apply_refresh.go`):

```go
func runInfraApplyRefreshPhase(ctx context.Context, cfgFile, envName string, autoApprove, allowProtectedPrune bool) error {
	store, err := resolveStateStore(cfgFile, envName)
	if err != nil {
		return err
	}
	states, err := store.ListResources(ctx, "" /* contextPath */)
	if err != nil {
		return fmt.Errorf("list state: %w", err)
	}
	if len(states) == 0 {
		fmt.Println("Refresh: no state to check.")
		return nil
	}

	groups, groupOrder := groupStatesByProvider(states, cfgFile, envName)
	for _, moduleRef := range groupOrder {
		g := groups[moduleRef]
		provider, closer, err := resolveIaCProvider(ctx, g.provType, g.provCfg)
		if err != nil {
			return fmt.Errorf("load provider %q: %w", moduleRef, err)
		}
		results, err := provider.DetectDrift(ctx, g.refs)
		if closer != nil {
			closer.Close()
		}
		if err != nil {
			return fmt.Errorf("detect drift for provider %q: %w", moduleRef, err)
		}
		for _, r := range results {
			if r.Class != interfaces.DriftClassGhost {
				continue
			}
			isProtected := isResourceProtected(states, r.Name)
			if isProtected && !allowProtectedPrune {
				fmt.Fprintf(os.Stderr, "::error::BLOCKED: %s is protected; cannot prune without --allow-protected-prune\n", r.Name)
				return fmt.Errorf("refresh: blocked on protected resource %q (use --allow-protected-prune to override)", r.Name)
			}
			if !autoApprove {
				fmt.Printf("Refresh: would prune ghost %s (%s) — cloud reports not found.\n", r.Name, r.Type)
				continue
			}
			fmt.Fprintf(os.Stderr, "wfctl: state mutation prune %s (type=%s) reason=ghost-in-state at %s\n", r.Name, r.Type, time.Now().Format(time.RFC3339))
			if err := store.DeleteResource(ctx, "" /* contextPath */, r.Name); err != nil {
				return fmt.Errorf("prune %s: %w", r.Name, err)
			}
			fmt.Printf("Refresh: pruned %s (%s)\n", r.Name, r.Type)
		}
	}
	return nil
}

// isResourceProtected returns true if the named resource has protected: true
// in its applied-state config.
func isResourceProtected(states []*interfaces.ResourceOutput, name string) bool {
	for _, s := range states {
		if s.Name == name {
			if p, ok := s.Outputs["protected"].(bool); ok && p {
				return true
			}
		}
	}
	return false
}
```

(Verify exact `StateStore.ListResources` signature — Step 0 survey showed `ListResources(ctx, contextPath)`. If contextPath needs a real value, derive it from cfg.)

**Step 9: Run tests — confirm pass**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./cmd/wfctl/ -run TestApplyRefresh -v
```

Expected: PASS for all 5 cases.

**Step 10: Extend `driftInfraModules` to print Class**

Edit `cmd/wfctl/infra_status_drift.go` around line 105-125. Replace the plain `DRIFT  %s (%s)\n` print with class-aware:

```go
for _, d := range results {
	switch d.Class {
	case interfaces.DriftClassGhost:
		fmt.Printf("  GHOST    %s (%s) — cloud reports not found\n", d.Name, d.Type)
		found = true
	case interfaces.DriftClassConfig:
		fmt.Printf("  CONFIG   %s (%s)\n", d.Name, d.Type)
		for k, v := range d.Expected {
			actual := d.Actual[k]
			if fmt.Sprintf("%v", v) != fmt.Sprintf("%v", actual) {
				fmt.Printf("    %s: expected=%v  actual=%v\n", k, v, actual)
			}
		}
		found = true
	case interfaces.DriftClassInSync:
		fmt.Printf("  IN-SYNC  %s (%s)\n", d.Name, d.Type)
	default:
		// DriftClassUnknown — fall through to legacy Drifted-bool behavior
		if d.Drifted {
			fmt.Printf("  DRIFT    %s (%s)\n", d.Name, d.Type)
			found = true
		} else {
			fmt.Printf("  OK       %s (%s)\n", d.Name, d.Type)
		}
	}
}
```

**Step 11: Run full test suite — no regressions**

```bash
GOFLAGS=-mod=mod GOWORK=off go test ./...
```

Expected: PASS across all packages. Existing consumers of DriftResult unaffected (Class is omitempty).

**Step 12: Add operator docs**

Create `docs/wfctl/drift-recovery.md` (~100 lines) covering:
- What is drift; the 3 classes
- When to run `wfctl infra drift`
- How to recover with `wfctl infra apply --refresh` (dry-run first, then --auto-approve)
- Protected-resource handling with `--allow-protected-prune` (two-key contract)
- Audit-log location and format
- Production safety checklist

**Step 13: Update CHANGELOG.md**

Prepend under Unreleased:

```markdown
## [Unreleased]

### Added

- `interfaces.DriftClass` enum with constants `DriftClassUnknown`, `DriftClassInSync`, `DriftClassGhost`, `DriftClassConfig`. Additive Class field on `DriftResult` with `json:"class,omitempty"` for backwards compat.
- `wfctl infra apply --refresh` flag: detect drift first, prune ghost-in-state entries before applying. Default is dry-run; pass `--auto-approve` to execute.
- `wfctl infra apply --allow-protected-prune` flag: required to prune state entries for resources marked `protected: true`. Two-key contract for production safety.
- `wfctl infra drift` CLI output now prints drift class (GHOST / CONFIG / IN-SYNC) to make recovery actions explicit.
- `docs/wfctl/drift-recovery.md` operator procedure for production drift recovery.

### Changed

- `cmd/wfctl/infra_status_drift.go` `driftInfraModules` consumes the new DriftClass field; backwards-compatible behavior for plugins still returning `DriftClassUnknown`.
```

**Step 14: Commit + push + open PR**

(Use the standard commit message + PR body pattern from earlier tasks. PR title: `feat(wfctl): drift class enum + apply --refresh ghost-prune`. Reviewer: Copilot via API. DM spec-reviewer when push complete.)

**Verification class:** CLI command + interface change (per matrix). Unit tests + integration test against a fake provider returning ghost results.

---

### Task 3: PR-D3 — bump core-dump + BMW lockfile to new workflow-plugin-digitalocean release + bump wfctl pin to new workflow release

PREREQUISITE: PR-D1 + PR-D2 both merged + workflow release tag (likely v0.20.5) + workflow-plugin-digitalocean release tag (likely v0.8.2) both exist.

Two parallel sub-PRs (one per repo):

## PR-D3a: core-dump

Repo: `GoCodeAlone/core-dump`. Branch: `chore/bump-for-drift-recovery`. Worktree: `/Users/jon/workspace/core-dump/_worktrees/bump-for-drift-recovery/`.

**Files:**
- `.wfctl-lock.yaml` — bump `workflow-plugin-digitalocean.version` to new tag
- `.github/workflows/{deploy,bootstrap,teardown,registry-retention}.yml` — bump wfctl version to new tag
- `infra.yaml` — bump engine compat header

Commit message references PR-D1 + PR-D2. Per workspace memory `feedback_version_bump_immediate_merge`: DM team-lead direct, admin-merge once CI green.

## PR-D3b: BMW

Repo: `GoCodeAlone/buymywishlist`. Branch: `chore/bump-for-drift-recovery`. Same structure as PR-D3a.

## Sequencing

PR-D3a + PR-D3b are independent and can run in parallel. Either order. After both merge, post-merge Deploy auto-fires on each main commit.

## Verification (post-merge — drives the actual recovery)

```bash
# core-dump staging recovery
cd /Users/jon/workspace/core-dump
gh workflow run deploy.yml --ref main  # OR wait for auto-fire

# Once Deploy is in flight, verify drift detection from CI (deploy.yml probably won't have apply --refresh; need a separate one-shot)

# OR: trigger manually via doctl/wfctl on a runner
# Add a temporary one-shot recovery workflow OR run locally:
gh workflow run drift-recovery.yml --ref main -f environment=staging  # if such workflow is added in PR-D3a
```

Actually, PR-D3a should include a small new workflow `drift-recovery.yml` that's `workflow_dispatch`-only and runs `wfctl infra apply --refresh --auto-approve` against staging. That's the operator's hands-on recovery primitive. Add to PR-D3a's scope.

After running drift-recovery.yml for core-dump staging:
- Expected output: pruned 2 ghosts (VPC + DB)
- Next CI completion → Deploy auto-fires → plan now generates create actions for VPC + DB → apply succeeds → /healthz green.

If new failures surface after VPC + DB recreate, file as Task 5+ iteration.

**Verification class:** version pin update (per matrix) + runtime-launch validation (deploy reaches /healthz).

---

### Task 4: Iterate — fix what surfaces during refresh recovery + first successful staging deploy

Open-ended. Predictable Task 5+ candidates:
- VPC + DB re-creation may hit DO API quirks (e.g., DB cluster name reuse delays after a soft-delete).
- App Platform deploy on second-pass may have a slow startup; healthz timing may need adjustment.
- NATS service-discovery DNS may not resolve if app + nats start in wrong order.
- trusted_sources may need to be re-enabled once the DB is created (see Task #16 follow-up).

Each new failure → triage → fix upstream where possible → version bump → retry. Stop when staging /healthz responds 200.

---

## Cross-task notes

**Branch hygiene:** PR-D1 + PR-D2 + PR-D3a + PR-D3b each on fresh branches. The `design/infra-drift-recovery` branch holds the design + plan only; do NOT layer implementation commits on it.

**Review discipline:**
- IAC_PLUGIN_REVIEW_CHECKLIST.md 8-bug-class scan + structpb-boundary scan on PR-D1 + PR-D2.
- Adversarial framing per workspace memory v5.2.0.
- Ghost-flag verification per `feedback_copilot_ghost_flags_verify_file_content`.
- Admin-merge under override per `feedback_admin_override_pr_merge` once Copilot resolved + non-Lint CI green.
- Active monitoring per `feedback_active_pr_monitoring`.

**Lint drift (workflow#516):** PR-D2 will hit pre-existing govet inline failures. Admin-merge with override.

**Release cutting after each upstream merges:**
- workflow-plugin-digitalocean → v0.8.2 (after PR-D1)
- workflow → v0.20.5 (after PR-D2)
- Use API-side tag creation to avoid v0.20.2-style ghost-tag mistakes.

**Auto-chaining:** per `feedback_continuous_autonomous_phases`. After each PR merges + post-merge runtime validation, immediately spawn the next dependent task.

**No doctl/openssl/gh-secret fallback.** Full wfctl dogfooding.

**Production safety reminder for PR-D2:** dry-run by default; `--allow-protected-prune` two-key contract; audit-log every state mutation.

## System Impact

(Carried forward from design doc.)

- **State store:** PR-D2 introduces state-mutation paths (prune) gated behind explicit flags. State-store contract unchanged; only callers added.
- **Plugin contract:** PR-D1 changes DOProvider.DetectDrift behavior. Backwards-compatible (only stub→real, return shape additive). Other plugins unaffected.
- **CLI:** `wfctl infra apply --refresh` is a new flag. Existing `wfctl infra apply` behavior unchanged for users who don't pass `--refresh`.
- **Production safety:** dry-run by default; `--allow-protected-prune` two-key contract; audit logging.
- **All other System Impact Matrix categories** (auth, anti-cheat, malware, sandbox, network, filesystem, process/OS, social, NPC, factions, economy, IoT, media, legal, forensics, VERA, achievements, client desktop, terminal, world history, content, telemetry): None — wfctl/plugin/state-mgmt plumbing.
