package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// DeployConfig holds all parameters needed to execute a deployment.
type DeployConfig struct {
	// EnvName is the target environment name (e.g. "staging", "production").
	EnvName string
	// Env is the resolved CIDeployEnvironment config for the target.
	Env *config.CIDeployEnvironment
	// Secrets maps secret name → value, injected as env vars or k8s secrets.
	Secrets map[string]string
	// AppName is the top-level application name from ci config or binary target.
	AppName string
	// ImageTag is the container image tag to deploy (e.g. "myapp:abc1234").
	ImageTag string
	// Verbose controls whether subcommand output is printed.
	Verbose bool
	// Services carries the parsed services: map for multi-service deployments.
	Services map[string]*config.ServiceConfig
}

// DeployProvider handles deploying to a specific infrastructure target.
type DeployProvider interface {
	// Deploy pushes the application to the target infrastructure.
	Deploy(ctx context.Context, cfg DeployConfig) error
	// HealthCheck polls the deployment until healthy or the timeout elapses.
	HealthCheck(ctx context.Context, cfg DeployConfig) error
}

// newDeployProvider returns the DeployProvider for the given provider name.
// For non-built-in providers, wfCfg is consulted to find a matching iac.provider
// module and its infra.container_service resource. Pass nil wfCfg to restrict to
// built-ins only.
func newDeployProvider(provider string, wfCfg *config.WorkflowConfig) (DeployProvider, error) {
	switch provider {
	case "kubernetes", "k8s":
		return &kubernetesProvider{}, nil
	case "docker", "docker-compose":
		return &dockerProvider{}, nil
	case "aws-ecs":
		return &awsECSProvider{}, nil
	default:
		return newPluginDeployProvider(provider, wfCfg)
	}
}

// resolveIaCProvider is the factory used by newPluginDeployProvider to obtain a
// live IaCProvider from module config. Tests override this to inject fakes.
var resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, error) {
	return nil, fmt.Errorf("no in-process provider loader available; use pre-deploy steps with 'wfctl infra apply' to deploy via a workflow plugin")
}

// newPluginDeployProvider looks up a matching iac.provider + infra.container_service
// module pair in wfCfg and wraps them as a DeployProvider.
func newPluginDeployProvider(providerName string, wfCfg *config.WorkflowConfig) (DeployProvider, error) {
	const hint = "\n  Example:\n    modules:\n    - name: my-provider\n      type: iac.provider\n      config:\n        provider: %s\n        credentials: env"
	if wfCfg == nil || len(wfCfg.Modules) == 0 {
		return nil, fmt.Errorf("unsupported deploy provider %q (built-ins: kubernetes, docker, aws-ecs; to use a plugin provider, declare an iac.provider module in your workflow config)%s", providerName, fmt.Sprintf(hint, providerName))
	}

	// Find the iac.provider module matching the requested provider name.
	var providerModName string
	var providerModCfg map[string]any
	for _, m := range wfCfg.Modules {
		if m.Type != "iac.provider" {
			continue
		}
		cfgProvider, _ := m.Config["provider"].(string)
		if cfgProvider == providerName || m.Name == providerName {
			providerModName = m.Name
			providerModCfg = m.Config
			break
		}
	}
	if providerModName == "" {
		return nil, fmt.Errorf("unsupported deploy provider %q (built-ins: kubernetes, docker, aws-ecs; to use a plugin provider, declare an iac.provider module in your workflow config)%s", providerName, fmt.Sprintf(hint, providerName))
	}

	// Find the first infra.container_service module referencing this provider.
	var resourceName string
	var resourceCfg map[string]any
	for _, m := range wfCfg.Modules {
		if m.Type != "infra.container_service" {
			continue
		}
		if p, _ := m.Config["provider"].(string); p == providerModName {
			resourceName = m.Name
			resourceCfg = m.Config
			break
		}
	}
	if resourceName == "" {
		return nil, fmt.Errorf("no infra.container_service module found for provider %q in workflow config", providerModName)
	}

	iacProvider, err := resolveIaCProvider(context.Background(), providerName, providerModCfg)
	if err != nil {
		return nil, fmt.Errorf("resolve provider %q: %w", providerName, err)
	}

	return &pluginDeployProvider{
		provider:     iacProvider,
		resourceName: resourceName,
		resourceType: "infra.container_service",
		resourceCfg:  resourceCfg,
	}, nil
}

// pluginDeployProvider wraps an IaCProvider and a single infra resource as a DeployProvider.
type pluginDeployProvider struct {
	provider     interfaces.IaCProvider
	resourceName string
	resourceType string
	resourceCfg  map[string]any
}

