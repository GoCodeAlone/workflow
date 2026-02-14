package community

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/dynamic"
	"github.com/GoCodeAlone/workflow/plugin"
)

// CheckResult represents the outcome of a single validation check.
type CheckResult struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}

// ValidationResult holds the full outcome of submission validation.
type ValidationResult struct {
	Valid  bool          `json:"valid"`
	Checks []CheckResult `json:"checks"`
}

// SubmissionValidator validates plugin submissions for community contributions.
type SubmissionValidator struct{}

// NewSubmissionValidator creates a new SubmissionValidator.
func NewSubmissionValidator() *SubmissionValidator {
	return &SubmissionValidator{}
}

// ValidateDirectory validates a plugin directory for community submission.
func (v *SubmissionValidator) ValidateDirectory(dir string) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Check 1: Directory exists
	info, err := os.Stat(dir)
	if err != nil {
		result.Valid = false
		result.Checks = append(result.Checks, CheckResult{
			Name:    "directory_exists",
			Passed:  false,
			Message: fmt.Sprintf("directory %s does not exist: %v", dir, err),
		})
		return result, nil
	}
	if !info.IsDir() {
		result.Valid = false
		result.Checks = append(result.Checks, CheckResult{
			Name:    "directory_exists",
			Passed:  false,
			Message: fmt.Sprintf("%s is not a directory", dir),
		})
		return result, nil
	}
	result.Checks = append(result.Checks, CheckResult{
		Name:   "directory_exists",
		Passed: true,
	})

	// Check 2: Manifest exists and is valid
	manifestPath := filepath.Join(dir, "plugin.json")
	manifest, manifestErr := plugin.LoadManifest(manifestPath)
	if manifestErr != nil {
		result.Valid = false
		result.Checks = append(result.Checks, CheckResult{
			Name:    "manifest_exists",
			Passed:  false,
			Message: fmt.Sprintf("failed to load manifest: %v", manifestErr),
		})
		return result, nil
	}
	result.Checks = append(result.Checks, CheckResult{
		Name:   "manifest_exists",
		Passed: true,
	})

	// Check 3: Manifest validation
	if valErr := manifest.Validate(); valErr != nil {
		result.Valid = false
		result.Checks = append(result.Checks, CheckResult{
			Name:    "manifest_valid",
			Passed:  false,
			Message: valErr.Error(),
		})
	} else {
		result.Checks = append(result.Checks, CheckResult{
			Name:   "manifest_valid",
			Passed: true,
		})
	}

	// Check 4: Source file exists
	sourceFiles, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	var nonTestSources []string
	for _, sf := range sourceFiles {
		if !strings.HasSuffix(sf, "_test.go") {
			nonTestSources = append(nonTestSources, sf)
		}
	}
	if len(nonTestSources) == 0 {
		result.Valid = false
		result.Checks = append(result.Checks, CheckResult{
			Name:    "source_exists",
			Passed:  false,
			Message: "no .go source files found",
		})
	} else {
		result.Checks = append(result.Checks, CheckResult{
			Name:   "source_exists",
			Passed: true,
		})
	}

	// Check 5: Source syntax validation
	for _, sf := range nonTestSources {
		data, readErr := os.ReadFile(sf)
		if readErr != nil {
			result.Valid = false
			result.Checks = append(result.Checks, CheckResult{
				Name:    "source_valid",
				Passed:  false,
				Message: fmt.Sprintf("failed to read %s: %v", filepath.Base(sf), readErr),
			})
			continue
		}
		if valErr := dynamic.ValidateSource(string(data)); valErr != nil {
			result.Valid = false
			result.Checks = append(result.Checks, CheckResult{
				Name:    "source_valid",
				Passed:  false,
				Message: fmt.Sprintf("%s: %v", filepath.Base(sf), valErr),
			})
		} else {
			result.Checks = append(result.Checks, CheckResult{
				Name:   "source_valid",
				Passed: true,
			})
		}
	}

	// Check 6: Has test files
	var testFiles []string
	for _, sf := range sourceFiles {
		if strings.HasSuffix(sf, "_test.go") {
			testFiles = append(testFiles, sf)
		}
	}
	if len(testFiles) == 0 {
		result.Valid = false
		result.Checks = append(result.Checks, CheckResult{
			Name:    "tests_exist",
			Passed:  false,
			Message: "no test files found (expected *_test.go)",
		})
	} else {
		result.Checks = append(result.Checks, CheckResult{
			Name:   "tests_exist",
			Passed: true,
		})
	}

	// Check 7: Has description in manifest
	if manifest.Description == "" {
		result.Valid = false
		result.Checks = append(result.Checks, CheckResult{
			Name:    "has_description",
			Passed:  false,
			Message: "plugin description is empty",
		})
	} else {
		result.Checks = append(result.Checks, CheckResult{
			Name:   "has_description",
			Passed: true,
		})
	}

	// Check 8: Has license
	if manifest.License == "" {
		// Warning, not a failure
		result.Checks = append(result.Checks, CheckResult{
			Name:    "has_license",
			Passed:  false,
			Message: "no license specified (recommended)",
		})
	} else {
		result.Checks = append(result.Checks, CheckResult{
			Name:   "has_license",
			Passed: true,
		})
	}

	return result, nil
}

