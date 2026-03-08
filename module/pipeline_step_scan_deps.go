package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// ScanDepsStep runs a dependency vulnerability scanner (e.g., Grype)
// against a source path and evaluates findings against a severity gate.
// Execution is delegated to a SecurityScannerProvider registered under
// the "security-scanner" service.
type ScanDepsStep struct {
	name           string
	scanner        string
	sourcePath     string
	failOnSeverity string
	outputFormat   string
	app            modular.Application
}

// NewScanDepsStepFactory returns a StepFactory that creates ScanDepsStep instances.
func NewScanDepsStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		scanner, _ := config["scanner"].(string)
		if scanner == "" {
			scanner = "grype"
		}

		sourcePath, _ := config["source_path"].(string)
		if sourcePath == "" {
			sourcePath = "/workspace"
		}

		failOnSeverity, _ := config["fail_on_severity"].(string)
		if failOnSeverity == "" {
			failOnSeverity = "high"
		}

		if err := validateSeverity(failOnSeverity); err != nil {
			return nil, fmt.Errorf("scan_deps step %q: %w", name, err)
		}

		outputFormat, _ := config["output_format"].(string)
		if outputFormat == "" {
			outputFormat = "sarif"
		}

		return &ScanDepsStep{
			name:           name,
			scanner:        scanner,
			sourcePath:     sourcePath,
			failOnSeverity: failOnSeverity,
			outputFormat:   outputFormat,
			app:            app,
		}, nil
	}
}

// Name returns the step name.
func (s *ScanDepsStep) Name() string { return s.name }

// Execute runs the dependency scanner via the SecurityScannerProvider and evaluates
// the severity gate. Returns an error if the gate fails or no provider is configured.
func (s *ScanDepsStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	var provider SecurityScannerProvider
	if err := s.app.GetService("security-scanner", &provider); err != nil {
		return nil, fmt.Errorf("scan_deps step %q: no security scanner provider configured — load a scanner plugin", s.name)
	}

	result, err := provider.ScanDeps(ctx, DepsScanOpts{
		Scanner:        s.scanner,
		SourcePath:     s.sourcePath,
		FailOnSeverity: s.failOnSeverity,
		OutputFormat:   s.outputFormat,
	})
	if err != nil {
		return nil, fmt.Errorf("scan_deps step %q: %w", s.name, err)
	}

	passed := result.EvaluateGate(s.failOnSeverity)

	output := map[string]any{
		"passed":   passed,
		"findings": result.Findings,
		"summary":  result.Summary,
		"scanner":  result.Scanner,
	}

	if !passed {
		return nil, fmt.Errorf("scan_deps step %q: severity gate failed (threshold: %s)", s.name, s.failOnSeverity)
	}

	return &StepResult{Output: output}, nil
}
