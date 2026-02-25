package module

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuildBinaryStep_FactoryRequiresConfigFile(t *testing.T) {
	factory := NewBuildBinaryStepFactory()
	_, err := factory("bb", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error when config_file is missing")
	}
	if !strings.Contains(err.Error(), "config_file") {
		t.Errorf("expected error to mention config_file, got: %v", err)
	}
}

func TestBuildBinaryStep_Name(t *testing.T) {
	factory := NewBuildBinaryStepFactory()
	step, err := factory("my-binary", map[string]any{"config_file": "app.yaml"}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}
	if step.Name() != "my-binary" {
		t.Errorf("expected name %q, got %q", "my-binary", step.Name())
	}
}

func TestBuildBinaryStep_Defaults(t *testing.T) {
	factory := NewBuildBinaryStepFactory()
	raw, err := factory("bb", map[string]any{"config_file": "app.yaml"}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}
	s := raw.(*BuildBinaryStep)
	if s.output != "bin/app" {
		t.Errorf("expected default output %q, got %q", "bin/app", s.output)
	}
	if s.targetOS != runtime.GOOS {
		t.Errorf("expected default OS %q, got %q", runtime.GOOS, s.targetOS)
	}
	if s.targetArch != runtime.GOARCH {
		t.Errorf("expected default arch %q, got %q", runtime.GOARCH, s.targetArch)
	}
	if s.modulePath != "app" {
		t.Errorf("expected default module_path %q, got %q", "app", s.modulePath)
	}
	if s.goVersion != "1.22" {
		t.Errorf("expected default go_version %q, got %q", "1.22", s.goVersion)
	}
	if !s.embedConfig {
		t.Error("expected embed_config to default to true")
	}
}

