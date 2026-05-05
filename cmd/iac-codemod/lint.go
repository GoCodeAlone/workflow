// Copyright (c) 2026 Jon Langevin
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"
)

// osStat / osReadFile are direct stdlib bindings that the var indirections
// `stat` and `readFile` point at by default. The indirection is in place
// so future tests could substitute in-memory filesystems without
// touching disk.
func osStat(p string) (os.FileInfo, error) { return os.Stat(p) }
func osReadFile(p string) ([]byte, error)  { return os.ReadFile(p) }

func init() {
	modes["lint"] = runLint
}

// AssertPlanDelegatesToHelper flags any provider type's Plan method
// whose body does NOT call platform.ComputePlan (the canonical Plan
// helper at platform/differ.go:72). Legacy `wfctlhelpers.Plan(...)` calls
// are also accepted as delegated, for forward-compatibility with rev0
// of this analyzer when the rewrite target was still misnamed. See
// refactor_plan.go's planHelperImportPath docstring for the rev1
// review-correction history (Copilot review finding #1).
//
// The check is syntactic — it matches the SelectorExpr regardless of
// whether the call resolves at type-check time, so it works on plugins
// that have not yet vendored the helper.
var AssertPlanDelegatesToHelper = &analysis.Analyzer{
	Name: "AssertPlanDelegatesToHelper",
	Doc:  "Provider Plan() must delegate to platform.ComputePlan.",
	Run:  runAssertPlanDelegatesToHelper,
}

// AssertApplyDelegatesToHelper flags any provider type's Apply method whose
// body does NOT call wfctlhelpers.ApplyPlan. The canonical migration target
// is `return wfctlhelpers.ApplyPlan(ctx, p, plan)`. Same syntactic-match
// approach as AssertPlanDelegatesToHelper.
var AssertApplyDelegatesToHelper = &analysis.Analyzer{
	Name: "AssertApplyDelegatesToHelper",
	Doc:  "Provider Apply() must delegate to wfctlhelpers.ApplyPlan.",
	Run:  runAssertApplyDelegatesToHelper,
}

// AssertDiffSetsNeedsReplaceForForceNew flags any driver Diff method that
// references a ForceNew field (typically FieldChange.ForceNew) but never
// assigns NeedsReplace = true (typically DiffResult.NeedsReplace). This is
// the W-3 contract: when a force-new field changes, the diff must signal
// replacement so platform.ComputePlan classifies the action correctly.
var AssertDiffSetsNeedsReplaceForForceNew = &analysis.Analyzer{
	Name: "AssertDiffSetsNeedsReplaceForForceNew",
	Doc:  "Driver Diff() that observes ForceNew fields must set DiffResult.NeedsReplace=true.",
	Run:  runAssertDiffSetsNeedsReplaceForForceNew,
}

// AssertProviderImplementsValidatePlan flags any provider-shaped type
// (a type with Plan + Apply methods matching the IaCProvider signature)
// that does NOT also have a ValidatePlan method satisfying the
// ProviderValidator interface (`ValidatePlan(plan *IaCPlan) []PlanDiagnostic`).
// The check uses pass.TypesInfo to verify method-set membership rather
// than raw AST string-match per team-lead's W-8 brief.
var AssertProviderImplementsValidatePlan = &analysis.Analyzer{
	Name: "AssertProviderImplementsValidatePlan",
	Doc:  "Provider type must implement ProviderValidator (ValidatePlan method).",
	Run:  runAssertProviderImplementsValidatePlan,
}

// lintAnalyzers is the canonical ordered list of T8.2 analyzers. Order
// is preserved in the report so output is deterministic across runs.
var lintAnalyzers = []*analysis.Analyzer{
	AssertPlanDelegatesToHelper,
	AssertApplyDelegatesToHelper,
	AssertDiffSetsNeedsReplaceForForceNew,
	AssertProviderImplementsValidatePlan,
}

// lintFinding captures one analyzer diagnostic for the report.
type lintFinding struct {
	Path     string
	Line     int
	Analyzer string
	Message  string
}

// skippedSite captures one declaration suppressed by SkipMarker.
type skippedSite struct {
	Path     string
	Line     int
	Analyzer string
	Decl     string // function or type name
}

// lintReport aggregates findings, skipped sites, and per-file errors
// across an entire lint run.
type lintReport struct {
	findings []lintFinding
	skipped  []skippedSite
	errors   []string
}

