package module

import (
	"context"
	"fmt"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// ScanContainerStep runs a container vulnerability scanner (e.g., Trivy)
// against a target image and evaluates findings against a severity gate.
// Execution is delegated to a SecurityScannerProvider registered under
// the "security-scanner" service.
type ScanContainerStep struct {
	name              string
	scanner           string
	targetImage       string
	severityThreshold string
	ignoreUnfixed     bool
	outputFormat      string
	app               modular.Application
}

// NewScanContainerStepFactory returns a StepFactory that creates ScanContainerStep instances.
func NewScanContainerStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		scanner, _ := config["scanner"].(string)
		if scanner == "" {
			scanner = "trivy"
		}

		targetImage, _ := config["target_image"].(string)
		if targetImage == "" {
			// Fall back to "image" config key for the scan target (as in the YAML spec)
			targetImage, _ = config["image"].(string)
		}
		targetImage = strings.TrimSpace(targetImage)
		if targetImage == "" {
			return nil, fmt.Errorf("scan_container step %q: target image is required; set 'target_image' or 'image' in config", name)
		}

		severityThreshold, _ := config["severity_threshold"].(string)
		if severityThreshold == "" {
			severityThreshold = "HIGH"
		}

		if err := validateSeverity(severityThreshold); err != nil {
			return nil, fmt.Errorf("scan_container step %q: %w", name, err)
		}

		ignoreUnfixed, _ := config["ignore_unfixed"].(bool)

		outputFormat, _ := config["output_format"].(string)
		if outputFormat == "" {
			outputFormat = "sarif"
		}

		return &ScanContainerStep{
			name:              name,
			scanner:           scanner,
			targetImage:       targetImage,
			severityThreshold: severityThreshold,
			ignoreUnfixed:     ignoreUnfixed,
			outputFormat:      outputFormat,
			app:               app,
		}, nil
	}
}

// Name returns the step name.
func (s *ScanContainerStep) Name() string { return s.name }

// Execute runs the container scanner via the SecurityScannerProvider and evaluates
// the severity gate. Returns an error if the gate fails or no provider is configured.
func (s *ScanContainerStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("scan_container step %q: no application context", s.name)
	}
	var provider SecurityScannerProvider
	if err := s.app.GetService("security-scanner", &provider); err != nil {
		return nil, fmt.Errorf("scan_container step %q: no security scanner provider configured — load a scanner plugin", s.name)
	}

	result, err := provider.ScanContainer(ctx, ContainerScanOpts{
		Scanner:           s.scanner,
		TargetImage:       s.targetImage,
		SeverityThreshold: s.severityThreshold,
		IgnoreUnfixed:     s.ignoreUnfixed,
		OutputFormat:      s.outputFormat,
	})
	if err != nil {
		return nil, fmt.Errorf("scan_container step %q: %w", s.name, err)
	}

	passed := result.EvaluateGate(s.severityThreshold)

	output := map[string]any{
		"passed":   passed,
		"findings": result.Findings,
		"summary":  result.Summary,
		"scanner":  result.Scanner,
	}

	if !passed {
		return nil, fmt.Errorf("scan_container step %q: severity gate failed (threshold: %s)", s.name, s.severityThreshold)
	}

	return &StepResult{Output: output}, nil
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
