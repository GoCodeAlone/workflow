package scanner

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

func TestNewScannerModule_Defaults(t *testing.T) {
	m, err := NewScannerModule("test", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.mode != "mock" {
		t.Errorf("expected mode=mock, got %q", m.mode)
	}
	if m.semgrepBinary != "semgrep" {
		t.Errorf("expected semgrepBinary=semgrep, got %q", m.semgrepBinary)
	}
	if m.trivyBinary != "trivy" {
		t.Errorf("expected trivyBinary=trivy, got %q", m.trivyBinary)
	}
	if m.grypeBinary != "grype" {
		t.Errorf("expected grypeBinary=grype, got %q", m.grypeBinary)
	}
}

func TestNewScannerModule_CustomConfig(t *testing.T) {
	m, err := NewScannerModule("test", map[string]any{
		"mode":          "cli",
		"semgrepBinary": "/usr/local/bin/semgrep",
		"trivyBinary":   "/usr/local/bin/trivy",
		"grypeBinary":   "/usr/local/bin/grype",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.mode != "cli" {
		t.Errorf("expected mode=cli, got %q", m.mode)
	}
}

func TestScannerModule_MockSAST_DefaultFindings(t *testing.T) {
	m, _ := NewScannerModule("test", map[string]any{})
	result, err := m.ScanSAST(context.Background(), module.SASTScanOpts{
		Scanner:        "semgrep",
		SourcePath:     "/workspace",
		FailOnSeverity: "high",
	})
	if err != nil {
		t.Fatalf("ScanSAST failed: %v", err)
	}
	if result.Scanner != "semgrep" {
		t.Errorf("expected scanner=semgrep, got %q", result.Scanner)
	}
	if len(result.Findings) != 2 {
		t.Errorf("expected 2 default findings, got %d", len(result.Findings))
	}
	if !result.PassedGate {
		t.Error("expected gate to pass with default findings (medium/low) and high threshold")
	}
}

func TestScannerModule_MockSAST_CustomFindings(t *testing.T) {
	m, err := NewScannerModule("test", map[string]any{
		"mockFindings": map[string]any{
			"sast": []any{
				map[string]any{
					"rule_id":  "SEC-001",
					"severity": "critical",
					"message":  "SQL injection detected",
					"location": "/src/db.go",
					"line":     float64(55),
				},
				map[string]any{
					"rule_id":  "SEC-002",
					"severity": "high",
					"message":  "Hardcoded credential",
					"location": "/src/auth.go",
					"line":     float64(12),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := m.ScanSAST(context.Background(), module.SASTScanOpts{
		Scanner:        "semgrep",
		FailOnSeverity: "high",
	})
	if err != nil {
		t.Fatalf("ScanSAST failed: %v", err)
	}
	if len(result.Findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(result.Findings))
	}
	if result.PassedGate {
		t.Error("expected gate to FAIL with critical finding and high threshold")
	}
	if result.Summary.Critical != 1 {
		t.Errorf("expected 1 critical, got %d", result.Summary.Critical)
	}
	if result.Summary.High != 1 {
		t.Errorf("expected 1 high, got %d", result.Summary.High)
	}
}

func TestScannerModule_MockContainer(t *testing.T) {
	m, _ := NewScannerModule("test", map[string]any{})
	result, err := m.ScanContainer(context.Background(), module.ContainerScanOpts{
		Scanner:           "trivy",
		TargetImage:       "myapp:latest",
		SeverityThreshold: "critical",
	})
	if err != nil {
		t.Fatalf("ScanContainer failed: %v", err)
	}
	if result.Scanner != "trivy" {
		t.Errorf("expected scanner=trivy, got %q", result.Scanner)
	}
	if !result.PassedGate {
		t.Error("expected gate to pass with default findings (medium/low) and critical threshold")
	}
}

func TestScannerModule_MockDeps(t *testing.T) {
	m, _ := NewScannerModule("test", map[string]any{})
	result, err := m.ScanDeps(context.Background(), module.DepsScanOpts{
		Scanner:        "grype",
		SourcePath:     "/workspace",
		FailOnSeverity: "medium",
	})
	if err != nil {
		t.Fatalf("ScanDeps failed: %v", err)
	}
	if result.Scanner != "grype" {
		t.Errorf("expected scanner=grype, got %q", result.Scanner)
	}
	if result.PassedGate {
		t.Error("expected gate to FAIL with medium finding and medium threshold")
	}
}

func TestScannerModule_MockContainer_CustomFindings(t *testing.T) {
	m, err := NewScannerModule("test", map[string]any{
		"mockFindings": map[string]any{
			"container": []any{
				map[string]any{
					"rule_id":  "CVE-2024-1234",
					"severity": "critical",
					"message":  "Remote code execution in libfoo",
					"location": "layer:sha256:abc123",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := m.ScanContainer(context.Background(), module.ContainerScanOpts{
		Scanner:           "trivy",
		TargetImage:       "myapp:latest",
		SeverityThreshold: "high",
	})
	if err != nil {
		t.Fatalf("ScanContainer failed: %v", err)
	}
	if result.PassedGate {
		t.Error("expected gate to fail with critical finding and high threshold")
	}
	if result.Summary.Critical != 1 {
		t.Errorf("expected 1 critical, got %d", result.Summary.Critical)
	}
}

func TestScannerModule_DefaultScanner(t *testing.T) {
	m, _ := NewScannerModule("test", map[string]any{})

	// SAST defaults to semgrep
	result, _ := m.ScanSAST(context.Background(), module.SASTScanOpts{
		FailOnSeverity: "critical",
	})
	if result.Scanner != "semgrep" {
		t.Errorf("expected SAST default scanner=semgrep, got %q", result.Scanner)
	}

	// Container defaults to trivy
	result, _ = m.ScanContainer(context.Background(), module.ContainerScanOpts{
		SeverityThreshold: "critical",
	})
	if result.Scanner != "trivy" {
		t.Errorf("expected container default scanner=trivy, got %q", result.Scanner)
	}

	// Deps defaults to grype
	result, _ = m.ScanDeps(context.Background(), module.DepsScanOpts{
		FailOnSeverity: "critical",
	})
	if result.Scanner != "grype" {
		t.Errorf("expected deps default scanner=grype, got %q", result.Scanner)
	}
}

func TestScannerModule_CLIModeErrors(t *testing.T) {
	m, _ := NewScannerModule("test", map[string]any{
		"mode": "cli",
	})

	_, err := m.ScanSAST(context.Background(), module.SASTScanOpts{Scanner: "semgrep"})
	if err == nil {
		t.Error("expected error for CLI mode SAST")
	}

	_, err = m.ScanContainer(context.Background(), module.ContainerScanOpts{Scanner: "trivy"})
	if err == nil {
		t.Error("expected error for CLI mode container")
	}

	_, err = m.ScanDeps(context.Background(), module.DepsScanOpts{Scanner: "grype"})
	if err == nil {
		t.Error("expected error for CLI mode deps")
	}
}

func TestScannerModule_Name(t *testing.T) {
	m, _ := NewScannerModule("my-scanner", map[string]any{})
	if m.Name() != "my-scanner" {
		t.Errorf("expected name=my-scanner, got %q", m.Name())
	}
}

func TestScannerModule_Lifecycle(t *testing.T) {
	m, _ := NewScannerModule("test", map[string]any{})
	if err := m.Start(context.Background()); err != nil {
		t.Errorf("Start failed: %v", err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

func TestPlugin_New(t *testing.T) {
	p := New()
	if p.PluginName != "scanner" {
		t.Errorf("expected plugin name=scanner, got %q", p.PluginName)
	}

	factories := p.ModuleFactories()
	if _, ok := factories["security.scanner"]; !ok {
		t.Error("expected security.scanner module factory")
	}

	caps := p.Capabilities()
	if len(caps) != 1 || caps[0].Name != "security-scanner" {
		t.Errorf("unexpected capabilities: %v", caps)
	}
}
