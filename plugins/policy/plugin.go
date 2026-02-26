// Package policy provides an EnginePlugin that registers the policy.mock module
// type and policy pipeline step types (step.policy_evaluate, step.policy_load,
// step.policy_list, step.policy_test). For OPA or Cedar backends, use the
// workflow-plugin-policy-opa or workflow-plugin-policy-cedar external plugins.
package policy

import (
	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// Plugin registers policy engine modules and pipeline steps.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new policy plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "policy",
				PluginVersion:     "1.0.0",
				PluginDescription: "Policy engine plugin providing mock backend; OPA and Cedar via external plugins",
			},
			Manifest: plugin.PluginManifest{
				Name:        "policy",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "Policy engine plugin with mock backend for testing and development",
				Tier:        plugin.TierCore,
				ModuleTypes: []string{
					"policy.mock",
				},
				StepTypes: []string{
					"step.policy_evaluate",
					"step.policy_load",
					"step.policy_list",
					"step.policy_test",
				},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "policy-enforcement", Role: "provider", Priority: 20},
				},
			},
		},
	}
}

// Capabilities returns the capability contracts defined by this plugin.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "policy-enforcement",
			Description: "Policy evaluation using mock backend for testing and development",
		},
	}
}

// ModuleFactories returns factories for policy.mock.
// For OPA or Cedar backends, use the workflow-plugin-policy-opa or
// workflow-plugin-policy-cedar external plugins.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"policy.mock": func(name string, cfg map[string]any) modular.Module {
			if cfg == nil {
				cfg = map[string]any{}
			}
			cfg["backend"] = "mock"
			return module.NewPolicyEngineModule(name, cfg)
		},
	}
}

// StepFactories returns the policy step factories.
func (p *Plugin) StepFactories() map[string]plugin.StepFactory {
	return map[string]plugin.StepFactory{
		"step.policy_evaluate": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewPolicyEvaluateStepFactory()(name, cfg, app)
		},
		"step.policy_load": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewPolicyLoadStepFactory()(name, cfg, app)
		},
		"step.policy_list": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewPolicyListStepFactory()(name, cfg, app)
		},
		"step.policy_test": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewPolicyTestStepFactory()(name, cfg, app)
		},
	}
}

// ModuleSchemas returns UI schema definitions for policy module types.
func (p *Plugin) ModuleSchemas() []*schema.ModuleSchema {
	sharedPolicyFields := []schema.ConfigFieldDef{
		{Key: "policies", Label: "Pre-loaded Policies", Type: schema.FieldTypeJSON, Description: "List of policies to load at startup: [{\"name\": \"authz\", \"content\": \"...\"}]"},
	}

	return []*schema.ModuleSchema{
		{
			Type:         "policy.mock",
			Label:        "Mock Policy Engine",
			Category:     "security",
			Description:  "In-memory mock policy engine for testing. Denies if any loaded policy contains the word 'deny'.",
			Inputs:       []schema.ServiceIODef{{Name: "input", Type: "PolicyInput", Description: "Evaluation input"}},
			Outputs:      []schema.ServiceIODef{{Name: "decision", Type: "PolicyDecision", Description: "Policy decision"}},
			ConfigFields: sharedPolicyFields,
		},
	}
}
