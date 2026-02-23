package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// ScanDepsStep runs a dependency vulnerability scanner (e.g., Grype)
// against a source path and evaluates findings against a severity gate.
//
// NOTE: This step is not yet implemented. Docker-based execution requires
// sandbox.DockerSandbox, which is not yet available. Calls to Execute will
// always return ErrNotImplemented.
type ScanDepsStep struct {
	name           string
	scanner        string
	image          string
	sourcePath     string
	failOnSeverity string
	outputFormat   string
}

// NewScanDepsStepFactory returns a StepFactory that creates ScanDepsStep instances.
func NewScanDepsStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		scanner, _ := config["scanner"].(string)
		if scanner == "" {
			scanner = "grype"
		}

		image, _ := config["image"].(string)
		if image == "" {
			image = "anchore/grype:latest"
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
			image:          image,
			sourcePath:     sourcePath,
			failOnSeverity: failOnSeverity,
			outputFormat:   outputFormat,
		}, nil
	}
}

// Name returns the step name.
func (s *ScanDepsStep) Name() string { return s.name }

// Execute runs the dependency scanner and returns findings as a ScanResult.
//
// NOTE: This step is not yet implemented. Execution via sandbox.DockerSandbox
// is required but the sandbox package is not yet available. This method always
// returns ErrNotImplemented to prevent silent no-ops in CI/CD pipelines.
func (s *ScanDepsStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	return nil, fmt.Errorf("scan_deps step %q: %w", s.name, ErrNotImplemented)
}
