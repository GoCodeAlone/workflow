package assembler

import (
	"testing"

	"github.com/GoCodeAlone/workflow/capability/inventory"
	"github.com/GoCodeAlone/workflow/schema"
)

func realInventory(t *testing.T) *inventory.Inventory {
	t.Helper()
	// minimal synthetic inventory exercising the compose path; real CollectEcosystem
	// is exercised end-to-end in the CLI + boot tests.
	return &inventory.Inventory{Capabilities: []inventory.Capability{
		cap("auth.authn", prov("auth", "builtin", "module:auth.jwt")),
		cap("http.routing", prov("http", "builtin", "module:http.server", "module:http.router")),
		cap("storage.database", prov("storage", "builtin", "module:database.workflow")),
		cap("observability.health", prov("observability", "builtin", "module:health.checker")),
	}}
}

func TestAssemble_StructurallyValid(t *testing.T) {
	reg := schema.NewModuleSchemaRegistry()
	app, err := Assemble(realInventory(t), AssemblyInput{
		Capabilities: []string{"auth.authn", "http.routing", "storage.database", "observability.health"},
	}, reg)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if !hasModuleType(app, "auth.jwt") || !hasModuleType(app, "database.workflow") ||
		!hasModuleType(app, "http.server") || !hasModuleType(app, "http.router") {
		t.Fatalf("missing expected modules: %+v", app.Modules)
	}
	if len(app.Unmatched) != 0 {
		t.Fatalf("unmatched=%v", app.Unmatched)
	}
}

func TestAssemble_FailClosedOnMissingRequiredField(t *testing.T) {
	// explicit storage.s3 with no config -> bucket (Required, no default) missing
	// -> ValidateConfig rejects -> Assemble returns error (V3 fail-closed). P3.
	reg := schema.NewModuleSchemaRegistry()
	_, err := Assemble(&inventory.Inventory{}, AssemblyInput{
		Modules: []ExplicitModule{{Type: "storage.s3", Name: "store"}},
	}, reg)
	if err == nil {
		t.Fatal("want V3 fail-closed error (storage.s3 missing required bucket)")
	}
}

func TestAssemble_V3RejectsUnknownExplicitType(t *testing.T) {
	// explicit module of an unknown type -> ValidateConfig rejects (V3 gate).
	reg := schema.NewModuleSchemaRegistry()
	_, err := Assemble(&inventory.Inventory{}, AssemblyInput{
		Modules: []ExplicitModule{{Type: "no.such.type", Name: "x"}},
	}, reg)
	if err == nil {
		t.Fatal("want V3 error for unknown explicit module type")
	}
}

func TestAssemble_SelectedTypeNotInRegistrySkippedWithFinding(t *testing.T) {
	// storage.database -> database.workflow, but ∉ registry -> skipped + no-schema
	// finding (V8/D6). wire() does NOT re-add database.workflow, so ValidateConfig
	// still passes (server/router/health present). Genuine V8 path.
	reg := schema.NewModuleSchemaRegistry()
	reg.Unregister("database.workflow")
	app, err := Assemble(realInventory(t), AssemblyInput{Capabilities: []string{"storage.database"}}, reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasFinding(app, "no-schema") {
		t.Fatalf("want no-schema finding, got %+v", app.Findings)
	}
	if hasModuleType(app, "database.workflow") {
		t.Fatal("database.workflow should be skipped (∉ registry)")
	}
}

func hasFinding(app *AssembledApp, code string) bool {
	for _, f := range app.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

func hasModuleType(app *AssembledApp, typ string) bool {
	for _, m := range app.Modules {
		if m.Type == typ {
			return true
		}
	}
	return false
}

func TestAssemble_RequiresPluginsScopedToRequested(t *testing.T) {
	// Regression: requires.plugins must list only external providers of REQUESTED+
	// matched capabilities — not the whole ecosystem (previously inflated to ~all
	// plugins because the loop iterated every inv.Capabilities).
	inv := &inventory.Inventory{Capabilities: []inventory.Capability{
		cap("http.routing", prov("workflow-plugin-http", "external", "module:http.server", "module:http.router")),
		cap("storage.database", prov("workflow-plugin-some-db", "external", "module:database.workflow")),
	}}
	reg := schema.NewModuleSchemaRegistry()
	app, err := Assemble(inv, AssemblyInput{Capabilities: []string{"http.routing"}}, reg)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	names := pluginNames(app)
	if len(names) != 1 || names[0] != "workflow-plugin-http" {
		t.Fatalf("requires.plugins=%v want exactly [workflow-plugin-http] (storage.database not requested)", names)
	}
}

func pluginNames(app *AssembledApp) []string {
	out := make([]string, 0, len(app.Requires.Plugins))
	for _, p := range app.Requires.Plugins {
		out = append(out, p.Name)
	}
	return out
}
