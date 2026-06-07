package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type docsGenerateAPIOptions struct {
	Source          string
	Out             string
	Module          string
	Version         string
	Packages        string
	Registry        string
	CacheDir        string
	Subjects        string
	MaxVersionLines int
}

type docsAPIMetadata struct {
	SchemaVersion int                  `json:"schemaVersion"`
	GeneratedAt   string               `json:"generatedAt"`
	Subject       string               `json:"subject"`
	Subjects      []string             `json:"subjects"`
	Versions      map[string][]string  `json:"versions"`
	Packages      []docsAPIPackageMeta `json:"packages"`
	Warnings      []string             `json:"warnings"`
}

type docsAPIPackageMeta struct {
	Subject    string `json:"subject"`
	Name       string `json:"name"`
	ImportPath string `json:"importPath"`
	Version    string `json:"version"`
	Path       string `json:"path"`
	Synopsis   string `json:"synopsis,omitempty"`
}

type goListPackage struct {
	Dir        string `json:"Dir"`
	ImportPath string `json:"ImportPath"`
	Name       string `json:"Name"`
	Doc        string `json:"Doc"`
}

type renderedDocsPackage struct {
	Meta     docsAPIPackageMeta
	Doc      string
	Warnings []string
}

func runDocsGenerateAPI(opts docsGenerateAPIOptions) error {
	if strings.TrimSpace(opts.Source) == "" {
		return fmt.Errorf("docs generate: --source is required for Go API docs")
	}
	if strings.TrimSpace(opts.Out) == "" {
		return fmt.Errorf("docs generate: --out is required for Go API docs")
	}
	if strings.TrimSpace(opts.Module) == "" {
		return fmt.Errorf("docs generate: --module is required for Go API docs")
	}
	if strings.TrimSpace(opts.Version) == "" {
		return fmt.Errorf("docs generate: --version is required for Go API docs")
	}
	packages := splitDocsCSV(opts.Packages)
	if len(packages) == 0 {
		return fmt.Errorf("docs generate: --packages must include at least one package")
	}

	source, err := filepath.Abs(opts.Source)
	if err != nil {
		return fmt.Errorf("docs generate: resolve source: %w", err)
	}
	out, err := filepath.Abs(opts.Out)
	if err != nil {
		return fmt.Errorf("docs generate: resolve out: %w", err)
	}
	if err := os.MkdirAll(out, 0o750); err != nil {
		return fmt.Errorf("docs generate: create output dir: %w", err)
	}

	ctx := context.Background()
	rendered := make([]renderedDocsPackage, 0, len(packages))
	var warnings []string
	for _, pkg := range packages {
		docPkg, err := renderWorkflowAPIPackage(ctx, source, opts.Module, opts.Version, pkg)
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		rendered = append(rendered, docPkg)
		warnings = append(warnings, docPkg.Warnings...)
	}
	if len(rendered) == 0 {
		return fmt.Errorf("docs generate: no packages generated")
	}

	for _, pkg := range rendered {
		dest := filepath.Join(out, filepath.FromSlash(pkg.Meta.Path))
		if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
			return fmt.Errorf("docs generate: create %s: %w", filepath.Dir(dest), err)
		}
		if err := os.WriteFile(dest, []byte(pkg.Doc), 0o640); err != nil {
			return fmt.Errorf("docs generate: write %s: %w", dest, err)
		}
		fmt.Printf("  create  %s\n", dest)
	}

	meta := docsAPIMetadata{
		SchemaVersion: 1,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Subject:       "workflow",
		Subjects:      splitDocsCSV(opts.Subjects),
		Versions:      map[string][]string{"workflow": {opts.Version}},
		Packages:      make([]docsAPIPackageMeta, 0, len(rendered)),
		Warnings:      warnings,
	}
	for _, pkg := range rendered {
		meta.Packages = append(meta.Packages, pkg.Meta)
	}
	sort.Slice(meta.Packages, func(i, j int) bool {
		return meta.Packages[i].ImportPath < meta.Packages[j].ImportPath
	})
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("docs generate: marshal metadata: %w", err)
	}
	metaPath := filepath.Join(out, "versions.json")
	if err := os.WriteFile(metaPath, append(metaBytes, '\n'), 0o640); err != nil {
		return fmt.Errorf("docs generate: write versions.json: %w", err)
	}
	fmt.Printf("  create  %s\n", metaPath)
	fmt.Printf("\nGenerated %d Go API package doc(s) in %s\n", len(rendered), out)
	return nil
}

