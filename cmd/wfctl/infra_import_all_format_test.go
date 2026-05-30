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
