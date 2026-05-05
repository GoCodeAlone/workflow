// Copyright (c) 2026 Jon Langevin
// SPDX-License-Identifier: Apache-2.0

// See main_test.go for the t.Parallel() prohibition (this file follows
// the same constraint — modes map is mutated transitively via the lint
// init() call and cross-test analyzer state).

package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
)

// runAnalyzerOnSource parses a single Go source string, type-checks it
// tolerantly, runs the supplied analyzer, and returns the REAL diagnostics
// (skip-encoded synthetic diagnostics from skip-marker handling are
// filtered out here, matching the driver's post-processing). Use
// runAnalyzerOnSourceRaw if you need to inspect skip records directly.
func runAnalyzerOnSource(t *testing.T, src string, analyzer *analysis.Analyzer) []analysis.Diagnostic {
	t.Helper()
	all := runAnalyzerOnSourceRaw(t, src, analyzer)
	out := all[:0]
	for _, d := range all {
		if strings.HasPrefix(d.Message, skipDiagnosticPrefix) {
			continue
		}
		out = append(out, d)
	}
	return out
}

// runAnalyzerOnSourceRaw is like runAnalyzerOnSource but returns ALL
// diagnostics (including skip-encoded ones). Used by skip-marker tests
// that need to verify the synthetic record was emitted at all.
func runAnalyzerOnSourceRaw(t *testing.T, src string, analyzer *analysis.Analyzer) []analysis.Diagnostic {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "src.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v\nsrc:\n%s", err, src)
	}
	conf := &types.Config{
		Importer: stubImporter{},
		Error:    func(err error) {}, // tolerate unresolved-import / undeclared-name errors
	}
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Implicits:  make(map[ast.Node]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	pkg, _ := conf.Check(file.Name.Name, fset, []*ast.File{file}, info)
	var diags []analysis.Diagnostic
	pass := &analysis.Pass{
		Analyzer:  analyzer,
		Fset:      fset,
		Files:     []*ast.File{file},
		Pkg:       pkg,
		TypesInfo: info,
		Report:    func(d analysis.Diagnostic) { diags = append(diags, d) },
	}
	if _, err := analyzer.Run(pass); err != nil {
		t.Fatalf("analyzer %s: %v", analyzer.Name, err)
	}
	return diags
}

// stubImporter is a tolerant importer that returns an empty package for
// any import path. It lets type-check proceed past unresolved imports
// like "wfctlhelpers" or "interfaces" without bailing.
type stubImporter struct{}

func (stubImporter) Import(path string) (*types.Package, error) {
	return types.NewPackage(path, filepath.Base(path)), nil
}

// ============================================================
// AssertPlanDelegatesToHelper
// ============================================================

// providerScaffold is the boilerplate every Plan/Apply test source
// includes so its receiver type satisfies the precision filter
// (providerLikeReceivers — must have BOTH Plan and Apply matching
// IaCProvider shape) and the integration-test "no findings" cases pass
// every analyzer. Apply is canonical and ValidatePlan is present so
// only the method under test (Plan) drives the analyzer behaviour.
const providerScaffold = `package p
import "context"

type ResourceSpec struct{}
type ResourceState struct{}
type IaCPlan struct{}
type ApplyResult struct{}
type PlanDiagnostic struct{}

type FooProvider struct{}

func (p *FooProvider) Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error) {
	return wfctlhelpers.ApplyPlan(ctx, p, plan)
}
func (p *FooProvider) ValidatePlan(plan *IaCPlan) []PlanDiagnostic { return nil }
`

// planCanonicalSrc uses the canonical 2-statement form per round-2
// finding #1 (the value/pointer bridge for platform.ComputePlan).
const planCanonicalSrc = providerScaffold + `
func (p *FooProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	plan, err := platform.ComputePlan(ctx, p, desired, current)
	return &plan, err
}
`

// planLegacyDelegatedSrc preserves the rev0 codemod's planned-but-not-shipped
// `wfctlhelpers.Plan` target as also-accepted. Pinned regression: a maintainer
// who hand-applied an early version of the codemod must NOT be re-flagged.
const planLegacyDelegatedSrc = providerScaffold + `
func (p *FooProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	return wfctlhelpers.Plan(ctx, p, desired, current)
}
`

const planNonCanonicalSrc = providerScaffold + `
func (p *FooProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	// Custom planning logic, not delegating to platform.ComputePlan.
	return &IaCPlan{}, nil
}
`

