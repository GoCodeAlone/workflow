package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

const testYAML = `name: test-workflow
modules:
  - name: mycomp
    type: dynamic.component
    config:
      source: components/test.go
  - name: mydb
    type: storage.sqlite
    config:
      dbPath: data/app.db
workflows: {}
triggers: {}
`

func setupWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Create well-known directories with files
	must := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	must("components/test.go", "package main\nfunc init() {}\n")
	must("spa/index.html", "<html><body>hello</body></html>")
	must("seed/data.json", `{"users":[]}`)
	// data/ should NOT be included
	must("data/app.db", "sqlite-binary-data")

	return dir
}

func TestRoundTrip(t *testing.T) {
	workspace := setupWorkspace(t)

	// Export
	var buf bytes.Buffer
	if err := Export(testYAML, workspace, &buf); err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Import
	destDir := t.TempDir()
	manifest, workflowPath, err := Import(&buf, destDir)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	// Verify manifest
	if manifest.Version != BundleFormatVersion {
		t.Errorf("manifest version = %q, want %q", manifest.Version, BundleFormatVersion)
	}
	if manifest.Name != "test-workflow" {
		t.Errorf("manifest name = %q, want %q", manifest.Name, "test-workflow")
	}

	// Verify workflow.yaml was extracted
	if workflowPath == "" {
		t.Fatal("workflowPath is empty")
	}
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read workflow.yaml: %v", err)
	}
	if string(data) != testYAML {
		t.Errorf("workflow.yaml content mismatch:\ngot:  %q\nwant: %q", string(data), testYAML)
	}

	// Verify well-known directory files were included
	expectFiles := map[string]string{
		"components/test.go": "package main\nfunc init() {}\n",
		"spa/index.html":     "<html><body>hello</body></html>",
		"seed/data.json":     `{"users":[]}`,
	}
	for relPath, wantContent := range expectFiles {
		got, err := os.ReadFile(filepath.Join(destDir, relPath))
		if err != nil {
			t.Errorf("expected file %s not found: %v", relPath, err)
			continue
		}
		if string(got) != wantContent {
			t.Errorf("file %s content mismatch:\ngot:  %q\nwant: %q", relPath, string(got), wantContent)
		}
	}

	// Verify data/ directory was NOT included
	if _, err := os.Stat(filepath.Join(destDir, "data/app.db")); err == nil {
		t.Error("data/app.db should NOT be included in the bundle")
	}
}

func TestExportNameFromYAML(t *testing.T) {
	yaml := `name: my-cool-workflow
modules: []
workflows: {}
triggers: {}
`
	var buf bytes.Buffer
	if err := Export(yaml, "", &buf); err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	destDir := t.TempDir()
	manifest, _, err := Import(&buf, destDir)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}
	if manifest.Name != "my-cool-workflow" {
		t.Errorf("manifest name = %q, want %q", manifest.Name, "my-cool-workflow")
	}
}

func TestExportDefaultName(t *testing.T) {
	yaml := `modules: []
workflows: {}
triggers: {}
`
	var buf bytes.Buffer
	if err := Export(yaml, "", &buf); err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	destDir := t.TempDir()
	manifest, _, err := Import(&buf, destDir)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}
	if manifest.Name != "workflow" {
		t.Errorf("manifest name = %q, want %q", manifest.Name, "workflow")
	}
}

func TestImportPathTraversal(t *testing.T) {
	// Build a malicious tar.gz with "../evil" path
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Add a valid manifest
	manifestData, _ := json.Marshal(Manifest{
		Version: "1.0",
		Name:    "evil",
		Files:   []string{"workflow.yaml"},
	})
	_ = writeToTar(tw, "manifest.json", manifestData)
	_ = writeToTar(tw, "workflow.yaml", []byte("modules: []\nworkflows: {}\ntriggers: {}"))
	_ = writeToTar(tw, "../evil.txt", []byte("pwned"))

	tw.Close()
	gw.Close()

	destDir := t.TempDir()
	_, _, err := Import(&buf, destDir)
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
	if !strings.Contains(err.Error(), "invalid path") {
		t.Errorf("expected 'invalid path' error, got: %v", err)
	}
}

