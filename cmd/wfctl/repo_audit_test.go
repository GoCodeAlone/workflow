package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAuditRepoCleanPass(t *testing.T) {
	dir := t.TempDir()
	writeRepoAuditFile(t, filepath.Join(dir, "main.go"), "package main\n")
	writeRepoAuditFile(t, filepath.Join(dir, "README.md"), "# Hello\n")

	var out bytes.Buffer
	err := runAuditWithOutput([]string{"repo", "--dir", dir}, &out)
	if err != nil {
		t.Fatalf("expected clean audit pass, got: %v\noutput:\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "PASS") {
		t.Fatalf("expected PASS in output:\n%s", out.String())
	}
}

func TestRunAuditRepoNonPortablePath(t *testing.T) {
	dir := t.TempDir()
	// Create a file with a colon in its name (Windows-incompatible)
	writeRepoAuditFile(t, filepath.Join(dir, "file:name.txt"), "content\n")

	var out bytes.Buffer
	err := runAuditWithOutput([]string{"repo", "--dir", dir}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "non_portable_path") {
		t.Fatalf("expected non_portable_path finding:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "WARN") {
		t.Fatalf("expected WARN status:\n%s", out.String())
	}
}

func TestRunAuditRepoUnsafePath(t *testing.T) {
	// The unsafe path check works on the relative path itself.
	// Since WalkDir won't produce ../ paths, test the checker directly.
	f := checkUnsafePath("../etc/passwd")
	if f == nil {
		t.Fatal("expected finding for path traversal")
	}
	if f.Code != "unsafe_path_traversal" {
		t.Fatalf("expected unsafe_path_traversal, got %s", f.Code)
	}
}

func TestRunAuditRepoDocFrontmatter(t *testing.T) {
	dir := t.TempDir()
	docsDir := filepath.Join(dir, "docs", "guides")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A structured doc without frontmatter
	writeRepoAuditFile(t, filepath.Join(docsDir, "guide.md"), `# Guide Title

## Section One

Content here.

## Section Two

More content.
`)

	var out bytes.Buffer
	err := runAuditWithOutput([]string{"repo", "--dir", dir}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "missing_doc_frontmatter") {
		t.Fatalf("expected missing_doc_frontmatter finding:\n%s", out.String())
	}
}

func TestRunAuditRepoMalformedFrontmatter(t *testing.T) {
	dir := t.TempDir()
	docsDir := filepath.Join(dir, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepoAuditFile(t, filepath.Join(docsDir, "broken.md"), "---\ntitle: test\nno closing delimiter\n")

	var out bytes.Buffer
	err := runAuditWithOutput([]string{"repo", "--dir", dir}, &out)
	if err == nil {
		t.Fatal("expected error for malformed frontmatter")
	}
	if !strings.Contains(out.String(), "malformed_frontmatter") {
		t.Fatalf("expected malformed_frontmatter finding:\n%s", out.String())
	}
}

func TestRunAuditRepoIndexDrift(t *testing.T) {
	dir := t.TempDir()
	plansDir := filepath.Join(dir, "docs", "plans")
	if err := os.MkdirAll(plansDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepoAuditFile(t, filepath.Join(plansDir, "INDEX.md"), "# Plans\n\n- plan-a.md\n")
	writeRepoAuditFile(t, filepath.Join(plansDir, "plan-a.md"), "# Plan A\n")
	writeRepoAuditFile(t, filepath.Join(plansDir, "plan-b.md"), "# Plan B\n")

	var out bytes.Buffer
	err := runAuditWithOutput([]string{"repo", "--dir", dir}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "index_drift") {
		t.Fatalf("expected index_drift finding:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "plan-b.md") {
		t.Fatalf("expected plan-b.md in drift message:\n%s", out.String())
	}
}

func TestRunAuditRepoJSON(t *testing.T) {
	dir := t.TempDir()
	writeRepoAuditFile(t, filepath.Join(dir, "clean.go"), "package main\n")

	var out bytes.Buffer
	err := runAuditWithOutput([]string{"repo", "--dir", dir, "--json"}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var report repoAuditReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if report.Summary.Status != "PASS" {
		t.Fatalf("expected PASS status, got %s", report.Summary.Status)
	}
	// Verify stable lower-case field names
	if bytes.Contains(out.Bytes(), []byte(`"Path"`)) {
		t.Fatalf("JSON should use lower-case field names:\n%s", out.String())
	}
}

func TestRunAuditRepoStrictMode(t *testing.T) {
	dir := t.TempDir()
	writeRepoAuditFile(t, filepath.Join(dir, "file:name.txt"), "content\n")

	var out bytes.Buffer
	err := runAuditWithOutput([]string{"repo", "--dir", dir, "--strict"}, &out)
	if err == nil {
		t.Fatal("expected strict mode to fail on warnings")
	}
	if !strings.Contains(err.Error(), "warning") {
		t.Fatalf("expected warning error message, got: %v", err)
	}
}

func TestRunAuditRepoConfigIgnores(t *testing.T) {
	dir := t.TempDir()
	writeRepoAuditFile(t, filepath.Join(dir, "file:name.txt"), "content\n")
	writeRepoAuditFile(t, filepath.Join(dir, ".wfctl.yaml"), `audit:
  ignores:
    - "file:name.txt"
`)

	var out bytes.Buffer
	err := runAuditWithOutput([]string{"repo", "--dir", dir}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out.String(), "non_portable_path") {
		t.Fatalf("expected ignore to suppress finding:\n%s", out.String())
	}
}

func TestRunAuditRepoConfigDisableCheck(t *testing.T) {
	dir := t.TempDir()
	writeRepoAuditFile(t, filepath.Join(dir, "file:name.txt"), "content\n")
	writeRepoAuditFile(t, filepath.Join(dir, ".wfctl.yaml"), `audit:
  checks:
    portable_paths: false
`)

	var out bytes.Buffer
	err := runAuditWithOutput([]string{"repo", "--dir", dir}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out.String(), "non_portable_path") {
		t.Fatalf("expected disabled check to suppress finding:\n%s", out.String())
	}
}

func TestRunAuditRepoRouting(t *testing.T) {
	var out bytes.Buffer
	err := runAuditWithOutput([]string{"repo", "--dir", t.TempDir()}, &out)
	if err != nil {
		t.Fatalf("expected audit repo to be routed: %v", err)
	}
}

func TestRunAuditRepoFlagParseError(t *testing.T) {
	var out bytes.Buffer
	err := runAuditWithOutput([]string{"repo", "--unknown"}, &out)
	if err == nil {
		t.Fatal("expected flag parse error")
	}
	if !strings.Contains(out.String(), "flag provided but not defined") {
		t.Fatalf("expected flag error in provided writer, got:\n%s", out.String())
	}
}

func TestCheckPortablePath(t *testing.T) {
	tests := []struct {
		path   string
		expect string
	}{
		{"normal/path.go", ""},
		{"path:with:colons.txt", "non_portable_path"},
		{"dir/trailing ./file.txt", "non_portable_path"},
		{"dir/file.txt ", "non_portable_path"},
		{"has\x01control.txt", "non_portable_path"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			f := checkPortablePath(tt.path)
			if tt.expect == "" && f != nil {
				t.Fatalf("unexpected finding: %+v", f)
			}
			if tt.expect != "" && (f == nil || f.Code != tt.expect) {
				t.Fatalf("expected %s, got %+v", tt.expect, f)
			}
		})
	}
}

func TestCheckUnsafePath(t *testing.T) {
	tests := []struct {
		path   string
		expect string
	}{
		{"normal/path.go", ""},
		{"../etc/passwd", "unsafe_path_traversal"},
		{"dir/../file.txt", "unsafe_path_traversal"},
		{"/absolute/path", "unsafe_absolute_path"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			f := checkUnsafePath(tt.path)
			if tt.expect == "" && f != nil {
				t.Fatalf("unexpected finding: %+v", f)
			}
			if tt.expect != "" && (f == nil || f.Code != tt.expect) {
				t.Fatalf("expected %s, got %+v", tt.expect, f)
			}
		})
	}
}

func TestIsDocFile(t *testing.T) {
	tests := []struct {
		path   string
		expect bool
	}{
		{"docs/guide.md", true},
		{"docs/sub/file.mdx", true},
		{"doc/file.md", true},
		{"src/main.go", false},
		{"README.md", false},
		{"documentation/setup.md", true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isDocFile(tt.path); got != tt.expect {
				t.Fatalf("isDocFile(%q) = %v, want %v", tt.path, got, tt.expect)
			}
		})
	}
}

// writeRepoAuditFile is a test helper for writing files.
func writeRepoAuditFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
