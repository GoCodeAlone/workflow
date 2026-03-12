package module

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
)

// mockSecurityScanner is a test implementation of SecurityScannerProvider.
type mockSecurityScanner struct {
	SASTResult      *ScanResult
	SASTErr         error
	ContainerResult *ScanResult
	ContainerErr    error
	DepsResult      *ScanResult
	DepsErr         error

	SASTCallOpts      SASTScanOpts
	ContainerCallOpts ContainerScanOpts
	DepsCallOpts      DepsScanOpts
}

func (m *mockSecurityScanner) ScanSAST(_ context.Context, opts SASTScanOpts) (*ScanResult, error) {
	m.SASTCallOpts = opts
	return m.SASTResult, m.SASTErr
}

func (m *mockSecurityScanner) ScanContainer(_ context.Context, opts ContainerScanOpts) (*ScanResult, error) {
	m.ContainerCallOpts = opts
	return m.ContainerResult, m.ContainerErr
}

func (m *mockSecurityScanner) ScanDeps(_ context.Context, opts DepsScanOpts) (*ScanResult, error) {
	m.DepsCallOpts = opts
	return m.DepsResult, m.DepsErr
}

// scanMockApp is a minimal modular.Application for scan step tests.
type scanMockApp struct {
	services map[string]any
}

func (a *scanMockApp) GetService(name string, target any) error {
	svc, ok := a.services[name]
	if !ok {
		return fmt.Errorf("service %q not found", name)
	}
	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("target must be a non-nil pointer")
	}
	rv.Elem().Set(reflect.ValueOf(svc))
	return nil
}

func (a *scanMockApp) RegisterService(name string, svc any) error           { a.services[name] = svc; return nil }
func (a *scanMockApp) RegisterConfigSection(string, modular.ConfigProvider) {}
func (a *scanMockApp) GetConfigSection(string) (modular.ConfigProvider, error) {
	return nil, nil
}
func (a *scanMockApp) ConfigSections() map[string]modular.ConfigProvider { return nil }
func (a *scanMockApp) Logger() modular.Logger                            { return nil }
func (a *scanMockApp) SetLogger(modular.Logger)                          {}
func (a *scanMockApp) ConfigProvider() modular.ConfigProvider            { return nil }
func (a *scanMockApp) SvcRegistry() modular.ServiceRegistry              { return a.services }
func (a *scanMockApp) RegisterModule(modular.Module)                     {}
func (a *scanMockApp) Init() error                                       { return nil }
func (a *scanMockApp) Start() error                                      { return nil }
func (a *scanMockApp) Stop() error                                       { return nil }
func (a *scanMockApp) Run() error                                        { return nil }
func (a *scanMockApp) IsVerboseConfig() bool                             { return false }
func (a *scanMockApp) SetVerboseConfig(bool)                             {}
func (a *scanMockApp) Context() context.Context                          { return context.Background() }
func (a *scanMockApp) GetServicesByModule(string) []string               { return nil }
func (a *scanMockApp) GetServiceEntry(string) (*modular.ServiceRegistryEntry, bool) {
	return nil, false
}
func (a *scanMockApp) GetServicesByInterface(_ reflect.Type) []*modular.ServiceRegistryEntry {
	return nil
}
func (a *scanMockApp) GetModule(string) modular.Module                { return nil }
func (a *scanMockApp) GetAllModules() map[string]modular.Module       { return nil }
func (a *scanMockApp) StartTime() time.Time                           { return time.Time{} }
func (a *scanMockApp) OnConfigLoaded(func(modular.Application) error) {}

func newScanApp(provider SecurityScannerProvider) *scanMockApp {
	app := &scanMockApp{services: map[string]any{}}
	app.services["security-scanner"] = provider
	return app
}