// runLint is the entry point for the lint subcommand. It is read-only
// by definition: the -fix flag is meaningless and a warning is surfaced
// so the user knows the flag did nothing. Mutation regardless of flag
// combination is pinned by TestRunLint_DoesNotMutateFilesEvenWithFixFlag.
func runLint(args []string, opts *Options, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "iac-codemod lint: at least one path is required")
		usage(stderr)
		return 2
	}
	if opts != nil && opts.Fix {
		// Lint never mutates. Surface a warning so the user knows -fix
		// did not change behavior; preserves predictable advisory-only
		// semantics from plan §W-8 line 397.
		fmt.Fprintln(stderr, "iac-codemod lint: warning: -fix has no effect (lint is read-only)")
	}

	report := &lintReport{}
	for _, path := range args {
		if err := lintPath(path, report); err != nil {
			fmt.Fprintf(stderr, "iac-codemod lint: %s: %v\n", path, err)
			return 1
		}
	}
	report.print(stdout)
	// Exit code semantics:
	//   0 = clean (no findings, no errors)
	//   1 = advisory findings present (no per-file errors)
	//   2 = per-file parse/type-check errors (findings count
	//       irrelevant; the analyzer never got a chance to run on
	//       at least one file)
	//
	// Round-1 #10 conflated findings and errors at exit 1, which let
	// `make migrate-providers || true` swallow real failures. Round-5
	// #7 splits the codes so callers can `|| [ $? -eq 1 ]` to accept
	// findings as advisory while still failing on unparseable input.
	if len(report.errors) > 0 {
		return 2
	}
	if len(report.findings) > 0 {
		return 1
	}
	return 0
}

// lintPath walks path for *.go files (excluding _test.go, vendor,
// testdata, hidden dirs) and invokes lintFile on each. Per-file errors
// are recorded in the report rather than aborting the whole run so a
// single broken file in a multi-package plugin does not lose findings
// from the rest.
//
// Round-10 #5: rev2 of this walker called lintFile per file, and
// lintFile re-parsed every sibling per-call → O(n²) on packages with
// many files. Now lintFile takes an optional pre-parsed sibling
// cache (lintDirCache) so per-directory parses are reused across the
// directory's files.
func lintPath(path string, report *lintReport) error {
	info, err := stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return fmt.Errorf("not a Go source file (or is a _test.go): %s", path)
		}
		if err := lintFile(path, nil, report); err != nil {
			report.errors = append(report.errors, fmt.Sprintf("%s: %v", path, err))
		}
		return nil
	}
	// Group files by directory so we can build a per-directory sibling
	// parse cache once and reuse it across the directory's files.
	dirFiles := make(map[string][]string)
	if err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
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
		dir := filepath.Dir(p)
		dirFiles[dir] = append(dirFiles[dir], p)
		return nil
	}); err != nil {
		return err
	}
	// Process each directory with a fresh sibling cache. Errors per
	// file are recorded in the report; we never abort the walk.
	for dir, paths := range dirFiles {
		cache := newLintDirCache(dir)
		for _, p := range paths {
			if err := lintFile(p, cache, report); err != nil {
				report.errors = append(report.errors, fmt.Sprintf("%s: %v", p, err))
			}
		}
	}
	return nil
}

// lintDirCache caches parsed sibling files for a single directory so
// lintFile doesn't re-parse them per-target. Round-10 #5: closes the
// O(n²) perf gap.
type lintDirCache struct {
	dir   string
	files map[string]*ast.File // path → parsed file (re-used across siblings)
	fset  *token.FileSet
}

// newLintDirCache constructs a cache and pre-parses every non-test
// .go file in dir. Errors during pre-parse are silently dropped (the
// per-file pass will surface them via its own parse).
func newLintDirCache(dir string) *lintDirCache {
	c := &lintDirCache{
		dir:   dir,
		files: make(map[string]*ast.File),
		fset:  token.NewFileSet(),
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return c
	}
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
		f, err := parser.ParseFile(c.fset, fpath, src, parser.ParseComments)
		if err != nil {
			continue
		}
		c.files[fpath] = f
	}
	return c
}

