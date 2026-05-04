// Copyright (c) 2026 Jon Langevin
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
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
	modes["refactor-plan"] = runRefactorPlan
}

// helperImportPath is the canonical Go import path for the wfctlhelpers
// package (used by refactor-apply for ApplyPlan delegation). Any source
// file that gains a `wfctlhelpers.ApplyPlan` call must also import this
// package.
const helperImportPath = "github.com/GoCodeAlone/workflow/iac/wfctlhelpers"

// planHelperImportPath is the import path for platform.ComputePlan, the
// canonical Plan helper provider Plan() bodies delegate to. This is a
// rev1-review correction: the plan-doc named `wfctlhelpers.Plan` as the
// rewrite target, but no such API exists today in the repo. The actual
// Plan-equivalent helper is `platform.ComputePlan(ctx, p, desired, current)`
// at platform/differ.go:72. Switching the codemod target to the real API
// closes Copilot review finding #1 (lint.go:45 + refactor_plan.go:36):
// "the rewrite target does not exist in the repository today; rewritten
// files would fail to compile".
const planHelperImportPath = "github.com/GoCodeAlone/workflow/platform"

// planCanonicalCallExpr is the canonical replacement-body expression
// emitted by refactor-plan. Calls platform.ComputePlan (the real helper);
// see planHelperImportPath above for the review-correction rationale.
const planCanonicalCallExpr = "platform.ComputePlan(ctx, p, desired, current)"

// planClassification labels the disposition of a single Plan() method
// site. Each report entry carries one classification; the rewriter
// honors only `planCanonical`.
type planClassification int

const (
	// planCanonical: body matches the configHash-compare template; safe
	// to rewrite to wfctlhelpers.Plan.
	planCanonical planClassification = iota
	// planNonCanonical: body has out-of-template logic; report only,
	// never rewrite.
	planNonCanonical
	// planAlreadyDelegated: body is already `return wfctlhelpers.Plan(...)`;
	// report as no-op (idempotent), do NOT rewrite.
	planAlreadyDelegated
	// planSkipped: function carries the SkipMarker; report into the
	// Skipped section. (Distinct from the lint-mode skip path because
	// refactor-plan tracks skips per-site for the report.)
	planSkipped
)

// String renders the classification for the report. Lower-case so
// "non-canonical" / "canonical" read naturally inline.
func (c planClassification) String() string {
	switch c {
	case planCanonical:
		return "canonical"
	case planNonCanonical:
		return "non-canonical"
	case planAlreadyDelegated:
		return "already-delegated"
	case planSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

// planSite captures one Plan-method site in the report.
type planSite struct {
	Path     string
	Line     int
	Receiver string             // type name, e.g. "DOProvider"
	Class    planClassification // canonical / non-canonical / already-delegated / skipped
	Reason   string             // for non-canonical: why detection rejected the body
	Rewrote  bool               // set true if this site was rewritten on -fix
}

// planReport aggregates per-file results across an entire refactor-plan run.
type planReport struct {
	sites  []planSite
	errors []string
}

// runRefactorPlan is the entry point for the refactor-plan subcommand.
// It walks the supplied paths, classifies each Plan method site, and
// (when -fix is set) rewrites canonical bodies in place via atomic
// temp-file + rename.
func runRefactorPlan(args []string, opts *Options, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "iac-codemod refactor-plan: at least one path is required")
		usage(stderr)
		return 2
	}
	report := &planReport{}
	for _, path := range args {
		if err := refactorPlanPath(path, opts, report); err != nil {
			fmt.Fprintf(stderr, "iac-codemod refactor-plan: %s: %v\n", path, err)
			return 1
		}
	}
	report.print(stdout, opts)
	if len(report.errors) > 0 {
		return 1
	}
	return 0
}

// refactorPlanPath walks `path` for *.go files (excluding _test.go,
// vendor, testdata, hidden) and processes each. Per-file errors are
// recorded in the report so a single broken file does not abort the run.
func refactorPlanPath(path string, opts *Options, report *planReport) error {
	info, err := stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return fmt.Errorf("not a Go source file (or is a _test.go): %s", path)
		}
		if err := refactorPlanFile(path, opts, report); err != nil {
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
		if err := refactorPlanFile(p, opts, report); err != nil {
			report.errors = append(report.errors, fmt.Sprintf("%s: %v", p, err))
		}
		return nil
	})
}

