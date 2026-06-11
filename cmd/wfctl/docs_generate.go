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
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/semver"
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
	Subject          string `json:"subject"`
	Name             string `json:"name"`
	ImportPath       string `json:"importPath"`
	Version          string `json:"version"`
	GenerationSource string `json:"generationSource"`
	Repository       string `json:"repository,omitempty"`
	Path             string `json:"path"`
	Synopsis         string `json:"synopsis,omitempty"`
}

type goListPackage struct {
	Dir        string   `json:"Dir"`
	ImportPath string   `json:"ImportPath"`
	Name       string   `json:"Name"`
	Doc        string   `json:"Doc"`
	GoFiles    []string `json:"GoFiles"`
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
	subjects := splitDocsCSV(opts.Subjects)
	if len(subjects) == 0 {
		subjects = []string{"workflow"}
	}
	packages := splitDocsCSV(opts.Packages)
	if docsContainsString(subjects, "workflow") && len(packages) == 0 {
		return fmt.Errorf("docs generate: --packages must include at least one package")
	}
	if docsContainsString(subjects, "plugins") && strings.TrimSpace(opts.Registry) == "" {
		return fmt.Errorf("docs generate: --registry is required when --subjects includes plugins")
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
	if docsContainsString(subjects, "workflow") {
		for _, pkg := range packages {
			docPkg, err := renderWorkflowAPIPackage(ctx, source, opts.Module, opts.Version, pkg)
			if err != nil {
				warnings = append(warnings, err.Error())
				continue
			}
			rendered = append(rendered, docPkg)
			warnings = append(warnings, docPkg.Warnings...)
		}
	}
	if docsContainsString(subjects, "plugins") {
		pluginDocs, pluginWarnings, err := renderRegistryPluginAPIPackages(ctx, opts)
		if err != nil {
			return err
		}
		rendered = append(rendered, pluginDocs...)
		warnings = append(warnings, pluginWarnings...)
	}
	if len(rendered) == 0 {
		return fmt.Errorf("docs generate: no packages generated")
	}

	for i := range rendered {
		pkg := &rendered[i]
		dest := filepath.Join(out, filepath.FromSlash(pkg.Meta.Path))
		if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
			return fmt.Errorf("docs generate: create %s: %w", filepath.Dir(dest), err)
		}
		// #nosec G306 -- generated documentation artifacts are intended to be readable by tooling.
		if err := os.WriteFile(dest, []byte(pkg.Doc), 0o644); err != nil {
			return fmt.Errorf("docs generate: write %s: %w", dest, err)
		}
		fmt.Printf("  create  %s\n", dest)
	}

	meta := docsAPIMetadata{
		SchemaVersion: 1,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Subject:       subjects[0],
		Subjects:      subjects,
		Versions:      map[string][]string{},
		Packages:      make([]docsAPIPackageMeta, 0, len(rendered)),
		Warnings:      warnings,
	}
	for i := range rendered {
		pkg := &rendered[i]
		meta.Packages = append(meta.Packages, pkg.Meta)
		key := pkg.Meta.Subject
		if pkg.Meta.Subject == "plugin" {
			key = "plugins/" + pluginSlug(pkg.Meta.Name)
		}
		if pkg.Meta.Version != "" && !docsContainsString(meta.Versions[key], pkg.Meta.Version) {
			meta.Versions[key] = append(meta.Versions[key], pkg.Meta.Version)
		}
	}
	sort.Slice(meta.Packages, func(i, j int) bool {
		return meta.Packages[i].ImportPath < meta.Packages[j].ImportPath
	})
	limitDocsVersionLines(meta.Versions, opts.MaxVersionLines)
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("docs generate: marshal metadata: %w", err)
	}
	metaPath := filepath.Join(out, "versions.json")
	// #nosec G306 -- generated documentation metadata is intended to be readable by tooling.
	if err := os.WriteFile(metaPath, append(metaBytes, '\n'), 0o644); err != nil {
		return fmt.Errorf("docs generate: write versions.json: %w", err)
	}
	fmt.Printf("  create  %s\n", metaPath)
	indexPath := filepath.Join(out, "index.json")
	// #nosec G306 -- generated documentation metadata is intended to be readable by tooling.
	if err := os.WriteFile(indexPath, append(metaBytes, '\n'), 0o644); err != nil {
		return fmt.Errorf("docs generate: write index.json: %w", err)
	}
	fmt.Printf("  create  %s\n", indexPath)
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
	synopsis := docPkg.Synopsis(docPkg.Doc)
	if synopsis == "" {
		synopsis = listPkg.Doc
	}
	meta := docsAPIPackageMeta{
		Subject:          "workflow",
		Name:             docPkg.Name,
		ImportPath:       listPkg.ImportPath,
		Version:          version,
		GenerationSource: "local",
		Repository:       "https://github.com/GoCodeAlone/workflow",
		Path:             route,
		Synopsis:         synopsis,
	}
	return renderedDocsPackage{
		Meta: meta,
		Doc:  renderPackageMarkdown(fset, docPkg, meta, workflowSourceLink(version, pkgRel), nil),
	}, nil
}

