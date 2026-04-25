---
status: implemented
area: wfctl
owner: workflow
implementation_refs:
  - repo: workflow
    commit: 119deea
  - repo: workflow
    commit: 31d4447
  - repo: workflow
    commit: fe295a7
external_refs: []
verification:
  last_checked: 2026-04-25
  commands:
    - 'rg -n "type CIConfig|func runCI|secrets detect|secrets setup" config cmd docs -S'
    - 'git log --oneline --all -- config/ci_config.go config/environments_config.go config/secrets_config.go cmd/wfctl/ci.go cmd/wfctl/secrets.go'
  result: pass
supersedes: []
superseded_by: []
---

# Tier 1 Platform Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement the foundation for the platform vision: expanded GitHub plugin, `ci:` config section with `wfctl ci run`, `environments:` config section, and `secrets:` config section with `wfctl secrets` commands.

**Architecture:** New config structs added to `config/config.go` for `ci:`, `environments:`, and `secrets:` top-level YAML keys. `wfctl ci run` reads the ci: section and executes build/test phases. `wfctl secrets` manages secret lifecycle with provider abstraction. GitHub plugin expanded with `google/go-github/v69` SDK. All using Go 1.26 modern idioms.

**Tech Stack:** Go 1.26, google/go-github/v69, github.com/mcp-go/mcp (existing MCP server)

**Design Doc:** `docs/plans/2026-03-28-platform-vision-design.md`

---

### Task 1: Config structs for ci:, environments:, secrets: sections

**Files:**
- Create: `config/ci_config.go`
- Create: `config/ci_config_test.go`
- Create: `config/environments_config.go`
- Create: `config/secrets_config.go`
- Modify: `config/config.go` — add fields to WorkflowConfig

**Step 1: Create ci_config.go with CIConfig structs**

```go
// config/ci_config.go
package config

// CIConfig holds the ci: section of a workflow config — build, test, and deploy lifecycle.
type CIConfig struct {
	Build  *CIBuildConfig             `json:"build,omitempty" yaml:"build,omitempty"`
	Test   *CITestConfig              `json:"test,omitempty" yaml:"test,omitempty"`
	Deploy *CIDeployConfig            `json:"deploy,omitempty" yaml:"deploy,omitempty"`
	Infra  *CIInfraConfig             `json:"infra,omitempty" yaml:"infra,omitempty"`
}

// CIBuildConfig defines what artifacts the build phase produces.
type CIBuildConfig struct {
	Binaries   []CIBinaryTarget    `json:"binaries,omitempty" yaml:"binaries,omitempty"`
	Containers []CIContainerTarget `json:"containers,omitempty" yaml:"containers,omitempty"`
	Assets     []CIAssetTarget     `json:"assets,omitempty" yaml:"assets,omitempty"`
}

// CIBinaryTarget is a Go binary to compile.
type CIBinaryTarget struct {
	Name    string            `json:"name" yaml:"name"`
	Path    string            `json:"path" yaml:"path"`
	OS      []string          `json:"os,omitempty" yaml:"os,omitempty"`
	Arch    []string          `json:"arch,omitempty" yaml:"arch,omitempty"`
	LDFlags string            `json:"ldflags,omitempty" yaml:"ldflags,omitempty"`
	Env     map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
}

// CIContainerTarget is a container image to build.
type CIContainerTarget struct {
	Name       string `json:"name" yaml:"name"`
	Dockerfile string `json:"dockerfile,omitempty" yaml:"dockerfile,omitempty"`
	Context    string `json:"context,omitempty" yaml:"context,omitempty"`
	Registry   string `json:"registry,omitempty" yaml:"registry,omitempty"`
	Tag        string `json:"tag,omitempty" yaml:"tag,omitempty"`
}

// CIAssetTarget is a non-binary build artifact (e.g., frontend bundle).
type CIAssetTarget struct {
	Name  string `json:"name" yaml:"name"`
	Build string `json:"build" yaml:"build"`
	Path  string `json:"path" yaml:"path"`
}

// CITestConfig defines test phases.
type CITestConfig struct {
	Unit        *CITestPhase `json:"unit,omitempty" yaml:"unit,omitempty"`
	Integration *CITestPhase `json:"integration,omitempty" yaml:"integration,omitempty"`
	E2E         *CITestPhase `json:"e2e,omitempty" yaml:"e2e,omitempty"`
}

// CITestPhase is a single test phase.
type CITestPhase struct {
	Command  string   `json:"command" yaml:"command"`
	Coverage bool     `json:"coverage,omitempty" yaml:"coverage,omitempty"`
	Needs    []string `json:"needs,omitempty" yaml:"needs,omitempty"`
}

// CIDeployConfig defines deployment environments.
type CIDeployConfig struct {
	Environments map[string]*CIDeployEnvironment `json:"environments,omitempty" yaml:"environments,omitempty"`
}

// CIDeployEnvironment is a single deployment target.
type CIDeployEnvironment struct {
	Provider        string         `json:"provider" yaml:"provider"`
	Cluster         string         `json:"cluster,omitempty" yaml:"cluster,omitempty"`
	Namespace       string         `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Region          string         `json:"region,omitempty" yaml:"region,omitempty"`
	Strategy        string         `json:"strategy,omitempty" yaml:"strategy,omitempty"`
	RequireApproval bool           `json:"requireApproval,omitempty" yaml:"requireApproval,omitempty"`
	PreDeploy       []string       `json:"preDeploy,omitempty" yaml:"preDeploy,omitempty"`
	HealthCheck     *CIHealthCheck `json:"healthCheck,omitempty" yaml:"healthCheck,omitempty"`
}

