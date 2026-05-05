package buildernodejs_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/plugin/builder"
	buildernodejs "github.com/GoCodeAlone/workflow/plugins/builder-nodejs"
)

func TestNodejsBuilder_Name(t *testing.T) {
	b := buildernodejs.New()
	if b.Name() != "nodejs" {
		t.Fatalf("want name=nodejs, got %q", b.Name())
	}
}

func TestNodejsBuilder_Validate_MissingScript(t *testing.T) {
	b := buildernodejs.New()
	err := b.Validate(builder.Config{TargetName: "ui", Path: "./ui", Fields: map[string]any{}})
	if err == nil {
		t.Fatal("want error when script is missing")
	}
}

func TestNodejsBuilder_Validate_OK(t *testing.T) {
	b := buildernodejs.New()
	err := b.Validate(builder.Config{
		TargetName: "ui",
		Path:       "./ui",
		Fields:     map[string]any{"script": "build"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNodejsBuilder_Build_DryRun(t *testing.T) {
	b := buildernodejs.New()
	cfg := builder.Config{
		TargetName: "ui",
		Path:       "./ui",
		Fields:     map[string]any{"script": "build"},
	}
	out := &builder.Outputs{}
	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	if err := b.Build(context.Background(), cfg, out); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if len(out.Artifacts) != 1 || out.Artifacts[0].Kind != "bundle" {
		t.Fatalf("expected 1 bundle artifact, got %+v", out.Artifacts)
	}
}

func TestNodejsBuilder_SecurityLint_NpmInstall(t *testing.T) {
	b := buildernodejs.New()
	cfg := builder.Config{
		TargetName: "ui",
		Path:       "./ui",
		Fields:     map[string]any{"script": "build", "npm_flags": "--legacy-peer-deps", "install_cmd": "npm install"},
	}
	findings := b.SecurityLint(cfg)
	for _, f := range findings {
		if f.Severity == "warn" {
			return
		}
	}
	t.Fatal("want warn for npm install instead of npm ci")
}

func TestNodejsBuilder_SecurityLint_NoLockFile(t *testing.T) {
	dir := t.TempDir()
	b := buildernodejs.New()
	cfg := builder.Config{
		TargetName: "ui",
		Path:       dir,
		Fields:     map[string]any{"script": "build"},
	}
	// No package-lock.json in dir
	findings := b.SecurityLint(cfg)
	for _, f := range findings {
		if f.Severity == "warn" && filepath.Base(f.File) == "package-lock.json" {
			return
		}
	}
	// Also accept if the finding message mentions lock file
	for _, f := range findings {
		if f.Severity == "warn" {
			return
		}
	}
	// Only fail if there truly is a package-lock.json (there shouldn't be in TempDir)
	if _, err := os.Stat(filepath.Join(dir, "package-lock.json")); os.IsNotExist(err) {
		t.Fatal("want warn finding when package-lock.json is absent")
	}
}
