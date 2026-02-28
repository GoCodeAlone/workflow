// Package secrets provides a plugin that registers secrets management modules:
// secrets.vault (HashiCorp Vault) and secrets.aws (AWS Secrets Manager),
// as well as the step.secret_rotate pipeline step type.
package secrets

import (
	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// Plugin registers secrets management module factories and step types.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new secrets management plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "secrets",
				PluginVersion:     "1.0.0",
				PluginDescription: "Secrets management modules (Vault, AWS Secrets Manager)",
			},
			Manifest: plugin.PluginManifest{
				Name:        "secrets",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "Secrets management modules (Vault, AWS Secrets Manager)",
				Tier:        plugin.TierCore,
				ModuleTypes: []string{"secrets.vault", "secrets.aws"},
				StepTypes:   []string{"step.secret_rotate"},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "secrets-management", Role: "provider", Priority: 50},
				},
			},
		},
	}
}

// Capabilities returns the capability contracts defined by this plugin.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "secrets-management",
			Description: "Secret retrieval from external vaults and key management services",
		},
	}
}

// ModuleFactories returns module factories for secrets providers.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"secrets.vault": func(name string, config map[string]any) modular.Module {
			vm := module.NewSecretsVaultModule(name)
			if mode, ok := config["mode"].(string); ok && mode != "" {
				vm.SetMode(mode)
			}
			if addr, ok := config["address"].(string); ok {
				vm.SetAddress(addr)
			}
			if token, ok := config["token"].(string); ok {
				vm.SetToken(token)
			}
			if mp, ok := config["mountPath"].(string); ok && mp != "" {
				vm.SetMountPath(mp)
			}
			if ns, ok := config["namespace"].(string); ok && ns != "" {
				vm.SetNamespace(ns)
			}
			return vm
		},
		"secrets.aws": func(name string, config map[string]any) modular.Module {
			am := module.NewSecretsAWSModule(name)
			if region, ok := config["region"].(string); ok && region != "" {
				am.SetRegion(region)
			}
			if akid, ok := config["accessKeyId"].(string); ok {
				am.SetAccessKeyID(akid)
			}
			if sak, ok := config["secretAccessKey"].(string); ok {
				am.SetSecretAccessKey(sak)
			}
			return am
		},
	}
}

// StepFactories returns the step factories provided by this plugin.
func (p *Plugin) StepFactories() map[string]plugin.StepFactory {
	return map[string]plugin.StepFactory{
		"step.secret_rotate": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewSecretRotateStepFactory()(name, cfg, app)
		},
	}
}
