package module

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupBuildFromConfigFiles creates a temporary directory with a fake config
// file and a fake server binary (empty files). It returns the directory path
// and a cleanup function.
func setupBuildFromConfigFiles(t *testing.T) (configFile, serverBinary string, cleanup func()) {
	t.Helper()
	dir := t.TempDir()

	configFile = filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(configFile, []byte("version: 1\n"), 0600); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}

	serverBinary = filepath.Join(dir, "workflow-server")
	if err := os.WriteFile(serverBinary, []byte("#!/bin/sh\n"), 0755); err != nil { //nolint:gosec
		t.Fatalf("failed to create server binary: %v", err)
	}

	return configFile, serverBinary, func() {} // t.TempDir cleans up automatically
}

// noopExecCommand returns a mock exec.CommandContext function that succeeds
// without running any real process.
func noopExecCommand(_ context.Context, name string, args ...string) *exec.Cmd {
	// Invoke a real no-op command so cmd.Run() succeeds.
	return exec.Command("true")
}

// failingExecCommand returns a mock that always fails with an exit error.
func failingExecCommand(_ context.Context, _ string, _ ...string) *exec.Cmd {
	return exec.Command("false")
}

func TestBuildFromConfigStep_FactoryRequiresConfigFile(t *testing.T) {
	factory := NewBuildFromConfigStepFactory()
	_, err := factory("bfc", map[string]any{"tag": "my-app:latest"}, nil)
	if err == nil {
		t.Fatal("expected error when config_file is missing")
	}
	if !strings.Contains(err.Error(), "config_file") {
		t.Errorf("expected error to mention config_file, got: %v", err)
	}
}

func TestBuildFromConfigStep_FactoryRequiresTag(t *testing.T) {
	factory := NewBuildFromConfigStepFactory()
	_, err := factory("bfc", map[string]any{"config_file": "app.yaml"}, nil)
	if err == nil {
		t.Fatal("expected error when tag is missing")
	}
	if !strings.Contains(err.Error(), "tag") {
		t.Errorf("expected error to mention tag, got: %v", err)
	}
}

func TestBuildFromConfigStep_FactoryPluginMissingFields(t *testing.T) {
	factory := NewBuildFromConfigStepFactory()
	_, err := factory("bfc", map[string]any{
		"config_file": "app.yaml",
		"tag":         "my-app:latest",
		"plugins": []any{
			map[string]any{"name": "admin"}, // missing binary
		},
	}, nil)
	if err == nil {
		t.Fatal("expected error when plugin binary is missing")
	}
	if !strings.Contains(err.Error(), "binary") {
		t.Errorf("expected error to mention binary, got: %v", err)
	}
}

