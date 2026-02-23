package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func runDeploy(args []string) error {
	if len(args) < 1 {
		return deployUsage()
	}
	switch args[0] {
	case "docker":
		return runDeployDocker(args[1:])
	case "kubernetes", "k8s":
		return runDeployKubernetes(args[1:])
	case "cloud":
		return runDeployCloud(args[1:])
	default:
		return deployUsage()
	}
}

func deployUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl deploy <target> [options]

Deploy a workflow application to a target environment.

Targets:
  docker      Build Docker image and run locally via docker compose
  kubernetes  Deploy to a Kubernetes cluster using Helm
  cloud       Deploy to a cloud environment (requires .wfctl.yaml or deploy.yaml)

Examples:
  wfctl deploy docker -config workflow.yaml
  wfctl deploy kubernetes -namespace prod -values custom.yaml
  wfctl deploy cloud -target staging
`)
	return fmt.Errorf("deploy target is required (docker, kubernetes, cloud)")
}

// runDeployDocker builds a Docker image and runs the app via docker compose.
func runDeployDocker(args []string) error {
	fs := flag.NewFlagSet("deploy docker", flag.ContinueOnError)
	config := fs.String("config", "workflow.yaml", "Workflow config file to deploy")
	image := fs.String("image", "workflow-app:local", "Docker image name:tag to build")
	noCompose := fs.Bool("no-compose", false, "Build image only, skip docker compose up")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl deploy docker [options]

Build a Docker image and run the workflow application locally via docker compose.
Generates a Dockerfile and docker-compose.yml if not already present.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	// Generate Dockerfile if missing
	dockerfilePath := filepath.Join(cwd, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		fmt.Println("generating Dockerfile...")
		if err := writeDockerfile(dockerfilePath); err != nil {
			return fmt.Errorf("generate Dockerfile: %w", err)
		}
		fmt.Printf("  created  Dockerfile\n")
	} else {
		fmt.Println("using existing Dockerfile")
	}

	// Generate docker-compose.yml if missing
	composePath := filepath.Join(cwd, "docker-compose.yml")
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		fmt.Println("generating docker-compose.yml...")
		if err := writeDockerCompose(composePath, *config, *image); err != nil {
			return fmt.Errorf("generate docker-compose.yml: %w", err)
		}
		fmt.Printf("  created  docker-compose.yml\n")
	} else {
		fmt.Println("using existing docker-compose.yml")
	}

	// Build the Docker image
	fmt.Printf("building Docker image %s...\n", *image)
	buildCmd := exec.Command("docker", "build", "-t", *image, ".") //nolint:gosec // G204: image and cwd are validated inputs
	buildCmd.Dir = cwd
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}
	fmt.Printf("image %s built successfully\n", *image)

	if *noCompose {
		return nil
	}

	// Run via docker compose
	fmt.Println("starting services with docker compose...")
	upCmd := exec.Command("docker", "compose", "up", "-d") //nolint:gosec // G204: no user-controlled args
	upCmd.Dir = cwd
	upCmd.Stdout = os.Stdout
	upCmd.Stderr = os.Stderr
	if err := upCmd.Run(); err != nil {
		return fmt.Errorf("docker compose up failed: %w", err)
	}

	fmt.Println("\nservices started. run 'docker compose logs -f' to follow logs.")
	return nil
}

// runDeployKubernetes deploys to Kubernetes using Helm.
func runDeployKubernetes(args []string) error {
	fs := flag.NewFlagSet("deploy kubernetes", flag.ContinueOnError)
	namespace := fs.String("namespace", "default", "Kubernetes namespace")
	releaseName := fs.String("release", "workflow", "Helm release name")
	chartDir := fs.String("chart", "", "Path to Helm chart directory (default: deploy/helm/workflow or bundled chart)")
	valuesFile := fs.String("values", "", "Additional Helm values file (-f flag passed to helm)")
	setValues := fs.String("set", "", "Comma-separated key=value pairs to override (--set passed to helm)")
	dryRun := fs.Bool("dry-run", false, "Pass --dry-run to helm (simulate install)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl deploy kubernetes [options]

Deploy the workflow application to a Kubernetes cluster using Helm.
The cluster must be reachable via kubectl and helm must be installed.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Resolve chart directory
	chart := *chartDir
	if chart == "" {
		// Look for the chart relative to cwd
		candidates := []string{
			"deploy/helm/workflow",
			"../deploy/helm/workflow",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				chart = c
				break
			}
		}
		if chart == "" {
			return fmt.Errorf("no Helm chart found; pass -chart <path> or run from the workflow project root\n" +
				"expected chart at: deploy/helm/workflow/")
		}
	}

	// Verify helm is installed
	if _, err := exec.LookPath("helm"); err != nil {
		return fmt.Errorf("helm not found in PATH: install from https://helm.sh/docs/intro/install/")
	}

	helmArgs := []string{
		"upgrade", "--install",
		*releaseName,
		chart,
		"--namespace", *namespace,
		"--create-namespace",
	}

	if *valuesFile != "" {
		helmArgs = append(helmArgs, "-f", *valuesFile)
	}

	if *setValues != "" {
		for _, pair := range strings.Split(*setValues, ",") {
			pair = strings.TrimSpace(pair)
			if pair != "" {
				helmArgs = append(helmArgs, "--set", pair)
			}
		}
	}

	if *dryRun {
		helmArgs = append(helmArgs, "--dry-run")
	}

	fmt.Printf("running: helm %s\n", strings.Join(helmArgs, " "))

	cmd := exec.Command("helm", helmArgs...) //nolint:gosec // G204: helm args are constructed from validated inputs
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("helm upgrade --install failed: %w", err)
	}

	if !*dryRun {
		fmt.Printf("\nrelease %q deployed to namespace %q\n", *releaseName, *namespace)
		fmt.Printf("check status: kubectl -n %s get pods\n", *namespace)
	}
	return nil
}

// runDeployCloud is a stub for cloud deployment that prints usage guidance.
func runDeployCloud(args []string) error {
	fs := flag.NewFlagSet("deploy cloud", flag.ContinueOnError)
	target := fs.String("target", "", "Deployment target: staging or production")
	configFile := fs.String("config-file", "", "Cloud deploy config file (default: .wfctl.yaml or deploy.yaml)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl deploy cloud [options]

Deploy the workflow application to a cloud environment.
Reads cloud configuration from .wfctl.yaml or deploy.yaml in the project root.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Validate target if given
	if *target != "" && *target != "staging" && *target != "production" {
		return fmt.Errorf("invalid target %q: must be staging or production", *target)
	}

	// Resolve config file
	cfg := *configFile
	if cfg == "" {
		for _, candidate := range []string{".wfctl.yaml", "deploy.yaml"} {
			if _, err := os.Stat(candidate); err == nil {
				cfg = candidate
				break
			}
		}
	}

	targetLabel := *target
	if targetLabel == "" {
		targetLabel = "staging"
	}

	fmt.Fprintf(os.Stderr, `wfctl deploy cloud: cloud deployment is not yet fully implemented.

