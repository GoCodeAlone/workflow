package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/GoCodeAlone/workflow/config"
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
func newDeployProvider(provider string) (DeployProvider, error) {
	switch provider {
	case "kubernetes", "k8s":
		return &kubernetesProvider{}, nil
	case "docker", "docker-compose":
		return &dockerProvider{}, nil
	case "aws-ecs":
		return &awsECSProvider{}, nil
	case "digitalocean", "do":
		return &digitaloceanProvider{}, nil
	default:
		return nil, fmt.Errorf("unsupported deploy provider %q (supported: kubernetes, docker, aws-ecs, digitalocean)", provider)
	}
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

// ── digitalocean provider ─────────────────────────────────────────────────────

type digitaloceanProvider struct {
	baseURL string // defaults to "https://api.digitalocean.com"; injectable for testing
	appID   string // populated after successful Deploy, used by HealthCheck
}

// DO App Platform API request/response types (minimal subset).
type doAppSpec struct {
	Name     string         `json:"name"`
	Region   string         `json:"region,omitempty"`
	Services []doAppService `json:"services"`
}

type doAppService struct {
	Name          string      `json:"name"`
	Image         *doAppImage `json:"image"`
	HTTPPort      int         `json:"http_port,omitempty"`
	InstanceCount int         `json:"instance_count,omitempty"`
	Envs          []doAppEnv  `json:"envs,omitempty"`
}

type doAppImage struct {
	RegistryType string `json:"registry_type"`
	Registry     string `json:"registry"`
	Repository   string `json:"repository"`
	Tag          string `json:"tag"`
}

type doAppEnv struct {
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
	Type  string `json:"type,omitempty"`
}

type doApp struct {
	ID      string    `json:"id"`
	Spec    doAppSpec `json:"spec"`
	LiveURL string    `json:"live_url,omitempty"`
}

type doListAppsResponse struct {
	Apps []doApp `json:"apps"`
}

type doCreateAppRequest struct {
	Spec doAppSpec `json:"spec"`
}

type doAppResponse struct {
	App doApp `json:"app"`
}

func (p *digitaloceanProvider) doBase() string {
	if p.baseURL != "" {
		return p.baseURL
	}
	return "https://api.digitalocean.com"
}

func (p *digitaloceanProvider) Deploy(ctx context.Context, cfg DeployConfig) error {
	token := os.Getenv("DIGITALOCEAN_TOKEN")
	if token == "" {
		return fmt.Errorf("DIGITALOCEAN_TOKEN is required for DigitalOcean deployments")
	}

	spec := p.buildAppSpec(cfg)

	existingID, err := p.findApp(ctx, token, cfg.AppName)
	if err != nil {
		return fmt.Errorf("find app: %w", err)
	}

	var appID string
	if existingID != "" {
		appID, err = p.updateApp(ctx, token, existingID, spec)
		if err != nil {
			return fmt.Errorf("update app: %w", err)
		}
		fmt.Printf("  updated DO app %q (id: %s)\n", cfg.AppName, appID)
	} else {
		appID, err = p.createApp(ctx, token, spec)
		if err != nil {
			return fmt.Errorf("create app: %w", err)
		}
		fmt.Printf("  created DO app %q (id: %s)\n", cfg.AppName, appID)
	}
	p.appID = appID
	return nil
}

func (p *digitaloceanProvider) buildAppSpec(cfg DeployConfig) doAppSpec {
	region := cmp(cfg.Env.Region, "nyc3")
	registry, repository, tag := parseImageRef(cfg.ImageTag)

	var envs []doAppEnv
	for k, v := range cfg.Secrets {
		envs = append(envs, doAppEnv{Key: k, Value: v, Type: "SECRET"})
	}

	instanceCount := 1
	if len(cfg.Services) == 1 {
		for _, svc := range cfg.Services {
			if svc.Scaling != nil && svc.Scaling.Replicas > 0 {
				instanceCount = svc.Scaling.Replicas
			}
		}
	}

	httpPort := 8080
	if len(cfg.Services) == 1 {
		for _, svc := range cfg.Services {
			if len(svc.Expose) > 0 {
				httpPort = svc.Expose[0].Port
			}
		}
	}

	svc := doAppService{
		Name: cfg.AppName,
		Image: &doAppImage{
			RegistryType: "DOCR",
			Registry:     registry,
			Repository:   repository,
			Tag:          tag,
		},
		HTTPPort:      httpPort,
		InstanceCount: instanceCount,
		Envs:          envs,
	}

	return doAppSpec{
		Name:     cfg.AppName,
		Region:   region,
		Services: []doAppService{svc},
	}
}

// parseImageRef splits "registry.digitalocean.com/myreg/myapp:sha" into (registry, repo, tag).
func parseImageRef(imageTag string) (registry, repository, tag string) {
	if i := strings.LastIndex(imageTag, ":"); i >= 0 {
		tag = imageTag[i+1:]
		imageTag = imageTag[:i]
	}
	if i := strings.Index(imageTag, "/"); i >= 0 {
		registry = imageTag[:i]
		repository = imageTag[i+1:]
	} else {
		repository = imageTag
	}
	return
}

func (p *digitaloceanProvider) findApp(ctx context.Context, token, name string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.doBase()+"/v2/apps?name="+name, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET /v2/apps: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("GET /v2/apps: HTTP %d: %s", resp.StatusCode, body)
	}

	var result doListAppsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode apps list: %w", err)
	}
	for _, app := range result.Apps {
		if app.Spec.Name == name {
			return app.ID, nil
		}
	}
	return "", nil
}

