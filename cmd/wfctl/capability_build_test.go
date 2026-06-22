package main

import (
	"testing"

	"github.com/GoCodeAlone/workflow/capability/inventory"
)

// fixtureInventory builds a minimal in-memory ecosystem inventory for the
// buildSelection unit tests: one capability (auth.authz) with one provider.
func fixtureInventory(t *testing.T) *inventory.Inventory {
	t.Helper()
	return &inventory.Inventory{
		Metadata: inventory.Metadata{Generator: "test"},
		Capabilities: []inventory.Capability{
			{
				ID:       "auth.authz",
				Category: "auth",
				Name:     "Authorization",
				Providers: []inventory.Provider{
					{Name: "auth", Kind: "local-plugin"},
				},
			},
		},
	}
}

func TestBuildSelection_ProvidersForChosenCapabilities(t *testing.T) {
	sel := newBuildSelection(fixtureInventory(t))
	sel.toggleCapability("auth.authz")
	rec := sel.recommendation()
	if len(rec.Capabilities) != 1 || rec.Capabilities[0].ID != "auth.authz" {
		t.Fatalf("selection -> %#v, want auth.authz", rec.Capabilities)
	}
}

func TestBuildSelection_ToggleDeselects(t *testing.T) {
	sel := newBuildSelection(fixtureInventory(t))
	sel.toggleCapability("auth.authz")
	sel.toggleCapability("auth.authz") // deselect
	if rec := sel.recommendation(); len(rec.Capabilities) != 0 {
		t.Fatalf("toggle should deselect: %#v", rec.Capabilities)
	}
}

func TestBuildSelection_EmptyIsEmpty(t *testing.T) {
	sel := newBuildSelection(fixtureInventory(t))
	if rec := sel.recommendation(); len(rec.Capabilities) != 0 {
		t.Fatalf("empty selection -> zero hits: %#v", rec)
	}
}
