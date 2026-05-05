package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

// devNamespace is the Kubernetes namespace used for dev deployments.
const devNamespace = "dev"

// runDevK8s deploys the application to a local minikube cluster.
func runDevK8s(cfg *config.WorkflowConfig, verbose bool) error {
	// 1. Verify minikube is running.
	if err := checkMinikube(); err != nil {
		return err
	}
	fmt.Println("[dev/k8s] minikube is running")

	// 2. Ensure the dev namespace exists.
	if err := ensureDevNamespace(verbose); err != nil {
		return err
	}

	// 3. Build and load images into minikube.
	if err := buildAndLoadImages(cfg, verbose); err != nil {
		return err
	}

	// 4. Generate k8s manifests.
	manifests, err := generateDevK8sManifests(cfg)
	if err != nil {
		return fmt.Errorf("generate k8s manifests: %w", err)
	}

	// 5. Write manifests to a temp file and apply.
	const manifestFile = "dev-manifests.yaml"
	if err := os.WriteFile(manifestFile, []byte(manifests), 0o600); err != nil {
		return fmt.Errorf("write manifests: %w", err)
	}
	defer os.Remove(manifestFile) //nolint:errcheck

	fmt.Printf("[dev/k8s] Applying manifests to namespace %q...\n", devNamespace)
	applyArgs := []string{"apply", "-f", manifestFile, "--namespace", devNamespace}
	applyCmd := exec.Command("kubectl", applyArgs...) //nolint:gosec
	applyCmd.Stdout = os.Stdout
	applyCmd.Stderr = os.Stderr
	if verbose {
		fmt.Printf("[dev/k8s] kubectl %s\n", strings.Join(applyArgs, " "))
	}
	if err := applyCmd.Run(); err != nil {
		return fmt.Errorf("kubectl apply: %w", err)
	}

	// 6. Port-forward exposed services.
	if err := portForwardDevServices(cfg, verbose); err != nil {
		fmt.Printf("[dev/k8s] Warning: port-forward setup failed: %v\n", err)
	}

	fmt.Printf("[dev/k8s] Deployment complete. Services are available in namespace %q.\n", devNamespace)
	fmt.Println("[dev/k8s] Run 'wfctl dev status --k8s' to check pod health.")
	return nil
}

// checkMinikube verifies that minikube is installed and running.
func checkMinikube() error {
	if _, err := exec.LookPath("minikube"); err != nil {
		return fmt.Errorf("minikube not found in PATH; install from https://minikube.sigs.k8s.io/")
	}
	out, err := exec.Command("minikube", "status", "--output=text").Output() //nolint:gosec
	if err != nil {
		return fmt.Errorf("minikube is not running; start with: minikube start")
	}
	if !strings.Contains(string(out), "Running") {
		return fmt.Errorf("minikube is not in Running state; start with: minikube start")
	}
	return nil
}

// ensureDevNamespace creates the dev namespace if it does not already exist.
func ensureDevNamespace(verbose bool) error {
	// Check if namespace exists.
	checkCmd := exec.Command("kubectl", "get", "namespace", devNamespace) //nolint:gosec
	if err := checkCmd.Run(); err == nil {
		if verbose {
			fmt.Printf("[dev/k8s] Namespace %q already exists\n", devNamespace)
		}
		return nil
	}
	fmt.Printf("[dev/k8s] Creating namespace %q...\n", devNamespace)
	createCmd := exec.Command("kubectl", "create", "namespace", devNamespace) //nolint:gosec
	createCmd.Stdout = os.Stdout
	createCmd.Stderr = os.Stderr
	return createCmd.Run()
}

// buildAndLoadImages builds Docker images for each service and loads them into minikube.
func buildAndLoadImages(cfg *config.WorkflowConfig, verbose bool) error {
	if !fileExists("Dockerfile") {
		if verbose {
			fmt.Println("[dev/k8s] No Dockerfile found; skipping image build")
		}
		return nil
	}

	// Determine image names.
	images := collectDevImageNames(cfg)
	for _, img := range images {
		fmt.Printf("[dev/k8s] Building image %s...\n", img)
		buildCmd := exec.Command("docker", "build", "-t", img, ".") //nolint:gosec
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
		if err := buildCmd.Run(); err != nil {
			return fmt.Errorf("docker build %s: %w", img, err)
		}

		fmt.Printf("[dev/k8s] Loading %s into minikube...\n", img)
		loadCmd := exec.Command("minikube", "image", "load", img) //nolint:gosec
		loadCmd.Stdout = os.Stdout
		loadCmd.Stderr = os.Stderr
		if err := loadCmd.Run(); err != nil {
			return fmt.Errorf("minikube image load %s: %w", img, err)
		}
	}
	return nil
}

