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
	states := []interfaces.ResourceState{
		{Type: "infra.droplet", Provider: "digitalocean", ProviderID: "droplet-1"},
		{Type: "infra.spaces_key", Provider: "digitalocean", ProviderID: "key-1"},
	}
	p := record.FromResourceStates(states)
	if len(p.Snapshots) != 0 {
		t.Fatalf("non-dns states should be skipped; got %d snapshots", len(p.Snapshots))
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
