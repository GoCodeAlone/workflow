package scanner

import (
	"context"
	"fmt"
	"strings"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/module"
)

// ScannerModule implements SecurityScannerProvider and registers itself
// in the service registry so that scan steps can find it.
type ScannerModule struct {
	name          string
	mode          string // "mock" or "cli"
	mockFindings  map[string][]module.Finding
	semgrepBinary string
	trivyBinary   string
	grypeBinary   string
}

// NewScannerModule creates a ScannerModule from config.
func NewScannerModule(name string, cfg map[string]any) (*ScannerModule, error) {
	m := &ScannerModule{
		name:          name,
		mode:          "mock",
		semgrepBinary: "semgrep",
		trivyBinary:   "trivy",
		grypeBinary:   "grype",
		mockFindings:  make(map[string][]module.Finding),
	}

	if v, ok := cfg["mode"].(string); ok && v != "" {
		if v != "mock" && v != "cli" {
			return nil, fmt.Errorf("security.scanner %q: invalid mode %q (expected \"mock\" or \"cli\")", name, v)
		}
		m.mode = v
	}
	if v, ok := cfg["semgrepBinary"].(string); ok && v != "" {
		m.semgrepBinary = v
	}
	if v, ok := cfg["trivyBinary"].(string); ok && v != "" {
		m.trivyBinary = v
	}
	if v, ok := cfg["grypeBinary"].(string); ok && v != "" {
		m.grypeBinary = v
	}

	if mockCfg, ok := cfg["mockFindings"].(map[string]any); ok {
		for scanType, findingsRaw := range mockCfg {
			findings, err := parseMockFindings(findingsRaw)
			if err != nil {
				return nil, fmt.Errorf("security.scanner %q: invalid mockFindings.%s: %w", name, scanType, err)
			}
			m.mockFindings[scanType] = findings
		}
	}

	return m, nil
}

// Name returns the module name.
func (m *ScannerModule) Name() string { return m.name }

// Init registers the module as a SecurityScannerProvider in the service registry.
// Only one security.scanner module may be loaded at a time; this is intentional —
// the engine uses a single provider under the "security-scanner" service key.
func (m *ScannerModule) Init(app modular.Application) error {
	return app.RegisterService("security-scanner", m)
}

// Start is a no-op.
func (m *ScannerModule) Start(_ context.Context) error { return nil }

// Stop is a no-op.
func (m *ScannerModule) Stop(_ context.Context) error { return nil }

// ScanSAST performs a SAST scan. In mock mode, returns preconfigured findings.
func (m *ScannerModule) ScanSAST(_ context.Context, opts module.SASTScanOpts) (*module.ScanResult, error) {
	scanner := opts.Scanner
	if scanner == "" {
		scanner = "semgrep"
	}

	if m.mode == "mock" {
		return m.mockScan("sast", scanner, opts.FailOnSeverity), nil
	}

	return nil, fmt.Errorf("security.scanner %q: CLI mode not yet implemented for SAST (scanner: %s)", m.name, scanner)
}

// ScanContainer performs a container image scan. In mock mode, returns preconfigured findings.
func (m *ScannerModule) ScanContainer(_ context.Context, opts module.ContainerScanOpts) (*module.ScanResult, error) {
	scanner := opts.Scanner
	if scanner == "" {
		scanner = "trivy"
	}

	if m.mode == "mock" {
		return m.mockScan("container", scanner, opts.SeverityThreshold), nil
	}

	return nil, fmt.Errorf("security.scanner %q: CLI mode not yet implemented for container scan (scanner: %s)", m.name, scanner)
}

// ScanDeps performs a dependency vulnerability scan. In mock mode, returns preconfigured findings.
func (m *ScannerModule) ScanDeps(_ context.Context, opts module.DepsScanOpts) (*module.ScanResult, error) {
	scanner := opts.Scanner
	if scanner == "" {
		scanner = "grype"
	}

	if m.mode == "mock" {
		return m.mockScan("deps", scanner, opts.FailOnSeverity), nil
	}

	return nil, fmt.Errorf("security.scanner %q: CLI mode not yet implemented for deps scan (scanner: %s)", m.name, scanner)
}

// mockScan returns a ScanResult from preconfigured mock findings.
func (m *ScannerModule) mockScan(scanType, scanner, threshold string) *module.ScanResult {
	result := module.NewScanResult(scanner)

	if findings, ok := m.mockFindings[scanType]; ok {
		for _, f := range findings {
			result.AddFinding(f)
		}
	} else {
		// Default mock: return a few sample findings
		result.AddFinding(module.Finding{
			RuleID:   "MOCK-001",
			Severity: "medium",
			Message:  fmt.Sprintf("Mock %s finding from %s scanner", scanType, scanner),
			Location: "/src/main.go",
			Line:     42,
		})
		result.AddFinding(module.Finding{
			RuleID:   "MOCK-002",
			Severity: "low",
			Message:  fmt.Sprintf("Mock informational %s finding", scanType),
			Location: "/src/util.go",
			Line:     15,
		})
	}

	result.ComputeSummary()
	if threshold != "" {
		result.EvaluateGate(threshold)
	} else {
		result.PassedGate = true
	}

	return result
}

// parseMockFindings converts raw config into a slice of Finding.
func parseMockFindings(raw any) ([]module.Finding, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("expected array of findings")
	}

	var findings []module.Finding
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		f := module.Finding{
			RuleID:   getString(m, "rule_id"),
			Severity: strings.ToLower(getString(m, "severity")),
			Message:  getString(m, "message"),
			Location: getString(m, "location"),
		}
		if line, ok := m["line"].(float64); ok {
			f.Line = int(line)
		}
		findings = append(findings, f)
	}
	return findings, nil
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
