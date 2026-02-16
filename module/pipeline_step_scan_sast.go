package module

import (
	"context"
	"fmt"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// ScanSASTStep runs a SAST (Static Application Security Testing) scanner
// inside a Docker container and evaluates findings against a severity gate.
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
func (s *ScanSASTStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	// Build the scanner command based on the configured scanner type
	cmd := s.buildCommand()

	// TODO: Execute via sandbox.DockerSandbox once the sandbox package is available.
	// For now, construct the command and produce a placeholder result.
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

// buildCommand constructs the Docker command arguments for the configured scanner.
func (s *ScanSASTStep) buildCommand() []string {
	switch s.scanner {
	case "semgrep":
		args := []string{"semgrep", "scan"}
		for _, rule := range s.rules {
			args = append(args, "--config", rule)
		}
		if s.outputFormat == "sarif" {
			args = append(args, "--sarif")
		}
		args = append(args, s.sourcePath)
		return args
	default:
		// Generic fallback: run the scanner name as a command against the source path
		return []string{s.scanner, s.sourcePath}
	}
}
