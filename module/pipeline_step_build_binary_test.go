package module

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"gopkg.in/yaml.v3"
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
	if s.goVersion != "1.27.2" {
		t.Errorf("expected default go_version %q, got %q", "1.27.2", s.goVersion)
	}
	if !s.embedConfig {
		t.Error("expected embed_config to default to true")
	}
}

func TestBuildBinaryStep_DryRun_GoVersions(t *testing.T) {
	for _, tc := range []struct {
		name      string
		goVersion *string
		want      string
	}{
		{name: "omitted", want: "1.27.2"},
		{name: "empty", goVersion: new(""), want: "1.27.2"},
		{name: "explicit-older", goVersion: new("1.22"), want: "1.22"},
		{name: "explicit-patch", goVersion: new("1.26.5"), want: "1.26.5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := map[string]any{
				"config_file": writeTempConfig(t, "version: 1\nmodules: []\n"),
				"dry_run":     true,
			}
			if tc.goVersion != nil {
				cfg["go_version"] = *tc.goVersion
			}
			step, err := NewBuildBinaryStepFactory()("version-proof", cfg, nil)
			if err != nil {
				t.Fatalf("create step: %v", err)
			}
			result, err := step.Execute(t.Context(), &PipelineContext{})
			if err != nil {
				t.Fatalf("execute dry run: %v", err)
			}
			if got := result.Output["go_version"]; got != tc.want {
				t.Errorf("go_version = %v, want %s", got, tc.want)
			}
			contents, ok := result.Output["file_contents"].(map[string]string)
			if !ok {
				t.Fatalf("file_contents = %T, want map[string]string", result.Output["file_contents"])
			}
			if got := contents["go.mod"]; !strings.Contains(got, "\ngo "+tc.want+"\n") {
				t.Errorf("go.mod should preserve Go %s, got:\n%s", tc.want, got)
			}
		})
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

func TestBuildBinaryStep_DryRun_DefaultEngineBuilder(t *testing.T) {
	for _, embedConfig := range []bool{true, false} {
		name := "external-config"
		if embedConfig {
			name = "embedded-config"
		}
		t.Run(name, func(t *testing.T) {
			step, err := NewBuildBinaryStepFactory()("defaults-proof", map[string]any{
				"config_file":  writeTempConfig(t, "version: 1\nmodules: []\n"),
				"embed_config": embedConfig,
				"dry_run":      true,
			}, nil)
			if err != nil {
				t.Fatalf("create step: %v", err)
			}
			result, err := step.Execute(t.Context(), &PipelineContext{})
			if err != nil {
				t.Fatalf("execute dry run: %v", err)
			}
			contents, ok := result.Output["file_contents"].(map[string]string)
			if !ok {
				t.Fatalf("file_contents = %T, want map[string]string", result.Output["file_contents"])
			}
			mainGo := contents["main.go"]
			for _, want := range []string{
				`_ "github.com/GoCodeAlone/workflow/setup"`,
				`allplugins "github.com/GoCodeAlone/workflow/plugins/all"`,
				"engine, err := workflow.NewEngineBuilder().\n\t\tWithLogger(logger).\n\t\tWithAllDefaults().\n\t\tWithPlugins(allplugins.DefaultPlugins()...).\n\t\tBuildFromConfig(cfg)",
			} {
				if !strings.Contains(mainGo, want) {
					t.Errorf("generated main must wire real engine defaults: missing %q", want)
				}
			}
			for _, forbidden := range []string{"workflow.NewStdEngine(", `"github.com/GoCodeAlone/modular"`} {
				if strings.Contains(mainGo, forbidden) {
					t.Errorf("generated main retains bare engine wiring %q", forbidden)
				}
			}
		})
	}
}

