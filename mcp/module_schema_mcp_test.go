package mcp

import (
	"testing"

	"github.com/GoCodeAlone/workflow/schema"
)

// TestModuleSchemaToMapIncludesAllFields asserts D24: get_module_schema's
// hand-map serializes EVERY ModuleSchema field, including the additive fields
// the prior literal dropped — MaxIncoming/MaxOutgoing and the Assembly Grammar
// fields (Provides/Requires/Attaches/RouteMiddlewares/Fragment/RuntimeHooks).
func TestModuleSchemaToMapIncludesAllFields(t *testing.T) {
	mi := 0
	mo := 8
	ms := &schema.ModuleSchema{
		Type:             "x.test",
		Label:            "Test",
		Category:         "http",
		Description:      "desc",
		Provides:         []string{"x.Svc"},
		Requires:         []string{"x.dep"},
		Attaches:         &schema.AttachSpec{To: "x.dep", Emit: "workflows.http"},
		RouteMiddlewares: []string{"auth-mw"},
		Fragment:         &schema.FragmentSpec{Kind: "crud-route", Fields: []string{"resourceName"}},
		RuntimeHooks:     []string{"auth-provider-registration"},
		MaxIncoming:      &mi,
		MaxOutgoing:      &mo,
	}

	out := moduleSchemaToMap(ms)

	// Every field must be PRESENT in the output map (D24 — ⊥ the old hand-map
	// which silently dropped additive fields). Check key existence explicitly.
	wantKeys := []string{
		"type", "label", "category", "description",
		"inputs", "outputs", "configFields", "defaultConfig",
		"maxIncoming", "maxOutgoing",
		"provides", "requires", "attaches", "routeMiddlewares", "fragment", "runtimeHooks",
	}
	for _, k := range wantKeys {
		if _, ok := out[k]; !ok {
			t.Errorf("D24: field %q missing from moduleSchemaToMap output", k)
		}
	}

	// Spot-check grammar CONTENT round-trips (not just presence).
	if r, ok := out["requires"].([]string); !ok || len(r) != 1 || r[0] != "x.dep" {
		t.Errorf("requires not round-tripped: %+v", out["requires"])
	}
	if a, ok := out["attaches"].(*schema.AttachSpec); !ok || a.To != "x.dep" || a.Emit != "workflows.http" {
		t.Errorf("attaches not round-tripped: %+v", out["attaches"])
	}
	if f, ok := out["fragment"].(*schema.FragmentSpec); !ok || f.Kind != "crud-route" || len(f.Fields) != 1 {
		t.Errorf("fragment not round-tripped: %+v", out["fragment"])
	}
	if mi2, ok := out["maxIncoming"].(*int); !ok || mi2 == nil || *mi2 != 0 {
		t.Errorf("maxIncoming not round-tripped: %+v", out["maxIncoming"])
	}
}

// TestModuleSchemaToMapNilOptionalFields asserts a bare schema (no grammar, no
// limits) still maps cleanly — optional fields carry their zero value, not panic.
func TestModuleSchemaToMapNilOptionalFields(t *testing.T) {
	ms := &schema.ModuleSchema{Type: "bare"}
	out := moduleSchemaToMap(ms)
	if out["type"] != "bare" {
		t.Fatalf("type not set: %+v", out)
	}
	// Grammar fields present as nil (⊥ absent). Existence is what matters for
	// agent consumers expecting a stable shape.
	for _, k := range []string{"provides", "requires", "attaches", "fragment", "runtimeHooks", "maxIncoming"} {
		if _, ok := out[k]; !ok {
			t.Errorf("D24: optional field %q must still be present (nil) for shape stability", k)
		}
	}
}
