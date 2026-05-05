package interfaces_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestTenant_IsZero(t *testing.T) {
	var z interfaces.Tenant
	if !z.IsZero() {
		t.Error("zero value Tenant not detected as zero")
	}
	nz := interfaces.Tenant{ID: "abc"}
	if nz.IsZero() {
		t.Error("non-zero Tenant flagged as zero")
	}
}

func TestTenant_Fields(t *testing.T) {
	tnt := interfaces.Tenant{
		ID:       "t1",
		Name:     "Acme Corp",
		Slug:     "acme",
		Domains:  []string{"acme.example.com"},
		Metadata: map[string]any{"plan": "pro"},
		IsActive: true,
	}
	if tnt.IsZero() {
		t.Error("fully-populated Tenant should not be zero")
	}
	if len(tnt.Domains) != 1 {
		t.Errorf("expected 1 domain, got %d", len(tnt.Domains))
	}
	if tnt.Metadata["plan"] != "pro" {
		t.Errorf("metadata plan: got %v, want pro", tnt.Metadata["plan"])
	}
}

// stubSelector is a minimal Selector for compile-time interface check.
type stubSelector struct {
	key     string
	matched bool
}

func (s stubSelector) Match(_ *http.Request) (string, bool, error) {
	return s.key, s.matched, nil
}

func TestSelector_Interface(t *testing.T) {
	var _ interfaces.Selector = stubSelector{}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	sel := stubSelector{key: "tenant-a", matched: true}
	k, ok, err := sel.Match(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected matched=true")
	}
	if k != "tenant-a" {
		t.Errorf("got key %q, want tenant-a", k)
	}
}
