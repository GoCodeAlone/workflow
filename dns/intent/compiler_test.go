package intent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestCompileDiscardParkedProducesCloudflareAndHoverResources(t *testing.T) {
	dir := t.TempDir()
	intentPath := writeTestFile(t, dir, "domains.json", `{
  "schema": "workflow.domain-intent.v1",
  "domains": {
    "Example.COM.": {
      "registrar": "hover",
      "dns_host": "cloudflare",
      "stage_dns": true,
      "nameserver_cutover": true,
      "records_policy": "discard_parked",
      "expected_current_nameservers": ["ns3.hover.com.", "ns1.hover.com", "ns2.hover.com"]
    }
  }
}`)
	portfolioPath := writeTestFile(t, dir, "portfolio.json", `{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {
      "id": "cf-example-com",
      "provider": "cloudflare",
      "domain": "example.com",
      "authority": {"name_servers": ["ZOE.NS.CLOUDFLARE.COM.", "adam.ns.cloudflare.com"]}
    },
    {
      "id": "hover-example-com",
      "provider": "hover",
      "domain": "example.com",
      "authority": {"registrar_nameservers": ["ns1.hover.com", "ns2.hover.com", "ns3.hover.com"]},
      "records": [
        {"type": "A", "name": "@", "value": "216.40.34.41", "ttl": 900},
        {"type": "A", "name": "*", "value": "216.40.34.41", "ttl": 900},
        {"type": "MX", "name": "@", "value": "10 mx.hover.com.cust.hostedemail.com", "ttl": 900},
        {"type": "CNAME", "name": "mail", "value": "mail.hover.com.cust.hostedemail.com", "ttl": 900}
      ]
    }
  ]
}`)

	bundle, err := Compile(Options{IntentPath: intentPath, PortfolioGlobs: []string{portfolioPath}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if bundle.Report.BlockedDomains != 0 {
		t.Fatalf("blocked domains = %d; report=%+v", bundle.Report.BlockedDomains, bundle.Report)
	}
	if bundle.Report.ActionCount != 2 {
		t.Fatalf("action count = %d, want 2", bundle.Report.ActionCount)
	}
	got := moduleByName(bundle.Config.Modules, "cf-example-com")
	if got == nil {
		t.Fatalf("missing cloudflare DNS module: %+v", bundle.Config.Modules)
	}
	if got.Type != "infra.dns" {
		t.Fatalf("cloudflare module type = %q", got.Type)
	}
	if got.Config["manage_unlisted"] != true {
		t.Fatalf("manage_unlisted = %#v, want true", got.Config["manage_unlisted"])
	}
	records, ok := got.Config["records"].([]map[string]any)
	if !ok || len(records) != 0 {
		t.Fatalf("records = %#v, want empty []map[string]any", got.Config["records"])
	}
	delegation := moduleByName(bundle.Config.Modules, "hover-delegation-example-com")
	if delegation == nil || delegation.Type != "infra.dns_delegation" {
		t.Fatalf("missing hover delegation module: %+v", bundle.Config.Modules)
	}
	nameservers, ok := delegation.Config["nameservers"].([]string)
	if !ok {
		t.Fatalf("nameservers type = %T", delegation.Config["nameservers"])
	}
	wantNS := []string{"adam.ns.cloudflare.com", "zoe.ns.cloudflare.com"}
	if !equalStrings(nameservers, wantNS) {
		t.Fatalf("nameservers = %v, want %v", nameservers, wantNS)
	}
}

func TestCompileDiscardParkedBlocksNonParkedRecords(t *testing.T) {
	dir := t.TempDir()
	intentPath := writeTestFile(t, dir, "domains.json", `{
  "schema": "workflow.domain-intent.v1",
  "domains": {
    "example.com": {
      "registrar": "hover",
      "dns_host": "cloudflare",
      "records_policy": "discard_parked"
    }
  }
}`)
	portfolioPath := writeTestFile(t, dir, "portfolio.json", `{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {"id": "cf", "provider": "cloudflare", "domain": "example.com", "authority": {"name_servers": ["a.ns.cloudflare.com", "b.ns.cloudflare.com"]}},
    {"id": "hover", "provider": "hover", "domain": "example.com", "records": [{"type": "MX", "name": "@", "value": "mail.protonmail.ch", "ttl": 900}]}
  ]
}`)

	bundle, err := Compile(Options{IntentPath: intentPath, PortfolioGlobs: []string{portfolioPath}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if bundle.Report.BlockedDomains != 1 {
		t.Fatalf("blocked domains = %d, want 1", bundle.Report.BlockedDomains)
	}
	if bundle.Report.ActionCount != 0 {
		t.Fatalf("action count = %d, want 0", bundle.Report.ActionCount)
	}
	if len(bundle.Report.Domains[0].Blockers) == 0 {
		t.Fatalf("expected blockers: %+v", bundle.Report.Domains[0])
	}
}

func moduleByName(modules []config.ModuleConfig, name string) *config.ModuleConfig {
	for i := range modules {
		if modules[i].Name == name {
			return &modules[i]
		}
	}
	return nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func writeTestFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}
