package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// ScanSASTStep runs a SAST (Static Application Security Testing) scanner
// inside a Docker container and evaluates findings against a severity gate.
//
// NOTE: This step is not yet implemented. Docker-based execution requires
// sandbox.DockerSandbox, which is not yet available. Calls to Execute will
// always return ErrNotImplemented.
type ScanSASTStep struct {
	name           string
	scanner        string
	image          string
	sourcePath     string
	rules          []string
	failOnSeverity string
	outputFormat   string
}

// NewScanSASTStepFactory returns a StepFactory that creates ScanSASTStep instances.
func NewScanSASTStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		scanner, _ := config["scanner"].(string)
		if scanner == "" {
			return nil, fmt.Errorf("scan_sast step %q: 'scanner' is required", name)
		}

		image, _ := config["image"].(string)
		if image == "" {
			image = "semgrep/semgrep:latest"
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
			failOnSeverity = "error"
		}

		outputFormat, _ := config["output_format"].(string)
		if outputFormat == "" {
			outputFormat = "sarif"
		}

		return &ScanSASTStep{
			name:           name,
			scanner:        scanner,
			image:          image,
			sourcePath:     sourcePath,
			rules:          rules,
			failOnSeverity: failOnSeverity,
			outputFormat:   outputFormat,
		}, nil
	}
}

// Name returns the step name.
func (s *ScanSASTStep) Name() string { return s.name }

// Execute runs the SAST scanner and returns findings as a ScanResult.
//
// NOTE: This step is not yet implemented. Execution via sandbox.DockerSandbox
// is required but the sandbox package is not yet available. This method always
// returns ErrNotImplemented to prevent silent no-ops in CI/CD pipelines.
func (s *ScanSASTStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	return nil, fmt.Errorf("scan_sast step %q: %w", s.name, ErrNotImplemented)
}
