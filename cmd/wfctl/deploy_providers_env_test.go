package main

import (
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestPluginDeployProvider_UsesEnvResolvedName(t *testing.T) {
	wfCfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name:   "do-provider",
				Type:   "iac.provider",
				Config: map[string]any{"provider": "digitalocean"},
			},
			{
				Name:   "bmw-app",
				Type:   "infra.container_service",
				Config: map[string]any{"provider": "do-provider"},
				Environments: map[string]*config.InfraEnvironmentResolution{
					"staging": {
						Config: map[string]any{"name": "bmw-staging"},
					},
				},
			},
		},
	}

	dp, err := newPluginDeployProvider("digitalocean", wfCfg, "staging")
	if err != nil {
		t.Fatalf("newPluginDeployProvider: %v", err)
	}

	pdp, ok := dp.(*pluginDeployProvider)
	if !ok {
		t.Fatalf("expected *pluginDeployProvider, got %T", dp)
	}
	if pdp.resourceName != "bmw-staging" {
		t.Errorf("resourceName = %q, want %q (env-resolved name)", pdp.resourceName, "bmw-staging")
	}
}

func TestPluginDeployProvider_FallsBackToModuleNameWhenNoEnv(t *testing.T) {
	wfCfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name:   "do-provider",
				Type:   "iac.provider",
				Config: map[string]any{"provider": "digitalocean"},
			},
			{
				Name:   "bmw-app",
				Type:   "infra.container_service",
				Config: map[string]any{"provider": "do-provider"},
				// NOTE: no Environments block — base name should be used
			},
		},
	}

	dp, err := newPluginDeployProvider("digitalocean", wfCfg, "")
	if err != nil {
		t.Fatalf("newPluginDeployProvider: %v", err)
	}

	pdp, ok := dp.(*pluginDeployProvider)
	if !ok {
		t.Fatalf("expected *pluginDeployProvider, got %T", dp)
	}
	if pdp.resourceName != "bmw-app" {
		t.Errorf("resourceName = %q, want %q (base module name when no env)", pdp.resourceName, "bmw-app")
	}
}
