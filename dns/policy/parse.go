package policy

import (
	"fmt"
	"strings"
)

// Parse parses TXT RR strings (one per RR) into a Policy.
// Unknown heritage values are silently skipped (forward-compat).
func Parse(zone string, txtRRs []string) (*Policy, error) {
	p := &Policy{Zone: zone}
	defaultCount := 0
	for _, rr := range txtRRs {
		fields := tokenize(rr)
		if fields["heritage"] != HeritageV1 {
			continue // foreign TXT (SPF, future schema, etc.)
		}
		owner := strings.TrimSpace(fields["o"])
		if owner == "" {
			return nil, fmt.Errorf("%w: rr=%q", ErrEmptyOwner, rr)
		}
		entry := Entry{
			Owner:    owner,
			Patterns: splitCSV(fields["p"]),
			Types:    splitCSV(fields["t"]),
			Default:  fields["d"] == "true",
		}
		if entry.Default {
			defaultCount++
			if defaultCount > 1 {
				return nil, fmt.Errorf("%w: rr=%q", ErrMultipleDefaults, rr)
			}
		}
		p.Entries = append(p.Entries, entry)
	}
	return p, nil
}

// tokenize splits "key=value key=value" into a map.
func tokenize(rr string) map[string]string {
	out := map[string]string{}
	for _, tok := range strings.Fields(rr) {
		eq := strings.IndexByte(tok, '=')
		if eq < 0 {
			continue
		}
		out[tok[:eq]] = tok[eq+1:]
	}
	return out
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}
