package main

import (
	"sort"

	"github.com/GoCodeAlone/workflow/capability/inventory"
	"github.com/GoCodeAlone/workflow/capability/recommend"
)

// buildSelection is the pure, tea-free selection state for `wfctl capability build`.
// It is separated from the Bubbletea model (buildModel, Task 5) so the selection
// logic is unit-testable without a terminal.
type buildSelection struct {
	inv    *inventory.Inventory
	chosen map[string]bool // capability IDs
}

func newBuildSelection(inv *inventory.Inventory) *buildSelection {
	return &buildSelection{inv: inv, chosen: map[string]bool{}}
}

// toggleCapability flips membership of id in the chosen set.
func (s *buildSelection) toggleCapability(id string) { s.chosen[id] = !s.chosen[id] }

// recommendation delegates to recommend.Recommend over the chosen capability set.
// An empty selection yields an empty recommendation (nothing requested), rather
// than the unfiltered inventory that recommend.Recommend returns for empty input.
func (s *buildSelection) recommendation() *recommend.Recommendation {
	var ids []string
	for id, on := range s.chosen {
		if on {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return &recommend.Recommendation{}
	}
	return recommend.Recommend(s.inv, recommend.Options{Capabilities: ids})
}
