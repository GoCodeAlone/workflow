package scaffold

import (
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/schema"
)

// categoryAGrammar is a complete HTTP Assembly Grammar exercising every
// Category-A rule: entry-point (http.server), Requires+Attaches (router),
// middleware attach (auth), and a crud-route Fragment with by-TYPE
// RouteMiddlewares (P8).
func categoryAGrammar() MergedGrammar {
	return MergedGrammar{
		"http.server":          {Provides: []string{"http.Server"}},
		"http.router":          {Requires: []string{"http.server"}, Attaches: &schema.AttachSpec{To: "http.server", Emit: "workflows.http"}},
		"http.middleware.auth": {Attaches: &schema.AttachSpec{To: "http.router"}},
		"api.handler": {
			Attaches:         &schema.AttachSpec{To: "http.router"},
			RouteMiddlewares: []string{"http.middleware.auth"}, // P8: by-TYPE
			Fragment:         &schema.FragmentSpec{Kind: "crud-route", Fields: []string{"resourceName"}},
		},
	}
}

// TestWire_CategoryA (D16/D18/D4/P8): a module with Requires+RouteMiddlewares+
// Fragment ensures its deps + DependsOn, emits the entry-point section, and
// emits crud-route routes tagged with the resolved middleware instance name.
func TestWire_CategoryA(t *testing.T) {
	eng := categoryAGrammar()
	reg := schema.NewModuleSchemaRegistry()
	mods := []config.ModuleConfig{{Name: "api", Type: "api.handler", Config: map[string]any{"resourceName": "orders"}}}

	res := GrammarWire(mods, eng, reg, []string{"health.checker"})

	// Attaches.To (engine-known) + Requires auto-ensured (functional scaffold).
	if !hasType(res.Modules, "http.server") || !hasType(res.Modules, "http.router") || !hasType(res.Modules, "http.middleware.auth") {
		t.Fatalf("ensure-selected failed (D16/D18/P8): %s", dumpGraph(res.Modules))
	}
	// RouteMiddleware by-TYPE materialized as an instance named defaultName(type).
	auth := findType(res.Modules, "http.middleware.auth")
	if auth == nil || auth.Name != "auth" {
		t.Fatalf("middleware instance name want \"auth\", got %+v", auth)
	}
	// DependsOn wiring: router→server (Requires), api→router (Attaches).
	if !dependsOn(findType(res.Modules, "http.router"), serverName(res.Modules)) {
		t.Fatal("router must depend on server (D16)")
	}
	if !dependsOn(findType(res.Modules, "api.handler"), routerName(res.Modules)) {
		t.Fatal("api.handler must attach to router")
	}
	// crud-route Fragment: 5 routes, each tagged with the resolved mw instance name.
	routes := wfRoutes(res.Workflows)
	if len(routes) != 5 {
		t.Fatalf("crud-route want 5 routes, got %d: %+v", len(routes), routes)
	}
	gotMethods := map[string]int{}
	for _, r := range routes {
		mw, _ := r["middlewares"].([]string)
		if len(mw) != 1 || mw[0] != "auth" {
			t.Fatalf("route missing auth middleware (D4/D18/P8): %+v", r)
		}
		gotMethods[r["method"].(string)]++
	}
	// 5 routes = GET(x2 list+item), POST, PUT, DELETE.
	if gotMethods["GET"] != 2 || gotMethods["POST"] != 1 || gotMethods["PUT"] != 1 || gotMethods["DELETE"] != 1 {
		t.Fatalf("crud-route method distribution wrong: %+v", gotMethods)
	}
	for _, r := range routes {
		p, _ := r["path"].(string)
		if p != "/orders" && p != "/orders/{id}" {
			t.Fatalf("crud-route path wrong: %q", p)
		}
	}
}