const planSkippedSrc = providerScaffold + `
// wfctl:skip-iac-codemod
func (p *FooProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	return &IaCPlan{}, nil
}
`

func TestAssertPlanDelegatesToHelper_Canonical_NoDiagnostic(t *testing.T) {
	diags := runAnalyzerOnSource(t, planCanonicalSrc, AssertPlanDelegatesToHelper)
	if len(diags) != 0 {
		t.Errorf("canonical Plan should produce no diagnostic; got %d:\n%s", len(diags), diagSummary(diags))
	}
}

func TestAssertPlanDelegatesToHelper_NonCanonical_Diagnoses(t *testing.T) {
	diags := runAnalyzerOnSource(t, planNonCanonicalSrc, AssertPlanDelegatesToHelper)
	if len(diags) != 1 {
		t.Fatalf("non-canonical Plan should produce 1 diagnostic; got %d:\n%s", len(diags), diagSummary(diags))
	}
	if !strings.Contains(diags[0].Message, "platform.ComputePlan") {
		t.Errorf("diagnostic should reference platform.ComputePlan (canonical target); got %q", diags[0].Message)
	}
}

// TestAssertPlanDelegatesToHelper_LegacyTargetAccepted pins review round-1
// finding #1: the analyzer accepts the legacy `wfctlhelpers.Plan` target as
// already-delegated so a maintainer who hand-applied the rev0 codemod isn't
// re-flagged on the next run.
func TestAssertPlanDelegatesToHelper_LegacyTargetAccepted(t *testing.T) {
	diags := runAnalyzerOnSource(t, planLegacyDelegatedSrc, AssertPlanDelegatesToHelper)
	if len(diags) != 0 {
		t.Errorf("legacy wfctlhelpers.Plan target must be accepted as delegated; got %d:\n%s", len(diags), diagSummary(diags))
	}
}

func TestAssertPlanDelegatesToHelper_SkipMarker_Honored(t *testing.T) {
	// Real findings should be empty (the marker suppresses the
	// non-canonical-Plan diagnostic).
	diags := runAnalyzerOnSource(t, planSkippedSrc, AssertPlanDelegatesToHelper)
	if len(diags) != 0 {
		t.Errorf("skip-marker should suppress real diagnostic; got %d:\n%s", len(diags), diagSummary(diags))
	}
	// And a skip-encoded synthetic diagnostic should be present so the
	// driver can surface the skipped site in its report (plan rev2 line
	// 2400: "Each mode also surfaces a list of skipped sites in its
	// report").
	all := runAnalyzerOnSourceRaw(t, planSkippedSrc, AssertPlanDelegatesToHelper)
	gotSkip := false
	for _, d := range all {
		if strings.HasPrefix(d.Message, skipDiagnosticPrefix) {
			gotSkip = true
			break
		}
	}
	if !gotSkip {
		t.Errorf("skip-marker should produce a skip record for the driver to surface; got:\n%s", diagSummary(all))
	}
}

// TestSkipMarker_AcceptsTrailingJustification pins review-round-2 finding
// #2: a trailing space + justification context after SkipMarker is a
// natural Go idiom (`// wfctl:skip-iac-codemod legacy upsert recovery,
// see ADR-042`) and must NOT silently turn the marker into a no-op.
// Plan rev2 line 2400 unifies the marker specifically to prevent
// silent-no-op surfaces; permissive trailing-context is in that family.
const planSkippedWithJustificationSrc = providerScaffold + `
// wfctl:skip-iac-codemod legacy upsert recovery, see ADR-042
func (p *FooProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	return &IaCPlan{}, nil
}
`

func TestSkipMarker_AcceptsTrailingJustification(t *testing.T) {
	diags := runAnalyzerOnSource(t, planSkippedWithJustificationSrc, AssertPlanDelegatesToHelper)
	if len(diags) != 0 {
		t.Errorf("trailing justification text after marker must NOT silently break suppression; got %d diagnostics:\n%s", len(diags), diagSummary(diags))
	}
}

// TestSkipMarker_RejectsCloseButWrongMarker confirms we only accept the
// canonical marker — a different prefix (e.g. legacy
// `// wfctl:skip-codemod` from the design rev1 era) should still
// flag the diagnostic. Guards against accidentally-too-loose matching.
const planSkippedWithWrongMarkerSrc = providerScaffold + `
// wfctl:skip-codemod
func (p *FooProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	return &IaCPlan{}, nil
}
`

