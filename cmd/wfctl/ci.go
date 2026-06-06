package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/GoCodeAlone/workflow/cigen"
	"github.com/mattn/go-isatty"
)

func runCI(args []string) error {
	if len(args) < 1 {
		return ciUsage()
	}
	switch args[0] {
	case "generate":
		return runCIGenerate(args[1:])
	case "plan":
		return runCIPlan(args[1:])
	case "run":
		return runCIRun(args[1:])
	case "init":
		return runCIInit(args[1:])
	case "validate":
		return runCIValidate(args[1:])
	case "--help", "-h", "help":
		_ = ciUsage()
		return nil
	default:
		return ciUsage()
	}
}

func ciUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl ci <action> [options]

Generate CI/CD pipeline configuration files.

Actions:
  generate  Generate CI config for a supported platform
  plan      Analyze config and emit a CIPlan JSON (platform-neutral)
  run       Run CI phases (build, test, deploy) from workflow config
  init      Generate bootstrap CI YAML for GitHub Actions or GitLab CI
  validate  Validate workflow CI config sections or rendered CI provider artifacts

Options:
  --platform <name>     CI platform: github_actions, gitlab_ci, jenkins, circleci (required for generate)
  --config <file>       Workflow config file (default: app.yaml or infra.yaml)
  --out <path>          Output path for generate files (directory, default: .)
  --from-plan <file>    Skip Analyze; load a CIPlan JSON directly
  --diff                Print unified diff vs on-disk file instead of writing
  --exit-code           With --diff: exit 1 when files differ, 0 when identical
  --write               Allow overwriting existing files
  --phase-config <file> Prerequisite phase config (adds a prereq DeployPhase)
  --runner <label>      Runner label (github_actions only, default: ubuntu-latest)

Examples:
  wfctl ci plan -c deploy.yaml --out -
  wfctl ci generate --platform github_actions --config deploy.yaml --write
  wfctl ci generate --platform github_actions --from-plan plan.json --write
  wfctl ci generate --platform github_actions --config deploy.yaml --diff --exit-code
