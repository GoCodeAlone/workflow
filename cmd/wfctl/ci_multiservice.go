package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"

	"github.com/GoCodeAlone/workflow/config"
)

// runMultiServiceBuild builds each service's binary in parallel.
// Services without a Binary field are skipped.
func runMultiServiceBuild(services map[string]*config.ServiceConfig, verbose bool) error {
	if len(services) == 0 {
		return nil
	}

	var mu sync.Mutex
	var buildErrs []error

	type job struct {
		name string
		svc  *config.ServiceConfig
	}

	jobs := make([]job, 0, len(services))
	for name, svc := range services {
		if svc.Binary == "" {
			continue
		}
		jobs = append(jobs, job{name: name, svc: svc})
	}

	if len(jobs) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(name string, svc *config.ServiceConfig) {
			defer wg.Done()
			if err := buildServiceBinary(name, svc, verbose); err != nil {
				mu.Lock()
				buildErrs = append(buildErrs, fmt.Errorf("service %s: %w", name, err))
				mu.Unlock()
			}
		}(j.name, j.svc)
	}
	wg.Wait()

	if len(buildErrs) > 0 {
		for _, e := range buildErrs {
			fmt.Fprintf(os.Stderr, "  error: %v\n", e)
		}
		return fmt.Errorf("%d service build(s) failed", len(buildErrs))
	}
	return nil
}

// buildServiceBinary compiles a single service binary.
// It mirrors the logic in runBuildPhase for individual binary targets.
func buildServiceBinary(name string, svc *config.ServiceConfig, verbose bool) error {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	// Allow cross-compile overrides via env vars (same convention as single-service build).
	if v := os.Getenv("GOOS"); v != "" {
		goos = v
	}
	if v := os.Getenv("GOARCH"); v != "" {
		goarch = v
	}

	outputName := fmt.Sprintf("bin/%s-%s-%s", name, goos, goarch)
	fmt.Printf("Building service %s (%s) for %s/%s...\n", name, svc.Binary, goos, goarch)

	buildArgs := []string{"build", "-o", outputName, svc.Binary}
	cmd := exec.Command("go", buildArgs...) //nolint:gosec // args from config
	cmd.Env = append(os.Environ(),
		"GOOS="+goos,
		"GOARCH="+goarch,
		"CGO_ENABLED=0",
	)
	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build %s (%s/%s): %w", svc.Binary, goos, goarch, err)
	}

	fmt.Printf("  built %s\n", outputName)
	return nil
}

// runMultiServiceDeploy deploys each service to the configured environment.
// Each service gets its own k8s Deployment/Service with per-service scaling and
// port configuration. The deploy provider is shared for all services.
func runMultiServiceDeploy(
	deploy *config.CIDeployConfig,
	envName string,
	secretsCfg *config.SecretsConfig,
	services map[string]*config.ServiceConfig,
	verbose bool,
) error {
	return runDeployPhaseWithConfig(deploy, envName, secretsCfg, services, verbose)
}
