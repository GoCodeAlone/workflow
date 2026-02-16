package module

import (
	"context"
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/sandbox"
)

// DockerRunStep runs a command inside a Docker container using the sandbox.
type DockerRunStep struct {
	name        string
	image       string
	command     []string
	env         map[string]string
	waitForExit bool
	timeout     time.Duration
}

// NewDockerRunStepFactory returns a StepFactory that creates DockerRunStep instances.
func NewDockerRunStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		img, _ := config["image"].(string)
		if img == "" {
			return nil, fmt.Errorf("docker_run step %q: 'image' is required", name)
		}

		var command []string
		if cmdRaw, ok := config["command"].([]any); ok {
			for i, c := range cmdRaw {
				s, ok := c.(string)
				if !ok {
					return nil, fmt.Errorf("docker_run step %q: command[%d] must be a string", name, i)
				}
				command = append(command, s)
			}
		}

		env := make(map[string]string)
		if envRaw, ok := config["env"].(map[string]any); ok {
			for k, v := range envRaw {
				env[k] = fmt.Sprintf("%v", v)
			}
		}

		waitForExit := true
		if wfe, ok := config["wait_for_exit"].(bool); ok {
			waitForExit = wfe
		}

		var timeout time.Duration
		if ts, ok := config["timeout"].(string); ok && ts != "" {
			var err error
			timeout, err = time.ParseDuration(ts)
			if err != nil {
				return nil, fmt.Errorf("docker_run step %q: invalid timeout %q: %w", name, ts, err)
			}
		}

		return &DockerRunStep{
			name:        name,
			image:       img,
			command:     command,
			env:         env,
			waitForExit: waitForExit,
			timeout:     timeout,
		}, nil
	}
}

// Name returns the step name.
func (s *DockerRunStep) Name() string { return s.name }

// Execute runs the container and returns exit code, stdout, and stderr.
func (s *DockerRunStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	cfg := sandbox.SandboxConfig{
		Image:   s.image,
		Env:     s.env,
		Timeout: s.timeout,
	}

	sb, err := sandbox.NewDockerSandbox(cfg)
	if err != nil {
		return nil, fmt.Errorf("docker_run step %q: failed to create sandbox: %w", s.name, err)
	}
	defer sb.Close()

	cmd := s.command
	if len(cmd) == 0 {
		// Use the image's default entrypoint/cmd by passing a no-op
		cmd = []string{"true"}
	}

	result, err := sb.Exec(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("docker_run step %q: execution failed: %w", s.name, err)
	}

	return &StepResult{
		Output: map[string]any{
			"exit_code": result.ExitCode,
			"stdout":    result.Stdout,
			"stderr":    result.Stderr,
			"image":     s.image,
		},
	}, nil
}
