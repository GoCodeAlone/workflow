package record

import "fmt"

// Record is the canonical, provider-neutral DNS record type.
// The Value field uses json:"value" to match scenario-88's fixture shape
// (fixture records use "value", NOT "data").
//
// knownTypes is advisory only — a portfolio is a SNAPSHOT of whatever the
// provider returns, so unknown/newer types (PTR, HTTPS, SVCB, TLSA, DNAME, …)
// MUST be preserved, never rejected. KnownType drives an optional warning only.
type Record struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	Value     string `json:"value"`
	TTL       int    `json:"ttl"`
	Priority  *int   `json:"priority,omitempty"`
	Port      *int   `json:"port,omitempty"`
	Weight    *int   `json:"weight,omitempty"`
	Flags     *int   `json:"flags,omitempty"`
	Tag       string `json:"tag,omitempty"`
	Proxied   *bool  `json:"proxied,omitempty"`
	Proxiable *bool  `json:"proxiable,omitempty"`
}

// Snapshot is a flat representation of one DNS zone at a point in time.
// One snapshot == one zone (matches scenario-88 fixture shape: flat, no zones[]).
type Snapshot struct {
	ID        string         `json:"id"`
	Provider  string         `json:"provider"`
	Domain    string         `json:"domain"`
	Authority map[string]any `json:"authority,omitempty"`
	Records   []Record       `json:"records"`
	Extra     map[string]any `json:"extra,omitempty"`
}

// Portfolio is the top-level export envelope for a canonical DNS catalog.
// Matches the "workflow.dns-portfolio.export.v1" schema used by scenario 88.
type Portfolio struct {
	Schema    string     `json:"schema"`
	Sanitized bool       `json:"sanitized,omitempty"`
	Snapshots []Snapshot `json:"snapshots"`
}

// SchemaV1 is the canonical schema identifier for a dns-portfolio export.
const SchemaV1 = "workflow.dns-portfolio.export.v1"

var knownTypes = map[string]bool{
	"A":     true,
	"AAAA":  true,
	"CNAME": true,
	"MX":    true,
	"TXT":   true,
	"NS":    true,
	"SRV":   true,
	"CAA":   true,
	"SOA":   true,
}

// KnownType reports whether t is a well-known DNS record type.
// Advisory only — unknown types are valid in a portfolio snapshot.
func KnownType(t string) bool { return knownTypes[t] }

// Validate enforces structural invariants on the Portfolio.
// It does NOT whitelist record types — unknown types (PTR, HTTPS, SVCB, …)
// are preserved. Only empty type and negative TTL are rejected.
func (p *Portfolio) Validate() error {
	if p.Schema != SchemaV1 {
		return fmt.Errorf("record: schema=%q want %q", p.Schema, SchemaV1)
	}
	for _, s := range p.Snapshots {
		if s.Provider == "" || s.Domain == "" {
			return fmt.Errorf("record: snapshot %q missing provider/domain", s.ID)
		}
		for _, r := range s.Records {
			if r.Type == "" {
				return fmt.Errorf("record: empty type in %s/%s", s.Domain, r.Name)
			}
			if r.TTL < 0 {
				return fmt.Errorf("record: negative ttl in %s/%s", s.Domain, r.Name)
			}
		}
	}
	return nil
}

// Equal reports whether two records are canonically equal, keying on
// (Type, Name, Value, TTL) and ignoring extra/optional fields like Priority.
func Equal(a, b Record) bool {
	return a.Type == b.Type && a.Name == b.Name && a.Value == b.Value && a.TTL == b.TTL
}