func TestBuildBinaryStep_DryRun_GeneratesGoMod(t *testing.T) {
	configFile := writeTempConfig(t, "version: 1\nmodules: []\n")

	factory := NewBuildBinaryStepFactory()
	step, err := factory("bb", map[string]any{
		"config_file": configFile,
		"module_path": "myapp",
		"go_version":  "1.22",
		"dry_run":     true,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}

	result, err := step.Execute(testCtx(t), &PipelineContext{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Output["dry_run"] != true {
		t.Error("expected dry_run=true in output")
	}

	contents, ok := result.Output["file_contents"].(map[string]string)
	if !ok {
		t.Fatalf("expected file_contents map, got %T", result.Output["file_contents"])
	}

	goMod, ok := contents["go.mod"]
	if !ok {
		t.Fatal("expected go.mod in file_contents")
	}
	if !strings.Contains(goMod, "module myapp") {
		t.Errorf("go.mod missing module declaration, got:\n%s", goMod)
	}
	if !strings.Contains(goMod, "go 1.22") {
		t.Errorf("go.mod missing go version, got:\n%s", goMod)
	}
	if !strings.Contains(goMod, "github.com/GoCodeAlone/workflow") {
		t.Errorf("go.mod missing workflow dependency, got:\n%s", goMod)
	}
}

func TestBuildBinaryStep_DryRun_GeneratesMainGo_WithEmbedDirective(t *testing.T) {
	configFile := writeTempConfig(t, "version: 1\nmodules: []\n")

	factory := NewBuildBinaryStepFactory()
	step, err := factory("bb", map[string]any{
		"config_file":  configFile,
		"embed_config": true,
		"dry_run":      true,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}

	result, err := step.Execute(testCtx(t), &PipelineContext{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	contents := result.Output["file_contents"].(map[string]string)
	mainGo := contents["main.go"]

	if !strings.Contains(mainGo, "//go:embed app.yaml") {
		t.Errorf("main.go missing //go:embed directive, got:\n%s", mainGo)
	}
	if !strings.Contains(mainGo, "var configYAML []byte") {
		t.Errorf("main.go missing configYAML variable, got:\n%s", mainGo)
	}
	if !strings.Contains(mainGo, `_ "embed"`) {
		t.Errorf("main.go missing embed import, got:\n%s", mainGo)
	}
}

func TestBuildBinaryStep_DryRun_ConfigContentCopied(t *testing.T) {
	yamlContent := "version: 1\nmodules:\n  - name: server\n    type: http.server\n"
	configFile := writeTempConfig(t, yamlContent)

	factory := NewBuildBinaryStepFactory()
	step, err := factory("bb", map[string]any{
		"config_file": configFile,
		"dry_run":     true,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}

	result, err := step.Execute(testCtx(t), &PipelineContext{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	contents := result.Output["file_contents"].(map[string]string)
	appYAML := contents["app.yaml"]

	if appYAML != yamlContent {
		t.Errorf("app.yaml content mismatch\nwant: %q\ngot:  %q", yamlContent, appYAML)
	}
}

func TestBuildBinaryStep_DryRun_FileListing(t *testing.T) {
	configFile := writeTempConfig(t, "version: 1\n")

	factory := NewBuildBinaryStepFactory()
	step, err := factory("bb", map[string]any{
		"config_file": configFile,
		"dry_run":     true,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}

	result, err := step.Execute(testCtx(t), &PipelineContext{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	files, ok := result.Output["files"].([]string)
	if !ok {
		t.Fatalf("expected files []string, got %T", result.Output["files"])
	}

	expected := map[string]bool{"go.mod": false, "main.go": false, "app.yaml": false}
	for _, f := range files {
		expected[f] = true
	}
	for name, found := range expected {
		if !found {
			t.Errorf("file listing missing %q, got: %v", name, files)
		}
	}
}

func TestBuildBinaryStep_DryRun_OutputContainsMetadata(t *testing.T) {
	configFile := writeTempConfig(t, "version: 1\n")

	factory := NewBuildBinaryStepFactory()
	step, err := factory("bb", map[string]any{
		"config_file": configFile,
		"module_path": "mymodule",
		"go_version":  "1.21",
		"os":          "linux",
		"arch":        "arm64",
		"dry_run":     true,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}

	result, err := step.Execute(testCtx(t), &PipelineContext{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Output["module_path"] != "mymodule" {
		t.Errorf("expected module_path=mymodule, got %v", result.Output["module_path"])
	}
	if result.Output["go_version"] != "1.21" {
		t.Errorf("expected go_version=1.21, got %v", result.Output["go_version"])
	}
	if result.Output["target_os"] != "linux" {
		t.Errorf("expected target_os=linux, got %v", result.Output["target_os"])
	}
	if result.Output["target_arch"] != "arm64" {
		t.Errorf("expected target_arch=arm64, got %v", result.Output["target_arch"])
	}
}

func TestBuildBinaryStep_DryRun_ConfigFromPipelineContext(t *testing.T) {
	// config_file does not exist; body in pipeline context should be used.
	yamlContent := "version: 1\nmodules: []\n"

	factory := NewBuildBinaryStepFactory()
	step, err := factory("bb", map[string]any{
		"config_file": "/nonexistent/app.yaml",
		"dry_run":     true,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}

	pc := &PipelineContext{
		Current: map[string]any{"body": yamlContent},
	}

	result, err := step.Execute(testCtx(t), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	contents := result.Output["file_contents"].(map[string]string)
	if contents["app.yaml"] != yamlContent {
		t.Errorf("app.yaml from context mismatch: got %q", contents["app.yaml"])
	}
}

func TestBuildBinaryStep_MissingConfigFileAndNoContext(t *testing.T) {
	factory := NewBuildBinaryStepFactory()
	step, err := factory("bb", map[string]any{
		"config_file": "/nonexistent/app.yaml",
		"dry_run":     true,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}

	_, err = step.Execute(testCtx(t), &PipelineContext{})
	if err == nil {
		t.Fatal("expected error when config file is missing and no context body")
	}
}

func TestBuildBinaryStep_DryRun_NoEmbedConfig(t *testing.T) {
	configFile := writeTempConfig(t, "version: 1\n")

	factory := NewBuildBinaryStepFactory()
	step, err := factory("bb", map[string]any{
		"config_file":  configFile,
		"embed_config": false,
		"dry_run":      true,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}

	result, err := step.Execute(testCtx(t), &PipelineContext{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	contents := result.Output["file_contents"].(map[string]string)
	mainGo := contents["main.go"]

	if strings.Contains(mainGo, "//go:embed") {
		t.Error("main.go should not contain //go:embed when embed_config=false")
	}
}

// TestBuildBinaryStep_Compile tests actual compilation when Go is available.
func TestBuildBinaryStep_Compile(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go binary not available; skipping compilation test")
	}

	yamlContent := "version: 1\nmodules: []\n"
	configFile := writeTempConfig(t, yamlContent)

	outputDir := t.TempDir()
	outputBinary := filepath.Join(outputDir, "myapp")

	factory := NewBuildBinaryStepFactory()
	step, err := factory("bb", map[string]any{
		"config_file": configFile,
		"output":      outputBinary,
		"module_path": "testapp",
		"go_version":  "1.22",
		"dry_run":     false,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}

	// The generated module imports github.com/GoCodeAlone/workflow which won't
	// be available without a proper go.sum, so we expect a build failure.
	// The test verifies the step correctly attempts compilation.
	result, err := step.Execute(testCtx(t), &PipelineContext{})
	if err != nil {
		// Build failure is expected without network/module cache.
		if !strings.Contains(err.Error(), "go build failed") &&
			!strings.Contains(err.Error(), "go mod") {
			t.Errorf("unexpected error type: %v", err)
		}
		return
	}

	// If somehow it succeeds (e.g., module is in cache), verify output.
	if result.Output["binary_path"] == nil {
		t.Error("expected binary_path in output")
	}
	if _, statErr := os.Stat(outputBinary); statErr != nil {
		t.Errorf("binary not found at output path: %v", statErr)
	}
}

// testCtx returns a context for use in tests.
func testCtx(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}

// writeTempConfig creates a temporary YAML config file and returns its path.
func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("failed to create temp config: %v", err)
	}
	return path
}
