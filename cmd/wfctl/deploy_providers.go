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
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/plugin/external"
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

// resolveIaCProvider is the factory used by pluginDeployProvider.ensureProvider
// to load a live IaCProvider from an installed external plugin. It returns both
// the provider and an io.Closer that shuts down any background subprocess.
// Tests override this var to inject fakes without touching the filesystem;
// they may return nil for the closer.
var resolveIaCProvider = discoverAndLoadIaCProvider

// iacPluginManifest is the minimal shape needed to read capabilities.iacProvider.name
// from a plugin.json without relying on the full PluginCapabilities struct.
type iacPluginManifest struct {
	Capabilities struct {
		IaCProvider struct {
			Name string `json:"name"`
		} `json:"iacProvider"`
	} `json:"capabilities"`
}

// findIaCPluginDir scans pluginDir subdirectories for a plugin.json that
// declares capabilities.iacProvider.name == providerName.
// Returns ("", false, nil) when not found; ("name", true/false, nil) when the
// manifest matches (hasBinary indicates whether the executable is present).
func findIaCPluginDir(pluginDir, providerName string) (name string, hasBinary bool, err error) {
	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("scan plugin directory %q: %w", pluginDir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pluginName := entry.Name()
		data, readErr := os.ReadFile(filepath.Join(pluginDir, pluginName, "plugin.json"))
		if readErr != nil {
			continue
		}
		var m iacPluginManifest
		if jsonErr := json.Unmarshal(data, &m); jsonErr != nil {
			continue
		}
		if m.Capabilities.IaCProvider.Name != providerName {
			continue
		}
		binaryPath := filepath.Join(pluginDir, pluginName, pluginName)
		_, statErr := os.Stat(binaryPath)
		return pluginName, statErr == nil, nil
	}
	return "", false, nil
}

// discoverAndLoadIaCProvider implements the default resolveIaCProvider: it scans
// the plugin directory for a plugin that declares iacProvider.name == providerName,
// loads it via ExternalPluginManager, and returns the IaCProvider plus a Closer
// that shuts down the plugin subprocess. The caller must call Close() when done.
func discoverAndLoadIaCProvider(ctx context.Context, providerName string, cfg map[string]any) (interfaces.IaCProvider, io.Closer, error) {
	pluginDir := os.Getenv("WFCTL_PLUGIN_DIR")
	if pluginDir == "" {
		pluginDir = "./data/plugins"
	}

	pluginName, hasBinary, err := findIaCPluginDir(pluginDir, providerName)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve IaC provider %q: %w", providerName, err)
	}
	if pluginName == "" {
		return nil, nil, fmt.Errorf("no plugin found for IaC provider %q in %s — run: wfctl plugin install <plugin-name>", providerName, pluginDir)
	}
	if !hasBinary {
		return nil, nil, fmt.Errorf("plugin %q declares provider %q but binary is missing — run: wfctl plugin install %s", pluginName, providerName, pluginName)
	}

	mgr := external.NewExternalPluginManager(pluginDir, nil)
	closer := closerFunc(func() error { mgr.Shutdown(); return nil })

	adapter, loadErr := mgr.LoadPlugin(pluginName)
	if loadErr != nil {
		mgr.Shutdown()
		return nil, nil, fmt.Errorf("load plugin %q for provider %q: %w", pluginName, providerName, loadErr)
	}

	factories := adapter.ModuleFactories()
	factory, ok := factories["iac.provider"]
	if !ok {
		mgr.Shutdown()
		return nil, nil, fmt.Errorf("plugin %q does not expose an iac.provider module type — upgrade with: wfctl plugin update %s", pluginName, pluginName)
	}

	mod := factory("iac-provider", cfg)
	if mod == nil {
		mgr.Shutdown()
		return nil, nil, fmt.Errorf("plugin %q iac.provider factory returned nil", pluginName)
	}

	// RemoteModule does not directly implement interfaces.IaCProvider; instead it
	// exposes InvokeService for cross-process method dispatch. Wrap it in a
	// remoteIaCProvider that routes each IaCProvider call through InvokeService.
	invoker, ok := mod.(remoteServiceInvoker)
	if !ok {
		mgr.Shutdown()
		return nil, nil, fmt.Errorf("plugin %q iac.provider module (%T) does not support service invocation — upgrade with: wfctl plugin update %s", pluginName, mod, pluginName)
	}

	iacProvider := &remoteIaCProvider{invoker: invoker}
	// Notify the plugin that Initialize has been called (the plugin may treat
	// this as a no-op if it already ran Initialize inside CreateModule).
	if initErr := iacProvider.Initialize(ctx, cfg); initErr != nil {
		mgr.Shutdown()
		return nil, nil, fmt.Errorf("initialize provider %q: %w", providerName, initErr)
	}
	return iacProvider, closer, nil
}