// ReviewChecklist is a structured checklist for plugin code review.
type ReviewChecklist struct {
	PluginName string       `json:"plugin_name"`
	Version    string       `json:"version"`
	Items      []ReviewItem `json:"items"`
}

// ReviewItem is a single item in the review checklist.
type ReviewItem struct {
	Category    string `json:"category"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Passed      bool   `json:"passed"`
	Notes       string `json:"notes,omitempty"`
}

// NewReviewChecklist creates a standard review checklist for a plugin.
func NewReviewChecklist(manifest *plugin.PluginManifest) *ReviewChecklist {
	return &ReviewChecklist{
		PluginName: manifest.Name,
		Version:    manifest.Version,
		Items: []ReviewItem{
			{Category: "manifest", Description: "Manifest contains valid name, version, author, description", Required: true},
			{Category: "manifest", Description: "Version follows semantic versioning (major.minor.patch)", Required: true},
			{Category: "manifest", Description: "Dependencies use valid semver constraints", Required: true},
			{Category: "code", Description: "Source passes sandbox validation (stdlib-only imports)", Required: true},
			{Category: "code", Description: "Component implements Execute function", Required: true},
			{Category: "code", Description: "Component declares Contract() for input/output documentation", Required: false},
			{Category: "code", Description: "No hardcoded secrets or credentials", Required: true},
			{Category: "testing", Description: "Test files exist and cover primary functionality", Required: true},
			{Category: "testing", Description: "Edge cases and error handling are tested", Required: false},
			{Category: "documentation", Description: "Description clearly explains plugin purpose", Required: true},
			{Category: "documentation", Description: "License is specified", Required: false},
			{Category: "security", Description: "Input validation is present for user-supplied data", Required: true},
			{Category: "security", Description: "No unbounded resource consumption (memory, goroutines)", Required: true},
		},
	}
}

// PassedRequired returns true if all required items are marked as passed.
func (rc *ReviewChecklist) PassedRequired() bool {
	for _, item := range rc.Items {
		if item.Required && !item.Passed {
			return false
		}
	}
	return true
}

// Summary returns a textual summary of the review.
func (rc *ReviewChecklist) Summary() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Review Checklist for %s v%s\n", rc.PluginName, rc.Version))
	b.WriteString(strings.Repeat("=", 50) + "\n\n")

	passed := 0
	total := 0
	for _, item := range rc.Items {
		total++
		status := "[ ]"
		if item.Passed {
			status = "[x]"
			passed++
		}
		req := ""
		if item.Required {
			req = " (required)"
		}
		b.WriteString(fmt.Sprintf("%s [%s] %s%s\n", status, item.Category, item.Description, req))
		if item.Notes != "" {
			b.WriteString(fmt.Sprintf("    Notes: %s\n", item.Notes))
		}
	}
	b.WriteString(fmt.Sprintf("\nResult: %d/%d checks passed\n", passed, total))
	if rc.PassedRequired() {
		b.WriteString("Status: APPROVED (all required checks passed)\n")
	} else {
		b.WriteString("Status: NEEDS REVISION (some required checks failed)\n")
	}
	return b.String()
}