// refactorPlanFile parses `path`, classifies every Plan method, and (in
// -fix mode) mutates the AST and writes the result atomically.
func refactorPlanFile(path string, opts *Options, report *planReport) error {
	src, err := readFile(path)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return err
	}

	// Build the receiver-shape filter using the directory-wide
	// method set so providers whose Plan/Apply live in sibling files
	// are still recognised (review round-1 finding #9). Per-file
	// fallback when the directory walk fails — keeps the rev0
	// behavior on isolated single-file targets.
	provs := planLikeReceiversInDir(filepath.Dir(path))
	if len(provs) == 0 {
		provs = planLikeReceivers(file)
	}
	typeDocs := receiverTypeDocs(file)

	mutated := false
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if !isProviderMethod(fn, "Plan", 3, 2) {
			continue
		}
		recv := receiverTypeName(fn)
		if !provs[recv] {
			// Method named Plan on a non-provider type — skip with no
			// report entry (lint already reports those if relevant; the
			// codemod focuses on rewriting providers).
			continue
		}
		// Honor the marker on the function doc, the receiver type's
		// TypeSpec doc, AND the wrapping GenDecl doc. Review round-1
		// finding #4: PR description promises type-doc + GenDecl-doc
		// honoring; rev0 only checked fn.Doc.
		if hasSkipMarkerOn(fn.Doc) || typeDocs[recv].carriesMarker() {
			report.sites = append(report.sites, planSite{
				Path:     path,
				Line:     fset.Position(fn.Pos()).Line,
				Receiver: recv,
				Class:    planSkipped,
			})
			continue
		}
		class, reason := classifyPlanBody(fn)
		site := planSite{
			Path:     path,
			Line:     fset.Position(fn.Pos()).Line,
			Receiver: recv,
			Class:    class,
			Reason:   reason,
		}
		if class == planCanonical && opts != nil && opts.Fix {
			rewritePlanBody(fn)
			mutated = true
			site.Rewrote = true
		}
		report.sites = append(report.sites, site)
	}

	if mutated && opts != nil && opts.Fix {
		// Ensure the platform import is present (refactor-plan emits
		// platform.ComputePlan). The function is idempotent.
		ensurePlatformImport(file)
		if err := writeFileAtomic(path, fset, file); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

// planLikeReceivers returns the set of receiver type names whose method
// set in `file` includes both Plan and Apply with shapes matching
// IaCProvider. Used as a fallback path when no package context is
// available; production callers should prefer planLikeReceiversInDir
// (review round-1 finding #9: rev0 of this function only consulted
// the current file, missing providers whose Plan and Apply live in
// sibling files).
func planLikeReceivers(file *ast.File) map[string]bool {
	methodsByRecv := make(map[string][]*ast.FuncDecl)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		recv := receiverTypeName(fn)
		if recv == "" {
			continue
		}
		methodsByRecv[recv] = append(methodsByRecv[recv], fn)
	}
	out := make(map[string]bool)
	for recv, methods := range methodsByRecv {
		if looksLikeProvider(methods) {
			out[recv] = true
		}
	}
	return out
}

// planLikeReceiversInDir returns the set of receiver type names whose
// method set across ALL non-test .go files in dir includes both Plan
// and Apply (canonical IaCProvider shape). Closes review round-1
// finding #9: a provider whose Plan() and Apply() live in sibling
// files (e.g. provider_plan.go + provider_apply.go) is invisible to
// the per-file planLikeReceivers helper. Per-directory aggregation
// matches Go's package-scoped method-set semantics.
//
// Errors are tolerated: any file whose parser.ParseFile call fails is
// silently dropped from the aggregation. The intent is to widen the
// detection net, not to enforce package-correctness (which is the
// linter's job).
func planLikeReceiversInDir(dir string) map[string]bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	methodsByRecv := make(map[string][]*ast.FuncDecl)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fpath := filepath.Join(dir, name)
		src, err := readFile(fpath)
		if err != nil {
			continue
		}
		fs := token.NewFileSet()
		file, err := parser.ParseFile(fs, fpath, src, parser.ParseComments)
		if err != nil {
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			recv := receiverTypeName(fn)
			if recv == "" {
				continue
			}
			methodsByRecv[recv] = append(methodsByRecv[recv], fn)
		}
	}
	out := make(map[string]bool)
	for recv, methods := range methodsByRecv {
		if looksLikeProvider(methods) {
			out[recv] = true
		}
	}
	return out
}