// TestWire_FragmentEmitsAllMiddlewares (Copilot #5): a crud-route fragment
// tags routes with ALL declared RouteMiddlewares (by-type → instance names),
// not just the first — Pass A materializes every one, so dropping any in
// emitFragment would silently lose a declared middleware.
func TestWire_FragmentEmitsAllMiddlewares(t *testing.T) {
	eng := MergedGrammar{
		"http.server":             {Provides: []string{"http.Server"}},
		"http.router":             {Requires: []string{"http.server"}, Attaches: &schema.AttachSpec{To: "http.server", Emit: "workflows.http"}},
		"http.middleware.auth":    {Attaches: &schema.AttachSpec{To: "http.router"}},
		"http.middleware.logging": {Attaches: &schema.AttachSpec{To: "http.router"}},
		"api.handler": {
			Attaches:         &schema.AttachSpec{To: "http.router"},
			RouteMiddlewares: []string{"http.middleware.auth", "http.middleware.logging"},
			Fragment:         &schema.FragmentSpec{Kind: "crud-route", Fields: []string{"resourceName"}},
		},
	}
	reg := schema.NewModuleSchemaRegistry()
	mods := []config.ModuleConfig{{Name: "api", Type: "api.handler", Config: map[string]any{"resourceName": "things"}}}

	res := GrammarWire(mods, eng, reg, []string{"health.checker"})

	for _, r := range wfRoutes(res.Workflows) {
		mw, _ := r["middlewares"].([]string)
		if len(mw) != 2 || mw[0] != "auth" || mw[1] != "logging" {
			t.Fatalf("route must carry ALL declared middlewares [auth logging], got %+v", mw)
		}
	}
}

// TestWire_AttachesPluginTypeIsGlueGap (D23): an Attaches.To target that is NOT
// engine-known and not selected surfaces as a glue-gap (⊥ engine-known targets
// which are auto-ensured for a functional scaffold).
func TestWire_AttachesPluginTypeIsGlueGap(t *testing.T) {
	eng := MergedGrammar{
		"plugin.handler": {Attaches: &schema.AttachSpec{To: "plugin.dependency"}},
		// plugin.dependency is grammar-known (so MergeGrammar wouldn't flag it
		// phantom) but NOT engine-known and not selected → glue-gap here.
		"plugin.dependency": {Provides: []string{"plugin.Dep"}},
	}
	reg := schema.NewModuleSchemaRegistry()
	mods := []config.ModuleConfig{{Name: "h", Type: "plugin.handler"}}

	res := GrammarWire(mods, eng, reg, nil)

	if hasType(res.Modules, "plugin.dependency") {
		t.Fatalf("non-engine Attaches.To must NOT be auto-ensured: %s", dumpGraph(res.Modules))
	}
	if len(res.GlueGaps) == 0 {
		t.Fatalf("expected a glue-gap for unselected plugin.dependency, got none: %+v", res.GlueGaps)
	}
}

// TestWire_EmptyGrammarPassthrough: with no grammar, modules pass through
// unchanged (no spurious ensures/DependsOn).
func TestWire_EmptyGrammarPassthrough(t *testing.T) {
	reg := schema.NewModuleSchemaRegistry()
	mods := []config.ModuleConfig{{Name: "a", Type: "x.y"}, {Name: "b", Type: "z.w"}}
	res := GrammarWire(mods, MergedGrammar{}, reg, nil)
	if len(res.Modules) != 2 || res.Modules[0].Name != "a" || res.Modules[1].Name != "b" {
		t.Fatalf("passthrough mutated mods: %+v", res.Modules)
	}
	if len(res.Workflows) != 0 || len(res.GlueGaps) != 0 {
		t.Fatalf("empty grammar should emit nothing: wf=%+v gaps=%+v", res.Workflows, res.GlueGaps)
	}
}

// --- test helpers ---

func hasType(mods []config.ModuleConfig, typ string) bool {
	return findType(mods, typ) != nil
}

func findType(mods []config.ModuleConfig, typ string) *config.ModuleConfig {
	for i := range mods {
		if mods[i].Type == typ {
			return &mods[i]
		}
	}
	return nil
}

func serverName(mods []config.ModuleConfig) string { return firstName(mods, "http.server") }
func routerName(mods []config.ModuleConfig) string { return firstName(mods, "http.router") }

func firstName(mods []config.ModuleConfig, typ string) string {
	if m := findType(mods, typ); m != nil {
		return m.Name
	}
	return ""
}

// wfRoutes extracts workflows.http.routes as a slice of route maps.
func wfRoutes(wf map[string]any) []map[string]any {
	section, _ := wf["http"].(map[string]any)
	if section == nil {
		return nil
	}
	routes, _ := section["routes"].([]map[string]any)
	return routes
}