// lintFile parses path, loads its sibling .go files (same directory,
// non-test) so cross-file method sets are visible to the analyzers,
// type-checks tolerantly, and runs every analyzer in lintAnalyzers.
//
// Review round-2 finding #9: rev0/rev1 of this function passed only
// the target file in pass.Files, so providerLikeReceivers /
// driverLikeReceivers / AssertProviderImplementsValidatePlan saw
// only methods declared in that file. Providers with Plan/Apply (or
// drivers with Diff + companion methods) split across sibling files
// were silently skipped — same blind spot the refactor-* modes had
// in round 1. Now lintFile loads every non-test .go file in the same
// directory and feeds the full slice to each analyzer.
//
// Diagnostics for files OTHER than `path` are silently dropped: each
// invocation of lintFile only owns the report for `path`, and the
// outer walker visits each file in turn. This avoids duplicate
// findings without requiring a higher-level dedup. Sibling files
// serve only as method-set context.
func lintFile(path string, cache *lintDirCache, report *lintReport) error {
	// Round-10 #5: prefer the per-directory cache (built once per dir
	// in lintPath) so sibling parses are reused across the directory's
	// files. Falls back to per-call parsing when no cache is supplied
	// (single-file invocation).
	var primary *ast.File
	var fset *token.FileSet
	files := []*ast.File{}
	if cache != nil && cache.files[path] != nil {
		primary = cache.files[path]
		fset = cache.fset
	} else {
		src, err := readFile(path)
		if err != nil {
			return err
		}
		fset = token.NewFileSet()
		primary, err = parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			return err
		}
	}
	files = append(files, primary)
	// Sibling files from the cache (or per-call fallback walk).
	if cache != nil {
		for sibPath, sib := range cache.files {
			if sibPath == path || sib == nil {
				continue
			}
			if sib.Name.Name != primary.Name.Name {
				continue
			}
			files = append(files, sib)
		}
	} else if entries, err := os.ReadDir(filepath.Dir(path)); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			sibPath := filepath.Join(filepath.Dir(path), name)
			if sibPath == path {
				continue
			}
			sibSrc, err := readFile(sibPath)
			if err != nil {
				continue
			}
			sib, err := parser.ParseFile(fset, sibPath, sibSrc, parser.ParseComments)
			if err != nil {
				continue
			}
			if sib.Name.Name != primary.Name.Name {
				continue
			}
			files = append(files, sib)
		}
	}

	conf := &types.Config{
		Importer: stubImporterRuntime{},
		Error:    func(error) {}, // tolerate type errors; lint is best-effort
	}
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Implicits:  make(map[ast.Node]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	pkg, _ := conf.Check(primary.Name.Name, fset, files, info)

	for _, analyzer := range lintAnalyzers {
		pass := &analysis.Pass{
			Analyzer:  analyzer,
			Fset:      fset,
			Files:     files,
			Pkg:       pkg,
			TypesInfo: info,
			Report: func(d analysis.Diagnostic) {
				// Drop diagnostics targeting other files: the outer
				// walker will visit them in their own turn.
				diagPath := fset.Position(d.Pos).Filename
				if diagPath != "" && diagPath != path {
					return
				}
				report.findings = append(report.findings, lintFinding{
					Path:     path,
					Line:     fset.Position(d.Pos).Line,
					Analyzer: analyzer.Name,
					Message:  d.Message,
				})
			},
		}
		if _, err := analyzer.Run(pass); err != nil {
			return fmt.Errorf("%s: %w", analyzer.Name, err)
		}
	}
	return nil
}

// stubImporterRuntime is the importer used by the runtime lintFile path.
// It mirrors stubImporter in lint_test.go so test and runtime behavior
// stay aligned.
type stubImporterRuntime struct{}

func (stubImporterRuntime) Import(path string) (*types.Package, error) {
	return types.NewPackage(path, filepath.Base(path)), nil
}

// stat / readFile are split out so tests could override them in future
// if needed. Today they are thin wrappers over os.Stat / os.ReadFile.
var (
	stat     = osStat
	readFile = osReadFile
)

// ============================================================
// Skip-marker helpers
// ============================================================

// hasSkipMarkerOn reports whether the given doc CommentGroup contains
// the canonical SkipMarker from main.go. Used by every analyzer that
// flags a function or type declaration.
//
// Accepted shapes:
//
//	// wfctl:skip-iac-codemod
//	// wfctl:skip-iac-codemod legacy upsert recovery, see ADR-042
//	// wfctl:skip-iac-codemod\tlegacy upsert recovery, see ADR-042
//
// The marker followed by ANY whitespace separator + arbitrary
// justification text is honored (review round-2 follow-up A). Go
// maintainers may use spaces or tabs to align justifications; silently
// ignoring tab-delimited reasons would replicate the silent-no-op
// surface plan rev2 line 2400 unifies the marker to prevent.
//
// Rejected shapes (a non-whitespace suffix means a different marker):
//
//	// wfctl:skip-iac-codemod-extension
//	// wfctl:skip-iac-codemodSOMETHING
//	// wfctl:skip-codemod                (legacy, design rev1)
//
// The whitespace-separator discipline keeps the match tight enough
// that no substring shadow can bypass marker discipline.
func hasSkipMarkerOn(doc *ast.CommentGroup) bool {
	if doc == nil {
		return false
	}
	for _, c := range doc.List {
		// Comment text includes the leading `//` per ast.Comment convention.
		text := strings.TrimSpace(c.Text)
		if text == SkipMarker {
			return true
		}
		if strings.HasPrefix(text, SkipMarker) && len(text) > len(SkipMarker) {
			next, _ := utf8.DecodeRuneInString(text[len(SkipMarker):])
			if unicode.IsSpace(next) {
				return true
			}
		}
	}
	return false
}

// Skipped sites are surfaced through the same pass.Report channel as
// real findings, distinguished by a message prefix. The driver
// (lintReport.unpackSkippedFromFindings) splits them out before
// rendering so skip records do NOT contribute to the finding count or
// the non-zero exit code. The indirection keeps each analyzer's API
// surface to a single Reportf-style channel rather than threading a
// second sink through every Run signature, and lets unit tests use a
// vanilla analysis.Pass without any custom rigging.
//
// IMPORTANT: lintFile invocation is currently sequential per path. If a
// future maintainer parallelises it, the skip-prefix encoding stays
// safe (each pass owns its own diagnostic slice via its Report closure)
// — but introducing concurrent map access via the package-level
// `modes` var or shared *lintReport pointer would not. See main_test.go
// header for the t.Parallel prohibition that applies here too.