// receiverDoc captures the documentation positions where a skip marker
// could be placed for a given receiver type: the inner TypeSpec.Doc
// (between `type` and the type name) and the wrapping GenDecl.Doc
// (before the `type` keyword). hasSkipMarkerOn handles nil so the
// call site can pass either field unconditionally.
type receiverDoc struct {
	TypeSpecDoc *ast.CommentGroup
	GenDeclDoc  *ast.CommentGroup
}

func (d receiverDoc) carriesMarker() bool {
	return hasSkipMarkerOn(d.TypeSpecDoc) || hasSkipMarkerOn(d.GenDeclDoc)
}

// receiverTypeDocs returns a map from receiver type name to its
// associated documentation positions. Used by refactor-plan and
// refactor-apply to check the SkipMarker at type-doc and GenDecl-doc
// levels in addition to the function-doc level (review round-1
// finding #4).
func receiverTypeDocs(file *ast.File) map[string]receiverDoc {
	out := make(map[string]receiverDoc)
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			out[ts.Name.Name] = receiverDoc{
				TypeSpecDoc: ts.Doc,
				GenDeclDoc:  gd.Doc,
			}
		}
	}
	return out
}

// classifyPlanBody inspects the body of a Plan method and returns its
// classification + (when non-canonical) a short reason. Detection is
// purely structural and conservative: only bodies that match the
// configHash-compare template are returned as canonical; anything else
// — including bodies that are MOSTLY canonical but have an extra
// statement — is reported as non-canonical. The conservative bias is
// intentional: a false-canonical risks silently dropping bespoke logic
// during rewrite, whereas a false-non-canonical merely surfaces a
// finding the maintainer can review and either skip-mark or hand-port.
func classifyPlanBody(fn *ast.FuncDecl) (planClassification, string) {
	if fn.Body == nil {
		return planNonCanonical, "missing body"
	}
	// Already-delegated: single statement `return wfctlhelpers.Plan(...)`.
	if isAlreadyDelegatedPlanBody(fn.Body) {
		return planAlreadyDelegated, ""
	}
	// Canonical: body matches the configHash-compare template.
	if isCanonicalPlanBody(fn.Body) {
		return planCanonical, ""
	}
	return planNonCanonical, "Plan body does not match configHash-compare template"
}

// isAlreadyDelegatedPlanBody returns true if the body is a single
// `return platform.ComputePlan(...)` statement. The argument list is not
// inspected: any prior migration that already routed to the helper is
// considered done and idempotent. Legacy `wfctlhelpers.Plan(...)` bodies
// (the planned-but-not-shipped API) are also accepted as already-delegated
// so a maintainer who hand-applied an earlier rev of this codemod isn't
// re-rewritten on the next pass.
func isAlreadyDelegatedPlanBody(body *ast.BlockStmt) bool {
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
	switch {
	case x.Name == "platform" && sel.Sel.Name == "ComputePlan":
		return true
	case x.Name == "wfctlhelpers" && sel.Sel.Name == "Plan":
		// Legacy target. Recognised so re-runs are idempotent if any
		// pre-rev1 fixtures linger in the workspace.
		return true
	}
	return false
}

