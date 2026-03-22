package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

func runCI(args []string) error {
	if len(args) < 1 {
		return ciUsage()
	}
	switch args[0] {
	case "generate":
		return runCIGenerate(args[1:])
	default:
		return ciUsage()
	}
}

func ciUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl ci <action> [options]

Generate CI/CD pipeline configuration files.

Actions:
  generate  Generate CI config for a supported platform

Options:
  --platform <name>   CI platform: github_actions, gitlab_ci (required)
  --config <file>     Workflow config file (default: app.yaml or infra.yaml)
  --output <dir>      Output directory (default: .)
  --runner <label>    Runner label (github_actions only, default: ubuntu-latest)

Examples:
  wfctl ci generate --platform github_actions --config infra.yaml --output .github/workflows/
  wfctl ci generate --platform gitlab_ci --config infra.yaml --output .
`)
	return fmt.Errorf("missing or unknown action")
}

func runCIGenerate(args []string) error {
	fs := flag.NewFlagSet("ci generate", flag.ContinueOnError)
	platform := fs.String("platform", "", "CI platform: github_actions, gitlab_ci")
	configFile := fs.String("config", "", "Workflow config file")
	outputDir := fs.String("output", ".", "Output directory")
	runner := fs.String("runner", "ubuntu-latest", "Runner label (github_actions only)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *platform == "" {
		return fmt.Errorf("--platform is required (github_actions, gitlab_ci)")
	}

	cfg, err := resolveCIConfig(*configFile)
	if err != nil {
		return err
	}

	moduleTypes := detectModuleTypes(cfg)

	opts := ciOptions{
		Platform:    *platform,
		InfraConfig: cfg,
		OutputDir:   *outputDir,
		Runner:      *runner,
		HasInfra:    moduleTypes["infra"],
		HasDatabase: moduleTypes["database"],
	}

	files, err := generateCIFiles(opts)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(*outputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	for relPath, content := range files {
		dest := filepath.Join(*outputDir, filepath.Base(relPath))
		// Preserve subdirectory structure relative to output for GHA workflows
		if strings.Contains(relPath, "/") {
			// relPath is already a full relative path like .github/workflows/infra.yml
			// Write relative to cwd, not outputDir
			dest = relPath
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return fmt.Errorf("create dir for %s: %w", dest, err)
			}
		}
		if err := os.WriteFile(dest, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dest, err)
		}
		fmt.Printf("wrote %s\n", dest)
	}

	return nil
}

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

type ciOptions struct {
	Platform    string
	InfraConfig string
	OutputDir   string
	Runner      string
	HasInfra    bool
	HasDatabase bool
}

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

// ── GitHub Actions ────────────────────────────────────────────────────────────

type ghaTemplateData struct {
	InfraConfig string
	Runner      string
	Branch      string
}

func generateGitHubActions(opts ciOptions) (map[string]string, error) {
	data := ghaTemplateData{
		InfraConfig: opts.InfraConfig,
		Runner:      opts.Runner,
		Branch:      "main",
	}

	infraYAML, err := renderCITemplate("gha-infra", ghaInfraTemplate, data)
	if err != nil {
		return nil, fmt.Errorf("render infra.yml: %w", err)
	}

	buildYAML, err := renderCITemplate("gha-build", ghaBuildTemplate, data)
	if err != nil {
		return nil, fmt.Errorf("render build.yml: %w", err)
	}

	return map[string]string{
		".github/workflows/infra.yml": infraYAML,
		".github/workflows/build.yml": buildYAML,
	}, nil
}

const ghaInfraTemplate = `name: Infrastructure
on:
  pull_request:
    paths:
      - '{{.InfraConfig}}'
      - 'infra/**'
  push:
    branches:
      - {{.Branch}}
    paths:
      - '{{.InfraConfig}}'
      - 'infra/**'
permissions:
  contents: read
  pull-requests: write
jobs:
  plan:
    if: github.event_name == 'pull_request'
    runs-on: '{{.Runner}}'
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true
      - name: Install wfctl
        run: go install github.com/GoCodeAlone/workflow/cmd/wfctl@latest
      - name: Plan infrastructure
        run: wfctl infra plan --config '{{.InfraConfig}}' --format markdown > plan.md
      - name: Post plan comment
        uses: actions/github-script@v7
        with:
          script: |
            const fs = require('fs');
            const plan = fs.readFileSync('plan.md', 'utf8');
            github.rest.issues.createComment({
              issue_number: context.issue.number,
              owner: context.repo.owner,
              repo: context.repo.repo,
              body: plan
            });
  apply:
    if: github.event_name == 'push' && github.ref == 'refs/heads/{{.Branch}}'
    runs-on: '{{.Runner}}'
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true
      - name: Install wfctl
        run: go install github.com/GoCodeAlone/workflow/cmd/wfctl@latest
      - name: Apply infrastructure
        run: wfctl infra apply --config '{{.InfraConfig}}' --auto-approve
`

const ghaBuildTemplate = `name: Build
on:
  push:
    branches:
      - {{.Branch}}
  pull_request:
    branches:
      - {{.Branch}}
permissions:
  contents: read
  packages: write
jobs:
  build:
    runs-on: '{{.Runner}}'
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true
      - name: Run tests
        run: go test ./...
      - name: Build
        run: go build ./...
`

// ── GitLab CI ─────────────────────────────────────────────────────────────────

type gitlabTemplateData struct {
	InfraConfig string
	Branch      string
}

func generateGitLabCI(opts ciOptions) (map[string]string, error) {
	data := gitlabTemplateData{
		InfraConfig: opts.InfraConfig,
		Branch:      "main",
	}

	content, err := renderCITemplate("gitlab-ci", gitlabCITemplate, data)
	if err != nil {
		return nil, fmt.Errorf("render .gitlab-ci.yml: %w", err)
	}

	return map[string]string{
		".gitlab-ci.yml": content,
	}, nil
}

const gitlabCITemplate = `stages:
  - plan
  - apply
  - build

variables:
  INFRA_CONFIG: "{{.InfraConfig}}"

infra-plan:
  stage: plan
  script:
    - go install github.com/GoCodeAlone/workflow/cmd/wfctl@latest
    - wfctl infra plan --config "$INFRA_CONFIG" --format markdown > plan.md
  artifacts:
    paths:
      - plan.md
    expire_in: 1 hour
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
      changes:
        - "{{.InfraConfig}}"
        - "infra/**/*"

infra-apply:
  stage: apply
  needs:
    - job: infra-plan
      artifacts: true
  script:
    - go install github.com/GoCodeAlone/workflow/cmd/wfctl@latest
    - wfctl infra apply --config "$INFRA_CONFIG" --auto-approve
  environment:
    name: production
  rules:
    - if: $CI_COMMIT_BRANCH == "{{.Branch}}" && $CI_PIPELINE_SOURCE == "push"
      changes:
        - "{{.InfraConfig}}"
        - "infra/**/*"

build:
  stage: build
  needs: []
  script:
    - go test ./...
    - go build ./...
  rules:
    - if: $CI_COMMIT_BRANCH == "{{.Branch}}"
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
`

// renderCITemplate executes a named text/template with data and returns the result.
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