const skipDiagnosticPrefix = "[skipped] "

// reportSkip emits a synthetic diagnostic that the driver decodes as a
// skipped-site record rather than a finding. This keeps the analyzer
// API surface minimal (one channel, not two).
func reportSkip(pass *analysis.Pass, pos token.Pos, declName string) {
	pass.Report(analysis.Diagnostic{
		Pos:     pos,
		Message: skipDiagnosticPrefix + declName,
	})
}

// ============================================================
// Analyzer #1: AssertPlanDelegatesToHelper
// ============================================================

func runAssertPlanDelegatesToHelper(pass *analysis.Pass) (any, error) {
	provs := providerLikeReceivers(pass)
	typeDocsByFile := receiverTypeDocsForPass(pass)
	for _, file := range pass.Files {
		typeDocs := typeDocsByFile[file]
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
				// Method named Plan on a non-provider type (e.g., a
				// deploy target). Skip to keep precision high.
				continue
			}
			// Honor SkipMarker on fn.Doc OR receiver-type docs (review
			// round-2 finding #6).
			if hasSkipMarkerOn(fn.Doc) || typeDocs[recv].carriesMarker() {
				routeSkip(pass, fn)
				continue
			}
			// Round-10 #7: rev3 accepted ANY platform.ComputePlan or
			// wfctlhelpers.Plan call anywhere in the body, so a Plan
			// method that called the helper as an intermediate step
			// (then added bespoke logic, returned a wrapped value,
			// etc.) was reported clean despite NOT actually delegating.
			// Now we require the canonical SHAPE: either the
			// 2-statement delegation form (matching
			// isAlreadyDelegatedPlanBody) OR a single-statement legacy
			// `return <alias>.Plan(...)`. Anything else flags the
			// diagnostic so the maintainer reviews the bespoke wrapper.
			if !planBodyDelegatesCanonically(fn.Body, file) {
				pass.Reportf(fn.Pos(), "%s.%s does not delegate to platform.ComputePlan; non-canonical Plan() body", receiverTypeName(fn), fn.Name.Name)
			}
		}
	}
	return nil, nil
}