// remoteServiceInvoker is satisfied by *external.RemoteModule, which provides
// InvokeService for cross-process method dispatch.
type remoteServiceInvoker interface {
	InvokeService(method string, args map[string]any) (map[string]any, error)
}

// remoteIaCProvider implements interfaces.IaCProvider by routing every method
// through InvokeService to the plugin subprocess. Only the methods needed by
// wfctl ci run deploy are fully implemented; the rest return a clear error.
type remoteIaCProvider struct {
	invoker remoteServiceInvoker
}

func (r *remoteIaCProvider) Name() string {
	res, err := r.invoker.InvokeService("IaCProvider.Name", nil)
	if err != nil {
		return ""
	}
	name, _ := res["name"].(string)
	return name
}

func (r *remoteIaCProvider) Version() string {
	res, err := r.invoker.InvokeService("IaCProvider.Version", nil)
	if err != nil {
		return ""
	}
	v, _ := res["version"].(string)
	return v
}

func (r *remoteIaCProvider) Initialize(_ context.Context, cfg map[string]any) error {
	_, err := r.invoker.InvokeService("IaCProvider.Initialize", cfg)
	return err
}

func (r *remoteIaCProvider) Capabilities() []interfaces.IaCCapabilityDeclaration { return nil }

func (r *remoteIaCProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, fmt.Errorf("IaCProvider.Plan not supported via remote deploy — use wfctl infra apply")
}

func (r *remoteIaCProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, fmt.Errorf("IaCProvider.Apply not supported via remote deploy — use wfctl infra apply")
}

func (r *remoteIaCProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, fmt.Errorf("IaCProvider.Destroy not supported via remote deploy — use wfctl infra apply")
}

func (r *remoteIaCProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, fmt.Errorf("IaCProvider.Status not supported via remote deploy")
}

func (r *remoteIaCProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, fmt.Errorf("IaCProvider.DetectDrift not supported via remote deploy")
}

func (r *remoteIaCProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, fmt.Errorf("IaCProvider.Import not supported via remote deploy")
}

func (r *remoteIaCProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, fmt.Errorf("IaCProvider.ResolveSizing not supported via remote deploy")
}

func (r *remoteIaCProvider) ResourceDriver(resourceType string) (interfaces.ResourceDriver, error) {
	return &remoteResourceDriver{invoker: r.invoker, resourceType: resourceType}, nil
}

func (r *remoteIaCProvider) Close() error { return nil }

// remoteResourceDriver routes ResourceDriver calls to the plugin via InvokeService.
type remoteResourceDriver struct {
	invoker      remoteServiceInvoker
	resourceType string
}

// decodeResourceOutput converts an InvokeService response map into a *interfaces.ResourceOutput,
// including the Outputs map and Sensitive flags that the previous Update implementation discarded.
func decodeResourceOutput(m map[string]any) *interfaces.ResourceOutput {
	out := &interfaces.ResourceOutput{
		ProviderID: stringFromMap(m, "provider_id"),
		Name:       stringFromMap(m, "name"),
		Type:       stringFromMap(m, "type"),
		Status:     stringFromMap(m, "status"),
	}
	if raw, ok := m["outputs"]; ok {
		if outputs, ok := raw.(map[string]any); ok {
			out.Outputs = outputs
		}
	}
	if raw, ok := m["sensitive"]; ok {
		switch v := raw.(type) {
		case map[string]bool:
			out.Sensitive = v
		case map[string]any:
			sens := make(map[string]bool, len(v))
			for k, val := range v {
				if b, ok := val.(bool); ok {
					sens[k] = b
				}
			}
			out.Sensitive = sens
		}
	}
	return out
}

