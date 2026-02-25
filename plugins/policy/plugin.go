// Package policy provides an EnginePlugin that registers policy engine module
// types (policy.opa, policy.cedar, policy.mock) and policy pipeline step types
// (step.policy_evaluate, step.policy_load, step.policy_list, step.policy_test).
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
				PluginDescription: "External policy engine adapter supporting OPA (Open Policy Agent) and Cedar",
			},
			Manifest: plugin.PluginManifest{
				Name:        "policy",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "Policy engine adapter for OPA (Open Policy Agent) and Cedar authorization policies",
				Tier:        plugin.TierCore,
				ModuleTypes: []string{
					"policy.opa",
					"policy.cedar",
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
			Description: "Policy evaluation using OPA (Rego), Cedar, or mock backends for authorization decisions",
		},
	}
}

// ModuleFactories returns factories for policy.opa, policy.cedar, and policy.mock.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"policy.opa": func(name string, cfg map[string]any) modular.Module {
			if cfg == nil {
				cfg = map[string]any{}
			}
			cfg["backend"] = "opa"
			return module.NewPolicyEngineModule(name, cfg)
		},
		"policy.cedar": func(name string, cfg map[string]any) modular.Module {
			if cfg == nil {
				cfg = map[string]any{}
			}
			cfg["backend"] = "cedar"
			return module.NewPolicyEngineModule(name, cfg)
		},
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
			Type:        "policy.opa",
			Label:       "OPA Policy Engine",
			Category:    "security",
			Description: "Open Policy Agent (OPA) policy engine. Evaluates Rego policies via OPA REST API.",
			Inputs:      []schema.ServiceIODef{{Name: "input", Type: "PolicyInput", Description: "JSON input document for policy evaluation"}},
			Outputs:     []schema.ServiceIODef{{Name: "decision", Type: "PolicyDecision", Description: "Policy decision: allowed/denied with reasons"}},
			ConfigFields: append([]schema.ConfigFieldDef{
				{Key: "endpoint", Label: "OPA Endpoint", Type: schema.FieldTypeString, DefaultValue: "http://localhost:8181", Description: "OPA REST API endpoint URL", Placeholder: "http://localhost:8181"},
			}, sharedPolicyFields...),
			DefaultConfig: map[string]any{"endpoint": "http://localhost:8181"},
		},
		{
			Type:        "policy.cedar",
			Label:       "Cedar Policy Engine",
			Category:    "security",
			Description: "Cedar policy language engine for authorization. Uses the cedar-go library.",
			Inputs:      []schema.ServiceIODef{{Name: "input", Type: "PolicyInput", Description: "Cedar request: principal, action, resource, context"}},
			Outputs:     []schema.ServiceIODef{{Name: "decision", Type: "PolicyDecision", Description: "Policy decision: allowed/denied with reasons"}},
			ConfigFields: sharedPolicyFields,
		},
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
