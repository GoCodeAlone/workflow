package assembler

import (
	"fmt"

	"github.com/GoCodeAlone/workflow/capability/inventory"
	"github.com/GoCodeAlone/workflow/capability/scaffold"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/schema"
	"gopkg.in/yaml.v3"
)

// Assemble composes a structurally-valid scaffold from the chosen capability set
// (+ explicit modules). Pure: no I/O. Runs the in-process schema.ValidateConfig
// gate (V3, fail-closed) before returning — on validation failure returns a
// non-nil error and a nil *AssembledApp (⊥ write-on-fail).
func Assemble(inv *inventory.Inventory, in AssemblyInput, reg *schema.ModuleSchemaRegistry) (*AssembledApp, error) {
	// 1. select (D1: inventory raw strings; D8: greedy set-cover + registry tie-break)
	selected, unmatched := selectModules(inv, in.Capabilities, reg)

	app := &AssembledApp{Unmatched: unmatched}

	// 2. config-gen + existence gate (D6): types ∉ registry -> skip + finding
	for _, typ := range selected {
		cfg, ok := genConfig(typ, reg)
		if !ok {
			app.Findings = append(app.Findings, Finding{
				Level: "warn", Code: "no-schema",
				Message: fmt.Sprintf("module type %q not in schema registry — skipped (add manually)", typ),
			})
			continue
		}
		app.Modules = append(app.Modules, config.ModuleConfig{Name: defaultName(typ), Type: typ, Config: cfg})
	}

	// 3. explicit modules (agent inject/pin) — emitted as-is
	for _, em := range in.Modules {
		name := em.Name
		if name == "" {
			name = defaultName(em.Type)
		}
		app.Modules = append(app.Modules, config.ModuleConfig{Name: name, Type: em.Type, Config: em.Config})
	}

	// 4. wiring: grammar-driven (C2; replaces v0.82.0's in-code wire). Merge
	//    engine grammar ∪ plugin grammar, then apply Category-A glue: Requires
	//    → ensure-selected + DependsOn; Attaches → entry-point section;
	//    RouteMiddlewares + Fragment → routes. This is what produces a functional
	//    scaffold (e.g. the crud-route routes an api.handler emits). Glue-gaps +
	//    Category-B runtime hooks surface as NEXT_STEPS findings. The MC-parity
	//    test (capability/scaffold) proves this reproduces v0.82.0 wiring.
	merged, hooks, err := scaffold.MergeGrammar(reg, inv)
	if err != nil {
		return nil, fmt.Errorf("assemble: grammar merge: %w", err)
	}
	res := scaffold.GrammarWire(app.Modules, merged, reg, httpAlwaysSelect())
	app.Modules = res.Modules
	app.Workflows = res.Workflows
	app.GlueGaps = res.GlueGaps
	app.RuntimeHooks = hooks
	for _, g := range res.GlueGaps {
		app.Findings = append(app.Findings, Finding{Level: "warn", Code: "glue-gap", Message: g})
	}
	for _, h := range hooks {
		app.Findings = append(app.Findings, Finding{Level: "info", Code: "runtime-hook", Message: "runtime precondition (fired at boot): " + h})
	}

	// 5. requires.plugins — external/local-plugin providers of REQUESTED+MATCHED
	//    capabilities only (D2). Iterating all inv.Capabilities would inflate the
	//    emitted requires.plugins to the entire ecosystem; scope to what the user
	//    actually asked for (unmatched capabilities contribute nothing).
	unmatchedSet := map[string]bool{}
	for _, u := range unmatched {
		unmatchedSet[u] = true
	}
	for _, want := range in.Capabilities {
		if unmatchedSet[want] {
			continue
		}
		for i := range inv.Capabilities {
			if inv.Capabilities[i].ID != want {
				continue
			}
			for j := range inv.Capabilities[i].Providers {
				p := &inv.Capabilities[i].Providers[j]
				if p.Kind != "external" && p.Kind != "local-plugin" {
					continue // builtin package — auto-loaded by DefaultPlugins
				}
				app.Requires.Plugins = appendUniquePlugin(app.Requires.Plugins, p.Name)
			}
		}
	}

	// 6. V3 gate — fail-closed (structural; ⊥ runtime-factory, D6/V8)
	cfg := &config.WorkflowConfig{
		Modules:   app.Modules,
		Workflows: app.Workflows,
		Triggers:  map[string]any{},
		Requires:  &app.Requires,
	}
	if err := schema.ValidateConfig(cfg); err != nil {
		return nil, fmt.Errorf("assemble: generated config fails validation (V3 fail-closed): %w", err)
	}
	return app, nil
}

// MarshalConfig renders the assembled app to workflow.yaml bytes. Pure; shared by
// the CLI emitter + the MC2-bis boot test (P4: avoids cmd/wfctl <-> capability/
// assembler import cycle).
func MarshalConfig(app *AssembledApp) ([]byte, error) {
	cfg := &config.WorkflowConfig{
		Modules: app.Modules, Workflows: app.Workflows, Triggers: map[string]any{},
	}
	if len(app.Requires.Plugins) > 0 {
		cfg.Requires = &app.Requires
	}
	return yaml.Marshal(cfg)
}

// defaultName + envPrefix live in config.go (shared by config-gen).

// httpAlwaysSelect returns the entry-point types always materialized for an HTTP
// scaffold (M1 is HTTP-only, V12): http.router (which Requires http.server) +
// health.checker (the /healthz binding precondition). This reproduces v0.82.0's
// in-code wire() always-ensure behavior; the MC-parity test guards it.
func httpAlwaysSelect() []string {
	return []string{"http.router", "health.checker"}
}

func appendUniquePlugin(plugs []config.PluginRequirement, name string) []config.PluginRequirement {
	for _, p := range plugs {
		if p.Name == name {
			return plugs
		}
	}
	return append(plugs, config.PluginRequirement{Name: name})
}
