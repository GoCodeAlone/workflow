package recommend

import (
	"fmt"
)

// WizardState is the agent-consumable scaffold wizard state (design G4/§C4).
// Transport-agnostic JSON: the MCP/ACP twin is deferred to M4; M1 emits this
// directly so any host (CLI, agent) can consume it. It composes the chosen
// capability providers (+ facts) with the grammar wire's glue-gaps and
// Category-B runtime-hook preconditions, surfaced as actionable NextSteps.
type WizardState struct {
	Chosen       []WizardProvider `json:"chosen"`
	Alternatives []WizardProvider `json:"alternatives,omitempty"`
	GlueGaps     []string         `json:"glueGaps,omitempty"`
	NextSteps    []string         `json:"nextSteps,omitempty"`
}

// WizardProvider is a single provider within the wizard state, carrying the
// selection facts (raw Kind/ReleaseStatus + installRequired).
type WizardProvider struct {
	CapabilityID    string `json:"capabilityId"`
	Category        string `json:"category,omitempty"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	ReleaseStatus   string `json:"releaseStatus,omitempty"`
	InstallRequired bool   `json:"installRequired,omitempty"`
}

// BuildWizardState composes a WizardState from a Recommendation. For each
// capability the first provider is "chosen" and the rest are "alternatives",
// each carrying its facts. glueGaps (unselected Attaches.To from the grammar
// wire) pass through; hooks (Category-B RuntimeHooks) become NextSteps, as does
// an explicit "install <name>" step for every installRequired provider.
func BuildWizardState(rec *Recommendation, glueGaps, hooks []string) *WizardState {
	ws := &WizardState{GlueGaps: append([]string(nil), glueGaps...)}

	seenInstall := map[string]bool{}
	for _, cap := range rec.Capabilities {
		for i, p := range cap.Providers {
			wp := WizardProvider{
				CapabilityID:    cap.ID,
				Category:        cap.Category,
				Name:            p.Name,
				Kind:            p.Kind,
				ReleaseStatus:   p.ReleaseStatus,
				InstallRequired: p.InstallRequired,
			}
			if i == 0 {
				ws.Chosen = append(ws.Chosen, wp)
			} else {
				ws.Alternatives = append(ws.Alternatives, wp)
			}
			if p.InstallRequired && !seenInstall[p.Name] {
				seenInstall[p.Name] = true
				ws.NextSteps = append(ws.NextSteps, fmt.Sprintf("install provider %q (%s) — not a built-in default", p.Name, p.Kind))
			}
		}
	}
	for _, h := range hooks {
		ws.NextSteps = append(ws.NextSteps, fmt.Sprintf("runtime precondition: %s (fired at boot)", h))
	}
	for _, g := range glueGaps {
		ws.NextSteps = append(ws.NextSteps, fmt.Sprintf("glue gap: %s", g))
	}
	return ws
}
