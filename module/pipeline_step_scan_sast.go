package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// ScanSASTStep runs a SAST (Static Application Security Testing) scanner
// and evaluates findings against a severity gate. Execution is delegated to
// a SecurityScannerProvider registered under the "security-scanner" service.
type ScanSASTStep struct {
	name           string
	scanner        string
	sourcePath     string
	rules          []string
	failOnSeverity string
	outputFormat   string
	app            modular.Application
}

// NewScanSASTStepFactory returns a StepFactory that creates ScanSASTStep instances.
func NewScanSASTStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		scanner, _ := config["scanner"].(string)
		if scanner == "" {
			return nil, fmt.Errorf("scan_sast step %q: 'scanner' is required", name)
		}

		sourcePath, _ := config["source_path"].(string)
		if sourcePath == "" {
			sourcePath = "/workspace"
		}

		var rules []string
		if rulesRaw, ok := config["rules"].([]any); ok {
			for _, r := range rulesRaw {
				if s, ok := r.(string); ok {
					rules = append(rules, s)
				}
			}
		}

		failOnSeverity, _ := config["fail_on_severity"].(string)
		if failOnSeverity == "" {
			failOnSeverity = "high"
		}

		if err := validateSeverity(failOnSeverity); err != nil {
			return nil, fmt.Errorf("scan_sast step %q: %w", name, err)
		}

		outputFormat, _ := config["output_format"].(string)
		if outputFormat == "" {
			outputFormat = "sarif"
		}

		return &ScanSASTStep{
			name:           name,
			scanner:        scanner,
			sourcePath:     sourcePath,
			rules:          rules,
			failOnSeverity: failOnSeverity,
			outputFormat:   outputFormat,
			app:            app,
		}, nil
	}
}

// Name returns the step name.
func (s *ScanSASTStep) Name() string { return s.name }

// Execute runs the SAST scanner via the SecurityScannerProvider and evaluates
// the severity gate. Returns an error if the gate fails or no provider is configured.
func (s *ScanSASTStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("scan_sast step %q: no application context", s.name)
	}
	var provider SecurityScannerProvider
	if err := s.app.GetService("security-scanner", &provider); err != nil {
		return nil, fmt.Errorf("scan_sast step %q: no security scanner provider configured — load a scanner plugin", s.name)
	}

	result, err := provider.ScanSAST(ctx, SASTScanOpts{
		Scanner:        s.scanner,
		SourcePath:     s.sourcePath,
		Rules:          s.rules,
		FailOnSeverity: s.failOnSeverity,
		OutputFormat:   s.outputFormat,
	})
	if err != nil {
		return nil, fmt.Errorf("scan_sast step %q: %w", s.name, err)
	}

	passed := result.EvaluateGate(s.failOnSeverity)

	output := map[string]any{
		"passed":   passed,
		"findings": result.Findings,
		"summary":  result.Summary,
		"scanner":  result.Scanner,
	}

	if !passed {
		return nil, fmt.Errorf("scan_sast step %q: severity gate failed (threshold: %s)", s.name, s.failOnSeverity)
	}

	return &StepResult{Output: output}, nil
}
