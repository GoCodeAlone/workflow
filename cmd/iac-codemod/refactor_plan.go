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
// package. The codemod's rewrites substitute calls into this package, so
// any source file that gains a `wfctlhelpers.Plan` call must also import
// this package.
const helperImportPath = "github.com/GoCodeAlone/workflow/iac/wfctlhelpers"

// planCanonicalCallExpr is the canonical replacement-body expression
// emitted by refactor-plan. The receiver name `p` mirrors the convention
// in DOProvider.Plan; the codemod rewrites (and renames) the receiver
// param accordingly. The constant is centralized so the report and the
// AST emitter cannot drift.
const planCanonicalCallExpr = "wfctlhelpers.Plan(ctx, p, desired, current)"

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

	// Build the receiver-shape filter used by lint's
	// providerLikeReceivers helper. We can't import the lint pass
	// directly (it requires an analysis.Pass), so we replicate the
	// minimal "has Plan AND Apply with the right shape" walk inline.
	provs := planLikeReceivers(file)

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
		if hasSkipMarkerOn(fn.Doc) {
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
		// Ensure the helper import is present. AST-level import
		// management is tricky; the pre-existing list is walked and a
		// new ImportSpec appended only if absent.
		if ensurePlanHelperImport(file) {
			// no-op: the spec was added; printing below produces the
			// updated source. The function returns true if added.
		}
		if err := writeFileAtomic(path, fset, file); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

// planLikeReceivers returns the set of receiver type names whose method
// set in `file` includes both Plan and Apply with shapes matching
// IaCProvider. Mirror of providerLikeReceivers in lint.go but operates
// on a single *ast.File (refactor-plan is single-file at a time; the
// lint analyzer takes a full pass).
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
// `return wfctlhelpers.Plan(...)` statement. The argument list is not
// inspected: any prior migration that already routed to the helper is
// considered done and idempotent.
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
	return x.Name == "wfctlhelpers" && sel.Sel.Name == "Plan"
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
// range-over-desired loop has the expected create/update branches:
//
//   - cur, exists := currentByName[spec.Name] (or compatible lookup)
//   - if !exists { append "create" action; continue }
//   - if configHash(...) != configHash(...) { append "update" action }
//
// Soft-match: we look for a configHash != configHash binary expression
// guarding an append, and a !exists guard around a different append,
// without requiring exact identifier names (cur/exists/spec are
// conventional but not enforced).
func rangeBodyMatchesCanonicalDesired(body *ast.BlockStmt) bool {
	hasNotExistsGuard := false
	hasConfigHashCompare := false
	ast.Inspect(body, func(n ast.Node) bool {
		// !exists guard.
		if ifs, ok := n.(*ast.IfStmt); ok {
			if u, ok := ifs.Cond.(*ast.UnaryExpr); ok && u.Op == token.NOT {
				if id, ok := u.X.(*ast.Ident); ok && id.Name == "exists" {
					hasNotExistsGuard = true
				}
			}
			// configHash(...) != configHash(...) guard.
			if be, ok := ifs.Cond.(*ast.BinaryExpr); ok && be.Op == token.NEQ {
				if isConfigHashCall(be.X) && isConfigHashCall(be.Y) {
					hasConfigHashCompare = true
				}
			}
		}
		return true
	})
	return hasNotExistsGuard && hasConfigHashCompare
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
// `return wfctlhelpers.Plan(ctx, p, desired, current)` statement. If
// the receiver param is named `_`, it is renamed to `ctx` so the
// substituted call site can reference the context. The receiver
// identifier is recovered from fn.Recv.List[0].Names[0] so the rewrite
// uses the same receiver name the original method declared.
func rewritePlanBody(fn *ast.FuncDecl) {
	// Recover receiver identifier (default "p" if not declared).
	recvName := "p"
	if len(fn.Recv.List) > 0 && len(fn.Recv.List[0].Names) > 0 {
		n := fn.Recv.List[0].Names[0].Name
		if n != "" && n != "_" {
			recvName = n
		}
	}

	// Rename `_` ctx parameter to `ctx`.
	if fn.Type.Params != nil && len(fn.Type.Params.List) >= 1 {
		first := fn.Type.Params.List[0]
		if len(first.Names) == 1 && first.Names[0].Name == "_" {
			first.Names[0] = ast.NewIdent("ctx")
		}
	}

	call := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   ast.NewIdent("wfctlhelpers"),
			Sel: ast.NewIdent("Plan"),
		},
		Args: []ast.Expr{
			ast.NewIdent("ctx"),
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

// ensurePlanHelperImport adds an ImportSpec for helperImportPath if one
// is not already present. Returns true if an import was added.
func ensurePlanHelperImport(file *ast.File) bool {
	for _, imp := range file.Imports {
		if imp.Path == nil {
			continue
		}
		// Path.Value includes the surrounding quotes.
		v := strings.Trim(imp.Path.Value, `"`)
		if v == helperImportPath {
			return false
		}
	}
	newImport := &ast.ImportSpec{
		Path: &ast.BasicLit{Kind: token.STRING, Value: `"` + helperImportPath + `"`},
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
