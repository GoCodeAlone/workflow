package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/dns/record"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestDumpPortfolioToFile pins the --format portfolio output contract:
// dumpPortfolioToFile produces a JSON file whose top-level "schema" is
// "workflow.dns-portfolio.export.v1" and whose snapshots[0].records[0].value
// is set (alias-map collapsed the provider "data" key).
func TestDumpPortfolioToFile(t *testing.T) {
	store := &fakeStateStore{}
	_ = store.SaveResource(context.Background(), interfaces.ResourceState{
		ID:         "do-example-com",
		Name:       "do-example-com",
		Type:       "infra.dns",
		Provider:   "digitalocean",
		ProviderID: "example.com",
		Outputs: map[string]any{
			"records": []any{
				map[string]any{"type": "A", "name": "@", "data": "192.0.2.10", "ttl": 300},
			},
		},
	})
	dir := t.TempDir()
	out := filepath.Join(dir, "portfolio.json")
	if err := dumpPortfolioToFile(context.Background(), store, out, false); err != nil {
		t.Fatalf("dumpPortfolioToFile: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Must have the canonical schema field
	if !strings.Contains(string(data), record.SchemaV1) {
		t.Errorf("portfolio missing schema %q: %s", record.SchemaV1, data)
	}
	// Must use "value" key, not "data"
	if !strings.Contains(string(data), `"value"`) {
		t.Errorf("portfolio missing 'value' key: %s", data)
	}
	if strings.Contains(string(data), `"resources"`) {
		t.Errorf("portfolio format must not have 'resources' key (that is --format state): %s", data)
	}
	// Verify snapshots[0].records[0].value is set
	var p record.Portfolio
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("unmarshal portfolio: %v", err)
	}
	if len(p.Snapshots) != 1 {
		t.Fatalf("want 1 snapshot; got %d", len(p.Snapshots))
	}
	if len(p.Snapshots[0].Records) == 0 {
		t.Fatal("want records in snapshot; got none")
	}
	if p.Snapshots[0].Records[0].Value == "" {
		t.Fatal("snapshots[0].records[0].value is empty")
	}
}

// TestDumpPortfolioToFile_WithSanitize pins I-3: dumpPortfolioToFile(...,true)
// with a genuinely-public IP in a record produces a portfolio with
// Sanitized==true and the record value redacted to an RFC-5737 example IP.
func TestDumpPortfolioToFile_WithSanitize(t *testing.T) {
	store := &fakeStateStore{}
	_ = store.SaveResource(context.Background(), interfaces.ResourceState{
		ID:         "do-example-com",
		Name:       "do-example-com",
		Type:       "infra.dns",
		Provider:   "digitalocean",
		ProviderID: "example.com",
		Outputs: map[string]any{
			"records": []any{
				// 8.8.8.8 is genuinely public (Google DNS), NOT RFC-5737.
				map[string]any{"type": "A", "name": "@", "data": "8.8.8.8", "ttl": 300},
			},
		},
	})
	dir := t.TempDir()
	out := filepath.Join(dir, "portfolio.json")
	if err := dumpPortfolioToFile(context.Background(), store, out, true); err != nil {
		t.Fatalf("dumpPortfolioToFile sanitize: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var p record.Portfolio
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("unmarshal portfolio: %v", err)
	}
	if !p.Sanitized {
		t.Error("sanitized portfolio must have sanitized==true")
	}
	if len(p.Snapshots) != 1 || len(p.Snapshots[0].Records) == 0 {
		t.Fatalf("want 1 snapshot with records; got %d snapshots", len(p.Snapshots))
	}
	got := p.Snapshots[0].Records[0].Value
	if got == "8.8.8.8" {
		t.Error("public IP 8.8.8.8 was NOT redacted in sanitized dump")
	}
	if !strings.HasPrefix(got, "192.0.2.") && !strings.HasPrefix(got, "198.51.100.") && !strings.HasPrefix(got, "203.0.113.") {
		t.Errorf("sanitized A value = %q; want an RFC-5737 example IP", got)
	}
}

// TestDumpStateToFile_StillWorksAsDefault pins that --format state (the
// default) still produces {"resources":[...]} format unchanged.
func TestDumpStateToFile_StillWorksAsDefault(t *testing.T) {
	store := &fakeStateStore{}
	_ = store.SaveResource(context.Background(), interfaces.ResourceState{
		Name: "alpha", Type: "infra.dns", ProviderID: "alpha.test",
	})
	dir := t.TempDir()
	out := filepath.Join(dir, "state.json")
	if err := dumpStateToFile(context.Background(), store, out); err != nil {
		t.Fatalf("dumpStateToFile: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), `"resources"`) {
		t.Errorf("state format missing 'resources' key: %s", data)
	}
}

// TestImportAllFormatFlagRejectsUnknown pins that --format with an
// unrecognized value returns an error.
func TestImportAllFormatFlagRejectsUnknown(t *testing.T) {
	err := runInfraImportAll([]string{"--provider", "do", "--type", "infra.dns", "--format", "xml"})
	if err == nil {
		t.Fatal("want error for unknown --format; got nil")
	}
	if !strings.Contains(err.Error(), "format") {
		t.Errorf("error should mention 'format'; got: %v", err)
	}
}

// TestImportAllSanitizeFlagRequiresPortfolioFormat pins that --sanitize
// is rejected unless --format portfolio is also set.
func TestImportAllSanitizeFlagRequiresPortfolioFormat(t *testing.T) {
	err := runInfraImportAll([]string{"--provider", "do", "--type", "infra.dns", "--sanitize"})
	if err == nil {
		t.Fatal("want error for --sanitize without --format portfolio; got nil")
	}
	if !strings.Contains(err.Error(), "sanitize") {
		t.Errorf("error should mention 'sanitize'; got: %v", err)
	}
}

// TestBuildResourceStateFromImport_TypeNamespacedID pins the CRITICAL-1 fix:
// importing infra.dns and infra.dns_delegation for the SAME domain must
// produce DISTINCT state IDs (and therefore distinct on-disk filenames) so
// that a second import does not overwrite the first. ProviderID must stay the
// bare domain in both cases — record.FromResourceStates keys the snapshot
// domain on ProviderID, not on ID.
func TestBuildResourceStateFromImport_TypeNamespacedID(t *testing.T) {
	imported := &interfaces.ResourceState{
		ProviderID: "example.com",
		Type:       "infra.dns",
	}

	dnsState, err := buildResourceStateFromImport("example.com", "example.com", "infra.dns", "hover", imported)
	if err != nil {
		t.Fatalf("buildResourceStateFromImport infra.dns: %v", err)
	}

	delegImported := &interfaces.ResourceState{
		ProviderID: "example.com",
		Type:       "infra.dns_delegation",
	}
	delegState, err := buildResourceStateFromImport("example.com", "example.com", "infra.dns_delegation", "hover", delegImported)
	if err != nil {
		t.Fatalf("buildResourceStateFromImport infra.dns_delegation: %v", err)
	}

	// IDs must be distinct so SaveResource writes to different filenames.
	if dnsState.ID == delegState.ID {
		t.Errorf("infra.dns and infra.dns_delegation produced the same ID %q; want distinct IDs", dnsState.ID)
	}

	// ProviderID must remain the bare domain so FromResourceStates can group
	// both states into a single snapshot.
	if dnsState.ProviderID != "example.com" {
		t.Errorf("infra.dns ProviderID = %q; want %q", dnsState.ProviderID, "example.com")
	}
	if delegState.ProviderID != "example.com" {
		t.Errorf("infra.dns_delegation ProviderID = %q; want %q", delegState.ProviderID, "example.com")
	}

	// Verify the on-disk filenames are also distinct via sanitizeStateID.
	dnsFname := sanitizeStateID(dnsState.ID) + ".json"
	delegFname := sanitizeStateID(delegState.ID) + ".json"
	if dnsFname == delegFname {
		t.Errorf("sanitized filenames are the same %q; want distinct files", dnsFname)
	}
}

// TestDumpPortfolio_MergesDnsAndDelegationForSameDomain pins the end-to-end
// portfolio merge contract (CRITICAL-1 class): when BOTH an infra.dns state
// and an infra.dns_delegation state for the same domain are present in the
// store (using type-namespaced IDs so they coexist), dumpPortfolioToFile must
// produce exactly ONE snapshot for that domain carrying both records and
// authority.registrar_nameservers.
func TestDumpPortfolio_MergesDnsAndDelegationForSameDomain(t *testing.T) {
	store := &fakeStateStore{}

	// infra.dns state — type-namespaced ID, bare domain as ProviderID.
	_ = store.SaveResource(context.Background(), interfaces.ResourceState{
		ID:         "infra.dns/example-com",
		Name:       "infra.dns/example-com",
		Type:       "infra.dns",
		Provider:   "hover",
		ProviderID: "example.com",
		Outputs: map[string]any{
			"records": []any{
				map[string]any{"type": "A", "name": "@", "data": "192.0.2.1", "ttl": 300},
			},
		},
	})

	// infra.dns_delegation state — distinct ID, same bare domain as ProviderID.
	_ = store.SaveResource(context.Background(), interfaces.ResourceState{
		ID:         "infra.dns_delegation/example-com",
		Name:       "infra.dns_delegation/example-com",
		Type:       "infra.dns_delegation",
		Provider:   "hover",
		ProviderID: "example.com",
		Outputs: map[string]any{
			"registrar_nameservers": []any{"ns1.example.net", "ns2.example.net"},
		},
	})

	dir := t.TempDir()
	out := filepath.Join(dir, "portfolio.json")
	if err := dumpPortfolioToFile(context.Background(), store, out, false); err != nil {
		t.Fatalf("dumpPortfolioToFile: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var p record.Portfolio
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("unmarshal portfolio: %v", err)
	}

	// Exactly one snapshot for example.com.
	if len(p.Snapshots) != 1 {
		t.Fatalf("want 1 snapshot for example.com; got %d: %+v", len(p.Snapshots), p.Snapshots)
	}
	snap := p.Snapshots[0]

	// Snapshot must carry at least one record from the infra.dns state.
	if len(snap.Records) == 0 {
		t.Error("snapshot has no records; want at least one (from infra.dns state)")
	}

	// Snapshot must carry authority.registrar_nameservers from the delegation state.
	if snap.Authority == nil {
		t.Fatal("snapshot.authority is nil; want registrar_nameservers from infra.dns_delegation state")
	}
	ns, ok := snap.Authority["registrar_nameservers"]
	if !ok {
		t.Errorf("snapshot.authority missing registrar_nameservers; got %+v", snap.Authority)
	} else {
		nsSlice, ok := ns.([]any)
		if !ok || len(nsSlice) == 0 {
			t.Errorf("registrar_nameservers = %v; want non-empty slice", ns)
		}
	}
}