// planBodyDelegatesCanonically returns true if body matches the
// canonical Plan-delegation shape (round-10 #7). Accepts EITHER:
//
//   - 2-statement rev2 form: `plan, err := <platform>.ComputePlan(...);
//     return &plan, err` (matches isAlreadyDelegatedPlanBody)
//   - single-statement legacy form: `return <wfctlhelpers>.Plan(...)`
//     OR `return <platform>.ComputePlan(...)` (planned-but-not-shipped
//     and broken-rev1 fixtures, accepted as advisory-clean here even
//     though the rewriter would repair them)
//
// Anything else (including bodies that CALL the helper anywhere but
// don't return its value verbatim) is rejected as non-canonical.
func planBodyDelegatesCanonically(body *ast.BlockStmt, file *ast.File) bool {
	if body == nil {
		return false
	}
	// Shape 1: 2-statement form (matches the rewriter's idempotency).
	if isAlreadyDelegatedPlanBody(body, file) {
		return true
	}
	// Shape 2: single-statement legacy `return <wfctlhelpers>.Plan(...)`.
	// The planned-but-not-shipped wfctlhelpers.Plan target was speculative;
	// any code using it is fictional and the type-check will fail anyway,
	// but we accept it as advisory-clean so a maintainer who hand-applied
	// rev0 of this codemod isn't re-flagged.
	//
	// Round-11 #1: the BROKEN `return platform.ComputePlan(...)`
	// single-statement form (rev1 ill-formed rewrite — uncompilable
	// due to value/pointer mismatch) is REJECTED here. Lint should
	// surface this as still-needs-fixup so `migrate-providers`
	// catches partially-migrated providers.
	if len(body.List) == 1 {
		if ret, ok := body.List[0].(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
			if call, ok := ret.Results[0].(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					if x, ok := sel.X.(*ast.Ident); ok {
						wfhAlias := pkgAliasFor(file, helperImportPath, "wfctlhelpers")
						if (x.Name == wfhAlias || x.Name == "wfctlhelpers") && sel.Sel.Name == "Plan" {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

// receiverTypeDocsForPass builds a SINGLE merged receiverDoc map
// across every file in pass.Files. The same map is returned per-file
// (callers do `typeDocs := typeDocsByFile[file]`) — they get the
// directory-wide view so a skip-marker on a sibling file's type
// declaration is honored even when the function being analyzed lives
// in a different file. Round-6 finding #1: rev2 returned per-file
// maps, so `typeDocs[recv]` missed sibling-file TypeSpec docs and
// providers split across files were rewritten despite type-doc skip
// markers.
//
// First-occurrence wins: if multiple files declare the same receiver
// type name (an unusual layout but possible), the first iteration
// order wins. The lint analyzers prefer the in-file declaration over
// shadows since they iterate pass.Files in stable order.
func receiverTypeDocsForPass(pass *analysis.Pass) map[*ast.File]map[string]receiverDoc {
	merged := make(map[string]receiverDoc)
	for _, file := range pass.Files {
		for recv, doc := range receiverTypeDocs(file) {
			if _, ok := merged[recv]; ok {
				continue
			}
			merged[recv] = doc
		}
	}
	out := make(map[*ast.File]map[string]receiverDoc, len(pass.Files))
	for _, file := range pass.Files {
		out[file] = merged
	}
	return out
}

// ============================================================
// Analyzer #2: AssertApplyDelegatesToHelper
// ============================================================

func runAssertApplyDelegatesToHelper(pass *analysis.Pass) (any, error) {
	provs := providerLikeReceivers(pass)
	typeDocsByFile := receiverTypeDocsForPass(pass)
	for _, file := range pass.Files {
		typeDocs := typeDocsByFile[file]
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
				// Method named Apply on a non-provider type. Skip.
				continue
			}
			// Honor SkipMarker on fn.Doc OR receiver-type docs (review
			// round-2 finding #7).
			if hasSkipMarkerOn(fn.Doc) || typeDocs[recv].carriesMarker() {
				routeSkip(pass, fn)
				continue
			}
			// Round-10 #8: rev3 accepted ANY wfctlhelpers.ApplyPlan
			// call anywhere in the body, so an Apply that referenced
			// the helper incidentally (with extra work before/after)
			// was reported clean despite NOT actually delegating. Now
			// we require the canonical single-statement
			// `return <alias>.ApplyPlan(...)` form (the same shape the
			// rewriter checks for idempotency).
			if !isAlreadyDelegatedApplyBody(fn.Body, file) {
				pass.Reportf(fn.Pos(), "%s.%s does not delegate to wfctlhelpers.ApplyPlan; non-canonical Apply() body", receiverTypeName(fn), fn.Name.Name)
			}
		}
	}
	return nil, nil
}

// ============================================================
// Analyzer #3: AssertDiffSetsNeedsReplaceForForceNew
// ============================================================

func runAssertDiffSetsNeedsReplaceForForceNew(pass *analysis.Pass) (any, error) {
	drivers := driverLikeReceivers(pass)
	typeDocsByFile := receiverTypeDocsForPass(pass)
	for _, file := range pass.Files {
		typeDocs := typeDocsByFile[file]
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if !isProviderMethod(fn, "Diff", 3, 2) {
				continue
			}
			recv := receiverTypeName(fn)
			if !drivers[recv] {
				// Method named Diff on a non-driver type (e.g., a
				// settings struct or config differ). Skip to keep
				// precision high — review finding #3.
				continue
			}
			// Honor SkipMarker on fn.Doc OR receiver-type docs (review
			// round-2 finding #8).
			if hasSkipMarkerOn(fn.Doc) || typeDocs[recv].carriesMarker() {
				routeSkip(pass, fn)
				continue
			}
			refsForceNew := bodyReferencesField(fn.Body, "ForceNew")
			assignsNeedsReplace := bodyAssignsField(fn.Body, "NeedsReplace")
			if refsForceNew && !assignsNeedsReplace {
				pass.Reportf(fn.Pos(), "%s.%s references ForceNew but never assigns NeedsReplace; W-3 force-new contract violated", receiverTypeName(fn), fn.Name.Name)
			}
		}
	}
	return nil, nil
}

// ============================================================
// Analyzer #4: AssertProviderImplementsValidatePlan
// ============================================================

func runAssertProviderImplementsValidatePlan(pass *analysis.Pass) (any, error) {
	if pass.Pkg == nil {
		return nil, nil
	}
	scope := pass.Pkg.Scope()
	if scope == nil {
		return nil, nil
	}
	// Group method sets by receiver type name, walking AST so we can
	// surface the original ast.FuncDecl for skip-marker handling.
	// typeDocsByName captures both TypeSpec.Doc and the wrapping
	// GenDecl.Doc so the skip-marker check can consult both — review
	// round-3 finding #7: rev2 only checked ts.Doc, missing markers
	// placed before the `type` keyword (the wrapping GenDecl).
	methodsByRecv := make(map[string][]*ast.FuncDecl)
	typeDecls := make(map[string]*ast.TypeSpec)
	typeDocsByName := make(map[string]receiverDoc)
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil || len(d.Recv.List) == 0 {
					continue
				}
				recv := receiverTypeName(d)
				if recv == "" {
					continue
				}
				methodsByRecv[recv] = append(methodsByRecv[recv], d)
			case *ast.GenDecl:
				if d.Tok != token.TYPE {
					continue
				}
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					if _, isStruct := ts.Type.(*ast.StructType); !isStruct {
						continue
					}
					typeDecls[ts.Name.Name] = ts
					typeDocsByName[ts.Name.Name] = receiverDoc{
						TypeSpecDoc: ts.Doc,
						GenDeclDoc:  d.Doc,
					}
				}
			}
		}
	}
	for recv, methods := range methodsByRecv {
		if !looksLikeProvider(methods) {
			continue
		}
		// Skip if the type's TypeSpec.Doc OR wrapping GenDecl.Doc
		// carries the marker, or any of the provider's signature
		// methods (Plan/Apply) carry it. ValidatePlan being absent is
		// the whole point of this analyzer, so checking only
		// Plan/Apply is sufficient.
		if typeDocsByName[recv].carriesMarker() {
			ts := typeDecls[recv]
			pos := token.NoPos
			if ts != nil {
				pos = ts.Pos()
			} else if len(methods) > 0 {
				pos = methods[0].Pos()
			}
			routeSkipName(pass, pos, recv)
			continue
		}
		// Round-8 #3: rev2 checked the marker on EVERY method, so a
		// marker on Destroy/Status/etc. accidentally suppressed the
		// whole provider's analysis. Restrict to Plan and Apply (the
		// provider-defining methods that actually opt the type out).
		anyMarker := false
		for _, m := range methods {
			if m.Name.Name != "Plan" && m.Name.Name != "Apply" {
				continue
			}
			if hasSkipMarkerOn(m.Doc) {
				anyMarker = true
				break
			}
		}
		if anyMarker {
			routeSkipName(pass, methods[0].Pos(), recv)
			continue
		}
		// Signature + receiver-kind match. Round-1 #11 added the
		// signature check; round-5 #4 added the receiver-kind check
		// (a value-receiver provider with a pointer-receiver
		// ValidatePlan still fails the ProviderValidator type
		// assertion because the method set on `T` does not include
		// `*T` methods). hasValidatePlanMethod centralises the logic.
		if hasValidatePlanMethod(methods) {
			continue
		}
		// Round-11 #4 reverts round-10 #3's broad-suppress on
		// embedded fields: many embeddings (sync.Mutex, loggers,
		// config mixins) don't promote ValidatePlan, so real targets
		// were silently missed. Maintainers whose providers actually
		// promote ValidatePlan can suppress with the explicit
		// `// wfctl:skip-iac-codemod` marker (the universal opt-out).
		// Report at the type decl if available, else at the first method.
		var pos token.Pos
		if ts, ok := typeDecls[recv]; ok {
			pos = ts.Pos()
		} else {
			pos = methods[0].Pos()
		}
		pass.Reportf(pos, "provider type %s does not implement ValidatePlan; ProviderValidator (R-A10) cannot run on plans involving this provider", recv)
	}
	return nil, nil
}

