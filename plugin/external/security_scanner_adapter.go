package external

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/module"
)

// SecurityScannerRemoteModule wraps a RemoteModule for security.scanner type modules.
// On Init it registers a RemoteSecurityScannerProvider in the service registry so
// that scan steps can call app.GetService("security-scanner", &provider).
type SecurityScannerRemoteModule struct {
	*RemoteModule
}

// NewSecurityScannerRemoteModule wraps a RemoteModule as a security scanner module.
func NewSecurityScannerRemoteModule(remote *RemoteModule) *SecurityScannerRemoteModule {
	return &SecurityScannerRemoteModule{RemoteModule: remote}
}

// Init calls the remote Init and registers the scanner provider in the service registry.
func (m *SecurityScannerRemoteModule) Init(app modular.Application) error {
	if err := m.RemoteModule.Init(app); err != nil {
		return err
	}
	provider := &remoteSecurityScannerProvider{module: m.RemoteModule}
	return app.RegisterService("security-scanner", provider)
}

// remoteSecurityScannerProvider implements module.SecurityScannerProvider by
// calling InvokeService on the remote module.
type remoteSecurityScannerProvider struct {
	module *RemoteModule
}

func (p *remoteSecurityScannerProvider) ScanSAST(ctx context.Context, opts module.SASTScanOpts) (*module.ScanResult, error) {
	args := map[string]any{
		"scanner":          opts.Scanner,
		"source_path":      opts.SourcePath,
		"rules":            opts.Rules,
		"fail_on_severity": opts.FailOnSeverity,
		"output_format":    opts.OutputFormat,
	}
	// TODO: add context-aware InvokeService — the context (deadline/cancellation) is
	// not propagated to the remote plugin because InvokeService does not accept a ctx parameter.
	result, err := p.module.InvokeService("ScanSAST", args)
	if err != nil {
		return nil, fmt.Errorf("remote ScanSAST: %w", err)
	}
	return decodeScanResult(result)
}

func (p *remoteSecurityScannerProvider) ScanContainer(ctx context.Context, opts module.ContainerScanOpts) (*module.ScanResult, error) {
	args := map[string]any{
		"scanner":            opts.Scanner,
		"target_image":       opts.TargetImage,
		"severity_threshold": opts.SeverityThreshold,
		"ignore_unfixed":     opts.IgnoreUnfixed,
		"output_format":      opts.OutputFormat,
	}
	result, err := p.module.InvokeService("ScanContainer", args)
	if err != nil {
		return nil, fmt.Errorf("remote ScanContainer: %w", err)
	}
	return decodeScanResult(result)
}

func (p *remoteSecurityScannerProvider) ScanDeps(ctx context.Context, opts module.DepsScanOpts) (*module.ScanResult, error) {
	args := map[string]any{
		"scanner":          opts.Scanner,
		"source_path":      opts.SourcePath,
		"fail_on_severity": opts.FailOnSeverity,
		"output_format":    opts.OutputFormat,
	}
	result, err := p.module.InvokeService("ScanDeps", args)
	if err != nil {
		return nil, fmt.Errorf("remote ScanDeps: %w", err)
	}
	return decodeScanResult(result)
}

// decodeScanResult converts a map[string]any from InvokeService to a *module.ScanResult.
// The map is encoded via JSON round-trip for simplicity.
func decodeScanResult(data map[string]any) (*module.ScanResult, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal scan result: %w", err)
	}
	// Use an intermediate struct matching the JSON fields.
	var wire struct {
		Scanner    string             `json:"scanner"`
		PassedGate bool               `json:"passed_gate"`
		Findings   []module.Finding   `json:"findings"`
		Summary    module.ScanSummary `json:"summary"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("decode scan result: %w", err)
	}
	sr := &module.ScanResult{
		Scanner:    wire.Scanner,
		PassedGate: wire.PassedGate,
		Findings:   wire.Findings,
		Summary:    wire.Summary,
	}
	return sr, nil
}

// Ensure SecurityScannerRemoteModule satisfies modular.Module at compile time.
var _ modular.Module = (*SecurityScannerRemoteModule)(nil)
