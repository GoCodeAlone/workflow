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
	modes["add-validate-plan"] = runAddValidatePlan
}

// validatePlanClassification labels the disposition of a single
// provider receiver type with respect to the ValidatePlan stub
// injection. Drives both the report grouping and the mutation gate.
type validatePlanClassification int

const (
	// validatePlanMissing: provider has Plan + Apply but no
	// ValidatePlan; the stub will be injected on -fix.
	validatePlanMissing validatePlanClassification = iota
	// validatePlanAlreadyImplemented: provider already has
	// ValidatePlan; idempotent no-op.
	validatePlanAlreadyImplemented
	// validatePlanSkipped: marker on the type decl or on Plan/Apply.
	validatePlanSkipped
)

func (c validatePlanClassification) String() string {
	switch c {
	case validatePlanMissing:
		return "missing-validate-plan"
	case validatePlanAlreadyImplemented:
		return "already-implemented"
	case validatePlanSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

// validatePlanSite captures one provider-type site in the report.
type validatePlanSite struct {
	Path     string
	Line     int
	Receiver string
	Class    validatePlanClassification
	Inserted bool // set when -fix actually injected a stub
}

type validatePlanReport struct {
	sites  []validatePlanSite
	errors []string
}

// runAddValidatePlan is the entry point for the add-validate-plan
// subcommand. It walks the supplied paths, classifies each provider
// receiver, and (under -fix) injects a no-op ValidatePlan stub on
// missing sites.
func runAddValidatePlan(args []string, opts *Options, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "iac-codemod add-validate-plan: at least one path is required")
		usage(stderr)
		return 2
	}
	report := &validatePlanReport{}
	for _, path := range args {
		if err := addValidatePlanPath(path, opts, report); err != nil {
			fmt.Fprintf(stderr, "iac-codemod add-validate-plan: %s: %v\n", path, err)
			return 1
		}
	}
	report.print(stdout, opts)
	if len(report.errors) > 0 {
		return 1
	}
	return 0
}

func addValidatePlanPath(path string, opts *Options, report *validatePlanReport) error {
	info, err := stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return fmt.Errorf("not a Go source file (or is a _test.go): %s", path)
		}
		if err := addValidatePlanFile(path, opts, report); err != nil {
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
		if err := addValidatePlanFile(p, opts, report); err != nil {
			report.errors = append(report.errors, fmt.Sprintf("%s: %v", p, err))
		}
		return nil
	})
}

