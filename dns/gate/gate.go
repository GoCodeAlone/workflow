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

// DriverReader adapts an interfaces.ResourceDriver to the
// policy.DNSPolicyReader interface so the Gate can read TXT records via
// the strict-contract driver path. Scans the zone's Outputs["records"]
// for TXT records matching the given name. Read-only: UpsertTXT delegates
// to a sister mutate path (Driver.Update with a synthesized spec); kept
// out of this adapter because the gate only ever reads. The dns-policy
// command surface owns the write path separately.
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
