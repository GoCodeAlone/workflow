package module

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// ValidationSeverity indicates how severe a validation issue is.
type ValidationSeverity string

const (
	SeverityError   ValidationSeverity = "error"
	SeverityWarning ValidationSeverity = "warning"
	SeverityInfo    ValidationSeverity = "info"
)

// ValidationIssue represents a single problem found during module validation.
type ValidationIssue struct {
	Severity ValidationSeverity
	Field    string
	Message  string
}

func (v ValidationIssue) String() string {
	return fmt.Sprintf("[%s] %s: %s", v.Severity, v.Field, v.Message)
}

// serviceNameRe matches valid service names: lowercase, dot-separated segments.
var serviceNameRe = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)*(-[a-z0-9]+)*$`)

// ValidateModule checks a module implementation for common issues and returns
// all detected problems. A well-implemented module should produce zero issues.
func ValidateModule(m modular.Module) []ValidationIssue {
	var issues []ValidationIssue

	// Check module name
	name := m.Name()
	if name == "" {
		issues = append(issues, ValidationIssue{
			Severity: SeverityError,
			Field:    "Name()",
			Message:  "module name must not be empty",
		})
	} else if strings.Contains(name, " ") {
		issues = append(issues, ValidationIssue{
			Severity: SeverityWarning,
			Field:    "Name()",
			Message:  fmt.Sprintf("module name %q contains spaces; prefer lowercase dot-separated names", name),
		})
	}

	// Check ProvidesServices
	type serviceProvider interface {
		ProvidesServices() []modular.ServiceProvider
	}
	if sp, ok := m.(serviceProvider); ok {
		services := sp.ProvidesServices()
		if len(services) == 0 {
			issues = append(issues, ValidationIssue{
				Severity: SeverityWarning,
				Field:    "ProvidesServices()",
				Message:  "module declares ProvidesServices but returns no services; it will be invisible to the dependency graph",
			})
		}

		seen := make(map[string]bool)
		for i, svc := range services {
			field := fmt.Sprintf("ProvidesServices()[%d]", i)

			if svc.Name == "" {
				issues = append(issues, ValidationIssue{
					Severity: SeverityError,
					Field:    field + ".Name",
					Message:  "service name must not be empty",
				})
			} else {
				if seen[svc.Name] {
					issues = append(issues, ValidationIssue{
						Severity: SeverityError,
						Field:    field + ".Name",
						Message:  fmt.Sprintf("duplicate service name %q", svc.Name),
					})
				}
				seen[svc.Name] = true

				if !serviceNameRe.MatchString(svc.Name) {
					issues = append(issues, ValidationIssue{
						Severity: SeverityWarning,
						Field:    field + ".Name",
						Message:  fmt.Sprintf("service name %q does not follow naming convention (lowercase, dot-separated)", svc.Name),
					})
				}
			}

			if svc.Instance == nil {
				issues = append(issues, ValidationIssue{
					Severity: SeverityError,
					Field:    field + ".Instance",
					Message:  fmt.Sprintf("service %q has nil Instance", svc.Name),
				})
			}
		}
	} else {
		issues = append(issues, ValidationIssue{
			Severity: SeverityInfo,
			Field:    "ProvidesServices()",
			Message:  "module does not implement ProvidesServices; it cannot provide services to other modules",
		})
	}

	// Check RequiresServices
	type serviceRequirer interface {
		RequiresServices() []modular.ServiceDependency
	}
	if sr, ok := m.(serviceRequirer); ok {
		deps := sr.RequiresServices()
		seen := make(map[string]bool)
		for i, dep := range deps {
			field := fmt.Sprintf("RequiresServices()[%d]", i)

			if dep.Name == "" {
				issues = append(issues, ValidationIssue{
					Severity: SeverityError,
					Field:    field + ".Name",
					Message:  "dependency name must not be empty",
				})
			} else if seen[dep.Name] {
				issues = append(issues, ValidationIssue{
					Severity: SeverityWarning,
					Field:    field + ".Name",
					Message:  fmt.Sprintf("duplicate dependency name %q", dep.Name),
				})
			}
			seen[dep.Name] = true
		}
	}

	return issues
}