func TestBuildFromConfigStep_FactoryPluginInvalidEntry(t *testing.T) {
	factory := NewBuildFromConfigStepFactory()
	_, err := factory("bfc", map[string]any{
		"config_file": "app.yaml",
		"tag":         "my-app:latest",
		"plugins":     []any{"not-a-map"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for non-map plugin entry")
	}
}

func TestBuildFromConfigStep_Name(t *testing.T) {
	factory := NewBuildFromConfigStepFactory()
	step, err := factory("my-build", map[string]any{
		"config_file": "app.yaml",
		"tag":         "my-app:latest",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}
	if step.Name() != "my-build" {
		t.Errorf("expected name %q, got %q", "my-build", step.Name())
	}
}

func TestBuildFromConfigStep_DefaultBaseImage(t *testing.T) {
	factory := NewBuildFromConfigStepFactory()
	raw, err := factory("bfc", map[string]any{
		"config_file": "app.yaml",
		"tag":         "my-app:latest",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}
	bfc := raw.(*BuildFromConfigStep)
	if bfc.baseImage != "ghcr.io/gocodealone/workflow-runtime:latest" {
		t.Errorf("unexpected default base_image: %q", bfc.baseImage)
	}
}

func TestBuildFromConfigStep_GenerateDockerfile_NoPLugins(t *testing.T) {
	s := &BuildFromConfigStep{
		name:      "bfc",
		baseImage: "gcr.io/distroless/static-debian12:nonroot",
		tag:       "my-app:latest",
		plugins:   nil,
	}

	got := s.generateDockerfile()

	expectedLines := []string{
		"FROM gcr.io/distroless/static-debian12:nonroot",
		"COPY server /server",
		"COPY config.yaml /app/config.yaml",
		"WORKDIR /app",
		"ENTRYPOINT [\"/server\"]",
		`CMD ["-config", "/app/config.yaml", "-data-dir", "/app/data"]`,
	}

	for _, line := range expectedLines {
		if !strings.Contains(got, line) {
			t.Errorf("Dockerfile missing line %q\nGot:\n%s", line, got)
		}
	}

	// Without plugins, there should be no plugins COPY line.
	if strings.Contains(got, "COPY plugins/") {
		t.Errorf("Dockerfile should not contain plugins COPY when no plugins configured")
	}
}

func TestBuildFromConfigStep_GenerateDockerfile_WithPlugins(t *testing.T) {
	s := &BuildFromConfigStep{
		name:      "bfc",
		baseImage: "gcr.io/distroless/static-debian12:nonroot",
		tag:       "my-app:latest",
		plugins: []PluginSpec{
			{Name: "admin", Binary: "data/plugins/admin/admin"},
		},
	}

	got := s.generateDockerfile()

	if !strings.Contains(got, "COPY plugins/ /app/data/plugins/") {
		t.Errorf("Dockerfile should contain plugins COPY line when plugins are configured\nGot:\n%s", got)
	}
}

func TestBuildFromConfigStep_Execute_MissingConfigFile(t *testing.T) {
	s := &BuildFromConfigStep{
		name:         "bfc",
		configFile:   "/nonexistent/app.yaml",
		serverBinary: "/nonexistent/server",
		tag:          "my-app:latest",
		execCommand:  noopExecCommand,
	}

	_, err := s.Execute(context.Background(), &PipelineContext{})
	if err == nil {
		t.Fatal("expected error for missing config_file")
	}
	if !strings.Contains(err.Error(), "config_file") {
		t.Errorf("expected error to mention config_file, got: %v", err)
	}
}

func TestBuildFromConfigStep_Execute_MissingServerBinary(t *testing.T) {
	configFile, _, _ := setupBuildFromConfigFiles(t)

	s := &BuildFromConfigStep{
		name:         "bfc",
		configFile:   configFile,
		serverBinary: "/nonexistent/server",
		tag:          "my-app:latest",
		execCommand:  noopExecCommand,
	}

	_, err := s.Execute(context.Background(), &PipelineContext{})
	if err == nil {
		t.Fatal("expected error for missing server_binary")
	}
	if !strings.Contains(err.Error(), "server_binary") {
		t.Errorf("expected error to mention server_binary, got: %v", err)
	}
}

func TestBuildFromConfigStep_Execute_MissingPluginBinary(t *testing.T) {
	configFile, serverBinary, _ := setupBuildFromConfigFiles(t)

	s := &BuildFromConfigStep{
		name:         "bfc",
		configFile:   configFile,
		serverBinary: serverBinary,
		tag:          "my-app:latest",
		plugins: []PluginSpec{
			{Name: "admin", Binary: "/nonexistent/admin"},
		},
		execCommand: noopExecCommand,
	}

	_, err := s.Execute(context.Background(), &PipelineContext{})
	if err == nil {
		t.Fatal("expected error for missing plugin binary")
	}
	if !strings.Contains(err.Error(), "plugin") {
		t.Errorf("expected error to mention plugin, got: %v", err)
	}
}

func TestBuildFromConfigStep_Execute_DockerBuildFailure(t *testing.T) {
	configFile, serverBinary, _ := setupBuildFromConfigFiles(t)

	s := &BuildFromConfigStep{
		name:         "bfc",
		configFile:   configFile,
		serverBinary: serverBinary,
		tag:          "my-app:latest",
		execCommand:  failingExecCommand,
	}

	_, err := s.Execute(context.Background(), &PipelineContext{})
	if err == nil {
		t.Fatal("expected error when docker build fails")
	}
	if !strings.Contains(err.Error(), "docker build") {
		t.Errorf("expected error to mention docker build, got: %v", err)
	}
}

func TestBuildFromConfigStep_Execute_DockerPushFailure(t *testing.T) {
	configFile, serverBinary, _ := setupBuildFromConfigFiles(t)

	callCount := 0
	s := &BuildFromConfigStep{
		name:         "bfc",
		configFile:   configFile,
		serverBinary: serverBinary,
		tag:          "my-app:latest",
		push:         true,
		execCommand: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			callCount++
			if callCount == 1 {
				// First call is docker build — succeed.
				return exec.Command("true")
			}
			// Second call is docker push — fail.
			return exec.Command("false")
		},
	}

	_, err := s.Execute(context.Background(), &PipelineContext{})
	if err == nil {
		t.Fatal("expected error when docker push fails")
	}
	if !strings.Contains(err.Error(), "docker push") {
		t.Errorf("expected error to mention docker push, got: %v", err)
	}
}

func TestBuildFromConfigStep_Execute_NoPush(t *testing.T) {
	configFile, serverBinary, _ := setupBuildFromConfigFiles(t)

	buildCalled := false
	pushCalled := false

	s := &BuildFromConfigStep{
		name:         "bfc",
		configFile:   configFile,
		serverBinary: serverBinary,
		baseImage:    "gcr.io/distroless/static-debian12:nonroot",
		tag:          "my-app:latest",
		push:         false,
		execCommand: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if name == "docker" && len(args) > 0 {
				switch args[0] {
				case "build":
					buildCalled = true
				case "push":
					pushCalled = true
				}
			}
			return exec.Command("true")
		},
	}

	result, err := s.Execute(context.Background(), &PipelineContext{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !buildCalled {
		t.Error("expected docker build to be called")
	}
	if pushCalled {
		t.Error("expected docker push NOT to be called when push=false")
	}

	if result.Output["image_tag"] != "my-app:latest" {
		t.Errorf("expected image_tag %q, got %v", "my-app:latest", result.Output["image_tag"])
	}

	dockerfileContent, ok := result.Output["dockerfile_content"].(string)
	if !ok || dockerfileContent == "" {
		t.Error("expected dockerfile_content to be non-empty string")
	}
}

func TestBuildFromConfigStep_Execute_WithPush(t *testing.T) {
	configFile, serverBinary, _ := setupBuildFromConfigFiles(t)

	var dockerCalls []string
	s := &BuildFromConfigStep{
		name:         "bfc",
		configFile:   configFile,
		serverBinary: serverBinary,
		baseImage:    "gcr.io/distroless/static-debian12:nonroot",
		tag:          "my-app:latest",
		push:         true,
		execCommand: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if name == "docker" && len(args) > 0 {
				dockerCalls = append(dockerCalls, args[0])
			}
			return exec.Command("true")
		},
	}

	result, err := s.Execute(context.Background(), &PipelineContext{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(dockerCalls) != 2 {
		t.Fatalf("expected 2 docker calls (build + push), got %d: %v", len(dockerCalls), dockerCalls)
	}
	if dockerCalls[0] != "build" {
		t.Errorf("expected first docker call to be 'build', got %q", dockerCalls[0])
	}
	if dockerCalls[1] != "push" {
		t.Errorf("expected second docker call to be 'push', got %q", dockerCalls[1])
	}

	if result.Output["image_tag"] != "my-app:latest" {
		t.Errorf("expected image_tag %q, got %v", "my-app:latest", result.Output["image_tag"])
	}
}

func TestBuildFromConfigStep_Execute_WithPlugins(t *testing.T) {
	configFile, serverBinary, _ := setupBuildFromConfigFiles(t)

	// Create fake plugin binaries.
	pluginDir := t.TempDir()
	adminBinary := filepath.Join(pluginDir, "admin")
	if err := os.WriteFile(adminBinary, []byte("#!/bin/sh\n"), 0755); err != nil { //nolint:gosec
		t.Fatalf("failed to create admin binary: %v", err)
	}
	bentoBinary := filepath.Join(pluginDir, "workflow-plugin-bento")
	if err := os.WriteFile(bentoBinary, []byte("#!/bin/sh\n"), 0755); err != nil { //nolint:gosec
		t.Fatalf("failed to create bento binary: %v", err)
	}

	var buildArgs []string
	s := &BuildFromConfigStep{
		name:         "bfc",
		configFile:   configFile,
		serverBinary: serverBinary,
		baseImage:    "gcr.io/distroless/static-debian12:nonroot",
		tag:          "my-app:latest",
		push:         false,
		plugins: []PluginSpec{
			{Name: "admin", Binary: adminBinary},
			{Name: "bento", Binary: bentoBinary},
		},
		execCommand: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if name == "docker" && len(args) > 0 && args[0] == "build" {
				buildArgs = args
			}
			return exec.Command("true")
		},
	}

	result, err := s.Execute(context.Background(), &PipelineContext{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify the Dockerfile includes the plugins COPY line.
	dockerfileContent, _ := result.Output["dockerfile_content"].(string)
	if !strings.Contains(dockerfileContent, "COPY plugins/ /app/data/plugins/") {
		t.Errorf("Dockerfile should contain plugins COPY line\nGot:\n%s", dockerfileContent)
	}

	// Verify docker build was called with a context dir argument.
	if len(buildArgs) < 3 {
		t.Fatalf("expected docker build -t <tag> <dir>, got args: %v", buildArgs)
	}
}

func TestBuildFromConfigStep_Execute_BuildContextLayout(t *testing.T) {
	configFile, serverBinary, _ := setupBuildFromConfigFiles(t)

	pluginDir := t.TempDir()
	adminBinary := filepath.Join(pluginDir, "admin")
	if err := os.WriteFile(adminBinary, []byte("#!/bin/sh\n"), 0755); err != nil { //nolint:gosec
		t.Fatalf("failed to create plugin binary: %v", err)
	}

	var capturedBuildDir string
	s := &BuildFromConfigStep{
		name:         "bfc",
		configFile:   configFile,
		serverBinary: serverBinary,
		baseImage:    "alpine:latest",
		tag:          "my-app:latest",
		plugins: []PluginSpec{
			{Name: "admin", Binary: adminBinary},
		},
		execCommand: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			// Capture the build context dir (last argument to docker build).
			if name == "docker" && len(args) > 0 && args[0] == "build" {
				capturedBuildDir = args[len(args)-1]
				// Make a copy so we can inspect it after Execute returns
				// (Execute defers RemoveAll on buildDir).
				copyDir := t.TempDir()
				_ = copyDirRecursive(capturedBuildDir, copyDir)
				capturedBuildDir = copyDir
			}
			return exec.Command("true")
		},
	}

	_, err := s.Execute(context.Background(), &PipelineContext{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Check expected files in the copied build context.
	expectedFiles := []string{
		"Dockerfile",
		"config.yaml",
		"server",
		filepath.Join("plugins", "admin", "admin"),
	}
	for _, f := range expectedFiles {
		if _, err := os.Stat(filepath.Join(capturedBuildDir, f)); err != nil {
			t.Errorf("build context missing expected file %q: %v", f, err)
		}
	}
}

// copyDirRecursive copies the contents of src into dst directory.
func copyDirRecursive(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}
		return func() error {
			in, err := os.Open(path) //nolint:gosec
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
			if err != nil {
				return err
			}
			defer out.Close()
			_, err = fmt.Fprintf(out, "")
			if err != nil {
				return err
			}
			_, err = out.Seek(0, 0)
			if err != nil {
				return err
			}
			f, err := os.Open(path) //nolint:gosec
			if err != nil {
				return err
			}
			defer f.Close()
			_, copyErr := io.Copy(out, f)
			return copyErr
		}()
	})
}
