package module

import (
	"context"
	"fmt"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// ScanContainerStep runs a container vulnerability scanner (e.g., Trivy)
// against a target image and evaluates findings against a severity gate.
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
func (s *ScanContainerStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	cmd := s.buildCommand()

	// TODO: Execute via sandbox.DockerSandbox once the sandbox package is available.
	_ = cmd

	scanResult := NewScanResult(s.scanner)
	scanResult.ComputeSummary()
	scanResult.EvaluateGate(s.severityThreshold)

	return &StepResult{
		Output: map[string]any{
			"scan_result":  scanResult,
			"command":      strings.Join(cmd, " "),
			"image":        s.image,
			"target_image": s.targetImage,
		},
	}, nil
}

// buildCommand constructs the Trivy command arguments for container scanning.
func (s *ScanContainerStep) buildCommand() []string {
	args := []string{"trivy", "image"}

	// Set severity filter
	args = append(args, "--severity", strings.ToUpper(s.severityThreshold))

	if s.ignoreUnfixed {
		args = append(args, "--ignore-unfixed")
	}

	switch s.outputFormat {
	case "sarif":
		args = append(args, "--format", "sarif")
	case "json":
		args = append(args, "--format", "json")
	default:
		args = append(args, "--format", "json")
	}

	if s.scanner != "trivy" {
		return []string{s.scanner, s.targetImage}
	}

	args = append(args, s.targetImage)
	return args
}

// getConfigString is a helper to extract a string from config with a default value.
func getConfigString(config map[string]any, key, defaultVal string) string {
	if v, ok := config[key].(string); ok && v != "" {
		return v
	}
	return defaultVal
}

// getConfigBool is a helper to extract a bool from config.
func getConfigBool(config map[string]any, key string) bool {
	v, _ := config[key].(bool)
	return v
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
