package module

import (
	"context"
	"fmt"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// ScanContainerStep runs a container vulnerability scanner (e.g., Trivy)
// against a target image and evaluates findings against a severity gate.
//
// NOTE: This step is not yet implemented. Docker-based execution requires
// sandbox.DockerSandbox, which is not yet available. Calls to Execute will
// always return ErrNotImplemented.
type ScanContainerStep struct {
	name              string
	scanner           string
	image             string
	targetImage       string
	severityThreshold string
	ignoreUnfixed     bool
	outputFormat      string
}

// NewScanContainerStepFactory returns a StepFactory that creates ScanContainerStep instances.
func NewScanContainerStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		scanner, _ := config["scanner"].(string)
		if scanner == "" {
			scanner = "trivy"
		}

		image, _ := config["image"].(string)
		if image == "" {
			image = "aquasec/trivy:latest"
		}

		targetImage, _ := config["target_image"].(string)
		if targetImage == "" {
			// Fall back to "image" config key for the scan target (as in the YAML spec)
			targetImage = image
		}

		severityThreshold, _ := config["severity_threshold"].(string)
		if severityThreshold == "" {
			severityThreshold = "HIGH"
		}

		ignoreUnfixed, _ := config["ignore_unfixed"].(bool)

		outputFormat, _ := config["output_format"].(string)
		if outputFormat == "" {
			outputFormat = "sarif"
		}

		return &ScanContainerStep{
			name:              name,
			scanner:           scanner,
			image:             "aquasec/trivy:latest", // scanner image is always Trivy
			targetImage:       targetImage,
			severityThreshold: severityThreshold,
			ignoreUnfixed:     ignoreUnfixed,
			outputFormat:      outputFormat,
		}, nil
	}
}

// Name returns the step name.
func (s *ScanContainerStep) Name() string { return s.name }

// Execute runs the container scanner and returns findings as a ScanResult.
//
// NOTE: This step is not yet implemented. Execution via sandbox.DockerSandbox
// is required but the sandbox package is not yet available. This method always
// returns ErrNotImplemented to prevent silent no-ops in CI/CD pipelines.
func (s *ScanContainerStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	return nil, fmt.Errorf("scan_container step %q: %w", s.name, ErrNotImplemented)
}

// validateSeverity checks that a severity string is valid.
func validateSeverity(severity string) error {
	switch strings.ToLower(severity) {
	case "critical", "high", "medium", "low", "info":
		return nil
	default:
		return fmt.Errorf("invalid severity %q (expected critical, high, medium, low, or info)", severity)
	}
}