func goListAPIPackage(ctx context.Context, source, modulePath, pkgRel string) (goListPackage, error) {
	importPath := strings.TrimRight(modulePath, "/")
	if strings.Trim(pkgRel, "/") != "" && pkgRel != "." {
		importPath += "/" + strings.Trim(pkgRel, "/")
	}
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

func goListAPIPackages(ctx context.Context, source string) ([]goListPackage, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-json", "./...") // #nosec G204 -- fixed go command.
	cmd.Dir = source
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	var packages []goListPackage
	for {
		var pkg goListPackage
		if err := dec.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if pkg.Name == "main" || len(pkg.GoFiles) == 0 {
			continue
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}

func parseDocPackage(listPkg goListPackage) (*doc.Package, *token.FileSet, error) {
	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(listPkg.GoFiles))
	for _, name := range listPkg.GoFiles {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(listPkg.Dir, name)
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil, nil, err
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("no Go package found in %s", listPkg.Dir)
	}
	docPkg, err := doc.NewFromFiles(fset, files, listPkg.ImportPath)
	if err != nil {
		return nil, nil, err
	}
	return docPkg, fset, nil
}

func renderPackageMarkdown(fset *token.FileSet, pkg *doc.Package, meta docsAPIPackageMeta, sourceLink string, warnings []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# package %s\n\n", pkg.Name)
	fmt.Fprintf(&b, "Import path: `%s`\n\n", meta.ImportPath)
	fmt.Fprintf(&b, "Version: `%s`\n\n", meta.Version)
	fmt.Fprintf(&b, "Source: %s\n\n", sourceLink)
	b.WriteString("## Warnings\n\n")
	if len(warnings) == 0 {
		b.WriteString("None\n\n")
	} else {
		for _, warning := range warnings {
			fmt.Fprintf(&b, "- %s\n", warning)
		}
		b.WriteString("\n")
	}
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

func renderRegistryPluginAPIPackages(ctx context.Context, opts docsGenerateAPIOptions) ([]renderedDocsPackage, []string, error) {
	manifests, err := loadDocsRegistry(ctx, opts.Registry)
	if err != nil {
		return nil, nil, fmt.Errorf("docs generate: load registry: %w", err)
	}
	cacheDir := opts.CacheDir
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "wfctl-docs-plugin-cache")
	}
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return nil, nil, fmt.Errorf("docs generate: create cache dir: %w", err)
	}
	var rendered []renderedDocsPackage
	var warnings []string
	allowLocalPluginSources := docsRegistryLocalSource(opts.Registry)
	for i := range manifests {
		manifest := &manifests[i]
		if strings.TrimSpace(manifest.Name) == "" {
			warnings = append(warnings, "plugin registry entry missing name")
			continue
		}
		if !trustedGoCodeAloneRepo(manifest.Repository) {
			warnings = append(warnings, fmt.Sprintf("%s skipped: repository %q is outside the GoCodeAlone GitHub trust boundary", manifest.Name, manifest.Repository))
			continue
		}
		if strings.TrimSpace(manifest.Version) == "" {
			warnings = append(warnings, fmt.Sprintf("%s skipped: missing version", manifest.Name))
			continue
		}
		if _, err := safeDocsPluginSlug(manifest.Name); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s %s skipped: %v", manifest.Name, manifest.Version, err))
			continue
		}
		checkout, err := checkoutDocsPluginRepo(ctx, manifest, cacheDir, allowLocalPluginSources)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s %s skipped: %v", manifest.Name, manifest.Version, err))
			continue
		}
		modulePath, err := readGoModulePath(filepath.Join(checkout, "go.mod"))
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s %s skipped: %v", manifest.Name, manifest.Version, err))
			continue
		}
		docPkgs, err := renderPluginAPIPackages(ctx, checkout, modulePath, manifest)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s %s skipped: %v", manifest.Name, manifest.Version, err))
			continue
		}
		rendered = append(rendered, docPkgs...)
	}
	return rendered, warnings, nil
}