func TestSkipMarker_RejectsCloseButWrongMarker(t *testing.T) {
	diags := runAnalyzerOnSource(t, planSkippedWithWrongMarkerSrc, AssertPlanDelegatesToHelper)
	if len(diags) != 1 {
		t.Errorf("non-canonical marker `// wfctl:skip-codemod` must NOT suppress the diagnostic (plan rev2 unifies on // wfctl:skip-iac-codemod ONLY); got %d:\n%s", len(diags), diagSummary(diags))
	}
}

// TestSkipMarker_AcceptsTabDelimitedJustification — review round-2
// follow-up A. Maintainers who tab-align justifications must NOT see a
// silent no-op; the marker logic widens to accept any whitespace
// separator.
const planSkippedTabJustifiedSrc = providerScaffold + "\n// wfctl:skip-iac-codemod\tlegacy upsert recovery, see ADR-042\nfunc (p *FooProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {\n\treturn &IaCPlan{}, nil\n}\n"

func TestSkipMarker_AcceptsTabDelimitedJustification(t *testing.T) {
	diags := runAnalyzerOnSource(t, planSkippedTabJustifiedSrc, AssertPlanDelegatesToHelper)
	if len(diags) != 0 {
		t.Errorf("tab-delimited justification must NOT silently break the marker; got %d:\n%s", len(diags), diagSummary(diags))
	}
}

// TestSkipMarker_RejectsAdjacentNonWhitespace — review round-2 follow-up
// C. Pin that strings sharing the marker prefix but extending without a
// whitespace separator (dash/letter/digit suffix) are NOT accepted as
// the marker, so future loosening of hasSkipMarkerOn fails this test.
func TestSkipMarker_RejectsAdjacentNonWhitespace(t *testing.T) {
	cases := []struct {
		name, comment string
	}{
		{"dash-suffix", "// wfctl:skip-iac-codemod-extension"},
		{"letters-suffix", "// wfctl:skip-iac-codemodSOMETHING"},
		{"digit-suffix", "// wfctl:skip-iac-codemod1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := providerScaffold + "\n" + tc.comment + "\nfunc (p *FooProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {\n\treturn &IaCPlan{}, nil\n}\n"
			diags := runAnalyzerOnSource(t, src, AssertPlanDelegatesToHelper)
			if len(diags) != 1 {
				t.Errorf("comment %q without whitespace separator must NOT match the marker; got %d:\n%s", tc.comment, len(diags), diagSummary(diags))
			}
		})
	}
}

// ============================================================
// AssertApplyDelegatesToHelper
// ============================================================

// applyTestScaffold mirrors providerScaffold but with a canonical Plan
// (so the receiver passes the provider-like filter without the Apply
// analyzer under test being affected). ValidatePlan is included so
// integration-test "no findings" cases stay clean across all analyzers.
const applyTestScaffold = `package p
import "context"

type ResourceSpec struct{}
type ResourceState struct{}
type IaCPlan struct{}
type ApplyResult struct{}
type PlanDiagnostic struct{}

type FooProvider struct{}

func (p *FooProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	plan, err := platform.ComputePlan(ctx, p, desired, current)
	return &plan, err
}
func (p *FooProvider) ValidatePlan(plan *IaCPlan) []PlanDiagnostic { return nil }
`

const applyCanonicalSrc = applyTestScaffold + `
func (p *FooProvider) Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error) {
	return wfctlhelpers.ApplyPlan(ctx, p, plan)
}
`

const applyNonCanonicalSrc = applyTestScaffold + `
func (p *FooProvider) Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error) {
	return &ApplyResult{}, nil
}
`

func TestAssertApplyDelegatesToHelper_Canonical_NoDiagnostic(t *testing.T) {
	diags := runAnalyzerOnSource(t, applyCanonicalSrc, AssertApplyDelegatesToHelper)
	if len(diags) != 0 {
		t.Errorf("canonical Apply should produce no diagnostic; got %d:\n%s", len(diags), diagSummary(diags))
	}
}

func TestAssertApplyDelegatesToHelper_NonCanonical_Diagnoses(t *testing.T) {
	diags := runAnalyzerOnSource(t, applyNonCanonicalSrc, AssertApplyDelegatesToHelper)
	if len(diags) != 1 {
		t.Fatalf("non-canonical Apply should produce 1 diagnostic; got %d:\n%s", len(diags), diagSummary(diags))
	}
	if !strings.Contains(diags[0].Message, "wfctlhelpers.ApplyPlan") {
		t.Errorf("diagnostic should reference wfctlhelpers.ApplyPlan; got %q", diags[0].Message)
	}
}

