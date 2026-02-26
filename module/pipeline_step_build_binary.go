package module

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// BuildBinaryStep reads a workflow config YAML, generates a self-contained Go
// project that embeds the config, and compiles it into a standalone binary.
type BuildBinaryStep struct {
	name         string
	configFile   string
	output       string
	targetOS     string
	targetArch   string
	embedConfig  bool
	modulePath   string
	goVersion    string
	dryRun       bool
	execCommand  func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// NewBuildBinaryStepFactory returns a StepFactory that creates BuildBinaryStep instances.
func NewBuildBinaryStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		configFile, _ := config["config_file"].(string)
		if configFile == "" {
			return nil, fmt.Errorf("build_binary step %q: 'config_file' is required", name)
		}

		output, _ := config["output"].(string)
		if output == "" {
			output = "bin/app"
		}

		targetOS, _ := config["os"].(string)
		if targetOS == "" {
			targetOS = runtime.GOOS
		}

		targetArch, _ := config["arch"].(string)
		if targetArch == "" {
			targetArch = runtime.GOARCH
		}

		embedConfig := true
		if v, ok := config["embed_config"].(bool); ok {
			embedConfig = v
		}

		modulePath, _ := config["module_path"].(string)
		if modulePath == "" {
			modulePath = "app"
		}

		goVersion, _ := config["go_version"].(string)
		if goVersion == "" {
			goVersion = "1.22"
		}

		dryRun, _ := config["dry_run"].(bool)

		return &BuildBinaryStep{
			name:        name,
			configFile:  configFile,
			output:      output,
			targetOS:    targetOS,
			targetArch:  targetArch,
			embedConfig: embedConfig,
			modulePath:  modulePath,
			goVersion:   goVersion,
			dryRun:      dryRun,
			execCommand: exec.CommandContext,
		}, nil
	}
}

// Name returns the step name.
func (s *BuildBinaryStep) Name() string { return s.name }

// Execute generates the Go project and optionally compiles it.
func (s *BuildBinaryStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	// Resolve config file content: check step config first, then pipeline context.
	configContent, err := s.resolveConfigContent(pc)
	if err != nil {
		return nil, fmt.Errorf("build_binary step %q: %w", s.name, err)
	}

	// Generate Go project files in memory.
	files, err := s.generateProject(configContent)
	if err != nil {
		return nil, fmt.Errorf("build_binary step %q: %w", s.name, err)
	}

	if s.dryRun {
		return s.dryRunResult(files), nil
	}

	// Write files to a temp directory and compile.
	return s.compileProject(ctx, files)
}

// resolveConfigContent reads the config file from disk, falling back to the
// pipeline context body if the file is not found.
func (s *BuildBinaryStep) resolveConfigContent(pc *PipelineContext) ([]byte, error) {
	if s.configFile != "" {
		data, err := os.ReadFile(s.configFile) //nolint:gosec // G304: path from trusted pipeline config
		if err == nil {
			return data, nil
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read config_file %q: %w", s.configFile, err)
		}
		// File not found — fall through to pipeline context.
	}

	// Try pipeline context body.
	if pc != nil {
		if body, ok := pc.Current["body"].(string); ok && body != "" {
			return []byte(body), nil
		}
		if body, ok := pc.Current["body"].([]byte); ok && len(body) > 0 {
			return body, nil
		}
	}

	return nil, fmt.Errorf("config_file %q not found and no body in pipeline context", s.configFile)
}

// generatedFile holds the path and content of a generated file.
type generatedFile struct {
	Path    string
	Content string
}

// generateProject returns the set of files that make up the generated Go project.
func (s *BuildBinaryStep) generateProject(configContent []byte) ([]generatedFile, error) {
	goMod := s.generateGoMod()
	mainGo := s.generateMainGo()

	return []generatedFile{
		{Path: "go.mod", Content: goMod},
		{Path: "main.go", Content: mainGo},
		{Path: "app.yaml", Content: string(configContent)},
	}, nil
}

