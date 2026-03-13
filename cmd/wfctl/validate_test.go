package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateSkipsNonWorkflowYAML(t *testing.T) {
	dir := t.TempDir()

	// GitHub Actions CI file — should NOT be recognized as workflow YAML
	ciYAML := `name: CI
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
`
	ciPath := filepath.Join(dir, "ci.yml")
	if err := os.WriteFile(ciPath, []byte(ciYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Workflow engine config — should be recognized
	appYAML := `modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
`
	appPath := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(appPath, []byte(appYAML), 0644); err != nil {
		t.Fatal(err)
	}

	if isWorkflowYAML(ciPath) {
		t.Errorf("isWorkflowYAML(%q) = true, want false (GitHub Actions file)", ciPath)
	}
	if !isWorkflowYAML(appPath) {
		t.Errorf("isWorkflowYAML(%q) = false, want true (workflow config)", appPath)
	}
}

func TestIsWorkflowYAMLVariants(t *testing.T) {
	dir := t.TempDir()

	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"with-modules", "modules:\n  - name: x\n", true},
		{"with-workflows", "workflows:\n  http: {}\n", true},
		{"with-pipelines", "pipelines:\n  - name: p\n", true},
		{"non-workflow", "name: CI\non: [push]\n", false},
		{"indented-modules", "  modules:\n  - name: x\n", false}, // indented, not top-level
		{"empty", "", false},
	}

	for _, tc := range cases {
		p := write(tc.name+".yaml", tc.content)
		got := isWorkflowYAML(p)
		if got != tc.want {
			t.Errorf("isWorkflowYAML(%q) = %v, want %v (content: %q)", tc.name, got, tc.want, tc.content)
		}
	}
}

func TestValidateDirSkipsNonWorkflowFiles(t *testing.T) {
	dir := t.TempDir()

	// Write a non-workflow YAML (GitHub Actions style)
	ciYAML := `name: CI
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
`
	if err := os.WriteFile(filepath.Join(dir, "ci.yml"), []byte(ciYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Write a valid workflow config
	appYAML := `modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
`
	if err := os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(appYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// --dir should succeed: ci.yml is skipped, app.yaml passes validation
	if err := runValidate([]string{"--dir", dir}); err != nil {
		t.Fatalf("runValidate --dir: %v", err)
	}
}
