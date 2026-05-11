package main

import "github.com/GoCodeAlone/workflow/plugin/external/sdk"

type provider struct{}

func (p *provider) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{
		Name:        "no-iac",
		Version:     "0.1.0",
		Author:      "workflow",
		Description: "Plugin fixture without typed IaC service",
	}
}

func main() {
	sdk.Serve(&provider{})
}
