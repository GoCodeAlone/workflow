package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"go/doc"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocsGenerateHelpIncludesAPIDocFlags(t *testing.T) {
	out, err := captureStderr(t, func() error {
		return runDocsGenerate([]string{"--help"})
	})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("expected flag.ErrHelp, got %v", err)
	}
	for _, want := range []string{"--source", "--out", "--module", "--version", "--packages", "--registry", "--subjects"} {
		if !strings.Contains(out, strings.TrimPrefix(want, "--")) {
			t.Fatalf("help output missing %s:\n%s", want, out)
		}
	}
}

func TestDocsGenerateWorkflowPackages(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	_, err = captureStdout(t, func() error {
		return runDocsGenerate([]string{
			"--source", repoRoot,
			"--out", outDir,
			"--module", "github.com/GoCodeAlone/workflow",
			"--version", "v0.75.0",
			"--packages", "plugin,plugin/sdk,plugin/external/sdk",
		})
	})
	if err != nil {
		t.Fatalf("docs generate: %v", err)
	}

	metaPath := filepath.Join(outDir, "versions.json")
	rawMeta, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read versions.json: %v", err)
	}
	var meta struct {
		SchemaVersion int `json:"schemaVersion"`
		Subject       string
		Versions      map[string][]string `json:"versions"`
		Packages      []struct {
			Subject    string `json:"subject"`
			ImportPath string `json:"importPath"`
			Version    string `json:"version"`
			Path       string `json:"path"`
			Synopsis   string `json:"synopsis"`
		} `json:"packages"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(rawMeta, &meta); err != nil {
		t.Fatalf("parse versions.json: %v", err)
	}
	if meta.SchemaVersion != 1 {
		t.Fatalf("schemaVersion = %d, want 1", meta.SchemaVersion)
	}
	if got := meta.Versions["workflow"]; len(got) != 1 || got[0] != "v0.75.0" {
		t.Fatalf("workflow versions = %v, want [v0.75.0]", got)
	}
	if len(meta.Packages) != 3 {
		t.Fatalf("packages = %d, want 3 (%+v)", len(meta.Packages), meta.Packages)
	}
	if len(meta.Warnings) != 0 {
		t.Fatalf("warnings = %v, want none", meta.Warnings)
	}

	docPath := filepath.Join(outDir, "workflow", "latest", "plugin", "index.md")
	if mode := statFileMode(t, docPath); mode != 0o644 {
		t.Fatalf("generated doc mode = %04o, want 0644", mode)
	}
	if mode := statFileMode(t, metaPath); mode != 0o644 {
		t.Fatalf("versions.json mode = %04o, want 0644", mode)
	}
	rawDoc, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read generated plugin doc: %v", err)
	}
	doc := string(rawDoc)
	for _, want := range []string{
		"# package plugin",
		"Import path: `github.com/GoCodeAlone/workflow/plugin`",
		"Version: `v0.75.0`",
		"https://github.com/GoCodeAlone/workflow/tree/v0.75.0/plugin",
		"## Types",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("generated doc missing %q:\n%s", want, doc)
		}
	}
}

func TestDocsGenerateRegistryPlugins(t *testing.T) {
	pluginRepo := createDocsPluginRepo(t, "github.com/GoCodeAlone/workflow-plugin-alpha", "alpha", "v0.1.0")
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	registry := `{
  "plugins": [
    {
      "name": "workflow-plugin-alpha",
      "version": "v0.1.0",
      "repository": "https://github.com/GoCodeAlone/workflow-plugin-alpha",
      "source": ` + strconvQuote(pluginRepo) + `
    },
    {
      "name": "workflow-plugin-missing",
      "version": "v0.2.0",
      "repository": "https://github.com/GoCodeAlone/workflow-plugin-missing",
      "source": ` + strconvQuote(pluginRepo) + `
    },
    {
      "name": "workflow-plugin-outside",
      "version": "v0.1.0",
      "repository": "https://github.com/Other/workflow-plugin-outside",
      "source": ` + strconvQuote(pluginRepo) + `
    },
    {
      "name": "workflow-plugin-..",
      "version": "v0.1.0",
      "repository": "https://github.com/GoCodeAlone/workflow-plugin-dotdot",
      "source": ` + strconvQuote(pluginRepo) + `
    },
    {
      "name": "workflow-plugin-untrusted-source",
      "version": "v0.1.0",
      "repository": "https://github.com/GoCodeAlone/workflow-plugin-untrusted-source",
      "source": "https://github.com/Other/workflow-plugin-untrusted-source"
    }
  ]
}`
	if err := os.WriteFile(registryPath, []byte(registry), 0o600); err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "cache")
	_, err := captureStdout(t, func() error {
		return runDocsGenerate([]string{
			"--source", ".",
			"--out", outDir,
			"--module", "github.com/GoCodeAlone/workflow",
			"--version", "v0.75.0",
			"--registry", registryPath,
			"--cache-dir", cacheDir,
			"--subjects", "plugins",
		})
	})
	if err != nil {
		t.Fatalf("docs generate registry plugins: %v", err)
	}

	docPath := filepath.Join(outDir, "plugins", "alpha", "latest", "index.md")
	rawDoc, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read generated plugin doc: %v", err)
	}
	doc := string(rawDoc)
	for _, want := range []string{
		"# package alpha",
		"Import path: `github.com/GoCodeAlone/workflow-plugin-alpha`",
		"Version: `v0.1.0`",
		"https://github.com/GoCodeAlone/workflow-plugin-alpha/tree/v0.1.0",
		"## Functions",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("generated plugin doc missing %q:\n%s", want, doc)
		}
	}

	rawMeta, err := os.ReadFile(filepath.Join(outDir, "versions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta docsAPIMetadata
	if err := json.Unmarshal(rawMeta, &meta); err != nil {
		t.Fatal(err)
	}
	if got := meta.Versions["plugins/alpha"]; len(got) != 1 || got[0] != "v0.1.0" {
		t.Fatalf("plugins/alpha versions = %v, want [v0.1.0]", got)
	}
	if len(meta.Packages) != 1 || meta.Packages[0].Path != "plugins/alpha/latest/index.md" {
		t.Fatalf("packages = %+v, want generated alpha plugin route", meta.Packages)
	}
	joinedWarnings := strings.Join(meta.Warnings, "\n")
	for _, want := range []string{"workflow-plugin-missing", "v0.2.0", "workflow-plugin-outside", "trust boundary", "invalid plugin slug", "workflow-plugin-untrusted-source"} {
		if !strings.Contains(joinedWarnings, want) {
			t.Fatalf("warnings missing %q:\n%s", want, joinedWarnings)
		}
	}
}

func TestLimitDocsVersionLines(t *testing.T) {
	versions := map[string][]string{
		"workflow": {
			"v0.74.3",
			"v0.74.2",
			"v0.75.0",
			"v1.0.0",
			"v0.73.9",
			"v0.75.0",
		},
	}

	limitDocsVersionLines(versions, 2)

	want := []string{"v1.0.0", "v0.75.0"}
	got := versions["workflow"]
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("workflow versions = %v, want %v", got, want)
	}
}

func TestDocsPluginCloneSourceRejectsLocalSourceFromRemoteRegistry(t *testing.T) {
	manifest := &RegistryManifest{
		Name:       "workflow-plugin-alpha",
		Repository: "https://github.com/GoCodeAlone/workflow-plugin-alpha",
		Source:     "../workflow-plugin-alpha",
	}

	if _, err := docsPluginCloneSource(manifest, false); err == nil {
		t.Fatal("expected remote registry local source to be rejected")
	}

	source, err := docsPluginCloneSource(manifest, true)
	if err != nil {
		t.Fatalf("local registry source rejected: %v", err)
	}
	if source != manifest.Source {
		t.Fatalf("source = %q, want %q", source, manifest.Source)
	}
}

func TestTrustedGoCodeAloneRepoRejectsDotSegments(t *testing.T) {
	for _, repo := range []string{
		"https://github.com/GoCodeAlone/../Other/workflow-plugin-alpha",
		"https://github.com/GoCodeAlone/%2e%2e/Other/workflow-plugin-alpha",
		"https://github.com/GoCodeAlone/./workflow-plugin-alpha",
	} {
		if trustedGoCodeAloneRepo(repo) {
			t.Fatalf("trustedGoCodeAloneRepo(%q) = true, want false", repo)
		}
	}
	if !trustedGoCodeAloneRepo("https://github.com/GoCodeAlone/workflow-plugin-alpha") {
		t.Fatal("expected normal GoCodeAlone HTTPS repo to be trusted")
	}
}

func TestCheckoutDocsPluginRepoReusesCache(t *testing.T) {
	pluginRepo := createDocsPluginRepo(t, "github.com/GoCodeAlone/workflow-plugin-alpha", "alpha", "v0.1.0")
	cacheDir := t.TempDir()
	manifest := &RegistryManifest{
		Name:       "workflow-plugin-alpha",
		Version:    "v0.1.0",
		Repository: "https://github.com/GoCodeAlone/workflow-plugin-alpha",
		Source:     pluginRepo,
	}

	checkout, err := checkoutDocsPluginRepo(context.Background(), manifest, cacheDir, true)
	if err != nil {
		t.Fatalf("first checkout: %v", err)
	}
	sentinel := filepath.Join(checkout, ".git", "wfctl-cache-sentinel")
	if err := os.WriteFile(sentinel, []byte("cached"), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	secondCheckout, err := checkoutDocsPluginRepo(context.Background(), manifest, cacheDir, true)
	if err != nil {
		t.Fatalf("second checkout: %v", err)
	}
	if secondCheckout != checkout {
		t.Fatalf("checkout path = %q, want %q", secondCheckout, checkout)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("cache sentinel was removed: %v", err)
	}
}

func TestRenderPackageMarkdownWarnings(t *testing.T) {
	rendered := renderPackageMarkdown(token.NewFileSet(), &doc.Package{Name: "alpha"}, docsAPIPackageMeta{
		ImportPath: "github.com/GoCodeAlone/workflow-plugin-alpha",
		Version:    "v0.1.0",
	}, "https://github.com/GoCodeAlone/workflow-plugin-alpha/tree/v0.1.0", []string{"missing package docs"})

	if !strings.Contains(rendered, "- missing package docs") {
		t.Fatalf("rendered doc missing warning:\n%s", rendered)
	}
	if strings.Contains(rendered, "## Warnings\n\nNone") {
		t.Fatalf("rendered doc still claims warnings are none:\n%s", rendered)
	}
}

func createDocsPluginRepo(t *testing.T, modulePath, packageName, tag string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module "+modulePath+"\n\ngo 1.26\n")
	write(packageName+".go", "package "+packageName+"\n\n// PluginName returns the fixture plugin name.\nfunc PluginName() string { return "+strconvQuote(packageName)+" }\n")
	runDocsTestGit(t, dir, "init")
	runDocsTestGit(t, dir, "config", "user.email", "docs@example.test")
	runDocsTestGit(t, dir, "config", "user.name", "Docs Test")
	runDocsTestGit(t, dir, "add", ".")
	runDocsTestGit(t, dir, "commit", "-m", "initial")
	runDocsTestGit(t, dir, "tag", tag)
	return dir
}

func runDocsTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func statFileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}
