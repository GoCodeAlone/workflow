package module_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/module"
)

// ─── Mock ContainerRegistry ───────────────────────────────────────────────────

type mockContainerRegistry struct {
	registryURL  string
	pushedImages []string
	pushErr      error
}

func (m *mockContainerRegistry) PushImage(_ context.Context, imageRef string) (string, error) {
	if m.pushErr != nil {
		return "", m.pushErr
	}
	m.pushedImages = append(m.pushedImages, imageRef)
	return "sha256:abc123def456", nil
}

func (m *mockContainerRegistry) RegistryURL() string {
	return m.registryURL
}

// ─── Helper ───────────────────────────────────────────────────────────────────

func setupContainerBuildApp(t *testing.T) (*module.MockApplication, *mockContainerRegistry) {
	t.Helper()
	app := module.NewMockApplication()
	reg := &mockContainerRegistry{registryURL: "registry.example.com"}
	if err := app.RegisterService("my-registry", reg); err != nil {
		t.Fatalf("register registry: %v", err)
	}
	return app, reg
}

func baseContainerBuildCfg(contextPath string) map[string]any {
	return map[string]any{
		"context":    contextPath,
		"tag":        "myapp:v2",
		"registry":   "my-registry",
		"dockerfile": "Dockerfile",
	}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestContainerBuild_DryRun(t *testing.T) {
	app, _ := setupContainerBuildApp(t)
	cfg := baseContainerBuildCfg(".")
	cfg["dry_run"] = true

	factory := module.NewContainerBuildStepFactory()
	step, err := factory("build", cfg, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["dry_run"] != true {
		t.Errorf("expected dry_run=true, got %v", result.Output["dry_run"])
	}
	if result.Output["image_ref"] != "registry.example.com/myapp:v2" {
		t.Errorf("unexpected image_ref: %v", result.Output["image_ref"])
	}
}

func TestContainerBuild_ImageRefIncludesRegistry(t *testing.T) {
	app, _ := setupContainerBuildApp(t)
	cfg := baseContainerBuildCfg(".")
	cfg["dry_run"] = true

	factory := module.NewContainerBuildStepFactory()
	step, _ := factory("build", cfg, app)
	result, _ := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	imageRef, _ := result.Output["image_ref"].(string)
	if imageRef == "" {
		t.Fatal("image_ref should not be empty")
	}
	// Should be prefixed with the registry URL.
	expectedPrefix := "registry.example.com/"
	if len(imageRef) < len(expectedPrefix) || imageRef[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("image_ref %q should start with %q", imageRef, expectedPrefix)
	}
}

func TestContainerBuild_BuildAndPush_MockExec(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture builder requires a POSIX shell")
	}

	contextDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(contextDir, "Dockerfile"), []byte("FROM scratch\n"), 0644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	builder := filepath.Join(t.TempDir(), "fixture builder")
	if err := os.WriteFile(builder, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$0.args\"\n"), 0700); err != nil {
		t.Fatalf("write fixture builder: %v", err)
	}

	app, reg := setupContainerBuildApp(t)
	cfg := baseContainerBuildCfg(contextDir)
	cfg["builder"] = builder

	factory := module.NewContainerBuildStepFactory()
	step, err := factory("build", cfg, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := step.Execute(ctx, &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("build+push: %v", err)
	}
	if result.Output["success"] != true {
		t.Errorf("expected success=true, got %v", result.Output["success"])
	}
	if len(reg.pushedImages) != 1 || reg.pushedImages[0] != "registry.example.com/myapp:v2" {
		t.Fatalf("unexpected pushed images: %v", reg.pushedImages)
	}
	if result.Output["digest"] != "sha256:abc123def456" {
		t.Errorf("unexpected pushed digest: %v", result.Output["digest"])
	}
	args, err := os.ReadFile(builder + ".args")
	if err != nil {
		t.Fatalf("read build arguments: %v", err)
	}
	want := strings.Join([]string{"build", "-f", "Dockerfile", "-t", "registry.example.com/myapp:v2", contextDir, ""}, "\n")
	if string(args) != want {
		t.Errorf("build arguments = %q; want %q", args, want)
	}
}

func TestContainerBuild_MissingContext(t *testing.T) {
	factory := module.NewContainerBuildStepFactory()
	_, err := factory("build", map[string]any{"tag": "myapp:v2", "registry": "r"}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing context, got nil")
	}
}

func TestContainerBuild_MissingTag(t *testing.T) {
	factory := module.NewContainerBuildStepFactory()
	_, err := factory("build", map[string]any{"context": ".", "registry": "r"}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing tag, got nil")
	}
}

func TestContainerBuild_MissingRegistry(t *testing.T) {
	factory := module.NewContainerBuildStepFactory()
	_, err := factory("build", map[string]any{"context": ".", "tag": "myapp:v2"}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing registry, got nil")
	}
}

func TestContainerBuild_RegistryNotFound(t *testing.T) {
	factory := module.NewContainerBuildStepFactory()
	step, _ := factory("build", map[string]any{
		"context":  ".",
		"tag":      "myapp:v2",
		"registry": "ghost-registry",
		"dry_run":  false,
	}, module.NewMockApplication())
	_, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for missing registry in service registry, got nil")
	}
}

func TestContainerBuild_BuildArgs(t *testing.T) {
	app, _ := setupContainerBuildApp(t)
	cfg := baseContainerBuildCfg(".")
	cfg["dry_run"] = true
	cfg["build_args"] = map[string]any{
		"VERSION": "2.0.0",
		"ENV":     "production",
	}

	factory := module.NewContainerBuildStepFactory()
	step, err := factory("build", cfg, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["dry_run"] != true {
		t.Errorf("expected dry_run=true, got %v", result.Output["dry_run"])
	}
}
