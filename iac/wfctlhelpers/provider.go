package wfctlhelpers

import (
	"context"
	"fmt"
	"io"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// IaCProviderResolverFunc loads a live interfaces.IaCProvider from a
// provider type identifier (e.g. "digitalocean", "aws", "stub") and the
// expanded module config. Implementations typically scan a plugin
// directory, spawn a subprocess, build the typed gRPC adapter, and
// enforce CapabilitiesResponse.compute_plan_version == "v2".
//
// The returned io.Closer (when non-nil) MUST be closed by the caller to
// shut down the plugin subprocess.
type IaCProviderResolverFunc func(ctx context.Context, providerType string, cfg map[string]any) (interfaces.IaCProvider, io.Closer, error)

// Resolver is the package-level seam used by LoadIaCProviderFromConfig
// (and LoadAllIaCProvidersFromConfig in Task 3) to spawn a live IaC
// provider plugin. Production callers register their loader via an
// init() in cmd/wfctl/provider_resolver_init.go (registers
// discoverAndLoadIaCProvider); tests substitute fakes with t.Cleanup
// restore. Per docs/plans/2026-05-27-infra-admin-dynamic.md Task 2.
//
// Production callers other than cmd/wfctl's init() MUST NOT mutate
// this var; tests substitute fakes with t.Cleanup restore. NOT
// goroutine-safe — mirrors the T1 loadPluginStateBackendClients seam
// precedent. Code-reviewer M-1 on commit 63129d65f flagged the export
// surface; the godoc tightening here keeps the contract explicit
// without adding a setter that would diverge from T1's pattern.
//
// The cmd/wfctl loader (discoverAndLoadIaCProvider) is ~2800 lines of
// plugin-manager + typed-adapter machinery (deploy_providers.go +
// iac_typed_adapter.go). Lifting that wholesale into wfctlhelpers was
// out of scope for Task 2; this seam decouples the loader from this
// package without requiring the move. The host-side infra.admin module
// (T15) resolves providers via app.GetService(<module>) per the
// modular DI graph rather than calling this function, so the seam
// principally serves wfctl's CLI codepaths.
var Resolver IaCProviderResolverFunc = UnregisteredResolver

// UnregisteredResolver is the safe default for the Resolver seam: it
// returns a clear error message naming the missing init-registration so
// operators can diagnose a missing wiring without a nil-func panic.
// Exposed so tests can restore the default after swapping Resolver.
var UnregisteredResolver IaCProviderResolverFunc = func(_ context.Context, providerType string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
	return nil, nil, fmt.Errorf("wfctlhelpers: no IaCProviderResolver registered for provider type %q — cmd/wfctl init() should assign wfctlhelpers.Resolver = <loader>", providerType)
}

// LoadIaCProviderFromConfig finds the first iac.provider module in
// cfgFile and resolves it via the registered Resolver. Returns
// (nil, nil, nil) — NOT an error — when no iac.provider module is
// declared, so callers can treat "provider not available" as a
// reportable-but-non-fatal condition. The returned io.Closer (when
// non-nil) MUST be closed by the caller.
//
// Lifted from cmd/wfctl/infra_bootstrap.go:loadIaCProviderFromConfig
// per docs/plans/2026-05-27-infra-admin-dynamic.md Task 2 so the
// in-tree wfctl bootstrap path and the upcoming infra.admin CLI
// subcommands (T19-T20) share one definition.
func LoadIaCProviderFromConfig(ctx context.Context, cfgFile string) (interfaces.IaCProvider, io.Closer, error) {
	rawCfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}
	for i := range rawCfg.Modules {
		mod := &rawCfg.Modules[i]
		if mod.Type != "iac.provider" {
			continue
		}
		prov, closer, ok, err := loadProviderModule(ctx, mod)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			continue
		}
		return prov, closer, nil
	}
	return nil, nil, nil // no iac.provider module in config
}

// LoadAllIaCProvidersFromConfig finds EVERY iac.provider module in
// cfgFile and resolves each one, returning them as a map keyed by
// module name (so the handler library + ListProviders response can
// attribute each Provider record to its declared module). The
// caller-returned []io.Closer carries one entry per resolved provider
// in declaration order; closing them releases the underlying plugin
// subprocesses.
//
// Per design doc cycle-4 Important #6 (resolved by plan §Task 3):
// LoadIaCProviderFromConfig is first-match-only, which is correct for
// the wfctl single-cloud bootstrap path but insufficient for the
// admin-UI handler library that lists all configured providers.
//
// On resolver failure for any provider, the helper closes every
// previously-resolved provider (best-effort) and returns
// (nil, nil, error) so callers cannot accidentally leak subprocesses
// they have no handle to release. iac.provider modules missing a
// `provider:` field are silently skipped (consistent with
// LoadIaCProviderFromConfig's single-module behavior).
//
// Invariant: cfg.Modules has unique Names — enforced upstream by
// config.LoadFromFile. If two iac.provider modules ever shared a name
// (config-validation bug), the later one would silently overwrite the
// earlier in the map while the earlier's closer still gets released by
// the caller; per code-reviewer T3 M-1 (commit 9dff95246) this is
// acceptable today but worth documenting so future readers know the
// uniqueness assumption is load-bearing.
func LoadAllIaCProvidersFromConfig(ctx context.Context, cfgFile string) (map[string]interfaces.IaCProvider, []io.Closer, error) {
	rawCfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}
	providers := map[string]interfaces.IaCProvider{}
	var closers []io.Closer
	for i := range rawCfg.Modules {
		mod := &rawCfg.Modules[i]
		if mod.Type != "iac.provider" {
			continue
		}
		prov, closer, ok, err := loadProviderModule(ctx, mod)
		if err != nil {
			// Roll back: close every successfully-resolved provider so the
			// caller does not leak subprocesses it has no handle to release.
			// Close errors during rollback are intentionally discarded — the
			// primary error from Resolver takes precedence; surfacing a
			// cleanup error would mask the root cause. Per code-reviewer T3
			// M-3 (commit 9dff95246).
			for _, c := range closers {
				_ = c.Close()
			}
			return nil, nil, err
		}
		if !ok {
			continue
		}
		providers[mod.Name] = prov
		if closer != nil {
			closers = append(closers, closer)
		}
	}
	return providers, closers, nil
}

// loadProviderModule resolves a single iac.provider ModuleConfig via
// the registered Resolver. Returns (provider, closer, true, nil) on
// success, (nil, nil, false, nil) when the module lacks a
// `provider:` field (caller skips it), and (nil, nil, false, err) on
// resolver failure. Factored out of LoadIaCProviderFromConfig +
// LoadAllIaCProvidersFromConfig so the body cannot drift between the
// two callsites.
func loadProviderModule(ctx context.Context, mod *config.ModuleConfig) (interfaces.IaCProvider, io.Closer, bool, error) {
	modCfg := config.ExpandEnvInMap(mod.Config)
	pt, ok := modCfg["provider"].(string)
	if !ok || pt == "" {
		return nil, nil, false, nil
	}
	prov, closer, err := Resolver(ctx, pt, modCfg)
	if err != nil {
		return nil, nil, false, fmt.Errorf("load provider %q: %w", pt, err)
	}
	return prov, closer, true, nil
}
