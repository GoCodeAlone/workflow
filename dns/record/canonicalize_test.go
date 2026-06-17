package record_test

import (
	"reflect"
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

func TestFromResourceStates_DNSStatePreservesProviderAuthority(t *testing.T) {
	states := []interfaces.ResourceState{
		{
			Type:       "infra.dns",
			Provider:   "cloudflare",
			ProviderID: "example.com",
			Outputs: map[string]any{
				"records": []any{
					map[string]any{"type": "A", "name": "example.com", "data": "192.0.2.10", "ttl": 300},
				},
				"authority": map[string]any{
					"role":                  "target_authoritative_dns",
					"dns_host":              "Cloudflare",
					"name_servers":          []any{"ada.ns.cloudflare.com", "bob.ns.cloudflare.com"},
					"original_name_servers": []any{"ns1.hover.com", "ns2.hover.com"},
				},
			},
		},
	}
	p := record.FromResourceStates(states)
	if len(p.Snapshots) != 1 {
		t.Fatalf("want 1 snapshot from DNS state; got %d", len(p.Snapshots))
	}
	snap := p.Snapshots[0]
	if snap.Authority == nil {
		t.Fatal("Authority missing provider-supplied DNS authority")
	}
	if got := snap.Authority["role"]; got != "target_authoritative_dns" {
		t.Fatalf("Authority[role] = %v; want target_authoritative_dns", got)
	}
	nameServers, ok := snap.Authority["name_servers"].([]any)
	if !ok || len(nameServers) != 2 || nameServers[0] != "ada.ns.cloudflare.com" {
		t.Fatalf("Authority[name_servers] = %#v; want Cloudflare assigned nameservers", snap.Authority["name_servers"])
	}
	original, ok := snap.Authority["original_name_servers"].([]any)
	if !ok || len(original) != 2 || original[0] != "ns1.hover.com" {
		t.Fatalf("Authority[original_name_servers] = %#v; want previous nameservers", snap.Authority["original_name_servers"])
	}
}

func TestFromResourceStates_DelegationAcceptsStringSlices(t *testing.T) {
	states := []interfaces.ResourceState{
		{
			Type:       "infra.dns_delegation",
			Provider:   "namecheap",
			ProviderID: "example.com",
			Outputs: map[string]any{
				"registrar_nameservers": []string{"dns1.registrar-servers.com", "dns2.registrar-servers.com"},
			},
		},
	}
	p := record.FromResourceStates(states)
	if len(p.Snapshots) != 1 {
		t.Fatalf("want 1 snapshot from delegation state; got %d", len(p.Snapshots))
	}
	ns, ok := p.Snapshots[0].Authority["registrar_nameservers"]
	if !ok {
		t.Fatalf("Authority missing registrar_nameservers")
	}
	nsSlice, ok := ns.([]any)
	if !ok || len(nsSlice) != 2 || nsSlice[0] != "dns1.registrar-servers.com" || nsSlice[1] != "dns2.registrar-servers.com" {
		t.Fatalf("registrar_nameservers = %#v, want two nameservers", ns)
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

func TestFromResourceStatesCanonicalizesRecordAndAuthorityOrder(t *testing.T) {
	states := []interfaces.ResourceState{
		{
			Type:       "infra.dns",
			Provider:   "namecheap",
			ProviderID: "example.com",
			Outputs: map[string]any{
				"records": []any{
					map[string]any{"type": "TXT", "name": "z", "address": "last", "ttl": 300},
					map[string]any{"type": "MX", "name": "@", "address": "mail.example.com.", "ttl": 300, "priority": 10},
					map[string]any{"type": "TXT", "name": "_dmarc", "address": "v=DMARC1; p=none", "ttl": 300},
					map[string]any{"type": "A", "name": "www", "address": "192.0.2.12", "ttl": 300},
					map[string]any{"type": "A", "name": "WWW", "address": "192.0.2.11", "ttl": 300},
					map[string]any{"type": "A", "name": "@", "address": "192.0.2.10", "ttl": 300},
				},
				"authority": map[string]any{
					"name_servers":          []any{"z.ns.example.com", "A.ns.example.com"},
					"original_name_servers": []string{"ns2.hover.com", "ns1.hover.com"},
				},
			},
		},
		{
			Type:       "infra.dns_delegation",
			Provider:   "namecheap",
			ProviderID: "example.com",
			Outputs: map[string]any{
				"registrar_nameservers": []any{"ns2.registrar.example", "ns1.registrar.example"},
				"live_nameservers":      []any{"ns2.live.example", "ns1.live.example"},
			},
		},
	}

	p := record.FromResourceStates(states)
	if len(p.Snapshots) != 1 {
		t.Fatalf("want 1 snapshot; got %d", len(p.Snapshots))
	}

	gotRecords := make([]string, len(p.Snapshots[0].Records))
	for i, r := range p.Snapshots[0].Records {
		gotRecords[i] = r.Type + " " + r.Name + " " + r.Value
	}
	wantRecords := []string{
		"A @ 192.0.2.10",
		"A WWW 192.0.2.11",
		"A www 192.0.2.12",
		"MX @ mail.example.com.",
		"TXT _dmarc v=DMARC1; p=none",
		"TXT z last",
	}
	if !reflect.DeepEqual(gotRecords, wantRecords) {
		t.Fatalf("records order = %#v; want %#v", gotRecords, wantRecords)
	}

	for key, want := range map[string][]any{
		"name_servers":          {"A.ns.example.com", "z.ns.example.com"},
		"original_name_servers": {"ns1.hover.com", "ns2.hover.com"},
		"registrar_nameservers": {"ns1.registrar.example", "ns2.registrar.example"},
		"live_nameservers":      {"ns1.live.example", "ns2.live.example"},
	} {
		got, ok := p.Snapshots[0].Authority[key].([]any)
		if !ok {
			t.Fatalf("Authority[%s] = %#v; want []any", key, p.Snapshots[0].Authority[key])
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Authority[%s] = %#v; want %#v", key, got, want)
		}
	}
}
