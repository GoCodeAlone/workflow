package record_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/dns/record"
	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestFromResourceStatesAliasesValueKey(t *testing.T) {
	states := []interfaces.ResourceState{
		{
			Type:       "infra.dns",
			Provider:   "digitalocean",
			ProviderID: "do.test",
			Outputs: map[string]any{
				"records": []any{
					map[string]any{"type": "A", "name": "@", "data": "192.0.2.1", "ttl": 300},
				},
			},
		},
		{
			Type:       "infra.dns",
			Provider:   "hover",
			ProviderID: "hv.test",
			Outputs: map[string]any{
				"records": []any{
					map[string]any{"type": "A", "name": "@", "content": "192.0.2.2", "ttl": 300},
				},
			},
		},
		{
			Type:       "infra.dns",
			Provider:   "namecheap",
			ProviderID: "nc.test",
			Outputs: map[string]any{
				"records": []any{
					map[string]any{"type": "A", "name": "@", "address": "192.0.2.3", "ttl": 300},
				},
			},
		},
		{Type: "infra.droplet", Provider: "digitalocean"}, // skipped
	}
	p := record.FromResourceStates(states)
	if len(p.Snapshots) != 3 {
		t.Fatalf("want 3 dns snapshots, got %d", len(p.Snapshots))
	}
	for _, s := range p.Snapshots {
		if s.Records[0].Value == "" {
			t.Fatalf("provider %s: empty Value (alias-map failed)", s.Provider)
		}
	}
}

func TestFromResourceStatesSkipsNonDNS(t *testing.T) {
	// infra.compute is a truly-unknown type and must be silently skipped.
	// infra.dns_delegation is now CONSUMED (not skipped), so it produces a snapshot.
	states := []interfaces.ResourceState{
		{Type: "infra.compute", Provider: "digitalocean", ProviderID: "vm-1"},
		{Type: "infra.spaces_key", Provider: "digitalocean", ProviderID: "key-1"},
	}
	p := record.FromResourceStates(states)
	if len(p.Snapshots) != 0 {
		t.Fatalf("genuinely-unknown states should be skipped; got %d snapshots", len(p.Snapshots))
	}
}

func TestFromResourceStates_DelegationPopulatesAuthority(t *testing.T) {
	states := []interfaces.ResourceState{
		{
			Type:       "infra.dns_delegation",
			Provider:   "hover",
			ProviderID: "x.com",
			Outputs: map[string]any{
				"registrar_nameservers": []any{"ns1.dnsimple.com"},
				"live_nameservers":      []any{"ns1.digitalocean.com"},
			},
		},
	}
	p := record.FromResourceStates(states)
	if len(p.Snapshots) != 1 {
		t.Fatalf("want 1 snapshot from delegation state; got %d", len(p.Snapshots))
	}
	snap := p.Snapshots[0]
	if snap.ID != "hover-x-com" {
		t.Errorf("want snap.ID == %q; got %q", "hover-x-com", snap.ID)
	}
	if snap.Records == nil {
		t.Errorf("Records must be non-nil (empty slice, not null)")
	}
	if len(snap.Records) != 0 {
		t.Errorf("want 0 records; got %d", len(snap.Records))
	}
	rns, ok := snap.Authority["registrar_nameservers"]
	if !ok {
		t.Fatalf("Authority missing registrar_nameservers")
	}
	wantRNS := []any{"ns1.dnsimple.com"}
	rnsSlice, ok := rns.([]any)
	if !ok || len(rnsSlice) != len(wantRNS) || rnsSlice[0] != wantRNS[0] {
		t.Errorf("registrar_nameservers = %v; want %v", rns, wantRNS)
	}
	lns, ok := snap.Authority["live_nameservers"]
	if !ok {
		t.Fatalf("Authority missing live_nameservers")
	}
	wantLNS := []any{"ns1.digitalocean.com"}
	lnsSlice, ok := lns.([]any)
	if !ok || len(lnsSlice) != len(wantLNS) || lnsSlice[0] != wantLNS[0] {
		t.Errorf("live_nameservers = %v; want %v", lns, wantLNS)
	}
}

func TestFromResourceStates_MergesBothLayersByDomain(t *testing.T) {
	states := []interfaces.ResourceState{
		{
			Type:       "infra.dns",
			Provider:   "hover",
			ProviderID: "x.com",
			Outputs: map[string]any{
				"records": []any{
					map[string]any{"type": "A", "name": "@", "content": "1.2.3.4", "ttl": 300},
				},
			},
		},
		{
			Type:       "infra.dns_delegation",
			Provider:   "hover",
			ProviderID: "x.com",
			Outputs: map[string]any{
				"registrar_nameservers": []any{"ns1.dnsimple.com"},
				"live_nameservers":      []any{"ns1.digitalocean.com"},
			},
		},
	}
	p := record.FromResourceStates(states)
	if len(p.Snapshots) != 1 {
		t.Fatalf("want exactly 1 merged snapshot; got %d", len(p.Snapshots))
	}
	snap := p.Snapshots[0]
	if snap.ID != "hover-x-com" {
		t.Errorf("want snap.ID == %q; got %q", "hover-x-com", snap.ID)
	}
	if len(snap.Records) == 0 {
		t.Errorf("want non-empty Records from infra.dns state")
	}
	if _, ok := snap.Authority["registrar_nameservers"]; !ok {
		t.Errorf("want Authority[registrar_nameservers] from infra.dns_delegation state")
	}
	if _, ok := snap.Authority["live_nameservers"]; !ok {
		t.Errorf("want Authority[live_nameservers] from infra.dns_delegation state")
	}
}

