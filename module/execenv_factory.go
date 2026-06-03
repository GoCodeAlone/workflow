package module

import (
	"context"
	"fmt"
	"strings"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/sandbox"
	"github.com/GoCodeAlone/workflow/sandbox/remote"
	"github.com/GoCodeAlone/workflow/secrets"
)

// resolveSandboxRunner returns a SandboxRunner for the requested execution environment.
//
// Supported values for execEnv:
//   - "" or "local-docker" — local Docker daemon (default).
//   - any registered runner name — dispatches to the named RemoteRunner from the
//     sandbox.remote_runners registry (PR8). The registry is looked up from the
//     modular service registry via app. If the name is not registered, a clear
//     error is returned at runtime (step Execute time).
//
// Deferred values:
//   - "ephemeral" — ephemeral/cloud runner (PR9)
//
// Validation strategy: the step factory (pipeline_step_sandbox_exec.go) no longer
// rejects non-local-docker exec_env values at construction time — named runners are
// determined by config at runtime, not build time. Any unresolved name (not in the
// registry) returns an error at Execute time, which is the appropriate gate (same as
// other service-name references in the pipeline).
func resolveSandboxRunner(ctx context.Context, app modular.Application, execEnv string, cfg sandbox.SandboxConfig) (sandbox.SandboxRunner, error) {
	switch execEnv {
	case "", "local-docker":
		return sandbox.NewLocalDockerRunner(cfg)
	case "ephemeral":
		// TODO(PR9): wire ephemeral/cloud runner here.
		return nil, fmt.Errorf("sandbox_exec: exec_env %q not yet configured (deferred to PR9)", execEnv)
	default:
		// Treat execEnv as a named remote runner. Look it up in the service registry.
		return resolveNamedRemoteRunner(ctx, app, execEnv, cfg)
	}
}

// resolveNamedRemoteRunner looks up a RemoteRunnerSpec by name from the
// RemoteRunnerRegistry service, resolves the spec's bearer token through the
// configured secrets provider, builds a RemoteRunner from the spec, and returns
// it wired with the per-exec SandboxConfig (profile, image, env, workdir).
//
// The app parameter may be nil in unit tests that don't exercise the remote path;
// a nil app with a non-local execEnv returns a clear "no registry" error.
func resolveNamedRemoteRunner(ctx context.Context, app modular.Application, name string, cfg sandbox.SandboxConfig) (sandbox.SandboxRunner, error) {
	if app == nil {
		return nil, fmt.Errorf("sandbox_exec: exec_env %q not configured (no application context)", name)
	}

	var registry *RemoteRunnerRegistry
	if err := app.GetService(SandboxRemoteRunnerServiceName, &registry); err != nil || registry == nil {
		return nil, fmt.Errorf("sandbox_exec: exec_env %q not configured (no sandbox.remote_runners module)", name)
	}

	spec, ok := registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("sandbox_exec: exec_env %q not configured (no runner named %q in sandbox.remote_runners)", name, name)
	}

	// Resolve the bearer token. It may be a secret:// reference, in which case it
	// MUST be resolved to its literal value before being sent as the Bearer header.
	resolvedToken, err := resolveRunnerToken(ctx, app, registry.SecretsProvider(), spec.Token, name)
	if err != nil {
		return nil, err
	}

	runnerCfg := remote.RemoteRunnerConfig{
		Profile: cfg.GetProfile(),
		Image:   cfg.Image,
		Env:     cfg.Env,
		WorkDir: cfg.WorkDir,
	}

	runner, err := buildRemoteRunnerFromSpec(spec, resolvedToken, runnerCfg)
	if err != nil {
		return nil, fmt.Errorf("sandbox_exec: exec_env %q: build remote runner: %w", name, err)
	}
	return runner, nil
}

// resolveRunnerToken resolves a runner's bearer token. A literal (non-secret://)
// token passes through unchanged. A secret:// reference is resolved through the
// named secrets provider; if no provider is configured (providerName == ""), a
// secret:// token is a configuration error — we MUST NOT send the literal
// "secret://..." string as the Bearer header (the agent would reject it).
//
// The resolved token value is NEVER logged (it is a credential).
func resolveRunnerToken(ctx context.Context, app modular.Application, providerName, token, runnerName string) (string, error) {
	if token == "" {
		return "", nil
	}
	if !strings.HasPrefix(token, secrets.SecretPrefix) {
		// Literal token — pass through unchanged.
		return token, nil
	}
	if providerName == "" {
		return "", fmt.Errorf("sandbox_exec: exec_env %q: token is a %s reference but the sandbox.remote_runners module has no 'secrets_provider' configured to resolve it", runnerName, secrets.SecretPrefix)
	}

	provider, err := resolveSecretsProviderFromRegistry(app, providerName)
	if err != nil {
		return "", fmt.Errorf("sandbox_exec: exec_env %q: %w", runnerName, err)
	}

	resolved, err := secrets.NewResolver(provider).Resolve(ctx, token)
	if err != nil {
		// Do not echo the token value or the resolved secret into the error.
		return "", fmt.Errorf("sandbox_exec: exec_env %q: failed to resolve token secret reference", runnerName)
	}
	return resolved, nil
}

// resolveSecretsProviderFromRegistry resolves a secrets.Provider from the service
// registry by name. The service may implement secrets.Provider directly or expose
// a Provider() accessor (mirrors resolveSecretsProvider in
// pipeline_step_iac_secret_reachability.go).
func resolveSecretsProviderFromRegistry(app modular.Application, providerName string) (secrets.Provider, error) {
	svc, ok := app.SvcRegistry()[providerName]
	if !ok {
		return nil, fmt.Errorf("secrets provider %q not found in service registry", providerName)
	}
	if p, ok := svc.(secrets.Provider); ok {
		return p, nil
	}
	type providerAccessor interface {
		Provider() secrets.Provider
	}
	if acc, ok := svc.(providerAccessor); ok {
		p := acc.Provider()
		if p == nil {
			return nil, fmt.Errorf("secrets provider %q exposes Provider() accessor but returned nil; module may not be started", providerName)
		}
		return p, nil
	}
	return nil, fmt.Errorf("secrets provider %q does not implement secrets.Provider directly or via Provider() accessor", providerName)
}
