// Package gate implements pre-apply DNS policy enforcement. Operators
// configure a TXT policy at `_workflow-dns-policy.<zone>` declaring which
// owners may upsert which (name, type) tuples; the Gate reads that policy
// at apply time and denies actions that the policy does not permit.
//
// Relocated from workflow-plugin-infra/internal/dnsgate. Adapted to
// dispatch via interfaces.ResourceDriver.Read (not dnspolicy.Adapter) so
// the same gate logic works against any DNS provider that implements the
// strict-contract ResourceDriver interface — not just providers carrying
// the legacy libdns surface.
package gate

import (
	"context"
	"fmt"
	"sync"

	"github.com/GoCodeAlone/workflow/dns/policy"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// PolicyName returns the TXT name where policy lives for a zone — the same
// convention enforced by the parser side (workflow/dns/policy.Parse expects
// records named `_workflow-dns-policy.<zone>`).
func PolicyName(zone string) string { return "_workflow-dns-policy." + zone }

// policyCache holds per-zone parsed policies for the lifetime of one Gate
// holder (e.g. one wfctl apply invocation = one *CachingGate instance).
// Avoids re-fetching + re-parsing the TXT policy once per record when the
// surrounding apply touches many records in the same zone.
type policyCache struct {
	mu    sync.RWMutex
	zones map[string]*policy.Policy
}

// CachingGate is a Gate-call wrapper with per-zone caching. One per
// wfctl apply invocation; releases at end of invocation (no TTL). Use
// this when an apply touches multiple records under the same zone — the
// dns-policy TXT is fetched + parsed exactly once per zone instead of
// once per record. Mirrors the v1 dnsgate.CachingGate pattern.
type CachingGate struct{ c *policyCache }

// NewCachingGate returns a new CachingGate.
func NewCachingGate() *CachingGate {
	return &CachingGate{c: &policyCache{zones: map[string]*policy.Policy{}}}
}

// Check is the cached entry point — single GetTXT per zone per Gate.
// Returns nil when (zone, name, recordType, owner) is permitted; non-nil
// when the policy denies the action. Fails closed: a missing policy TXT
// (zero policy entries after parse) returns an error rather than allowing.
//
// Reader is the policy.DNSPolicyReader interface — either a libdns adapter
// (legacy) or a *DriverReader (wfctl-driven, this package). The narrow
// 2-method interface means new transports can be added without touching
// the gate logic.
func (g *CachingGate) Check(ctx context.Context, reader policy.DNSPolicyReader, zone, name, recordType, owner string) error {
	g.c.mu.RLock()
	cached, ok := g.c.zones[zone]
	g.c.mu.RUnlock()
	if !ok {
		rrs, err := reader.GetTXT(ctx, PolicyName(zone))
		if err != nil {
			return fmt.Errorf("dnsgate: fetch policy: %w", err)
		}
		parsed, perr := policy.Parse(zone, rrs)
		if perr != nil {
			return perr
		}
		if len(parsed.Entries) == 0 {
			return fmt.Errorf("dnsgate: fail-closed — no policy found at %s", PolicyName(zone))
		}
		cached = parsed
		g.c.mu.Lock()
		g.c.zones[zone] = cached
		g.c.mu.Unlock()
	}
	return cached.CheckAllowed(name, recordType, owner)
}

// Gate is the uncached entry point (one GetTXT per call). Use this for
// one-off invocations (CLI commands, integration tests). For step handlers
// or apply loops processing many records in one go, use NewCachingGate +
// Check so the policy TXT is read at most once per zone.
func Gate(ctx context.Context, reader policy.DNSPolicyReader, zone, name, recordType, owner string) error {
	return NewCachingGate().Check(ctx, reader, zone, name, recordType, owner)
}

// DriverReader adapts an interfaces.ResourceDriver to the full
// policy.DNSPolicyReader interface so the Gate can read TXT records AND
// the dns-policy mutating commands (set / transfer-ownership) can write
// them — all via the strict-contract driver path (no libdns dependency).
//
// GetTXT scans Outputs["records"] for TXT records matching the given
// name. UpsertTXT issues a Driver.Update against a synthesized
// ResourceSpec whose records list replaces all TXT entries at the target
// name. Both halves are scoped narrowly to the policy-TXT use case; the
// general DNS-record CRUD surface is `wfctl infra apply`.
type DriverReader struct {
	// Driver is the resolved ResourceDriver for resource type "infra.dns".
	// Caller is responsible for getting the right driver (via
	// IaCProvider.ResourceDriver("infra.dns")) before constructing the
	// reader — keeps this adapter's concerns narrow.
	Driver interfaces.ResourceDriver
	// Zone is the FQDN of the zone whose policy is being read. Used as
	// ResourceRef.ProviderID — the DNS provider plugins (DO, CF, NC,
	// Hover) all accept zone-name-as-ID for the infra.dns resource type.
	Zone string
}

// GetTXT implements policy.DNSPolicyReader. Reads the zone via
// Driver.Read, then scans Outputs["records"] for TXT records whose name
// matches the requested policy name. Returns an empty (nil) slice when
// the zone has no matching TXT records — the caller (Gate.Check) handles
// the "no policy" case by failing closed.
//
// Tolerates both `[]map[string]any` and `[]any` (with element-wise
// type-assertion) for the Outputs["records"] entry — different provider
// plugins surface the records slice with slightly different concrete
// types depending on whether the value passed through a structpb roundtrip
// or stayed Go-native within the same process. Either shape works.
func (r *DriverReader) GetTXT(ctx context.Context, name string) ([]string, error) {
	if r.Driver == nil {
		return nil, fmt.Errorf("dnsgate.DriverReader: nil Driver")
	}
	if r.Zone == "" {
		return nil, fmt.Errorf("dnsgate.DriverReader: empty Zone")
	}
	out, err := r.Driver.Read(ctx, interfaces.ResourceRef{Type: "infra.dns", ProviderID: r.Zone})
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, nil
	}
	return extractTXTValues(out.Outputs, name), nil
}

