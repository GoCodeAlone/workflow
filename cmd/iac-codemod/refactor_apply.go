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
	// `iac-codemod -h` (T8.2 carry-forward #1).
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
			if base == "vendor" || base == "testdata" || (strings.HasPrefix(base, ".") && base != ".") {
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

	provs := planLikeReceivers(file)

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
		if hasSkipMarkerOn(fn.Doc) {
			report.sites = append(report.sites, applySite{
				Path:     path,
				Line:     fset.Position(fn.Pos()).Line,
				Receiver: recv,
				Class:    applySkipped,
			})
			continue
		}
		class, offenderPos, suggestion := classifyApplyBody(fn, fset, path)
		site := applySite{
			Path:        path,
			Line:        fset.Position(fn.Pos()).Line,
			Receiver:    recv,
			Class:       class,
			OffenderPos: offenderPos,
			Suggestion:  suggestion,
		}
		if class == applyCanonical && opts != nil && opts.Fix {
			rewriteApplyBody(fn)
			mutated = true
			site.Rewrote = true
		}
		report.sites = append(report.sites, site)
	}

	if mutated && opts != nil && opts.Fix {
		ensurePlanHelperImport(file) // shared with refactor-plan: idempotent if present
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
func classifyApplyBody(fn *ast.FuncDecl, fset *token.FileSet, path string) (applyClassification, string, string) {
	if fn.Body == nil {
		return applyNonCanonicalOther, "", ""
	}
	if isAlreadyDelegatedApplyBody(fn.Body) {
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
		return applyCustomErrorWrapping, fmtPosShort(path, offender.Line), "preserve domain-context wrapping by registering a post-action error hook on the provider (extension-point hook ApplyResultErrorHook). Sample: implement `WrapActionError(action, err) error` on the provider type; wfctlhelpers calls it before appending to ApplyResult.Errors. Without the hook, the helper records the raw driver error and the bespoke wrap is lost."
	}
	// Heuristic: if the switch has the canonical create/update[/delete]
	// triple (plus optional separate replace) and no detected non-canonical
	// idiom, treat as canonical.
	if hasCanonicalCases(sw) {
		return applyCanonical, "", ""
	}
	return applyNonCanonicalOther, "", "Apply switch has unrecognised case shape; review manually."
}

// fmtPosShort renders a path:line short form for offender positions.
// Path is left as-supplied (caller provides the path the user gave).
func fmtPosShort(path string, line int) string {
	return fmt.Sprintf("%s:%d", path, line)
}

// isAlreadyDelegatedApplyBody returns true if fn.Body is a single
// `return wfctlhelpers.ApplyPlan(...)`.
func isAlreadyDelegatedApplyBody(body *ast.BlockStmt) bool {
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
	return x.Name == "wfctlhelpers" && sel.Sel.Name == "ApplyPlan"
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
// providers that don't support delete via Apply). Replace as a
// separate case is allowed; collapse is filtered earlier.
func hasCanonicalCases(sw *ast.SwitchStmt) bool {
	hasCreate, hasUpdate := false, false
	for _, stmt := range sw.Body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, expr := range cc.List {
			s, ok := stringLiteral(expr)
			if !ok {
				continue
			}
			switch s {
			case "create":
				hasCreate = true
			case "update":
				hasUpdate = true
			}
		}
	}
	return hasCreate && hasUpdate
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

// rewriteApplyBody replaces fn.Body with `return wfctlhelpers.ApplyPlan(ctx, p, plan)`.
// Receiver name is recovered from fn.Recv.List[0].Names[0]; default "p".
// Both `ctx` and `plan` parameter names are recovered from the function
// signature so the rewrite compiles.
func rewriteApplyBody(fn *ast.FuncDecl) {
	recvName := "p"
	if len(fn.Recv.List) > 0 && len(fn.Recv.List[0].Names) > 0 {
		n := fn.Recv.List[0].Names[0].Name
		if n != "" && n != "_" {
			recvName = n
		}
	}
	// Recover or rename the ctx and plan param names so the substituted
	// call references real identifiers. Apply has 2 parameters in
	// position [ctx context.Context, plan *IaCPlan].
	ctxName := "ctx"
	planName := "plan"
	if fn.Type.Params != nil && len(fn.Type.Params.List) >= 1 {
		if len(fn.Type.Params.List[0].Names) == 1 {
			n := fn.Type.Params.List[0].Names[0].Name
			if n == "_" {
				fn.Type.Params.List[0].Names[0] = ast.NewIdent("ctx")
			} else if n != "" {
				ctxName = n
			}
		}
	}
	if fn.Type.Params != nil && len(fn.Type.Params.List) >= 2 {
		if len(fn.Type.Params.List[1].Names) == 1 {
			n := fn.Type.Params.List[1].Names[0].Name
			if n == "_" {
				fn.Type.Params.List[1].Names[0] = ast.NewIdent("plan")
			} else if n != "" {
				planName = n
			}
		}
	}

	call := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   ast.NewIdent("wfctlhelpers"),
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

// (writeFileAtomic + ensurePlanHelperImport live in refactor_plan.go;
// refactor-apply reuses them.)

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