func loadDocsRegistry(ctx context.Context, ref string) ([]RegistryManifest, error) {
	var data []byte
	var err error
	if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, ref, nil)
		if reqErr != nil {
			return nil, reqErr
		}
		client := &http.Client{Timeout: 30 * time.Second}
		resp, httpErr := client.Do(req)
		if httpErr != nil {
			return nil, httpErr
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("registry returned HTTP %d", resp.StatusCode)
		}
		data, err = io.ReadAll(resp.Body)
	} else {
		data, err = os.ReadFile(ref)
	}
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Plugins []RegistryManifest `json:"plugins"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil && envelope.Plugins != nil {
		return envelope.Plugins, nil
	}
	var manifests []RegistryManifest
	if err := json.Unmarshal(data, &manifests); err != nil {
		return nil, err
	}
	return manifests, nil
}

func checkoutDocsPluginRepo(ctx context.Context, manifest *RegistryManifest, cacheDir string, allowLocalSource bool) (string, error) {
	slug, err := safeDocsPluginSlug(manifest.Name)
	if err != nil {
		return "", err
	}
	dest, err := docsPluginCacheDestination(cacheDir, slug)
	if err != nil {
		return "", err
	}
	cloneSource, err := docsPluginCloneSource(manifest, allowLocalSource)
	if err != nil {
		return "", err
	}
	if docsPluginRepoExists(dest) {
		if err := refreshDocsPluginRepo(ctx, dest, cloneSource, manifest.Version); err == nil {
			return dest, nil
		}
	}
	if err := os.RemoveAll(dest); err != nil {
		return "", err
	}
	args := []string{"clone", "--depth", "1", "--branch", manifest.Version, cloneSource, dest}
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- fixed git command; args are not shell-expanded.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git clone tag %s: %w: %s", manifest.Version, err, strings.TrimSpace(stderr.String()))
	}
	return dest, nil
}

func renderPluginAPIPackages(ctx context.Context, checkout, modulePath string, manifest *RegistryManifest) ([]renderedDocsPackage, error) {
	listPkgs, err := goListAPIPackages(ctx, checkout)
	if err != nil {
		return nil, err
	}
	sortPluginAPIPackages(listPkgs, modulePath)
	slug, err := safeDocsPluginSlug(manifest.Name)
	if err != nil {
		return nil, err
	}
	rendered := make([]renderedDocsPackage, 0, len(listPkgs))
	for i, listPkg := range listPkgs {
		docPkg, fset, err := parseDocPackage(listPkg)
		if err != nil {
			return nil, err
		}
		pkgRel := strings.TrimPrefix(listPkg.ImportPath, strings.TrimRight(modulePath, "/"))
		pkgRel = strings.Trim(pkgRel, "/")
		route := pluginAPIDocRoute(slug, pkgRel, i == 0)
		synopsis := docPkg.Synopsis(docPkg.Doc)
		if synopsis == "" {
			synopsis = listPkg.Doc
		}
		meta := docsAPIPackageMeta{
			Subject:          "plugin",
			Name:             manifest.Name,
			ImportPath:       listPkg.ImportPath,
			Version:          manifest.Version,
			GenerationSource: "release",
			Repository:       manifest.Repository,
			Path:             route,
			Synopsis:         synopsis,
		}
		rendered = append(rendered, renderedDocsPackage{
			Meta: meta,
			Doc:  renderPackageMarkdown(fset, docPkg, meta, pluginSourceLink(manifest.Repository, manifest.Version, pkgRel), nil),
		})
	}
	if len(rendered) == 0 {
		return nil, fmt.Errorf("no non-command Go packages found")
	}
	return rendered, nil
}

func sortPluginAPIPackages(packages []goListPackage, modulePath string) {
	modulePath = strings.TrimRight(modulePath, "/")
	sort.SliceStable(packages, func(i, j int) bool {
		leftRank := pluginAPIPackageRank(packages[i], modulePath)
		rightRank := pluginAPIPackageRank(packages[j], modulePath)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return packages[i].ImportPath < packages[j].ImportPath
	})
}

func pluginAPIPackageRank(pkg goListPackage, modulePath string) int {
	switch pkg.ImportPath {
	case modulePath:
		return 0
	case modulePath + "/internal":
		return 1
	default:
		if strings.TrimSpace(pkg.Doc) != "" {
			return 2
		}
		return 3
	}
}

func pluginAPIDocRoute(slug, pkgRel string, primary bool) string {
	if primary {
		return "plugins/" + slug + "/latest/index.md"
	}
	return "plugins/" + slug + "/latest/" + strings.Trim(pkgRel, "/") + "/index.md"
}

func readGoModulePath(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("module path not found in %s", path)
}

func trustedGoCodeAloneRepo(repo string) bool {
	parsed, err := url.Parse(strings.TrimSpace(repo))
	if err != nil {
		return false
	}
	if parsed.Scheme != "https" || parsed.Host != "github.com" {
		return false
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) < 2 || segments[0] != "GoCodeAlone" {
		return false
	}
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func pluginSlug(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, ".git")
	name = strings.TrimPrefix(name, "workflow-plugin-")
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
		name = strings.TrimPrefix(name, "workflow-plugin-")
	}
	return name
}

func safeDocsPluginSlug(name string) (string, error) {
	slug := pluginSlug(name)
	if slug == "" || slug == "." || slug == ".." {
		return "", fmt.Errorf("invalid plugin slug %q", slug)
	}
	if strings.ContainsAny(slug, `/\`) {
		return "", fmt.Errorf("invalid plugin slug %q", slug)
	}
	for i, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		if i > 0 && (r == '-' || r == '_' || r == '.') {
			continue
		}
		return "", fmt.Errorf("invalid plugin slug %q", slug)
	}
	return slug, nil
}

func docsPluginCacheDestination(cacheDir, slug string) (string, error) {
	absCacheDir, err := filepath.Abs(cacheDir)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(absCacheDir, slug)
	rel, err := filepath.Rel(absCacheDir, dest)
	if err != nil {
		return "", err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid plugin cache destination %q", dest)
	}
	return dest, nil
}

func docsPluginRepoExists(dest string) bool {
	info, err := os.Stat(filepath.Join(dest, ".git"))
	return err == nil && info.IsDir()
}

func refreshDocsPluginRepo(ctx context.Context, dest, cloneSource, version string) error {
	remote, err := runDocsGit(ctx, dest, "remote", "get-url", "origin")
	if err != nil {
		return err
	}
	if strings.TrimSpace(remote) != cloneSource {
		return fmt.Errorf("cached repository remote %q does not match %q", strings.TrimSpace(remote), cloneSource)
	}
	tagRef := "refs/tags/" + version + ":refs/tags/" + version
	if _, err := runDocsGit(ctx, dest, "fetch", "--depth", "1", "--force", "origin", tagRef); err != nil {
		return err
	}
	if _, err := runDocsGit(ctx, dest, "checkout", "--detach", version); err != nil {
		return err
	}
	if _, err := runDocsGit(ctx, dest, "reset", "--hard"); err != nil {
		return err
	}
	if _, err := runDocsGit(ctx, dest, "clean", "-fdx"); err != nil {
		return err
	}
	return nil
}

func runDocsGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- fixed git command; args are not shell-expanded.
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

func docsPluginCloneSource(manifest *RegistryManifest, allowLocalSource bool) (string, error) {
	source := strings.TrimSpace(manifest.Source)
	if source == "" {
		source = strings.TrimSpace(manifest.Repository)
	}
	if docsPluginLocalCloneSource(source) {
		if allowLocalSource {
			return source, nil
		}
		return "", fmt.Errorf("source %q is outside the GoCodeAlone GitHub trust boundary", source)
	}
	if trustedGoCodeAloneRepo(source) {
		return source, nil
	}
	return "", fmt.Errorf("source %q is outside the GoCodeAlone GitHub trust boundary", source)
}

func docsRegistryLocalSource(registry string) bool {
	registry = strings.TrimSpace(registry)
	return !strings.HasPrefix(registry, "https://") && !strings.HasPrefix(registry, "http://")
}

func docsPluginLocalCloneSource(source string) bool {
	source = strings.TrimSpace(source)
	if source == "" {
		return false
	}
	if filepath.IsAbs(source) || strings.HasPrefix(source, ".") {
		return true
	}
	if strings.Contains(source, "://") || strings.HasPrefix(source, "git@") || strings.Contains(source, ":") {
		return false
	}
	return true
}

func limitDocsVersionLines(versions map[string][]string, maxLines int) {
	for key, values := range versions {
		values = uniqueDocsVersions(values)
		sort.SliceStable(values, func(i, j int) bool {
			return compareDocsVersions(values[i], values[j]) > 0
		})
		if maxLines > 0 && len(values) > maxLines {
			values = values[:maxLines]
		}
		versions[key] = values
	}
}

func uniqueDocsVersions(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func compareDocsVersions(left, right string) int {
	leftValid := semver.IsValid(left)
	rightValid := semver.IsValid(right)
	if leftValid && rightValid {
		return semver.Compare(left, right)
	}
	if leftValid {
		return 1
	}
	if rightValid {
		return -1
	}
	return strings.Compare(left, right)
}

func pluginSourceLink(repository, version, pkgRel string) string {
	repository = strings.TrimSuffix(strings.TrimSpace(repository), ".git")
	if repository == "" {
		return ""
	}
	link := repository + "/tree/" + version
	if pkgRel != "" && pkgRel != "." {
		link += "/" + strings.Trim(pkgRel, "/")
	}
	return link
}

func docsContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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
