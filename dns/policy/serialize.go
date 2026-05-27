package policy

import (
	"fmt"
	"sort"
	"strings"
)

// Serialize emits Policy as deterministically-ordered TXT RR strings.
// Refuses to emit if multiple entries have Default=true.
func Serialize(p *Policy) ([]string, error) {
	defaultCount := 0
	for _, e := range p.Entries {
		if e.Default {
			defaultCount++
		}
	}
	if defaultCount > 1 {
		return nil, fmt.Errorf("%w (Policy has %d defaults; only 1 allowed)", ErrMultipleDefaults, defaultCount)
	}
	out := make([]string, 0, len(p.Entries))
	for _, e := range p.Entries {
		// Sort patterns + types within entry for deterministic hash
		pats := append([]string(nil), e.Patterns...)
		sort.Strings(pats)
		types := append([]string(nil), e.Types...)
		sort.Strings(types)

		sb := strings.Builder{}
		fmt.Fprintf(&sb, "heritage=%s o=%s", HeritageV1, e.Owner)
		if len(pats) > 0 {
			fmt.Fprintf(&sb, " p=%s", strings.Join(pats, ","))
		}
		if len(types) > 0 {
			fmt.Fprintf(&sb, " t=%s", strings.Join(types, ","))
		}
		if e.Default {
			sb.WriteString(" d=true")
		}
		out = append(out, sb.String())
	}
	sort.Strings(out) // RR-level sort for deterministic hashing
	return out, nil
}
