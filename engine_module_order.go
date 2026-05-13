package workflow

import (
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
// Missing dependency targets are tolerated here and reported via the existing
// schema.ValidateConfig pass (schema/validate.go:191); this function only
// orders the modules that *are* declared.
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

	// Initial frontier: every module with no remaining declared deps,
	// in declared order.
	ready := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if inDegree[i] == 0 {
			ready = append(ready, i)
		}
	}

	out := make([]config.ModuleConfig, 0, n)
	for len(ready) > 0 {
		// Stable tie-break: pop the lowest declared index first.
		sort.Ints(ready)
		i := ready[0]
		ready = ready[1:]
		out = append(out, modules[i])
		for _, j := range dependents[i] {
			inDegree[j]--
			if inDegree[j] == 0 {
				ready = append(ready, j)
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