func (p *digitaloceanProvider) createApp(ctx context.Context, token string, spec doAppSpec) (string, error) {
	payload, err := json.Marshal(doCreateAppRequest{Spec: spec})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.doBase()+"/v2/apps", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("POST /v2/apps: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("POST /v2/apps: HTTP %d: %s", resp.StatusCode, body)
	}

	var result doAppResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode create app response: %w", err)
	}
	return result.App.ID, nil
}

func (p *digitaloceanProvider) updateApp(ctx context.Context, token, appID string, spec doAppSpec) (string, error) {
	payload, err := json.Marshal(doCreateAppRequest{Spec: spec})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, p.doBase()+"/v2/apps/"+appID, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("PUT /v2/apps/%s: %w", appID, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("PUT /v2/apps/%s: HTTP %d: %s", appID, resp.StatusCode, body)
	}

	var result doAppResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode update app response: %w", err)
	}
	return result.App.ID, nil
}

func (p *digitaloceanProvider) HealthCheck(ctx context.Context, cfg DeployConfig) error {
	if cfg.Env.HealthCheck == nil {
		return nil
	}

	// If we have the app ID and a token, fetch the live URL from DO and prepend it.
	if p.appID != "" {
		if token := os.Getenv("DIGITALOCEAN_TOKEN"); token != "" {
			liveURL, err := p.fetchLiveURL(ctx, token)
			if err == nil && liveURL != "" {
				hcPath := cfg.Env.HealthCheck.Path
				fullURL := strings.TrimRight(liveURL, "/") + "/" + strings.TrimLeft(hcPath, "/")
				hcCopy := *cfg.Env.HealthCheck
				hcCopy.Path = fullURL
				envCopy := *cfg.Env
				envCopy.HealthCheck = &hcCopy
				cfgCopy := cfg
				cfgCopy.Env = &envCopy
				return pollHealthCheck(ctx, cfgCopy)
			}
		}
	}

	return pollHealthCheck(ctx, cfg)
}

func (p *digitaloceanProvider) fetchLiveURL(ctx context.Context, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.doBase()+"/v2/apps/"+p.appID, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("GET /v2/apps/%s: HTTP %d: %s", p.appID, resp.StatusCode, body)
	}

	var result doAppResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	return result.App.LiveURL, nil
}