// generateGoMod returns the contents of the generated go.mod file.
func (s *BuildBinaryStep) generateGoMod() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "module %s\n\n", s.modulePath)
	fmt.Fprintf(&sb, "go %s\n\n", s.goVersion)
	sb.WriteString("require (\n")
	sb.WriteString("\tgithub.com/GoCodeAlone/workflow v0.0.0\n")
	sb.WriteString(")\n")
	return sb.String()
}

// generateMainGo returns the contents of the generated main.go file.
// The generated binary loads a workflow config (either embedded at compile-time
// or read from disk at runtime), builds the workflow engine, and runs it until
// SIGINT/SIGTERM is received.
func (s *BuildBinaryStep) generateMainGo() string {
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	sb.WriteString("import (\n")
	if s.embedConfig {
		sb.WriteString("\t_ \"embed\"\n")
	}
	sb.WriteString("\t\"context\"\n")
	sb.WriteString("\t\"fmt\"\n")
	sb.WriteString("\t\"log/slog\"\n")
	sb.WriteString("\t\"os\"\n")
	sb.WriteString("\t\"os/signal\"\n")
	sb.WriteString("\t\"syscall\"\n")
	sb.WriteString("\n")
	sb.WriteString("\t\"github.com/CrisisTextLine/modular\"\n")
	sb.WriteString("\tworkflow \"github.com/GoCodeAlone/workflow\"\n")
	sb.WriteString("\t\"github.com/GoCodeAlone/workflow/config\"\n")
	sb.WriteString(")\n\n")

	if s.embedConfig {
		sb.WriteString("//go:embed app.yaml\n")
		sb.WriteString("var configYAML []byte\n\n")
	}

	sb.WriteString("func main() {\n")
	sb.WriteString("\tlogger := slog.New(slog.NewTextHandler(os.Stdout, nil))\n\n")

	if s.embedConfig {
		sb.WriteString("\tif len(configYAML) == 0 {\n")
		sb.WriteString("\t\tfmt.Fprintln(os.Stderr, \"embedded config is empty\")\n")
		sb.WriteString("\t\tos.Exit(1)\n")
		sb.WriteString("\t}\n\n")
		sb.WriteString("\tcfg, err := config.LoadFromString(string(configYAML))\n")
	} else {
		sb.WriteString("\tcfgFile := \"app.yaml\"\n")
		sb.WriteString("\tif len(os.Args) > 1 {\n")
		sb.WriteString("\t\tcfgFile = os.Args[1]\n")
		sb.WriteString("\t}\n\n")
		sb.WriteString("\tcfg, err := config.LoadFromFile(cfgFile)\n")
	}
	sb.WriteString("\tif err != nil {\n")
	sb.WriteString("\t\tfmt.Fprintf(os.Stderr, \"parse config: %v\\n\", err)\n")
	sb.WriteString("\t\tos.Exit(1)\n")
	sb.WriteString("\t}\n\n")

	sb.WriteString("\tapp := modular.NewStdApplication(nil, logger)\n")
	sb.WriteString("\tengine := workflow.NewStdEngine(app, logger)\n\n")

	sb.WriteString("\tif err := engine.BuildFromConfig(cfg); err != nil {\n")
	sb.WriteString("\t\tfmt.Fprintf(os.Stderr, \"build engine: %v\\n\", err)\n")
	sb.WriteString("\t\tos.Exit(1)\n")
	sb.WriteString("\t}\n\n")

	sb.WriteString("\tctx, cancel := context.WithCancel(context.Background())\n")
	sb.WriteString("\tdefer cancel()\n\n")

	sb.WriteString("\tif err := engine.Start(ctx); err != nil {\n")
	sb.WriteString("\t\tfmt.Fprintf(os.Stderr, \"start engine: %v\\n\", err)\n")
	sb.WriteString("\t\tos.Exit(1)\n")
	sb.WriteString("\t}\n\n")

	sb.WriteString("\tsigCh := make(chan os.Signal, 1)\n")
	sb.WriteString("\tsignal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)\n")
	sb.WriteString("\t<-sigCh\n\n")

	sb.WriteString("\tcancel()\n")
	sb.WriteString("\tif err := engine.Stop(context.Background()); err != nil {\n")
	sb.WriteString("\t\tfmt.Fprintf(os.Stderr, \"stop engine: %v\\n\", err)\n")
	sb.WriteString("\t\tos.Exit(1)\n")
	sb.WriteString("\t}\n")
	sb.WriteString("}\n")
	return sb.String()
}