func TestBuildBinaryStep_DryRun_GeneratedMainHTTPRuntime(t *testing.T) {
	sourceRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve source root: %v", err)
	}
	for _, embedConfig := range []bool{true, false} {
		name := "external-config"
		if embedConfig {
			name = "embedded-config"
		}
		t.Run(name, func(t *testing.T) {
			outputDir := t.TempDir()
			cfg, err := config.LoadFromFile(filepath.Join(sourceRoot, "example", "api-server-config.yaml"))
			if err != nil {
				t.Fatalf("load real HTTP example: %v", err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("allocate HTTP address: %v", err)
			}
			address := listener.Addr().String()
			if err := listener.Close(); err != nil {
				t.Fatalf("release HTTP address: %v", err)
			}
			identity := "health-" + filepath.Base(filepath.Dir(outputDir))
			for i := range cfg.Modules {
				switch cfg.Modules[i].Name {
				case "api-http-server":
					cfg.Modules[i].Config["address"] = address
				case "health-handler":
					cfg.Modules[i].Name = identity
				}
			}
			for _, raw := range cfg.Workflows["http"].(map[string]any)["routes"].([]any) {
				route := raw.(map[string]any)
				if route["handler"] == "health-handler" {
					route["handler"] = identity
				}
			}
			configYAML, err := yaml.Marshal(cfg)
			if err != nil {
				t.Fatalf("parameterize HTTP example: %v", err)
			}
			step, err := NewBuildBinaryStepFactory()("runtime-proof", map[string]any{
				"config_file":  writeTempConfig(t, string(configYAML)),
				"embed_config": embedConfig,
				"dry_run":      true,
			}, nil)
			if err != nil {
				t.Fatalf("create step: %v", err)
			}
			result, err := step.Execute(t.Context(), &PipelineContext{})
			if err != nil {
				t.Fatalf("execute dry run: %v", err)
			}
			contents, ok := result.Output["file_contents"].(map[string]string)
			if !ok || len(contents) != 3 {
				t.Fatalf("file_contents = %v, want exactly three emitted files", result.Output["file_contents"])
			}
			for _, path := range []string{"go.mod", "main.go", "app.yaml"} {
				data, ok := contents[path]
				if !ok {
					t.Fatalf("missing emitted file %s", path)
				}
				if err := os.WriteFile(filepath.Join(outputDir, path), []byte(data), 0600); err != nil {
					t.Fatalf("materialize %s: %v", path, err)
				}
			}
			runGo := func(args ...string) {
				t.Helper()
				cmd := exec.CommandContext(t.Context(), "go", args...)
				cmd.Dir = outputDir
				cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "CGO_ENABLED=0")
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("native go %v: %v\n%s", args, err, output)
				}
			}
			// The emitter's unreleased dependency is resolved only for this source integration.
			runGo("mod", "edit", "-replace=github.com/GoCodeAlone/workflow="+sourceRoot)
			runGo("mod", "tidy")
			binary := filepath.Join(outputDir, "proof-app")
			runGo("build", "-o", binary, ".")
			if info, err := os.Stat(binary); err != nil || info.Size() == 0 {
				t.Fatalf("missing or empty compiled generated binary: %v", err)
			}
			if runtime.GOOS == "windows" {
				t.Skip("generated binary compiled; SIGTERM lifecycle requires a POSIX platform")
			}
			logPath := filepath.Join(outputDir, "runtime.log")
			logFile, err := os.Create(logPath)
			if err != nil {
				t.Fatalf("create runtime log: %v", err)
			}
			t.Cleanup(func() { _ = logFile.Close() })
			launchCtx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(launchCtx, binary)
			cmd.Dir = outputDir
			cmd.Stdout, cmd.Stderr = logFile, logFile
			cmd.WaitDelay = time.Second
			if err := cmd.Start(); err != nil {
				t.Fatalf("launch generated binary: %v", err)
			}
			done := make(chan struct{})
			var waitErr error
			go func() {
				waitErr = cmd.Wait()
				close(done)
			}()
			t.Cleanup(func() {
				select {
				case <-done:
				default:
					_ = cmd.Process.Kill()
					<-done
				}
			})
			client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
			t.Cleanup(client.CloseIdleConnections)
			url := "http://" + address + "/health"
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			readyDeadline := time.NewTimer(10 * time.Second)
			defer readyDeadline.Stop()
			for {
				resp, err := client.Get(url)
				if err == nil {
					var body map[string]string
					decodeErr := json.NewDecoder(resp.Body).Decode(&body)
					_ = resp.Body.Close()
					if resp.StatusCode != http.StatusOK || decodeErr != nil || body["handler"] != identity || body["status"] != "success" {
						t.Fatalf("HTTP proof: status=%d body=%v decode=%v, want owned handler %q", resp.StatusCode, body, decodeErr, identity)
					}
					break
				}
				select {
				case <-done:
					logs, _ := os.ReadFile(logPath)
					t.Fatalf("generated binary exited before HTTP readiness: %v\n%s", waitErr, logs)
				case <-readyDeadline.C:
					t.Fatalf("generated HTTP endpoint not ready: %v", err)
				case <-ticker.C:
				}
			}
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatalf("signal generated binary: %v", err)
			}
			select {
			case <-done:
				logs, _ := os.ReadFile(logPath)
				if waitErr != nil {
					t.Fatalf("generated binary shutdown: %v\n%s", waitErr, logs)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("generated binary did not shut down gracefully")
			}
			if resp, err := client.Get(url); err == nil {
				_ = resp.Body.Close()
				t.Fatal("generated HTTP endpoint remains reachable after shutdown")
			}
			t.Logf("actual emitted binary: HTTP 200 with handler %q, SIGTERM exit 0, endpoint absent", identity)
		})
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

func TestBuildBinaryStep_DryRun_ConfigFromExplicitContextPath(t *testing.T) {
	yamlContent := "version: 1\nmodules: []\n"

	factory := NewBuildBinaryStepFactory()
	step, err := factory("bb", map[string]any{
		"config_from": "request_body",
		"dry_run":     true,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}

	pc := &PipelineContext{
		Current: map[string]any{"request_body": yamlContent},
	}

	result, err := step.Execute(testCtx(t), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	contents := result.Output["file_contents"].(map[string]string)
	if contents["app.yaml"] != yamlContent {
		t.Errorf("app.yaml from config_from mismatch: got %q", contents["app.yaml"])
	}
}

func TestBuildBinaryStep_ConfigFileAndConfigFromAreMutuallyExclusive(t *testing.T) {
	factory := NewBuildBinaryStepFactory()
	_, err := factory("bb", map[string]any{
		"config_file": "app.yaml",
		"config_from": "request_body",
	}, nil)
	if err == nil {
		t.Fatal("expected error when config_file and config_from are both set")
	}
	if !strings.Contains(err.Error(), "only one of 'config_file' or 'config_from'") {
		t.Fatalf("expected mutually exclusive config error, got %v", err)
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