// driverLikeReceivers returns the set of receiver type names whose
// method set in pass.Files contains a Diff method AND at least one
// canonical companion driver method (Read, Create, Update, Delete).
// Used by AssertDiffSetsNeedsReplaceForForceNew to keep precision high
// — review finding #3: a type with only Diff (e.g. a config differ)
// is not a resource driver and should not be analysed for force-new
// contract compliance.
func driverLikeReceivers(pass *analysis.Pass) map[string]bool {
	methodsByRecv := make(map[string][]*ast.FuncDecl)
	for _, file := range pass.Files {
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
		hasDiff, hasCompanion := false, false
		for _, m := range methods {
			switch m.Name.Name {
			case "Diff":
				if m.Type.Params != nil && len(m.Type.Params.List) >= 2 && m.Type.Results != nil && len(m.Type.Results.List) == 2 {
					hasDiff = true
				}
			case "Read", "Create", "Update", "Delete":
				hasCompanion = true
			}
		}
		if hasDiff && hasCompanion {
			out[recv] = true
		}
	}
	return out
}

// providerLikeReceivers returns the set of receiver type names whose
// method set in pass.Files contains both Plan and Apply with shapes
// matching IaCProvider. Used by every analyzer that should fire only
// on IaC providers (not on deploy targets or other Apply-shaped types).
func providerLikeReceivers(pass *analysis.Pass) map[string]bool {
	methodsByRecv := make(map[string][]*ast.FuncDecl)
	for _, file := range pass.Files {
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

// looksLikeProvider returns true if the method list contains both Plan
// and Apply with shapes matching IaCProvider:
//
//	Plan(context.Context, []ResourceSpec, []ResourceState) (*IaCPlan, error)
//	Apply(context.Context, *IaCPlan) (*ApplyResult, error)
//
// Round-12 #8: rev1 only checked method NAMES + rough arity, so any
// unrelated type with `Plan(...)` and `Apply(...)` (e.g., a deploy
// strategy or a UI handler) was treated as a provider. Tightened to
// match the signature shape via type-name suffix checks (qualified or
// unqualified): IaCPlan / ResourceSpec / ResourceState / ApplyResult /
// context.Context.
func looksLikeProvider(methods []*ast.FuncDecl) bool {
	hasPlan, hasApply := false, false
	for _, m := range methods {
		switch m.Name.Name {
		case "Plan":
			if planSignatureMatches(m.Type) {
				hasPlan = true
			}
		case "Apply":
			if applySignatureMatches(m.Type) {
				hasApply = true
			}
		}
	}
	return hasPlan && hasApply
}

// planSignatureMatches verifies the function type matches
// `Plan(ctx, []ResourceSpec, []ResourceState) (*IaCPlan, error)`.
// Returns true if the parameter and result types match by name suffix
// (qualified or unqualified). Used by looksLikeProvider to filter
// false positives on unrelated `Plan` methods (round-12 #8).
func planSignatureMatches(ft *ast.FuncType) bool {
	if ft == nil || ft.Params == nil || ft.Results == nil {
		return false
	}
	paramTypes := flattenFieldTypes(ft.Params.List)
	if len(paramTypes) != 3 {
		return false
	}
	resultTypes := flattenFieldTypes(ft.Results.List)
	if len(resultTypes) != 2 {
		return false
	}
	// Param 1: context.Context (selector .Context)
	if !typeNameTailMatches(paramTypes[0], "Context") {
		return false
	}
	// Param 2: []ResourceSpec
	arr, ok := paramTypes[1].(*ast.ArrayType)
	if !ok || arr.Len != nil || !typeNameTailMatches(arr.Elt, "ResourceSpec") {
		return false
	}
	// Param 3: []ResourceState
	arr2, ok := paramTypes[2].(*ast.ArrayType)
	if !ok || arr2.Len != nil || !typeNameTailMatches(arr2.Elt, "ResourceState") {
		return false
	}
	// Result 1: *IaCPlan
	star, ok := resultTypes[0].(*ast.StarExpr)
	if !ok || !typeNameTailMatches(star.X, "IaCPlan") {
		return false
	}
	// Result 2: error
	return typeNameTailMatches(resultTypes[1], "error")
}

// applySignatureMatches verifies the function type matches
// `Apply(ctx, *IaCPlan) (*ApplyResult, error)`.
func applySignatureMatches(ft *ast.FuncType) bool {
	if ft == nil || ft.Params == nil || ft.Results == nil {
		return false
	}
	paramTypes := flattenFieldTypes(ft.Params.List)
	if len(paramTypes) != 2 {
		return false
	}
	resultTypes := flattenFieldTypes(ft.Results.List)
	if len(resultTypes) != 2 {
		return false
	}
	if !typeNameTailMatches(paramTypes[0], "Context") {
		return false
	}
	star, ok := paramTypes[1].(*ast.StarExpr)
	if !ok || !typeNameTailMatches(star.X, "IaCPlan") {
		return false
	}
	starR, ok := resultTypes[0].(*ast.StarExpr)
	if !ok || !typeNameTailMatches(starR.X, "ApplyResult") {
		return false
	}
	return typeNameTailMatches(resultTypes[1], "error")
}

// flattenFieldTypes expands a Go FieldList (where `a, b T` is one
// field with two names) into a flat slice of types — one per
// parameter or return value.
func flattenFieldTypes(list []*ast.Field) []ast.Expr {
	var out []ast.Expr
	for _, f := range list {
		count := 1
		if len(f.Names) > 1 {
			count = len(f.Names)
		}
		for i := 0; i < count; i++ {
			out = append(out, f.Type)
		}
	}
	return out
}

// ============================================================
// Shared AST helpers
// ============================================================

// isProviderMethod returns true if fn is a method (has receiver) named
// methodName, with at least minParams parameter fields and exactly
// expectedResults result fields. Parameter and result counts are
// approximate (Go FieldList groups multi-param fields like `a, b T`
// into one field), so the actual call-site arity may differ — but the
// shape filter is sufficient for distinguishing IaCProvider/Driver
// methods from unrelated lookalikes.
func isProviderMethod(fn *ast.FuncDecl, methodName string, minParams, expectedResults int) bool {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return false
	}
	if fn.Name.Name != methodName {
		return false
	}
	if fn.Type.Params == nil || len(fn.Type.Params.List) < minParams {
		return false
	}
	if fn.Type.Results == nil || len(fn.Type.Results.List) != expectedResults {
		return false
	}
	if fn.Body == nil {
		return false
	}
	return true
}

// receiverTypeName extracts the receiver type identifier from a method
// declaration, stripping any pointer indirection. Returns "" for
// unrecognised receiver shapes.
func receiverTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	id, ok := expr.(*ast.Ident)
	if !ok {
		return ""
	}
	return id.Name
}

