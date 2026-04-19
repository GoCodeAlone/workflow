package buildercustom

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/GoCodeAlone/workflow/plugin/builder"
)

// CustomBuilder runs an arbitrary shell command as a build step.
type CustomBuilder struct{}

// New returns a new CustomBuilder.
func New() builder.Builder { return &CustomBuilder{} }

func (c *CustomBuilder) Name() string { return "custom" }

func (c *CustomBuilder) Validate(cfg builder.Config) error {
	cmd, _ := cfg.Fields["command"].(string)
	if cmd == "" {
		return fmt.Errorf("custom builder: command is required")
	}
	return nil
}

func (c *CustomBuilder) Build(ctx context.Context, cfg builder.Config, out *builder.Outputs) error {
	if err := c.Validate(cfg); err != nil {
		return err
	}

	command, _ := cfg.Fields["command"].(string)
	outputs := collectOutputPaths(cfg.Fields)
	timeoutStr, _ := cfg.Fields["timeout"].(string)

	if timeoutStr != "" {
		d, err := time.ParseDuration(timeoutStr)
		if err == nil {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, d)
			defer cancel()
		}
	}

	if os.Getenv("WFCTL_BUILD_DRY_RUN") == "1" {
		paths := outputs
		if len(paths) == 0 {
			paths = []string{"(dynamic)"}
		}
		out.Artifacts = append(out.Artifacts, builder.Artifact{
			Name:  cfg.TargetName,
			Kind:  "other",
			Paths: paths,
			Metadata: map[string]any{
				"dry_run": true,
				"command": command,
			},
		})
		return nil
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if envMap, ok := cfg.Fields["env"].(map[string]any); ok {
		for k, v := range envMap {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%v", k, v))
		}
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("custom builder: %w\n%s", err, output)
	}

	paths := outputs
	if len(paths) == 0 {
		paths = []string{"."}
	}
	out.Artifacts = append(out.Artifacts, builder.Artifact{
		Name:  cfg.TargetName,
		Kind:  "other",
		Paths: paths,
	})
	return nil
}

func (c *CustomBuilder) SecurityLint(_ builder.Config) []builder.Finding {
	return []builder.Finding{
		{Severity: "warn", Message: "custom builder cannot enforce hardening"},
	}
}

func collectOutputPaths(fields map[string]any) []string {
	raw, ok := fields["outputs"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