// addValidatePlanFile parses `path`, identifies provider-shaped
// receiver types, and (under -fix) appends a no-op ValidatePlan stub
// for each provider missing one. The stub uses an unqualified
// `*IaCPlan` and `[]PlanDiagnostic` so the substituted code compiles
// against whichever package alias the rest of the file uses.
//
// Insertion strategy: rather than synthesising the FuncDecl via
// AST nodes (which is brittle when the package's IaCPlan type is
// imported under an alias), we append the stub as raw text after
// printing the file. This keeps the rest of the file byte-identical
// for files that only need a stub appended, and avoids any risk of
// printer-induced reformatting elsewhere in the source.
func addValidatePlanFile(path string, opts *Options, report *validatePlanReport) error {
	src, err := readFile(path)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return err
	}

	provs, methodsByRecv, typeDecls := providerReceiversWithMethods(file)
	// Widen `provs` AND `methodsByRecv` to the directory-wide method
	// set so all per-receiver decisions (skip-marker check,
	// hasValidatePlanMethod, receiver-kind inference) consult ALL
	// methods of the type, not only the ones declared in this file.
	// Review round-2 finding #9: rev1 only widened `provs`, leaving
	// methodsByRecv file-local. A provider whose ValidatePlan was
	// already implemented in a sibling file would still receive a
	// duplicate stub here. Now methodsByRecv carries the package-wide
	// view; stub injection still only fires when typeDecls[recv] is
	// non-nil so we never APPEND to a sibling file.
	if dirProvs, dirMethods := planLikeProviderMethodsInDir(filepath.Dir(path)); dirProvs != nil {
		for recv := range dirProvs {
			if _, ok := provs[recv]; !ok && typeDecls[recv] != nil {
				provs[recv] = typeDecls[recv]
			}
		}
		// Merge sibling methods into methodsByRecv. Per-recv slice is
		// append-merged so any sibling ValidatePlan declaration is
		// visible to hasValidatePlanMethod, and any sibling Plan/Apply
		// is visible to providerReceiverConvention.
		for recv, sibMethods := range dirMethods {
			if _, ok := provs[recv]; !ok {
				continue
			}
			// Avoid double-counting methods declared in `file` (already
			// in methodsByRecv from providerReceiversWithMethods); the
			// directory-walker re-parses every file, so a method's
			// *ast.FuncDecl identity may differ between the local and
			// directory parses even for the same source line. We
			// dedupe by name+file-path.
			existing := make(map[string]bool)
			for _, m := range methodsByRecv[recv] {
				existing[m.Name.Name] = true
			}
			for _, m := range sibMethods {
				if existing[m.Name.Name] {
					// Already represented by the local parse.
					continue
				}
				methodsByRecv[recv] = append(methodsByRecv[recv], m)
			}
		}
	}
	// Determine the qualifier for *IaCPlan / []PlanDiagnostic so the
	// stub's signature matches whatever import-naming convention the
	// file already uses (review round-1 finding #7). Review round-4
	// finding #1: when the type declaration lives in a sibling file
	// (no interfaces import in THIS file) but ANY sibling does import
	// interfaces, fall back to the qualifier the package uses ("interfaces")
	// AND inject the import into this file via AST manipulation so the
	// stub's qualified types resolve.
	qualifier := interfacesQualifier(file)
	needsInterfacesImport := false
	if qualifier == "" {
		if siblingUsesInterfacesImport(filepath.Dir(path), path) {
			qualifier = "interfaces"
			needsInterfacesImport = true
		}
	}
	// Deterministic order for the report and for mutation: sort by
	// declaration line.
	type recvOrder struct {
		Name string
		Pos  token.Pos
	}
	var ordered []recvOrder
	for recv := range provs {
		ordered = append(ordered, recvOrder{Name: recv, Pos: provs[recv].Pos()})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Pos < ordered[j].Pos })

	mutated := false
	var pendingStubs []string
	for _, rec := range ordered {
		recv := rec.Name
		methods := methodsByRecv[recv]
		// Skip-marker check: the type decl OR any of the existing
		// Plan/Apply methods carrying the marker suppresses the
		// classification. (Mirrors the lint analyzer's logic for
		// AssertProviderImplementsValidatePlan.)
		ts := typeDecls[recv]
		skipped := false
		if ts != nil && hasSkipMarkerOn(ts.Doc) {
			skipped = true
		}
		if !skipped {
			for _, m := range methods {
				if hasSkipMarkerOn(m.Doc) {
					skipped = true
					break
				}
			}
		}
		// Also honor the parent GenDecl's doc for a `type Foo struct{}`
		// declared in a single-spec block: hasSkipMarkerOn already
		// short-circuits if the doc is nil, but we explicitly look at
		// the GenDecl wrapper's Doc as well so a marker placed before
		// the `type` keyword is honored.
		if !skipped {
			if gd := genDeclFor(file, ts); gd != nil && hasSkipMarkerOn(gd.Doc) {
				skipped = true
			}
		}

		var class validatePlanClassification
		switch {
		case skipped:
			class = validatePlanSkipped
		case hasValidatePlanMethod(methods):
			class = validatePlanAlreadyImplemented
		default:
			class = validatePlanMissing
		}

		line := 0
		if ts != nil {
			line = fset.Position(ts.Pos()).Line
		} else if len(methods) > 0 {
			line = fset.Position(methods[0].Pos()).Line
		}
		site := validatePlanSite{
			Path:     path,
			Line:     line,
			Receiver: recv,
			Class:    class,
		}
		if class == validatePlanMissing && opts != nil && opts.Fix {
			pointerRecv := providerReceiverConvention(methods)
			pendingStubs = append(pendingStubs, validatePlanStubText(recv, qualifier, pointerRecv))
			site.Inserted = true
			mutated = true
		}
		report.sites = append(report.sites, site)
	}

	if mutated && opts != nil && opts.Fix {
		baseSrc := src
		// Round-4 finding #1: when the stub uses a qualified type but
		// the file doesn't import interfaces, add the import via AST
		// printing first so the qualified type resolves.
		if needsInterfacesImport {
			ensureImport(file, "github.com/GoCodeAlone/workflow/interfaces")
			var buf bytes.Buffer
			if err := format.Node(&buf, fset, file); err != nil {
				return fmt.Errorf("format %s: %w", path, err)
			}
			baseSrc = buf.Bytes()
		}
		// Append stubs as raw text. baseSrc is either the unmodified
		// original (no interfaces import needed) or the AST-reprinted
		// form with the interfaces import injected.
		appended := append([]byte{}, baseSrc...)
		if len(appended) == 0 || appended[len(appended)-1] != '\n' {
			appended = append(appended, '\n')
		}
		for _, stub := range pendingStubs {
			appended = append(appended, '\n')
			appended = append(appended, stub...)
			if !strings.HasSuffix(stub, "\n") {
				appended = append(appended, '\n')
			}
		}
		if err := writeFileAtomicBytes(path, appended); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

// validatePlanStubText returns the source text for a no-op ValidatePlan
// stub on the named receiver type. `qualifier` is the package alias
// the source file uses for github.com/GoCodeAlone/workflow/interfaces
// (typically "interfaces", or "" if the file is itself in that package
// and uses unqualified names). `pointerReceiver` controls whether the
// stub uses `*T` or `T` as its receiver — set to match the existing
// receiver convention of the type's other methods.
//
// Review history:
//   - rev0 (round 0): always emitted unqualified `*IaCPlan` /
//     `[]PlanDiagnostic`, breaking compile in files importing
//     interfaces. Fixed in round-1 (qualifier param added).
//   - rev1 (round 2 finding #5): always used `(p *T)` even when the
//     type's existing methods used value receivers. Method-set
//     mismatch left the type failing the ProviderValidator type
//     assertion. Fixed by threading pointerReceiver through the
//     caller, which inspects the type's existing Plan/Apply
//     receivers.
func validatePlanStubText(recv, qualifier string, pointerReceiver bool) string {
	planType := "*IaCPlan"
	diagsType := "[]PlanDiagnostic"
	if qualifier != "" {
		planType = "*" + qualifier + ".IaCPlan"
		diagsType = "[]" + qualifier + ".PlanDiagnostic"
	}
	receiver := recv
	if pointerReceiver {
		receiver = "*" + recv
	}
	return fmt.Sprintf(`// ValidatePlan reports diagnostics for any plan-time concerns. The
// stub generated by iac-codemod returns no diagnostics; replace with
// real provider-specific checks (region constraints, quota limits,
// resource-type conflicts, etc.) before relying on it.
func (p %s) ValidatePlan(plan %s) %s {
	return nil
}
`, receiver, planType, diagsType)
}

// receiverIsPointer returns true if the receiver of fn is `*T` (i.e.
// a pointer receiver). Helps determine the convention to use when
// inserting a new ValidatePlan stub on the same type so the method-set
// matches the existing Plan/Apply (review round-2 #5).
func receiverIsPointer(fn *ast.FuncDecl) bool {
	if fn == nil || fn.Recv == nil || len(fn.Recv.List) == 0 {
		return false
	}
	_, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
	return ok
}

// providerReceiverConvention reports whether the receiver type's
// Plan/Apply methods use a pointer receiver. The convention used by
// the existing Plan method takes precedence; if Plan is missing the
// Apply convention is used. Defaults to true (pointer receiver) when
// no Plan/Apply pair exists, matching the dominant Go style.
func providerReceiverConvention(methods []*ast.FuncDecl) bool {
	for _, m := range methods {
		if m.Name.Name == "Plan" {
			return receiverIsPointer(m)
		}
	}
	for _, m := range methods {
		if m.Name.Name == "Apply" {
			return receiverIsPointer(m)
		}
	}
	return true
}

// siblingUsesInterfacesImport returns true if any non-test .go file
// in dir (other than excludePath) imports
// github.com/GoCodeAlone/workflow/interfaces. Used to decide whether
// to inject an interfaces import into a file that doesn't have one
// when emitting a qualified ValidatePlan stub (review round-4 #1).
func siblingUsesInterfacesImport(dir, excludePath string) bool {
	const wantPath = "github.com/GoCodeAlone/workflow/interfaces"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
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
		if fpath == excludePath {
			continue
		}
		src, err := readFile(fpath)
		if err != nil {
			continue
		}
		fs := token.NewFileSet()
		sib, err := parser.ParseFile(fs, fpath, src, parser.ImportsOnly)
		if err != nil {
			continue
		}
		for _, imp := range sib.Imports {
			if imp.Path == nil {
				continue
			}
			if strings.Trim(imp.Path.Value, `"`) == wantPath {
				return true
			}
		}
	}
	return false
}