`)
	return fmt.Errorf("missing or unknown action")
}

func runCIGenerate(args []string) error {
	fs := flag.NewFlagSet("ci generate", flag.ContinueOnError)
	platform := fs.String("platform", "", "CI platform: github_actions, gitlab_ci, jenkins, circleci")
	configFile := fs.String("config", "", "Workflow config file")
	configFileShort := fs.String("c", "", "Workflow config file (shorthand)")
	outputDir := fs.String("output", ".", "Output directory")
	out := fs.String("out", "", "Output directory (alias for --output)")
	runner := fs.String("runner", "ubuntu-latest", "Runner label (github_actions only)")
	fromPlan := fs.String("from-plan", "", "Load a CIPlan JSON file instead of analyzing")
	diff := fs.Bool("diff", false, "Print unified diff vs on-disk file instead of writing")
	exitCode := fs.Bool("exit-code", false, "With --diff: exit 1 when files differ")
	write := fs.Bool("write", false, "Allow overwriting existing files")
	phaseConfig := fs.String("phase-config", "", "Prerequisite phase config path")
	configPathAlias := fs.String("config-path-alias", "", "Logical repo-relative path for the primary config in generated CI (default: relativized real path)")
	phaseConfigAlias := fs.String("phase-config-alias", "", "Logical repo-relative path for the prereq config in generated CI")
	interactive := fs.Bool("interactive", false, "Force interactive wizard even when --platform is set")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Resolve config shorthand
	if *configFile == "" && *configFileShort != "" {
		*configFile = *configFileShort
	}
	// --out as alias for --output
	if *out != "" && *outputDir == "." {
		*outputDir = *out
	}

	// When --platform is absent, use the interactive wizard (TTY required).
	// When stdin is not a TTY and --platform is also absent, fail clearly.
	if *platform == "" && !*interactive {
		if !isatty.IsTerminal(os.Stdin.Fd()) {
			return fmt.Errorf("specify --platform for non-interactive generation (github_actions, gitlab_ci, jenkins, circleci)")
		}
		// Fall through — wizard will be run after the plan is built.
	}

	// Build the plan
	var plan *cigen.CIPlan
	if *fromPlan != "" {
		data, err := os.ReadFile(*fromPlan)
		if err != nil {
			return fmt.Errorf("ci generate: read plan: %w", err)
		}
		plan = &cigen.CIPlan{}
		if err := json.Unmarshal(data, plan); err != nil {
			return fmt.Errorf("ci generate: parse plan: %w", err)
		}
	} else {
		configPath, err := resolveCIConfig(*configFile)
		if err != nil {
			return err
		}
		opts := cigen.Options{
			WfctlVersion:     ciGeneratedWfctlVersion(),
			Runner:           *runner,
			PhaseConfig:      *phaseConfig,
			ConfigPathAlias:  *configPathAlias,
			PhaseConfigAlias: *phaseConfigAlias,
		}
		var analyzeErr error
		plan, analyzeErr = cigen.Analyze([]string{configPath}, opts)
		if analyzeErr != nil {
			return fmt.Errorf("ci generate: analyze: %w", analyzeErr)
		}
	}

	// Wizard: run when --platform is absent or --interactive is explicitly set.
	// The wizard populates choices which override plan fields and set the platform.
	resolvedPlatform := *platform
	if resolvedPlatform == "" || *interactive {
		choices, wizErr := runCIWizard(plan)
		if wizErr != nil {
			return fmt.Errorf("ci generate: wizard: %w", wizErr)
		}
		applyWizardOverrides(plan, choices)
		if resolvedPlatform == "" {
			resolvedPlatform = choices.Platform
		}
		// Wizard controls the write flag when it ran (unless already set via CLI)
		if !*write && choices.Write {
			*write = true
		}
	}

	// Render
	var files map[string]string
	var renderErr error
	switch resolvedPlatform {
	case "github_actions":
		files, renderErr = cigen.RenderGitHubActions(plan)
	case "gitlab_ci":
		files, renderErr = cigen.RenderGitLabCI(plan)
	case "jenkins":
		files, renderErr = cigen.RenderJenkins(plan)
	case "circleci":
		files, renderErr = cigen.RenderCircleCI(plan)
	default:
		return fmt.Errorf("unsupported platform %q (supported: github_actions, gitlab_ci, jenkins, circleci)", resolvedPlatform)
	}
	if renderErr != nil {
		return renderErr
	}
	if findings := cigen.ValidateRenderedFiles(resolvedPlatform, files); len(findings) > 0 {
		return fmt.Errorf("ci generate: rendered %s artifact failed validation: %s", resolvedPlatform, strings.Join(cigen.ValidationMessages(findings), "; "))
	}

	// --diff mode: print diff and optionally exit 1 if different
	if *diff {
		hasDiff := false
		for relPath, content := range files {
			destPath := resolveOutputPath(relPath, *outputDir)
			existing, err := os.ReadFile(destPath)
			if err != nil {
				// File doesn't exist — everything is a diff
				fmt.Printf("--- %s (new file)\n+++ %s\n", destPath, destPath)
				for _, line := range strings.Split(content, "\n") {
					fmt.Printf("+ %s\n", line)
				}
				hasDiff = true
				continue
			}
			if string(existing) != content {
				hasDiff = true
				fmt.Printf("--- %s\n+++ %s (generated)\n", destPath, destPath)
				printLineDiff(string(existing), content)
			}
		}
		if *exitCode && hasDiff {
			os.Exit(1)
		}
		return nil
	}

	// Write mode: write files, respecting --write flag
	if err := os.MkdirAll(*outputDir, 0o750); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	for relPath, content := range files {
		destPath := resolveOutputPath(relPath, *outputDir)
		if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
			return fmt.Errorf("create dir for %s: %w", destPath, err)
		}
		if _, err := os.Stat(destPath); err == nil && !*write {
			return fmt.Errorf("file %s already exists; use --write to overwrite", destPath)
		}
		if err := os.WriteFile(destPath, []byte(content), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", destPath, err)
		}
		fmt.Printf("wrote %s\n", destPath)
	}

	return nil
}

// resolveOutputPath determines the final destination for a generated file.
// When outputDir is "." (the default), paths that contain "/" are kept relative
// to cwd (e.g. .github/workflows/foo.yml stays as-is). When outputDir is an
// explicit non-"." directory, ALL generated paths are rooted there.
func resolveOutputPath(relPath, outputDir string) string {
	if outputDir == "" || outputDir == "." {
		return relPath
	}
	return filepath.Join(outputDir, relPath)
}

// printLineDiff prints a simple +/- line-level diff.
func printLineDiff(old, newStr string) {
	oldLines := strings.Split(old, "\n")
	newLines := strings.Split(newStr, "\n")

	maxLen := len(oldLines)
	if len(newLines) > maxLen {
		maxLen = len(newLines)
	}
	for i := 0; i < maxLen; i++ {
		var ov, nv string
		if i < len(oldLines) {
			ov = oldLines[i]
		}
		if i < len(newLines) {
			nv = newLines[i]
		}
		if ov != nv {
			if i < len(oldLines) {
				fmt.Printf("- %s\n", ov)
			}
			if i < len(newLines) {
				fmt.Printf("+ %s\n", nv)
			}
		}
	}
}

// ── Legacy API: keep ciOptions and generateCIFiles for backward-compat tests ─

// resolveCIConfig finds the config file or tries defaults.
func resolveCIConfig(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	for _, candidate := range []string{"app.yaml", "infra.yaml"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "infra.yaml", nil // fall back to the conventional name even if absent
}

// ciOptions is retained for backward-compatible internal use by tests.
type ciOptions struct {
	Platform    string
	InfraConfig string
	OutputDir   string
	Runner      string
	HasInfra    bool
	HasDatabase bool
}

// generateCIFiles is the legacy entry point used by existing tests.
// It builds a minimal CIPlan from ciOptions and delegates to the cigen renderers.
func generateCIFiles(opts ciOptions) (map[string]string, error) {
	switch opts.Platform {
	case "github_actions":
		return generateGitHubActions(opts)
	case "gitlab_ci":
		return generateGitLabCI(opts)
	case "jenkins":
		return generateJenkins(opts)
	case "circleci":
		return generateCircleCI(opts)
	default:
		return nil, fmt.Errorf("unsupported platform %q (supported: github_actions, gitlab_ci, jenkins, circleci)", opts.Platform)
	}
}

// generateGitHubActions builds a minimal CIPlan from opts and renders GHA files.
func generateGitHubActions(opts ciOptions) (map[string]string, error) {
	plan := ciOptionsToPlan(opts)
	return cigen.RenderGitHubActions(plan)
}

// generateGitLabCI builds a minimal CIPlan from opts and renders GitLab CI.
func generateGitLabCI(opts ciOptions) (map[string]string, error) {
	plan := ciOptionsToPlan(opts)
	return cigen.RenderGitLabCI(plan)
}

// generateJenkins builds a minimal CIPlan from opts and renders a Jenkinsfile.
func generateJenkins(opts ciOptions) (map[string]string, error) {
	plan := ciOptionsToPlan(opts)
	return cigen.RenderJenkins(plan)
}

// generateCircleCI builds a minimal CIPlan from opts and renders CircleCI config.
func generateCircleCI(opts ciOptions) (map[string]string, error) {
	plan := ciOptionsToPlan(opts)
	return cigen.RenderCircleCI(plan)
}

// ciOptionsToPlan converts legacy ciOptions to a minimal CIPlan.
func ciOptionsToPlan(opts ciOptions) *cigen.CIPlan {
	configPath := opts.InfraConfig
	if configPath == "" {
		configPath = "infra.yaml"
	}
	runner := opts.Runner
	if runner == "" {
		runner = "ubuntu-latest"
	}
	return &cigen.CIPlan{
		Project:       "infra",
		WfctlVersion:  ciGeneratedWfctlVersion(),
		DefaultBranch: "main",
		Runner:        runner,
		Phases: []cigen.DeployPhase{
			{Name: "deploy", ConfigPath: configPath},
		},
		Secrets:  []cigen.SecretRef{},
		Warnings: []string{},
		Triggers: cigen.TriggerSpec{PR: true, PushMain: true, Dispatch: true},
	}
}

var cleanReleaseTagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

func ciGeneratedWfctlVersion() string {
	if !cleanReleaseTagPattern.MatchString(version) {
		return "latest"
	}
	return version
}