// TestScanSASTStep_Success verifies that Execute returns output when the scan passes.
func TestScanSASTStep_Success(t *testing.T) {
	mock := &mockSecurityScanner{
		SASTResult: &ScanResult{
			Scanner:  "semgrep",
			Findings: []Finding{},
		},
	}
	app := newScanApp(mock)

	factory := NewScanSASTStepFactory()
	step, err := factory("sast-step", map[string]any{
		"scanner":          "semgrep",
		"source_path":      "/src",
		"fail_on_severity": "high",
		"output_format":    "sarif",
		"rules":            []any{"p/ci"},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if passed, _ := result.Output["passed"].(bool); !passed {
		t.Error("expected passed=true in output")
	}
	if mock.SASTCallOpts.Scanner != "semgrep" {
		t.Errorf("expected scanner=semgrep, got %q", mock.SASTCallOpts.Scanner)
	}
	if mock.SASTCallOpts.SourcePath != "/src" {
		t.Errorf("expected source_path=/src, got %q", mock.SASTCallOpts.SourcePath)
	}
}

// TestScanSASTStep_GateFails verifies that Execute returns an error when the gate fails.
func TestScanSASTStep_GateFails(t *testing.T) {
	mock := &mockSecurityScanner{
		SASTResult: &ScanResult{
			Scanner: "semgrep",
			Findings: []Finding{
				{RuleID: "sql-injection", Severity: "high", Message: "SQL Injection"},
			},
		},
	}
	app := newScanApp(mock)

	factory := NewScanSASTStepFactory()
	step, err := factory("sast-step", map[string]any{
		"scanner":          "semgrep",
		"fail_on_severity": "high",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	_, execErr := step.Execute(context.Background(), &PipelineContext{})
	if execErr == nil {
		t.Fatal("expected error when gate fails")
	}
	if !strings.Contains(execErr.Error(), "severity gate failed") {
		t.Errorf("expected severity gate error, got: %v", execErr)
	}
}

// TestScanSASTStep_ProviderError verifies that Execute propagates provider errors.
func TestScanSASTStep_ProviderError(t *testing.T) {
	mock := &mockSecurityScanner{SASTErr: fmt.Errorf("scanner unavailable")}
	app := newScanApp(mock)

	factory := NewScanSASTStepFactory()
	step, err := factory("sast-step", map[string]any{"scanner": "semgrep"}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	_, execErr := step.Execute(context.Background(), &PipelineContext{})
	if execErr == nil {
		t.Fatal("expected error from provider")
	}
	if !strings.Contains(execErr.Error(), "scanner unavailable") {
		t.Errorf("expected provider error, got: %v", execErr)
	}
}

// TestScanContainerStep_Success verifies container scan success.
func TestScanContainerStep_Success(t *testing.T) {
	mock := &mockSecurityScanner{
		ContainerResult: &ScanResult{
			Scanner:  "trivy",
			Findings: []Finding{{RuleID: "CVE-low", Severity: "low"}},
		},
	}
	app := newScanApp(mock)

	factory := NewScanContainerStepFactory()
	step, err := factory("container-step", map[string]any{
		"scanner":            "trivy",
		"target_image":       "myapp:latest",
		"severity_threshold": "high",
		"ignore_unfixed":     true,
		"output_format":      "json",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if mock.ContainerCallOpts.TargetImage != "myapp:latest" {
		t.Errorf("expected target_image=myapp:latest, got %q", mock.ContainerCallOpts.TargetImage)
	}
	if !mock.ContainerCallOpts.IgnoreUnfixed {
		t.Error("expected ignore_unfixed=true")
	}
}

// TestScanContainerStep_GateFails verifies container scan gate failure.
func TestScanContainerStep_GateFails(t *testing.T) {
	mock := &mockSecurityScanner{
		ContainerResult: &ScanResult{
			Scanner: "trivy",
			Findings: []Finding{
				{RuleID: "CVE-2024-1234", Severity: "critical"},
			},
		},
	}
	app := newScanApp(mock)

	factory := NewScanContainerStepFactory()
	step, err := factory("container-step", map[string]any{
		"target_image":       "vulnerable:latest",
		"severity_threshold": "high",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	_, execErr := step.Execute(context.Background(), &PipelineContext{})
	if execErr == nil {
		t.Fatal("expected error when gate fails")
	}
	if !strings.Contains(execErr.Error(), "severity gate failed") {
		t.Errorf("expected severity gate error, got: %v", execErr)
	}
}

// TestScanDepsStep_Success verifies dependency scan success.
func TestScanDepsStep_Success(t *testing.T) {
	mock := &mockSecurityScanner{
		DepsResult: &ScanResult{
			Scanner:  "grype",
			Findings: []Finding{{RuleID: "GHSA-medium", Severity: "medium"}},
		},
	}
	app := newScanApp(mock)

	factory := NewScanDepsStepFactory()
	step, err := factory("deps-step", map[string]any{
		"scanner":          "grype",
		"source_path":      "/code",
		"fail_on_severity": "high",
		"output_format":    "table",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if mock.DepsCallOpts.SourcePath != "/code" {
		t.Fatalf("expected source_path=/code, got %q", mock.DepsCallOpts.SourcePath)
	}
}

// TestScanDepsStep_GateFails verifies dependency scan gate failure.
func TestScanDepsStep_GateFails(t *testing.T) {
	mock := &mockSecurityScanner{
		DepsResult: &ScanResult{
			Scanner: "grype",
			Findings: []Finding{
				{RuleID: "GHSA-1234", Severity: "high"},
			},
		},
	}
	app := newScanApp(mock)

	factory := NewScanDepsStepFactory()
	step, err := factory("deps-step", map[string]any{
		"fail_on_severity": "high",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	_, execErr := step.Execute(context.Background(), &PipelineContext{})
	if execErr == nil {
		t.Fatal("expected error when gate fails")
	}
	if !strings.Contains(execErr.Error(), "severity gate failed") {
		t.Errorf("expected severity gate error, got: %v", execErr)
	}
}

// TestSeverityRank verifies severity ranking.
func TestSeverityRank(t *testing.T) {
	cases := []struct {
		severity string
		rank     int
	}{
		{"critical", 5},
		{"CRITICAL", 5},
		{"high", 4},
		{"HIGH", 4},
		{"medium", 3},
		{"low", 2},
		{"info", 1},
		{"unknown", 0},
		{"", 0},
	}
	for _, tc := range cases {
		got := SeverityRank(tc.severity)
		if got != tc.rank {
			t.Errorf("SeverityRank(%q) = %d, want %d", tc.severity, got, tc.rank)
		}
	}
}
