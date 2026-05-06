package main

import "github.com/GoCodeAlone/workflow/interfaces"

// buildAppliedSpecMap walks states + refs and returns the per-ref applied-
// config map for DriftConfigDetector.DetectDriftWithApplied. Entries whose
// AppliedConfigSource is anything other than "apply" are OMITTED from the
// returned map — providers cannot meaningfully compute config-drift against
// adoption-shaped Outputs (would yield false-positives) and legacy state
// (no provenance recorded) defaults to adoption treatment per ADR 0010.
//
// Empty AppliedConfig (nil map or zero-length map) is also omitted: there
// is no spec to compare against.
//
// Returns nil when no safe entries exist, so callers can short-circuit the
// type-assertion entirely and fall back to legacy DetectDrift.
func buildAppliedSpecMap(states []interfaces.ResourceState, refs []interfaces.ResourceRef) map[string]map[string]any {
	if len(states) == 0 || len(refs) == 0 {
		return nil
	}
	byName := make(map[string]*interfaces.ResourceState, len(states))
	for i := range states {
		byName[states[i].Name] = &states[i]
	}
	out := make(map[string]map[string]any, len(refs))
	for _, ref := range refs {
		st, ok := byName[ref.Name]
		if !ok {
			continue
		}
		// Only "apply"-provenance entries carry true user-supplied config.
		// "adoption", "" (legacy), or any other value defaults to adoption
		// treatment: omit to avoid false-positive config-drift.
		if st.AppliedConfigSource != "apply" {
			continue
		}
		if len(st.AppliedConfig) == 0 {
			continue
		}
		// Shallow copy so providers cannot mutate the caller's state map.
		cfg := make(map[string]any, len(st.AppliedConfig))
		for k, v := range st.AppliedConfig {
			cfg[k] = v
		}
		out[ref.Name] = cfg
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
