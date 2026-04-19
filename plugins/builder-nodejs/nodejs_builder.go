package buildernodejs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/plugin/builder"
)

// NodejsBuilder builds Node.js bundles via npm/yarn/pnpm.
type NodejsBuilder struct{}

// New returns a new NodejsBuilder.
func New() builder.Builder { return &NodejsBuilder{} }

func (n *NodejsBuilder) Name() string { return "nodejs" }

func (n *NodejsBuilder) Validate(cfg builder.Config) error {
	if script(cfg.Fields) == "" {
		return fmt.Errorf("nodejs builder: script is required (e.g. script: build)")
	}
	return nil
}

func (n *NodejsBuilder) Build(ctx context.Context, cfg builder.Config, out *builder.Outputs) error {
	if err := n.Validate(cfg); err != nil {
		return err
	}

	pm := packageManager(cfg.Fields)
	cwd := cfg.Path
	if cwd == "" {
		cwd = "."
	}
	scr := script(cfg.Fields)
	distDir := distPath(cfg.Fields, cwd)
	outputName := cfg.TargetName
	if outputName == "" {
		outputName = filepath.Base(cwd)
	}

	if os.Getenv("WFCTL_BUILD_DRY_RUN") == "1" {
		out.Artifacts = append(out.Artifacts, builder.Artifact{
			Name:  outputName,
			Kind:  "bundle",
			Paths: []string{distDir},
			Metadata: map[string]any{
				"dry_run":         true,
				"package_manager": pm,
				"script":          scr,
				"cwd":             cwd,
			},
		})
		return nil
	}

	// Install step: <pm> ci (or install equivalent).
	installArgs := installCommand(pm, cfg.Fields)
	installCmd := exec.CommandContext(ctx, installArgs[0], installArgs[1:]...)
	installCmd.Dir = cwd
	installCmd.Env = os.Environ()
	if output, err := installCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nodejs install: %w\n%s", err, output)
	}

	// Build step: <pm> run <script> [npm_flags...]
	runArgs := runCommand(pm, scr, cfg.Fields)
	runCmd := exec.CommandContext(ctx, runArgs[0], runArgs[1:]...)
	runCmd.Dir = cwd
	runCmd.Env = os.Environ()
	if output, err := runCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nodejs build: %w\n%s", err, output)
	}

	out.Artifacts = append(out.Artifacts, builder.Artifact{
		Name:  outputName,
		Kind:  "bundle",
		Paths: []string{distDir},
	})
	return nil
}

func (n *NodejsBuilder) SecurityLint(cfg builder.Config) []builder.Finding {
	var findings []builder.Finding

	// Warn if npm install is used instead of npm ci.
	if installCmd, ok := cfg.Fields["install_cmd"].(string); ok {
		if strings.Contains(installCmd, "npm install") {
			findings = append(findings, builder.Finding{
				Severity: "warn",
				Message:  "use `npm ci` instead of `npm install` for reproducible builds",
			})
		}
	}
	pm := packageManager(cfg.Fields)
	if pm == "npm" {
		// Also check npm_flags for install hints.
		if flags, ok := cfg.Fields["npm_flags"].(string); ok {
			if strings.Contains(flags, "--no-ci") {
				findings = append(findings, builder.Finding{
					Severity: "warn",
					Message:  "npm_flags contains --no-ci; builds may not be reproducible",
				})
			}
		}
	}

	// Warn if package-lock.json is absent (when path is a real directory).
	cwd := cfg.Path
	if cwd == "" {
		cwd = "."
	}
	lockFile := filepath.Join(cwd, "package-lock.json")
	if _, err := os.Stat(lockFile); os.IsNotExist(err) {
		findings = append(findings, builder.Finding{
			Severity: "warn",
			Message:  "package-lock.json not found; commit it for reproducible installs",
			File:     lockFile,
		})
	}

	return findings
}

// helpers

func script(fields map[string]any) string {
	s, _ := fields["script"].(string)
	return s
}

func packageManager(fields map[string]any) string {
	pm, _ := fields["package_manager"].(string)
	if pm == "" {
		pm = "npm"
	}
	return pm
}

func distPath(fields map[string]any, cwd string) string {
	d, _ := fields["dist"].(string)
	if d == "" {
		d = "dist"
	}
	return filepath.Join(cwd, d)
}

func installCommand(pm string, fields map[string]any) []string {
	switch pm {
	case "yarn":
		return []string{"yarn", "install", "--frozen-lockfile"}
	case "pnpm":
		return []string{"pnpm", "install", "--frozen-lockfile"}
	default:
		return []string{"npm", "ci"}
	}
}

func runCommand(pm, script string, fields map[string]any) []string {
	npmFlags, _ := fields["npm_flags"].(string)
	var args []string
	switch pm {
	case "yarn":
		args = []string{"yarn", script}
	case "pnpm":
		args = []string{"pnpm", "run", script}
	default:
		args = []string{"npm", "run", script}
		if npmFlags != "" {
			args = append(args, strings.Fields(npmFlags)...)
		}
	}
	return args
}
