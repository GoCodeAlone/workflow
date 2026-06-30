package recommend_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/capability/inventory"
	"github.com/GoCodeAlone/workflow/capability/recommend"
)

// TestBuildWizardState (design G4/§C4): the wizard-state emitter produces an
// agent-consumable JSON — Chosen (first provider per capability, with facts),
// Alternatives (the rest), GlueGaps, and NextSteps (Category-B hooks +
// install steps for installRequired providers). An agent can parse + read it.
func TestBuildWizardState(t *testing.T) {
	inv := &inventory.Inventory{Capabilities: []inventory.Capability{
		{ID: "auth.authn", Category: "auth", Name: "Authentication", Providers: []inventory.Provider{
			{Name: "auth", Kind: "external", ReleaseStatus: "released"},           // chosen (first)
			{Name: "auth-alt", Kind: "local-plugin", ReleaseStatus: "local-only"}, // alternative
		}},
	}}
	rec := recommend.Recommend(inv, recommend.Options{Categories: []string{"auth"}})

	ws := recommend.BuildWizardState(rec,
		[]string{"auth attaches to http.router (not selected)"}, // glue-gaps from the grammar wire
		[]string{"auth-provider-registration"},                  // Category-B RuntimeHooks
	)

	// Chosen = first provider of the capability, carrying facts.
	if len(ws.Chosen) != 1 || ws.Chosen[0].CapabilityID != "auth.authn" || ws.Chosen[0].Name != "auth" {
		t.Fatalf("chosen wrong: %+v", ws.Chosen)
	}
	if ws.Chosen[0].Kind != "external" {
		t.Fatalf("chosen must carry raw Kind: %+v", ws.Chosen[0])
	}
	// Alternatives = the remaining providers.
	if len(ws.Alternatives) != 1 || ws.Alternatives[0].Name != "auth-alt" {
		t.Fatalf("alternatives wrong: %+v", ws.Alternatives)
	}
	// GlueGaps pass through.
	if len(ws.GlueGaps) != 1 || !strings.Contains(ws.GlueGaps[0], "http.router") {
		t.Fatalf("glueGaps wrong: %+v", ws.GlueGaps)
	}
	// NextSteps carry the Category-B hook.
	foundHook := false
	for _, s := range ws.NextSteps {
		if strings.Contains(s, "auth-provider-registration") {
			foundHook = true
		}
	}
	if !foundHook {
		t.Fatalf("nextSteps must surface the Category-B hook: %+v", ws.NextSteps)
	}

	// Agent-consumable: JSON round-trips + has the documented top-level keys.
	b, err := json.Marshal(ws)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("wizard-state JSON not agent-parseable: %v", err)
	}
	for _, key := range []string{"chosen", "alternatives", "glueGaps", "nextSteps"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("wizard-state JSON missing top-level key %q: %s", key, b)
		}
	}
}

// TestBuildWizardState_InstallSteps: an installRequired alternative provider
// surfaces a NextStep telling the agent to install it.
func TestBuildWizardState_InstallSteps(t *testing.T) {
	inv := &inventory.Inventory{Capabilities: []inventory.Capability{
		{ID: "x.y", Category: "x", Name: "X", Providers: []inventory.Provider{
			{Name: "builtin", Kind: "registry", ReleaseStatus: "released"},
			{Name: "needs-install", Kind: "local-plugin", ReleaseStatus: "local-only"},
		}},
	}}
	rec := recommend.Recommend(inv, recommend.Options{Categories: []string{"x"}})
	ws := recommend.BuildWizardState(rec, nil, nil)

	foundInstall := false
	for _, s := range ws.NextSteps {
		if strings.Contains(s, "needs-install") && strings.Contains(strings.ToLower(s), "install") {
			foundInstall = true
		}
	}
	if !foundInstall {
		t.Fatalf("installRequired provider must surface an install NextStep: %+v", ws.NextSteps)
	}
}
