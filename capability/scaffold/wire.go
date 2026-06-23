package scaffold

import (
	"fmt"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/schema"
)

// ApplyResult is the Category-A wire output: the (possibly extended) module
// list, the emitted workflows.<class> sections (entry-point + routes), and any
// glue-gaps (unselected Attaches.To targets) surfaced as NEXT_STEPS data.
type ApplyResult struct {
	Modules   []config.ModuleConfig
	Workflows map[string]any
	GlueGaps  []string
}

// GrammarWire applies Category-A Assembly Grammar to mods (D16/D17/D18/D22/D23,
// + P2/P6/P8 fixes):
//   - Requires (by type): ensure-selected (visited-set transitive closure) + DependsOn.
//   - RouteMiddlewares (by TYPE — P8): materialize an instance (named defaultName(mwt))
//     so the runtime GetService resolves consistently with the emitted route reference.
//   - AlwaysSelect (P2): entry-point-class-declared types always materialized
//     (e.g. http.router + health.checker for the http class, for v0.82.0 parity).
//   - ensure-selected materializes WITH DefaultConfig from reg (P6).
//   - Attaches.To: DependsOn + entry-point section; unselected (real type) → glue-gap;
//     phantom → already rejected at MergeGrammar (D23).
//   - Fragment: emit the route block with RouteMiddlewares-as-defaultNames (D4 crud-route).
//
// reg is the engine ModuleSchemaRegistry (for DefaultConfig + type lookups); g is the
// MergeGrammar output. The function does not append to res.Modules after Pass A, so
// pointers indexed post-Pass-A stay valid through Pass B (⊥ storing pointers into a
// slice that is still being appended to — retro #1 dangling-pointer class).
func GrammarWire(mods []config.ModuleConfig, g MergedGrammar, reg *schema.ModuleSchemaRegistry, alwaysSelect []string) ApplyResult {
	res := ApplyResult{
		Modules:   append([]config.ModuleConfig{}, mods...),
		Workflows: map[string]any{},
	}

	present := map[string]bool{}
	for i := range res.Modules {
		present[res.Modules[i].Type] = true
	}
	visited := map[string]bool{}

	// ensureType materializes type t (with DefaultConfig — P6) if absent, then
	// transitively ensures its Requires (visited-set bounded — D17 wire-time closure).
	// Declared as var first so the literal can reference itself recursively.
	var ensureType func(t string)
	ensureType = func(t string) {
		if visited[t] {
			return
		}
		visited[t] = true
		if !present[t] {
			res.Modules = append(res.Modules, config.ModuleConfig{
				Name:   defaultName(t),
				Type:   t,
				Config: defaultConfigFor(reg, t),
			})
			present[t] = true
		}
		if decl, ok := g[t]; ok {
			for _, req := range decl.Requires {
				ensureType(req)
			}
		}
	}

	// Pass A: ensure each selected module's Requires + RouteMiddlewares, then the
	// always-selected entry-point types. Snapshot first so appends don't extend the
	// range we iterate.
	snapshot := append([]config.ModuleConfig{}, res.Modules...)
	for i := range snapshot {
		ensureType(snapshot[i].Type)
		decl, ok := g[snapshot[i].Type]
		if !ok {
			continue
		}
		for _, mwt := range decl.RouteMiddlewares { // P8: RouteMiddlewares are TYPES
			ensureType(mwt)
		}
		// Attaches.To: auto-ensure ENGINE-KNOWN targets so scaffolds are functional
		// (api.handler → http.router → http.server). Plugin-type targets that are not
		// selected stay absent and surface as glue-gaps in Pass B (design D23).
		if decl.Attaches != nil && regHas(reg, decl.Attaches.To) {
			ensureType(decl.Attaches.To)
		}
	}
	for _, t := range alwaysSelect { // P2
		ensureType(t)
	}

	// byType is rebuilt AFTER all appends; no further appends occur, so these
	// pointers are stable through Pass B.
	byType := indexByType(res.Modules)

	// Pass B: DependsOn (Requires + Attaches.To), entry-point sections, fragments.
	for i := range res.Modules {
		decl, ok := g[res.Modules[i].Type]
		if !ok {
			continue
		}
		m := &res.Modules[i]
		if decl.Attaches != nil {
			parent, has := byType[decl.Attaches.To]
			if !has {
				// Unselected real type (phantom caught at MergeGrammar) → glue-gap.
				res.GlueGaps = append(res.GlueGaps, fmt.Sprintf("%s attaches to %s (not selected)", m.Type, decl.Attaches.To))
			} else {
				if !dependsOn(m, parent.Name) {
					m.DependsOn = append(m.DependsOn, parent.Name)
				}
				if decl.Attaches.Emit != "" {
					emitEntryPoint(&res, decl.Attaches, parent, m)
				}
			}
		}
		for _, req := range decl.Requires {
			if p, has := byType[req]; has && !dependsOn(m, p.Name) {
				m.DependsOn = append(m.DependsOn, p.Name)
			}
		}
		if decl.Fragment != nil {
			emitFragment(&res, decl.Fragment, m, g)
		}
	}
	return res
}

