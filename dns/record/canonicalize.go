package record

import (
	"sort"
	"strings"
	"unicode"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// FromResourceStates converts imported IaC state into a canonical Portfolio.
// Reads each infra.dns ResourceState's records (Outputs preferred, else
// AppliedConfig), renaming provider-specific value keys to the canonical "value".
// If an infra.dns state includes provider-supplied authority metadata in
// Outputs["authority"], safe keys are copied into Snapshot.Authority.
// Each infra.dns_delegation state populates Snapshot.Authority with
// registrar_nameservers and live_nameservers for the matching domain.
// States of other types are silently skipped.
//
// Snapshots are grouped by (Provider, Domain) so that infra.dns and
// infra.dns_delegation states for the same domain are merged into one Snapshot.
// Output is sorted by (Provider, Domain) for deterministic order.
//
// Provider value-key divergence (verified against provider drivers):
//   - DigitalOcean + Cloudflare emit "data"
//   - Hover emits "content" (workflow-plugin-hover/internal/drivers/dns.go:538)
//   - Namecheap emits "address"
//
// The valueAlias helper resolves the first non-empty of: data → content → address → value.
func FromResourceStates(states []interfaces.ResourceState) Portfolio {
	p := Portfolio{Schema: SchemaV1}

	type key struct{ provider, domain string }
	order := []key{}
	snapByKey := map[key]*Snapshot{}

	getOrCreate := func(provider, domain string) *Snapshot {
		k := key{provider, domain}
		if s, ok := snapByKey[k]; ok {
			return s
		}
		s := &Snapshot{
			ID:       provider + "-" + sanitizeDomainForID(domain),
			Provider: provider,
			Domain:   domain,
			Records:  []Record{},
		}
		snapByKey[k] = s
		order = append(order, k)
		return s
	}

	for i := range states {
		st := &states[i]

		switch st.Type {
		case "infra.dns":
			domain := st.ProviderID
			if domain == "" {
				if d, ok := st.AppliedConfig["domain"].(string); ok {
					domain = d
				}
			}
			if domain == "" {
				continue
			}
			snap := getOrCreate(st.Provider, domain)
			recs := pickRecords(st.Outputs, st.AppliedConfig)
			for _, raw := range recs {
				m, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				snap.Records = append(snap.Records, recordFromMap(m))
			}
			mergeAuthority(snap, st.Outputs["authority"])

		case "infra.dns_delegation":
			domain := st.ProviderID
			if domain == "" {
				if d, ok := st.AppliedConfig["domain"].(string); ok {
					domain = d
				}
			}
			if domain == "" {
				continue
			}
			snap := getOrCreate(st.Provider, domain)
			for _, nsKey := range []string{"registrar_nameservers", "live_nameservers"} {
				if v, ok := st.Outputs[nsKey]; ok {
					if slice, ok := v.([]any); ok {
						cp := make([]any, len(slice))
						copy(cp, slice)
						if snap.Authority == nil {
							snap.Authority = map[string]any{}
						}
						snap.Authority[nsKey] = cp
					}
				}
			}

		default:
			continue
		}
	}

	// Sort by (provider, domain) for deterministic output.
	sort.Slice(order, func(i, j int) bool {
		if order[i].provider != order[j].provider {
			return order[i].provider < order[j].provider
		}
		return order[i].domain < order[j].domain
	})

	for _, k := range order {
		snap := *snapByKey[k]
		canonicalizeSnapshot(&snap)
		p.Snapshots = append(p.Snapshots, snap)
	}
	return p
}

func canonicalizeSnapshot(snap *Snapshot) {
	sort.SliceStable(snap.Records, func(i, j int) bool {
		return recordLess(snap.Records[i], snap.Records[j])
	})
	sortAuthorityStringSlices(snap.Authority)
}

func recordLess(a, b Record) bool {
	for _, cmp := range []struct{ a, b string }{
		{strings.ToUpper(a.Type), strings.ToUpper(b.Type)},
		{a.Type, b.Type},
		{strings.ToLower(a.Name), strings.ToLower(b.Name)},
		{a.Name, b.Name},
		{strings.ToLower(a.Value), strings.ToLower(b.Value)},
		{a.Value, b.Value},
	} {
		if cmp.a == cmp.b {
			continue
		}
		return cmp.a < cmp.b
	}
	if a.TTL != b.TTL {
		return a.TTL < b.TTL
	}
	if less, ok := optionalIntLess(a.Priority, b.Priority); ok {
		return less
	}
	if less, ok := optionalIntLess(a.Port, b.Port); ok {
		return less
	}
	if less, ok := optionalIntLess(a.Weight, b.Weight); ok {
		return less
	}
	if less, ok := optionalIntLess(a.Flags, b.Flags); ok {
		return less
	}
	if !strings.EqualFold(a.Tag, b.Tag) {
		return strings.ToLower(a.Tag) < strings.ToLower(b.Tag)
	}
	return a.Tag < b.Tag
}

func optionalIntLess(a, b *int) (bool, bool) {
	switch {
	case a == nil && b == nil:
		return false, false
	case a == nil:
		return true, true
	case b == nil:
		return false, true
	case *a != *b:
		return *a < *b, true
	default:
		return false, false
	}
}

func sortAuthorityStringSlices(authority map[string]any) {
	for _, key := range []string{
		"registrar_nameservers",
		"live_nameservers",
		"name_servers",
		"original_name_servers",
	} {
		value, ok := authority[key]
		if !ok {
			continue
		}
		if sorted, ok := sortedStringAnySlice(value); ok {
			authority[key] = sorted
		}
	}
}

func sortedStringAnySlice(value any) ([]any, bool) {
	var values []string
	switch v := value.(type) {
	case []any:
		values = make([]string, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			values[i] = s
		}
	case []string:
		values = append([]string(nil), v...)
	default:
		return nil, false
	}
	sort.SliceStable(values, func(i, j int) bool {
		li := strings.ToLower(values[i])
		lj := strings.ToLower(values[j])
		if li == lj {
			return values[i] < values[j]
		}
		return li < lj
	})
	out := make([]any, len(values))
	for i := range values {
		out[i] = values[i]
	}
	return out, true
}

// sanitizeDomainForID converts a domain string into an ID-safe slug:
// lowercase, runs of non-alphanumeric runes (incl. '.' and '/') replaced
// with a single '-', leading/trailing '-' trimmed.
func sanitizeDomainForID(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	inRun := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			inRun = false
		} else if !inRun {
			b.WriteByte('-')
			inRun = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// pickRecords returns the records slice from Outputs if non-empty,
// otherwise falls back to AppliedConfig.
func pickRecords(outputs, appliedConfig map[string]any) []any {
	if recs, ok := outputs["records"].([]any); ok && len(recs) > 0 {
		return recs
	}
	if recs, ok := appliedConfig["records"].([]any); ok {
		return recs
	}
	return nil
}

func mergeAuthority(snap *Snapshot, raw any) {
	authority, ok := raw.(map[string]any)
	if !ok {
		return
	}
	for key, value := range authority {
		if !authorityAllowList[key] {
			continue
		}
		if snap.Authority == nil {
			snap.Authority = map[string]any{}
		}
		snap.Authority[key] = cloneAuthorityValue(value)
	}
}

func cloneAuthorityValue(value any) any {
	switch v := value.(type) {
	case []any:
		cp := make([]any, len(v))
		copy(cp, v)
		return cp
	case []string:
		cp := make([]any, len(v))
		for i := range v {
			cp[i] = v[i]
		}
		return cp
	default:
		return v
	}
}

// recordFromMap converts a provider record map to a canonical Record.
// Value is resolved by the first-non-empty alias chain:
// "data" → "content" → "address" → "value"
func recordFromMap(m map[string]any) Record {
	r := Record{
		Type:  stringVal(m, "type"),
		Name:  stringVal(m, "name"),
		Value: valueAlias(m),
		TTL:   intVal(m, "ttl"),
		Tag:   stringVal(m, "tag"),
	}
	// I-1: store the value when the KEY is present regardless of its numeric
	// value — a present zero is meaningful (RFC-7505 null-MX priority=0,
	// SRV weight=0, port=0). Dropping zeros would silently corrupt the record.
	if v, ok := m["priority"]; ok {
		n := toInt(v)
		r.Priority = &n
	}
	if v, ok := m["port"]; ok {
		n := toInt(v)
		r.Port = &n
	}
	if v, ok := m["weight"]; ok {
		n := toInt(v)
		r.Weight = &n
	}
	if v, ok := m["flags"]; ok {
		n := toInt(v)
		r.Flags = &n
	}
	return r
}

// valueAlias resolves the canonical record value from provider-specific key names.
// DO/CF use "data", Hover uses "content", Namecheap uses "address"; canonical emits "value".
func valueAlias(m map[string]any) string {
	for _, k := range []string{"data", "content", "address", "value"} {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func stringVal(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func intVal(m map[string]any, key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	return toInt(v)
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	}
	return 0
}