// isCanonicalPlanBody recognizes the configHash-compare template. The
// shape we accept (fuzzy on whitespace + identifier choice but tight on
// semantic structure):
//
//  1. An assignment building a name->state map: `<X> := make(map[string]<T>, len(current))`.
//  2. A range over `current` populating that map.
//  3. A composite-literal assignment building a `*<TypeName>{...}` plan
//     (any IaCPlan-shaped struct).
//  4. A range over `desired` whose body appends `Action: "create"` or
//     `Action: "update"` to plan.Actions, with the update branch gated
//     on `configHash(...) != configHash(...)`.
//  5. A final `return plan, nil`.
//
// This is intentionally tighter than "first-pass heuristic" — review
// round 0 finding (anticipated): a too-loose canonical detector silently
// rewrites bespoke planners that happen to share keywords.
func isCanonicalPlanBody(body *ast.BlockStmt) bool {
	stmts := body.List

	// Skip leading comment-only statements (none in Go AST: comments are
	// CommentGroup-attached, not statements). So we proceed directly.

	// 1. currentByName := make(map[string]...)
	idx := 0
	if idx >= len(stmts) {
		return false
	}
	if !isMapMakeAssign(stmts[idx]) {
		return false
	}
	idx++

	// 2. range over `current`
	if idx >= len(stmts) {
		return false
	}
	if !isRangeOverIdent(stmts[idx], "current") {
		return false
	}
	idx++

	// 3. plan composite literal assignment
	if idx >= len(stmts) {
		return false
	}
	if !isPlanCompositeAssign(stmts[idx]) {
		return false
	}
	idx++

	// 4. range over `desired` whose body has create + configHash-gated update
	if idx >= len(stmts) {
		return false
	}
	rng, ok := stmts[idx].(*ast.RangeStmt)
	if !ok {
		return false
	}
	xIdent, ok := rng.X.(*ast.Ident)
	if !ok || xIdent.Name != "desired" {
		return false
	}
	if !rangeBodyMatchesCanonicalDesired(rng.Body) {
		return false
	}
	idx++

	// 5. return plan, nil
	if idx >= len(stmts) {
		return false
	}
	ret, ok := stmts[idx].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 2 {
		return false
	}
	idx++

	// Trailing junk → reject.
	return idx == len(stmts)
}

