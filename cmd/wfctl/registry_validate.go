package main

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ValidationError represents a single validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationOptions configures validation behavior.
type ValidationOptions struct {
	VerifyURLs      bool   // HEAD-check download URLs
	VerifyChecksums bool   // Verify SHA256 format (not content)
	EngineVersion   string // Current engine version for minEngineVersion check
	TargetOS        string // Filter downloads by OS
	TargetArch      string // Filter downloads by arch
}

var semverRegex = regexp.MustCompile(`^\d+\.\d+\.\d+`)
var sha256Regex = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
var validPluginTypes = map[string]bool{"builtin": true, "external": true, "ui": true}
var validPluginTiers = map[string]bool{"core": true, "community": true, "premium": true}
var validDownloadOS = map[string]bool{"linux": true, "darwin": true, "windows": true}
var validDownloadArch = map[string]bool{"amd64": true, "arm64": true}

// ValidateManifest performs full validation of a registry manifest.
func ValidateManifest(m *RegistryManifest, opts ValidationOptions) []ValidationError {
	var errs []ValidationError

	// Required fields
	if m.Name == "" {
		errs = append(errs, ValidationError{Field: "name", Message: "required field is empty"})
	}
	if m.Version == "" {
		errs = append(errs, ValidationError{Field: "version", Message: "required field is empty"})
	} else if !semverRegex.MatchString(m.Version) {
		errs = append(errs, ValidationError{Field: "version", Message: fmt.Sprintf("must be semver format (got %q)", m.Version)})
	}
	if m.Author == "" {
		errs = append(errs, ValidationError{Field: "author", Message: "required field is empty"})
	}
	if m.Description == "" {
		errs = append(errs, ValidationError{Field: "description", Message: "required field is empty"})
	}
	if m.Type == "" {
		errs = append(errs, ValidationError{Field: "type", Message: "required field is empty"})
	} else if !validPluginTypes[m.Type] {
		errs = append(errs, ValidationError{Field: "type", Message: fmt.Sprintf("must be one of: builtin, external, ui (got %q)", m.Type)})
	}
	if m.Tier == "" {
		errs = append(errs, ValidationError{Field: "tier", Message: "required field is empty"})
	} else if !validPluginTiers[m.Tier] {
		errs = append(errs, ValidationError{Field: "tier", Message: fmt.Sprintf("must be one of: core, community, premium (got %q)", m.Tier)})
	}
	if m.License == "" {
		errs = append(errs, ValidationError{Field: "license", Message: "required field is empty"})
	}

	// MinEngineVersion format check
	if m.MinEngineVersion != "" && !semverRegex.MatchString(m.MinEngineVersion) {
		errs = append(errs, ValidationError{Field: "minEngineVersion", Message: fmt.Sprintf("must be semver format (got %q)", m.MinEngineVersion)})
	}

	// Engine version compatibility check
	if opts.EngineVersion != "" && m.MinEngineVersion != "" {
		if compareSemver(opts.EngineVersion, m.MinEngineVersion) < 0 {
			errs = append(errs, ValidationError{
				Field:   "minEngineVersion",
				Message: fmt.Sprintf("requires engine %s but current engine is %s", m.MinEngineVersion, opts.EngineVersion),
			})
		}
	}

	// Downloads validation
	if m.Type == "external" && len(m.Downloads) == 0 {
		errs = append(errs, ValidationError{Field: "downloads", Message: "external plugins must have at least one download entry"})
	}
	for i, dl := range m.Downloads {
		prefix := fmt.Sprintf("downloads[%d]", i)
		if !validDownloadOS[dl.OS] {
			errs = append(errs, ValidationError{Field: prefix + ".os", Message: fmt.Sprintf("must be one of: linux, darwin, windows (got %q)", dl.OS)})
		}
		if !validDownloadArch[dl.Arch] {
			errs = append(errs, ValidationError{Field: prefix + ".arch", Message: fmt.Sprintf("must be one of: amd64, arm64 (got %q)", dl.Arch)})
		}
		if dl.URL == "" {
			errs = append(errs, ValidationError{Field: prefix + ".url", Message: "required field is empty"})
		}
		if dl.SHA256 != "" && !sha256Regex.MatchString(dl.SHA256) {
			errs = append(errs, ValidationError{Field: prefix + ".sha256", Message: fmt.Sprintf("must be 64-character hex string (got %q)", dl.SHA256)})
		}
	}

	// URL reachability check
	if opts.VerifyURLs {
		client := &http.Client{Timeout: 10 * time.Second}
		for i, dl := range m.Downloads {
			if dl.URL == "" {
				continue
			}
			if opts.TargetOS != "" && dl.OS != opts.TargetOS {
				continue
			}
			if opts.TargetArch != "" && dl.Arch != opts.TargetArch {
				continue
			}
			resp, err := client.Head(dl.URL) //nolint:gosec // URL from registry manifest
			if err != nil {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("downloads[%d].url", i),
					Message: fmt.Sprintf("URL unreachable: %v", err),
				})
			} else {
				resp.Body.Close()
				if resp.StatusCode >= 400 {
					errs = append(errs, ValidationError{
						Field:   fmt.Sprintf("downloads[%d].url", i),
						Message: fmt.Sprintf("URL returned HTTP %d", resp.StatusCode),
					})
				}
			}
		}
	}

	return errs
}

// compareSemver compares two semver strings. Returns -1, 0, or 1.
func compareSemver(a, b string) int {
	parseVer := func(s string) (int, int, int) {
		var major, minor, patch int
		fmt.Sscanf(s, "%d.%d.%d", &major, &minor, &patch) //nolint:errcheck // format is validated by semverRegex
		return major, minor, patch
	}
	aMaj, aMin, aPat := parseVer(a)
	bMaj, bMin, bPat := parseVer(b)
	if aMaj != bMaj {
		if aMaj < bMaj {
			return -1
		}
		return 1
	}
	if aMin != bMin {
		if aMin < bMin {
			return -1
		}
		return 1
	}
	if aPat != bPat {
		if aPat < bPat {
			return -1
		}
		return 1
	}
	return 0
}

// FormatValidationErrors formats validation errors for display.
func FormatValidationErrors(errs []ValidationError) string {
	if len(errs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, e := range errs {
		fmt.Fprintf(&b, "  - %s: %s\n", e.Field, e.Message)
	}
	return b.String()
}