// collectDevImageNames returns the Docker image names to build for the config.
func collectDevImageNames(cfg *config.WorkflowConfig) []string {
	if len(cfg.Services) == 0 {
		return []string{"app:dev"}
	}
	names := make([]string, 0, len(cfg.Services))
	for name := range cfg.Services {
		names = append(names, name+":dev")
	}
	return names
}

// generateDevK8sManifests produces Kubernetes Deployment + Service YAML for
// each service in the config, targeting the dev namespace.
func generateDevK8sManifests(cfg *config.WorkflowConfig) (string, error) {
	deployCfg := DeployConfig{
		AppName: "app",
		EnvName: "dev",
		Env: &config.CIDeployEnvironment{
			Namespace: devNamespace,
			Strategy:  "rolling",
		},
		Services: cfg.Services,
	}
	if len(cfg.Services) == 0 {
		deployCfg.ImageTag = "app:dev"
	}
	return generateK8sManifests(deployCfg)
}

// portForwardDevServices sets up kubectl port-forward for each exposed service.
// The port-forward processes are started in the background.
func portForwardDevServices(cfg *config.WorkflowConfig, verbose bool) error {
	type fwdEntry struct {
		service string
		port    int
	}
	var entries []fwdEntry

	if len(cfg.Services) > 0 {
		for svcName, svc := range cfg.Services {
			if svc == nil {
				continue
			}
			for _, exp := range svc.Expose {
				entries = append(entries, fwdEntry{service: svcName, port: exp.Port})
			}
		}
	} else {
		for _, mod := range cfg.Modules {
			if mod.Type == "http.server" || mod.Type == "http.router" {
				port, _ := extractModulePort(mod)
				if port > 0 {
					entries = append(entries, fwdEntry{service: "app", port: port})
				}
			}
		}
	}

	for _, e := range entries {
		portStr := fmt.Sprintf("%d:%d", e.port, e.port)
		args := []string{
			"port-forward",
			"--namespace", devNamespace,
			"service/" + e.service,
			portStr,
		}
		cmd := exec.Command("kubectl", args...) //nolint:gosec
		if verbose {
			fmt.Printf("[dev/k8s] kubectl %s\n", strings.Join(args, " "))
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		// Start in background; ignore errors (service may not be ready yet).
		if err := cmd.Start(); err != nil {
			fmt.Printf("[dev/k8s] Warning: port-forward %s:%d failed to start: %v\n", e.service, e.port, err)
			continue
		}
		fmt.Printf("[dev/k8s] Port-forwarding %s → localhost:%d\n", e.service, e.port)
	}
	return nil
}

// devK8sDown deletes the dev namespace and all its resources.
func devK8sDown(verbose bool) error {
	fmt.Printf("[dev/k8s] Deleting namespace %q...\n", devNamespace)
	args := []string{"delete", "namespace", devNamespace, "--ignore-not-found"}
	cmd := exec.Command("kubectl", args...) //nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if verbose {
		fmt.Printf("[dev/k8s] kubectl %s\n", strings.Join(args, " "))
	}
	return cmd.Run()
}

// devK8sLogs streams logs from pods in the dev namespace.
func devK8sLogs(service string, follow bool) error {
	args := []string{"logs", "--namespace", devNamespace}
	if follow {
		args = append(args, "-f")
	}
	if service != "" {
		args = append(args, "deployment/"+service)
	} else {
		args = append(args, "--all-containers=true", "--selector=app")
	}
	cmd := exec.Command("kubectl", args...) //nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// devK8sStatus shows pod status in the dev namespace.
func devK8sStatus() error {
	args := []string{"get", "pods", "--namespace", devNamespace, "-o", "wide"}
	cmd := exec.Command("kubectl", args...) //nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// devK8sRestart rolls out a restart for a deployment in the dev namespace.
func devK8sRestart(service string) error {
	target := "deployment"
	if service != "" {
		target = "deployment/" + service
	}
	args := []string{"rollout", "restart", target, "--namespace", devNamespace}
	cmd := exec.Command("kubectl", args...) //nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