func (d *remoteResourceDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	res, err := d.invoker.InvokeService("ResourceDriver.Create", map[string]any{
		"resource_type": d.resourceType,
		"spec_name":     spec.Name,
		"spec_type":     spec.Type,
		"spec_config":   spec.Config,
	})
	if err != nil {
		return nil, err
	}
	return decodeResourceOutput(res), nil
}

func (d *remoteResourceDriver) Read(_ context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	res, err := d.invoker.InvokeService("ResourceDriver.Read", map[string]any{
		"resource_type":   d.resourceType,
		"ref_name":        ref.Name,
		"ref_type":        ref.Type,
		"ref_provider_id": ref.ProviderID,
	})
	if err != nil {
		return nil, err
	}
	return decodeResourceOutput(res), nil
}

func (d *remoteResourceDriver) Update(_ context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	res, err := d.invoker.InvokeService("ResourceDriver.Update", map[string]any{
		"resource_type":   d.resourceType,
		"ref_name":        ref.Name,
		"ref_type":        ref.Type,
		"ref_provider_id": ref.ProviderID,
		"spec_name":       spec.Name,
		"spec_type":       spec.Type,
		"spec_config":     spec.Config,
	})
	if err != nil {
		return nil, err
	}
	return decodeResourceOutput(res), nil
}

func (d *remoteResourceDriver) Delete(_ context.Context, ref interfaces.ResourceRef) error {
	_, err := d.invoker.InvokeService("ResourceDriver.Delete", map[string]any{
		"resource_type":   d.resourceType,
		"ref_name":        ref.Name,
		"ref_type":        ref.Type,
		"ref_provider_id": ref.ProviderID,
	})
	return err
}

func (d *remoteResourceDriver) Diff(_ context.Context, desired interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	args := map[string]any{
		"resource_type":       d.resourceType,
		"spec_name":           desired.Name,
		"spec_type":           desired.Type,
		"spec_config":         desired.Config,
		"current_name":        current.Name,
		"current_type":        current.Type,
		"current_provider_id": current.ProviderID,
		"current_status":      current.Status,
		"current_outputs":     current.Outputs,
		"current_sensitive":   current.Sensitive,
	}
	res, err := d.invoker.InvokeService("ResourceDriver.Diff", args)
	if err != nil {
		return nil, err
	}
	result := &interfaces.DiffResult{}
	result.NeedsUpdate, _ = res["needs_update"].(bool)
	result.NeedsReplace, _ = res["needs_replace"].(bool)
	if rawChanges, ok := res["changes"]; ok {
		if changes, ok := rawChanges.([]any); ok {
			for _, c := range changes {
				if cm, ok := c.(map[string]any); ok {
					fc := interfaces.FieldChange{
						Path: stringFromMap(cm, "path"),
						Old:  cm["old"],
						New:  cm["new"],
					}
					fc.ForceNew, _ = cm["force_new"].(bool)
					result.Changes = append(result.Changes, fc)
				}
			}
		}
	}
	return result, nil
}

func (d *remoteResourceDriver) HealthCheck(_ context.Context, ref interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	res, err := d.invoker.InvokeService("ResourceDriver.HealthCheck", map[string]any{
		"resource_type":   d.resourceType,
		"ref_name":        ref.Name,
		"ref_type":        ref.Type,
		"ref_provider_id": ref.ProviderID,
	})
	if err != nil {
		return nil, err
	}
	healthy, _ := res["healthy"].(bool)
	message, _ := res["message"].(string)
	return &interfaces.HealthResult{Healthy: healthy, Message: message}, nil
}

func (d *remoteResourceDriver) Scale(_ context.Context, ref interfaces.ResourceRef, replicas int) (*interfaces.ResourceOutput, error) {
	res, err := d.invoker.InvokeService("ResourceDriver.Scale", map[string]any{
		"resource_type":   d.resourceType,
		"ref_name":        ref.Name,
		"ref_type":        ref.Type,
		"ref_provider_id": ref.ProviderID,
		"replicas":        replicas,
	})
	if err != nil {
		return nil, err
	}
	return decodeResourceOutput(res), nil
}