// dryRunResult returns a StepResult with the generated file listing and contents.
func (s *BuildBinaryStep) dryRunResult(files []generatedFile) *StepResult {
	fileList := make([]string, len(files))
	fileContents := make(map[string]string, len(files))
	for i, f := range files {
		fileList[i] = f.Path
		fileContents[f.Path] = f.Content
	}
	return &StepResult{
		Output: map[string]any{
			"dry_run":       true,
			"files":         fileList,
			"file_contents": fileContents,
			"module_path":   s.modulePath,
			"go_version":    s.goVersion,
			"target_os":     s.targetOS,
			"target_arch":   s.targetArch,
		},
	}
}

// compileProject writes the generated files to a temp directory, runs go build,
// and copies the binary to the configured output path.
func (s *BuildBinaryStep) compileProject(ctx context.Context, files []generatedFile) (*StepResult, error) {
	buildDir, err := os.MkdirTemp("", "workflow-build-binary-*")
	if err != nil {
		return nil, fmt.Errorf("create temp build dir: %w", err)
	}
	defer os.RemoveAll(buildDir)

	for _, f := range files {
		dst := filepath.Join(buildDir, f.Path)
		if err := os.MkdirAll(filepath.Dir(dst), 0750); err != nil {
			return nil, fmt.Errorf("create dir for %q: %w", f.Path, err)
		}
		if err := os.WriteFile(dst, []byte(f.Content), 0600); err != nil {
			return nil, fmt.Errorf("write %q: %w", f.Path, err)
		}
	}

	// Run go mod tidy to resolve dependencies (best-effort).
	tidyCmd := s.execCommand(ctx, "go", "mod", "tidy") //nolint:gosec // G204: trusted args
	tidyCmd.Dir = buildDir
	tidyCmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	_ = tidyCmd.Run() // ignore errors — tidy may fail without network

	// Run go build.
	binaryName := filepath.Base(s.output)
	if s.targetOS == "windows" && !strings.HasSuffix(binaryName, ".exe") {
		binaryName += ".exe"
	}
	builtBinary := filepath.Join(buildDir, binaryName)

	var stdout, stderr bytes.Buffer
	buildCmd := s.execCommand(ctx, "go", "build", "-o", builtBinary, ".") //nolint:gosec // G204: trusted args
	buildCmd.Dir = buildDir
	buildCmd.Stdout = &stdout
	buildCmd.Stderr = &stderr
	buildCmd.Env = append(os.Environ(),
		"GOOS="+s.targetOS,
		"GOARCH="+s.targetArch,
		"CGO_ENABLED=0",
	)

	if err := buildCmd.Run(); err != nil {
		return nil, fmt.Errorf("go build failed: %w\nstdout: %s\nstderr: %s",
			err, stdout.String(), stderr.String())
	}

	// Copy binary to output path.
	if err := os.MkdirAll(filepath.Dir(s.output), 0750); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}
	if err := copyFile(builtBinary, s.output); err != nil {
		return nil, fmt.Errorf("copy binary to %q: %w", s.output, err)
	}
	if err := os.Chmod(s.output, 0755); err != nil { //nolint:gosec // G302: intentionally executable
		return nil, fmt.Errorf("chmod binary: %w", err)
	}

	info, err := os.Stat(s.output)
	if err != nil {
		return nil, fmt.Errorf("stat output binary: %w", err)
	}

	return &StepResult{
		Output: map[string]any{
			"binary_path": s.output,
			"binary_size": info.Size(),
			"target_os":   s.targetOS,
			"target_arch": s.targetArch,
		},
	}, nil
}
