package module

import (
	"context"
	"fmt"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/sandbox"
)

const defaultSandboxImage = "cgr.dev/chainguard/wolfi-base:latest"

// SandboxExecStep runs a command in a hardened Docker sandbox container.
type SandboxExecStep struct {
	name            string
	image           string
	command         []string
	securityProfile string
	execEnv         string // "" or "local-docker" → local Docker; "ephemeral" → Argo; others are remote runner names
	argoModule      string // optional: argo.workflows module name for exec_env: ephemeral
	memoryLimit     int64
	cpuLimit        float64
	timeout         time.Duration
	network         string
	env             map[string]string
	mounts          []sandbox.Mount
	failOnError     bool
	app             modular.Application
}

// NewSandboxExecStepFactory returns a StepFactory for step.sandbox_exec.
func NewSandboxExecStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		step := &SandboxExecStep{
			name:            name,
			image:           defaultSandboxImage,
			securityProfile: "strict",
			failOnError:     true,
			app:             app,
		}

		if img, ok := cfg["image"].(string); ok && img != "" {
			step.image = img
		}

		// command
		switch v := cfg["command"].(type) {
		case []any:
			for i, c := range v {
				s, ok := c.(string)
				if !ok {
					return nil, fmt.Errorf("sandbox_exec step %q: command[%d] must be a string", name, i)
				}
				step.command = append(step.command, s)
			}
		case []string:
			step.command = v
		case nil:
			// allowed — step may be used without a command for future use
		default:
			return nil, fmt.Errorf("sandbox_exec step %q: 'command' must be a list of strings", name)
		}

		if profile, ok := cfg["security_profile"].(string); ok && profile != "" {
			switch profile {
			case "strict", "standard", "permissive":
				step.securityProfile = profile
			default:
				return nil, fmt.Errorf("sandbox_exec step %q: security_profile must be strict, standard, or permissive", name)
			}
		}

		if ms, ok := cfg["memory_limit"].(string); ok && ms != "" {
			limit, err := parseMemoryLimit(ms)
			if err != nil {
				return nil, fmt.Errorf("sandbox_exec step %q: invalid memory_limit: %w", name, err)
			}
			step.memoryLimit = limit
		}

		if cpu, ok := cfg["cpu_limit"].(float64); ok {
			step.cpuLimit = cpu
		}

		if ts, ok := cfg["timeout"].(string); ok && ts != "" {
			d, err := time.ParseDuration(ts)
			if err != nil {
				return nil, fmt.Errorf("sandbox_exec step %q: invalid timeout %q: %w", name, ts, err)
			}
			step.timeout = d
		}

		if net, ok := cfg["network"].(string); ok && net != "" {
			step.network = net
		}

		if ee, ok := cfg["exec_env"].(string); ok && ee != "" {
			// exec_env validation: "local-docker" is the local runner;
			// "ephemeral" routes to the Argo Workflows ephemeral runner (PR9);
			// any other non-empty string is treated as a named remote runner and
			// validated at Execute time by resolveSandboxRunner (PR8). We no longer
			// reject unknown values at construction time since named runner
			// registrations are config-driven and not known until runtime.
			step.execEnv = ee
		}

		if am, ok := cfg["argo_module"].(string); ok && am != "" {
			// argo_module names the argo.workflows service to use when
			// exec_env is "ephemeral". If unset, the factory auto-detects the
			// sole registered *ArgoWorkflowsModule (error if 0 or >1 found).
			step.argoModule = am
		}

		if envRaw, ok := cfg["env"].(map[string]any); ok {
			step.env = make(map[string]string, len(envRaw))
			for k, v := range envRaw {
				step.env[k] = fmt.Sprintf("%v", v)
			}
		}

		if mountsRaw, ok := cfg["mounts"].([]any); ok {
			for i, m := range mountsRaw {
				mmap, ok := m.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("sandbox_exec step %q: mounts[%d] must be a map", name, i)
				}
				src, _ := mmap["source"].(string)
				tgt, _ := mmap["target"].(string)
				ro, _ := mmap["read_only"].(bool)
				step.mounts = append(step.mounts, sandbox.Mount{Source: src, Target: tgt, ReadOnly: ro})
			}
		}

		if foe, ok := cfg["fail_on_error"].(bool); ok {
			step.failOnError = foe
		}

		return step, nil
	}
}

// Name returns the step name.
func (s *SandboxExecStep) Name() string { return s.name }

// Execute runs the configured command in a Docker sandbox.
func (s *SandboxExecStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	sbCfg := s.buildSandboxConfig()

	sb, err := resolveSandboxRunner(ctx, s.app, s.execEnv, sbCfg, s.argoModule)
	if err != nil {
		return nil, fmt.Errorf("sandbox_exec step %q: failed to create sandbox: %w", s.name, err)
	}
	defer sb.Close()

	result, err := sb.Exec(ctx, s.command)
	if err != nil {
		return nil, fmt.Errorf("sandbox_exec step %q: execution failed: %w", s.name, err)
	}

	output := map[string]any{
		"exit_code": result.ExitCode,
		"stdout":    result.Stdout,
		"stderr":    result.Stderr,
	}

	if result.ExitCode != 0 && s.failOnError {
		return &StepResult{Output: output, Stop: true}, nil
	}

	return &StepResult{Output: output}, nil
}

// buildSandboxConfig constructs a SandboxConfig based on the security profile
// and any explicit overrides provided in the step config.
func (s *SandboxExecStep) buildSandboxConfig() sandbox.SandboxConfig {
	// Delegate profile→config mapping to the shared sandbox package function so
	// remote runners (PR7/8) can reuse the same profile clamping logic.
	cfg := sandbox.BuildSandboxConfig(s.securityProfile, s.image)

	// Apply explicit overrides
	if s.memoryLimit > 0 {
		cfg.MemoryLimit = s.memoryLimit
	}
	if s.cpuLimit > 0 {
		cfg.CPULimit = s.cpuLimit
	}
	if s.timeout > 0 {
		cfg.Timeout = s.timeout
	}
	if s.network != "" {
		cfg.NetworkMode = s.network
	}
	if len(s.env) > 0 {
		cfg.Env = s.env
	}
	if len(s.mounts) > 0 {
		cfg.Mounts = s.mounts
	}

	return cfg
}

// parseMemoryLimit parses a human-readable memory string (e.g., "128m", "1g") into bytes.
func parseMemoryLimit(s string) (int64, error) {
	if len(s) == 0 {
		return 0, fmt.Errorf("empty memory limit")
	}
	last := s[len(s)-1]
	var multiplier int64 = 1
	numStr := s
	switch last {
	case 'k', 'K':
		multiplier = 1024
		numStr = s[:len(s)-1]
	case 'm', 'M':
		multiplier = 1024 * 1024
		numStr = s[:len(s)-1]
	case 'g', 'G':
		multiplier = 1024 * 1024 * 1024
		numStr = s[:len(s)-1]
	case 'b', 'B':
		numStr = s[:len(s)-1]
	}

	var n int64
	_, err := fmt.Sscanf(numStr, "%d", &n)
	if err != nil {
		return 0, fmt.Errorf("invalid memory limit %q", s)
	}
	return n * multiplier, nil
}