// interfacesQualifier returns the package alias `file` uses for
// github.com/GoCodeAlone/workflow/interfaces. If the import is
// renamed (`alias "github.com/.../interfaces"`), the alias name is
// returned. If the file does not import interfaces at all, returns
// "" (the rare case of a file declaring providers entirely in
// terms of locally-defined types, e.g. unit-test fixtures).
func interfacesQualifier(file *ast.File) string {
	const wantPath = "github.com/GoCodeAlone/workflow/interfaces"
	for _, imp := range file.Imports {
		if imp.Path == nil {
			continue
		}
		v := strings.Trim(imp.Path.Value, `"`)
		if v != wantPath {
			continue
		}
		if imp.Name != nil {
			if imp.Name.Name == "_" || imp.Name.Name == "." {
				// Blank/dot imports — the latter would let the user
				// reference IaCPlan unqualified. We can't safely
				// disambiguate so we err on the side of qualifying
				// (the file would not compile with a blank import
				// of the types anyway).
				continue
			}
			return imp.Name.Name
		}
		// Default-named import — the package's actual name is
		// "interfaces" (verified by reading the package clause).
		return "interfaces"
	}
	return ""
}

// providerReceiversWithMethods returns three views of the file's
// receiver-type structure:
//   - the set of receiver type names whose method set (in this file
//     alone) looks like an IaCProvider (has Plan + Apply);
//   - methodsByRecv: every method's *ast.FuncDecl indexed by receiver;
//   - typeDecls: the *ast.TypeSpec for each struct receiver, used so
//     the report can point at the type's declaration line and the
//     skip-marker can be looked up on the type doc.
//
// Note: cross-file method sets are not supported in this single-file
// pass. A provider whose Plan and Apply live in different files will
// be missed; the codemod's spec scope is single-file (the four
// per-plugin Apply/Plan files in the workspace today are each
// self-contained).
func providerReceiversWithMethods(file *ast.File) (
	map[string]*ast.TypeSpec, // provs (key = recv name; value = its TypeSpec or nil)
	map[string][]*ast.FuncDecl, // methodsByRecv
	map[string]*ast.TypeSpec, // typeDecls
) {
	methodsByRecv := make(map[string][]*ast.FuncDecl)
	typeDecls := make(map[string]*ast.TypeSpec)
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
			}
		}
	}
	provs := make(map[string]*ast.TypeSpec)
	for recv, methods := range methodsByRecv {
		if !looksLikeProvider(methods) {
			continue
		}
		provs[recv] = typeDecls[recv]
	}
	return provs, methodsByRecv, typeDecls
}

