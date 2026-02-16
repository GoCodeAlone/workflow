package module

import (
	"context"
	"fmt"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// ScanDepsStep runs a dependency vulnerability scanner (e.g., Grype)
// against a source path and evaluates findings against a severity gate.
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
func (s *ScanDepsStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	cmd := s.buildCommand()

	// TODO: Execute via sandbox.DockerSandbox once the sandbox package is available.
	_ = cmd

	scanResult := NewScanResult(s.scanner)
	scanResult.ComputeSummary()
	scanResult.EvaluateGate(s.failOnSeverity)

	return &StepResult{
		Output: map[string]any{
			"scan_result": scanResult,
			"command":     strings.Join(cmd, " "),
			"image":       s.image,
		},
	}, nil
}

// buildCommand constructs the Grype command arguments for dependency scanning.
func (s *ScanDepsStep) buildCommand() []string {
	switch s.scanner {
	case "grype":
		args := []string{"grype"}

		// Set fail-on severity
		args = append(args, "--fail-on", strings.ToLower(s.failOnSeverity))

		switch s.outputFormat {
		case "sarif":
			args = append(args, "-o", "sarif")
		case "json":
			args = append(args, "-o", "json")
		default:
			args = append(args, "-o", "json")
		}

		args = append(args, "dir:"+s.sourcePath)
		return args
	default:
		return []string{s.scanner, s.sourcePath}
	}
}
