package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNetworkingConfig_ParseYAML(t *testing.T) {
	yamlStr := `
networking:
  ingress:
    - service: api
      port: 8080
      externalPort: 443
      protocol: https
      path: /
      tls:
        provider: letsencrypt
        domain: api.example.com
        minVersion: "1.2"
    - service: admin
      port: 9090
      protocol: http
  policies:
    - from: api
      to: [worker, db]
    - from: worker
      to: [db]
  dns:
    provider: cloudflare
    zone: example.com
    records:
      - name: api
        type: CNAME
        target: lb.example.com

security:
  tls:
    internal: true
    external: true
    provider: letsencrypt
    minVersion: "1.3"
  network:
    defaultPolicy: deny
  identity:
    provider: spiffe
    perService: true
  runtime:
    readOnlyFilesystem: true
    noNewPrivileges: true
    runAsNonRoot: true
    dropCapabilities: [ALL]
    addCapabilities: [NET_BIND_SERVICE]
  scanning:
    containerScan: true
    dependencyScan: true
    sast: false
`
	var cfg WorkflowConfig
	if err := yaml.Unmarshal([]byte(yamlStr), &cfg); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if cfg.Networking == nil {
		t.Fatal("networking section missing")
	}
	if len(cfg.Networking.Ingress) != 2 {
		t.Fatalf("expected 2 ingress entries, got %d", len(cfg.Networking.Ingress))
	}
	ing0 := cfg.Networking.Ingress[0]
	if ing0.Service != "api" {
		t.Errorf("expected service=api, got %q", ing0.Service)
	}
	if ing0.Port != 8080 {
		t.Errorf("expected port=8080, got %d", ing0.Port)
	}
	if ing0.ExternalPort != 443 {
		t.Errorf("expected externalPort=443, got %d", ing0.ExternalPort)
	}
	if ing0.TLS == nil {
		t.Fatal("ingress[0].tls missing")
	}
	if ing0.TLS.Provider != "letsencrypt" {
		t.Errorf("expected provider=letsencrypt, got %q", ing0.TLS.Provider)
	}
	if ing0.TLS.Domain != "api.example.com" {
		t.Errorf("expected domain=api.example.com, got %q", ing0.TLS.Domain)
	}

	if len(cfg.Networking.Policies) != 2 {
		t.Fatalf("expected 2 policies, got %d", len(cfg.Networking.Policies))
	}
	p0 := cfg.Networking.Policies[0]
	if p0.From != "api" {
		t.Errorf("expected from=api, got %q", p0.From)
	}
	if len(p0.To) != 2 {
		t.Errorf("expected 2 destinations, got %d", len(p0.To))
	}

	if cfg.Networking.DNS == nil {
		t.Fatal("networking.dns missing")
	}
	if cfg.Networking.DNS.Provider != "cloudflare" {
		t.Errorf("expected dns provider=cloudflare, got %q", cfg.Networking.DNS.Provider)
	}
	if len(cfg.Networking.DNS.Records) != 1 {
		t.Fatalf("expected 1 dns record, got %d", len(cfg.Networking.DNS.Records))
	}
	if cfg.Networking.DNS.Records[0].Name != "api" {
		t.Errorf("unexpected dns record name: %q", cfg.Networking.DNS.Records[0].Name)
	}

	if cfg.Security == nil {
		t.Fatal("security section missing")
	}
	if cfg.Security.TLS == nil {
		t.Fatal("security.tls missing")
	}
	if !cfg.Security.TLS.Internal {
		t.Error("expected security.tls.internal=true")
	}
	if cfg.Security.TLS.MinVersion != "1.3" {
		t.Errorf("expected minVersion=1.3, got %q", cfg.Security.TLS.MinVersion)
	}
	if cfg.Security.Network == nil {
		t.Fatal("security.network missing")
	}
	if cfg.Security.Network.DefaultPolicy != "deny" {
		t.Errorf("expected defaultPolicy=deny, got %q", cfg.Security.Network.DefaultPolicy)
	}
	if cfg.Security.Identity == nil {
		t.Fatal("security.identity missing")
	}
	if !cfg.Security.Identity.PerService {
		t.Error("expected perService=true")
	}
	if cfg.Security.Runtime == nil {
		t.Fatal("security.runtime missing")
	}
	if !cfg.Security.Runtime.ReadOnlyFilesystem {
		t.Error("expected readOnlyFilesystem=true")
	}
	if len(cfg.Security.Runtime.DropCapabilities) != 1 || cfg.Security.Runtime.DropCapabilities[0] != "ALL" {
		t.Errorf("unexpected dropCapabilities: %v", cfg.Security.Runtime.DropCapabilities)
	}
	if cfg.Security.Scanning == nil {
		t.Fatal("security.scanning missing")
	}
	if !cfg.Security.Scanning.ContainerScan {
		t.Error("expected containerScan=true")
	}
	if cfg.Security.Scanning.SAST {
		t.Error("expected sast=false")
	}
}
