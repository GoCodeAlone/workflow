package main

import sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"

// Version is ldflag-injected at build time. Initial "dev" so
// sdk.ResolveBuildVersion falls back to "(devel) [@ <sha>]" when no
// ldflag fires (exercises the missing-ldflag scenario faithfully).
var Version = "dev"

type stubProvider struct{}

func (stubProvider) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{
		Name:        "verify-release-good",
		Version:     "1.2.3",
		Author:      "test fixture",
		Description: "verify-capabilities release-good scenario",
	}
}

func main() {
	sdk.Serve(stubProvider{},
		sdk.WithBuildVersion(sdk.ResolveBuildVersion(Version)),
	)
}