// hasValidatePlanMethod returns true if the method list contains a
// ValidatePlan method whose signature matches
// `ValidatePlan(*IaCPlan) []PlanDiagnostic` (qualified or unqualified).
// Review round-1 finding #8: rev0 of this function only checked the
// method name, so a ValidatePlan with the wrong parameter or result
// type passed silently — defeating the codemod's purpose since the
// type would still fail to satisfy interfaces.ProviderValidator.
//
// Signature match is conservative-syntactic: 1 parameter that's a
// pointer type with name suffix "IaCPlan", and 1 result that's a
// slice type with element name suffix "PlanDiagnostic". Suffix match
// covers both qualified (`*interfaces.IaCPlan`,
// `[]interfaces.PlanDiagnostic`) and unqualified shapes.
func hasValidatePlanMethod(methods []*ast.FuncDecl) bool {
	for _, m := range methods {
		if m.Name.Name != "ValidatePlan" {
			continue
		}
		if !validatePlanSignatureMatches(m.Type) {
			continue
		}
		return true
	}
	return false
}

// validatePlanSignatureMatches returns true if ft has the canonical
// `func(*IaCPlan) []PlanDiagnostic` signature shape (qualified or
// unqualified). See hasValidatePlanMethod for the rationale.
func validatePlanSignatureMatches(ft *ast.FuncType) bool {
	if ft == nil {
		return false
	}
	if ft.Params == nil || len(ft.Params.List) != 1 {
		return false
	}
	if ft.Results == nil || len(ft.Results.List) != 1 {
		return false
	}
	// Param must be a pointer to a type whose name ends in "IaCPlan".
	star, ok := ft.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	if !typeNameTailMatches(star.X, "IaCPlan") {
		return false
	}
	// Result must be a slice whose element name ends in "PlanDiagnostic".
	arr, ok := ft.Results.List[0].Type.(*ast.ArrayType)
	if !ok {
		return false
	}
	if arr.Len != nil {
		// Fixed-size array (e.g. [3]PlanDiagnostic) is not a slice.
		return false
	}
	return typeNameTailMatches(arr.Elt, "PlanDiagnostic")
}

