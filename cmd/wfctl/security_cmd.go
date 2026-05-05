package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/GoCodeAlone/workflow/config"
	"gopkg.in/yaml.v3"
)

func runSecurity(args []string) error {
	if len(args) < 1 {
		return securityUsage()
	}
	switch args[0] {
	case "audit":
		return runSecurityAudit(args[1:])
	case "generate-network-policies":
		return runSecurityGenerateNetworkPolicies(args[1:])
	default:
		return securityUsage()
	}
}

func securityUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl security <action> [options] [config.yaml]

Audit and generate security policies from workflow config.

Actions:
  audit                      Scan config for security issues (TLS, auth, network)
  generate-network-policies  Generate Kubernetes NetworkPolicy YAML from config

Options:
  --config <file>    Config file (default: config.yaml or app.yaml)
  --output <dir>     Output directory for generated files (generate-network-policies)

Examples:
  wfctl security audit
  wfctl security audit --config config/app.yaml
  wfctl security generate-network-policies --output k8s/
`)
	return fmt.Errorf("missing or unknown action")
}

// securityFinding is a single audit finding.
type securityFinding struct {
	Severity string
	Category string
	Message  string
}

func runSecurityAudit(args []string) error {
	fs := flag.NewFlagSet("security audit", flag.ContinueOnError)
	configFile := fs.String("config", "", "Config file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgPath, err := resolveConfigFile(*configFile, fs.Args())
	if err != nil {
		return err
	}

	cfg, err := config.LoadFromFile(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	findings := auditSecurity(cfg)

	if len(findings) == 0 {
		fmt.Println("No security issues found.")
		return nil
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SEVERITY\tCATEGORY\tFINDING")
	fmt.Fprintln(tw, "--------\t--------\t-------")
	for _, f := range findings {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", f.Severity, f.Category, f.Message)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	// Return error if any HIGH findings.
	for _, f := range findings {
		if f.Severity == "HIGH" {
			return fmt.Errorf("%d security issue(s) found", len(findings))
		}
	}
	return nil
}

// auditSecurity returns all security findings for a config.
func auditSecurity(cfg *config.WorkflowConfig) []securityFinding {
	var findings []securityFinding

	add := func(severity, category, message string) {
		findings = append(findings, securityFinding{
			Severity: severity,
			Category: category,
			Message:  message,
		})
	}

	// TLS checks.
	if cfg.Security == nil || cfg.Security.TLS == nil {
		add("WARN", "TLS", "No security.tls section defined; TLS configuration is missing")
	} else {
		tls := cfg.Security.TLS
		if !tls.External {
			add("HIGH", "TLS", "security.tls.external is not enabled; external traffic is unencrypted")
		}
		if !tls.Internal {
			add("WARN", "TLS", "security.tls.internal is not enabled; service-to-service traffic is unencrypted")
		}
		if tls.MinVersion != "" && tls.MinVersion < "1.2" {
			add("HIGH", "TLS", fmt.Sprintf("security.tls.minVersion %q is below recommended minimum (1.2)", tls.MinVersion))
		}
	}

	// Network policy checks.
	if cfg.Security == nil || cfg.Security.Network == nil {
		add("WARN", "Network", "No security.network section defined; default network policy is unspecified")
	} else if cfg.Security.Network.DefaultPolicy != "deny" {
		add("WARN", "Network", fmt.Sprintf("security.network.defaultPolicy is %q; recommend 'deny' for least-privilege", cfg.Security.Network.DefaultPolicy))
	}

	// Check ingress TLS.
	if cfg.Networking != nil {
		for _, ing := range cfg.Networking.Ingress {
			if ing.TLS == nil {
				add("HIGH", "Ingress", fmt.Sprintf("ingress for service %q port %d has no TLS configured", ing.Service, ing.Port))
			}
		}
	}

	// Auth checks: look for http.server modules without auth modules.
	hasAuth := false
	hasHTTPServer := false
	for _, mod := range cfg.Modules {
		switch mod.Type {
		case "auth.jwt", "auth.basic", "auth.apikey", "auth.oidc":
			hasAuth = true
		case "http.server":
			hasHTTPServer = true
		}
	}
	if hasHTTPServer && !hasAuth {
		add("WARN", "Auth", "HTTP server module found but no auth module (auth.jwt, auth.basic, auth.oidc) is configured")
	}

	// Runtime security checks.
	if cfg.Security != nil && cfg.Security.Runtime != nil {
		rt := cfg.Security.Runtime
		if !rt.RunAsNonRoot {
			add("WARN", "Runtime", "security.runtime.runAsNonRoot is not enabled; containers may run as root")
		}
		if !rt.NoNewPrivileges {
			add("WARN", "Runtime", "security.runtime.noNewPrivileges is not enabled")
		}
	}

	// Scanning checks.
	if cfg.Security == nil || cfg.Security.Scanning == nil {
		add("INFO", "Scanning", "No security.scanning section defined; automated scanning is not configured")
	} else {
		sc := cfg.Security.Scanning
		if !sc.ContainerScan {
			add("INFO", "Scanning", "security.scanning.containerScan is disabled")
		}
		if !sc.DependencyScan {
			add("INFO", "Scanning", "security.scanning.dependencyScan is disabled")
		}
	}

	return findings
}

func runSecurityGenerateNetworkPolicies(args []string) error {
	fs := flag.NewFlagSet("security generate-network-policies", flag.ContinueOnError)
	configFile := fs.String("config", "", "Config file")
	outputDir := fs.String("output", "k8s", "Output directory for generated NetworkPolicy YAML files")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgPath, err := resolveConfigFile(*configFile, fs.Args())
	if err != nil {
		return err
	}

	cfg, err := config.LoadFromFile(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	policies := generateNetworkPolicies(cfg)
	if len(policies) == 0 {
		fmt.Println("No network policies to generate (add networking.policies or mesh.routes to your config).")
		return nil
	}

	if err := os.MkdirAll(*outputDir, 0750); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	for name := range policies {
		policy := policies[name]
		outPath := filepath.Join(*outputDir, fmt.Sprintf("netpol-%s.yaml", name))
		data, err := yaml.Marshal(policy)
		if err != nil {
			return fmt.Errorf("failed to marshal policy for %s: %w", name, err)
		}
		if err := os.WriteFile(outPath, data, 0644); err != nil { //nolint:gosec // generated file
			return fmt.Errorf("failed to write %s: %w", outPath, err)
		}
		fmt.Printf("  Wrote %s\n", outPath)
	}

	fmt.Printf("Generated %d NetworkPolicy file(s) in %s/\n", len(policies), *outputDir)
	return nil
}

// k8sNetworkPolicy is a minimal Kubernetes NetworkPolicy for YAML generation.
type k8sNetworkPolicy struct {
	APIVersion string               `yaml:"apiVersion"`
	Kind       string               `yaml:"kind"`
	Metadata   k8sMetadata          `yaml:"metadata"`
	Spec       k8sNetworkPolicySpec `yaml:"spec"`
}

type k8sMetadata struct {
	Name      string            `yaml:"name"`
	Namespace string            `yaml:"namespace,omitempty"`
	Labels    map[string]string `yaml:"labels,omitempty"`
}

type k8sNetworkPolicySpec struct {
	PodSelector k8sPodSelector         `yaml:"podSelector"`
	PolicyTypes []string               `yaml:"policyTypes"`
	Ingress     []k8sNetworkPolicyRule `yaml:"ingress,omitempty"`
	Egress      []k8sNetworkPolicyRule `yaml:"egress,omitempty"`
}

type k8sPodSelector struct {
	MatchLabels map[string]string `yaml:"matchLabels,omitempty"`
}

type k8sNetworkPolicyRule struct {
	From []k8sPeerSelector `yaml:"from,omitempty"`
	To   []k8sPeerSelector `yaml:"to,omitempty"`
}

type k8sPeerSelector struct {
	PodSelector k8sPodSelector `yaml:"podSelector"`
}

// generateNetworkPolicies builds a map of service→NetworkPolicy from config.
func generateNetworkPolicies(cfg *config.WorkflowConfig) map[string]k8sNetworkPolicy {
	policies := make(map[string]k8sNetworkPolicy)

	// Build allowed-from map: to → list of froms.
	allowedFrom := make(map[string][]string)

	// From networking.policies.
	if cfg.Networking != nil {
		for _, pol := range cfg.Networking.Policies {
			for _, to := range pol.To {
				allowedFrom[to] = append(allowedFrom[to], pol.From)
			}
		}
	}

	// From mesh.routes.
	if cfg.Mesh != nil {
		for _, route := range cfg.Mesh.Routes {
			if route.To != "" && route.From != "" {
				allowedFrom[route.To] = append(allowedFrom[route.To], route.From)
			}
		}
	}

	defaultDeny := cfg.Security != nil &&
		cfg.Security.Network != nil &&
		cfg.Security.Network.DefaultPolicy == "deny"

	// Collect all service names.
	serviceNames := collectServiceNames(cfg)

	for _, svcName := range serviceNames {
		froms := deduplicateStrings(allowedFrom[svcName])

		var ingressRules []k8sNetworkPolicyRule
		for _, from := range froms {
			ingressRules = append(ingressRules, k8sNetworkPolicyRule{
				From: []k8sPeerSelector{
					{PodSelector: k8sPodSelector{MatchLabels: map[string]string{"app": from}}},
				},
			})
		}

		policyTypes := []string{"Ingress"}
		if defaultDeny {
			policyTypes = append(policyTypes, "Egress")
		}

		if len(ingressRules) == 0 && !defaultDeny {
			continue // Nothing to generate for this service.
		}

		policies[svcName] = k8sNetworkPolicy{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "NetworkPolicy",
			Metadata: k8sMetadata{
				Name:   fmt.Sprintf("allow-ingress-%s", svcName),
				Labels: map[string]string{"app": svcName},
			},
			Spec: k8sNetworkPolicySpec{
				PodSelector: k8sPodSelector{MatchLabels: map[string]string{"app": svcName}},
				PolicyTypes: policyTypes,
				Ingress:     ingressRules,
			},
		}
	}

	return policies
}

// collectServiceNames returns all service names from services: and networking.policies.
func collectServiceNames(cfg *config.WorkflowConfig) []string {
	seen := make(map[string]bool)
	for name := range cfg.Services {
		seen[name] = true
	}
	if cfg.Networking != nil {
		for _, pol := range cfg.Networking.Policies {
			seen[pol.From] = true
			for _, to := range pol.To {
				seen[to] = true
			}
		}
	}
	if cfg.Mesh != nil {
		for _, route := range cfg.Mesh.Routes {
			if route.From != "" {
				seen[route.From] = true
			}
			if route.To != "" {
				seen[route.To] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	return names
}

func deduplicateStrings(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// labelForSeverity returns an ASCII label prefix for display.
func labelForSeverity(s string) string {
	switch strings.ToUpper(s) {
	case "HIGH":
		return "[HIGH]"
	case "WARN":
		return "[WARN]"
	default:
		return "[INFO]"
	}
}

var _ = labelForSeverity // suppress unused warning; used in tests
