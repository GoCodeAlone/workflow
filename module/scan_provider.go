package module

import "context"

// SecurityScannerProvider is implemented by plugins that provide security scanning.
type SecurityScannerProvider interface {
	ScanSAST(ctx context.Context, opts SASTScanOpts) (*ScanResult, error)
	ScanContainer(ctx context.Context, opts ContainerScanOpts) (*ScanResult, error)
	ScanDeps(ctx context.Context, opts DepsScanOpts) (*ScanResult, error)
}

// SASTScanOpts configures a SAST scan.
type SASTScanOpts struct {
	Scanner        string
	SourcePath     string
	Rules          []string
	FailOnSeverity string
	OutputFormat   string
}

// ContainerScanOpts configures a container vulnerability scan.
type ContainerScanOpts struct {
	Scanner           string
	TargetImage       string
	SeverityThreshold string
	IgnoreUnfixed     bool
	OutputFormat      string
}

// DepsScanOpts configures a dependency vulnerability scan.
type DepsScanOpts struct {
	Scanner        string
	SourcePath     string
	FailOnSeverity string
	OutputFormat   string
}

// SeverityRank returns a numeric rank for severity comparison (higher = more severe).
func SeverityRank(severity string) int {
	return severityRank(severity)
}
