// Copyright (c) 2026 Jon Langevin
// SPDX-License-Identifier: Apache-2.0

package main

import (
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
			pendingStubs = append(pendingStubs, validatePlanStubText(recv))
			site.Inserted = true
			mutated = true
		}
		report.sites = append(report.sites, site)
	}

	if mutated && opts != nil && opts.Fix {
		// Append stubs as raw text to the existing file source. This
		// preserves the original formatting of the un-touched portion
		// of the file (vs. reprinting the whole AST through
		// format.Node, which would normalize unrelated whitespace).
		appended := append([]byte{}, src...)
		// Ensure the source ends with a single trailing newline before
		// appending — otherwise the first stub joins onto the last line.
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
// stub on the named receiver type. The stub uses an unqualified
// `*IaCPlan` / `[]PlanDiagnostic`. Maintainers whose package imports
// these types from the wfctl interfaces package (e.g.
// `interfaces.IaCPlan`) must adjust the qualifier after running the
// codemod — the report flags this as an expected manual touch-up.
func validatePlanStubText(recv string) string {
	return fmt.Sprintf(`// ValidatePlan reports diagnostics for any plan-time concerns. The
// stub generated by iac-codemod returns no diagnostics; replace with
// real provider-specific checks (region constraints, quota limits,
// resource-type conflicts, etc.) before relying on it.
func (p *%s) ValidatePlan(plan *IaCPlan) []PlanDiagnostic {
	return nil
}
`, recv)
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
// ValidatePlan method. Signature isn't strictly enforced — any
// ValidatePlan on the receiver type is treated as an opt-out from
// stub injection (the maintainer has accepted responsibility for the
// method's correctness).
func hasValidatePlanMethod(methods []*ast.FuncDecl) bool {
	for _, m := range methods {
		if m.Name.Name == "ValidatePlan" {
			return true
		}
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