func (p *pluginDeployProvider) Deploy(ctx context.Context, cfg DeployConfig) error {
	driver, err := p.provider.ResourceDriver(p.resourceType)
	if err != nil {
		return fmt.Errorf("plugin deploy: no driver for %q: %w", p.resourceType, err)
	}
	merged := make(map[string]any, len(p.resourceCfg)+1)
	for k, v := range p.resourceCfg {
		merged[k] = v
	}
	merged["image"] = cfg.ImageTag
	ref := interfaces.ResourceRef{Name: p.resourceName, Type: p.resourceType}
	spec := interfaces.ResourceSpec{Name: p.resourceName, Type: p.resourceType, Config: merged}
	if _, err := driver.Update(ctx, ref, spec); err != nil {
		return fmt.Errorf("plugin deploy %q: update image: %w", p.resourceName, err)
	}
	fmt.Printf("  plugin deploy: updated %q to %s\n", p.resourceName, cfg.ImageTag)
	return nil
}

func (p *pluginDeployProvider) HealthCheck(ctx context.Context, cfg DeployConfig) error {
	if cfg.Env == nil || cfg.Env.HealthCheck == nil {
		return nil
	}
	driver, err := p.provider.ResourceDriver(p.resourceType)
	if err != nil {
		return fmt.Errorf("plugin health check: no driver for %q: %w", p.resourceType, err)
	}
	ref := interfaces.ResourceRef{Name: p.resourceName, Type: p.resourceType}
	result, err := driver.HealthCheck(ctx, ref)
	if err != nil {
		return fmt.Errorf("plugin health check %q: %w", p.resourceName, err)
	}
	if !result.Healthy {
		return fmt.Errorf("plugin health check %q: unhealthy: %s", p.resourceName, result.Message)
	}
	return nil
}

// ── kubernetes provider ───────────────────────────────────────────────────────

type kubernetesProvider struct{}

func (p *kubernetesProvider) Deploy(ctx context.Context, cfg DeployConfig) error {
	namespace := cmp(cfg.Env.Namespace, "default")
	cluster := cfg.Env.Cluster

	manifests, err := generateK8sManifests(cfg)
	if err != nil {
		return fmt.Errorf("generate k8s manifests: %w", err)
	}

	kubectlArgs := []string{"apply", "-f", "-"}
	if namespace != "" {
		kubectlArgs = append(kubectlArgs, "--namespace", namespace)
	}
	if cluster != "" {
		kubectlArgs = append(kubectlArgs, "--context", cluster)
	}

	cmd := exec.CommandContext(ctx, "kubectl", kubectlArgs...) //nolint:gosec // args from config
	cmd.Stdin = strings.NewReader(manifests)
	if cfg.Verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl apply: %w", err)
	}

	strategy := cmp(cfg.Env.Strategy, "rolling")
	fmt.Printf("  applied k8s manifests (namespace: %s, strategy: %s)\n", namespace, strategy)
	return nil
}

func (p *kubernetesProvider) HealthCheck(ctx context.Context, cfg DeployConfig) error {
	if cfg.Env.HealthCheck == nil {
		return nil
	}
	return pollHealthCheck(ctx, cfg)
}

// generateK8sManifests produces Deployment + Service YAML for the app.
// When cfg.Services is populated each service gets its own Deployment/Service.
func generateK8sManifests(cfg DeployConfig) (string, error) {
	if len(cfg.Services) > 0 {
		var sb strings.Builder
		for name, svc := range cfg.Services {
			m, err := renderServiceManifest(name, svc, cfg)
			if err != nil {
				return "", fmt.Errorf("service %s: %w", name, err)
			}
			sb.WriteString(m)
			sb.WriteString("---\n")
		}
		return sb.String(), nil
	}
	return renderSingleManifest(cfg)
}

const k8sDeploymentTmpl = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
  labels:
    app: {{ .Name }}
spec:
  replicas: {{ .Replicas }}
  selector:
    matchLabels:
      app: {{ .Name }}
  strategy:
    type: {{ .Strategy }}
  template:
    metadata:
      labels:
        app: {{ .Name }}
    spec:
      containers:
      - name: {{ .Name }}
        image: {{ .Image }}
        ports:{{ range .Ports }}
        - containerPort: {{ .Port }}
          protocol: {{ .Protocol }}{{ end }}{{ if .EnvVars }}
        env:{{ range .EnvVars }}
        - name: {{ .Name }}
          value: "{{ .Value }}"{{ end }}{{ end }}
