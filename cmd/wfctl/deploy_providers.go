package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
// built-ins only. envName selects the per-environment config overlay; pass ""
// to use top-level config only.
func newDeployProvider(provider string, wfCfg *config.WorkflowConfig, envName string) (DeployProvider, error) {
	switch provider {
	case "kubernetes", "k8s":
		return &kubernetesProvider{}, nil
	case "docker", "docker-compose":
		return &dockerProvider{}, nil
	case "aws-ecs":
		return &awsECSProvider{}, nil
	default:
		return newPluginDeployProvider(provider, wfCfg, envName)
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

// jsonToAny converts any typed value to a JSON-compatible any (map[string]any,
// []any, etc.) suitable for embedding in InvokeService arg maps.
func jsonToAny(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// anyToStruct decodes a JSON-compatible any value (map[string]any / []any) into
// a typed struct using a JSON round-trip.
func anyToStruct(src any, dst any) error {
	b, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

func (r *remoteIaCProvider) Capabilities() []interfaces.IaCCapabilityDeclaration {
	res, err := r.invoker.InvokeService("IaCProvider.Capabilities", nil)
	if err != nil {
		return nil
	}
	raw, ok := res["capabilities"]
	if !ok {
		return nil
	}
	var caps []interfaces.IaCCapabilityDeclaration
	if err := anyToStruct(raw, &caps); err != nil {
		return nil
	}
	return caps
}

func (r *remoteIaCProvider) Plan(_ context.Context, desired []interfaces.ResourceSpec, current []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	desiredAny, err := jsonToAny(desired)
	if err != nil {
		return nil, fmt.Errorf("IaCProvider.Plan: marshal desired: %w", err)
	}
	currentAny, err := jsonToAny(current)
	if err != nil {
		return nil, fmt.Errorf("IaCProvider.Plan: marshal current: %w", err)
	}
	res, err := r.invoker.InvokeService("IaCProvider.Plan", map[string]any{
		"desired": desiredAny,
		"current": currentAny,
	})
	if err != nil {
		return nil, err
	}
	var plan interfaces.IaCPlan
	if err := anyToStruct(res, &plan); err != nil {
		return nil, fmt.Errorf("IaCProvider.Plan: decode result: %w", err)
	}
	return &plan, nil
}

func (r *remoteIaCProvider) Apply(_ context.Context, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	planAny, err := jsonToAny(plan)
	if err != nil {
		return nil, fmt.Errorf("IaCProvider.Apply: marshal plan: %w", err)
	}
	res, err := r.invoker.InvokeService("IaCProvider.Apply", map[string]any{"plan": planAny})
	if err != nil {
		return nil, err
	}
	var result interfaces.ApplyResult
	if err := anyToStruct(res, &result); err != nil {
		return nil, fmt.Errorf("IaCProvider.Apply: decode result: %w", err)
	}
	return &result, nil
}

func (r *remoteIaCProvider) Destroy(_ context.Context, refs []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	refsAny, err := jsonToAny(refs)
	if err != nil {
		return nil, fmt.Errorf("IaCProvider.Destroy: marshal refs: %w", err)
	}
	res, err := r.invoker.InvokeService("IaCProvider.Destroy", map[string]any{
		"refs": refsAny,
	})
	if err != nil {
		return nil, err
	}
	var result interfaces.DestroyResult
	if err := anyToStruct(res, &result); err != nil {
		return nil, fmt.Errorf("IaCProvider.Destroy: decode result: %w", err)
	}
	return &result, nil
}

func (r *remoteIaCProvider) Status(_ context.Context, refs []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	refsAny, err := jsonToAny(refs)
	if err != nil {
		return nil, fmt.Errorf("IaCProvider.Status: marshal refs: %w", err)
	}
	res, err := r.invoker.InvokeService("IaCProvider.Status", map[string]any{
		"refs": refsAny,
	})
	if err != nil {
		return nil, err
	}
	raw, ok := res["statuses"]
	if !ok {
		return nil, nil
	}
	var statuses []interfaces.ResourceStatus
	if err := anyToStruct(raw, &statuses); err != nil {
		return nil, fmt.Errorf("IaCProvider.Status: decode result: %w", err)
	}
	return statuses, nil
}

func (r *remoteIaCProvider) DetectDrift(_ context.Context, refs []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	refsAny, err := jsonToAny(refs)
	if err != nil {
		return nil, fmt.Errorf("IaCProvider.DetectDrift: marshal refs: %w", err)
	}
	res, err := r.invoker.InvokeService("IaCProvider.DetectDrift", map[string]any{
		"refs": refsAny,
	})
	if err != nil {
		return nil, err
	}
	raw, ok := res["drifts"]
	if !ok {
		return nil, nil
	}
	var drifts []interfaces.DriftResult
	if err := anyToStruct(raw, &drifts); err != nil {
		return nil, fmt.Errorf("IaCProvider.DetectDrift: decode result: %w", err)
	}
	return drifts, nil
}

func (r *remoteIaCProvider) Import(_ context.Context, cloudID string, resourceType string) (*interfaces.ResourceState, error) {
	res, err := r.invoker.InvokeService("IaCProvider.Import", map[string]any{
		"provider_id":   cloudID,
		"resource_type": resourceType,
	})
	if err != nil {
		return nil, err
	}
	var state interfaces.ResourceState
	if err := anyToStruct(res, &state); err != nil {
		return nil, fmt.Errorf("IaCProvider.Import: decode result: %w", err)
	}
	return &state, nil
}

func (r *remoteIaCProvider) ResolveSizing(resourceType string, size interfaces.Size, hints *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	hintsAny, err := jsonToAny(hints)
	if err != nil {
		return nil, fmt.Errorf("IaCProvider.ResolveSizing: marshal hints: %w", err)
	}
	res, err := r.invoker.InvokeService("IaCProvider.ResolveSizing", map[string]any{
		"resource_type": resourceType,
		"size":          string(size),
		"hints":         hintsAny,
	})
	if err != nil {
		return nil, err
	}
	var sizing interfaces.ProviderSizing
	if err := anyToStruct(res, &sizing); err != nil {
		return nil, fmt.Errorf("IaCProvider.ResolveSizing: decode result: %w", err)
	}
	return &sizing, nil
}

func (r *remoteIaCProvider) RepairDirtyMigration(_ context.Context, req interfaces.MigrationRepairRequest) (*interfaces.MigrationRepairResult, error) {
	reqAny, err := jsonToAny(req)
	if err != nil {
		return nil, fmt.Errorf("IaCProvider.RepairDirtyMigration: marshal request: %w", err)
	}
	res, err := r.invoker.InvokeService("IaCProvider.RepairDirtyMigration", map[string]any{
		"request": reqAny,
	})
	if err != nil {
		return nil, err
	}
	var result interfaces.MigrationRepairResult
	if err := anyToStruct(res, &result); err != nil {
		return nil, fmt.Errorf("IaCProvider.RepairDirtyMigration: decode result: %w", err)
	}
	return &result, nil
}

func (r *remoteIaCProvider) ResourceDriver(resourceType string) (interfaces.ResourceDriver, error) {
	return &remoteResourceDriver{invoker: r.invoker, resourceType: resourceType}, nil
}

// SupportedCanonicalKeys returns the full canonical key set for remote providers.
// External plugin providers may override this via the plugin manifest in a future phase.
func (r *remoteIaCProvider) BootstrapStateBackend(ctx context.Context, cfg map[string]any) (*interfaces.BootstrapResult, error) {
	res, err := r.invoker.InvokeService("IaCProvider.BootstrapStateBackend", cfg)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	var result interfaces.BootstrapResult
	if err := anyToStruct(res, &result); err != nil {
		return nil, fmt.Errorf("BootstrapStateBackend: decode result: %w", err)
	}
	return &result, nil
}

func (r *remoteIaCProvider) SupportedCanonicalKeys() []string {
	return interfaces.CanonicalKeys()
}

func (r *remoteIaCProvider) Close() error { return nil }

// remoteResourceDriver routes ResourceDriver calls to the plugin via InvokeService.
type remoteResourceDriver struct {
	invoker      remoteServiceInvoker
	resourceType string
}

// wrapIaCError categorizes plugin errors by matching HTTP status codes and
// common message patterns, wrapping with the appropriate IaC sentinel so
// callers can use errors.Is for control flow. Errors crossing the plugin
// boundary arrive as plain strings, so sentinel matching must be text-based.
// Returns err unchanged when no pattern matches.
func wrapIaCError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	switch {
	case containsAny(msg, "not found", "404", "405", "does not exist", "no such"):
		return fmt.Errorf("%w: %v", interfaces.ErrResourceNotFound, err)
	case containsAny(msg, "already exists", "409", "conflict"):
		return fmt.Errorf("%w: %v", interfaces.ErrResourceAlreadyExists, err)
	case containsAny(msg, "rate limit", "429", "too many requests"):
		return fmt.Errorf("%w: %v", interfaces.ErrRateLimited, err)
	case containsAny(msg, "500", "502", "503", "504", "bad gateway", "gateway timeout", "service unavailable"):
		return fmt.Errorf("%w: %v", interfaces.ErrTransient, err)
	case containsAny(msg, "401", "unauthorized", "unable to authenticate"):
		return fmt.Errorf("%w: %v", interfaces.ErrUnauthorized, err)
	case containsAny(msg, "403", "forbidden"):
		return fmt.Errorf("%w: %v", interfaces.ErrForbidden, err)
	case containsAny(msg, "400", "422", "validation", "invalid"):
		return fmt.Errorf("%w: %v", interfaces.ErrValidation, err)
	}
	return err
}

// containsAny reports whether s contains any of the provided substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// deployRetryDelays controls the per-attempt sleep for retryOnTransient.
// The first entry is always 0 (no delay before the first attempt); subsequent
// entries are the delays before each retry. Overriding this var in tests
// prevents real sleeping. Total attempts = len(deployRetryDelays).
var deployRetryDelays = []time.Duration{0, time.Second, 2 * time.Second, 4 * time.Second}

// retryOnTransient calls op repeatedly, sleeping deployRetryDelays[i] before
// attempt i. Returns nil on the first success. Returns immediately (without
// retry) if the error is not ErrRateLimited or ErrTransient. Returns a
// "exhausted retries" error wrapping the last error when all attempts fail.
func retryOnTransient(ctx context.Context, op func() error) error {
	var lastErr error
	for i, d := range deployRetryDelays {
		if d > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
			}
		}
		lastErr = op()
		if lastErr == nil {
			return nil
		}
		if !errors.Is(lastErr, interfaces.ErrRateLimited) && !errors.Is(lastErr, interfaces.ErrTransient) {
			return lastErr // non-retryable: surface immediately
		}
		log.Printf("plugin deploy: retry %d/%d (after %v): %v", i+1, len(deployRetryDelays)-1, d, lastErr)
	}
	return fmt.Errorf("exhausted retries: %w", lastErr)
}

// deployOpError annotates an operation error with context and, for auth/validation
// failures, an actionable hint so operators know what to fix.
func deployOpError(resourceName, op string, err error) error {
	switch {
	case errors.Is(err, interfaces.ErrUnauthorized) || errors.Is(err, interfaces.ErrForbidden):
		return fmt.Errorf("plugin deploy %q: %s: auth failed — check DIGITALOCEAN_TOKEN permissions: %w", resourceName, op, err)
	case errors.Is(err, interfaces.ErrValidation):
		return fmt.Errorf("plugin deploy %q: %s: validation error: %w", resourceName, op, err)
	default:
		return fmt.Errorf("plugin deploy %q: %s failed: %w", resourceName, op, err)
	}
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
		return nil, wrapIaCError(err)
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
		return nil, wrapIaCError(err)
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
		return nil, wrapIaCError(err)
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
	return wrapIaCError(err)
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
		return nil, wrapIaCError(err)
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
		return nil, wrapIaCError(err)
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
		return nil, wrapIaCError(err)
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

// Troubleshoot calls the plugin's optional Troubleshooter.Troubleshoot.
// Returns (nil, nil) silently when the plugin returns Unimplemented so
// the caller doesn't need to probe for capability — absence is a valid answer.
func (d *remoteResourceDriver) Troubleshoot(ctx context.Context, ref interfaces.ResourceRef, failureMsg string) ([]interfaces.Diagnostic, error) {
	// Pass ref as flat primitives — structpb.NewStruct (the gRPC transport)
	// cannot encode arbitrary Go structs; each field must be a scalar.
	res, err := d.invoker.InvokeService("ResourceDriver.Troubleshoot", map[string]any{
		"resource_type":   d.resourceType,
		"ref_name":        ref.Name,
		"ref_provider_id": ref.ProviderID,
		"ref_type":        ref.Type,
		"failure_msg":     failureMsg,
	})
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.Unimplemented {
			return nil, nil
		}
		return nil, fmt.Errorf("resource driver Troubleshoot: %w", err)
	}
	raw, _ := res["diagnostics"].([]any)
	out := make([]interfaces.Diagnostic, 0, len(raw))
	for _, r := range raw {
		m, _ := r.(map[string]any)
		diag := interfaces.Diagnostic{
			ID:     stringVal(m, "id"),
			Phase:  stringVal(m, "phase"),
			Cause:  stringVal(m, "cause"),
			Detail: stringVal(m, "detail"),
		}
		if s := stringVal(m, "at"); s != "" {
			if t, perr := time.Parse(time.RFC3339, s); perr == nil {
				diag.At = t
			}
		}
		out = append(out, diag)
	}
	return out, nil
}

// stringVal returns a string field from a map or "" if missing/wrong type.
func stringVal(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func stringFromMap(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// closerFunc adapts a func() error to io.Closer.
type closerFunc func() error

func (f closerFunc) Close() error { return f() }

// newPluginDeployProvider looks up a matching iac.provider + infra.container_service
// module pair in wfCfg and wraps them as a DeployProvider. envName selects the
// per-environment config overlay via ModuleConfig.ResolveForEnv; pass "" to use
// top-level config only (modules marked deleted for the env are skipped).
func newPluginDeployProvider(providerName string, wfCfg *config.WorkflowConfig, envName string) (DeployProvider, error) {
	const hint = "\n  Example:\n    modules:\n    - name: my-provider\n      type: iac.provider\n      config:\n        provider: %s\n        credentials: env"
	if wfCfg == nil || len(wfCfg.Modules) == 0 {
		return nil, fmt.Errorf("unsupported deploy provider %q (built-ins: kubernetes, docker, aws-ecs; to use a plugin provider, declare an iac.provider module in your workflow config)%s", providerName, fmt.Sprintf(hint, providerName))
	}

	// resolveModule returns the effective ResolvedModule for m after applying the
	// per-env overlay (when envName is set). ok=false means the module is
	// explicitly deleted for this env and should be skipped.
	// Callers must read resolved.Name (not m.Name) to get the env-overridden identity.
	resolveModule := func(m *config.ModuleConfig) (*config.ResolvedModule, bool) {
		if envName == "" {
			return &config.ResolvedModule{
				Name:   m.Name,
				Type:   m.Type,
				Config: m.Config,
			}, true
		}
		return m.ResolveForEnv(envName)
	}

	// Find the iac.provider module matching the requested provider name.
	var providerModName string
	var providerModCfg map[string]any
	for i := range wfCfg.Modules {
		m := &wfCfg.Modules[i]
		if m.Type != "iac.provider" {
			continue
		}
		resolved, ok := resolveModule(m)
		if !ok {
			continue
		}
		cfgProvider, _ := resolved.Config["provider"].(string)
		if cfgProvider == providerName || resolved.Name == providerName {
			providerModName = resolved.Name
			providerModCfg = resolved.Config
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
		for i := range wfCfg.Modules {
			m := &wfCfg.Modules[i]
			if m.Type != target {
				continue
			}
			resolved, ok := resolveModule(m)
			if !ok {
				continue
			}
			if p, _ := resolved.Config["provider"].(string); p == providerModName {
				resourceName = resolved.Name
				resourceType = resolved.Type
				resourceCfg = resolved.Config
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
		for i := range wfCfg.Modules {
			m := &wfCfg.Modules[i]
			if m.Type == "iac.provider" || m.Type == "" {
				continue
			}
			resolved, ok := resolveModule(m)
			if !ok {
				continue
			}
			if p, _ := resolved.Config["provider"].(string); p == providerModName {
				fmt.Fprintf(os.Stderr, "warning: no deploy-target module (%v) found for provider %q; falling back to first infra module %q (type %q)\n",
					deployTargetTypes, providerModName, resolved.Name, resolved.Type)
				resourceName = resolved.Name
				resourceType = resolved.Type
				resourceCfg = resolved.Config
				break
			}
		}
	}
	if resourceName == "" {
		return nil, fmt.Errorf("no infra resource module found for provider %q in workflow config", providerModName)
	}

	// Expand env-var references in both configs after the env-config merge.
	// This resolves ${TOKEN} / $TOKEN placeholders written into the YAML.
	// Expansion happens here — at construction time — so the resolved values
	// are always used downstream, regardless of which method accesses them.
	//
	// Order matters: ResolveForEnv (above) merges per-env config into the map
	// first, so ${VAR} refs introduced by per-env overlays are expanded here.
	//
	// Secrets flow: if the caller has already injected secrets via os.Setenv
	// (e.g. env-provider secrets), ExpandEnvInMap picks them up here. Secrets
	// that come from vault / other stores and are carried in DeployConfig.Secrets
	// are NOT yet available at this point; those are applied in Deploy() just
	// before the final config is sent to the resource driver.
	providerModCfg = config.ExpandEnvInMap(providerModCfg)
	resourceCfg = config.ExpandEnvInMap(resourceCfg)

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
	// lastProviderID holds the ProviderID returned by the most recent successful
	// Deploy call (from either Update or Create). It is passed to HealthCheck so
	// the driver can locate the exact cloud resource rather than a blank ID.
	lastProviderID string
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
	if cfg.ImageTag != "" {
		merged["image"] = cfg.ImageTag
	}
	// else: preserve spec.Config["image"] from the (already-substituted) module config

	// Secrets carried in DeployConfig (fetched from vault / external stores by
	// injectSecrets) are not in the OS environment. Export them temporarily so
	// that ExpandEnvInMap can resolve any ${SECRET_NAME} references that were
	// not already substituted at construction time. Each secret is restored to
	// its previous value (or unset) after expansion to avoid leaking values into
	// other goroutines or child processes.
	type envSnapshot struct {
		key        string
		prev       string
		wasDefined bool
	}
	snapshots := make([]envSnapshot, 0, len(cfg.Secrets))
	for k, v := range cfg.Secrets {
		prev, had := os.LookupEnv(k)
		os.Setenv(k, v) //nolint:errcheck
		snapshots = append(snapshots, envSnapshot{key: k, prev: prev, wasDefined: had})
	}
	defer func() {
		for _, s := range snapshots {
			if s.wasDefined {
				os.Setenv(s.key, s.prev) //nolint:errcheck
			} else {
				os.Unsetenv(s.key) //nolint:errcheck
			}
		}
	}()
	merged = config.ExpandEnvInMap(merged)
	if img, _ := merged["image"].(string); img == "" {
		return fmt.Errorf("plugin deploy %q: image is empty — set IMAGE_TAG or configure image in YAML", p.resourceName)
	}
	imageStr, _ := merged["image"].(string)
	ref := interfaces.ResourceRef{Name: p.resourceName, Type: p.resourceType}
	spec := interfaces.ResourceSpec{Name: p.resourceName, Type: p.resourceType, Config: merged}

	// Read-by-name first: discover the existing ProviderID (if any) so Update
	// can target the exact cloud resource rather than a blank ID.
	var readOut *interfaces.ResourceOutput
	readErr := retryOnTransient(ctx, func() error {
		var err error
		readOut, err = driver.Read(ctx, ref)
		return err
	})
	switch {
	case readErr == nil && readOut != nil && readOut.ProviderID != "":
		ref.ProviderID = readOut.ProviderID
		log.Printf("plugin deploy %q: found existing resource (id=%s)", p.resourceName, ref.ProviderID)
	case readErr != nil && errors.Is(readErr, interfaces.ErrResourceNotFound):
		// Resource confirmed absent — skip Update, go straight to Create.
		return p.doCreate(ctx, driver, ref, spec, imageStr)
	case readErr != nil:
		return deployOpError(p.resourceName, "read", readErr)
	}

	// Belt-and-suspenders: Update first; fall back to Create on not-found.
	var out *interfaces.ResourceOutput
	updateErr := retryOnTransient(ctx, func() error {
		var err error
		out, err = driver.Update(ctx, ref, spec)
		return err
	})
	if updateErr == nil {
		p.lastProviderID = out.ProviderID
		fmt.Printf("  plugin deploy: updated %q at %s (id=%s)\n", p.resourceName, imageStr, out.ProviderID)
		return nil
	}
	if !errors.Is(updateErr, interfaces.ErrResourceNotFound) {
		return deployOpError(p.resourceName, "update", updateErr)
	}
	// Resource does not exist yet — fall back to Create.
	return p.doCreate(ctx, driver, ref, spec, imageStr)
}

// doCreate calls driver.Create with retry. On ErrResourceAlreadyExists (a race
// where another process created the resource between our Read and Create), it
// re-reads by name to discover the ProviderID and falls back to Update.
func (p *pluginDeployProvider) doCreate(ctx context.Context, driver interfaces.ResourceDriver, ref interfaces.ResourceRef, spec interfaces.ResourceSpec, imageStr string) error {
	log.Printf("plugin deploy %q: resource not found, creating new", p.resourceName)
	var out *interfaces.ResourceOutput
	createErr := retryOnTransient(ctx, func() error {
		var err error
		out, err = driver.Create(ctx, spec)
		return err
	})
	if createErr == nil {
		p.lastProviderID = out.ProviderID
		fmt.Printf("  plugin deploy: created %q at %s (id=%s)\n", p.resourceName, imageStr, out.ProviderID)
		return nil
	}
	if !errors.Is(createErr, interfaces.ErrResourceAlreadyExists) {
		return deployOpError(p.resourceName, "create", createErr)
	}

	// Race condition: re-read by name to discover the ProviderID, then Update.
	log.Printf("plugin deploy %q: create returned already-exists, re-reading to discover ProviderID", p.resourceName)
	var raceOut *interfaces.ResourceOutput
	if raceReadErr := retryOnTransient(ctx, func() error {
		var err error
		raceOut, err = driver.Read(ctx, ref)
		return err
	}); raceReadErr != nil {
		return fmt.Errorf("plugin deploy %q: create raced (already-exists), re-read failed: %w", p.resourceName, raceReadErr)
	}
	if raceOut != nil && raceOut.ProviderID != "" {
		ref.ProviderID = raceOut.ProviderID
	}
	var updateOut *interfaces.ResourceOutput
	if updateErr := retryOnTransient(ctx, func() error {
		var err error
		updateOut, err = driver.Update(ctx, ref, spec)
		return err
	}); updateErr != nil {
		return deployOpError(p.resourceName, "post-already-exists update", updateErr)
	}
	p.lastProviderID = updateOut.ProviderID
	fmt.Printf("  plugin deploy: updated %q at %s (id=%s) [post-conflict]\n", p.resourceName, imageStr, updateOut.ProviderID)
	return nil
}

// healthPollInitialInterval is the poll interval for the first healthPollBackoffAfter of waiting.
var healthPollInitialInterval = 10 * time.Second

// healthPollBackoffInterval is the poll interval after healthPollBackoffAfter has elapsed.
var healthPollBackoffInterval = 30 * time.Second

// healthPollBackoffAfter is the duration after which the poll interval switches to healthPollBackoffInterval.
var healthPollBackoffAfter = 60 * time.Second

// healthPollDefaultTimeout is the default maximum time to wait for a healthy result.
var healthPollDefaultTimeout = 10 * time.Minute

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
	if p.lastProviderID == "" {
		return fmt.Errorf("health check: no ProviderID available — Deploy must run first")
	}
	ref := interfaces.ResourceRef{Name: p.resourceName, Type: p.resourceType, ProviderID: p.lastProviderID}
	if err := pollUntilHealthy(ctx, driver, ref, p.resourceName, cfg.EnvName); err != nil {
		return err
	}
	return nil
}

// healthPollProgressInterval is how often a "still waiting" heartbeat is printed
// when no new status message has arrived.
var healthPollProgressInterval = 30 * time.Second

// pollUntilHealthy polls driver.HealthCheck until Healthy=true, context cancels,
// or the default timeout elapses.  ErrTransient/ErrRateLimited are treated as
// "keep polling"; any other error fails fast.
//
// Progress lines are emitted on every status change and at least every
// healthPollProgressInterval so the user can see the deploy is still running.
// On timeout, if the driver implements interfaces.Troubleshooter, recent
// provider-side events are fetched and printed in a structured failure block.
// envName threads through to healthPollTimeout for step-summary labeling.
func pollUntilHealthy(ctx context.Context, driver interfaces.ResourceDriver, ref interfaces.ResourceRef, name, envName string) error {
	deadline := time.Now().Add(healthPollDefaultTimeout)
	pollCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	start := time.Now()
	var lastMsg string
	lastProgress := start // initialise to start so the first heartbeat fires after healthPollProgressInterval

	fmt.Printf("  → health poll: waiting for %q to become healthy (timeout: %s)\n", name, healthPollDefaultTimeout)

	emitProgress := func(msg string) {
		elapsed := time.Since(start).Round(time.Second)
		ts := time.Now().Format("15:04:05")
		if msg != "" {
			fmt.Printf("  [%s] health poll %q: %s (%s elapsed)\n", ts, name, msg, elapsed)
		} else {
			fmt.Printf("  [%s] health poll %q: still waiting (%s elapsed)\n", ts, name, elapsed)
		}
		lastProgress = time.Now()
	}

	for {
		result, hcErr := driver.HealthCheck(pollCtx, ref)
		switch {
		case hcErr != nil:
			wrapped := wrapIaCError(hcErr)
			if !errors.Is(wrapped, interfaces.ErrTransient) && !errors.Is(wrapped, interfaces.ErrRateLimited) {
				return fmt.Errorf("plugin health check %q: %w", name, wrapped)
			}
			log.Printf("plugin health check %q: transient error, continuing poll: %v", name, hcErr)
		case result.Healthy:
			elapsed := time.Since(start).Round(time.Second)
			fmt.Printf("  [%s] health poll %q: ✓ healthy (%s)\n", time.Now().Format("15:04:05"), name, elapsed)
			return nil
		default:
			if result.Message != lastMsg {
				lastMsg = result.Message
				emitProgress(lastMsg)
			}
		}

		// Choose poll interval based on elapsed time.
		interval := healthPollInitialInterval
		if time.Since(start) >= healthPollBackoffAfter {
			interval = healthPollBackoffInterval
		}

		select {
		case <-pollCtx.Done():
			// Distinguish parent-cancel (Ctrl-C / pipeline abort) from our own deadline.
			if errors.Is(pollCtx.Err(), context.Canceled) {
				return fmt.Errorf("plugin health check %q: cancelled", name)
			}
			return healthPollTimeout(ctx, driver, ref, name, lastMsg, start, envName)
		case <-time.After(interval):
		}

		// Check again after sleeping (context may have expired during sleep).
		if pollCtx.Err() != nil {
			if errors.Is(pollCtx.Err(), context.Canceled) {
				return fmt.Errorf("plugin health check %q: cancelled", name)
			}
			return healthPollTimeout(ctx, driver, ref, name, lastMsg, start, envName)
		}

		// Emit a heartbeat if nothing has been logged recently.
		if time.Since(lastProgress) >= healthPollProgressInterval {
			emitProgress(lastMsg)
		}
	}
}

// healthPollTimeout builds the timeout error, emits a structured failure block,
// auto-troubleshoots via the driver's Troubleshooter (if any), and writes a GHA
// step summary via WriteStepSummary (no-op on non-GHA runners) before returning.
func healthPollTimeout(ctx context.Context, driver interfaces.ResourceDriver, ref interfaces.ResourceRef, name, lastMsg string, start time.Time, envName string) error {
	elapsed := time.Since(start).Round(time.Second)

	// Keep the returned error text identical to the pre-v0.18.10 format so
	// grep-based CI parsers are not broken (observability is additive).
	baseErr := fmt.Sprintf("plugin health check %q: timed out waiting for healthy", name)
	if lastMsg != "" {
		baseErr = fmt.Sprintf("%s; last status: %s", baseErr, lastMsg)
	}

	// Print structured failure block (elapsed only in the human-readable output).
	fmt.Fprintf(os.Stderr, "\n❌ Deploy health check timed out for %q after %s\n", name, elapsed)
	if lastMsg != "" {
		fmt.Fprintf(os.Stderr, "   Last observed status: %s\n", lastMsg)
	}
	fmt.Fprintln(os.Stderr)

	rootCause := lastMsg
	if rootCause == "" {
		rootCause = "deploy timed out"
	}
	em := detectCIProvider()
	diags := troubleshootAfterFailure(ctx, os.Stderr, driver, ref, errors.New(baseErr), 30*time.Second, em)
	if sumErr := WriteStepSummary(em, SummaryInput{
		Operation:   "deploy",
		Env:         envName,
		Resource:    name,
		Outcome:     "FAILED",
		RootCause:   rootCause,
		Diagnostics: diags,
	}); sumErr != nil {
		log.Printf("step summary: %v (ignored)", sumErr)
	}

	return errors.New(baseErr)
}

// emitDiagnostics renders diagnostics into a CI group block on w.
// No-op when diags is empty.
func emitDiagnostics(w io.Writer, resource string, diags []interfaces.Diagnostic, em CIGroupEmitter) {
	if len(diags) == 0 {
		return
	}
	em.GroupStart(w, fmt.Sprintf("Troubleshoot: %s", resource))
	for _, d := range diags {
		fmt.Fprintf(w, "  [%s] %s — %s (at %s)\n", d.Phase, d.ID, d.Cause, d.At.Format(time.RFC3339))
		if d.Detail != "" {
			for _, line := range strings.Split(strings.TrimRight(d.Detail, "\n"), "\n") {
				fmt.Fprintf(w, "    %s\n", line)
			}
		}
	}
	em.GroupEnd(w)
}

// troubleshootAfterFailure probes driver for Troubleshooter, calls it with a bounded
// timeout, and renders diagnostics via the provided emitter. All errors are swallowed —
// observability is additive; it never masks the original failure.
// Returns the collected diagnostics so callers can include them in a step summary.
func troubleshootAfterFailure(ctx context.Context, w io.Writer, driver interface{}, ref interfaces.ResourceRef, origErr error, timeout time.Duration, em CIGroupEmitter) []interfaces.Diagnostic {
	ts, ok := driver.(interfaces.Troubleshooter)
	if !ok {
		return nil
	}
	tsCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	diags, err := ts.Troubleshoot(tsCtx, ref, origErr.Error())
	if err != nil {
		log.Printf("troubleshoot: %v (ignored)", err)
		return nil
	}
	emitDiagnostics(w, ref.Name, diags, em)
	return diags
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
