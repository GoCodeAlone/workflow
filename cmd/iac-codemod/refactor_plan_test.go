// Copyright (c) 2026 Jon Langevin
// SPDX-License-Identifier: Apache-2.0

// Tests in this file MUST NOT call t.Parallel(). Same global-state
// constraint as main_test.go and lint_test.go (the package-level `modes`
// map is mutated transitively through init()).

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================
// Golden-file source fixtures
// ============================================================

// canonicalPlanSrc is the configHash-compare canonical pattern T8.3
// targets for rewrite. Modeled on the DigitalOcean DOProvider.Plan body
// at workflow-plugin-digitalocean/internal/provider.go:141 (rev1 of the
// codemod). Mutation must replace the entire body with a single
// `return wfctlhelpers.Plan(ctx, p, desired, current)` and add an import
// for the helper package if it is not already present.
const canonicalPlanSrc = `package p

import (
	"context"
	"fmt"
	"time"
)

type ResourceSpec struct{ Name string; Config map[string]any }
type ResourceState struct{ Name string; AppliedConfig map[string]any }
type IaCPlan struct{ ID string; CreatedAt time.Time; Actions []PlanAction }
type PlanAction struct{ Action string; Resource ResourceSpec; Current *ResourceState }
type ApplyResult struct{}
type PlanDiagnostic struct{}

type DOProvider struct{}

func configHash(m map[string]any) string { return "" }

// Plan computes the set of actions needed to reach the desired state.
func (p *DOProvider) Plan(_ context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	currentByName := make(map[string]ResourceState, len(current))
	for _, r := range current {
		currentByName[r.Name] = r
	}

	plan := &IaCPlan{
		ID:        fmt.Sprintf("plan-%d", time.Now().UnixNano()),
		CreatedAt: time.Now(),
	}

	for _, spec := range desired {
		cur, exists := currentByName[spec.Name]
		if !exists {
			plan.Actions = append(plan.Actions, PlanAction{
				Action:   "create",
				Resource: spec,
			})
			continue
		}
		if configHash(cur.AppliedConfig) != configHash(spec.Config) {
			plan.Actions = append(plan.Actions, PlanAction{
				Action:   "update",
				Resource: spec,
				Current:  &cur,
			})
		}
	}
	return plan, nil
}

func (p *DOProvider) Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error) {
	return wfctlhelpers.ApplyPlan(ctx, p, plan)
}
func (p *DOProvider) ValidatePlan(plan *IaCPlan) []PlanDiagnostic { return nil }
`

// nonCanonicalPlanSrc has out-of-template logic (an extra log call and a
// custom return shape) that T8.3 must REFUSE to rewrite. The mode emits
// a finding instead.
const nonCanonicalPlanSrc = `package p

import (
	"context"
	"fmt"
)

type ResourceSpec struct{ Name string; Config map[string]any }
type ResourceState struct{ Name string; AppliedConfig map[string]any }
type IaCPlan struct{ Actions []PlanAction }
type PlanAction struct{ Action string; Resource ResourceSpec }
type ApplyResult struct{}

type FooProvider struct{}

func (p *FooProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	// Out-of-template: telemetry call + bespoke ordering logic.
	fmt.Println("planning custom flow")
	plan := &IaCPlan{}
	for _, spec := range desired {
		_ = spec
		plan.Actions = append(plan.Actions, PlanAction{Action: "noop"})
	}
	return plan, nil
}

func (p *FooProvider) Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error) { return nil, nil }
`

// skippedPlanSrc carries the canonical marker on the function doc and
// must NOT be rewritten regardless of body shape. The skipped site is
// listed in the report.
const skippedPlanSrc = `package p

import "context"

type ResourceSpec struct{}
type ResourceState struct{}
type IaCPlan struct{}
type ApplyResult struct{}
type FooProvider struct{}

// wfctl:skip-iac-codemod legacy custom planning, see ADR-042
func (p *FooProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	return &IaCPlan{}, nil
}

func (p *FooProvider) Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error) { return nil, nil }
`

// canonicalPlanWithExtraLoggingSrc — review round-1 finding #3. A Plan
// body whose desired-loop has an additional logging call (a real-world
// bespoke planner) must NOT be classified as canonical: silently
// rewriting it would drop the log line.
const canonicalPlanWithExtraLoggingSrc = `package p

import (
	"context"
	"fmt"
	"time"
)

type ResourceSpec struct{ Name string; Config map[string]any }
type ResourceState struct{ Name string; AppliedConfig map[string]any }
type IaCPlan struct{ ID string; CreatedAt time.Time; Actions []PlanAction }
type PlanAction struct{ Action string; Resource ResourceSpec; Current *ResourceState }
type ApplyResult struct{}
type PlanDiagnostic struct{}

type DOProvider struct{}

func configHash(m map[string]any) string { return "" }

func (p *DOProvider) Plan(_ context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	currentByName := make(map[string]ResourceState, len(current))
	for _, r := range current {
		currentByName[r.Name] = r
	}

	plan := &IaCPlan{
		ID:        fmt.Sprintf("plan-%d", time.Now().UnixNano()),
		CreatedAt: time.Now(),
	}

	for _, spec := range desired {
		fmt.Println("planning:", spec.Name)  // BESPOKE TELEMETRY — must not be silently dropped
		cur, exists := currentByName[spec.Name]
		if !exists {
			plan.Actions = append(plan.Actions, PlanAction{Action: "create", Resource: spec})
			continue
		}
		if configHash(cur.AppliedConfig) != configHash(spec.Config) {
			plan.Actions = append(plan.Actions, PlanAction{Action: "update", Resource: spec, Current: &cur})
		}
	}
	return plan, nil
}

func (p *DOProvider) Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error) {
	return wfctlhelpers.ApplyPlan(ctx, p, plan)
}
func (p *DOProvider) ValidatePlan(plan *IaCPlan) []PlanDiagnostic { return nil }
`

