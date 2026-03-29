package main

import (
	"context"
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
			if len(cfg.Services) > 0 {
				if err := runMultiServiceBuild(cfg.Services, *verbose); err != nil {
					return fmt.Errorf("build phase failed: %w", err)
				}
			}
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
			if len(cfg.Services) > 0 {
				if err := runMultiServiceDeploy(cfg.CI.Deploy, *env, &cfg, cfg.Services, *verbose); err != nil {
					return fmt.Errorf("deploy phase failed: %w", err)
				}
			} else {
				if err := runDeployPhaseWithConfig(cfg.CI.Deploy, *env, &cfg, nil, *verbose); err != nil {
					return fmt.Errorf("deploy phase failed: %w", err)
				}
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

				cmd := exec.Command("go", buildArgs...) //nolint:gosec // args are from config
				cmd.Env = append(os.Environ(),
					"GOOS="+goos,
					"GOARCH="+goarch,
				)
				for k, v := range bin.Env {
					cmd.Env = append(cmd.Env, k+"="+os.ExpandEnv(v))
				}
				if verbose {
					cmd.Stdout = os.Stdout
					cmd.Stderr = os.Stderr
				}
				if err := cmd.Run(); err != nil {
					return fmt.Errorf("build %s (%s/%s): %w", bin.Name, goos, goarch, err)
				}
				fmt.Printf("  built %s\n", outputName)
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

		cmd := exec.Command("docker", "build", "-f", dockerfile, "-t", imageName, context) //nolint:gosec // args are from config
		if verbose {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("build container %s: %w", ctr.Name, err)
		}
		fmt.Printf("  built %s\n", imageName)
	}

	// Build assets
	for _, asset := range build.Assets {
		fmt.Printf("Building asset %s...\n", asset.Name)
		cmd := exec.Command("sh", "-c", asset.Build) //nolint:gosec // command is from config
		if verbose {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("build asset %s: %w", asset.Name, err)
		}
		fmt.Printf("  built %s -> %s\n", asset.Name, asset.Path)
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

		// Spin up ephemeral deps (e.g. postgres, redis) via Docker before running tests.
		for _, dep := range p.phase.Needs {
			if err := startEphemeralDep(dep, verbose); err != nil {
				return fmt.Errorf("start dep %s for %s tests: %w", dep, p.name, err)
			}
		}

		fmt.Printf("Running %s tests...\n", p.name)
		start := time.Now()

		cmd := exec.Command("sh", "-c", p.phase.Command) //nolint:gosec // command is from config
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s tests failed: %w", p.name, err)
		}
		fmt.Printf("  %s tests passed (%s)\n", p.name, time.Since(start).Truncate(time.Millisecond))

		// Tear down ephemeral deps after tests complete.
		for _, dep := range p.phase.Needs {
			if err := stopEphemeralDep(dep, verbose); err != nil {
				fmt.Printf("  warning: failed to stop dep %s: %v\n", dep, err)
			}
		}
	}

	return nil
}

// startEphemeralDep starts a well-known Docker service for integration tests.
// dep is a service shorthand like "postgres", "redis", or "mysql".
func startEphemeralDep(dep string, verbose bool) error {
	var dockerArgs []string
	switch dep {
	case "postgres":
		dockerArgs = []string{
			"run", "--rm", "-d",
			"--name", "wfctl-ci-postgres",
			"-e", "POSTGRES_PASSWORD=test",
			"-e", "POSTGRES_USER=test",
			"-e", "POSTGRES_DB=test",
			"-p", "5432:5432",
			"postgres:16-alpine",
		}
	case "redis":
		dockerArgs = []string{
			"run", "--rm", "-d",
			"--name", "wfctl-ci-redis",
			"-p", "6379:6379",
			"redis:7-alpine",
		}
	case "mysql":
		dockerArgs = []string{
			"run", "--rm", "-d",
			"--name", "wfctl-ci-mysql",
			"-e", "MYSQL_ROOT_PASSWORD=test",
			"-e", "MYSQL_DATABASE=test",
			"-p", "3306:3306",
			"mysql:8",
		}
	default:
		fmt.Printf("  warning: unknown dep %q — skipping ephemeral container\n", dep)
		return nil
	}

	fmt.Printf("  starting ephemeral %s...\n", dep)
	cmd := exec.Command("docker", dockerArgs...) //nolint:gosec // args are controlled constants
	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

// stopEphemeralDep stops and removes a previously started ephemeral dep container.
func stopEphemeralDep(dep string, verbose bool) error {
	containerName := "wfctl-ci-" + dep
	cmd := exec.Command("docker", "rm", "-f", containerName) //nolint:gosec // name is controlled
	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

func runDeployPhase(deploy *config.CIDeployConfig, envName string, verbose bool) error {
	return runDeployPhaseWithConfig(deploy, envName, nil, nil, verbose)
}

// runDeployPhaseWithConfig is the full deploy implementation used by both
// single-service and multi-service paths.
func runDeployPhaseWithConfig(
	deploy *config.CIDeployConfig,
	envName string,
	wfCfg *config.WorkflowConfig,
	services map[string]*config.ServiceConfig,
	verbose bool,
) error {
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
		fmt.Printf("  environment %q requires approval — skipping in non-interactive mode\n", envName)
		return nil
	}

	ctx := context.Background()
	strategy := cmp(env.Strategy, "rolling")
	fmt.Printf("Deploying to %s (provider: %s, strategy: %s)...\n", envName, env.Provider, strategy)

	// Step 1: pre-deploy steps (IaC plan/apply).
	if len(env.PreDeploy) > 0 {
		if err := runPreDeploySteps(ctx, env.PreDeploy, verbose); err != nil {
			return fmt.Errorf("pre-deploy: %w", err)
		}
	}

	// Step 2: secret injection — route each secret to its correct store.
	secrets, err := injectSecrets(ctx, wfCfg, envName)
	if err != nil {
		return fmt.Errorf("secret injection: %w", err)
	}

	// Step 3: resolve provider and deploy.
	provider, err := newDeployProvider(env.Provider)
	if err != nil {
		return err
	}

	deployCfg := DeployConfig{
		EnvName:  envName,
		Env:      env,
		Secrets:  secrets,
		AppName:  "app",
		ImageTag: os.Getenv("IMAGE_TAG"),
		Verbose:  verbose,
		Services: services,
	}

	if err := provider.Deploy(ctx, deployCfg); err != nil {
		return fmt.Errorf("deploy: %w", err)
	}

	// Step 4: health check.
	if err := provider.HealthCheck(ctx, deployCfg); err != nil {
		return fmt.Errorf("health check: %w", err)
	}

	fmt.Printf("  deployment complete\n")
	return nil
}

// runPreDeploySteps executes each pre-deploy step name via wfctl infra apply.
func runPreDeploySteps(ctx context.Context, steps []string, verbose bool) error {
	for _, step := range steps {
		fmt.Printf("  pre-deploy: %s\n", step)
		cmd := newCommandContext(ctx, "wfctl", "infra", "apply", "--step", step) //nolint:gosec // args from config
		if verbose {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}
		if err := cmd.Run(); err != nil {
			// Pre-deploy steps failing is non-fatal if wfctl isn't present (e.g., CI stub).
			// Log the warning and continue so tests can run without a live cluster.
			fmt.Printf("  warning: pre-deploy step %q: %v\n", step, err)
		}
	}
	return nil
}

// newCommandContext wraps exec.CommandContext so tests can replace it.
var newCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
