package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/GoCodeAlone/workflow/cigen"
	"gopkg.in/yaml.v3"
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
  validate  Validate CI config sections

Options:
  --platform <name>     CI platform: github_actions, gitlab_ci (required for generate)
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
	platform := fs.String("platform", "", "CI platform: github_actions, gitlab_ci")
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

	if *platform == "" {
		return fmt.Errorf("--platform is required (github_actions, gitlab_ci)")
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
			WfctlVersion: ciGeneratedWfctlVersion(),
			Runner:       *runner,
			PhaseConfig:  *phaseConfig,
		}
		var analyzeErr error
		plan, analyzeErr = cigen.Analyze([]string{configPath}, opts)
		if analyzeErr != nil {
			return fmt.Errorf("ci generate: analyze: %w", analyzeErr)
		}
	}

	// Render
	var files map[string]string
	var renderErr error
	switch *platform {
	case "github_actions":
		files, renderErr = cigen.RenderGitHubActions(plan)
	case "gitlab_ci":
		files, renderErr = cigen.RenderGitLabCI(plan)
	default:
		return fmt.Errorf("unsupported platform %q (supported: github_actions, gitlab_ci)", *platform)
	}
	if renderErr != nil {
		return renderErr
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
// For paths containing directories (e.g. .github/workflows/foo.yml), the
// path is kept relative to cwd. For flat filenames, it is placed in outputDir.
func resolveOutputPath(relPath, outputDir string) string {
	if strings.Contains(relPath, "/") {
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

// detectModuleTypes parses the config YAML and returns which module categories exist.
func detectModuleTypes(cfgFile string) map[string]bool {
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		return map[string]bool{}
	}
	var parsed struct {
		Modules []struct {
			Type string `yaml:"type"`
		} `yaml:"modules"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return map[string]bool{}
	}
	result := map[string]bool{}
	for _, m := range parsed.Modules {
		switch {
		case strings.HasPrefix(m.Type, "infra."):
			result["infra"] = true
		case strings.HasPrefix(m.Type, "database."):
			result["database"] = true
		case strings.HasPrefix(m.Type, "platform."):
			result["platform"] = true
		}
	}
	return result
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
	default:
		return nil, fmt.Errorf("unsupported platform %q (supported: github_actions, gitlab_ci)", opts.Platform)
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

// renderCITemplate is kept for callers in ci_init.go and similar.
func renderCITemplate(name, tmplStr string, data any) (string, error) {
	tmpl, err := template.New(name).Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

var cleanReleaseTagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

func ciGeneratedWfctlVersion() string {
	if !cleanReleaseTagPattern.MatchString(version) {
		return "latest"
	}
	return version
}
