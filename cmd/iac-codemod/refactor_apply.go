// Copyright (c) 2026 Jon Langevin
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func init() {
	modes["refactor-apply"] = runRefactorApply
}

// applyCanonicalCallExpr is the canonical replacement-body expression
// emitted by refactor-apply.
const applyCanonicalCallExpr = "wfctlhelpers.ApplyPlan(ctx, p, plan)"

// applyClassification labels the disposition of a single Apply()
// method site. The non-canonical idioms are surfaced as distinct
// classes so the report can suggest the right hand-port handling.
type applyClassification int

const (
	applyCanonical applyClassification = iota
	applyAlreadyDelegated
	applySkipped
	// Non-canonical idioms (each with its own suggested handling):
	applyUpsertRecovery        // DO upsert-on-create-conflict — emit upsertSupporter hook patch
	applyUpdateReplaceCollapse // AWS `case "update", "replace":` — emit "manual port required"
	applyCustomErrorWrapping   // custom fmt.Errorf wrapping — emit extension-point hook + sample
	applyNonCanonicalOther     // some other shape we don't recognise
	applyMissingSwitch         // no switch-on-action; cannot mechanically rewrite
)

func (c applyClassification) String() string {
	switch c {
	case applyCanonical:
		return "canonical"
	case applyAlreadyDelegated:
		return "already-delegated"
	case applySkipped:
		return "skipped"
	case applyUpsertRecovery:
		return "upsert-recovery"
	case applyUpdateReplaceCollapse:
		return "update+replace-collapse"
	case applyCustomErrorWrapping:
		return "custom-error-wrapping"
	case applyNonCanonicalOther:
		return "non-canonical"
	case applyMissingSwitch:
		return "missing-action-switch"
	default:
		return "unknown"
	}
}

// applySite captures one Apply-method site in the report.
type applySite struct {
	Path        string
	Line        int
	Receiver    string
	Class       applyClassification
	OffenderPos string // path:line of the offending construct (for collapse/wrap idioms)
	Suggestion  string // hand-port suggestion text
	Rewrote     bool
}

// applyReport aggregates per-file results across an entire refactor-apply
// run.
type applyReport struct {
	sites  []applySite
	errors []string
}

// runRefactorApply is the entry point for the refactor-apply subcommand.
// Mode-local flags (currently `-report-file`) are parsed off `args`
// before path walking begins.
func runRefactorApply(args []string, opts *Options, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("iac-codemod refactor-apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	// Steer per-mode -h to stdout for symmetry with the top-level
	// `iac-codemod -h` (T8.2 carry-forward #1). The dispatcher in main.go
	// intercepts `-h` before it reaches this FlagSet, so this closure
	// only fires on parse errors. Mode-specific flags (-report-file)
	// are documented in main.go's global usage() text — that's the
	// fix surface for review round-1 finding #11.
	fs.Usage = func() { usage(stdout) }
	reportFile := fs.String("report-file", "", "if set, also write the report (Markdown) to this path; default is stdout-only")
	if err := fs.Parse(args); err != nil {
		// flag.ContinueOnError already wrote a parse-error message via
		// SetOutput(stderr); a -h returns ErrHelp which we surface as 0.
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(stderr, "iac-codemod refactor-apply: at least one path is required")
		usage(stderr)
		return 2
	}
	report := &applyReport{}
	for _, path := range rest {
		if err := refactorApplyPath(path, opts, report); err != nil {
			fmt.Fprintf(stderr, "iac-codemod refactor-apply: %s: %v\n", path, err)
			return 1
		}
	}
	report.print(stdout, opts)
	if *reportFile != "" {
		var buf bytes.Buffer
		report.print(&buf, opts)
		if err := os.WriteFile(*reportFile, buf.Bytes(), 0o644); err != nil {
			fmt.Fprintf(stderr, "iac-codemod refactor-apply: write report-file %s: %v\n", *reportFile, err)
			return 1
		}
	}
	if len(report.errors) > 0 {
		return 1
	}
	return 0
}

// refactorApplyPath walks `path` for *.go files and processes each.
func refactorApplyPath(path string, opts *Options, report *applyReport) error {
	info, err := stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return fmt.Errorf("not a Go source file (or is a _test.go): %s", path)
		}
		if err := refactorApplyFile(path, opts, report); err != nil {
			report.errors = append(report.errors, fmt.Sprintf("%s: %v", path, err))
		}
		return nil
	}
	return filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			if shouldSkipDir(base) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		if err := refactorApplyFile(p, opts, report); err != nil {
			report.errors = append(report.errors, fmt.Sprintf("%s: %v", p, err))
		}
		return nil
	})
}

