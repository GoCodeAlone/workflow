package assembler

import "github.com/GoCodeAlone/workflow/config"

// wire applies the thin in-code wiring-rules table (D4: NOT recipes, NOT a data dir):
//  1. ensure an entry-point module exists (http.server) — required by checkEntryPoints (V3)
//  2. http.router depends on http.server
//  3. middleware chain depends on the router
//  4. auth.* depends on the rate-limit/middleware predecessor (best-effort)
//  5. health.checker is present alongside http.router so the observability
//     health-endpoints wiring hook can bind /healthz at boot (D15)
//
// It mutates mods in place and returns the (possibly appended) slice.
func wire(mods []config.ModuleConfig) []config.ModuleConfig {
	server := findType(mods, "http.server")
	if server == nil {
		mods = append(mods, config.ModuleConfig{Name: "server", Type: "http.server",
			Config: map[string]any{"address": ":8080"}})
		server = &mods[len(mods)-1]
	}
	router := findType(mods, "http.router")
	if router == nil {
		mods = append(mods, config.ModuleConfig{Name: "router", Type: "http.router",
			DependsOn: []string{server.Name}})
		router = &mods[len(mods)-1]
	} else if !dependsOn(router, server.Name) {
		router.DependsOn = append(router.DependsOn, server.Name)
	}
	// middleware -> router
	for i := range mods {
		if isMiddleware(mods[i].Type) && !dependsOn(&mods[i], router.Name) {
			mods[i].DependsOn = append(mods[i].DependsOn, router.Name)
		}
	}
	// health.checker presence for /healthz auto-binding (D15)
	if findType(mods, "health.checker") == nil {
		mods = append(mods, config.ModuleConfig{Name: "health", Type: "health.checker"})
	}
	return mods
}

// findType returns a pointer to the first module of the given type, or nil.
func findType(mods []config.ModuleConfig, typ string) *config.ModuleConfig {
	for i := range mods {
		if mods[i].Type == typ {
			return &mods[i]
		}
	}
	return nil
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

func isMiddleware(t string) bool {
	const p = "http.middleware."
	return len(t) > len(p) && t[:len(p)] == p
}

// httpWorkflow returns the workflows.http section that attaches the router to the
// server at boot. The engine runs ConfigureWorkflow only for workflowType=="http"
// (engine.go:676), which calls server.AddRouter(router); WITHOUT this section
// http.server.Start() fails "no router configured for HTTP server" (P1).
func httpWorkflow(mods []config.ModuleConfig) map[string]any {
	server := findType(mods, "http.server")
	router := findType(mods, "http.router")
	if server == nil || router == nil {
		return map[string]any{} // wire() ensured both; defensive
	}
	return map[string]any{
		"http": map[string]any{
			"server": server.Name,
			"router": router.Name,
		},
	}
}

// WirePublic exposes the v0.82.0 in-code wire() for the grammar-driven wire's
// MC-parity regression test (capability/scaffold). Behavior is unchanged; this
// is a thin test seam only. Once the assembler routes through scaffold.GrammarWire
// (post glue-sweep), wire() remains as the parity reference.
func WirePublic(mods []config.ModuleConfig) []config.ModuleConfig { return wire(mods) }

// HttpWorkflowPublic exposes the v0.82.0 httpWorkflow() for the parity test.
func HttpWorkflowPublic(mods []config.ModuleConfig) map[string]any { return httpWorkflow(mods) }
