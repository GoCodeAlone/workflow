package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

// topoSortModules reorders modules so each module appears after every module it
// lists in DependsOn. Sibling modules (modules with no inter-dependency) keep
// their declared order — Kahn's algorithm with a stable tie-break on the
// original index. Returns an error if a dependency cycle is detected.
//
// Missing dependency targets are tolerated here and reported via the existing
// schema.ValidateConfig pass (schema/validate.go:191); this function only
// orders the modules that *are* declared.
func topoSortModules(modules []config.ModuleConfig) ([]config.ModuleConfig, error) {
	n := len(modules)
	if n <= 1 {
		return modules, nil
	}

	index := make(map[string]int, n)
	for i, m := range modules {
		index[m.Name] = i
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
		// Cycle: collect the names of all modules with remaining in-degree
		// so the error message is actionable.
		remaining := make([]string, 0, n-len(out))
		for i := 0; i < n; i++ {
			if inDegree[i] > 0 {
				remaining = append(remaining, modules[i].Name)
			}
		}
		sort.Strings(remaining)
		return nil, fmt.Errorf(
			"module dependsOn forms a cycle among: %s",
			strings.Join(remaining, ", "),
		)
	}

	return out, nil
}
