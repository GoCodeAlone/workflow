package assembler

import (
	"github.com/GoCodeAlone/workflow/config"
	"testing"
)

func TestWire_EnsuresEntryPointAndRouterChain(t *testing.T) {
	mods := []config.ModuleConfig{
		{Name: "db", Type: "database.workflow"},
		{Name: "auth", Type: "auth.jwt"},
	}
	// wire appends (may reallocate); reassign from return (retro lesson #1).
	mods = wire(mods)
	// http.server auto-added as entry point (none present) ; http.router added + depends on server
	if findType(mods, "http.server") == nil {
		t.Fatal("want http.server entry point auto-added")
	}
	if r := findType(mods, "http.router"); r == nil || !dependsOn(r, serverName(mods)) {
		t.Fatal("want http.router depending on the http.server")
	}
}

func TestWire_NoDuplicateServerWhenPresent(t *testing.T) {
	mods := []config.ModuleConfig{{Name: "srv", Type: "http.server"}}
	mods = wire(mods)
	count := 0
	for _, m := range mods {
		if m.Type == "http.server" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("want exactly 1 http.server, got %d", count)
	}
}

func serverName(mods []config.ModuleConfig) string {
	for i := range mods {
		if mods[i].Type == "http.server" {
			return mods[i].Name
		}
	}
	return ""
}

func TestWire_HttpWorkflowSection(t *testing.T) {
	mods := wire([]config.ModuleConfig{{Name: "db", Type: "database.workflow"}})
	wf := httpWorkflow(mods)
	http, ok := wf["http"].(map[string]any)
	if !ok || http["server"] == nil || http["router"] == nil {
		t.Fatalf("want workflows.http{server,router}, got %+v", wf) // P1
	}
}

func TestWire_RouterDependsOnExplicitServerName(t *testing.T) {
	// explicit http.server named "api" -> auto-added router depends on "api" (P6)
	mods := wire([]config.ModuleConfig{{Name: "api", Type: "http.server"}})
	r := findType(mods, "http.router")
	if r == nil || !dependsOn(r, "api") {
		t.Fatalf("want router depending on explicit 'api' server, got %+v", r) // P6
	}
}
