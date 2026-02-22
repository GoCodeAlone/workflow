package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeUIDir creates a minimal fake UI source directory with package.json and vite.config.ts.
func makeUIDir(t *testing.T, dir string) {
	t.Helper()
	pkg := `{"name":"test-ui","scripts":{"build":"echo built"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0644); err != nil {
		t.Fatalf("failed to write package.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vite.config.ts"), []byte("export default {}"), 0644); err != nil {
		t.Fatalf("failed to write vite.config.ts: %v", err)
	}
}

// makeDistDir creates a valid dist/ directory matching the validation contract.
func makeDistDir(t *testing.T, uiDir string) {
	t.Helper()
	distDir := filepath.Join(uiDir, "dist")
	assetsDir := filepath.Join(distDir, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatalf("failed to create assets dir: %v", err)
	}
	mustWrite := func(path, content string) {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", path, err)
		}
	}
	mustWrite(filepath.Join(distDir, "index.html"), "<html></html>")
	mustWrite(filepath.Join(assetsDir, "index.js"), "console.log('hi')")
	mustWrite(filepath.Join(assetsDir, "index.css"), "body{}")
}

// --- detectUIFramework ---

func TestDetectUIFramework_Vite(t *testing.T) {
	dir := t.TempDir()
	makeUIDir(t, dir)

	info, err := detectUIFramework(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.framework != "vite" {
		t.Errorf("expected framework=vite, got %s", info.framework)
	}
	if info.packageManager != "npm" {
		t.Errorf("expected packageManager=npm, got %s", info.packageManager)
	}
	if info.buildCmd != "build" {
		t.Errorf("expected buildCmd=build, got %s", info.buildCmd)
	}
}

func TestDetectUIFramework_NoPackageJSON(t *testing.T) {
	dir := t.TempDir()
	_, err := detectUIFramework(dir)
	if err == nil {
		t.Fatal("expected error for missing package.json")
	}
	if !strings.Contains(err.Error(), "package.json") {
		t.Errorf("expected error about package.json, got: %v", err)
	}
}

func TestDetectUIFramework_UsesNpmCI(t *testing.T) {
	dir := t.TempDir()
	makeUIDir(t, dir)
	// Add a package-lock.json to trigger npm ci.
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("failed to write package-lock.json: %v", err)
	}

	info, err := detectUIFramework(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(info.installArgs) == 0 || info.installArgs[0] != "ci" {
		t.Errorf("expected installArgs=[ci] when package-lock.json present, got %v", info.installArgs)
	}
}

func TestDetectUIFramework_FallbackInstall(t *testing.T) {
	dir := t.TempDir()
	makeUIDir(t, dir)
	// No package-lock.json → should fall back to npm install.

	info, err := detectUIFramework(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(info.installArgs) == 0 || info.installArgs[0] != "install" {
		t.Errorf("expected installArgs=[install] when no package-lock.json, got %v", info.installArgs)
	}
}

// --- validateUIBuild ---

func TestValidateUIBuild_Valid(t *testing.T) {
	dir := t.TempDir()
	makeDistDir(t, dir)

	if err := validateUIBuild(dir); err != nil {
		t.Fatalf("expected valid build, got error: %v", err)
	}
}

func TestValidateUIBuild_MissingDist(t *testing.T) {
	dir := t.TempDir()
	err := validateUIBuild(dir)
	if err == nil {
		t.Fatal("expected error for missing dist/")
	}
	if !strings.Contains(err.Error(), "dist/") {
		t.Errorf("expected error about dist/, got: %v", err)
	}
}

func TestValidateUIBuild_MissingIndexHTML(t *testing.T) {
	dir := t.TempDir()
	distDir := filepath.Join(dir, "dist")
	assetsDir := filepath.Join(distDir, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	// No index.html
	if err := os.WriteFile(filepath.Join(assetsDir, "index.js"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "index.css"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	err := validateUIBuild(dir)
	if err == nil {
		t.Fatal("expected error for missing index.html")
	}
	if !strings.Contains(err.Error(), "index.html") {
		t.Errorf("expected error about index.html, got: %v", err)
	}
}

func TestValidateUIBuild_MissingJS(t *testing.T) {
	dir := t.TempDir()
	distDir := filepath.Join(dir, "dist")
	assetsDir := filepath.Join(distDir, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte("<html>"), 0644); err != nil {
		t.Fatal(err)
	}
	// CSS but no JS
	if err := os.WriteFile(filepath.Join(assetsDir, "index.css"), []byte("body{}"), 0644); err != nil {
		t.Fatal(err)
	}

	err := validateUIBuild(dir)
	if err == nil {
		t.Fatal("expected error for missing .js in assets/")
	}
	if !strings.Contains(err.Error(), ".js") {
		t.Errorf("expected error about .js files, got: %v", err)
	}
}

func TestValidateUIBuild_MissingCSS(t *testing.T) {
	dir := t.TempDir()
	distDir := filepath.Join(dir, "dist")
	assetsDir := filepath.Join(distDir, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte("<html>"), 0644); err != nil {
		t.Fatal(err)
	}
	// JS but no CSS
	if err := os.WriteFile(filepath.Join(assetsDir, "index.js"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	err := validateUIBuild(dir)
	if err == nil {
		t.Fatal("expected error for missing .css in assets/")
	}
	if !strings.Contains(err.Error(), ".css") {
		t.Errorf("expected error about .css files, got: %v", err)
	}
}

// --- runBuildUI --validate flag ---

func TestRunBuildUI_ValidateFlag_Valid(t *testing.T) {
	dir := t.TempDir()
	makeDistDir(t, dir)

	if err := runBuildUI([]string{"--validate", "--ui-dir", dir}); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

func TestRunBuildUI_ValidateFlag_Missing(t *testing.T) {
	dir := t.TempDir()
	err := runBuildUI([]string{"--validate", "--ui-dir", dir})
	if err == nil {
		t.Fatal("expected error for missing dist/")
	}
}

// --- copyDir ---

func TestCopyDir(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Create source structure.
	subDir := filepath.Join(src, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "nested.txt"), []byte("nested"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir failed: %v", err)
	}

	// Verify.
	data, err := os.ReadFile(filepath.Join(dst, "file.txt"))
	if err != nil {
		t.Fatalf("missing file.txt in dst: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got %q", string(data))
	}

	data, err = os.ReadFile(filepath.Join(dst, "sub", "nested.txt"))
	if err != nil {
		t.Fatalf("missing nested.txt in dst: %v", err)
	}
	if string(data) != "nested" {
		t.Errorf("expected 'nested', got %q", string(data))
	}
}

// --- runBuildUI --output copies files ---

func TestRunBuildUI_OutputFlag(t *testing.T) {
	uiDir := t.TempDir()
	makeUIDir(t, uiDir)
	makeDistDir(t, uiDir)
	// Skip actual npm by running --validate only then copy manually below.
	// We test the --output path by having a pre-built dist and running --validate + --output.
	// Since we can't run npm in unit tests, we test the copyDir path directly.
	outputDir := t.TempDir()
	distDir := filepath.Join(uiDir, "dist")

	if err := copyDir(distDir, outputDir); err != nil {
		t.Fatalf("copyDir failed: %v", err)
	}

	// index.html should be in outputDir.
	if _, err := os.Stat(filepath.Join(outputDir, "index.html")); os.IsNotExist(err) {
		t.Error("expected index.html in output dir")
	}
	// assets/ should be in outputDir.
	if _, err := os.Stat(filepath.Join(outputDir, "assets")); os.IsNotExist(err) {
		t.Error("expected assets/ in output dir")
	}
}

// --- fileExists helper ---

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	if fileExists(path) {
		t.Error("expected false for non-existent file")
	}

	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if !fileExists(path) {
		t.Error("expected true for existing file")
	}

	// Directories should return false.
	if fileExists(dir) {
		t.Error("expected false for directory")
	}
}
