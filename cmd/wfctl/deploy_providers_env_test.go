package main

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
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

func TestPluginDeployProvider_ResourceNameMatchesInfraPlanForEnv(t *testing.T) {
	yaml := `
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: digitalocean
  - name: bmw-app
    type: infra.container_service
    config:
      provider: do-provider
      image: registry.example.com/bmw:latest
      http_port: 8080
    environments:
      staging:
        config:
          name: bmw-staging
          image: registry.example.com/bmw:staging
      prod:
        config:
          name: buymywishlist
          image: registry.example.com/bmw:prod
`
	path, err := writeTempYAML(t, yaml)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)

	cfg, err := config.LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	planSpecs, err := parseInfraResourceSpecsForEnv(path, "staging")
	if err != nil {
		t.Fatalf("parseInfraResourceSpecsForEnv: %v", err)
	}
	var plannedName string
	for _, spec := range planSpecs {
		if spec.Type == "infra.container_service" {
			plannedName = spec.Name
			break
		}
	}
	if plannedName == "" {
		t.Fatal("plan did not include infra.container_service")
	}

	dp, err := newPluginDeployProvider("digitalocean", cfg, "staging")
	if err != nil {
		t.Fatalf("newPluginDeployProvider: %v", err)
	}
	pdp, ok := dp.(*pluginDeployProvider)
	if !ok {
		t.Fatalf("expected *pluginDeployProvider, got %T", dp)
	}

	if pdp.resourceName != plannedName {
		t.Fatalf("deploy resourceName = %q, want infra plan resource name %q", pdp.resourceName, plannedName)
	}
	if plannedName != "bmw-staging" {
		t.Fatalf("planned resource name = %q, want bmw-staging", plannedName)
	}
}

func TestRunDeployPhaseWithConfig_UsesEnvResolvedDeployResourceName(t *testing.T) {
	driver := &fakeResourceDriver{
		updateOut: &interfaces.ResourceOutput{ProviderID: "app-123"},
	}
	fake := &fakeIaCProvider{
		name:    "digitalocean",
		drivers: map[string]interfaces.ResourceDriver{"infra.container_service": driver},
	}

	orig := resolveIaCProvider
	defer func() { resolveIaCProvider = orig }()
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return fake, nil, nil
	}

	t.Setenv("IMAGE_TAG", "registry.example.com/bmw:staging")

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
	deploy := &config.CIDeployConfig{
		Environments: map[string]*config.CIDeployEnvironment{
			"staging": {
				Provider: "digitalocean",
				Strategy: "apply",
			},
		},
	}

	if err := runDeployPhaseWithConfig(deploy, "staging", wfCfg, nil, false); err != nil {
		t.Fatalf("runDeployPhaseWithConfig: %v", err)
	}
	if !driver.updateCalled {
		t.Fatal("expected deploy to update the env-resolved resource")
	}
	if driver.updateRef.Name != "bmw-staging" {
		t.Fatalf("driver.Update ref name = %q, want bmw-staging", driver.updateRef.Name)
	}
}
