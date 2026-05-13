package workflow

import (
	"container/heap"
	"fmt"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

// topoSortModules reorders modules so each module appears after every module
// it lists in DependsOn. Kahn's algorithm with a stable tie-break on the
// original declared index: among the modules in the ready frontier at any
// point, the one with the lowest declared index is dequeued first. This means
// a module's final position can shift relative to its declared siblings if a
// sibling becomes ready earlier (e.g., a root pops first and unblocks its
// dependent, which now sits in the ready queue alongside other declared-later
// roots). What is preserved is the relative order of modules that are *both*
// in the ready frontier at the same iteration. Returns an error if a
// dependency cycle is detected.
//
// Missing dependency targets are tolerated as no-op edges. Schema.ValidateConfig
// rejects unresolvable targets for the *declared* modules, but ConfigTransformHooks
// can inject modules after validation runs, and those transform-injected modules'
// dependsOn was never schema-checked. Silently ignoring the edge keeps this
// function side-effect-free; the actual runtime failure (if any) surfaces when
// the dependent looks up its parent in a plugin-local registry.
func topoSortModules(modules []config.ModuleConfig) ([]config.ModuleConfig, error) {
	n := len(modules)
	if n <= 1 {
		return modules, nil
	}

	// Record the first declared index for each name so duplicates (which the
	// schema rejects, but which can in theory slip in via ConfigTransformHooks
	// merging fragments) resolve their dependents against the original entry
	// rather than silently shadowing it.
	index := make(map[string]int, n)
	for i, m := range modules {
		if _, exists := index[m.Name]; !exists {
			index[m.Name] = i
		}
	}

	// inDegree[i] = number of declared dependencies of modules[i] that
	// actually exist in `modules`. dependents[i] = indices of modules that
	// depend on modules[i].
	inDegree := make([]int, n)
	dependents := make([][]int, n)
	for i, m := range modules {
		for _, dep := range m.DependsOn {
			if dep == "" {
				continue
			}
			depIdx, ok := index[dep]
			if !ok {
				continue
			}
			inDegree[i]++
			dependents[depIdx] = append(dependents[depIdx], i)
		}
	}

	// Initial frontier: every module with no remaining declared deps. Ordered
	// by declared index via a min-heap so Pop is O(log n) per step instead of
	// O(n log n) from re-sorting a slice every iteration.
	ready := &intHeap{}
	heap.Init(ready)
	for i := 0; i < n; i++ {
		if inDegree[i] == 0 {
			heap.Push(ready, i)
		}
	}

	out := make([]config.ModuleConfig, 0, n)
	for ready.Len() > 0 {
		i := heap.Pop(ready).(int)
		out = append(out, modules[i])
		for _, j := range dependents[i] {
			inDegree[j]--
			if inDegree[j] == 0 {
				heap.Push(ready, j)
			}
		}
	}

	if len(out) != n {
		// Cycle detected. Kahn's algorithm cannot distinguish strict cycle
		// members from their downstream dependents using inDegree alone — both
		// retain non-zero inDegree after the frontier drains — so the error
		// names every module that could not be ordered. The cycle members are
		// the SCC root(s) somewhere in this set; the rest are modules that
		// transitively depend on them. Listing all of them is the
		// actionable surface: operators have to break a cycle somewhere in
		// this set to make the graph schedulable.
		unordered := make([]string, 0, n-len(out))
		for i := 0; i < n; i++ {
			if inDegree[i] > 0 {
				unordered = append(unordered, modules[i].Name)
			}
		}
		sort.Strings(unordered)
		return nil, fmt.Errorf(
			"module dependsOn cycle (or dependents of one) among: %s",
			strings.Join(unordered, ", "),
		)
	}

	return out, nil
}

// intHeap is a min-heap of declared-module indices used as the Kahn-frontier
// in topoSortModules. Implementing container/heap.Interface keeps Pop O(log n)
// per step, which matters when configs grow into the hundreds of modules.
type intHeap []int

func (h intHeap) Len() int           { return len(h) }
func (h intHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h intHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *intHeap) Push(x any) { *h = append(*h, x.(int)) }
func (h *intHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// filterResolvableDeps returns a new slice containing only entries from deps
// that are present in the moduleNames set, in the same order. Empty strings
// and names not in moduleNames are dropped. Used by engine BuildFromConfig
// before calling SetDependencies on a module, so modular's DependencyAware
// sort sees the same edge set that topoSortModules used when ordering
// cfg.Modules (both ignore unresolvable + empty entries).
//
// Schema validation rejects empty + unknown dependsOn entries for declared
// modules, but ConfigTransformHooks can inject modules whose dependsOn was
// never validated. This is the engine's defensive boundary against that.
func filterResolvableDeps(deps []string, moduleNames map[string]struct{}) []string {
	out := make([]string, 0, len(deps))
	for _, dep := range deps {
		if dep == "" {
			continue
		}
		if _, exists := moduleNames[dep]; !exists {
			continue
		}
		out = append(out, dep)
	}
	return out
}
