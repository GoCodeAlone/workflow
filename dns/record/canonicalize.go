package record

import "github.com/GoCodeAlone/workflow/interfaces"

// FromResourceStates converts imported IaC state into a canonical Portfolio.
// Reads each infra.dns ResourceState's records (Outputs preferred, else
// AppliedConfig), renaming provider-specific value keys to the canonical "value".
//
// Provider value-key divergence (verified against provider drivers):
//   - DigitalOcean + Cloudflare emit "data"
//   - Hover emits "content" (workflow-plugin-hover/internal/drivers/dns.go:538)
//   - Namecheap emits "address"
//
// The valueAlias helper resolves the first non-empty of: data → content → address → value.
// Non-infra.dns states are silently skipped.
func FromResourceStates(states []interfaces.ResourceState) Portfolio {
	p := Portfolio{Schema: SchemaV1}
	for _, st := range states {
		if st.Type != "infra.dns" {
			continue
		}
		recs := pickRecords(st.Outputs, st.AppliedConfig)
		snap := Snapshot{
			ID:       st.ID,
			Provider: st.Provider,
			Domain:   st.ProviderID,
		}
		// Fall back to AppliedConfig["domain"] if ProviderID is empty.
		if snap.Domain == "" {
			if d, ok := st.AppliedConfig["domain"].(string); ok {
				snap.Domain = d
			}
		}
		for _, raw := range recs {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			snap.Records = append(snap.Records, recordFromMap(m))
		}
		p.Snapshots = append(p.Snapshots, snap)
	}
	return p
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
	if v, ok := m["priority"]; ok {
		if n := toInt(v); n != 0 {
			r.Priority = &n
		}
	}
	if v, ok := m["port"]; ok {
		if n := toInt(v); n != 0 {
			r.Port = &n
		}
	}
	if v, ok := m["weight"]; ok {
		if n := toInt(v); n != 0 {
			r.Weight = &n
		}
	}
	if v, ok := m["flags"]; ok {
		if n := toInt(v); n != 0 {
			r.Flags = &n
		}
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