// CIHealthCheck defines how to verify a deployment is healthy.
type CIHealthCheck struct {
	Path    string `json:"path" yaml:"path"`
	Timeout string `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}

// CIInfraConfig defines infrastructure provisioning for CI.
type CIInfraConfig struct {
	Provision    bool                `json:"provision" yaml:"provision"`
	StateBackend string              `json:"stateBackend,omitempty" yaml:"stateBackend,omitempty"`
	Resources    []InfraResourceConfig `json:"resources,omitempty" yaml:"resources,omitempty"`
}
```

**Step 2: Create environments_config.go**

```go
// config/environments_config.go
package config

// EnvironmentConfig defines a deployment environment with its provider and overrides.
type EnvironmentConfig struct {
	Provider        string            `json:"provider" yaml:"provider"`
	Region          string            `json:"region,omitempty" yaml:"region,omitempty"`
	EnvVars         map[string]string `json:"envVars,omitempty" yaml:"envVars,omitempty"`
	SecretsProvider string            `json:"secretsProvider,omitempty" yaml:"secretsProvider,omitempty"`
	SecretsPrefix   string            `json:"secretsPrefix,omitempty" yaml:"secretsPrefix,omitempty"`
	ApprovalRequired bool            `json:"approvalRequired,omitempty" yaml:"approvalRequired,omitempty"`
	Exposure        *ExposureConfig   `json:"exposure,omitempty" yaml:"exposure,omitempty"`
}

// ExposureConfig defines how a service is exposed to the network.
type ExposureConfig struct {
	Method          string                 `json:"method" yaml:"method"`
	Tailscale       *TailscaleConfig       `json:"tailscale,omitempty" yaml:"tailscale,omitempty"`
	CloudflareTunnel *CloudflareTunnelConfig `json:"cloudflareTunnel,omitempty" yaml:"cloudflareTunnel,omitempty"`
	PortForward     map[string]string      `json:"portForward,omitempty" yaml:"portForward,omitempty"`
}

// TailscaleConfig for Tailscale Funnel exposure.
type TailscaleConfig struct {
	Funnel   bool   `json:"funnel,omitempty" yaml:"funnel,omitempty"`
	Hostname string `json:"hostname,omitempty" yaml:"hostname,omitempty"`
}

// CloudflareTunnelConfig for Cloudflare Tunnel exposure.
type CloudflareTunnelConfig struct {
	TunnelName string `json:"tunnelName,omitempty" yaml:"tunnelName,omitempty"`
	Domain     string `json:"domain,omitempty" yaml:"domain,omitempty"`
}
```

**Step 3: Create secrets_config.go**

```go
// config/secrets_config.go
package config

// SecretsConfig defines secret management for the application.
type SecretsConfig struct {
	Provider string                `json:"provider" yaml:"provider"`
	Config   map[string]any        `json:"config,omitempty" yaml:"config,omitempty"`
	Rotation *SecretsRotationConfig `json:"rotation,omitempty" yaml:"rotation,omitempty"`
	Entries  []SecretEntry         `json:"entries,omitempty" yaml:"entries,omitempty"`
}

// SecretsRotationConfig defines default rotation policy.
type SecretsRotationConfig struct {
	Enabled  bool   `json:"enabled" yaml:"enabled"`
	Interval string `json:"interval,omitempty" yaml:"interval,omitempty"`
}

// SecretEntry declares a single secret the application needs.
type SecretEntry struct {
	Name        string                `json:"name" yaml:"name"`
	Description string                `json:"description,omitempty" yaml:"description,omitempty"`
	Rotation    *SecretsRotationConfig `json:"rotation,omitempty" yaml:"rotation,omitempty"`
}
```

**Step 4: Add fields to WorkflowConfig in config.go**

Add these three fields to the `WorkflowConfig` struct after the `Engine` field:

```go
CI           *CIConfig                       `json:"ci,omitempty" yaml:"ci,omitempty"`
Environments map[string]*EnvironmentConfig   `json:"environments,omitempty" yaml:"environments,omitempty"`
Secrets      *SecretsConfig                  `json:"secrets,omitempty" yaml:"secrets,omitempty"`
```

**Step 5: Write tests**

```go
// config/ci_config_test.go
package config

import (
	"testing"
	"gopkg.in/yaml.v3"
)

func TestCIConfig_ParseYAML(t *testing.T) {
	yamlStr := `
ci:
  build:
    binaries:
      - name: server
        path: ./cmd/server
        os: [linux]
        arch: [amd64, arm64]
  test:
    unit:
      command: go test ./... -race
  deploy:
    environments:
      staging:
        provider: aws-ecs
        strategy: rolling
secrets:
  provider: env
  entries:
    - name: DATABASE_URL
      description: PostgreSQL connection string
environments:
  local:
    provider: docker
    envVars:
      LOG_LEVEL: debug
`
	var cfg WorkflowConfig
	if err := yaml.Unmarshal([]byte(yamlStr), &cfg); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if cfg.CI == nil { t.Fatal("ci section missing") }
	if len(cfg.CI.Build.Binaries) != 1 { t.Fatalf("expected 1 binary, got %d", len(cfg.CI.Build.Binaries)) }
	if cfg.CI.Build.Binaries[0].Name != "server" { t.Errorf("expected 'server', got %q", cfg.CI.Build.Binaries[0].Name) }
	if cfg.CI.Test.Unit.Command != "go test ./... -race" { t.Errorf("unexpected test command") }
	if cfg.CI.Deploy.Environments["staging"].Provider != "aws-ecs" { t.Errorf("unexpected provider") }
	if cfg.Secrets == nil { t.Fatal("secrets section missing") }
	if cfg.Secrets.Provider != "env" { t.Errorf("expected env provider") }
	if len(cfg.Secrets.Entries) != 1 { t.Fatalf("expected 1 secret entry") }
	if cfg.Environments == nil { t.Fatal("environments section missing") }
	if cfg.Environments["local"].Provider != "docker" { t.Errorf("expected docker provider") }
	if cfg.Environments["local"].EnvVars["LOG_LEVEL"] != "debug" { t.Errorf("expected debug log level") }
}
```

**Step 6:** Run `go test ./config/ -run TestCIConfig -v`

**Step 7:** Commit: `feat: add ci:, environments:, secrets: config sections`

---

### Task 2: wfctl ci run — build and test phases

**Files:**
- Create: `cmd/wfctl/ci_run.go`
- Create: `cmd/wfctl/ci_run_test.go`
- Modify: `cmd/wfctl/ci.go` — add "run" subcommand dispatch

**Step 1: Add "run" case to runCI dispatch**

In `cmd/wfctl/ci.go`, add to the switch in `runCI()`:
```go
case "run":
    return runCIRun(args[1:])
```

**Step 2: Create ci_run.go**

```go
// cmd/wfctl/ci_run.go
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"gopkg.in/yaml.v3"
)

func runCIRun(args []string) error {
	fs := flag.NewFlagSet("ci run", flag.ContinueOnError)
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	phases := fs.String("phase", "build,test", "Comma-separated phases: build, test, deploy")
	env := fs.String("env", "", "Target environment (required for deploy phase)")
	verbose := fs.Bool("verbose", false, "Show detailed output")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl ci run [options]\n\nRun CI phases from workflow config.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	data, err := os.ReadFile(*configFile)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var cfg config.WorkflowConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	if cfg.CI == nil {
		return fmt.Errorf("no ci: section in %s", *configFile)
	}

	phaseList := strings.Split(*phases, ",")
	for _, phase := range phaseList {
		switch strings.TrimSpace(phase) {
		case "build":
			if err := runBuildPhase(cfg.CI.Build, *verbose); err != nil {
				return fmt.Errorf("build phase failed: %w", err)
			}
		case "test":
			if err := runTestPhase(cfg.CI.Test, *verbose); err != nil {
				return fmt.Errorf("test phase failed: %w", err)
			}
		case "deploy":
			if *env == "" {
				return fmt.Errorf("--env is required for deploy phase")
			}
			if err := runDeployPhase(cfg.CI.Deploy, *env, *verbose); err != nil {
				return fmt.Errorf("deploy phase failed: %w", err)
			}
		default:
			return fmt.Errorf("unknown phase: %q (valid: build, test, deploy)", phase)
		}
	}
	return nil
}

func runBuildPhase(build *config.CIBuildConfig, verbose bool) error {
	if build == nil {
		fmt.Println("No build configuration, skipping build phase")
		return nil
	}

	// Build binaries
	for _, bin := range build.Binaries {
		osList := bin.OS
		if len(osList) == 0 {
			osList = []string{runtime.GOOS}
		}
		archList := bin.Arch
		if len(archList) == 0 {
			archList = []string{runtime.GOARCH}
		}
		for _, goos := range osList {
			for _, goarch := range archList {
				outputName := fmt.Sprintf("bin/%s-%s-%s", bin.Name, goos, goarch)
				fmt.Printf("Building %s (%s/%s)...\n", bin.Name, goos, goarch)

				buildArgs := []string{"build", "-o", outputName}
				if bin.LDFlags != "" {
					ldflags := os.ExpandEnv(bin.LDFlags)
					buildArgs = append(buildArgs, "-ldflags", ldflags)
				}
				buildArgs = append(buildArgs, bin.Path)

				cmd := exec.Command("go", buildArgs...)
				cmd.Env = append(os.Environ(),
					"GOOS="+goos,
					"GOARCH="+goarch,
				)
				for k, v := range bin.Env {
					cmd.Env = append(cmd.Env, k+"="+os.ExpandEnv(v))
				}
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					return fmt.Errorf("build %s (%s/%s): %w", bin.Name, goos, goarch, err)
				}
				fmt.Printf("  ✓ %s\n", outputName)
			}
		}
	}

	// Build containers
	for _, ctr := range build.Containers {
		fmt.Printf("Building container %s...\n", ctr.Name)
		dockerfile := ctr.Dockerfile
		if dockerfile == "" {
			dockerfile = "Dockerfile"
		}
		context := ctr.Context
		if context == "" {
			context = "."
		}
		tag := os.ExpandEnv(ctr.Tag)
		if tag == "" {
			tag = "latest"
		}
		registry := os.ExpandEnv(ctr.Registry)
		imageName := ctr.Name + ":" + tag
		if registry != "" && registry != "local" {
			imageName = registry + "/" + imageName
		}

		cmd := exec.Command("docker", "build", "-f", dockerfile, "-t", imageName, context)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("build container %s: %w", ctr.Name, err)
		}
		fmt.Printf("  ✓ %s\n", imageName)
	}

	// Build assets
	for _, asset := range build.Assets {
		fmt.Printf("Building asset %s...\n", asset.Name)
		cmd := exec.Command("sh", "-c", asset.Build)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("build asset %s: %w", asset.Name, err)
		}
		fmt.Printf("  ✓ %s → %s\n", asset.Name, asset.Path)
	}

	return nil
}

func runTestPhase(test *config.CITestConfig, verbose bool) error {
	if test == nil {
		fmt.Println("No test configuration, skipping test phase")
		return nil
	}

	phases := []struct {
		name  string
		phase *config.CITestPhase
	}{
		{"unit", test.Unit},
		{"integration", test.Integration},
		{"e2e", test.E2E},
	}

	for _, p := range phases {
		if p.phase == nil {
			continue
		}
		fmt.Printf("Running %s tests...\n", p.name)
		start := time.Now()

		cmd := exec.Command("sh", "-c", p.phase.Command)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s tests failed: %w", p.name, err)
		}
		fmt.Printf("  ✓ %s tests passed (%s)\n", p.name, time.Since(start).Truncate(time.Millisecond))
	}

	return nil
}

func runDeployPhase(deploy *config.CIDeployConfig, envName string, verbose bool) error {
	if deploy == nil {
		return fmt.Errorf("no deploy configuration")
	}
	env, ok := deploy.Environments[envName]
	if !ok {
		available := make([]string, 0, len(deploy.Environments))
		for k := range deploy.Environments {
			available = append(available, k)
		}
		return fmt.Errorf("environment %q not found (available: %s)", envName, strings.Join(available, ", "))
	}

	if env.RequireApproval {
		fmt.Printf("⚠ Environment %q requires approval — skipping in non-interactive mode\n", envName)
		return nil
	}

	fmt.Printf("Deploying to %s (provider: %s, strategy: %s)...\n", envName, env.Provider, env.Strategy)
	// TODO: implement actual deployment providers in Tier 2
	fmt.Printf("  ⚠ Deploy phase is a placeholder — full implementation in Tier 2\n")
	return nil
}
```

**Step 3: Write tests**

```go
// cmd/wfctl/ci_run_test.go
package main

import (
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestRunBuildPhase_NilConfig(t *testing.T) {
	if err := runBuildPhase(nil, false); err != nil {
		t.Fatalf("nil build config should not error: %v", err)
	}
}

func TestRunTestPhase_NilConfig(t *testing.T) {
	if err := runTestPhase(nil, false); err != nil {
		t.Fatalf("nil test config should not error: %v", err)
	}
}

func TestRunDeployPhase_MissingEnv(t *testing.T) {
	deploy := &config.CIDeployConfig{
		Environments: map[string]*config.CIDeployEnvironment{
			"staging": {Provider: "aws-ecs"},
		},
	}
	err := runDeployPhase(deploy, "production", false)
	if err == nil {
		t.Fatal("expected error for missing environment")
	}
}

func TestRunDeployPhase_RequiresApproval(t *testing.T) {
	deploy := &config.CIDeployConfig{
		Environments: map[string]*config.CIDeployEnvironment{
			"prod": {Provider: "aws-ecs", RequireApproval: true},
		},
	}
	if err := runDeployPhase(deploy, "prod", false); err != nil {
		t.Fatalf("approval skip should not error: %v", err)
	}
}
```

**Step 4:** Run `go test ./cmd/wfctl/ -run TestRun -v`

**Step 5:** Commit: `feat: wfctl ci run — build, test, deploy phases from workflow config`

---

### Task 3: wfctl ci init — generate bootstrap YAML

**Files:**
- Create: `cmd/wfctl/ci_init.go`
- Create: `cmd/wfctl/ci_init_test.go`
- Modify: `cmd/wfctl/ci.go` — add "init" subcommand

**Step 1: Add "init" case to runCI dispatch**

**Step 2: Create ci_init.go**

Generates the thin bootstrap YAML for GitHub Actions or GitLab CI:

```go
func runCIInit(args []string) error {
	fs := flag.NewFlagSet("ci init", flag.ContinueOnError)
	platform := fs.String("platform", "github-actions", "CI platform: github-actions, gitlab-ci")
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	// ...
	// Read config, detect environments from ci.deploy.environments
	// Generate minimal bootstrap YAML that calls wfctl ci run
}
```

Template for GitHub Actions:
```yaml
# Generated by wfctl ci init — customize as needed
name: CI/CD
on:
  push:
    branches: [main]
  pull_request:
jobs:
  build-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: GoCodeAlone/setup-wfctl@v1
      - run: wfctl ci run --phase build,test
  # One deploy job per environment in ci.deploy.environments
```

**Step 3:** Write test that generates bootstrap YAML and validates structure

**Step 4:** Run tests, commit: `feat: wfctl ci init — generate CI bootstrap YAML`

---

### Task 4: wfctl secrets — detect, set, list, validate commands

**Files:**
- Create: `cmd/wfctl/secrets.go`
- Create: `cmd/wfctl/secrets_test.go`
- Create: `cmd/wfctl/secrets_detect.go`
- Create: `cmd/wfctl/secrets_providers.go`
- Modify: `cmd/wfctl/main.go` — add "secrets" command

**Step 1: Register "secrets" in main.go commands map**

Add: `"secrets": runSecrets,`

**Step 2: Create secrets.go — command dispatcher**

```go
func runSecrets(args []string) error {
	if len(args) < 1 { return secretsUsage() }
	switch args[0] {
	case "detect":  return runSecretsDetect(args[1:])
	case "set":     return runSecretsSet(args[1:])
	case "list":    return runSecretsList(args[1:])
	case "validate": return runSecretsValidate(args[1:])
	default:        return secretsUsage()
	}
}
```

**Step 3: Create secrets_detect.go — scan config for secret-like values**

```go
func runSecretsDetect(args []string) error {
	// Parse config
	// Scan modules for fields that look like secrets:
	//   - field names: dsn, apiKey, api_key, token, secret, password, signingKey
	//   - values with ${...} env var references
	//   - module types: auth.jwt (signingKey), auth.oauth2 (clientSecret), etc.
	// Print detected secrets with recommendations
}
```

**Step 4: Create secrets_providers.go — provider interface + env provider**

```go
// SecretsProvider is the interface for secret storage backends.
type SecretsProvider interface {
	Get(ctx context.Context, name string) (string, error)
	Set(ctx context.Context, name, value string) error
	List(ctx context.Context) ([]SecretStatus, error)
	Delete(ctx context.Context, name string) error
}

type SecretStatus struct {
	Name        string
	IsSet       bool
	LastRotated time.Time
}

// envProvider reads/writes secrets as environment variables.
type envProvider struct{}

func (p *envProvider) Get(_ context.Context, name string) (string, error) {
	return os.Getenv(name), nil
}
// ... etc
```

**Step 5:** Write tests for detect (mock config with known patterns), provider interface

**Step 6:** Run tests, commit: `feat: wfctl secrets — detect, set, list, validate commands`

---

### Task 5: GitHub plugin expansion — go-github SDK + new steps

**Files:**
- Modify: `/Users/jon/workspace/workflow-plugin-github/go.mod` — add go-github
- Create: `internal/github_sdk_client.go` — wraps go-github/v69
- Create: `internal/step_pr_create.go`
- Create: `internal/step_pr_merge.go`
- Create: `internal/step_pr_comment.go`
- Create: `internal/step_issue_create.go`
- Create: `internal/step_issue_close.go`
- Create: `internal/step_release_create.go`
- Create: `internal/step_release_upload.go`
- Create: `internal/step_repo_dispatch.go`
- Create: `internal/step_deployment_create.go`
- Create: `internal/step_graphql.go`
- Create: `internal/module_github_app.go`
- Modify: `internal/plugin.go` — register all new steps + modules

**Step 1: Add go-github dependency**

```bash
cd /Users/jon/workspace/workflow-plugin-github
go get github.com/google/go-github/v69@latest
```

**Step 2: Create github_sdk_client.go**

Thin wrapper around `github.NewClient()` that supports both PAT and GitHub App auth:

```go
package internal

import (
	"context"
	"net/http"

	"github.com/google/go-github/v69/github"
)

// SDKClient wraps the go-github client with auth support.
type SDKClient struct {
	client *github.Client
}

// NewSDKClient creates a client from a personal access token.
func NewSDKClient(token string) *SDKClient {
	client := github.NewClient(nil).WithAuthToken(token)
	return &SDKClient{client: client}
}

// NewSDKClientFromTransport creates a client from an http.RoundTripper (for GitHub App auth).
func NewSDKClientFromTransport(rt http.RoundTripper) *SDKClient {
	httpClient := &http.Client{Transport: rt}
	client := github.NewClient(httpClient)
	return &SDKClient{client: client}
}
```

**Step 3: Create step_pr_create.go**

```go
package internal

import (
	"context"
	"fmt"
	"os"

	"github.com/google/go-github/v69/github"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

type prCreateStep struct {
	name   string
	config prCreateConfig
}

type prCreateConfig struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Token string `json:"token"`
}

func (s *prCreateStep) Execute(ctx context.Context, stepName string, cfg map[string]any, inputs map[string]any, outputs map[string]any, metadata map[string]any) (*sdk.StepResult, error) {
	// Parse config, create PR via SDK
	token := os.ExpandEnv(s.config.Token)
	client := NewSDKClient(token)

	pr, _, err := client.client.PullRequests.Create(ctx, s.config.Owner, s.config.Repo, &github.NewPullRequest{
		Title: github.Ptr(s.config.Title),
		Body:  github.Ptr(s.config.Body),
		Head:  github.Ptr(s.config.Head),
		Base:  github.Ptr(s.config.Base),
	})
	if err != nil {
		return nil, fmt.Errorf("create PR: %w", err)
	}

	return &sdk.StepResult{
		Data: map[string]any{
			"number": pr.GetNumber(),
			"url":    pr.GetHTMLURL(),
			"id":     pr.GetID(),
		},
	}, nil
}
```

**Step 4: Create remaining steps** following the same pattern — each is a thin wrapper around one go-github API call.

**Step 5: Register all steps in plugin.go**

Add to `StepFactories()`:
```go
"step.gh_pr_create":        newPRCreateFactory(),
"step.gh_pr_merge":         newPRMergeFactory(),
"step.gh_pr_comment":       newPRCommentFactory(),
"step.gh_issue_create":     newIssueCreateFactory(),
"step.gh_issue_close":      newIssueCloseFactory(),
"step.gh_release_create":   newReleaseCreateFactory(),
"step.gh_release_upload":   newReleaseUploadFactory(),
"step.gh_repo_dispatch":    newRepoDispatchFactory(),
"step.gh_deployment_create": newDeploymentCreateFactory(),
"step.gh_graphql":          newGraphQLFactory(),
```

**Step 6: Create module_github_app.go** — GitHub App module that manages installation tokens

**Step 7:** Run `go build ./... && go test ./... -race`

**Step 8:** Update plugin.json capabilities

**Step 9:** Commit: `feat: expand GitHub plugin — go-github SDK, 10 new steps, github.app module`

---

### Task 6: MCP scaffold tools + setup guide resource

**Files:**
- Create: `mcp/scaffold_tools.go`
- Create: `mcp/scaffold_tools_test.go`
- Create: `mcp/setup_guide.go`
- Modify: `mcp/server.go` — register new tools and resources

**Step 1: Create scaffold_tools.go**

Implement 6 MCP tools:
- `scaffold_ci` — generate ci: section from app description
- `scaffold_infra` — generate infra: section from detected module needs
- `scaffold_environment` — generate environment config
- `detect_secrets` — scan config and return secret candidates
- `detect_ports` — scan config and return port list
- `generate_bootstrap` — generate CI platform bootstrap YAML

Each tool takes YAML content as input and returns generated YAML:

```go
func (s *Server) handleScaffoldCI(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Parse the description parameter
	// Generate a ci: section with sensible defaults
	// Return YAML string
}
```

**Step 2: Create setup_guide.go**

MCP resource at `workflow://guides/setup` containing the decision trees for AI assistants:

```go
func (s *Server) handleSetupGuide(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	guide := `# Workflow Setup Guide

## For AI Assistants

This guide helps you configure a workflow application step by step.
Follow the decision trees below based on the user's needs.

### Infrastructure Setup Flow
1. Ask: "What cloud provider?" → AWS | GCP | DigitalOcean | Local
2. Ask: "Provision new or connect existing?"
...
`
	return []mcp.ResourceContents{
		mcp.NewTextResourceContents(req.Params.URI, guide),
	}, nil
}
```

**Step 3: Register in server.go**

In `registerTools()` or a new `registerScaffoldTools()`:
```go
s.mcpServer.AddTool(mcp.NewTool("scaffold_ci", ...), s.handleScaffoldCI)
// ... etc
```

In `registerResources()`:
```go
s.mcpServer.AddResource(mcp.NewResource("workflow://guides/setup", ...), s.handleSetupGuide)
```

**Step 4:** Write tests for scaffold tools

**Step 5:** Run tests, commit: `feat: MCP scaffold tools + setup guide resource`

---

### Task 7: Schema validation for new sections + wfctl validate integration

**Files:**
- Modify: `schema/schema.go` — extend schema generation to include ci:, environments:, secrets: sections
- Modify: `cmd/wfctl/validate.go` — validate the new sections
- Create: `config/ci_config_validate.go` — validation logic

**Step 1: Add validation for ci: section**

```go
func (c *CIConfig) Validate() error {
	var errs []error
	if c.Build != nil {
		for _, bin := range c.Build.Binaries {
			if bin.Name == "" { errs = append(errs, fmt.Errorf("ci.build.binaries: name is required")) }
			if bin.Path == "" { errs = append(errs, fmt.Errorf("ci.build.binaries[%s]: path is required", bin.Name)) }
		}
	}
	if c.Deploy != nil {
		for name, env := range c.Deploy.Environments {
			if env.Provider == "" { errs = append(errs, fmt.Errorf("ci.deploy.environments[%s]: provider is required", name)) }
		}
	}
	return errors.Join(errs...)
}
```

**Step 2: Wire into wfctl validate**

**Step 3:** Write validation tests

**Step 4:** Run tests, commit: `feat: validate ci:, environments:, secrets: config sections`

---

### Task 8: Documentation updates

**Files:**
- Modify: `docs/dsl-reference.md` — add ci:, environments:, secrets: sections
- Modify: `cmd/wfctl/dsl-reference-embedded.md` — same
- Modify: `docs/WFCTL.md` — add ci run, ci init, secrets commands
- Modify: `CHANGELOG.md` — add entries

**Step 1:** Add ci:, environments:, secrets: sections to DSL reference with examples

**Step 2:** Add wfctl ci run, ci init, secrets detect/set/list/validate to WFCTL.md

**Step 3:** Commit: `docs: ci/environments/secrets YAML sections, wfctl ci + secrets commands`

---

---

## Alignment Fixes (from design-to-plan check)

### Task 1 additions:
- Add `Strategy string` field to `SecretsRotationConfig` (e.g., "dual-credential", "graceful")

### Task 2 additions:
- `runTestPhase` must handle `Needs` field: spin up ephemeral deps (postgres, redis) as Docker containers before running test command, tear down after
- After test phase, if `GITHUB_TOKEN` env var is set, create a GitHub Check Run with test results summary

### Task 4 additions:
- Add `wfctl secrets init --provider <name> --env <env>` — initialize secrets provider config
- Add `wfctl secrets rotate <name> --env <env>` — trigger secret rotation
- Add `wfctl secrets sync --from <env> --to <env>` — copy secret structure between environments
- Add `--from-file` flag to `wfctl secrets set` for certificates/keys

### Task 5 additions:
- Add `step.gh_pr_review` — request/submit PR review
- Add `step.gh_issue_label` — add/remove labels on issues
- Add `step.gh_secret_set` — set repository/org secret (encrypted)
- Add enhanced `github.webhook` module — signature validation, event type routing (update existing module_webhook.go)

### Task 6 additions:
- Add `mcp.detect_infra_needs` tool (analyze modules → suggest infrastructure)
- Change resource URI from `workflow://guides/setup` to `workflow://docs/setup-guide` (matches design)

---

## Summary

| Task | Scope | Repo |
|------|-------|------|
| 1 | Config structs (ci, environments, secrets) + Strategy field | workflow |
| 2 | wfctl ci run (build + test + ephemeral deps + Check Run) | workflow |
| 3 | wfctl ci init (bootstrap YAML generation) | workflow |
| 4 | wfctl secrets (detect, set, list, validate, init, rotate, sync) | workflow |
| 5 | GitHub plugin expansion (go-github SDK, 13 steps, github.app + webhook) | workflow-plugin-github |
| 6 | MCP scaffold tools (7 tools) + setup guide resource | workflow |
| 7 | Schema validation for new sections | workflow |
| 8 | Documentation | workflow |