func TestFromResourceStates_DelegationOnlyDomain(t *testing.T) {
	states := []interfaces.ResourceState{
		{
			Type:       "infra.dns_delegation",
			Provider:   "hover",
			ProviderID: "x.com",
			Outputs: map[string]any{
				"registrar_nameservers": []any{"ns1.dnsimple.com"},
				"live_nameservers":      []any{"ns1.digitalocean.com"},
			},
		},
	}
	p := record.FromResourceStates(states)
	if len(p.Snapshots) != 1 {
		t.Fatalf("want 1 authority-only snapshot; got %d", len(p.Snapshots))
	}
	snap := p.Snapshots[0]
	if snap.Records == nil {
		t.Errorf("Records must be non-nil empty slice (so JSON marshals to [] not null)")
	}
	if len(snap.Records) != 0 {
		t.Errorf("want 0 records; got %d", len(snap.Records))
	}
	if snap.Authority == nil {
		t.Errorf("want non-nil Authority")
	}
}

func TestFromResourceStatesUsesOutputsPreferredOverAppliedConfig(t *testing.T) {
	states := []interfaces.ResourceState{
		{
			Type:       "infra.dns",
			Provider:   "digitalocean",
			ProviderID: "do.test",
			Outputs: map[string]any{
				"records": []any{
					map[string]any{"type": "A", "name": "@", "data": "192.0.2.10", "ttl": 300},
				},
			},
			AppliedConfig: map[string]any{
				"records": []any{
					map[string]any{"type": "A", "name": "@", "data": "10.0.0.1", "ttl": 300},
				},
			},
		},
	}
	p := record.FromResourceStates(states)
	if len(p.Snapshots) != 1 {
		t.Fatalf("want 1 snapshot; got %d", len(p.Snapshots))
	}
	// Outputs takes priority over AppliedConfig
	if p.Snapshots[0].Records[0].Value != "192.0.2.10" {
		t.Fatalf("want Outputs value 192.0.2.10; got %s", p.Snapshots[0].Records[0].Value)
	}
}

// TestFromResourceStatesPreservesZeroValues pins I-1: a present key with a
// zero value (null-MX RFC-7505 priority=0, SRV weight=0, SRV port=0) must
// round-trip as a non-nil pointer to 0 — NOT be dropped to nil. The old
// `if n:=toInt(v); n!=0` logic silently lost these legitimate zeros.
func TestFromResourceStatesPreservesZeroValues(t *testing.T) {
	states := []interfaces.ResourceState{
		{
			Type:       "infra.dns",
			Provider:   "digitalocean",
			ProviderID: "do.test",
			Outputs: map[string]any{
				"records": []any{
					// RFC-7505 null MX: priority 0, target "."
					map[string]any{"type": "MX", "name": "@", "data": ".", "ttl": 300, "priority": 0},
					// SRV with zero weight + zero port (valid wire values)
					map[string]any{"type": "SRV", "name": "_sip._tcp", "data": "sip.example.com.", "ttl": 300, "priority": 0, "weight": 0, "port": 0},
				},
			},
		},
	}
	p := record.FromResourceStates(states)
	if len(p.Snapshots) != 1 || len(p.Snapshots[0].Records) != 2 {
		t.Fatalf("want 1 snapshot with 2 records; got %d snapshots", len(p.Snapshots))
	}
	mx := p.Snapshots[0].Records[0]
	if mx.Priority == nil {
		t.Errorf("null-MX priority dropped to nil; want &0 (RFC-7505)")
	} else if *mx.Priority != 0 {
		t.Errorf("MX priority = %d; want 0", *mx.Priority)
	}
	srv := p.Snapshots[0].Records[1]
	if srv.Priority == nil || *srv.Priority != 0 {
		t.Errorf("SRV priority = %v; want &0", srv.Priority)
	}
	if srv.Weight == nil || *srv.Weight != 0 {
		t.Errorf("SRV weight = %v; want &0", srv.Weight)
	}
	if srv.Port == nil || *srv.Port != 0 {
		t.Errorf("SRV port = %v; want &0", srv.Port)
	}
}

// TestFromResourceStatesOmitsAbsentOptionalFields pins the complement of I-1:
// when an optional key is ABSENT from the provider map, its pointer stays nil
// (so json omitempty drops it). Only present-with-zero should become &0.
func TestFromResourceStatesOmitsAbsentOptionalFields(t *testing.T) {
	states := []interfaces.ResourceState{
		{
			Type:       "infra.dns",
			Provider:   "digitalocean",
			ProviderID: "do.test",
			Outputs: map[string]any{
				"records": []any{
					map[string]any{"type": "A", "name": "@", "data": "192.0.2.1", "ttl": 300},
				},
			},
		},
	}
	p := record.FromResourceStates(states)
	r := p.Snapshots[0].Records[0]
	if r.Priority != nil || r.Port != nil || r.Weight != nil || r.Flags != nil {
		t.Errorf("absent optional fields should stay nil; got priority=%v port=%v weight=%v flags=%v",
			r.Priority, r.Port, r.Weight, r.Flags)
	}
}