// bodyCallsSelector reports whether the function body contains a
// CallExpr whose callee is a SelectorExpr with the given X.Name and
// Sel.Name, e.g. `wfctlhelpers.Plan(...)`.
func bodyCallsSelector(body *ast.BlockStmt, pkgIdent, selName string) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
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
		if !ok {
			return true
		}
		if x.Name == pkgIdent && sel.Sel.Name == selName {
			found = true
			return false
		}
		return true
	})
	return found
}

// bodyReferencesField reports whether the function body references any
// SelectorExpr with the given Sel.Name, e.g. any `<X>.ForceNew`.
func bodyReferencesField(body *ast.BlockStmt, fieldName string) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == fieldName {
			found = true
			return false
		}
		return true
	})
	return found
}

// bodyAssignsField reports whether the function body contains any
// assignment `<X>.fieldName = <expr>` regardless of RHS shape, EXCEPT
// for an explicit literal `false` (which is treated as no-assignment).
// Both `r.NeedsReplace = true` (inside an `if c.ForceNew` guard) and
// the terser `r.NeedsReplace = c.ForceNew` are valid expressions of
// the W-3 force-new contract — review round-1 finding #4 widened the
// matcher so the second pattern doesn't trigger a false positive.
// Review round-2 follow-up B then narrowed the widening so the
// copy-paste bug `r.NeedsReplace = false` (assigning the wrong
// constant inside a ForceNew branch) is still flagged. Maintainers
// who genuinely don't propagate ForceNew leave NeedsReplace untouched
// entirely, which the analyzer also catches.
func bodyAssignsField(body *ast.BlockStmt, fieldName string) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range assign.Lhs {
			sel, ok := lhs.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			if sel.Sel.Name != fieldName {
				continue
			}
			if i < len(assign.Rhs) {
				if id, ok := assign.Rhs[i].(*ast.Ident); ok && id.Name == "false" {
					// Literal-false assignment is treated as
					// no-assignment so a copy-paste typo inside the
					// ForceNew branch is still flagged.
					continue
				}
			}
			found = true
			return false
		}
		return true
	})
	return found
}