func TestRefactorPlan_ExtraLoggingNotCanonical(t *testing.T) {
	path := writeFixture(t, "provider.go", canonicalPlanWithExtraLoggingSrc)
	var stdout, stderr bytes.Buffer
	code := runRefactorPlan([]string{path}, &Options{DryRun: true, Fix: false}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if strings.Contains(out, "DOProvider.Plan canonical") {
		t.Errorf("Plan body with extra side-effect (telemetry) must NOT be classified canonical; got:\n%s", out)
	}
	if !strings.Contains(out, "non-canonical") {
		t.Errorf("Plan body with extra side-effect should be reported non-canonical; got:\n%s", out)
	}
}

// alreadyDelegatedPlanSrc has a Plan body that is already
// `return platform.ComputePlan(...)` (the rev1 review-corrected target).
// The mode must NOT report it as non-canonical and must NOT mutate it
// (idempotent).
const alreadyDelegatedPlanSrc = `package p

import "context"

type ResourceSpec struct{}
type ResourceState struct{}
type IaCPlan struct{}
type ApplyResult struct{}
type FooProvider struct{}

func (p *FooProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	return platform.ComputePlan(ctx, p, desired, current)
}

func (p *FooProvider) Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error) { return nil, nil }
`

// ============================================================
// Helpers
// ============================================================

// writeFixture writes src to a fresh tempdir, returning the path.
func writeFixture(t *testing.T, name, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
	return path
}

// ============================================================
// Detection / reporting (dry-run)
// ============================================================

func TestRefactorPlan_DryRun_DetectsCanonical(t *testing.T) {
	path := writeFixture(t, "provider.go", canonicalPlanSrc)
	var stdout, stderr bytes.Buffer
	code := runRefactorPlan([]string{path}, &Options{DryRun: true, Fix: false}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "DOProvider.Plan") {
		t.Errorf("report should name DOProvider.Plan; got:\n%s", out)
	}
	if !strings.Contains(out, "canonical") {
		t.Errorf("report should mark site as canonical (rewrite candidate); got:\n%s", out)
	}
	// Dry-run must not mutate.
	got, _ := os.ReadFile(path)
	if string(got) != canonicalPlanSrc {
		t.Errorf("dry-run modified the file; expected no mutation")
	}
}

func TestRefactorPlan_DryRun_ReportsNonCanonical(t *testing.T) {
	path := writeFixture(t, "provider.go", nonCanonicalPlanSrc)
	var stdout, stderr bytes.Buffer
	code := runRefactorPlan([]string{path}, &Options{DryRun: true, Fix: false}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "FooProvider.Plan") {
		t.Errorf("report should name FooProvider.Plan; got:\n%s", out)
	}
	if !strings.Contains(out, "non-canonical") {
		t.Errorf("report should mark site as non-canonical; got:\n%s", out)
	}
}

func TestRefactorPlan_DryRun_HonorsSkipMarker(t *testing.T) {
	path := writeFixture(t, "provider.go", skippedPlanSrc)
	var stdout, stderr bytes.Buffer
	code := runRefactorPlan([]string{path}, &Options{DryRun: true, Fix: false}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Skipped") {
		t.Errorf("report should have a Skipped section; got:\n%s", out)
	}
	if !strings.Contains(out, "FooProvider.Plan") {
		t.Errorf("Skipped section should list FooProvider.Plan; got:\n%s", out)
	}
}

func TestRefactorPlan_DryRun_AlreadyDelegatedReportedAsNoop(t *testing.T) {
	path := writeFixture(t, "provider.go", alreadyDelegatedPlanSrc)
	var stdout, stderr bytes.Buffer
	code := runRefactorPlan([]string{path}, &Options{DryRun: true, Fix: false}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if strings.Contains(out, "non-canonical") {
		t.Errorf("already-delegated Plan should NOT be reported non-canonical; got:\n%s", out)
	}
	if !strings.Contains(out, "already-delegated") {
		t.Errorf("already-delegated should be classified explicitly; got:\n%s", out)
	}
}

// ============================================================
// Mutation (-fix)
// ============================================================

func TestRefactorPlan_Fix_RewritesCanonical(t *testing.T) {
	path := writeFixture(t, "provider.go", canonicalPlanSrc)
	var stdout, stderr bytes.Buffer
	code := runRefactorPlan([]string{path}, &Options{DryRun: false, Fix: true}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after fix: %v", err)
	}
	gotStr := string(got)
	if !strings.Contains(gotStr, "return platform.ComputePlan(ctx, p, desired, current)") {
		t.Errorf("rewritten body should call platform.ComputePlan; got:\n%s", gotStr)
	}
	if strings.Contains(gotStr, "currentByName := make(") {
		t.Errorf("canonical body should be removed by rewrite; got:\n%s", gotStr)
	}
	// Helper import must be present after rewrite.
	if !strings.Contains(gotStr, `"github.com/GoCodeAlone/workflow/platform"`) {
		t.Errorf("rewrite should add platform import; got:\n%s", gotStr)
	}
}

func TestRefactorPlan_Fix_RenamesBlankReceiverParamSoCtxResolves(t *testing.T) {
	// The DO provider declares Plan(_ context.Context, ...) and after
	// rewrite the body must reference the ctx parameter. The codemod
	// renames the blank `_` parameter to `ctx` so the substituted call
	// compiles. Pinned regression: if the renamer is dropped, the
	// rewritten file fails to type-check.
	path := writeFixture(t, "provider.go", canonicalPlanSrc)
	var stdout, stderr bytes.Buffer
	if code := runRefactorPlan([]string{path}, &Options{DryRun: false, Fix: true}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "Plan(ctx context.Context") {
		t.Errorf("blank ctx param should be renamed to ctx so the rewritten body compiles; got:\n%s", string(got))
	}
}

func TestRefactorPlan_Fix_DoesNotRewriteNonCanonical(t *testing.T) {
	path := writeFixture(t, "provider.go", nonCanonicalPlanSrc)
	var stdout, stderr bytes.Buffer
	code := runRefactorPlan([]string{path}, &Options{DryRun: false, Fix: true}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	got, _ := os.ReadFile(path)
	if string(got) != nonCanonicalPlanSrc {
		t.Errorf("non-canonical body must NOT be rewritten; file changed:\n%s", string(got))
	}
}

func TestRefactorPlan_Fix_HonorsSkipMarker(t *testing.T) {
	path := writeFixture(t, "provider.go", skippedPlanSrc)
	var stdout, stderr bytes.Buffer
	code := runRefactorPlan([]string{path}, &Options{DryRun: false, Fix: true}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	got, _ := os.ReadFile(path)
	if string(got) != skippedPlanSrc {
		t.Errorf("skip-marker'd body must NOT be rewritten; file changed:\n%s", string(got))
	}
}

func TestRefactorPlan_Fix_IdempotentOnAlreadyDelegated(t *testing.T) {
	path := writeFixture(t, "provider.go", alreadyDelegatedPlanSrc)
	var stdout, stderr bytes.Buffer
	if code := runRefactorPlan([]string{path}, &Options{DryRun: false, Fix: true}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	got, _ := os.ReadFile(path)
	if string(got) != alreadyDelegatedPlanSrc {
		t.Errorf("already-delegated source must be byte-identical after fix (idempotent); diff:\nbefore:\n%s\nafter:\n%s", alreadyDelegatedPlanSrc, string(got))
	}
}

// ============================================================
// Mutation-gate negative tests (T8.1 review pattern)
// ============================================================

// TestRefactorPlan_DryRunFalseWithoutFix_DoesNotMutate pins the dispatcher
// gate from main_test.go: a user-supplied -dry-run=false without -fix must
// NOT bypass mutation. The mode is invoked via run() so dispatcher
// normalization runs; we then verify file mtime and content unchanged.
func TestRefactorPlan_DryRunFalseWithoutFix_DoesNotMutate(t *testing.T) {
	path := writeFixture(t, "provider.go", canonicalPlanSrc)
	stat0, _ := os.Stat(path)
	mtime0 := stat0.ModTime()

	// Sleep 1 nanosecond worth of mtime resolution? We use file mtime AND
	// content equality; either being unchanged is sufficient. For
	// portability across filesystems, we don't require sub-second mtime
	// granularity — we assert content unchanged AND the dispatcher
	// normalized DryRun=true.
	time.Sleep(10 * time.Millisecond)

	var stdout, stderr bytes.Buffer
	code := run([]string{"refactor-plan", "-dry-run=false", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	got, _ := os.ReadFile(path)
	if string(got) != canonicalPlanSrc {
		t.Errorf("file must NOT be mutated when -dry-run=false is passed without -fix; content changed:\n%s", string(got))
	}
	stat1, _ := os.Stat(path)
	if !stat1.ModTime().Equal(mtime0) {
		t.Errorf("file mtime should be unchanged; before=%v after=%v", mtime0, stat1.ModTime())
	}
}