// ============================================================
// AssertDiffSetsNeedsReplaceForForceNew
// ============================================================

// driverScaffold provides the Read companion method that
// driverLikeReceivers requires before AssertDiffSetsNeedsReplaceForForceNew
// will fire. Drivers conventionally have Read in addition to Diff.
const driverScaffold = `package p
import "context"

type ResourceSpec struct{}
type ResourceOutput struct{}
type ResourceState struct{}
type FieldChange struct {
	ForceNew bool
}
type DiffResult struct {
	NeedsReplace bool
	Changes      []FieldChange
}

type FooDriver struct{}

func (d *FooDriver) Read(ctx context.Context, s ResourceState) (*ResourceOutput, error) {
	return nil, nil
}
`

const diffCanonicalSrc = driverScaffold + `
func (d *FooDriver) Diff(ctx context.Context, desired ResourceSpec, current *ResourceOutput) (*DiffResult, error) {
	r := &DiffResult{}
	for _, c := range r.Changes {
		if c.ForceNew {
			r.NeedsReplace = true
		}
	}
	return r, nil
}
`

const diffMissingNeedsReplaceSrc = driverScaffold + `
func (d *FooDriver) Diff(ctx context.Context, desired ResourceSpec, current *ResourceOutput) (*DiffResult, error) {
	r := &DiffResult{}
	for _, c := range r.Changes {
		if c.ForceNew {
			// Forgot to set NeedsReplace=true — this is the bug the analyzer flags.
			_ = c
		}
	}
	return r, nil
}
`

func TestAssertDiffSetsNeedsReplaceForForceNew_Canonical_NoDiagnostic(t *testing.T) {
	diags := runAnalyzerOnSource(t, diffCanonicalSrc, AssertDiffSetsNeedsReplaceForForceNew)
	if len(diags) != 0 {
		t.Errorf("canonical Diff should produce no diagnostic; got %d:\n%s", len(diags), diagSummary(diags))
	}
}

func TestAssertDiffSetsNeedsReplaceForForceNew_MissingAssign_Diagnoses(t *testing.T) {
	diags := runAnalyzerOnSource(t, diffMissingNeedsReplaceSrc, AssertDiffSetsNeedsReplaceForForceNew)
	if len(diags) != 1 {
		t.Fatalf("Diff that references ForceNew but never assigns NeedsReplace should produce 1 diagnostic; got %d:\n%s", len(diags), diagSummary(diags))
	}
	if !strings.Contains(diags[0].Message, "NeedsReplace") {
		t.Errorf("diagnostic should reference NeedsReplace; got %q", diags[0].Message)
	}
}

// TestAssertDiffSetsNeedsReplaceForForceNew_AcceptsDirectAssign pins
// review finding #4: the alternate canonical pattern `r.NeedsReplace =
// c.ForceNew` (instead of `if c.ForceNew { r.NeedsReplace = true }`)
// also satisfies the W-3 force-new contract and must NOT trigger a
// false-positive diagnostic.
const diffDirectAssignSrc = `package p
import "context"

type ResourceSpec struct{}
type ResourceOutput struct{}
type ResourceState struct{}
type IaCPlan struct{}
type ApplyResult struct{}
type FieldChange struct {
	ForceNew bool
}
type DiffResult struct {
	NeedsReplace bool
	Changes      []FieldChange
}

type FooDriver struct{}

func (d *FooDriver) Read(ctx context.Context, s ResourceState) (*ResourceOutput, error) {
	return nil, nil
}

func (d *FooDriver) Diff(ctx context.Context, desired ResourceSpec, current *ResourceOutput) (*DiffResult, error) {
	r := &DiffResult{}
	for _, c := range r.Changes {
		r.NeedsReplace = c.ForceNew
	}
	return r, nil
}
`

func TestAssertDiffSetsNeedsReplaceForForceNew_AcceptsDirectAssign(t *testing.T) {
	diags := runAnalyzerOnSource(t, diffDirectAssignSrc, AssertDiffSetsNeedsReplaceForForceNew)
	if len(diags) != 0 {
		t.Errorf("`r.NeedsReplace = c.ForceNew` is a valid alternate canonical; should NOT flag; got %d:\n%s", len(diags), diagSummary(diags))
	}
}

