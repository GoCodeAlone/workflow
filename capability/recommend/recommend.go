// Package recommend produces a filtered, grouped view of which plugins provide
// a set of requested capabilities. Pure + deterministic (design V2): no ranking
// (inventory carries no quality/popularity signal — design review D13).
//
// NOTE: the inventory is manifest-derived (registry manifests + sibling plugin.json
// checkouts); it does NOT carry runtime-factory-verified signal. Providers carry
// real Kind ("registry"|"external"|"local-plugin") + ReleaseStatus ("released"|
// "local-only") fields, surfaced as-is for the consumer to interpret.
package recommend

import (
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/capability/inventory"
)

// Options selects capabilities to recommend for.
type Options struct {
	Capabilities         []string
	Categories           []string
	IncludeUncategorized bool
}

// Recommendation is the filtered + grouped result.
type Recommendation struct {
	Requested    []string        `json:"requested"`
	Capabilities []CapabilityHit `json:"capabilities"`
	Unmatched    []string        `json:"unmatched,omitempty"`
}

// CapabilityHit groups providers of one capability.
type CapabilityHit struct {
	ID        string            `json:"id"`
	Category  string            `json:"category"`
	Name      string            `json:"name"`
	Providers []ProviderSummary `json:"providers"`
}

// ProviderSummary is a compact provider descriptor (real inventory fields).
type ProviderSummary struct {
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	ReleaseStatus string `json:"releaseStatus,omitempty"`
	Source        string `json:"source,omitempty"`
}

// Recommend filters inv to requested capabilities and groups their providers.
// It is pure: it performs no ranking (the inventory carries no quality or
// popularity signal) and produces a deterministic ordering.
func Recommend(inv *inventory.Inventory, opts Options) *Recommendation {
	r := &Recommendation{}
	req := normalize(opts.Capabilities)
	cats := normalize(opts.Categories)
	r.Requested = append([]string(nil), keysSorted(req)...)

	// matchedTerms records which REQUESTED terms found at least one capability.
	// A requested term may match by id, name, tag, or description substring, so
	// we cannot infer "matched" from capability ids/names alone — tracking the
	// terms themselves avoids false "unmatched" reports for tag/description hits.
	matchedTerms := make(map[string]bool)
	for i := range inv.Capabilities {
		cap := &inv.Capabilities[i]
		if !opts.IncludeUncategorized && isUncategorized(*cap) {
			continue
		}
		if len(cats) > 0 && !cats[strings.ToLower(cap.Category)] {
			continue
		}
		if len(req) > 0 {
			any := false
			for term := range req {
				if termMatches(cap, term) {
					matchedTerms[term] = true
					any = true
				}
			}
			if !any {
				continue
			}
		}
		r.Capabilities = append(r.Capabilities, buildHit(cap))
	}
	sort.Slice(r.Capabilities, func(i, j int) bool {
		if r.Capabilities[i].Category != r.Capabilities[j].Category {
			return r.Capabilities[i].Category < r.Capabilities[j].Category
		}
		return r.Capabilities[i].ID < r.Capabilities[j].ID
	})
	for _, want := range keysSorted(req) {
		if !matchedTerms[want] {
			r.Unmatched = append(r.Unmatched, want)
		}
	}
	return r
}

func buildHit(cap *inventory.Capability) CapabilityHit {
	h := CapabilityHit{ID: cap.ID, Category: cap.Category, Name: cap.Name}
	seen := map[string]bool{}
	for i := range cap.Providers {
		p := &cap.Providers[i]
		// De-dupe by (name, kind, releaseStatus): the inventory may carry the
		// same provider name with different release status (registry vs local),
		// and both variants are meaningful to surface.
		key := strings.Join([]string{p.Name, p.Kind, p.ReleaseStatus}, "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		h.Providers = append(h.Providers, ProviderSummary{
			Name:          p.Name,
			Kind:          p.Kind,
			ReleaseStatus: p.ReleaseStatus,
			Source:        p.Source,
		})
	}
	sort.Slice(h.Providers, func(i, j int) bool { return h.Providers[i].Name < h.Providers[j].Name })
	return h
}

// termMatches reports whether a single requested term matches cap by id, name,
// tag, or description substring (all case-insensitive; term is pre-lowercased).
func termMatches(cap *inventory.Capability, term string) bool {
	if strings.EqualFold(term, cap.ID) || strings.EqualFold(term, cap.Name) {
		return true
	}
	for _, t := range cap.Tags {
		if strings.EqualFold(term, t) {
			return true
		}
	}
	if desc := strings.ToLower(cap.Description); desc != "" && strings.Contains(desc, term) {
		return true
	}
	return false
}

func isUncategorized(c inventory.Capability) bool {
	return c.Category == "uncategorized" || strings.HasPrefix(c.ID, "uncategorized:")
}

// normalize lower-cases, trims, and drops empty entries from ss.
func normalize(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			m[s] = true
		}
	}
	return m
}

// keysSorted returns the keys of m sorted ascending for deterministic output.
func keysSorted(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
