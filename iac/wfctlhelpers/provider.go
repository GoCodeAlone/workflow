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
		modCfg := config.ExpandEnvInMap(mod.Config)
		pt, ok := modCfg["provider"].(string)
		if !ok || pt == "" {
			continue
		}
		prov, closer, err := Resolver(ctx, pt, modCfg)
		if err != nil {
			return nil, nil, fmt.Errorf("load provider %q: %w", pt, err)
		}
		return prov, closer, nil
	}
	return nil, nil, nil // no iac.provider module in config
}