// TestAssertDiffSetsNeedsReplaceForForceNew_RejectsLiteralFalseAssign
// — review round-2 follow-up B. The widened bodyAssignsField (any RHS)
// would silently accept `r.NeedsReplace = false` inside a ForceNew
// branch — a real copy-paste bug pattern. The matcher must specifically
// treat literal-`false` RHS as no-assignment so this typo is still
// flagged.
const diffLiteralFalseSrc = driverScaffold + `
func (d *FooDriver) Diff(ctx context.Context, desired ResourceSpec, current *ResourceOutput) (*DiffResult, error) {
	r := &DiffResult{}
	for _, c := range r.Changes {
		if c.ForceNew {
			r.NeedsReplace = false
		}
	}
	return r, nil
}
`

func TestAssertDiffSetsNeedsReplaceForForceNew_RejectsLiteralFalseAssign(t *testing.T) {
	diags := runAnalyzerOnSource(t, diffLiteralFalseSrc, AssertDiffSetsNeedsReplaceForForceNew)
	if len(diags) != 1 {
		t.Errorf("`r.NeedsReplace = false` is a copy-paste bug — analyzer must flag; got %d:\n%s", len(diags), diagSummary(diags))
	}
}

// TestAssertDiffSetsNeedsReplaceForForceNew_AccumulatorPatternIsClean pins
// workflow#539: the local-accumulator pattern is a valid expression of
// the W-3 force-new contract. The driver declares `var needsReplace
// bool`, sets `needsReplace = true` inside ForceNew-driven branches,
// then returns `&DiffResult{NeedsReplace: needsReplace, ...}`. The
// analyzer must recognize the struct-literal `KeyValueExpr` form as an
// assignment site, not just `*ast.AssignStmt` — otherwise the
// accumulator pattern is reported as a contract violation.
const diffAccumulatorSrc = driverScaffold + `
func (d *FooDriver) Diff(ctx context.Context, desired ResourceSpec, current *ResourceOutput) (*DiffResult, error) {
	if current == nil {
		return &DiffResult{}, nil
	}
	var changes []FieldChange
	var needsReplace bool
	for _, c := range changes {
		if c.ForceNew {
			needsReplace = true
		}
	}
	return &DiffResult{
		NeedsReplace: needsReplace,
		Changes:      changes,
	}, nil
}
`

func TestAssertDiffSetsNeedsReplaceForForceNew_AccumulatorPatternIsClean(t *testing.T) {
	diags := runAnalyzerOnSource(t, diffAccumulatorSrc, AssertDiffSetsNeedsReplaceForForceNew)
	if len(diags) != 0 {
		t.Errorf("accumulator pattern `&DiffResult{NeedsReplace: needsReplace}` is a valid alternate canonical (workflow#539); should NOT flag; got %d:\n%s", len(diags), diagSummary(diags))
	}
}

// TestAssertDiffSetsNeedsReplaceForForceNew_StructLiteralFalseStillFlags
// is the symmetry test for the accumulator widening: a struct literal
// `&DiffResult{NeedsReplace: false}` inside a function that observes
// ForceNew is still a copy-paste bug and must be flagged. Without this
// guard, the widened matcher would silently accept the bug-shape.
const diffStructLiteralFalseSrc = driverScaffold + `
func (d *FooDriver) Diff(ctx context.Context, desired ResourceSpec, current *ResourceOutput) (*DiffResult, error) {
	var changes []FieldChange
	for _, c := range changes {
		if c.ForceNew {
			_ = c
		}
	}
	return &DiffResult{
		NeedsReplace: false,
		Changes:      changes,
	}, nil
}
`

func TestAssertDiffSetsNeedsReplaceForForceNew_StructLiteralFalseStillFlags(t *testing.T) {
	diags := runAnalyzerOnSource(t, diffStructLiteralFalseSrc, AssertDiffSetsNeedsReplaceForForceNew)
	if len(diags) != 1 {
		t.Errorf("`&DiffResult{NeedsReplace: false}` is a copy-paste bug; analyzer must flag; got %d:\n%s", len(diags), diagSummary(diags))
	}
}

