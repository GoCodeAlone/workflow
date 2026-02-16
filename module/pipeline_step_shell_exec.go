package module

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/artifact"
	"github.com/GoCodeAlone/workflow/sandbox"
)

// ArtifactOutput defines an artifact to collect after shell execution.
type ArtifactOutput struct {
	Key  string `yaml:"key"`
	Path string `yaml:"path"`
}

// ShellExecStep executes shell commands inside a Docker container,
// optionally collecting output artifacts.
type ShellExecStep struct {
	name         string
	image        string
	commands     []string
	workDir      string
	timeout      time.Duration
	env          map[string]string
	artifactsOut []ArtifactOutput
}

// NewShellExecStepFactory returns a StepFactory that creates ShellExecStep instances.
func NewShellExecStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		img, _ := config["image"].(string)
		if img == "" {
			return nil, fmt.Errorf("shell_exec step %q: 'image' is required", name)
		}

		cmdsRaw, ok := config["commands"].([]any)
		if !ok || len(cmdsRaw) == 0 {
			return nil, fmt.Errorf("shell_exec step %q: 'commands' is required and must be non-empty", name)
		}
		commands := make([]string, 0, len(cmdsRaw))
		for i, c := range cmdsRaw {
			s, ok := c.(string)
			if !ok {
				return nil, fmt.Errorf("shell_exec step %q: command %d must be a string", name, i)
			}
			commands = append(commands, s)
		}

		workDir, _ := config["work_dir"].(string)

		var timeout time.Duration
		if ts, ok := config["timeout"].(string); ok && ts != "" {
			var err error
			timeout, err = time.ParseDuration(ts)
			if err != nil {
				return nil, fmt.Errorf("shell_exec step %q: invalid timeout %q: %w", name, ts, err)
			}
		}

		env := make(map[string]string)
		if envRaw, ok := config["env"].(map[string]any); ok {
			for k, v := range envRaw {
				env[k] = fmt.Sprintf("%v", v)
			}
		}

		var artifactsOut []ArtifactOutput
		if aoRaw, ok := config["artifacts_out"].([]any); ok {
			for i, item := range aoRaw {
				m, ok := item.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("shell_exec step %q: artifacts_out[%d] must be a map", name, i)
				}
				key, _ := m["key"].(string)
				path, _ := m["path"].(string)
				if key == "" || path == "" {
					return nil, fmt.Errorf("shell_exec step %q: artifacts_out[%d] requires 'key' and 'path'", name, i)
				}
				artifactsOut = append(artifactsOut, ArtifactOutput{Key: key, Path: path})
			}
		}

		return &ShellExecStep{
			name:         name,
			image:        img,
			commands:     commands,
			workDir:      workDir,
			timeout:      timeout,
			env:          env,
			artifactsOut: artifactsOut,
		}, nil
	}
}

// Name returns the step name.
func (s *ShellExecStep) Name() string { return s.name }

// Execute runs each command in a Docker sandbox and collects artifacts.
func (s *ShellExecStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	cfg := sandbox.SandboxConfig{
		Image:   s.image,
		WorkDir: s.workDir,
		Env:     s.env,
		Timeout: s.timeout,
	}

	sb, err := sandbox.NewDockerSandbox(cfg)
	if err != nil {
		return nil, fmt.Errorf("shell_exec step %q: failed to create sandbox: %w", s.name, err)
	}
	defer sb.Close()

	outputs := make([]map[string]any, 0, len(s.commands))
	for i, cmd := range s.commands {
		result, err := sb.Exec(ctx, []string{"sh", "-c", cmd})
		if err != nil {
			return nil, fmt.Errorf("shell_exec step %q: command %d failed: %w", s.name, i, err)
		}
		if result.ExitCode != 0 {
			return nil, fmt.Errorf("shell_exec step %q: command %d exited with code %d: %s",
				s.name, i, result.ExitCode, result.Stderr)
		}
		outputs = append(outputs, map[string]any{
			"command":   cmd,
			"exit_code": result.ExitCode,
			"stdout":    result.Stdout,
			"stderr":    result.Stderr,
		})
	}

	// Collect artifacts if configured
	var artifactsMeta []map[string]any
	if len(s.artifactsOut) > 0 {
		executionID, _ := pc.Metadata["execution_id"].(string)
		var store artifact.Store
		if storeVal, ok := pc.Metadata["artifact_store"]; ok {
			store, _ = storeVal.(artifact.Store)
		}

		if store != nil && executionID != "" {
			for _, ao := range s.artifactsOut {
				// Use ExecInContainer to copy out the artifact
				copyCmd := []string{"cat", ao.Path}
				catResult, err := sb.Exec(ctx, copyCmd)
				if err != nil {
					return nil, fmt.Errorf("shell_exec step %q: failed to read artifact %q at %s: %w",
						s.name, ao.Key, ao.Path, err)
				}
				reader := io.NopCloser(strings.NewReader(catResult.Stdout))
				if err := store.Put(ctx, executionID, ao.Key, reader); err != nil {
					return nil, fmt.Errorf("shell_exec step %q: failed to store artifact %q: %w",
						s.name, ao.Key, err)
				}
				artifactsMeta = append(artifactsMeta, map[string]any{
					"key":  ao.Key,
					"path": ao.Path,
				})
			}
		}
	}

	output := map[string]any{
		"commands": outputs,
		"image":    s.image,
	}
	if len(artifactsMeta) > 0 {
		output["artifacts"] = artifactsMeta
	}

	return &StepResult{Output: output}, nil
}
