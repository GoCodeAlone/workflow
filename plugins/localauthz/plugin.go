// Package localauthz provides an in-process authz.local EnginePlugin for use
// with the scenario_stub build tag. It registers a module that implements the
// same Enforcer interface as authz.casbin — exact-match, allow-effect,
// default-deny — without the Casbin dependency.
//
// Intended use: scenario 92 and integration tests that need a lightweight
// in-process RBAC enforcer. NOT a replacement for authz.casbin in production.
//
// Config shape (YAML):
//
//	type: authz.local
//	config:
//	  policies:
//	    - ["operator", "infra:read",    "allow"]
//	    - ["operator", "infra:apply",   "allow"]
//	    - ["operator", "infra:destroy", "allow"]
//	    - ["viewer",   "infra:read",    "allow"]
//
// Each policy is a [subject, object, action] triple. A request is allowed
// when it exactly matches at least one triple; all other requests are denied.
package localauthz

import (
	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/plugin"
)

// Plugin is the engine plugin that registers the authz.local factory.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// Compile-time assertion.
var _ plugin.EnginePlugin = (*Plugin)(nil)

// New creates a new localauthz plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "localauthz",
				PluginVersion:     "0.1.0",
				PluginDescription: "In-process authz.local RBAC enforcer for scenario testing",
			},
			Manifest: plugin.PluginManifest{
				Name:        "localauthz",
				Version:     "0.1.0",
				Author:      "GoCodeAlone",
				Description: "In-process authz.local RBAC enforcer for scenario testing",
				ModuleTypes: []string{"authz.local"},
			},
		},
	}
}

// ModuleFactories returns a factory for "authz.local".
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"authz.local": func(name string, cfg map[string]any) modular.Module {
			return &localAuthzModule{name: name, cfg: cfg}
		},
	}
}

// ── in-process module ──────────────────────────────────────────────────────

// policy is a parsed [subject, object, action] triple.
type policy struct{ sub, obj, act string }

// localAuthzModule implements modular.Module + modular.ServiceAware and
// satisfies the module.Enforcer interface (variadic Enforce method).
type localAuthzModule struct {
	name     string
	cfg      map[string]any
	policies []policy
}

// Name returns the module instance name.
func (m *localAuthzModule) Name() string { return m.name }

// Init parses the policies from config and logs a startup message.
func (m *localAuthzModule) Init(app modular.Application) error {
	m.policies = parsePolicies(m.cfg)
	app.Logger().Info("authz.local: loaded policies",
		"module", m.name,
		"count", len(m.policies),
	)
	return nil
}

// ProvidesServices registers this module under its own name so
// infra.admin can resolve it via app.GetService(authzModule, &Enforcer).
func (m *localAuthzModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "in-process RBAC enforcer (authz.local)",
			Instance:    m,
		},
	}
}

// RequiresServices returns nil — no dependencies.
func (m *localAuthzModule) RequiresServices() []modular.ServiceDependency { return nil }

// Enforce checks whether (sub, obj, act) matches any configured policy.
// The variadic extra ...string is accepted but ignored — it exists to
// match the concrete Casbin wrapper's method signature (module.Enforcer
// plan-review C-NEW-1 constraint). Default-deny: returns false when no
// policy matches.
func (m *localAuthzModule) Enforce(sub, obj, act string, _ ...string) (bool, error) {
	for _, p := range m.policies {
		if p.sub == sub && p.obj == obj && p.act == act {
			return true, nil
		}
	}
	return false, nil
}

// parsePolicies decodes config.policies from the raw map.
// Accepts []any{[]any{string, string, string}, ...} (YAML-decoded shape).
func parsePolicies(cfg map[string]any) []policy {
	raw, ok := cfg["policies"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]policy, 0, len(items))
	for _, item := range items {
		row, ok := item.([]any)
		if !ok || len(row) < 3 {
			continue
		}
		sub, _ := row[0].(string)
		obj, _ := row[1].(string)
		act, _ := row[2].(string)
		if sub != "" && obj != "" && act != "" {
			out = append(out, policy{sub, obj, act})
		}
	}
	return out
}
