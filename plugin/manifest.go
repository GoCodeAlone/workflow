package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/GoCodeAlone/workflow/dynamic"
)

// PluginManifest describes a plugin's metadata, dependencies, and contract.
type PluginManifest struct {
	Name         string                 `json:"name" yaml:"name"`
	Version      string                 `json:"version" yaml:"version"`
	Author       string                 `json:"author" yaml:"author"`
	Description  string                 `json:"description" yaml:"description"`
	License      string                 `json:"license,omitempty" yaml:"license,omitempty"`
	Dependencies []Dependency           `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	Contract     *dynamic.FieldContract `json:"contract,omitempty" yaml:"contract,omitempty"`
	Tags         []string               `json:"tags,omitempty" yaml:"tags,omitempty"`
	Repository   string                 `json:"repository,omitempty" yaml:"repository,omitempty"`
}

// Dependency declares a versioned dependency on another plugin.
type Dependency struct {
	Name       string `json:"name" yaml:"name"`
	Constraint string `json:"constraint" yaml:"constraint"` // semver constraint, e.g. ">=1.0.0", "^2.1"
}

// Validate checks that a manifest has all required fields and valid semver.
func (m *PluginManifest) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("manifest: name is required")
	}
	if !isValidPluginName(m.Name) {
		return fmt.Errorf("manifest: name %q must be lowercase alphanumeric with hyphens", m.Name)
	}
	if m.Version == "" {
		return fmt.Errorf("manifest: version is required")
	}
	if _, err := ParseSemver(m.Version); err != nil {
		return fmt.Errorf("manifest: invalid version %q: %w", m.Version, err)
	}
	if m.Author == "" {
		return fmt.Errorf("manifest: author is required")
	}
	if m.Description == "" {
		return fmt.Errorf("manifest: description is required")
	}
	for _, dep := range m.Dependencies {
		if dep.Name == "" {
			return fmt.Errorf("manifest: dependency name is required")
		}
		if dep.Constraint == "" {
			return fmt.Errorf("manifest: dependency %q constraint is required", dep.Name)
		}
		if _, err := ParseConstraint(dep.Constraint); err != nil {
			return fmt.Errorf("manifest: dependency %q has invalid constraint %q: %w", dep.Name, dep.Constraint, err)
		}
	}
	return nil
}

var pluginNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

func isValidPluginName(name string) bool {
	if len(name) < 2 {
		return len(name) == 1 && name[0] >= 'a' && name[0] <= 'z'
	}
	return pluginNameRe.MatchString(name)
}

// LoadManifest reads a manifest from a JSON file.
func LoadManifest(path string) (*PluginManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m PluginManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

// Semver represents a parsed semantic version.
type Semver struct {
	Major int
	Minor int
	Patch int
}

func (s Semver) String() string {
	return fmt.Sprintf("%d.%d.%d", s.Major, s.Minor, s.Patch)
}

// Compare returns -1, 0, or 1.
func (s Semver) Compare(other Semver) int {
	if s.Major != other.Major {
		if s.Major < other.Major {
			return -1
		}
		return 1
	}
	if s.Minor != other.Minor {
		if s.Minor < other.Minor {
			return -1
		}
		return 1
	}
	if s.Patch != other.Patch {
		if s.Patch < other.Patch {
			return -1
		}
		return 1
	}
	return 0
}

// ParseSemver parses a version string like "1.2.3" into a Semver.
func ParseSemver(v string) (Semver, error) {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return Semver{}, fmt.Errorf("expected major.minor.patch, got %q", v)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return Semver{}, fmt.Errorf("invalid major version: %w", err)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return Semver{}, fmt.Errorf("invalid minor version: %w", err)
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return Semver{}, fmt.Errorf("invalid patch version: %w", err)
	}
	return Semver{Major: major, Minor: minor, Patch: patch}, nil
}

// Constraint represents a semver constraint that can check version compatibility.
type Constraint struct {
	Op      string
	Version Semver
}

// ParseConstraint parses a constraint string like ">=1.0.0", "^2.1.0", "~1.2.0".
func ParseConstraint(s string) (*Constraint, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty constraint")
	}

	var op string
	var vStr string

	switch {
	case strings.HasPrefix(s, ">="):
		op = ">="
		vStr = strings.TrimPrefix(s, ">=")
	case strings.HasPrefix(s, "<="):
		op = "<="
		vStr = strings.TrimPrefix(s, "<=")
	case strings.HasPrefix(s, "!="):
		op = "!="
		vStr = strings.TrimPrefix(s, "!=")
	case strings.HasPrefix(s, ">"):
		op = ">"
		vStr = strings.TrimPrefix(s, ">")
	case strings.HasPrefix(s, "<"):
		op = "<"
		vStr = strings.TrimPrefix(s, "<")
	case strings.HasPrefix(s, "^"):
		op = "^"
		vStr = strings.TrimPrefix(s, "^")
	case strings.HasPrefix(s, "~"):
		op = "~"
		vStr = strings.TrimPrefix(s, "~")
	case strings.HasPrefix(s, "="):
		op = "="
		vStr = strings.TrimPrefix(s, "=")
	default:
		op = "="
		vStr = s
	}

	v, err := ParseSemver(strings.TrimSpace(vStr))
	if err != nil {
		return nil, err
	}
	return &Constraint{Op: op, Version: v}, nil
}

// Check returns true if the given version satisfies the constraint.
func (c *Constraint) Check(v Semver) bool {
	cmp := v.Compare(c.Version)
	switch c.Op {
	case "=":
		return cmp == 0
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	case "!=":
		return cmp != 0
	case "^":
		// Compatible with: same major, >= constraint version
		if v.Major != c.Version.Major {
			return false
		}
		return cmp >= 0
	case "~":
		// Approximately: same major.minor, >= constraint version
		if v.Major != c.Version.Major || v.Minor != c.Version.Minor {
			return false
		}
		return cmp >= 0
	}
	return false
}

// CheckVersion checks if a version string satisfies a constraint string.
func CheckVersion(version, constraint string) (bool, error) {
	v, err := ParseSemver(version)
	if err != nil {
		return false, fmt.Errorf("invalid version: %w", err)
	}
	c, err := ParseConstraint(constraint)
	if err != nil {
		return false, fmt.Errorf("invalid constraint: %w", err)
	}
	return c.Check(v), nil
}
