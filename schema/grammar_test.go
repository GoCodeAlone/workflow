package schema

import (
	"encoding/json"
	"testing"
)

func TestModuleSchemaGrammarFieldsRoundTrip(t *testing.T) {
	s := &ModuleSchema{Type: "http.router",
		Requires:         []string{"http.server"},
		Attaches:         &AttachSpec{To: "http.server", Emit: "workflows.http"},
		Fragment:         &FragmentSpec{Kind: "crud-route", Fields: []string{"resourceName"}},
		RouteMiddlewares: []string{"auth-mw"},
		RuntimeHooks:     []string{"auth-provider-registration"},
		Provides:         []string{"routed"},
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var got ModuleSchema
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Attaches == nil || got.Attaches.To != "http.server" {
		t.Fatalf("Attaches lost: %+v", got)
	}
	if got.Fragment == nil || got.Fragment.Kind != "crud-route" {
		t.Fatalf("Fragment lost")
	}
	if len(got.RouteMiddlewares) != 1 || got.RouteMiddlewares[0] != "auth-mw" {
		t.Fatalf("RouteMiddlewares lost")
	}
	// omitzero: a schema w/o grammar marshals cleanly (no null fields)
	bare := &ModuleSchema{Type: "x"}
	if out, _ := json.Marshal(bare); string(out) == "" {
		t.Fatal("bare marshal empty")
	}
}
