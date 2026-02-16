package module

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/CrisisTextLine/modular"
)

// BuildUIStep executes a UI build pipeline natively (without Docker),
// producing static assets in a target directory for static.fileserver to serve.
// This enables config-driven UI builds without requiring external CLI commands.
type BuildUIStep struct {
	name       string
	sourceDir  string   // UI source directory (containing package.json)
	outputDir  string   // Where to place built assets (for static.fileserver root)
	installCmd string   // npm install command (default: "npm install --silent")
	buildCmd   string   // Build command (default: "npm run build")
	env        []string // Extra environment variables
	timeout    time.Duration
}

// NewBuildUIStepFactory returns a StepFactory that creates BuildUIStep instances.
func NewBuildUIStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		srcDir, _ := config["source_dir"].(string)
		if srcDir == "" {
			return nil, fmt.Errorf("build_ui step %q: 'source_dir' is required", name)
		}

		outDir, _ := config["output_dir"].(string)
		if outDir == "" {
			return nil, fmt.Errorf("build_ui step %q: 'output_dir' is required", name)
		}

		// Resolve relative paths from config file directory (consistent with module configs)
		if cfgDir, ok := config["_config_dir"].(string); ok && cfgDir != "" {
			if !filepath.IsAbs(srcDir) {
				srcDir = filepath.Join(cfgDir, srcDir)
			}
			if !filepath.IsAbs(outDir) {
				outDir = filepath.Join(cfgDir, outDir)
			}
		}

		installCmd, _ := config["install_cmd"].(string)
		if installCmd == "" {
			installCmd = "npm install --silent"
		}

		buildCmd, _ := config["build_cmd"].(string)
		if buildCmd == "" {
			buildCmd = "npm run build"
		}

		var env []string
		if envRaw, ok := config["env"].(map[string]any); ok {
			for k, v := range envRaw {
				env = append(env, fmt.Sprintf("%s=%v", k, v))
			}
		}

		var timeout time.Duration
		if ts, ok := config["timeout"].(string); ok && ts != "" {
			var err error
			timeout, err = time.ParseDuration(ts)
			if err != nil {
				return nil, fmt.Errorf("build_ui step %q: invalid timeout %q: %w", name, ts, err)
			}
		}
		if timeout == 0 {
			timeout = 5 * time.Minute
		}

		return &BuildUIStep{
			name:       name,
			sourceDir:  srcDir,
			outputDir:  outDir,
			installCmd: installCmd,
			buildCmd:   buildCmd,
			env:        env,
			timeout:    timeout,
		}, nil
	}
}

// Name returns the step name.
func (s *BuildUIStep) Name() string { return s.name }

// Execute runs the UI build process natively:
//  1. Resolves source and output directories
//  2. Runs npm install (or equivalent)
//  3. Runs the build command
//  4. Copies build output to the target directory
func (s *BuildUIStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	// Resolve absolute paths
	srcAbs, err := filepath.Abs(s.sourceDir)
	if err != nil {
		return nil, fmt.Errorf("build_ui step %q: invalid source_dir: %w", s.name, err)
	}
	outAbs, err := filepath.Abs(s.outputDir)
	if err != nil {
		return nil, fmt.Errorf("build_ui step %q: invalid output_dir: %w", s.name, err)
	}

	// Verify source directory exists and has package.json
	if _, err := os.Stat(filepath.Join(srcAbs, "package.json")); err != nil {
		return nil, fmt.Errorf("build_ui step %q: source_dir %q must contain package.json: %w", s.name, srcAbs, err)
	}

	outputs := make([]map[string]any, 0)

	// Step 1: Install dependencies
	installOut, err := s.runCmd(ctx, srcAbs, s.installCmd)
	if err != nil {
		return nil, fmt.Errorf("build_ui step %q: install failed: %w", s.name, err)
	}
	outputs = append(outputs, map[string]any{
		"phase":   "install",
		"command": s.installCmd,
		"output":  installOut,
	})

	// Step 2: Build
	buildOut, err := s.runCmd(ctx, srcAbs, s.buildCmd)
	if err != nil {
		return nil, fmt.Errorf("build_ui step %q: build failed: %w", s.name, err)
	}
	outputs = append(outputs, map[string]any{
		"phase":   "build",
		"command": s.buildCmd,
		"output":  buildOut,
	})

	// Step 3: Find build output directory (convention: dist/ under source)
	distDir := filepath.Join(srcAbs, "dist")
	if _, err := os.Stat(distDir); err != nil {
		return nil, fmt.Errorf("build_ui step %q: build output not found at %s: %w", s.name, distDir, err)
	}

	// Step 4: Copy build output to target
	if err := os.MkdirAll(outAbs, 0750); err != nil {
		return nil, fmt.Errorf("build_ui step %q: failed to create output_dir: %w", s.name, err)
	}

	// Clear old output
	entries, err := os.ReadDir(outAbs)
	if err != nil {
		return nil, fmt.Errorf("build_ui step %q: failed to read output_dir: %w", s.name, err)
	}
	for _, entry := range entries {
		if entry.Name() == ".gitkeep" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(outAbs, entry.Name())); err != nil {
			return nil, fmt.Errorf("build_ui step %q: failed to clean output_dir: %w", s.name, err)
		}
	}

	// Copy dist -> output
	copyOut, err := s.runCmd(ctx, srcAbs, fmt.Sprintf("cp -r %s/* %s/", distDir, outAbs))
	if err != nil {
		return nil, fmt.Errorf("build_ui step %q: copy failed: %w", s.name, err)
	}
	outputs = append(outputs, map[string]any{
		"phase":   "copy",
		"command": fmt.Sprintf("cp -r %s/* %s/", distDir, outAbs),
		"output":  copyOut,
	})

	// Count output files
	fileCount := 0
	_ = filepath.WalkDir(outAbs, func(_ string, d os.DirEntry, _ error) error {
		if d != nil && !d.IsDir() {
			fileCount++
		}
		return nil
	})

	result := map[string]any{
		"phases":     outputs,
		"source_dir": srcAbs,
		"output_dir": outAbs,
		"dist_dir":   distDir,
		"file_count": fileCount,
		"status":     "success",
	}

	// Store in pipeline context so downstream steps can reference the output path
	if pc.Metadata == nil {
		pc.Metadata = make(map[string]any)
	}
	pc.Metadata["build_output_dir"] = outAbs

	return &StepResult{Output: result}, nil
}

// runCmd executes a shell command in the given directory.
func (s *BuildUIStep) runCmd(ctx context.Context, dir, cmdStr string) (string, error) {
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty command")
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), s.env...)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("command %q failed: %w\nOutput: %s", cmdStr, err, string(out))
	}
	return string(out), nil
}
