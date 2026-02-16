package module

import "strings"

// ScanResult holds the output of a security scanner.
type ScanResult struct {
	Scanner    string      `json:"scanner"`
	Findings   []Finding   `json:"findings"`
	Summary    ScanSummary `json:"summary"`
	PassedGate bool        `json:"passed_gate"`
}

// Finding represents a single issue found by a scanner.
type Finding struct {
	RuleID   string `json:"rule_id"`
	Severity string `json:"severity"` // "critical", "high", "medium", "low", "info"
	Message  string `json:"message"`
	Location string `json:"location"`
	Line     int    `json:"line,omitempty"`
}

// ScanSummary counts findings by severity level.
type ScanSummary struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Info     int `json:"info"`
}

// NewScanResult creates a ScanResult for the given scanner name.
func NewScanResult(scanner string) *ScanResult {
	return &ScanResult{
		Scanner:  scanner,
		Findings: []Finding{},
	}
}

// AddFinding appends a finding to the scan result.
func (sr *ScanResult) AddFinding(f Finding) {
	sr.Findings = append(sr.Findings, f)
}

// ComputeSummary tallies findings by severity level.
func (sr *ScanResult) ComputeSummary() {
	sr.Summary = ScanSummary{}
	for _, f := range sr.Findings {
		switch strings.ToLower(f.Severity) {
		case "critical":
			sr.Summary.Critical++
		case "high":
			sr.Summary.High++
		case "medium":
			sr.Summary.Medium++
		case "low":
			sr.Summary.Low++
		case "info":
			sr.Summary.Info++
		}
	}
}

// severityRank returns a numeric rank for a severity string (higher = more severe).
func severityRank(severity string) int {
	switch strings.ToLower(severity) {
	case "critical":
		return 5
	case "high":
		return 4
	case "medium":
		return 3
	case "low":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

// EvaluateGate checks whether the scan passes a severity gate.
// The gate passes if no findings are at or above the given threshold severity.
// For example, threshold "high" means the gate fails if any critical or high findings exist.
func (sr *ScanResult) EvaluateGate(threshold string) bool {
	sr.ComputeSummary()
	thresholdRank := severityRank(threshold)
	if thresholdRank == 0 {
		// Unknown threshold — gate passes by default
		sr.PassedGate = true
		return true
	}
	for _, f := range sr.Findings {
		if severityRank(f.Severity) >= thresholdRank {
			sr.PassedGate = false
			return false
		}
	}
	sr.PassedGate = true
	return true
}