// extractTXTValues handles the two records-slice concrete-type variants
// produced by DNS provider plugins. Tolerant: returns nil for any shape
// that doesn't carry record entries.
func extractTXTValues(outputs map[string]any, recordName string) []string {
	if outputs == nil {
		return nil
	}
	var values []string
	switch recs := outputs["records"].(type) {
	case []map[string]any:
		for _, rec := range recs {
			if matchTXT(rec, recordName) {
				if v, ok := rec["data"].(string); ok {
					values = append(values, v)
				}
			}
		}
	case []any:
		for _, raw := range recs {
			rec, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if matchTXT(rec, recordName) {
				if v, ok := rec["data"].(string); ok {
					values = append(values, v)
				}
			}
		}
	}
	return values
}

func matchTXT(rec map[string]any, recordName string) bool {
	t, _ := rec["type"].(string)
	n, _ := rec["name"].(string)
	return t == "TXT" && n == recordName
}

// UpsertTXT implements the write half of policy.DNSPolicyReader so
// DriverReader satisfies the full interface used by the wfctl dns-policy
// mutating commands AND can be passed to CachingGate.Check (which takes a
// DNSPolicyReader, not a narrower Reader sub-interface).
//
// Strategy: Read the current zone via Driver.Read, rewrite the records
// slice replacing all TXT entries at `name` with the supplied values (one
// TXT RR per value), then call Driver.Update with a synthesized
// ResourceSpec carrying the new records list.
//
// Limitations: the synthesized ResourceSpec carries only `domain` +
// `records` fields. Providers requiring additional zone-level config on
// Update (e.g. CF "type" / "settings") may reject. The narrow scope is
// intentional — this adapter exists to manage the policy TXT specifically,
// not to be a general DNS-record CRUD surface (the `wfctl infra apply`
// path covers general record CRUD via config-declared records).
func (r *DriverReader) UpsertTXT(ctx context.Context, name string, values []string, ttl int) error {
	if r.Driver == nil {
		return fmt.Errorf("dnsgate.DriverReader: nil Driver")
	}
	if r.Zone == "" {
		return fmt.Errorf("dnsgate.DriverReader: empty Zone")
	}
	out, err := r.Driver.Read(ctx, interfaces.ResourceRef{Type: "infra.dns", ProviderID: r.Zone})
	if err != nil {
		return fmt.Errorf("dnsgate.UpsertTXT: read current zone: %w", err)
	}
	var records []map[string]any
	if out != nil {
		records = recordsToMaps(out.Outputs["records"])
	}
	updated := upsertTXTInRecords(records, name, values, ttl)
	spec := interfaces.ResourceSpec{
		Name: r.Zone,
		Type: "infra.dns",
		Config: map[string]any{
			"domain":  r.Zone,
			"records": updated,
		},
	}
	_, err = r.Driver.Update(ctx, interfaces.ResourceRef{Type: "infra.dns", ProviderID: r.Zone}, spec)
	if err != nil {
		return fmt.Errorf("dnsgate.UpsertTXT: update zone: %w", err)
	}
	return nil
}

// recordsToMaps normalises both concrete-type variants of the records
// slice ([]map[string]any and []any-of-map) into the typed form needed
// for Update.
func recordsToMaps(records any) []map[string]any {
	switch v := records.(type) {
	case []map[string]any:
		return append([]map[string]any(nil), v...)
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, raw := range v {
			if rec, ok := raw.(map[string]any); ok {
				out = append(out, rec)
			}
		}
		return out
	}
	return nil
}

// upsertTXTInRecords removes all TXT records at `name` from the slice and
// appends one fresh TXT record per value. Idempotent on the policy shape:
// re-running with the same values produces an equivalent records list
// (modulo slice order).
func upsertTXTInRecords(records []map[string]any, name string, values []string, ttl int) []map[string]any {
	out := make([]map[string]any, 0, len(records))
	for _, rec := range records {
		t, _ := rec["type"].(string)
		n, _ := rec["name"].(string)
		if t == "TXT" && n == name {
			continue // drop existing TXT at this name; replaced below
		}
		out = append(out, rec)
	}
	for _, v := range values {
		out = append(out, map[string]any{
			"type": "TXT",
			"name": name,
			"data": v,
			"ttl":  ttl,
		})
	}
	return out
}

// Compile-time assertion that *DriverReader satisfies the full
// policy.DNSPolicyReader contract (GetTXT + UpsertTXT). Catches a
// regression where one half of the interface gets accidentally removed
// (the failure mode that broke PR 7's first push).
var _ policy.DNSPolicyReader = (*DriverReader)(nil)