// emitEntryPoint records the workflows.<class> entry-point section. class is the
// Attaches.Emit value minus its "workflows." prefix. The section keys are the role
// names of the parent + child module types (defaultName(type)), matching the
// v0.82.0 httpWorkflow shape {http:{server, router}}.
func emitEntryPoint(res *ApplyResult, attaches *schema.AttachSpec, parent, child *config.ModuleConfig) {
	class := strings.TrimPrefix(attaches.Emit, "workflows.")
	if class == "" || class == attaches.Emit {
		// Emit is not a workflows.<class> reference; nothing to record.
		return
	}
	section, _ := res.Workflows[class].(map[string]any)
	if section == nil {
		section = map[string]any{}
		res.Workflows[class] = section
	}
	section[defaultName(parent.Type)] = parent.Name
	section[defaultName(child.Type)] = child.Name
}

// emitFragment emits a Category-A route fragment for the owning module (D4
// crud-route). M1 supports only the "crud-route" kind: per-method routes
// (GET/POST /{resource}, GET/PUT/DELETE /{resource}/{id}) under
// workflows.http.routes, each tagged with the owning module's RouteMiddlewares
// resolved to instance names (P8). Field substitution is resourceName from the
// owner config (charset-validated at grammar-load per D21; resourceName is
// authored, not user-injected here).
func emitFragment(res *ApplyResult, f *schema.FragmentSpec, owner *config.ModuleConfig, g MergedGrammar) {
	if f.Kind != "crud-route" {
		return // M1: only crud-route (D9 kind-enum)
	}
	resource, _ := owner.Config["resourceName"].(string)
	if resource == "" {
		return // D20: required field absent — grammar-load should have errored; defensive.
	}
	mwName := ""
	if decl, ok := g[owner.Type]; ok && len(decl.RouteMiddlewares) > 0 {
		mwName = defaultName(decl.RouteMiddlewares[0]) // P8: by-type → instance name
	}
	routes := []map[string]any{
		{"method": "GET", "path": "/" + resource, "handler": owner.Name},
		{"method": "POST", "path": "/" + resource, "handler": owner.Name},
		{"method": "GET", "path": "/" + resource + "/{id}", "handler": owner.Name},
		{"method": "PUT", "path": "/" + resource + "/{id}", "handler": owner.Name},
		{"method": "DELETE", "path": "/" + resource + "/{id}", "handler": owner.Name},
	}
	for i := range routes {
		if mwName != "" {
			routes[i]["middlewares"] = []string{mwName}
		}
	}
	attachRoutes(res, "http", routes)
}

// attachRoutes merges routes into the workflows.<class>.routes list.
func attachRoutes(res *ApplyResult, class string, routes []map[string]any) {
	section, _ := res.Workflows[class].(map[string]any)
	if section == nil {
		section = map[string]any{}
		res.Workflows[class] = section
	}
	existing, _ := section["routes"].([]map[string]any)
	section["routes"] = append(existing, routes...)
}

// --- helpers ---

// indexByType maps each module type to its first instance (pointer into mods).
func indexByType(mods []config.ModuleConfig) map[string]*config.ModuleConfig {
	out := make(map[string]*config.ModuleConfig, len(mods))
	for i := range mods {
		if _, ok := out[mods[i].Type]; !ok {
			out[mods[i].Type] = &mods[i]
		}
	}
	return out
}

// defaultName is the instance name for a materialized module of type t: the
// segment after the final "." (http.server→server, http.router→router,
// health.checker→health). Matches the names v0.82.0's in-code wire() used.
func defaultName(t string) string {
	if i := strings.LastIndex(t, "."); i >= 0 {
		return t[i+1:]
	}
	return t
}

// defaultConfigFor returns a shallow copy of the registry's DefaultConfig for t
// (P6) so materialized modules carry authored defaults (⊥ bare append). nil if
// the type or its DefaultConfig is absent.
func defaultConfigFor(reg *schema.ModuleSchemaRegistry, t string) map[string]any {
	if reg == nil {
		return nil
	}
	s := reg.Get(t)
	if s == nil || s.DefaultConfig == nil {
		return nil
	}
	out := make(map[string]any, len(s.DefaultConfig))
	for k, v := range s.DefaultConfig {
		out[k] = v
	}
	return out
}

// dependsOn reports whether m declares name in its DependsOn list.
func dependsOn(m *config.ModuleConfig, name string) bool {
	for _, d := range m.DependsOn {
		if d == name {
			return true
		}
	}
	return false
}

// regHas reports whether t is an engine-known (registered) module type.
func regHas(reg *schema.ModuleSchemaRegistry, t string) bool {
	return reg != nil && reg.Get(t) != nil
}
