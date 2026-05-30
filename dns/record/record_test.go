package record_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/dns/record"
)

func TestRecordRoundTripMatchesFixtureSubset(t *testing.T) {
	p := record.Portfolio{
		Schema: "workflow.dns-portfolio.export.v1",
		Snapshots: []record.Snapshot{{
			ID: "do-example", Provider: "digitalocean", Domain: "example.com",
			Records: []record.Record{{Type: "A", Name: "@", Value: "192.0.2.10", TTL: 900}},
		}},
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	// record serializes "value" not "data"; snapshot is flat (no zones[])
	if !strings.Contains(string(b), `"value":"192.0.2.10"`) {
		t.Fatalf("want value field, got %s", b)
	}
	if strings.Contains(string(b), `"zones"`) {
		t.Fatalf("snapshot must be flat, no zones[]: %s", b)
	}
}

func TestValidateRejectsBadRecords(t *testing.T) {
	// empty type → error
	p := record.Portfolio{
		Schema: record.SchemaV1,
		Snapshots: []record.Snapshot{{
			Provider: "digitalocean", Domain: "example.com",
			Records: []record.Record{{Type: "", Name: "@", Value: "1.2.3.4", TTL: 300}},
		}},
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected error for empty record type; got nil")
	}

	// negative TTL → error
	p2 := record.Portfolio{
		Schema: record.SchemaV1,
		Snapshots: []record.Snapshot{{
			Provider: "digitalocean", Domain: "example.com",
			Records: []record.Record{{Type: "A", Name: "@", Value: "1.2.3.4", TTL: -1}},
		}},
	}
	if err := p2.Validate(); err == nil {
		t.Fatal("expected error for negative TTL; got nil")
	}

	// unknown type PTR → NO error (open-set, preserved)
	p3 := record.Portfolio{
		Schema: record.SchemaV1,
		Snapshots: []record.Snapshot{{
			Provider: "digitalocean", Domain: "example.com",
			Records: []record.Record{{Type: "PTR", Name: "@", Value: "1.2.3.4.in-addr.arpa.", TTL: 300}},
		}},
	}
	if err := p3.Validate(); err != nil {
		t.Fatalf("PTR record should be preserved (unknown type OK); got error: %v", err)
	}

	// HTTPS type → NO error
	p4 := record.Portfolio{
		Schema: record.SchemaV1,
		Snapshots: []record.Snapshot{{
			Provider: "digitalocean", Domain: "example.com",
			Records: []record.Record{{Type: "HTTPS", Name: "@", Value: "1 . alpn=h2", TTL: 300}},
		}},
	}
	if err := p4.Validate(); err != nil {
		t.Fatalf("HTTPS record should be preserved (unknown type OK); got error: %v", err)
	}
}

func TestEqualIgnoresExtra(t *testing.T) {
	a := record.Record{Type: "A", Name: "@", Value: "1.2.3.4", TTL: 300}
	b := record.Record{Type: "A", Name: "@", Value: "1.2.3.4", TTL: 300}
	if !record.Equal(a, b) {
		t.Fatal("identical records should be Equal")
	}

	// extra fields (Priority) differ but core (type,name,value,ttl) same → Equal
	pri := 10
	c := record.Record{Type: "A", Name: "@", Value: "1.2.3.4", TTL: 300, Priority: &pri}
	if !record.Equal(a, c) {
		t.Fatal("records differing only in Priority should be Equal (Equal ignores extra fields)")
	}

	// different value → not Equal
	d := record.Record{Type: "A", Name: "@", Value: "5.6.7.8", TTL: 300}
	if record.Equal(a, d) {
		t.Fatal("records with different Value should not be Equal")
	}
}
