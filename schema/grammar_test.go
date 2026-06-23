package schema

import (
	"encoding/json"
	"strings"
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
	// omitempty: a schema with no grammar marshals cleanly, OMITTING the grammar
	// keys entirely (⊥ serializing them as explicit nulls). Assert each grammar
	// key is absent from the marshaled output.
	bare := &ModuleSchema{Type: "x"}
	out, err := json.Marshal(bare)
	if err != nil {
		t.Fatal(err)
	}
	got2 := string(out)
	for _, field := range []string{"provides", "requires", "attaches", "routeMiddlewares", "fragment", "runtimeHooks"} {
		if strings.Contains(got2, `"`+field+`"`) {
			t.Fatalf("bare schema must omit empty grammar field %q via omitempty, got %s", field, got2)
		}
	}
}
