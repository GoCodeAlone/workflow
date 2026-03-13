package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/GoCodeAlone/workflow/dynamic"
	"github.com/GoCodeAlone/workflow/modernize"
	"github.com/GoCodeAlone/workflow/schema"
)

// PluginTier indicates the support and licensing tier for a plugin.
type PluginTier string

const (
	// TierCore identifies plugins that are built-in and always available.
	TierCore PluginTier = "core"
	// TierCommunity identifies open-source community-contributed plugins.
	TierCommunity PluginTier = "community"
	// TierPremium identifies plugins that require a valid license to use.
	TierPremium PluginTier = "premium"
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
	Tier         PluginTier             `json:"tier,omitempty" yaml:"tier,omitempty"`

	// Engine plugin declarations
	Capabilities  []CapabilityDecl `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	ModuleTypes   []string         `json:"moduleTypes,omitempty" yaml:"moduleTypes,omitempty"`
	StepTypes     []string         `json:"stepTypes,omitempty" yaml:"stepTypes,omitempty"`
	TriggerTypes  []string         `json:"triggerTypes,omitempty" yaml:"triggerTypes,omitempty"`
	WorkflowTypes []string         `json:"workflowTypes,omitempty" yaml:"workflowTypes,omitempty"`
	WiringHooks   []string         `json:"wiringHooks,omitempty" yaml:"wiringHooks,omitempty"`

	// StepSchemas provides schema definitions for step types registered by this plugin.
	// Used by MCP/LSP for hover docs, completions, and output documentation.
	StepSchemas []*schema.StepSchema `json:"stepSchemas,omitempty" yaml:"stepSchemas,omitempty"`

	// ModernizeRules declares migration rules for the wfctl modernize command.
	// Each rule describes a common migration pattern (type renames, config key
	// renames) that users of this plugin may need to apply when upgrading.
	// These rules are loaded automatically when --plugin-dir is passed to
	// wfctl modernize or wfctl mcp.
	ModernizeRules []modernize.ManifestRule `json:"modernizeRules,omitempty" yaml:"modernizeRules,omitempty"`

	// OverridableTypes lists type names (modules, steps, triggers, handlers) that may be
	// overridden by later-loaded plugins without requiring LoadPluginWithOverride.
	// Typically used for placeholder/mock implementations.
	OverridableTypes []string `json:"overridableTypes,omitempty" yaml:"overridableTypes,omitempty"`

	// Config mutability and sample plugin support
	ConfigMutable  bool   `json:"configMutable,omitempty" yaml:"configMutable,omitempty"`
	SampleCategory string `json:"sampleCategory,omitempty" yaml:"sampleCategory,omitempty"`
}

// CapabilityDecl declares a capability relationship for a plugin in the manifest.
type CapabilityDecl struct {
	Name     string `json:"name" yaml:"name"`
	Role     string `json:"role" yaml:"role"` // "provider" or "consumer"
	Priority int    `json:"priority,omitempty" yaml:"priority,omitempty"`
}

// legacyCapabilitiesObject represents the alternative object-style capabilities
// field used by some external plugin manifests (e.g. workflow-plugin-authz) where
// capabilities is a single JSON object instead of an array of CapabilityDecl.
type legacyCapabilitiesObject struct {
	ConfigProvider bool     `json:"configProvider"`
	ModuleTypes    []string `json:"moduleTypes"`
	StepTypes      []string `json:"stepTypes"`
	TriggerTypes   []string `json:"triggerTypes"`
	WorkflowTypes  []string `json:"workflowTypes"`
}

// UnmarshalJSON implements custom JSON decoding for PluginManifest so that the
// "capabilities" field can be either:
//   - an array of CapabilityDecl objects (the canonical engine format), or
//   - a plain JSON object with moduleTypes/stepTypes/triggerTypes keys
//     (the legacy external-plugin format used by e.g. workflow-plugin-authz).
//
// In the legacy-object case the type lists are merged into the manifest's
// top-level ModuleTypes/StepTypes/TriggerTypes fields so the information is
// not lost, and Capabilities is left nil.
func (m *PluginManifest) UnmarshalJSON(data []byte) error {
	// Use a type alias to avoid infinite recursion through UnmarshalJSON.
	type Alias PluginManifest
	type rawManifest struct {
		Alias
		Capabilities json.RawMessage `json:"capabilities,omitempty"`
	}

	var raw rawManifest
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*m = PluginManifest(raw.Alias)

	if len(raw.Capabilities) == 0 {
		return nil
	}

	// Peek at the first non-whitespace byte to decide which branch to take.
	// This avoids silently ignoring genuinely invalid values.
	firstByte := firstNonSpace(raw.Capabilities)

	switch firstByte {
	case 0, 'n':
		// Empty or JSON null – treat as absent.

	case '[':
		// Canonical array-of-CapabilityDecl format.
		var caps []CapabilityDecl
		if err := json.Unmarshal(raw.Capabilities, &caps); err != nil {
			return fmt.Errorf("capabilities: %w", err)
		}
		m.Capabilities = caps

	case '{':
		// Legacy object format – extract type lists into top-level fields.
		var legacy legacyCapabilitiesObject
		if err := json.Unmarshal(raw.Capabilities, &legacy); err != nil {
			return fmt.Errorf("capabilities: %w", err)
		}
		m.ModuleTypes = appendUnique(m.ModuleTypes, legacy.ModuleTypes...)
		m.StepTypes = appendUnique(m.StepTypes, legacy.StepTypes...)
		m.TriggerTypes = appendUnique(m.TriggerTypes, legacy.TriggerTypes...)
		m.WorkflowTypes = appendUnique(m.WorkflowTypes, legacy.WorkflowTypes...)

	default:
		return fmt.Errorf("capabilities: unsupported JSON type (expected array or object, got %q)", string(raw.Capabilities))
	}

	return nil
}

// firstNonSpace returns the first non-whitespace byte in b, or 0 if b is empty/all-whitespace.
func firstNonSpace(b []byte) byte {
	for _, c := range b {
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			return c
		}
	}
	return 0
}

// appendUnique appends values to dst, skipping any that are already present.
func appendUnique(dst []string, values ...string) []string {
	existing := make(map[string]struct{}, len(dst))
	for _, v := range dst {
		existing[v] = struct{}{}
	}
	for _, v := range values {
		if _, ok := existing[v]; !ok {
			dst = append(dst, v)
			existing[v] = struct{}{}
		}
	}
	return dst
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
