package main

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRunDNSRegistered(t *testing.T) {
	if _, ok := commands["dns"]; !ok {
		t.Fatal("dns command not registered")
	}
	embedded := string(wfctlConfigBytes)
	for _, want := range []string{"name: dns", "command: dns"} {
		if !strings.Contains(embedded, want) {
			t.Fatalf("embedded wfctl config missing %q", want)
		}
	}
}

func TestRunDNSHelpReturnsFlagErrHelp(t *testing.T) {
	if err := runDNS([]string{"--help"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("wfctl dns --help error = %v, want flag.ErrHelp", err)
	}
	if err := runDNS([]string{}); err == nil || errors.Is(err, flag.ErrHelp) {
		t.Fatalf("wfctl dns without subcommand should be a usage error, not help: %v", err)
	}
}

func TestRunDNSIntentHelpReturnsFlagErrHelp(t *testing.T) {
	if err := runDNS([]string{"intent", "--help"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("wfctl dns intent --help error = %v, want flag.ErrHelp", err)
	}
	if err := runDNS([]string{"intent"}); err == nil || errors.Is(err, flag.ErrHelp) {
		t.Fatalf("wfctl dns intent without subcommand should be a usage error, not help: %v", err)
	}
}

func TestRunDNSIntentCompileWritesConfigAndReport(t *testing.T) {
	dir := t.TempDir()
	intentPath := filepath.Join(dir, "domains.json")
	if err := os.WriteFile(intentPath, []byte(`{
  "schema": "workflow.domain-intent.v1",
  "domains": {
    "example.com": {
      "registrar": "hover",
      "dns_host": "cloudflare",
      "stage_dns": true,
      "nameserver_cutover": false,
      "records_policy": "preserve_cloudflare"
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("write intent: %v", err)
	}
	portfolioPath := filepath.Join(dir, "portfolio.json")
	if err := os.WriteFile(portfolioPath, []byte(`{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {
      "id": "cf-example-com",
      "provider": "cloudflare",
      "domain": "example.com",
      "authority": {"name_servers": ["a.ns.cloudflare.com", "b.ns.cloudflare.com"]},
      "records": [{"type": "A", "name": "@", "value": "192.0.2.10", "ttl": 30}]
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write portfolio: %v", err)
	}
	outputPath := filepath.Join(dir, "out.yaml")
	reportPath := filepath.Join(dir, "report.json")
	if err := runDNS([]string{"intent", "compile",
		"--intent", intentPath,
		"--portfolio", portfolioPath,
		"--output", outputPath,
		"--report", reportPath,
	}); err != nil {
		t.Fatalf("runDNS intent compile: %v", err)
	}

	cfgData, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output config: %v", err)
	}
	var cfg struct {
		Modules []struct {
			Name   string         `yaml:"name"`
			Type   string         `yaml:"type"`
			Config map[string]any `yaml:"config"`
		} `yaml:"modules"`
	}
	if err := yaml.Unmarshal(cfgData, &cfg); err != nil {
		t.Fatalf("parse output config: %v\n%s", err, cfgData)
	}
	if len(cfg.Modules) != 3 {
		t.Fatalf("module count = %d, want provider+state+dns: %#v", len(cfg.Modules), cfg.Modules)
	}
	var foundDNS bool
	for _, module := range cfg.Modules {
		if module.Name == "cf-example-com" {
			foundDNS = true
			if module.Type != "infra.dns" {
				t.Fatalf("dns module type = %q", module.Type)
			}
			records, ok := module.Config["records"].([]any)
			if !ok || len(records) != 1 {
				t.Fatalf("records = %#v, want one record", module.Config["records"])
			}
			rec, ok := records[0].(map[string]any)
			if !ok || rec["ttl"] != 60 {
				t.Fatalf("record = %#v, want ttl normalized to 60", records[0])
			}
		}
	}
	if !foundDNS {
		t.Fatalf("generated config missing cf-example-com: %#v", cfg.Modules)
	}

	reportData, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report struct {
		Schema         string `json:"schema"`
		BlockedDomains int    `json:"blocked_domains"`
		ActionCount    int    `json:"action_count"`
	}
	if err := json.Unmarshal(reportData, &report); err != nil {
		t.Fatalf("parse report: %v\n%s", err, reportData)
	}
	if report.Schema != "workflow.domain-intent.report.v1" || report.BlockedDomains != 0 || report.ActionCount != 1 {
		t.Fatalf("bad report: %+v", report)
	}
}