To deploy to a cloud environment:
  1. Create a .wfctl.yaml or deploy.yaml with your cloud provider settings.
  2. Build and push your Docker image:
       docker build -t <registry>/<image>:<tag> .
       docker push <registry>/<image>:<tag>
  3. Apply infrastructure (OpenTofu / Terraform):
       cd deploy/tofu/environments/%s
       tofu apply
  4. Update your Kubernetes deployment or ECS task definition with the new image.

Config file: %s
Target:      %s
`, targetLabel, ifEmpty(cfg, "(none found)"), targetLabel)

	return fmt.Errorf("cloud deployment requires manual configuration; see guidance above")
}

// writeDockerfile writes a minimal multi-stage Dockerfile suitable for workflow engine projects.
func writeDockerfile(path string) error {
	const content = `# Auto-generated by wfctl deploy docker
# Multi-stage build for a workflow engine application.

FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git ca-certificates
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o app ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 65532 nonroot
WORKDIR /app
COPY --from=builder /build/app .
USER nonroot
EXPOSE 8080 8081
ENTRYPOINT ["./app"]
`
	return os.WriteFile(path, []byte(content), 0640) //nolint:gosec // G306: generated project file
}

// writeDockerCompose writes a minimal docker-compose.yml for the workflow app.
func writeDockerCompose(path, configFile, image string) error {
	content := fmt.Sprintf(`# Auto-generated by wfctl deploy docker
services:
  app:
    image: %s
    build:
      context: .
      dockerfile: Dockerfile
    ports:
      - "8080:8080"
      - "8081:8081"
    environment:
      WORKFLOW_ADDR: ":8080"
    volumes:
      - ./%s:/etc/workflow/config.yaml:ro
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/healthz"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 30s
`, image, configFile)
	return os.WriteFile(path, []byte(content), 0640) //nolint:gosec // G306: generated project file
}

func ifEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
