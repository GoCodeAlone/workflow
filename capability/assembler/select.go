package assembler

import (
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/capability/inventory"
	"github.com/GoCodeAlone/workflow/schema"
)

// selectModules runs greedy module-level set-cover over requested capability IDs.
// Candidates = raw "module:<type>" strings on inventory.Provider.Capabilities[]
// (⊥ NOT recommend.ProviderSummary — drops them, design D1). Tie-break:
// (coverage desc, in-registry first, type-name asc) — deterministic (V2, D8).
func selectModules(inv *inventory.Inventory, requested []string, reg *schema.ModuleSchemaRegistry) (selected []string, unmatched []string) {
	want := map[string]bool{}
	for _, c := range requested {
		want[c] = true
	}
	// coverage: type -> set of requested caps it covers
	cov := map[string]map[string]bool{}
	for i := range inv.Capabilities {
		cap := &inv.Capabilities[i]
		if !want[cap.ID] {
			continue
		}
		for j := range cap.Providers {
			for _, raw := range cap.Providers[j].Capabilities {
				typeName, ok := moduleTypeOf(raw)
				if !ok {
					continue
				}
				if cov[typeName] == nil {
					cov[typeName] = map[string]bool{}
				}
				cov[typeName][cap.ID] = true
			}
		}
	}
	covered := map[string]bool{}
	for len(covered) < len(want) {
		best, bestUncovered := "", 0
		for typ := range cov {
			uncovered := 0
			for c := range cov[typ] {
				if !covered[c] {
					uncovered++
				}
			}
			if uncovered == 0 {
				continue
			}
			if betterCandidate(typ, uncovered, reg, best, bestUncovered) {
				best, bestUncovered = typ, uncovered
			}
		}
		if best == "" {
			break
		}
		selected = append(selected, best)
		for c := range cov[best] {
			covered[c] = true
		}
		delete(cov, best)
	}
	sort.Strings(selected)
	// unmatched: requested caps never covered, in input order
	seen := map[string]bool{}
	for _, c := range requested {
		if !covered[c] && !seen[c] {
			unmatched = append(unmatched, c)
			seen[c] = true
		}
	}
	return selected, unmatched
}

// betterCandidate implements the deterministic tie-break (D8).
func betterCandidate(typ string, uncovered int, reg *schema.ModuleSchemaRegistry, best string, bestUncovered int) bool {
	if best == "" {
		return true
	}
	if uncovered != bestUncovered {
		return uncovered > bestUncovered
	}
	typInReg, bestInReg := reg.Get(typ) != nil, reg.Get(best) != nil
	if typInReg != bestInReg {
		return typInReg // prefer config-generatable (builtin-core)
	}
	return typ < best // name ascending
}

// moduleTypeOf strips the "module:" prefix from a raw inventory capability string.
func moduleTypeOf(raw string) (string, bool) {
	const prefix = "module:"
	if !strings.HasPrefix(raw, prefix) {
		return "", false
	}
	return strings.TrimPrefix(raw, prefix), true
}
