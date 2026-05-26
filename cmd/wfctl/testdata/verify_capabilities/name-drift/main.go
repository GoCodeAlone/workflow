package main

import sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"

var Version = "dev"

type stubProvider struct{}

// Manifest intentionally returns a DIFFERENT name than plugin.json declares.
// plugin.json says "verify-name-drift"; runtime says "verify-name-drift-binary".
func (stubProvider) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{
		Name:        "verify-name-drift-binary",
		Version:     "0.0.0",
		Author:      "test fixture",
		Description: "verify-capabilities name-drift scenario",
	}
}

func main() {
	sdk.Serve(stubProvider{},
		sdk.WithBuildVersion(sdk.ResolveBuildVersion(Version)),
	)
}