func (d *remoteResourceDriver) SensitiveKeys() []string {
	res, err := d.invoker.InvokeService("ResourceDriver.SensitiveKeys", map[string]any{
		"resource_type": d.resourceType,
	})
	if err != nil {
		return nil
	}
	raw, ok := res["keys"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			keys = append(keys, s)
		}
	}
	return keys
}

func stringFromMap(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// closerFunc adapts a func() error to io.Closer.
type closerFunc func() error

func (f closerFunc) Close() error { return f() }

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

	// Find the deploy-target resource module referencing this provider.
	// Prefer known container/app deployment types (where Update(image) makes
	// sense) over generic infra resources like VPC, firewall, DNS, etc. which
	// don't have an "image" concept and would reject the Update call. The
	// ordered preference list captures the common deployment targets; if none
	// match, fall back to the first infra.* module with a warning so the
	// behaviour is predictable rather than silently wrong.
	deployTargetTypes := []string{
		"infra.container_service",
		"platform.do_app",
		"platform.app_platform",
		"infra.k8s_cluster",
	}
	var resourceName, resourceType string
	var resourceCfg map[string]any
	findByType := func(target string) bool {
		for _, m := range wfCfg.Modules {
			if m.Type != target {
				continue
			}
			if p, _ := m.Config["provider"].(string); p == providerModName {
				resourceName = m.Name
				resourceType = m.Type
				resourceCfg = m.Config
				return true
			}
		}
		return false
	}
	for _, t := range deployTargetTypes {
		if findByType(t) {
			break
		}
	}
	if resourceName == "" {
		// Fallback: first infra.* module with matching provider.
		for _, m := range wfCfg.Modules {
			if m.Type == "iac.provider" || m.Type == "" {
				continue
			}
			if p, _ := m.Config["provider"].(string); p == providerModName {
				fmt.Fprintf(os.Stderr, "warning: no deploy-target module (%v) found for provider %q; falling back to first infra module %q (type %q)\n",
					deployTargetTypes, providerModName, m.Name, m.Type)
				resourceName = m.Name
				resourceType = m.Type
				resourceCfg = m.Config
				break
			}
		}
	}
	if resourceName == "" {
		return nil, fmt.Errorf("no infra resource module found for provider %q in workflow config", providerModName)
	}

	// Provider is resolved lazily on first Deploy/HealthCheck to thread the real ctx.
	return &pluginDeployProvider{
		providerName: providerName,
		providerCfg:  providerModCfg,
		resourceName: resourceName,
		resourceType: resourceType,
		resourceCfg:  resourceCfg,
	}, nil
}

// pluginDeployProvider wraps an IaCProvider and a single infra resource as a DeployProvider.
// The IaCProvider is resolved lazily on first use so the real request context is threaded
// through to Initialize rather than a synthetic context.Background().
type pluginDeployProvider struct {
	// lazy-resolution fields (set at construction)
	providerName string
	providerCfg  map[string]any
	// resource target (set at construction)
	resourceName string
	resourceType string
	resourceCfg  map[string]any
	// resolved once on first ensureProvider call
	once     sync.Once
	provider interfaces.IaCProvider
	provErr  error
	closer   io.Closer
}

func (p *pluginDeployProvider) ensureProvider(ctx context.Context) error {
	p.once.Do(func() {
		if p.provider != nil {
			return // already injected (e.g. by tests constructing the struct directly)
		}
		prov, closer, err := resolveIaCProvider(ctx, p.providerName, p.providerCfg)
		p.provider = prov
		p.closer = closer
		p.provErr = err
	})
	if p.provErr != nil {
		return fmt.Errorf("resolve provider %q: %w", p.providerName, p.provErr)
	}
	return nil
}

// Close shuts down the plugin subprocess, if any. The DeployProvider interface
// does not include Close; callers should type-assert to io.Closer after use.
func (p *pluginDeployProvider) Close() error {
	if p.closer != nil {
		return p.closer.Close()
	}
	return nil
}

func (p *pluginDeployProvider) Deploy(ctx context.Context, cfg DeployConfig) error {
	if err := p.ensureProvider(ctx); err != nil {
		return err
	}
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
	if err := p.ensureProvider(ctx); err != nil {
		return err
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