// refactorApplyFile parses `path`, classifies every Apply method, and
// (in -fix mode) mutates canonical bodies in place.
func refactorApplyFile(path string, opts *Options, report *applyReport) error {
	src, err := readFile(path)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return err
	}

	// Directory-wide method set (review round-1 finding #9).
	provs := planLikeReceiversInDir(filepath.Dir(path))
	if len(provs) == 0 {
		provs = planLikeReceivers(file)
	}
	// Directory-wide type-doc lookup (review round-6 finding #1) so
	// skip-marker on a sibling file's type declaration is honored.
	typeDocs := receiverTypeDocsInDir(filepath.Dir(path), file)

	mutated := false
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if !isProviderMethod(fn, "Apply", 2, 2) {
			continue
		}
		recv := receiverTypeName(fn)
		if !provs[recv] {
			continue
		}
		// Honor SkipMarker on fn.Doc OR receiver-type docs (review
		// round-1 finding #4).
		if hasSkipMarkerOn(fn.Doc) || typeDocs[recv].carriesMarker() {
			report.sites = append(report.sites, applySite{
				Path:     path,
				Line:     fset.Position(fn.Pos()).Line,
				Receiver: recv,
				Class:    applySkipped,
			})
			continue
		}
		class, offenderPos, suggestion := classifyApplyBody(fn, file, fset, path)
		site := applySite{
			Path:        path,
			Line:        fset.Position(fn.Pos()).Line,
			Receiver:    recv,
			Class:       class,
			OffenderPos: offenderPos,
			Suggestion:  suggestion,
		}
		if class == applyCanonical && opts != nil && opts.Fix {
			rewriteApplyBody(fn, file)
			mutated = true
			site.Rewrote = true
		}
		report.sites = append(report.sites, site)
	}

	if mutated && opts != nil && opts.Fix {
		ensureWfctlhelpersImport(file) // refactor-apply emits wfctlhelpers.ApplyPlan
		if err := writeFileAtomic(path, fset, file); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

// classifyApplyBody returns the disposition of fn's Apply body. If the
// body has any of the recognised non-canonical idioms, the offender's
// path:line and a hand-port suggestion are returned alongside the class
// label. The order of detection is intentional: the most-disruptive
// idiom (collapse) is reported first since it cannot be mechanically
// migrated, then upsert (which has a clean wfctlhelpers hook), then
// custom-error-wrapping. Multiple idioms in one body produce a single
// label; the report points at the first detected.
func classifyApplyBody(fn *ast.FuncDecl, file *ast.File, fset *token.FileSet, path string) (applyClassification, string, string) {
	if fn.Body == nil {
		return applyNonCanonicalOther, "", ""
	}
	if isAlreadyDelegatedApplyBody(fn.Body, file) {
		return applyAlreadyDelegated, "", ""
	}
	sw := findActionSwitch(fn.Body)
	if sw == nil {
		return applyMissingSwitch, "", "Apply body has no `switch action.Action` dispatch — wfctlhelpers.ApplyPlan expects this loop+switch shape; hand-port required."
	}
	// AWS update+replace collapse: any case clause with both "update"
	// and "replace" string literals.
	if pos := findUpdateReplaceCollapseCase(sw); pos.IsValid() {
		offender := fset.Position(pos)
		return applyUpdateReplaceCollapse, fmtPosShort(path, offender.Line), "manual port required: split `case \"update\", \"replace\":` into separate `update` and `replace` clauses (or rely on wfctlhelpers.ApplyPlan's doReplace semantic). The collapsed shape silently treats Replace as Update which loses the delete+create semantic for force-new fields."
	}
	// DO upsert recovery: errors.Is(err, ErrResourceAlreadyExists).
	if pos := findUpsertRecovery(sw); pos.IsValid() {
		offender := fset.Position(pos)
		return applyUpsertRecovery, fmtPosShort(path, offender.Line), "preserve via wfctlhelpers.ApplyPlan's upsertSupporter hook: drivers that support name-based discovery should implement `SupportsUpsert() bool` returning true; the helper handles ErrResourceAlreadyExists → Read+Update internally. Sample patch: keep the existing `upsertSupporter` interface declaration on the driver type, then delete the manual upsert branch from Apply."
	}
	// Custom error wrapping: a `case` body where err is reassigned via
	// fmt.Errorf with %w wrapping after a driver call.
	if pos := findCustomErrorWrap(sw); pos.IsValid() {
		offender := fset.Position(pos)
		return applyCustomErrorWrapping, fmtPosShort(path, offender.Line), "manual port required: wfctlhelpers.ApplyPlan does NOT expose a per-action error-wrap hook today (review round-1 finding #6: rev0 of this report named a fictional ApplyResultErrorHook / WrapActionError API). Two honest options: (a) preserve the domain-context wrap by adding `// wfctl:skip-iac-codemod` to the Apply method and keeping the manual switch; (b) move the wrap into the driver itself (Create/Update/Delete return the already-wrapped error) so wfctlhelpers' generic dispatcher records it verbatim. Option (b) is preferred because it survives any future migration."
	}
	// Heuristic: if the switch has the canonical create/update[/delete]
	// triple (plus optional separate replace), no non-canonical idiom
	// inside the switch, AND the surrounding Apply body matches the
	// canonical scaffold (result-init + range-loop + return), treat as
	// canonical. Round-5 finding #2: rev3 only verified the switch
	// shape — setup/teardown/custom result aggregation OUTSIDE the
	// switch was silently dropped on -fix.
	// Extract receiver + plan parameter identifier names so the
	// outer-shape and loop-body validators don't hardcode `p` /
	// `result` / `plan` (round-8 #5 + #9: providers using `res` /
	// `pl` / etc. were misclassified as non-canonical even though
	// rewriteApplyBody preserves custom names).
	recvName := ""
	if fn.Recv != nil && len(fn.Recv.List) > 0 && len(fn.Recv.List[0].Names) > 0 {
		recvName = fn.Recv.List[0].Names[0].Name
	}
	planName := ""
	if fn.Type.Params != nil && len(fn.Type.Params.List) >= 2 && len(fn.Type.Params.List[1].Names) >= 1 {
		planName = fn.Type.Params.List[1].Names[0].Name
	}
	if hasCanonicalCases(sw, recvName) && isCanonicalApplyOuterShape(fn.Body, recvName, planName) {
		return applyCanonical, "", ""
	}
	return applyNonCanonicalOther, "", "Apply outer shape (result-init + range-loop + return) or switch has unrecognised statements; review manually."
}

// isCanonicalApplyOuterShape returns true if fn.Body matches the
// canonical 3-statement scaffold around the action switch:
//
//  1. `<result-ident> := &ApplyResult{...}`
//  2. `for _, action := range <plan-ident>.Actions { ... }`
//  3. `return <result-ident>, nil`
//
// `recvName` is the receiver identifier (used by isCanonicalApplyLoopBody
// to validate the driver-lookup receiver is the provider).
// `planName` is the actual `plan` parameter name from the signature
// (round-8 #9: providers using `pl` etc. were misclassified).
//
// The accumulator-variable name is recovered from statement 1 and
// then required to match in statement 3, so any local convention
// (`result`, `res`, `out`) survives as long as it's consistent.
//
// Reject any deviation (extra setup, teardown, custom aggregation,
// trailing helper calls) so bespoke logic outside the switch is
// preserved as non-canonical (review round-5 #2).
func isCanonicalApplyOuterShape(body *ast.BlockStmt, recvName, planName string) bool {
	if body == nil || len(body.List) != 3 {
		return false
	}
	if planName == "" {
		planName = "plan"
	}
	// 1. <result-ident> := &ApplyResult{...} — recover the local
	// accumulator name so the canonical detector doesn't hardcode
	// "result" (round-8 #9).
	a, ok := body.List[0].(*ast.AssignStmt)
	if !ok || a.Tok != token.DEFINE || len(a.Lhs) != 1 || len(a.Rhs) != 1 {
		return false
	}
	resultIdent, ok := a.Lhs[0].(*ast.Ident)
	if !ok {
		return false
	}
	resultName := resultIdent.Name
	if resultName == "" || resultName == "_" {
		return false
	}
	un, ok := a.Rhs[0].(*ast.UnaryExpr)
	if !ok || un.Op != token.AND {
		return false
	}
	cl, ok := un.X.(*ast.CompositeLit)
	if !ok {
		return false
	}
	if !typeNameTailMatches(cl.Type, "ApplyResult") {
		return false
	}
	// 2. for _, action := range <planName>.Actions { ... }
	rng, ok := body.List[1].(*ast.RangeStmt)
	if !ok {
		return false
	}
	xSel, ok := rng.X.(*ast.SelectorExpr)
	if !ok || xSel.Sel.Name != "Actions" {
		return false
	}
	if planId, ok := xSel.X.(*ast.Ident); !ok || planId.Name != planName {
		return false
	}
	// Round-7 #3 + #9: validate the loop body is one of the recognised
	// canonical scaffolds. Pass the provider receiver name so the
	// driver-lookup check (round-8 #5) verifies <recvName>.ResourceDriver
	// rather than accepting any selector.
	if !isCanonicalApplyLoopBody(rng.Body, recvName, resultName) {
		return false
	}
	// 3. return <resultName>, nil
	ret, ok := body.List[2].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 2 {
		return false
	}
	if id, ok := ret.Results[0].(*ast.Ident); !ok || id.Name != resultName {
		return false
	}
	if id, ok := ret.Results[1].(*ast.Ident); !ok || id.Name != "nil" {
		return false
	}
	return true
}

// isCanonicalApplyLoopBody returns true if the for-loop body matches
// one of the canonical scaffolds. Round-7 #3 + #9: rev5 of
// isCanonicalApplyOuterShape only verified the outer 3 statements;
// any per-action logging/metrics/accumulators inside the for loop
// was silently dropped on -fix.
//
// Whitelist (every loop-body statement must match one of these):
//
//   - SwitchStmt with tag `<X>.Action` (the action dispatch). Exactly 1
//     such switch is required across the loop body.
//   - DeclStmt: `var out *ResourceOutput` (or qualified equivalent).
//   - AssignStmt: `<a>, err := <X>.ResourceDriver(...)` (driver lookup).
//   - IfStmt: `if err != nil { result.Errors = append(...); continue }`
//     OR `if out != nil { result.Resources = append(*out) }`
//
// Anything else (bare logging calls, metric increments, helper-call
// statements, alternate-driver lookup) rejects the canonical
// classification.
func isCanonicalApplyLoopBody(body *ast.BlockStmt, recvName, resultName string) bool {
	if body == nil {
		return false
	}
	switchCount := 0
	for _, stmt := range body.List {
		switch s := stmt.(type) {
		case *ast.SwitchStmt:
			switchCount++
			// (the switch body itself is validated by hasCanonicalCases
			// in classifyApplyBody before this function fires).
		case *ast.DeclStmt:
			if !isLocalOutPointerDecl(s) {
				return false
			}
		case *ast.AssignStmt:
			if !isCanonicalApplyLoopAssign(s, recvName) {
				return false
			}
		case *ast.IfStmt:
			if !isCanonicalApplyLoopIf(s, resultName) {
				return false
			}
		default:
			return false
		}
	}
	return switchCount == 1
}

// isCanonicalApplyLoopAssign returns true for the canonical loop-body
// AssignStmt shapes: `<a>, err := <recvName>.ResourceDriver(...)`. The
// receiver MUST be the provider's own receiver identifier (round-8 #5:
// rev3 accepted any `<x>.ResourceDriver(...)`, so `helper.ResourceDriver(...)`
// or `plan.ResourceDriver(...)` falsely classified as canonical).
func isCanonicalApplyLoopAssign(a *ast.AssignStmt, recvName string) bool {
	if len(a.Lhs) != 2 || len(a.Rhs) != 1 {
		return false
	}
	if id, ok := a.Lhs[1].(*ast.Ident); !ok || id.Name != "err" {
		return false
	}
	call, ok := a.Rhs[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	// Round-9 #2: only ResourceDriver is canonical. wfctlhelpers.ApplyPlan
	// dispatches through IaCProvider.ResourceDriver specifically — a
	// provider that wraps lookup in `Driver(...)` or `DriverFor(...)`
	// would have its wrapper bypassed on rewrite, which can change the
	// driver returned (caching, instrumentation, etc.).
	if sel.Sel.Name != "ResourceDriver" {
		return false
	}
	// Receiver must be the provider's own identifier.
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	if recvName != "" && x.Name == recvName {
		return true
	}
	// Tolerate the conventional `p` when recvName is empty/unknown.
	return recvName == "" && x.Name == "p"
}

// isCanonicalApplyLoopIf returns true for the canonical loop-body
// IfStmt shapes:
//
//   - `if err != nil { <resultName>.Errors = append(...); continue }`
//   - `if out != nil { <resultName>.Resources = append(...) }`
//
// `resultName` is the local accumulator identifier (recovered from the
// outer scaffold).
//
// Round-8 #8: rev6 of isCanonicalApplyLoopIfBodyStmt accepted a bare
// `continue`/`break` statement, but wfctlhelpers ALWAYS records an
// ActionError before continuing past a failure. So a guard like
// `if err != nil { continue }` (no append) would silently change
// behavior on rewrite. Now we require: when the guard body contains a
// continue/break, it MUST also contain an append-to-result statement.
func isCanonicalApplyLoopIf(ifs *ast.IfStmt, resultName string) bool {
	if ifs == nil {
		return false
	}
	be, ok := ifs.Cond.(*ast.BinaryExpr)
	if !ok || be.Op != token.NEQ {
		return false
	}
	id, ok := be.X.(*ast.Ident)
	if !ok || (id.Name != "err" && id.Name != "out") {
		return false
	}
	if rhs, ok := be.Y.(*ast.Ident); !ok || rhs.Name != "nil" {
		return false
	}
	if ifs.Else != nil {
		return false
	}
	hasAppend := false
	hasBranch := false
	for _, s := range ifs.Body.List {
		switch ss := s.(type) {
		case *ast.AssignStmt:
			if !isCanonicalAppendToResult(ss, resultName) {
				return false
			}
			hasAppend = true
		case *ast.BranchStmt:
			// Round-9 #1: only `continue` is canonical; `break`
			// silently aborts the loop on first error, but
			// wfctlhelpers.ApplyPlan records the error and KEEPS
			// processing later actions, so accepting `break` would
			// silently change behavior on rewrite.
			if ss.Tok != token.CONTINUE {
				return false
			}
			hasBranch = true
		default:
			return false
		}
	}
	// A bare continue/break (no append) is rejected — wfctlhelpers
	// always records the ActionError before continuing.
	if hasBranch && !hasAppend {
		return false
	}
	return true
}

// isCanonicalAppendToResult returns true if stmt is
// `<resultName>.<F> = append(...)`. Used inside loop-body if-guards
// (round-8 #8: tightened to require this shape, not just "any
// append").
func isCanonicalAppendToResult(s *ast.AssignStmt, resultName string) bool {
	if len(s.Lhs) != 1 || len(s.Rhs) != 1 || s.Tok != token.ASSIGN {
		return false
	}
	sel, ok := s.Lhs[0].(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if id, ok := sel.X.(*ast.Ident); !ok || id.Name != resultName {
		return false
	}
	call, ok := s.Rhs[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	idFn, ok := call.Fun.(*ast.Ident)
	if !ok || idFn.Name != "append" {
		return false
	}
	return true
}

// fmtPosShort renders a path:line short form for offender positions.
// Path is left as-supplied (caller provides the path the user gave).
func fmtPosShort(path string, line int) string {
	return fmt.Sprintf("%s:%d", path, line)
}

// isAlreadyDelegatedApplyBody returns true if fn.Body is a single
// `return <wfctlhelpers-alias>.ApplyPlan(...)`. Review round-4 finding
// #4: rev3 hardcoded the package identifier as `wfctlhelpers`. A
// provider that already delegates through an aliased import (e.g.
// `wf "github.com/.../wfctlhelpers"; return wf.ApplyPlan(...)`) was
// misreported as non-canonical. Resolves the import alias via
// pkgAliasFor so any aliased delegation is recognised.
func isAlreadyDelegatedApplyBody(body *ast.BlockStmt, file *ast.File) bool {
	if len(body.List) != 1 {
		return false
	}
	ret, ok := body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	call, ok := ret.Results[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	if sel.Sel.Name != "ApplyPlan" {
		return false
	}
	// Accept the literal default name OR the file's local alias for
	// the wfctlhelpers import path. Falls back to the literal name
	// when file is nil (test paths that don't pass it).
	wantAlias := pkgAliasFor(file, helperImportPath, "wfctlhelpers")
	return x.Name == wantAlias || x.Name == "wfctlhelpers"
}

// findActionSwitch returns the first switch statement whose tag is a
// SelectorExpr `<X>.Action` (canonical: `action.Action`). Only the
// outermost RangeStmt's body is searched: nested switches inside if
// branches are still matched by ast.Inspect, which is fine — the
// dispatch must be on `something.Action`.
func findActionSwitch(body *ast.BlockStmt) *ast.SwitchStmt {
	var found *ast.SwitchStmt
	ast.Inspect(body, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		sw, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		sel, ok := sw.Tag.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "Action" {
			found = sw
			return false
		}
		return true
	})
	return found
}

// findUpdateReplaceCollapseCase returns the position of the first case
// clause whose case-list literals include both "update" and "replace".
// Returns token.NoPos if no such collapse exists.
func findUpdateReplaceCollapseCase(sw *ast.SwitchStmt) token.Pos {
	for _, stmt := range sw.Body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		hasUpdate, hasReplace := false, false
		for _, expr := range cc.List {
			s, ok := stringLiteral(expr)
			if !ok {
				continue
			}
			switch s {
			case "update":
				hasUpdate = true
			case "replace":
				hasReplace = true
			}
		}
		if hasUpdate && hasReplace {
			return cc.Pos()
		}
	}
	return token.NoPos
}

// findUpsertRecovery returns the position of an `errors.Is(err, X)`
// call inside a case clause where X has the suffix `AlreadyExists`.
// Match is conservative: the receiver is `errors`, the selector is
// `Is`, and the second arg's name (or its selector tail) ends in
// "AlreadyExists". This catches both `ErrResourceAlreadyExists` and
// `interfaces.ErrResourceAlreadyExists`.
func findUpsertRecovery(sw *ast.SwitchStmt) token.Pos {
	var found token.Pos
	ast.Inspect(sw, func(n ast.Node) bool {
		if found.IsValid() {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok || x.Name != "errors" || sel.Sel.Name != "Is" {
			return true
		}
		if len(call.Args) < 2 {
			return true
		}
		if name := tailIdent(call.Args[1]); strings.HasSuffix(name, "AlreadyExists") {
			found = call.Pos()
			return false
		}
		return true
	})
	return found
}

// tailIdent returns the trailing identifier name of a SelectorExpr
// chain (or the bare ident name), or "" for unrecognised shapes.
func tailIdent(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	}
	return ""
}

// findCustomErrorWrap returns the position of an `err = fmt.Errorf(...,
// %w, err)` reassignment that wraps an existing error — i.e., the RHS
// fmt.Errorf call references the local `err` variable as one of its
// arguments. This is the bespoke domain-context wrapping pattern.
//
// The narrower-than-just-`err = fmt.Errorf(...)` shape is intentional:
// a `default:` case in the action switch often has `err = fmt.Errorf("unknown action %q", ...)`,
// which is a FRESH error for an unknown action, not a wrap of a driver
// error. wfctlhelpers' generic dispatcher already errors on unknown
// actions, so the codemod must NOT flag that benign case.
//
// Match shape: assignment whose LHS is `err` and whose RHS is a
// fmt.Errorf call where at least one arg is the identifier `err`.
func findCustomErrorWrap(sw *ast.SwitchStmt) token.Pos {
	var found token.Pos
	ast.Inspect(sw, func(n ast.Node) bool {
		if found.IsValid() {
			return false
		}
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		if len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		id, ok := assign.Lhs[0].(*ast.Ident)
		if !ok || id.Name != "err" {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if x.Name != "fmt" || sel.Sel.Name != "Errorf" {
			return true
		}
		// Must reference `err` somewhere in the args (the wrap target).
		// A fmt.Errorf for a fresh error doesn't pass `err`, so this
		// keeps the unknown-action default case clean.
		for _, a := range call.Args {
			if argId, ok := a.(*ast.Ident); ok && argId.Name == "err" {
				found = assign.Pos()
				return false
			}
		}
		return true
	})
	return found
}

// hasCanonicalCases returns true if the switch has at least the
// "create" + "update" cases (delete is conventional but optional in
// providers that don't support delete via Apply) AND every case body
// has only the canonical-shape statements (driver method calls,
// ResourceRef construction, simple if-guards on action.Current). The
// body-shape validation closes review round-1 finding #5: rev0 of this
// function only checked case labels, so extra bookkeeping or metrics
// inside a case body would still classify as canonical and get silently
// dropped during rewrite.
//
// Recognised case-body statement kinds (each maps to a known shape
// in wfctlhelpers' generic dispatcher):
//
//   - AssignStmt: `out, err = drv.Create(ctx, action.Resource)` /
//     `err = drv.Delete(ctx, ref)` / `ref := ResourceRef{...}` /
//     `ref.ProviderID = action.Current.ProviderID`
//   - IfStmt: only the `if action.Current != nil` ProviderID-set
//     guard pattern (cond is BinaryExpr NEQ on action.Current and nil)
//   - DeclStmt: `var out *ResourceOutput` (rare but legal; wfctlhelpers
//     handles its own out variable)
//
// Round-8 #4: rev3 of this function accepted `default:` clauses
// without inspecting their body. Logging/metrics/etc. in default
// silently dropped. Now default bodies are validated against the
// same shape: only AssignStmt of `err = fmt.Errorf(...)` (the
// canonical unknown-action error pattern) is allowed. Everything
// else (including bare logging) rejects.
//
// `recvName` is the provider receiver identifier — passed through to
// caseBodyIsCanonical → isCanonicalCaseAssign → isDriverMethodCall to
// validate driver-receiver names per the round-4 #3 fix.
func hasCanonicalCases(sw *ast.SwitchStmt, recvName string) bool {
	hasCreate, hasUpdate := false, false
	for _, stmt := range sw.Body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		labels := caseLabels(cc)
		isCanonicalLabel := false
		for _, l := range labels {
			switch l {
			case "create":
				hasCreate = true
				isCanonicalLabel = true
			case "update":
				hasUpdate = true
				isCanonicalLabel = true
			case "delete", "replace":
				isCanonicalLabel = true
			}
		}
		// `default:` (no labels) — round-8 #4: validate body matches
		// the canonical unknown-action error shape. Anything else
		// (logging, metrics, alternate side-effect) rejects.
		if len(labels) == 0 {
			if !isCanonicalDefaultBody(cc.Body) {
				return false
			}
			continue
		}
		if !isCanonicalLabel {
			return false
		}
		if !caseBodyIsCanonical(cc.Body) {
			return false
		}
	}
	return hasCreate && hasUpdate
}

// caseLabels returns the unquoted string-literal values of the case
// clause's case-list. A `default:` clause returns an empty slice.
// isCanonicalDefaultBody returns true if body matches the canonical
// `default:` clause shape: a single `err = fmt.Errorf("unknown action
// %q", ...)` assignment. Anything else (logging, metrics, alternate
// side-effects) rejects (round-8 #4).
func isCanonicalDefaultBody(body []ast.Stmt) bool {
	if len(body) != 1 {
		return false
	}
	a, ok := body[0].(*ast.AssignStmt)
	if !ok || a.Tok != token.ASSIGN || len(a.Lhs) != 1 || len(a.Rhs) != 1 {
		return false
	}
	if id, ok := a.Lhs[0].(*ast.Ident); !ok || id.Name != "err" {
		return false
	}
	call, ok := a.Rhs[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return x.Name == "fmt" && sel.Sel.Name == "Errorf"
}

func caseLabels(cc *ast.CaseClause) []string {
	var out []string
	for _, expr := range cc.List {
		if s, ok := stringLiteral(expr); ok {
			out = append(out, s)
		}
	}
	return out
}

// caseBodyIsCanonical returns true if every statement in body is in
// the recognised whitelist (driver call, ResourceRef construction,
// ProviderID guard). The whitelist is intentionally narrow so that
// bespoke statements (logging, metrics, alternate construction) cause
// rejection — the codemod errs on the side of NOT rewriting in
// ambiguous shapes.
func caseBodyIsCanonical(body []ast.Stmt) bool {
	for _, stmt := range body {
		if !canonicalCaseStmt(stmt) {
			return false
		}
	}
	return true
}

// canonicalCaseStmt returns true if stmt fits one of the canonical
// shapes inside an action-switch case body. The whitelist is
// intentionally narrow: any statement outside the recognised set
// (bookkeeping counters, map updates, accumulators, alternate calls)
// causes rejection. Review round-3 finding #5: rev2 of this function
// accepted ANY AssignStmt, so `createsTotal++` / `metrics[action.Action]++`
// / `result.Stats.Updates++` all passed and the bespoke logic was
// silently dropped during -fix.
//
// Recognised AssignStmt shapes:
//
//   - Multi-target call: `out, err = <X>.<METHOD>(...)` with X a Driver
//     identifier and METHOD in {Create, Read, Update, Delete}
//   - Single-target call: `err = <X>.<METHOD>(...)` (delete-style)
//   - Composite literal: `ref := <T>{...}` where T is ResourceRef-shaped
//   - Selector assignment: `<X>.<F> = <Y>.<G>` where the LHS is a known
//     ProviderID-style field (ProviderID, Name, Type)
//
// Recognised non-Assign shapes:
//
//   - if-guard: `if action.Current != nil { ... }` containing only
//     canonical shapes (recursion via isProviderIDGuard)
//   - var-decl: `var out *ResourceOutput`
func canonicalCaseStmt(stmt ast.Stmt) bool {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		return isCanonicalCaseAssign(s)
	case *ast.IfStmt:
		return isProviderIDGuard(s)
	case *ast.DeclStmt:
		// Only `var out *ResourceOutput` (or qualified equivalent).
		// Review round-4 finding #6: rev3 accepted ALL DeclStmts, so
		// `var x SomeBookkeepingType` declarations passed as canonical
		// and the bespoke local variable was silently dropped.
		return isLocalOutPointerDecl(s)
	}
	return false
}

// isLocalOutPointerDecl returns true if stmt is a single
// `var <name> *<ResourceOutput-shaped>` declaration. The name is not
// constrained (the standard convention is `out` but `o` / `result`
// are valid) but the type tail must be ResourceOutput.
func isLocalOutPointerDecl(s *ast.DeclStmt) bool {
	gd, ok := s.Decl.(*ast.GenDecl)
	if !ok || gd.Tok != token.VAR || len(gd.Specs) != 1 {
		return false
	}
	vs, ok := gd.Specs[0].(*ast.ValueSpec)
	if !ok || vs.Type == nil || len(vs.Names) != 1 {
		return false
	}
	star, ok := vs.Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	return typeNameTailMatches(star.X, "ResourceOutput")
}

// isCanonicalCaseAssign tightens the AssignStmt acceptance whitelist
// to known canonical shapes (round-3 #5).
func isCanonicalCaseAssign(a *ast.AssignStmt) bool {
	// Multi-target driver call: `out, err = <X>.<METHOD>(...)`.
	// Two LHS, one RHS that is a CallExpr on a SelectorExpr.
	if len(a.Lhs) == 2 && len(a.Rhs) == 1 {
		if isDriverMethodCall(a.Rhs[0]) {
			return true
		}
	}
	// Single-target driver call: `err = <X>.<METHOD>(...)`.
	if len(a.Lhs) == 1 && len(a.Rhs) == 1 {
		if isDriverMethodCall(a.Rhs[0]) {
			// LHS must be `err` — a different LHS would mean
			// custom variable bookkeeping.
			if id, ok := a.Lhs[0].(*ast.Ident); ok && id.Name == "err" {
				return true
			}
			return false
		}
		// Composite-literal `ref := ResourceRef{...}` ONLY. Review
		// round-4 finding #2: rev3 of this branch accepted any
		// composite literal, so a bookkeeping struct construction
		// (`payload := AuditPayload{...}`) was misclassified as
		// canonical and silently dropped. Now the literal type's
		// name (qualified or unqualified) must be ResourceRef.
		if a.Tok == token.DEFINE {
			if cl, ok := a.Rhs[0].(*ast.CompositeLit); ok && typeNameTailMatches(cl.Type, "ResourceRef") {
				return true
			}
		}
		// Selector assignment `<X>.<F> = <Y>` to a ResourceRef-style
		// field (ProviderID, Name, Type).
		if sel, ok := a.Lhs[0].(*ast.SelectorExpr); ok && a.Tok == token.ASSIGN {
			switch sel.Sel.Name {
			case "ProviderID", "Name", "Type":
				return true
			}
			return false
		}
	}
	return false
}

// isDriverMethodCall reports whether expr is a call to a Driver method
// (Create/Read/Update/Delete) where the receiver is a known
// driver-bound identifier. Review round-4 finding #3: rev3 of this
// function only checked the selector NAME, so any call like
// `helper.Update(...)` or `metrics.Delete(...)` was misclassified as
// canonical driver dispatch and the case body was rewritten away.
//
// The receiver allowlist is intentionally narrow: `d`, `drv`,
// `driver` are the canonical names produced by the standard
// `d, err := p.ResourceDriver(action.Resource.Type)` pattern (DO,
// AWS, GCP, Azure). Anything else falls outside the rewrite-safe
// shape and the case body is reported as non-canonical.
func isDriverMethodCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Create", "Read", "Update", "Delete":
		// fall through to receiver check
	default:
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	// Conservative driver-receiver allowlist. Round-5 finding #9: rev3
	// allowlist {d, drv, driver} missed `dr`, `rd`, `rdrv`, etc. Widen
	// to a slightly larger set of common single-/short-identifier names
	// while still rejecting bookkeeping-style receivers like `metrics`,
	// `audit`, `helper` (per round-4 #3 — that's the whole point of
	// the receiver check).
	switch x.Name {
	case "d", "dr", "drv", "rd", "rdrv", "driver", "resourceDriver":
		return true
	}
	return false
}

// isProviderIDGuard checks for the canonical
// `if action.Current != nil { ... }` guard. Permissive on the body
// since the inner statement is itself a canonical AssignStmt
// (`ref.ProviderID = action.Current.ProviderID`).
func isProviderIDGuard(ifs *ast.IfStmt) bool {
	be, ok := ifs.Cond.(*ast.BinaryExpr)
	if !ok || be.Op != token.NEQ {
		return false
	}
	xIsCurrent := false
	if sel, ok := be.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "Current" {
		xIsCurrent = true
	}
	yIsNil := false
	if id, ok := be.Y.(*ast.Ident); ok && id.Name == "nil" {
		yIsNil = true
	}
	if !(xIsCurrent && yIsNil) {
		// Allow the reverse order too (`nil != action.Current`),
		// though it's not idiomatic Go.
		yIsCurrent := false
		if sel, ok := be.Y.(*ast.SelectorExpr); ok && sel.Sel.Name == "Current" {
			yIsCurrent = true
		}
		xIsNil := false
		if id, ok := be.X.(*ast.Ident); ok && id.Name == "nil" {
			xIsNil = true
		}
		if !(yIsCurrent && xIsNil) {
			return false
		}
	}
	if ifs.Else != nil {
		return false
	}
	for _, s := range ifs.Body.List {
		if !canonicalCaseStmt(s) {
			return false
		}
	}
	return true
}

// stringLiteral returns the unquoted value of a BasicLit STRING
// expression, or ("", false) for any other shape.
func stringLiteral(expr ast.Expr) (string, bool) {
	bl, ok := expr.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	if len(bl.Value) < 2 {
		return "", false
	}
	// Strip surrounding quotes (single-line strings only).
	return bl.Value[1 : len(bl.Value)-1], true
}

// rewriteApplyBody replaces fn.Body with
// `return wfctlhelpers.ApplyPlan(<ctx>, <recv>, <plan>)`.
//
// Identifier recovery + injection (review round-1 #2, round-2 #4):
//
//   - Receiver: ensureReceiverName injects "p" if the receiver is
//     unnamed (`func (*Provider) Apply(...)`). rev1 fell back to a
//     hardcoded "p" without updating the receiver decl, so the
//     rewritten call referenced an undefined identifier.
//   - ctx: ensureCtxParamName renames `_` → `ctx`; preserves any other
//     non-blank name.
//   - plan: same shape as ctx, applied to the second parameter slot.
func rewriteApplyBody(fn *ast.FuncDecl, file *ast.File) {
	recvName := ensureReceiverName(fn, "p")
	ctxName := ensureCtxParamName(fn)
	planName := ensureNthParamName(fn, 1, "plan")
	// Resolve the wfctlhelpers package alias (review round-3 finding #6:
	// rev2 hardcoded "wfctlhelpers" but a file using
	// `wf "github.com/.../wfctlhelpers"` wouldn't compile).
	pkgAlias := pkgAliasFor(file, helperImportPath, "wfctlhelpers")

	call := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   ast.NewIdent(pkgAlias),
			Sel: ast.NewIdent("ApplyPlan"),
		},
		Args: []ast.Expr{
			ast.NewIdent(ctxName),
			ast.NewIdent(recvName),
			ast.NewIdent(planName),
		},
	}
	fn.Body = &ast.BlockStmt{
		List: []ast.Stmt{
			&ast.ReturnStmt{Results: []ast.Expr{call}},
		},
	}
}

