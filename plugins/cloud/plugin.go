// Package cloud provides an EnginePlugin that registers the cloud.account
// module type and the step.cloud_validate pipeline step.
// cloud.account is the foundation for Infrastructure-as-Config: other modules
// (platform.kubernetes, platform.ecs, etc.) look up CloudCredentialProvider
// from the service registry by account name.
package cloud

import (
	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// Plugin registers cloud.account and step.cloud_validate.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new cloud plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "cloud",
				PluginVersion:     "1.0.0",
				PluginDescription: "Cloud provider credentials (cloud.account) and validation step",
			},
			Manifest: plugin.PluginManifest{
				Name:        "cloud",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "Cloud provider credentials and validation. Foundation for IaC modules.",
				Tier:        plugin.TierCore,
				ModuleTypes: []string{"cloud.account"},
				StepTypes:   []string{"step.cloud_validate"},
			},
		},
	}
}

// ModuleFactories returns the cloud.account module factory.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"cloud.account": func(name string, cfg map[string]any) modular.Module {
			return module.NewCloudAccount(name, cfg)
		},
	}
}

// StepFactories returns the step.cloud_validate factory.
func (p *Plugin) StepFactories() map[string]plugin.StepFactory {
	return map[string]plugin.StepFactory{
		"step.cloud_validate": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewCloudValidateStepFactory()(name, cfg, app)
		},
	}
}

// ModuleSchemas returns UI schema definitions for cloud module types.
func (p *Plugin) ModuleSchemas() []*schema.ModuleSchema {
	return []*schema.ModuleSchema{
		{
			Type:        "cloud.account",
			Label:       "Cloud Account",
			Category:    "cloud",
			Description: "Cloud provider credentials (AWS, GCP, Azure, DigitalOcean, Kubernetes, or mock). Used by IaC modules.",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "provider", Label: "Provider", Type: schema.FieldTypeString, Required: true, Description: "Cloud provider: aws, gcp, azure, digitalocean, kubernetes, mock"},
				{Key: "region", Label: "Region", Type: schema.FieldTypeString, Description: "Primary region (e.g. us-east-1, us-central1, eastus, nyc3)"},
				{Key: "credentials", Label: "Credentials", Type: schema.FieldTypeJSON, Description: "Credential configuration (type, keys, paths). GCP types: service_account_json, service_account_key, workload_identity, application_default. Azure types: client_credentials, managed_identity, cli, env. DigitalOcean types: api_token, env."},
				{Key: "project_id", Label: "GCP Project ID", Type: schema.FieldTypeString, Description: "GCP project ID (google cloud project)"},
				{Key: "subscription_id", Label: "Azure Subscription ID", Type: schema.FieldTypeString, Description: "Azure subscription ID"},
			},
		},
	}
}