// typeNameTailMatches returns true if expr is `<X>.<want>` or just
// `<want>` (i.e. matches an unqualified or any-qualifier-qualified
// type name).
func typeNameTailMatches(expr ast.Expr, want string) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name == want
	case *ast.SelectorExpr:
		return e.Sel.Name == want
	}
	return false
}

// genDeclFor returns the *ast.GenDecl wrapper for the given TypeSpec,
// which is where a doc comment placed before the `type` keyword
// (rather than between `type` and the type name) lives. AST attaches
// such comments to the GenDecl rather than the inner TypeSpec.
func genDeclFor(file *ast.File, ts *ast.TypeSpec) *ast.GenDecl {
	if ts == nil {
		return nil
	}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			if spec == ts {
				return gd
			}
		}
	}
	return nil
}

// writeFileAtomicBytes is the bytes-input twin of writeFileAtomic. It
// writes `data` to a sibling temp file, flushes, and renames over
// `path` so concurrent readers see either the old or new contents,
// never a partial write.
func writeFileAtomicBytes(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".codemod-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// ============================================================
// Report rendering
// ============================================================

func (r *validatePlanReport) print(w io.Writer, opts *Options) {
	sort.Slice(r.sites, func(i, j int) bool {
		if r.sites[i].Path != r.sites[j].Path {
			return r.sites[i].Path < r.sites[j].Path
		}
		return r.sites[i].Line < r.sites[j].Line
	})
	fmt.Fprintln(w, "# iac-codemod add-validate-plan report")
	fmt.Fprintln(w)
	mode := "dry-run"
	if opts != nil && opts.Fix {
		mode = "fix"
	}
	fmt.Fprintf(w, "Mode:    %s\n", mode)
	fmt.Fprintf(w, "Sites:   %d\n", len(r.sites))
	fmt.Fprintf(w, "Errors:  %d\n", len(r.errors))
	fmt.Fprintln(w)

	groups := map[validatePlanClassification][]validatePlanSite{}
	for _, s := range r.sites {
		groups[s.Class] = append(groups[s.Class], s)
	}
	order := []validatePlanClassification{
		validatePlanMissing,
		validatePlanAlreadyImplemented,
		validatePlanSkipped,
	}
	headers := map[validatePlanClassification]string{
		validatePlanMissing:            "Missing ValidatePlan (stub injection candidate)",
		validatePlanAlreadyImplemented: "Already-implemented (no-op)",
		validatePlanSkipped:            "Skipped (// wfctl:skip-iac-codemod)",
	}
	for _, c := range order {
		sites := groups[c]
		if len(sites) == 0 {
			continue
		}
		fmt.Fprintf(w, "## %s\n\n", headers[c])
		for _, s := range sites {
			suffix := ""
			if c == validatePlanMissing && s.Inserted {
				suffix = " (stub inserted)"
			}
			fmt.Fprintf(w, "- %s:%d %s %s%s\n", s.Path, s.Line, s.Receiver, s.Class, suffix)
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
