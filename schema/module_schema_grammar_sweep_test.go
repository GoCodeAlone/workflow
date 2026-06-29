package schema

import "testing"

// TestBuiltinGrammarSweep asserts the glue-gap sweep encoded the v0.82.0 in-code
// wire() rules + the Category-A crud-route fragment as Assembly Grammar on the
// engine builtins (design §C1/§C2, plan Task 6). The real registry is the single
// source of truth so the grammar-driven wire reproduces v0.82.0 wiring without a
// hand-maintained rule table.
func TestBuiltinGrammarSweep(t *testing.T) {
	r := NewModuleSchemaRegistry()

	t.Run("http.server entry-point", func(t *testing.T) {
		s := r.Get("http.server")
		if s == nil {
			t.Fatal("http.server missing")
		}
		if len(s.Provides) == 0 {
			t.Fatalf("http.server must Provide an entry-point service: %+v", s)
		}
	})

	t.Run("http.router requires + attaches server", func(t *testing.T) {
		rt := r.Get("http.router")
		if rt == nil {
			t.Fatal("http.router missing")
		}
		if len(rt.Requires) != 1 || rt.Requires[0] != "http.server" {
			t.Fatalf("http.router must Require [http.server]: %+v", rt.Requires)
		}
		if rt.Attaches == nil || rt.Attaches.To != "http.server" || rt.Attaches.Emit != "workflows.http" {
			t.Fatalf("http.router Attaches want {To:http.server,Emit:workflows.http}: %+v", rt.Attaches)
		}
	})

	t.Run("middlewares attach to router", func(t *testing.T) {
		for _, typ := range []string{"http.middleware.auth", "http.middleware.logging", "http.middleware.cors", "http.middleware.ratelimit"} {
			m := r.Get(typ)
			if m == nil {
				t.Fatalf("%s missing", typ)
			}
			if m.Attaches == nil || m.Attaches.To != "http.router" {
				t.Fatalf("%s must Attach to http.router: %+v", typ, m.Attaches)
			}
		}
	})

	t.Run("api.handler attaches router + crud-route fragment", func(t *testing.T) {
		api := r.Get("api.handler")
		if api == nil {
			t.Fatal("api.handler missing")
		}
		if api.Attaches == nil || api.Attaches.To != "http.router" {
			t.Fatalf("api.handler must Attach to http.router: %+v", api.Attaches)
		}
		if api.Fragment == nil || api.Fragment.Kind != "crud-route" {
			t.Fatalf("api.handler must have a crud-route fragment: %+v", api.Fragment)
		}
	})

	t.Run("category B runtime hooks documented", func(t *testing.T) {
		// Category B preconditions are DOCUMENTED (⊥ emitted); the MC-functional
		// proof runs a real engine where the hooks fire (design V13).
		hc := r.Get("health.checker")
		if hc == nil || len(hc.RuntimeHooks) == 0 {
			t.Fatalf("health.checker must document RuntimeHooks: %+v", hc)
		}
		jwt := r.Get("auth.jwt")
		if jwt == nil || len(jwt.RuntimeHooks) == 0 {
			t.Fatalf("auth.jwt must document RuntimeHooks: %+v", jwt)
		}
	})
}
