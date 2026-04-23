package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/deploy"
	"github.com/GoCodeAlone/workflow/deploy/sidecars"
	"github.com/GoCodeAlone/workflow/manifest"
	k8s "github.com/GoCodeAlone/workflow/pkg/k8s"
	"github.com/GoCodeAlone/workflow/pkg/k8s/imageloader"
)

func runDeploy(args []string) error {
	if len(args) < 1 {
		return deployUsage()
	}
	switch args[0] {
	case "docker":
		return runDeployDocker(args[1:])
	case "kubernetes", "k8s":
		return runDeployK8s(args[1:])
	case "helm":
		return runDeployHelm(args[1:])
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
  docker          Build Docker image and run locally via docker compose
  kubernetes|k8s  Deploy to Kubernetes via client-go (server-side apply)
  helm            Deploy to Kubernetes using Helm charts
  cloud           Deploy to a cloud environment (requires .wfctl.yaml or deploy.yaml)

Kubernetes subcommands:
  generate  Produce K8s manifests for review or version control
  apply     Build (optional), load, and apply manifests to a cluster
  destroy   Delete all resources for an app
  status    Show deployment status and pod health
  logs      Stream logs from the deployed app
  diff      Compare generated manifests against live cluster state

Build flags (used with --build on apply):
  --build            Build Docker image and load into cluster before deploying
  --dockerfile PATH  Path to Dockerfile (default: Dockerfile)
  --build-context DIR  Docker build context directory (default: .)
  --build-arg ARGS   Docker build args (comma-separated KEY=VALUE pairs)
  --runtime NAME     Override auto-detected runtime (minikube|kind|docker-desktop|k3d|remote)
  --registry URL     Registry for remote clusters (e.g. ghcr.io/org)

Runtime auto-detection (from kubeconfig context name):
  minikube / minikube-*  →  minikube image load     (imagePullPolicy: Never)
  kind-*                 →  kind load docker-image   (imagePullPolicy: Never)
  docker-desktop         →  shared daemon (no load)  (imagePullPolicy: IfNotPresent)
  k3d-*                  →  k3d image import         (imagePullPolicy: Never)
  anything else          →  docker push --registry   (imagePullPolicy: IfNotPresent)

Recommended workflow (local development):
  # One command: build, load into cluster, apply manifests, wait for healthy
  wfctl deploy k8s apply --build -config app.yaml --force --wait

  # With explicit image tag
  wfctl deploy k8s apply --build -config app.yaml -image myapp:v2 --force --wait

  # Preview manifests without applying
  wfctl deploy k8s generate -config app.yaml -image myapp:v1

  # Check what's running
  wfctl deploy k8s status -app myapp

  # Stream logs
  wfctl deploy k8s logs -app myapp --follow

Recommended workflow (remote cluster):
  # Build, push to registry, deploy
  wfctl deploy k8s apply --build -config app.yaml --registry ghcr.io/org --wait

  # Compare local config against live cluster
  wfctl deploy k8s diff -config app.yaml -image ghcr.io/org/myapp:v1

Persisting defaults (.wfctl.yaml):
  deploy:
    target: kubernetes
    namespace: prod
    build:
      dockerfile: Dockerfile
      runtime: minikube
      registry: ghcr.io/myorg

Other examples:
  wfctl deploy docker -config workflow.yaml
  wfctl deploy helm -namespace prod -values custom.yaml
  wfctl deploy cloud -target staging
`)
	return fmt.Errorf("deploy target is required")
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
	if *config == "" && fs.NArg() > 0 {
		*config = fs.Arg(0)
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
	if err := buildDockerImage(*image, filepath.Join(cwd, "Dockerfile"), cwd, nil); err != nil {
		return err
	}

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

// runDeployHelm deploys to Kubernetes using Helm.
func runDeployHelm(args []string) error {
	fs := flag.NewFlagSet("deploy helm", flag.ContinueOnError)
	namespace := fs.String("namespace", "default", "Kubernetes namespace")
	releaseName := fs.String("release", "workflow", "Helm release name")
	chartDir := fs.String("chart", "", "Path to Helm chart directory (default: deploy/helm/workflow or bundled chart)")
	valuesFile := fs.String("values", "", "Additional Helm values file (-f flag passed to helm)")
	setValues := fs.String("set", "", "Comma-separated key=value pairs to override (--set passed to helm)")
	dryRun := fs.Bool("dry-run", false, "Pass --dry-run to helm (simulate install)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl deploy helm [options]

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

// buildDockerImage builds a Docker image using the local Docker daemon.
// buildArgs is a slice of KEY=VALUE strings passed as --build-arg flags.
func buildDockerImage(image, dockerfile, buildCtx string, buildArgs []string) error {
	fmt.Printf("building image %s...\n", image)
	args := []string{"build", "-t", image, "-f", dockerfile}
	for _, ba := range buildArgs {
		args = append(args, "--build-arg", ba)
	}
	args = append(args, buildCtx)
	cmd := exec.Command("docker", args...) //nolint:gosec // G204: validated build inputs
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}
	fmt.Printf("image %s built\n", image)
	return nil
}

// resolveImageTag appends a git short hash tag if the image has no tag.
func resolveImageTag(image string) string {
	if strings.Contains(image, ":") {
		return image
	}
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return image + ":latest"
	}
	return image + ":" + strings.TrimSpace(string(out))
}

// runDeployK8s dispatches kubernetes subcommands.
func runDeployK8s(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("kubernetes subcommand required: generate, apply, destroy, status, logs, diff")
	}
	switch args[0] {
	case "generate":
		return runK8sGenerate(args[1:])
	case "apply":
		return runK8sApply(args[1:])
	case "destroy":
		return runK8sDestroy(args[1:])
	case "status":
		return runK8sStatus(args[1:])
	case "logs":
		return runK8sLogs(args[1:])
	case "diff":
		return runK8sDiff(args[1:])
	default:
		return fmt.Errorf("unknown kubernetes subcommand %q (try: generate, apply, destroy, status, logs, diff)", args[0])
	}
}

// k8sCommonFlags bundles the flags shared across k8s subcommands.
type k8sCommonFlags struct {
	configFile      string
	image           string
	namespace       string
	appName         string
	replicas        int
	secretRef       string
	command         string
	args            string
	imagePullPolicy string
	strategy        string
	serviceAccount  string
	healthPath      string
	configMapName   string
}

func addK8sCommonFlags(fs *flag.FlagSet, f *k8sCommonFlags) {
	fs.StringVar(&f.configFile, "config", "app.yaml", "Workflow config file")
	fs.StringVar(&f.image, "image", "", "Container image name:tag (required)")
	fs.StringVar(&f.namespace, "namespace", "default", "Kubernetes namespace")
	fs.StringVar(&f.appName, "app", "", "Application name (default: derived from config)")
	fs.IntVar(&f.replicas, "replicas", 1, "Number of replicas")
	fs.StringVar(&f.secretRef, "secret", "", "Secret name for environment variables")
	fs.StringVar(&f.command, "command", "", "Container command (comma-separated)")
	fs.StringVar(&f.args, "args", "", "Container args (comma-separated, overrides default)")
	fs.StringVar(&f.imagePullPolicy, "image-pull-policy", "", "Image pull policy (Never, Always, IfNotPresent)")
	fs.StringVar(&f.strategy, "strategy", "", "Deployment strategy (Recreate or RollingUpdate)")
	fs.StringVar(&f.serviceAccount, "service-account", "", "Pod service account name")
	fs.StringVar(&f.healthPath, "health-path", "", "Health check path (default: /healthz)")
	fs.StringVar(&f.configMapName, "configmap-name", "", "Override configmap name")
}

func (f *k8sCommonFlags) toDeployRequest(cfg *config.WorkflowConfig, m *manifest.WorkflowManifest) *deploy.DeployRequest {
	req := &deploy.DeployRequest{
		Config:          cfg,
		Manifest:        m,
		Image:           f.image,
		Namespace:       f.namespace,
		AppName:         f.appName,
		Replicas:        f.replicas,
		SecretRef:       f.secretRef,
		ImagePullPolicy: f.imagePullPolicy,
		Strategy:        f.strategy,
		ServiceAccount:  f.serviceAccount,
		HealthPath:      f.healthPath,
		ConfigMapName:   f.configMapName,
	}
	if f.command != "" {
		req.Command = strings.Split(f.command, ",")
	}
	if f.args != "" {
		req.Args = strings.Split(f.args, ",")
	}

	// Read raw config file and expand env vars for the ConfigMap.
	// Use os.Expand with a safe mapper that only replaces variables
	// actually set in the environment — preserves $1, $2, etc.
	rawData, err := os.ReadFile(f.configFile)
	if err == nil {
		req.ConfigFileData = []byte(os.Expand(string(rawData), func(key string) string {
			if v, ok := os.LookupEnv(key); ok {
				return v
			}
			return "$" + key
		}))
	}

	return req
}

func resolveSidecars(cfg *config.WorkflowConfig, platform string) ([]*deploy.SidecarSpec, error) {
	if len(cfg.Sidecars) == 0 {
		return nil, nil
	}
	registry := deploy.NewSidecarRegistry()
	registry.Register(sidecars.NewTailscale())
	registry.Register(sidecars.NewGeneric())
	return registry.Resolve(cfg.Sidecars, platform)
}

func runK8sGenerate(args []string) error {
	fs := flag.NewFlagSet("deploy k8s generate", flag.ContinueOnError)
	var f k8sCommonFlags
	addK8sCommonFlags(fs, &f)
	outputDir := fs.String("output", "./k8s-generated/", "Output directory for generated manifests")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if f.configFile == "" && fs.NArg() > 0 {
		f.configFile = fs.Arg(0)
	}
	if f.image == "" {
		return fmt.Errorf("-image is required")
	}

	cfg, err := config.LoadFromFile(f.configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	m := manifest.Analyze(cfg)
	if f.appName == "" {
		f.appName = m.Name
	}

	req := f.toDeployRequest(cfg, m)
	req.OutputDir = *outputDir

	// Resolve sidecars from config
	sidecarSpecs, err := resolveSidecars(cfg, "kubernetes")
	if err != nil {
		return fmt.Errorf("resolve sidecars: %w", err)
	}
	req.Sidecars = sidecarSpecs

	ms, err := k8s.Build(req)
	if err != nil {
		return fmt.Errorf("build manifests: %w", err)
	}

	if err := ms.WriteYAML(*outputDir, false); err != nil {
		return fmt.Errorf("write manifests: %w", err)
	}

	fmt.Printf("generated %d manifests in %s\n", len(ms.Objects), *outputDir)
	return nil
}

func runK8sApply(args []string) error {
	fs := flag.NewFlagSet("deploy k8s apply", flag.ContinueOnError)
	var f k8sCommonFlags
	addK8sCommonFlags(fs, &f)
	dryRun := fs.Bool("dry-run", false, "Server-side dry run without applying")
	wait := fs.Bool("wait", false, "Wait for rollout to complete")
	force := fs.Bool("force", false, "Force take ownership of fields from other managers")
	build := fs.Bool("build", false, "Build Docker image and load into cluster before deploying")
	dockerfile := fs.String("dockerfile", "Dockerfile", "Path to Dockerfile")
	buildCtx := fs.String("build-context", ".", "Docker build context directory")
	buildArgStr := fs.String("build-arg", "", "Docker build args (comma-separated KEY=VALUE pairs)")
	runtime := fs.String("runtime", "", "Cluster runtime override (minikube|kind|docker-desktop|k3d|remote)")
	registry := fs.String("registry", "", "Registry for remote clusters (e.g. ghcr.io/org)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if f.configFile == "" && fs.NArg() > 0 {
		f.configFile = fs.Arg(0)
	}

	// Load .wfctl.yaml defaults for build settings
	if wfcfg, loadErr := loadWfctlConfig(); loadErr == nil {
		if *dockerfile == "Dockerfile" && wfcfg.BuildDockerfile != "" {
			*dockerfile = wfcfg.BuildDockerfile
		}
		if *buildCtx == "." && wfcfg.BuildContext != "" {
			*buildCtx = wfcfg.BuildContext
		}
		if *runtime == "" && wfcfg.BuildRuntime != "" {
			*runtime = wfcfg.BuildRuntime
		}
		if *registry == "" && wfcfg.BuildRegistry != "" {
			*registry = wfcfg.BuildRegistry
		}
	}

	// Derive image name when --build is set and no -image provided
	if f.image == "" {
		if *build {
			cwd, _ := os.Getwd()
			name := filepath.Base(cwd)
			if wfcfg, err := loadWfctlConfig(); err == nil && wfcfg.ProjectName != "" {
				name = wfcfg.ProjectName
			}
			f.image = resolveImageTag(name)
		} else {
			return fmt.Errorf("-image is required (or use --build to build from Dockerfile)")
		}
	}

	if *build {
		// Resolve image tag if none provided
		f.image = resolveImageTag(f.image)

		// Build the Docker image
		var buildArgs []string
		if *buildArgStr != "" {
			buildArgs = strings.Split(*buildArgStr, ",")
		}
		if err := buildDockerImage(f.image, *dockerfile, *buildCtx, buildArgs); err != nil {
			return err
		}

		// Detect or use explicit runtime
		var rtInfo *k8s.RuntimeInfo
		if *runtime != "" {
			rtInfo = &k8s.RuntimeInfo{
				Runtime:     imageloader.Runtime(*runtime),
				ContextName: *runtime,
				ClusterName: *runtime,
			}
		} else {
			var detectErr error
			rtInfo, detectErr = k8s.DetectRuntime("", "")
			if detectErr != nil {
				return fmt.Errorf("detect cluster runtime: %w", detectErr)
			}
			fmt.Printf("detected runtime: %s (context: %s)\n", rtInfo.Runtime, rtInfo.ContextName)
		}

		// Load image into cluster
		reg := imageloader.NewRegistry()
		reg.Register(imageloader.NewMinikube())
		reg.Register(imageloader.NewKind())
		reg.Register(imageloader.NewDockerDesktop())
		reg.Register(imageloader.NewK3d())
		reg.Register(imageloader.NewRemote())

		loadCfg := &imageloader.LoadConfig{
			Image:    f.image,
			Runtime:  rtInfo.Runtime,
			Registry: *registry,
			Cluster:  rtInfo.ClusterName,
		}

		if rtInfo.Runtime == imageloader.RuntimeRemote && *registry == "" {
			return fmt.Errorf("--registry is required for remote clusters (context %q does not match a local runtime)", rtInfo.ContextName)
		}

		if err := reg.Load(loadCfg); err != nil {
			return fmt.Errorf("load image: %w", err)
		}

		// For remote, use the registry-qualified image name
		if loadCfg.ResolvedImage != "" {
			f.image = loadCfg.ResolvedImage
		}

		// Auto-set imagePullPolicy based on runtime
		if f.imagePullPolicy == "" {
			switch rtInfo.Runtime {
			case imageloader.RuntimeMinikube, imageloader.RuntimeKind, imageloader.RuntimeK3d:
				f.imagePullPolicy = "Never"
			case imageloader.RuntimeDockerDesktop, imageloader.RuntimeRemote:
				f.imagePullPolicy = "IfNotPresent"
			}
		}
	}

	cfg, err := config.LoadFromFile(f.configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	m := manifest.Analyze(cfg)
	if f.appName == "" {
		f.appName = m.Name
	}

	req := f.toDeployRequest(cfg, m)

	// Resolve sidecars from config
	sidecarSpecs, err := resolveSidecars(cfg, "kubernetes")
	if err != nil {
		return fmt.Errorf("resolve sidecars: %w", err)
	}
	req.Sidecars = sidecarSpecs

	target := k8s.NewDeployTarget()
	artifacts, err := target.Generate(context.Background(), req)
	if err != nil {
		return fmt.Errorf("generate: %w", err)
	}

	opts := deploy.ApplyOpts{
		DryRun:       *dryRun,
		Force:        *force,
		FieldManager: "wfctl",
	}

	result, err := target.Apply(context.Background(), artifacts, opts)
	if err != nil {
		return fmt.Errorf("apply: %w", err)
	}

	for _, r := range result.Resources {
		fmt.Printf("  %s  %s/%s\n", r.Status, r.Kind, r.Name)
	}
	fmt.Printf("\n%s\n", result.Message)

	if *wait && !*dryRun {
		fmt.Printf("waiting for rollout...\n")
		waitClient, clientErr := k8s.NewClient(k8s.ClientConfig{Namespace: f.namespace})
		if clientErr != nil {
			return fmt.Errorf("create k8s client for wait: %w", clientErr)
		}
		if waitErr := k8s.WaitForRollout(context.Background(), waitClient, f.appName, f.namespace, 5*60*time.Second); waitErr != nil {
			return fmt.Errorf("rollout: %w", waitErr)
		}
		fmt.Printf("rollout complete\n")
	}

	return nil
}

func runK8sDestroy(args []string) error {
	fs := flag.NewFlagSet("deploy k8s destroy", flag.ContinueOnError)
	namespace := fs.String("namespace", "default", "Kubernetes namespace")
	appName := fs.String("app", "", "Application name (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *appName == "" {
		return fmt.Errorf("-app is required")
	}

	target := k8s.NewDeployTarget()
	if err := target.Destroy(context.Background(), *appName, *namespace); err != nil {
		return fmt.Errorf("destroy: %w", err)
	}

	fmt.Printf("destroyed resources for %q in namespace %q\n", *appName, *namespace)
	return nil
}

func runK8sStatus(args []string) error {
	fs := flag.NewFlagSet("deploy k8s status", flag.ContinueOnError)
	namespace := fs.String("namespace", "default", "Kubernetes namespace")
	appName := fs.String("app", "", "Application name (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *appName == "" {
		return fmt.Errorf("-app is required")
	}

	target := k8s.NewDeployTarget()
	status, err := target.Status(context.Background(), *appName, *namespace)
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}

	fmt.Printf("App:       %s\n", status.AppName)
	fmt.Printf("Namespace: %s\n", status.Namespace)
	fmt.Printf("Phase:     %s\n", status.Phase)
	fmt.Printf("Ready:     %d/%d\n", status.Ready, status.Desired)
	if status.Message != "" {
		fmt.Printf("Message:   %s\n", status.Message)
	}

	if len(status.Resources) > 0 {
		fmt.Printf("\nResources:\n")
		for _, r := range status.Resources {
			fmt.Printf("  %-12s %-30s %s\n", r.Kind, r.Name, r.Status)
		}
	}
	return nil
}

func runK8sLogs(args []string) error {
	fs := flag.NewFlagSet("deploy k8s logs", flag.ContinueOnError)
	namespace := fs.String("namespace", "default", "Kubernetes namespace")
	appName := fs.String("app", "", "Application name (required)")
	container := fs.String("container", "", "Container name (default: app name)")
	follow := fs.Bool("follow", false, "Follow log output")
	tail := fs.Int64("tail", 100, "Number of lines to show from end of logs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *appName == "" {
		return fmt.Errorf("-app is required")
	}

	target := k8s.NewDeployTarget()
	logOpts := deploy.LogOpts{
		Container: *container,
		Follow:    *follow,
		TailLines: *tail,
	}

	reader, err := target.Logs(context.Background(), *appName, *namespace, logOpts)
	if err != nil {
		return fmt.Errorf("logs: %w", err)
	}
	defer reader.Close()

	_, copyErr := io.Copy(os.Stdout, reader)
	return copyErr
}

func runK8sDiff(args []string) error {
	fs := flag.NewFlagSet("deploy k8s diff", flag.ContinueOnError)
	var f k8sCommonFlags
	addK8sCommonFlags(fs, &f)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if f.image == "" {
		return fmt.Errorf("-image is required")
	}

	cfg, err := config.LoadFromFile(f.configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	m := manifest.Analyze(cfg)
	if f.appName == "" {
		f.appName = m.Name
	}

	req := f.toDeployRequest(cfg, m)

	// Resolve sidecars from config
	sidecarSpecs, err := resolveSidecars(cfg, "kubernetes")
	if err != nil {
		return fmt.Errorf("resolve sidecars: %w", err)
	}
	req.Sidecars = sidecarSpecs

	target := k8s.NewDeployTarget()
	artifacts, err := target.Generate(context.Background(), req)
	if err != nil {
		return fmt.Errorf("generate: %w", err)
	}

	diff, err := target.Diff(context.Background(), artifacts)
	if err != nil {
		return fmt.Errorf("diff: %w", err)
	}

	fmt.Print(diff)
	return nil
}

// runDeployCloud deploys infrastructure defined in a workflow config to a cloud provider.
// It reads the config, discovers cloud.account and platform modules, validates
// credentials, shows a plan, and applies changes.
func runDeployCloud(args []string) error {
	fs := flag.NewFlagSet("deploy cloud", flag.ContinueOnError)
	target := fs.String("target", "", "Deployment target: staging or production")
	configFile := fs.String("config", "", "Workflow config file (default: app.yaml or workflow.yaml)")
	dryRun := fs.Bool("dry-run", false, "Show plan without applying changes")
	yes := fs.Bool("yes", false, "Skip confirmation prompt")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl deploy cloud [options]

Deploy infrastructure defined in a workflow config to a cloud environment.
Discovers cloud.account and platform.* modules, validates credentials,
shows a deployment plan, and applies changes.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configFile == "" && fs.NArg() > 0 {
		*configFile = fs.Arg(0)
	}

	if *target != "" && *target != "staging" && *target != "production" {
		return fmt.Errorf("invalid target %q: must be staging or production", *target)
	}

	// Resolve config file
	cfg := *configFile
	if cfg == "" {
		for _, candidate := range []string{"config/app.yaml", "app.yaml", "workflow.yaml", ".wfctl.yaml", "deploy.yaml"} {
			if _, err := os.Stat(candidate); err == nil {
				cfg = candidate
				break
			}
		}
	}
	if cfg == "" {
		return fmt.Errorf("no config file found (tried config/app.yaml, app.yaml, workflow.yaml, .wfctl.yaml, deploy.yaml)")
	}

	data, err := os.ReadFile(cfg)
	if err != nil {
		return fmt.Errorf("read config %s: %w", cfg, err)
	}

	// Parse YAML modules
	type moduleEntry struct {
		Name   string         `yaml:"name"`
		Type   string         `yaml:"type"`
		Config map[string]any `yaml:"config"`
	}
	type appConfig struct {
		Modules []moduleEntry `yaml:"modules"`
	}
	var parsed appConfig
	if yamlErr := yaml.Unmarshal(data, &parsed); yamlErr != nil {
		return fmt.Errorf("parse config %s: %w", cfg, yamlErr)
	}

	// Discover cloud accounts and platform modules
	var cloudAccounts []moduleEntry
	var platformModules []moduleEntry
	for _, m := range parsed.Modules {
		if m.Type == "cloud.account" {
			cloudAccounts = append(cloudAccounts, m)
		}
		if strings.HasPrefix(m.Type, "platform.") {
			platformModules = append(platformModules, m)
		}
	}

	targetLabel := *target
	if targetLabel == "" {
		targetLabel = "default"
	}

	fmt.Printf("Cloud Deployment Plan\n")
	fmt.Printf("=====================\n")
	fmt.Printf("Config:  %s\n", cfg)
	fmt.Printf("Target:  %s\n\n", targetLabel)

	// Report cloud accounts
	if len(cloudAccounts) == 0 {
		fmt.Printf("WARNING: No cloud.account modules found in config.\n")
		fmt.Printf("         Infrastructure modules may not have credentials.\n\n")
	} else {
		fmt.Printf("Cloud Accounts:\n")
		for _, ca := range cloudAccounts {
			provider, _ := ca.Config["provider"].(string)
			region, _ := ca.Config["region"].(string)
			fmt.Printf("  - %s (provider: %s, region: %s)\n", ca.Name, provider, region)
			// TODO(follow-up #28): delegate credential validation to the provider plugin
			// rather than hard-coding per-provider env var names here. Each provider
			// plugin should expose a ValidateCredentials() method or similar.
			// Validate credentials exist
			switch {
			case provider == "aws":
				if os.Getenv("AWS_ACCESS_KEY_ID") != "" || os.Getenv("AWS_PROFILE") != "" {
					fmt.Printf("    credentials: OK (from environment)\n")
				} else {
					credsMap, _ := ca.Config["credentials"].(map[string]any)
					if credsMap != nil {
						fmt.Printf("    credentials: OK (from config)\n")
					} else {
						fmt.Printf("    credentials: WARNING — no AWS credentials found\n")
					}
				}
			case provider == "gcp":
				if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" || os.Getenv("GOOGLE_CLOUD_PROJECT") != "" {
					fmt.Printf("    credentials: OK (from environment)\n")
				} else {
					fmt.Printf("    credentials: check GOOGLE_APPLICATION_CREDENTIALS\n")
				}
			case provider == "azure":
				if os.Getenv("AZURE_SUBSCRIPTION_ID") != "" || os.Getenv("AZURE_TENANT_ID") != "" {
					fmt.Printf("    credentials: OK (from environment)\n")
				} else {
					fmt.Printf("    credentials: check AZURE_TENANT_ID / AZURE_CLIENT_ID\n")
				}
			case provider != "mock":
				fmt.Printf("    credentials: unknown provider %q\n", provider)
			}
		}
		fmt.Println()
	}

	// Report platform modules (deployment plan)
	if len(platformModules) == 0 {
		return fmt.Errorf("no platform.* modules found in config — nothing to deploy")
	}

	fmt.Printf("Infrastructure Modules (%d):\n", len(platformModules))
	for _, pm := range platformModules {
		account, _ := pm.Config["account"].(string)
		detail := pm.Type
		if account != "" {
			detail += fmt.Sprintf(" (account: %s)", account)
		}
		fmt.Printf("  + CREATE  %s  [%s]\n", pm.Name, detail)
	}
	fmt.Println()

	if *dryRun {
		fmt.Printf("Dry run complete. Use 'wfctl deploy cloud --yes' to apply.\n")
		return nil
	}

	if !*yes {
		fmt.Printf("Apply these changes? [y/N] ")
		var answer string
		if _, scanErr := fmt.Scanln(&answer); scanErr != nil || (answer != "y" && answer != "Y" && answer != "yes") {
			return fmt.Errorf("deployment cancelled")
		}
	}

	// Execute deployment via the engine
	fmt.Printf("\nApplying infrastructure...\n")
	cmdArgs := []string{"run", "-config", cfg}
	if *target != "" {
		cmdArgs = append(cmdArgs, "-env", *target)
	}

	wfctl, _ := os.Executable()
	cmd := exec.Command(wfctl, cmdArgs...) //nolint:gosec // G204: re-executing self with validated args
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if runErr := cmd.Run(); runErr != nil {
		return fmt.Errorf("deployment failed: %w", runErr)
	}

	fmt.Printf("\nDeployment complete.\n")
	return nil
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
