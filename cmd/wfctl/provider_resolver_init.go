package main

import (
	"context"
	"io"

	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// init wires the wfctlhelpers.Resolver seam to cmd/wfctl's real plugin
// loader so wfctlhelpers.LoadIaCProviderFromConfig (and Task 3's
// LoadAllIaCProvidersFromConfig) produces a live typed adapter without
// having to lift discoverAndLoadIaCProvider + typedIaCAdapter +
// findIaCPluginDir + buildTypedIaCAdapterFrom + the
// CapabilitiesResponse=v2 gate (~2800 lines) into the shared helper
// package. Per docs/plans/2026-05-27-infra-admin-dynamic.md Task 2.
//
// The host-side infra.admin module (T15) does NOT call
// wfctlhelpers.LoadIaCProviderFromConfig — it resolves providers via
// app.GetService(<module>) per the modular DI graph. This seam is
// therefore registered only in cmd/wfctl; any future caller wanting to
// load providers from a config file outside the modular DI lifecycle
// (e.g. a standalone CLI extension) can register its own resolver.
//
// The wrapper does not introduce new logic — it delegates to the
// existing resolveIaCProvider package-level var (which itself defaults
// to discoverAndLoadIaCProvider; tests override via the same seam).
func init() {
	wfctlhelpers.Resolver = func(ctx context.Context, providerType string, cfg map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return resolveIaCProvider(ctx, providerType, cfg)
	}
}