func renderWorkflowAPIPackage(ctx context.Context, source, modulePath, version, pkgRel string) (renderedDocsPackage, error) {
	pkgRel = strings.Trim(strings.TrimSpace(pkgRel), "/")
	if pkgRel == "" {
		return renderedDocsPackage{}, fmt.Errorf("docs generate: empty package path")
	}
	listPkg, err := goListAPIPackage(ctx, source, modulePath, pkgRel)
	if err != nil {
		return renderedDocsPackage{}, fmt.Errorf("docs generate: go list %s: %w", pkgRel, err)
	}
	docPkg, fset, err := parseDocPackage(listPkg)
	if err != nil {
		return renderedDocsPackage{}, fmt.Errorf("docs generate: parse %s: %w", pkgRel, err)
	}
	route := "workflow/latest/" + strings.Trim(pkgRel, "/") + "/index.md"
	synopsis := doc.Synopsis(docPkg.Doc)
	if synopsis == "" {
		synopsis = listPkg.Doc
	}
	meta := docsAPIPackageMeta{
		Subject:    "workflow",
		Name:       docPkg.Name,
		ImportPath: listPkg.ImportPath,
		Version:    version,
		Path:       route,
		Synopsis:   synopsis,
	}
	return renderedDocsPackage{
		Meta: meta,
		Doc:  renderPackageMarkdown(fset, docPkg, meta, workflowSourceLink(version, pkgRel)),
	}, nil
}

func goListAPIPackage(ctx context.Context, source, modulePath, pkgRel string) (goListPackage, error) {
	importPath := strings.TrimRight(modulePath, "/") + "/" + strings.Trim(pkgRel, "/")
	cmd := exec.CommandContext(ctx, "go", "list", "-json", importPath) // #nosec G204 -- fixed go command with package arg from CLI input.
	cmd.Dir = source
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return goListPackage{}, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var pkg goListPackage
	if err := json.Unmarshal(out, &pkg); err != nil {
		return goListPackage{}, err
	}
	return pkg, nil
}

func parseDocPackage(listPkg goListPackage) (*doc.Package, *token.FileSet, error) {
	fset := token.NewFileSet()
	filter := func(info os.FileInfo) bool {
		name := info.Name()
		return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
	}
	pkgs, err := parser.ParseDir(fset, listPkg.Dir, filter, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}
	astPkg := pkgs[listPkg.Name]
	if astPkg == nil {
		for _, candidate := range pkgs {
			astPkg = candidate
			break
		}
	}
	if astPkg == nil {
		return nil, nil, fmt.Errorf("no Go package found in %s", listPkg.Dir)
	}
	return doc.New(astPkg, listPkg.ImportPath, 0), fset, nil
}

func renderPackageMarkdown(fset *token.FileSet, pkg *doc.Package, meta docsAPIPackageMeta, sourceLink string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# package %s\n\n", pkg.Name)
	fmt.Fprintf(&b, "Import path: `%s`\n\n", meta.ImportPath)
	fmt.Fprintf(&b, "Version: `%s`\n\n", meta.Version)
	fmt.Fprintf(&b, "Source: %s\n\n", sourceLink)
	b.WriteString("## Warnings\n\nNone\n\n")
	if strings.TrimSpace(pkg.Doc) != "" {
		b.WriteString("## Synopsis\n\n")
		b.WriteString(strings.TrimSpace(pkg.Doc))
		b.WriteString("\n\n")
	}
	renderValues(&b, fset, "Constants", pkg.Consts)
	renderValues(&b, fset, "Variables", pkg.Vars)
	renderFuncs(&b, fset, "Functions", pkg.Funcs)
	renderTypes(&b, fset, pkg.Types)
	return b.String()
}

func renderValues(b *strings.Builder, fset *token.FileSet, heading string, values []*doc.Value) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(b, "## %s\n\n", heading)
	for _, value := range values {
		if strings.TrimSpace(value.Doc) != "" {
			b.WriteString(strings.TrimSpace(value.Doc))
			b.WriteString("\n\n")
		}
		writeDecl(b, fset, value.Decl)
	}
}

func renderFuncs(b *strings.Builder, fset *token.FileSet, heading string, funcs []*doc.Func) {
	if len(funcs) == 0 {
		return
	}
	fmt.Fprintf(b, "## %s\n\n", heading)
	for _, fn := range funcs {
		fmt.Fprintf(b, "### func %s\n\n", fn.Name)
		if strings.TrimSpace(fn.Doc) != "" {
			b.WriteString(strings.TrimSpace(fn.Doc))
			b.WriteString("\n\n")
		}
		writeDecl(b, fset, fn.Decl)
	}
}

func renderTypes(b *strings.Builder, fset *token.FileSet, types []*doc.Type) {
	if len(types) == 0 {
		return
	}
	b.WriteString("## Types\n\n")
	for _, typ := range types {
		fmt.Fprintf(b, "### type %s\n\n", typ.Name)
		if strings.TrimSpace(typ.Doc) != "" {
			b.WriteString(strings.TrimSpace(typ.Doc))
			b.WriteString("\n\n")
		}
		writeDecl(b, fset, typ.Decl)
		renderFuncs(b, fset, "Functions", typ.Funcs)
		renderFuncs(b, fset, "Methods", typ.Methods)
	}
}

func writeDecl(b *strings.Builder, fset *token.FileSet, decl ast.Node) {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, decl); err != nil {
		return
	}
	b.WriteString("```go\n")
	b.WriteString(strings.TrimSpace(buf.String()))
	b.WriteString("\n```\n\n")
}

func workflowSourceLink(version, pkgRel string) string {
	ref := strings.TrimSpace(version)
	if ref == "" || ref == "latest" {
		ref = "main"
	}
	return "https://github.com/GoCodeAlone/workflow/tree/" + ref + "/" + strings.Trim(pkgRel, "/")
}

func splitDocsCSV(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