// TestAssertDiffSetsNeedsReplaceForForceNew_NonDriverNotFlagged pins
// review finding #3: the analyzer must NOT fire on types that have a
// method named Diff but are not resource drivers (no Read / Create /
// Update / Delete companion methods). Adversarially: a `func (s
// *Settings) Diff(...)` that happens to match the arity should be
// invisible to this analyzer.
const diffNonDriverSrc = `package p
import "context"

type ResourceSpec struct{}
type ResourceOutput struct{}
type FieldChange struct {
	ForceNew bool
}
type DiffResult struct {
	NeedsReplace bool
	Changes      []FieldChange
}

// Not a driver — no Read/Create/Update/Delete. Just a settings struct
// that exposes a "Diff" method for unrelated reasons (e.g. config diff).
type SettingsDiff struct{}

func (s *SettingsDiff) Diff(ctx context.Context, desired ResourceSpec, current *ResourceOutput) (*DiffResult, error) {
	r := &DiffResult{}
	for _, c := range r.Changes {
		if c.ForceNew {
			// No NeedsReplace assign — but this isn't a driver, so we
			// should not flag.
			_ = c
		}
	}
	return r, nil
}
`

func TestAssertDiffSetsNeedsReplaceForForceNew_NonDriverNotFlagged(t *testing.T) {
	diags := runAnalyzerOnSource(t, diffNonDriverSrc, AssertDiffSetsNeedsReplaceForForceNew)
	if len(diags) != 0 {
		t.Errorf("type with Diff() but no driver-companion method (Read/Create/Update/Delete) should NOT be flagged; got %d:\n%s", len(diags), diagSummary(diags))
	}
}

// Refresh diffCanonicalSrc to include a Read companion method so it
// passes the new precision filter (provider/driver heuristic).
// (The constant is replaced via Edit after the analyzer is updated;
// this comment is an intent marker only — see lint_test.go body.)

// ============================================================
// AssertProviderImplementsValidatePlan
// ============================================================

const providerWithValidatePlanSrc = `package p
import "context"

type ResourceSpec struct{}
type ResourceState struct{}
type IaCPlan struct{}
type ApplyResult struct{}
type PlanDiagnostic struct{}

type FooProvider struct{}

func (p *FooProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	return nil, nil
}
func (p *FooProvider) Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error) {
	return nil, nil
}
func (p *FooProvider) ValidatePlan(plan *IaCPlan) []PlanDiagnostic {
	return nil
}
`

const providerWithoutValidatePlanSrc = `package p
import "context"

type ResourceSpec struct{}
type ResourceState struct{}
type IaCPlan struct{}
type ApplyResult struct{}

type FooProvider struct{}

func (p *FooProvider) Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error) {
	return nil, nil
}
func (p *FooProvider) Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error) {
	return nil, nil
}
`

func TestAssertProviderImplementsValidatePlan_HasValidatePlan_NoDiagnostic(t *testing.T) {
	diags := runAnalyzerOnSource(t, providerWithValidatePlanSrc, AssertProviderImplementsValidatePlan)
	if len(diags) != 0 {
		t.Errorf("provider with ValidatePlan should produce no diagnostic; got %d:\n%s", len(diags), diagSummary(diags))
	}
}

func TestAssertProviderImplementsValidatePlan_Missing_Diagnoses(t *testing.T) {
	diags := runAnalyzerOnSource(t, providerWithoutValidatePlanSrc, AssertProviderImplementsValidatePlan)
	if len(diags) != 1 {
		t.Fatalf("provider without ValidatePlan should produce 1 diagnostic; got %d:\n%s", len(diags), diagSummary(diags))
	}
	if !strings.Contains(diags[0].Message, "ValidatePlan") {
		t.Errorf("diagnostic should reference ValidatePlan; got %q", diags[0].Message)
	}
}

// ============================================================
// runLint dispatcher (integration)
// ============================================================

