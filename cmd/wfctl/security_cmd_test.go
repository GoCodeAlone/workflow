package main

import (
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestAuditSecurity_NoConfig(t *testing.T) {
	cfg := &config.WorkflowConfig{}
	findings := auditSecurity(cfg)
	if len(findings) == 0 {
		t.Fatal("expected findings for empty security config")
	}
	// Should include TLS and network findings.
	var hasTLS, hasNetwork bool
	for _, f := range findings {
		if f.Category == "TLS" {
			hasTLS = true
		}
		if f.Category == "Network" {
			hasNetwork = true
		}
	}
	if !hasTLS {
		t.Error("expected TLS finding")
	}
	if !hasNetwork {
		t.Error("expected Network finding")
	}
}

func TestAuditSecurity_NoExternalTLS(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Security: &config.SecurityConfig{
			TLS: &config.SecurityTLSConfig{
				Internal: true,
				External: false,
			},
		},
	}
	findings := auditSecurity(cfg)
	var hasHighTLS bool
	for _, f := range findings {
		if f.Category == "TLS" && f.Severity == "HIGH" {
			hasHighTLS = true
		}
	}
	if !hasHighTLS {
		t.Error("expected HIGH TLS finding when external TLS is off")
	}
}

func TestAuditSecurity_IngressNoTLS(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Networking: &config.NetworkingConfig{
			Ingress: []config.IngressConfig{
				{Service: "api", Port: 8080},
			},
		},
		Security: &config.SecurityConfig{
			TLS: &config.SecurityTLSConfig{External: true, Internal: true},
		},
	}
	findings := auditSecurity(cfg)
	var hasIngressFinding bool
	for _, f := range findings {
		if f.Category == "Ingress" && f.Severity == "HIGH" {
			hasIngressFinding = true
		}
	}
	if !hasIngressFinding {
		t.Error("expected HIGH Ingress finding for ingress without TLS")
	}
}

func TestAuditSecurity_FullySecure(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Security: &config.SecurityConfig{
			TLS: &config.SecurityTLSConfig{
				Internal:   true,
				External:   true,
				Provider:   "letsencrypt",
				MinVersion: "1.3",
			},
			Network: &config.SecurityNetworkConfig{
				DefaultPolicy: "deny",
			},
			Runtime: &config.SecurityRuntimeConfig{
				RunAsNonRoot:    true,
				NoNewPrivileges: true,
			},
			Scanning: &config.SecurityScanningConfig{
				ContainerScan:  true,
				DependencyScan: true,
			},
		},
		Networking: &config.NetworkingConfig{
			Ingress: []config.IngressConfig{
				{
					Service: "api",
					Port:    8080,
					TLS:     &config.TLSConfig{Provider: "letsencrypt"},
				},
			},
		},
	}
	findings := auditSecurity(cfg)
	// Should have no HIGH findings.
	for _, f := range findings {
		if f.Severity == "HIGH" {
			t.Errorf("unexpected HIGH finding: %+v", f)
		}
	}
}

func TestGenerateNetworkPolicies_FromNetworkingPolicies(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Services: map[string]*config.ServiceConfig{
			"api":    {},
			"worker": {},
			"db":     {},
		},
		Networking: &config.NetworkingConfig{
			Policies: []config.NetworkPolicy{
				{From: "api", To: []string{"worker", "db"}},
				{From: "worker", To: []string{"db"}},
			},
		},
		Security: &config.SecurityConfig{
			Network: &config.SecurityNetworkConfig{DefaultPolicy: "deny"},
		},
	}
	policies := generateNetworkPolicies(cfg)
	if len(policies) == 0 {
		t.Fatal("expected network policies to be generated")
	}
	// db should have ingress from both api and worker.
	dbPolicy, ok := policies["db"]
	if !ok {
		t.Fatal("expected policy for db")
	}
	if len(dbPolicy.Spec.Ingress) != 2 {
		t.Errorf("expected 2 ingress rules for db, got %d", len(dbPolicy.Spec.Ingress))
	}
}

func TestGenerateNetworkPolicies_FromMeshRoutes(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Services: map[string]*config.ServiceConfig{
			"api":    {},
			"worker": {},
		},
		Mesh: &config.MeshConfig{
			Routes: []config.MeshRouteConfig{
				{From: "api", To: "worker", Via: "nats"},
			},
		},
	}
	policies := generateNetworkPolicies(cfg)
	workerPolicy, ok := policies["worker"]
	if !ok {
		t.Fatal("expected policy for worker from mesh route")
	}
	if len(workerPolicy.Spec.Ingress) != 1 {
		t.Errorf("expected 1 ingress rule for worker, got %d", len(workerPolicy.Spec.Ingress))
	}
}

func TestGenerateNetworkPolicies_NoRoutes(t *testing.T) {
	cfg := &config.WorkflowConfig{}
	policies := generateNetworkPolicies(cfg)
	if len(policies) != 0 {
		t.Errorf("expected 0 policies for empty config, got %d", len(policies))
	}
}