// ensureNthParamName returns the name of fn's `idx`-th parameter,
// injecting `defaultName` (and renaming `_`) the same way
// ensureCtxParamName does for the first parameter. Used by
// rewriteApplyBody for the `plan` argument slot.
func ensureNthParamName(fn *ast.FuncDecl, idx int, defaultName string) string {
	if fn.Type.Params == nil || len(fn.Type.Params.List) <= idx {
		return defaultName
	}
	field := fn.Type.Params.List[idx]
	if len(field.Names) == 0 {
		field.Names = []*ast.Ident{ast.NewIdent(defaultName)}
		return defaultName
	}
	if len(field.Names) == 1 {
		n := field.Names[0].Name
		if n == "_" || n == "" {
			field.Names[0] = ast.NewIdent(defaultName)
			return defaultName
		}
		return n
	}
	if field.Names[0].Name != "" && field.Names[0].Name != "_" {
		return field.Names[0].Name
	}
	field.Names[0] = ast.NewIdent(defaultName)
	return defaultName
}

// (writeFileAtomic + ensureImport live in refactor_plan.go;
// refactor-apply reuses them via ensureWfctlhelpersImport.)

// ============================================================
// Report rendering
// ============================================================

func (r *applyReport) print(w io.Writer, opts *Options) {
	sort.Slice(r.sites, func(i, j int) bool {
		if r.sites[i].Path != r.sites[j].Path {
			return r.sites[i].Path < r.sites[j].Path
		}
		return r.sites[i].Line < r.sites[j].Line
	})
	fmt.Fprintln(w, "# iac-codemod refactor-apply report")
	fmt.Fprintln(w)
	mode := "dry-run"
	if opts != nil && opts.Fix {
		mode = "fix"
	}
	fmt.Fprintf(w, "Mode:    %s\n", mode)
	fmt.Fprintf(w, "Sites:   %d\n", len(r.sites))
	fmt.Fprintf(w, "Errors:  %d\n", len(r.errors))
	fmt.Fprintln(w)

	groups := map[applyClassification][]applySite{}
	order := []applyClassification{
		applyCanonical,
		applyUpsertRecovery,
		applyUpdateReplaceCollapse,
		applyCustomErrorWrapping,
		applyNonCanonicalOther,
		applyMissingSwitch,
		applyAlreadyDelegated,
		applySkipped,
	}
	for _, s := range r.sites {
		groups[s.Class] = append(groups[s.Class], s)
	}
	headers := map[applyClassification]string{
		applyCanonical:             "Canonical (rewrite candidate)",
		applyUpsertRecovery:        "Upsert recovery — DO-style ErrResourceAlreadyExists path",
		applyUpdateReplaceCollapse: "Update+replace collapse — manual port required",
		applyCustomErrorWrapping:   "Custom error wrapping — extension-point hook required",
		applyNonCanonicalOther:     "Non-canonical (manual review required)",
		applyMissingSwitch:         "Missing action-switch — hand-port required",
		applyAlreadyDelegated:      "Already-delegated (no-op)",
		applySkipped:               "Skipped (// wfctl:skip-iac-codemod)",
	}
	for _, c := range order {
		sites := groups[c]
		if len(sites) == 0 {
			continue
		}
		fmt.Fprintf(w, "## %s\n\n", headers[c])
		for _, s := range sites {
			suffix := ""
			if c == applyCanonical && s.Rewrote {
				suffix = " (rewritten)"
			}
			line := fmt.Sprintf("- %s:%d %s.Apply %s%s", s.Path, s.Line, s.Receiver, s.Class, suffix)
			if s.OffenderPos != "" {
				line += fmt.Sprintf(" (offender at %s)", s.OffenderPos)
			}
			fmt.Fprintln(w, line)
			if s.Suggestion != "" {
				fmt.Fprintf(w, "  - suggestion: %s\n", s.Suggestion)
			}
		}
		fmt.Fprintln(w)
	}

	if len(r.errors) > 0 {
		fmt.Fprintln(w, "## Errors")
		fmt.Fprintln(w)
		for _, e := range r.errors {
			fmt.Fprintf(w, "- %s\n", e)
		}
		fmt.Fprintln(w)
	}
}