---
apiVersion: v1
kind: Service
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
spec:
  selector:
    app: {{ .Name }}
  ports:{{ range .Ports }}
  - port: {{ .Port }}
    targetPort: {{ .Port }}
    protocol: {{ .Protocol }}{{ end }}
`

type k8sManifestData struct {
	Name      string
	Namespace string
	Replicas  int
	Strategy  string
	Image     string
	Ports     []k8sPort
	EnvVars   []k8sEnvVar
}

type k8sPort struct {
	Port     int
	Protocol string
}

type k8sEnvVar struct {
	Name  string
	Value string
}

func renderSingleManifest(cfg DeployConfig) (string, error) {
	namespace := cmp(cfg.Env.Namespace, "default")
	strategy := k8sStrategy(cmp(cfg.Env.Strategy, "rolling"))
	image := cmp(cfg.ImageTag, cfg.AppName+":latest")

	data := k8sManifestData{
		Name:      cfg.AppName,
		Namespace: namespace,
		Replicas:  1,
		Strategy:  strategy,
		Image:     image,
		EnvVars:   secretsToEnvVars(cfg.Secrets),
	}
	return renderManifestTemplate(data)
}

func renderServiceManifest(name string, svc *config.ServiceConfig, cfg DeployConfig) (string, error) {
	namespace := cmp(cfg.Env.Namespace, "default")
	strategy := k8sStrategy(cmp(cfg.Env.Strategy, "rolling"))

	replicas := 1
	if svc.Scaling != nil && svc.Scaling.Replicas > 0 {
		replicas = svc.Scaling.Replicas
	}

	image := name + ":latest"
	if svc.Binary != "" {
		image = name + ":latest"
	}
	if cfg.ImageTag != "" {
		image = name + ":" + imageTagSuffix(cfg.ImageTag)
	}

	var ports []k8sPort
	for _, e := range svc.Expose {
		proto := strings.ToUpper(cmp(e.Protocol, "TCP"))
		ports = append(ports, k8sPort{Port: e.Port, Protocol: proto})
	}

	data := k8sManifestData{
		Name:      name,
		Namespace: namespace,
		Replicas:  replicas,
		Strategy:  strategy,
		Image:     image,
		Ports:     ports,
		EnvVars:   secretsToEnvVars(cfg.Secrets),
	}
	return renderManifestTemplate(data)
}

func renderManifestTemplate(data k8sManifestData) (string, error) {
	tmpl, err := template.New("k8s").Parse(k8sDeploymentTmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// k8sStrategy maps the workflow strategy name to a Kubernetes DeploymentStrategy type.
func k8sStrategy(strategy string) string {
	switch strategy {
	case "rolling":
		return "RollingUpdate"
	case "blue-green", "canary":
		// Both map to RollingUpdate at the Deployment level; true blue-green/canary
		// would require Argo Rollouts or a service mesh, which is out of scope here.
		return "RollingUpdate"
	case "recreate":
		return "Recreate"
	default:
		return "RollingUpdate"
	}
}

// imageTagSuffix extracts the tag portion from an "image:tag" string.
func imageTagSuffix(imageTag string) string {
	if i := strings.LastIndex(imageTag, ":"); i >= 0 {
		return imageTag[i+1:]
	}
	return imageTag
}

func secretsToEnvVars(secrets map[string]string) []k8sEnvVar {
	if len(secrets) == 0 {
		return nil
	}
	envVars := make([]k8sEnvVar, 0, len(secrets))
	for k, v := range secrets {
		envVars = append(envVars, k8sEnvVar{Name: k, Value: v})
	}
	return envVars
}

// ── docker provider ───────────────────────────────────────────────────────────

type dockerProvider struct{}

func (p *dockerProvider) Deploy(ctx context.Context, cfg DeployConfig) error {
	compose, err := generateDockerCompose(cfg)
	if err != nil {
		return fmt.Errorf("generate docker-compose: %w", err)
	}

	composeFile := "docker-compose.wfctl.yml"
	if err := os.WriteFile(composeFile, []byte(compose), 0o600); err != nil {
		return fmt.Errorf("write compose file: %w", err)
	}
	defer os.Remove(composeFile)

	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", composeFile, "up", "-d", "--remove-orphans") //nolint:gosec // args from config
	if cfg.Verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose up: %w", err)
	}

	fmt.Printf("  docker compose up complete\n")
	return nil
}

func (p *dockerProvider) HealthCheck(ctx context.Context, cfg DeployConfig) error {
	if cfg.Env.HealthCheck == nil {
		return nil
	}
	return pollHealthCheck(ctx, cfg)
}

const dockerComposeTmpl = `version: "3.8"
services:{{ range .Services }}
  {{ .Name }}:
    image: {{ .Image }}{{ if .Ports }}
    ports:{{ range .Ports }}
    - "{{ .Host }}:{{ .Container }}"{{ end }}{{ end }}{{ if .EnvVars }}
    environment:{{ range .EnvVars }}
      {{ .Name }}: "{{ .Value }}"{{ end }}{{ end }}
{{ end }}`

type composeData struct {
	Services []composeService
}

type composeService struct {
	Name    string
	Image   string
	Ports   []composePort
	EnvVars []k8sEnvVar
}

type composePort struct {
	Host      int
	Container int
}

func generateDockerCompose(cfg DeployConfig) (string, error) {
	var services []composeService

	if len(cfg.Services) > 0 {
		for name, svc := range cfg.Services {
			image := name + ":latest"
			if cfg.ImageTag != "" {
				image = name + ":" + imageTagSuffix(cfg.ImageTag)
			}
			var ports []composePort
			for _, e := range svc.Expose {
				ports = append(ports, composePort{Host: e.Port, Container: e.Port})
			}
			services = append(services, composeService{
				Name:    name,
				Image:   image,
				Ports:   ports,
				EnvVars: secretsToEnvVars(cfg.Secrets),
			})
		}
	} else {
		image := cmp(cfg.ImageTag, cfg.AppName+":latest")
		services = append(services, composeService{
			Name:    cfg.AppName,
			Image:   image,
			EnvVars: secretsToEnvVars(cfg.Secrets),
		})
	}

	tmpl, err := template.New("compose").Parse(dockerComposeTmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, composeData{Services: services}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ── aws-ecs provider (stub) ───────────────────────────────────────────────────

type awsECSProvider struct{}

func (p *awsECSProvider) Deploy(_ context.Context, cfg DeployConfig) error {
	fmt.Printf("  aws-ecs deploy stub: region=%s cluster=%s (full implementation requires AWS SDK)\n",
		cfg.Env.Region, cfg.Env.Cluster)
	return nil
}

func (p *awsECSProvider) HealthCheck(ctx context.Context, cfg DeployConfig) error {
	if cfg.Env.HealthCheck == nil {
		return nil
	}
	return pollHealthCheck(ctx, cfg)
}

// ── health check ─────────────────────────────────────────────────────────────

// pollHealthCheck polls cfg.Env.HealthCheck.Path until it returns HTTP 2xx
// or the configured timeout elapses.
func pollHealthCheck(ctx context.Context, cfg DeployConfig) error {
	hc := cfg.Env.HealthCheck
	if hc.Path == "" {
		return nil
	}

	timeout := 60 * time.Second
	if hc.Timeout != "" {
		if d, err := time.ParseDuration(hc.Timeout); err == nil {
			timeout = d
		}
	}

	deadline := time.Now().Add(timeout)
	url := hc.Path
	if !strings.HasPrefix(url, "http") {
		url = "http://localhost" + url
	}

	fmt.Printf("  health check: %s (timeout: %s)\n", url, timeout)

	client := &http.Client{Timeout: 5 * time.Second}
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("health check request: %w", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				fmt.Printf("  health check passed (%d)\n", resp.StatusCode)
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return fmt.Errorf("health check timed out after %s", timeout)
}

// ── secret injection ──────────────────────────────────────────────────────────

// injectSecrets fetches secrets from the configured provider(s) and returns them
// as a name→value map for use during deployment. When cfg contains a SecretStores
// map or per-secret Store fields, each secret is routed to its correct store.
// The envName parameter is used to apply environment-level SecretsStoreOverride.
func injectSecrets(ctx context.Context, cfg *config.WorkflowConfig, envName string) (map[string]string, error) {
	if cfg == nil || cfg.Secrets == nil || len(cfg.Secrets.Entries) == 0 {
		return nil, nil
	}

	result := make(map[string]string, len(cfg.Secrets.Entries))
	for _, entry := range cfg.Secrets.Entries {
		storeName := ResolveSecretStore(entry.Name, envName, cfg)
		provider, err := getProviderForStore(storeName, cfg)
		if err != nil {
			return nil, fmt.Errorf("secret %q: store %q: %w", entry.Name, storeName, err)
		}
		val, err := provider.Get(ctx, entry.Name)
		if err != nil {
			return nil, fmt.Errorf("secret %q: fetch from %q: %w", entry.Name, storeName, err)
		}
		result[entry.Name] = val
	}
	return result, nil
}

// cmp returns a if non-empty, otherwise b. Mirrors cmp.Or for strings.
func cmp(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