func TestImportFileSizeLimit(t *testing.T) {
	// Build a tar.gz with a file exceeding MaxFileSize
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	manifestData, _ := json.Marshal(Manifest{
		Version: "1.0",
		Name:    "big",
		Files:   []string{"workflow.yaml"},
	})
	_ = writeToTar(tw, "manifest.json", manifestData)
	_ = writeToTar(tw, "workflow.yaml", []byte("modules: []\nworkflows: {}\ntriggers: {}"))

	// Write a header claiming a large size (we won't actually write that much data,
	// but the header size check should catch it)
	hdr := &tar.Header{
		Name: "huge.bin",
		Mode: 0644,
		Size: MaxFileSize + 1,
	}
	_ = tw.WriteHeader(hdr)
	// Write just enough to satisfy tar, the size check happens on hdr.Size
	_, _ = tw.Write(make([]byte, MaxFileSize+1))

	tw.Close()
	gw.Close()

	destDir := t.TempDir()
	_, _, err := Import(&buf, destDir)
	if err == nil {
		t.Fatal("expected error for oversized file, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds max size") {
		t.Errorf("expected 'exceeds max size' error, got: %v", err)
	}
}

func TestImportMissingManifest(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	_ = writeToTar(tw, "workflow.yaml", []byte("modules: []\nworkflows: {}\ntriggers: {}"))

	tw.Close()
	gw.Close()

	destDir := t.TempDir()
	_, _, err := Import(&buf, destDir)
	if err == nil {
		t.Fatal("expected error for missing manifest, got nil")
	}
	if !strings.Contains(err.Error(), "missing manifest.json") {
		t.Errorf("expected 'missing manifest.json' error, got: %v", err)
	}
}

func TestImportMissingWorkflowYAML(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	manifestData, _ := json.Marshal(Manifest{
		Version: "1.0",
		Name:    "noworkflow",
		Files:   []string{},
	})
	_ = writeToTar(tw, "manifest.json", manifestData)

	tw.Close()
	gw.Close()

	destDir := t.TempDir()
	_, _, err := Import(&buf, destDir)
	if err == nil {
		t.Fatal("expected error for missing workflow.yaml, got nil")
	}
	if !strings.Contains(err.Error(), "missing workflow.yaml") {
		t.Errorf("expected 'missing workflow.yaml' error, got: %v", err)
	}
}

func TestScanReferencedPaths(t *testing.T) {
	yaml := `modules:
  - name: comp1
    type: dynamic.component
    config:
      source: components/my.go
  - name: db
    type: storage.sqlite
    config:
      dbPath: data/test.db
  - name: web
    type: static.fileserver
    config:
      root: spa
  - name: other
    type: http.server
    config:
      port: 8080
workflows: {}
triggers: {}
`
	cfg, err := loadConfigForTest(yaml)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}

	paths := ScanReferencedPaths(cfg)
	expected := map[string]bool{
		"components/my.go": true,
		"data/test.db":     true,
		"spa":              true,
	}

	if len(paths) != len(expected) {
		t.Fatalf("got %d paths, want %d: %v", len(paths), len(expected), paths)
	}
	for _, p := range paths {
		if !expected[p] {
			t.Errorf("unexpected path: %s", p)
		}
	}
}

func TestManifestFilesListIsComplete(t *testing.T) {
	workspace := setupWorkspace(t)

	var buf bytes.Buffer
	if err := Export(testYAML, workspace, &buf); err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	destDir := t.TempDir()
	manifest, _, err := Import(&buf, destDir)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	// workflow.yaml must always be listed
	found := false
	for _, f := range manifest.Files {
		if f == "workflow.yaml" {
			found = true
			break
		}
	}
	if !found {
		t.Error("manifest.Files does not contain workflow.yaml")
	}

	// All listed files should exist on disk
	for _, f := range manifest.Files {
		p := filepath.Join(destDir, f)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("manifest lists %q but file does not exist: %v", f, err)
		}
	}
}

// loadConfigForTest is a helper to parse YAML into a WorkflowConfig.
func loadConfigForTest(yamlContent string) (*config.WorkflowConfig, error) {
	return config.LoadFromString(yamlContent)
}