// isMapMakeAssign matches `<X> := make(map[string]<T>, ...)`.
func isMapMakeAssign(stmt ast.Stmt) bool {
	a, ok := stmt.(*ast.AssignStmt)
	if !ok || a.Tok != token.DEFINE || len(a.Rhs) != 1 {
		return false
	}
	call, ok := a.Rhs[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name != "make" {
		return false
	}
	if len(call.Args) < 1 {
		return false
	}
	_, ok = call.Args[0].(*ast.MapType)
	return ok
}

// isRangeOverIdent matches `for ..., ... := range <name> { ... }`.
func isRangeOverIdent(stmt ast.Stmt, name string) bool {
	rng, ok := stmt.(*ast.RangeStmt)
	if !ok {
		return false
	}
	id, ok := rng.X.(*ast.Ident)
	if !ok {
		return false
	}
	return id.Name == name
}

// isPlanCompositeAssign matches `plan := &<TypeName>{...}`.
func isPlanCompositeAssign(stmt ast.Stmt) bool {
	a, ok := stmt.(*ast.AssignStmt)
	if !ok || a.Tok != token.DEFINE || len(a.Lhs) != 1 || len(a.Rhs) != 1 {
		return false
	}
	if id, ok := a.Lhs[0].(*ast.Ident); !ok || id.Name != "plan" {
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
	_ = cl
	return true
}

// rangeBodyMatchesCanonicalDesired verifies the body of the
// range-over-desired loop is EXACTLY the configHash-compare template:
//
//  1. lookup statement (`cur, exists := <X>[<key>]`)
//  2. `if !exists { ...append create... }` (the body must not return
//     anything bespoke — only the create-action append + continue/break
//     control flow)
//  3. `if configHash(...) != configHash(...) { ...append update... }`
//
// Reject any statement that doesn't fit these three slots — bespoke
// telemetry, metrics, alternate construction, etc. — to keep the canonical
// detector tight (review round-1 finding #3: a too-loose detector
// silently rewrites bespoke planners that happen to share keywords).
//
// Top-level statement count must be exactly 3; the second-and-third
// must be the !exists guard and configHash guard respectively. The
// lookup statement may be assignment-style (`:=`) or simple-assign
// (`=`) — both are valid Go.
func rangeBodyMatchesCanonicalDesired(body *ast.BlockStmt) bool {
	stmts := body.List
	if len(stmts) != 3 {
		return false
	}
	// 1. lookup `<a>, <b> := <map>[<key>]` or single-target equivalent.
	a, ok := stmts[0].(*ast.AssignStmt)
	if !ok || (a.Tok != token.DEFINE && a.Tok != token.ASSIGN) {
		return false
	}
	if len(a.Lhs) != 2 || len(a.Rhs) != 1 {
		return false
	}
	if _, isIndex := a.Rhs[0].(*ast.IndexExpr); !isIndex {
		return false
	}
	// 2. !exists guard.
	notExists, ok := stmts[1].(*ast.IfStmt)
	if !ok {
		return false
	}
	u, ok := notExists.Cond.(*ast.UnaryExpr)
	if !ok || u.Op != token.NOT {
		return false
	}
	if id, ok := u.X.(*ast.Ident); !ok || id.Name != "exists" {
		return false
	}
	if notExists.Else != nil {
		return false // else-branch means out-of-template logic
	}
	// 3. configHash != configHash guard.
	hashGuard, ok := stmts[2].(*ast.IfStmt)
	if !ok {
		return false
	}
	be, ok := hashGuard.Cond.(*ast.BinaryExpr)
	if !ok || be.Op != token.NEQ {
		return false
	}
	if !isConfigHashCall(be.X) || !isConfigHashCall(be.Y) {
		return false
	}
	if hashGuard.Else != nil {
		return false
	}
	return true
}

// isConfigHashCall reports whether expr is a call to the unexported
// `configHash` function: `configHash(<arg>)`. Used to recognise the
// configHash-compare guard inside the canonical Plan template.
func isConfigHashCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok {
		return false
	}
	return id.Name == "configHash"
}

// rewritePlanBody replaces the entire body of fn with a single
// `return platform.ComputePlan(<ctx>, <recv>, desired, current)`
// statement. The receiver identifier and ctx parameter name are
// recovered from the function signature so the rewrite compiles
// regardless of the original author's naming choice:
//
//   - blank `_` ctx parameters are renamed to `ctx` (standard idiom);
//   - non-blank ctx parameters keep their original name and the call
//     references that name (review round-1 finding #2: not just blank
//     idents need handling — if the maintainer wrote `c context.Context`
//     the rewrite must reference `c`, not an undefined `ctx`).
//   - the receiver identifier is recovered from fn.Recv.List[0].Names[0].
func rewritePlanBody(fn *ast.FuncDecl) {
	// Recover receiver identifier (default "p" if not declared).
	recvName := "p"
	if len(fn.Recv.List) > 0 && len(fn.Recv.List[0].Names) > 0 {
		n := fn.Recv.List[0].Names[0].Name
		if n != "" && n != "_" {
			recvName = n
		}
	}

	// Recover or rename the ctx (first) parameter so the rewrite
	// references a real identifier. Blank `_` is renamed to "ctx";
	// any other non-empty name is preserved as-is.
	ctxName := "ctx"
	if fn.Type.Params != nil && len(fn.Type.Params.List) >= 1 {
		first := fn.Type.Params.List[0]
		if len(first.Names) == 1 {
			n := first.Names[0].Name
			if n == "_" || n == "" {
				first.Names[0] = ast.NewIdent("ctx")
			} else {
				ctxName = n
			}
		}
	}

	call := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   ast.NewIdent("platform"),
			Sel: ast.NewIdent("ComputePlan"),
		},
		Args: []ast.Expr{
			ast.NewIdent(ctxName),
			ast.NewIdent(recvName),
			ast.NewIdent("desired"),
			ast.NewIdent("current"),
		},
	}
	fn.Body = &ast.BlockStmt{
		List: []ast.Stmt{
			&ast.ReturnStmt{Results: []ast.Expr{call}},
		},
	}
}

// ensureImport adds an ImportSpec for `path` if one is not already
// present. Returns true if an import was added.
func ensureImport(file *ast.File, path string) bool {
	for _, imp := range file.Imports {
		if imp.Path == nil {
			continue
		}
		// Path.Value includes the surrounding quotes.
		v := strings.Trim(imp.Path.Value, `"`)
		if v == path {
			return false
		}
	}
	newImport := &ast.ImportSpec{
		Path: &ast.BasicLit{Kind: token.STRING, Value: `"` + path + `"`},
	}
	// Locate the first import GenDecl; append a spec to it. If no
	// import block exists, prepend a new one to the file decls.
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		gd.Specs = append(gd.Specs, newImport)
		// Force parens so multi-spec rendering stays lexically valid.
		if !gd.Lparen.IsValid() {
			gd.Lparen = gd.Pos()
			gd.Rparen = gd.End()
		}
		return true
	}
	gd := &ast.GenDecl{
		Tok:    token.IMPORT,
		Lparen: token.NoPos,
		Specs:  []ast.Spec{newImport},
	}
	file.Decls = append([]ast.Decl{gd}, file.Decls...)
	return true
}

