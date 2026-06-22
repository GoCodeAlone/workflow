package main

import (
	"testing"

	tea "charm.land/bubbletea/v2"
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

func TestBuildModel_CtrlCCancels(t *testing.T) {
	m := newBuildModel(newBuildSelection(fixtureInventory(t)))
	got, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	if cmd == nil {
		t.Fatal("ctrl+c did not request quit")
	}
	bm, ok := got.(buildModel)
	if !ok || !bm.cancelled {
		t.Fatalf("ctrl+c did not mark cancelled: %#v", got)
	}
}

func TestBuildModel_AdvancesToReview(t *testing.T) {
	m := newBuildModel(newBuildSelection(fixtureInventory(t)))
	m.selection.toggleCapability("auth.authz") // choose one on screenCapability
	got, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if bm, ok := got.(buildModel); !ok || bm.screen != screenReview {
		t.Fatalf("Enter did not advance to review: %#v", got)
	}
}
