package workflow

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/iac/iactest"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/mock"
	"github.com/GoCodeAlone/workflow/plugin"
	pluginplatform "github.com/GoCodeAlone/workflow/plugins/platform"
)

type preInitProviderPlugin struct {
	plugin.BaseEnginePlugin
	provider interfaces.IaCProvider
}

func newPreInitProviderPlugin(provider interfaces.IaCProvider) *preInitProviderPlugin {
	return &preInitProviderPlugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "test-iac-provider-plugin",
				PluginVersion:     "1.0.0",
				PluginDescription: "test provider plugin",
			},
			Manifest: plugin.PluginManifest{
				Name:        "test-iac-provider-plugin",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "test provider plugin",
			},
		},
		provider: provider,
	}
}

func (p *preInitProviderPlugin) PreInitWiringHooks() []plugin.WiringHook {
	return []plugin.WiringHook{{
		Name:     "test-provider-preinit",
		Priority: 50,
		Hook: func(app modular.Application, _ *config.WorkflowConfig) error {
			return app.RegisterService("workflow-plugin-aws", p.provider)
		},
	}}
}

type engineRecordingIaCProvider struct {
	iactest.NoopProvider
	initCfg map[string]any
}

func (p *engineRecordingIaCProvider) Initialize(_ context.Context, cfg map[string]any) error {
	p.initCfg = cfg
	return nil
}

func TestEngineBuildFromConfig_RunsIaCProviderPreInitHookBeforeModuleInit(t *testing.T) {
	provider := &engineRecordingIaCProvider{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), &mock.Logger{})
	engine := NewStdEngine(app, &mock.Logger{})
	if err := engine.LoadPlugin(pluginplatform.New()); err != nil {
		t.Fatalf("LoadPlugin(platform): %v", err)
	}
	if err := engine.LoadPlugin(newPreInitProviderPlugin(provider)); err != nil {
		t.Fatalf("LoadPlugin(provider): %v", err)
	}

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{{
			Name: "aws-provider",
			Type: "iac.provider",
			Config: map[string]any{
				"plugin": "workflow-plugin-aws",
				"mode":   "mock",
				"region": "us-east-1",
			},
		}},
		Workflows: map[string]any{},
	}

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}

	var got interfaces.IaCProvider
	if err := app.GetService("aws-provider", &got); err != nil {
		t.Fatalf("GetService(aws-provider): %v", err)
	}
	if got != provider {
		t.Fatalf("provider alias = %T %p, want %p", got, got, provider)
	}
	if provider.initCfg["mode"] != "mock" || provider.initCfg["region"] != "us-east-1" {
		t.Fatalf("Initialize config = %#v", provider.initCfg)
	}
}