// ensurePlatformImport adds a `platform` import to file if absent;
// idempotent. Used by refactor-plan rewrites which substitute
// platform.ComputePlan calls.
func ensurePlatformImport(file *ast.File) bool {
	return ensureImport(file, planHelperImportPath)
}

// ensureWfctlhelpersImport adds a `wfctlhelpers` import to file if
// absent; idempotent. Used by refactor-apply rewrites which substitute
// wfctlhelpers.ApplyPlan calls.
func ensureWfctlhelpersImport(file *ast.File) bool {
	return ensureImport(file, helperImportPath)
}

// writeFileAtomic prints `file` to a temp sibling and renames it over
// `path`. The two-step write protects against partial writes on crash:
// either the destination contains the full new contents or it remains
// unchanged.
func writeFileAtomic(path string, fset *token.FileSet, file *ast.File) error {
	var buf bytes.Buffer
	// format.Node produces gofmt-canonical output (the same algorithm
	// `go fmt` uses), which keeps the rewrite indistinguishable from a
	// hand-formatted file. Plain printer.Fprint produces tab-aligned
	// columns that drift from gofmt output and would look like
	// codemod-touched files in code review.
	if err := format.Node(&buf, fset, file); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".codemod-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		// Best-effort cleanup if rename fails.
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// print renders the report. Findings/sites are sorted by file, line so
// output is deterministic across runs.
func (r *planReport) print(w io.Writer, opts *Options) {
	sort.Slice(r.sites, func(i, j int) bool {
		if r.sites[i].Path != r.sites[j].Path {
			return r.sites[i].Path < r.sites[j].Path
		}
		return r.sites[i].Line < r.sites[j].Line
	})

	fmt.Fprintln(w, "# iac-codemod refactor-plan report")
	fmt.Fprintln(w)
	mode := "dry-run"
	if opts != nil && opts.Fix {
		mode = "fix"
	}
	fmt.Fprintf(w, "Mode:    %s\n", mode)
	fmt.Fprintf(w, "Sites:   %d\n", len(r.sites))
	fmt.Fprintf(w, "Errors:  %d\n", len(r.errors))
	fmt.Fprintln(w)

	if len(r.sites) > 0 {
		// Group by classification for readability.
		var canonical, nonCanonical, alreadyDelegated, skipped []planSite
		for _, s := range r.sites {
			switch s.Class {
			case planCanonical:
				canonical = append(canonical, s)
			case planNonCanonical:
				nonCanonical = append(nonCanonical, s)
			case planAlreadyDelegated:
				alreadyDelegated = append(alreadyDelegated, s)
			case planSkipped:
				skipped = append(skipped, s)
			}
		}
		printSitesSection(w, "Canonical (rewrite candidate)", canonical, true)
		printSitesSection(w, "Non-canonical (manual review required)", nonCanonical, false)
		printSitesSection(w, "Already-delegated (no-op)", alreadyDelegated, false)
		printSitesSection(w, "Skipped (// wfctl:skip-iac-codemod)", skipped, false)
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

// printSitesSection renders one classification group.
func printSitesSection(w io.Writer, header string, sites []planSite, showRewrite bool) {
	if len(sites) == 0 {
		return
	}
	fmt.Fprintf(w, "## %s\n\n", header)
	for _, s := range sites {
		suffix := ""
		if showRewrite && s.Rewrote {
			suffix = " (rewritten)"
		}
		if s.Reason != "" {
			fmt.Fprintf(w, "- %s:%d %s.Plan %s — %s%s\n", s.Path, s.Line, s.Receiver, s.Class, s.Reason, suffix)
		} else {
			fmt.Fprintf(w, "- %s:%d %s.Plan %s%s\n", s.Path, s.Line, s.Receiver, s.Class, suffix)
		}
	}
	fmt.Fprintln(w)
}
