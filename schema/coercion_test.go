package schema

import "testing"

func TestCoercionRegistryHasHTTPRequest(t *testing.T) {
	reg := NewTypeCoercionRegistry()
	rules := reg.Rules()
	targets, ok := rules["http.Request"]
	if !ok {
		t.Fatal("expected http.Request in coercion rules")
	}
	found := false
	for _, t2 := range targets {
		if t2 == "any" {
			found = true
		}
	}
	if !found {
		t.Error("http.Request should coerce to 'any'")
	}
}

func TestCoercionRegistryNotEmpty(t *testing.T) {
	reg := NewTypeCoercionRegistry()
	if len(reg.Rules()) == 0 {
		t.Fatal("coercion registry should not be empty")
	}
}
