// Package gitlab provides an EnginePlugin that registers GitLab CI integration:
//   - Module types: gitlab.webhook, gitlab.client
//   - Step types: step.gitlab_trigger_pipeline, step.gitlab_pipeline_status,
//     step.gitlab_create_mr, step.gitlab_mr_comment, step.gitlab_parse_webhook
package gitlab

import (
	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// Plugin registers GitLab CI module types and pipeline step types.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new GitLab plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "gitlab",
				PluginVersion:     "1.0.0",
				PluginDescription: "GitLab CI integration: webhook receiver, API client, and pipeline steps",
			},
			Manifest: plugin.PluginManifest{
				Name:        "gitlab",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "GitLab CI integration: webhook receiver (gitlab.webhook), API client (gitlab.client), pipeline trigger/status steps, and MR management steps.",
				Tier:        plugin.TierCore,
				ModuleTypes: []string{
					"gitlab.webhook",
					"gitlab.client",
				},
				StepTypes: []string{
					"step.gitlab_trigger_pipeline",
					"step.gitlab_pipeline_status",
					"step.gitlab_create_mr",
					"step.gitlab_mr_comment",
					"step.gitlab_parse_webhook",
				},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "gitlab-ci", Role: "provider", Priority: 50},
				},
			},
		},
	}
}

// Capabilities returns the capability contracts defined by this plugin.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "gitlab-ci",
			Description: "GitLab CI integration: webhooks, pipeline triggers, pipeline status, merge request management",
		},
	}
}

// ModuleFactories returns factories for gitlab.webhook and gitlab.client module types.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"gitlab.webhook": func(name string, cfg map[string]any) modular.Module {
			return module.NewGitLabWebhookModule(name, cfg)
		},
		"gitlab.client": func(name string, cfg map[string]any) modular.Module {
			return module.NewGitLabClientModule(name, cfg)
		},
	}
}

// StepFactories returns factories for the GitLab pipeline step types.
func (p *Plugin) StepFactories() map[string]plugin.StepFactory {
	return map[string]plugin.StepFactory{
		"step.gitlab_trigger_pipeline": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewGitLabTriggerPipelineStepFactory()(name, cfg, app)
		},
		"step.gitlab_pipeline_status": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewGitLabPipelineStatusStepFactory()(name, cfg, app)
		},
		"step.gitlab_create_mr": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewGitLabCreateMRStepFactory()(name, cfg, app)
		},
		"step.gitlab_mr_comment": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewGitLabMRCommentStepFactory()(name, cfg, app)
		},
		"step.gitlab_parse_webhook": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewGitLabWebhookParseStepFactory()(name, cfg, app)
		},
	}
}