// routeSkip records a skipped FuncDecl through the pass.Report channel
// using the skipDiagnosticPrefix encoding.
func routeSkip(pass *analysis.Pass, fn *ast.FuncDecl) {
	declName := fmt.Sprintf("%s.%s", receiverTypeName(fn), fn.Name.Name)
	reportSkip(pass, fn.Pos(), declName)
}

// routeSkipName records a skipped declaration by its name (used for
// type-level skips).
func routeSkipName(pass *analysis.Pass, pos token.Pos, name string) {
	reportSkip(pass, pos, name)
}

// ============================================================
// Report rendering
// ============================================================

// print renders the report to w in Markdown-ish format. Findings come
// first (sorted by file, line, analyzer); then skipped sites; then
// per-file errors. Skipped diagnostics encoded with skipDiagnosticPrefix
// are extracted from findings into the skipped section first so the
// finding count reflects only real issues.
func (r *lintReport) print(w io.Writer) {
	r.unpackSkippedFromFindings()

	sort.Slice(r.findings, func(i, j int) bool {
		if r.findings[i].Path != r.findings[j].Path {
			return r.findings[i].Path < r.findings[j].Path
		}
		if r.findings[i].Line != r.findings[j].Line {
			return r.findings[i].Line < r.findings[j].Line
		}
		return r.findings[i].Analyzer < r.findings[j].Analyzer
	})
	sort.Slice(r.skipped, func(i, j int) bool {
		if r.skipped[i].Path != r.skipped[j].Path {
			return r.skipped[i].Path < r.skipped[j].Path
		}
		return r.skipped[i].Line < r.skipped[j].Line
	})

	fmt.Fprintln(w, "# iac-codemod lint report")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Findings: %d\n", len(r.findings))
	fmt.Fprintf(w, "Skipped:  %d\n", len(r.skipped))
	fmt.Fprintf(w, "Errors:   %d\n", len(r.errors))
	fmt.Fprintln(w)

	if len(r.findings) > 0 {
		fmt.Fprintln(w, "## Findings")
		fmt.Fprintln(w)
		for _, f := range r.findings {
			fmt.Fprintf(w, "- %s:%d [%s] %s\n", f.Path, f.Line, f.Analyzer, f.Message)
		}
		fmt.Fprintln(w)
	}

	if len(r.skipped) > 0 {
		fmt.Fprintln(w, "## Skipped (// wfctl:skip-iac-codemod)")
		fmt.Fprintln(w)
		for _, s := range r.skipped {
			fmt.Fprintf(w, "- %s:%d [%s] %s\n", s.Path, s.Line, s.Analyzer, s.Decl)
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

// unpackSkippedFromFindings moves skip-prefixed diagnostics from the
// findings list into the skipped list, restoring the canonical exit-code
// semantics (skipped sites do not count as findings).
func (r *lintReport) unpackSkippedFromFindings() {
	if len(r.findings) == 0 {
		return
	}
	kept := r.findings[:0]
	for _, f := range r.findings {
		if decl, ok := strings.CutPrefix(f.Message, skipDiagnosticPrefix); ok {
			r.skipped = append(r.skipped, skippedSite{
				Path:     f.Path,
				Line:     f.Line,
				Analyzer: f.Analyzer,
				Decl:     decl,
			})
			continue
		}
		kept = append(kept, f)
	}
	r.findings = kept
}