// writeTempPackage writes a single-package set of files to a tempdir
// and returns the dir.
func writeTempPackage(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// runLintInDir invokes runLint against dir with the given Options and
// returns stdout, stderr, exit code.
func runLintInDir(t *testing.T, dir string, opts Options) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runLint([]string{dir}, &opts, &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func TestRunLint_NoArgs_Exits2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runLint(nil, &Options{DryRun: true}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}

func TestRunLint_CanonicalSource_NoFindings(t *testing.T) {
	dir := writeTempPackage(t, map[string]string{
		"provider.go": planCanonicalSrc,
	})
	stdout, _, code := runLintInDir(t, dir, Options{DryRun: true})
	if code != 0 {
		t.Errorf("exit = %d, want 0; stdout=%s", code, stdout)
	}
	if strings.Contains(stdout, "AssertPlanDelegatesToHelper") {
		t.Errorf("canonical source should not be flagged; stdout:\n%s", stdout)
	}
}

func TestRunLint_NonCanonical_FindingsPresent(t *testing.T) {
	dir := writeTempPackage(t, map[string]string{
		"provider.go": planNonCanonicalSrc,
	})
	stdout, _, code := runLintInDir(t, dir, Options{DryRun: true})
	if code != 1 {
		t.Errorf("exit = %d, want 1 (findings present); stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, "AssertPlanDelegatesToHelper") {
		t.Errorf("expected analyzer name in report; stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "provider.go") {
		t.Errorf("expected file path in report; stdout:\n%s", stdout)
	}
}

func TestRunLint_SkipMarker_SurfacedInReport(t *testing.T) {
	dir := writeTempPackage(t, map[string]string{
		"provider.go": planSkippedSrc,
	})
	stdout, _, code := runLintInDir(t, dir, Options{DryRun: true})
	if code != 0 {
		t.Errorf("exit = %d, want 0 (skipped, no findings); stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, "Skipped") {
		t.Errorf("report must surface skipped sites; stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "provider.go") {
		t.Errorf("skipped section must include file path; stdout:\n%s", stdout)
	}
}

// TestRunLint_DoesNotMutateFilesEvenWithFixFlag pins the contract from
// carry-forward #2: lint is read-only by definition. Even with -fix and
// -dry-run=false, file mtimes and contents must be unchanged across the
// run. (Fix=true cannot reach this code path through the dispatcher
// because run() in main.go normalizes the gate, but the in-mode contract
// is also pinned for defense-in-depth.)
func TestRunLint_DoesNotMutateFilesEvenWithFixFlag(t *testing.T) {
	dir := writeTempPackage(t, map[string]string{
		"provider.go": planNonCanonicalSrc,
	})
	target := filepath.Join(dir, "provider.go")

	beforeStat, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat before: %v", err)
	}
	beforeContent, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	// Hostile flags: simulate a caller bypassing the dispatcher's gate.
	_, _, _ = runLintInDir(t, dir, Options{Fix: true, DryRun: false})

	afterStat, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	afterContent, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}

	if !beforeStat.ModTime().Equal(afterStat.ModTime()) {
		t.Errorf("mtime changed: before=%v, after=%v — lint must never mutate", beforeStat.ModTime(), afterStat.ModTime())
	}
	if !bytes.Equal(beforeContent, afterContent) {
		t.Errorf("content changed — lint must never mutate")
	}
}

func TestRunLint_FixFlag_WarnsItHasNoEffect(t *testing.T) {
	dir := writeTempPackage(t, map[string]string{
		"provider.go": planCanonicalSrc,
	})
	_, stderr, _ := runLintInDir(t, dir, Options{Fix: true, DryRun: false})
	if !strings.Contains(stderr, "no effect") {
		t.Errorf("stderr should warn that -fix has no effect on lint; got:\n%s", stderr)
	}
}

func TestRunLint_AnalyzerCount_FourRegistered(t *testing.T) {
	if len(lintAnalyzers) != 4 {
		t.Errorf("plan §T8.2 mandates 4 analyzers; got %d", len(lintAnalyzers))
	}
	want := []string{
		"AssertPlanDelegatesToHelper",
		"AssertApplyDelegatesToHelper",
		"AssertDiffSetsNeedsReplaceForForceNew",
		"AssertProviderImplementsValidatePlan",
	}
	got := make(map[string]bool)
	for _, a := range lintAnalyzers {
		got[a.Name] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("plan-literal analyzer %q is missing from lintAnalyzers", name)
		}
	}
}

func TestRunLint_RegistersIntoModesMap(t *testing.T) {
	fn, ok := modes["lint"]
	if !ok {
		t.Fatalf("lint init() must register runLint into modes map")
	}
	if fn == nil {
		t.Fatalf("modes[\"lint\"] is nil")
	}
}

// diagSummary formats a slice of diagnostics for test failure messages.
func diagSummary(diags []analysis.Diagnostic) string {
	if len(diags) == 0 {
		return "  (none)"
	}
	var sb strings.Builder
	for i, d := range diags {
		fmt.Fprintf(&sb, "  [%d] pos=%d: %s\n", i, d.Pos, d.Message)
	}
	return sb.String()
}
