// Package builder defines the Builder interface and supporting types for
// the wfctl build pipeline. Implementations live in plugins/builder-*.
package builder

import "context"

// Builder is the contract every build plugin must satisfy.
type Builder interface {
	// Name returns the unique identifier used in ci.build.targets[].type.
	Name() string
	// Validate checks the Config without executing anything.
	Validate(cfg Config) error
	// Build executes the build and populates out with produced artifacts.
	Build(ctx context.Context, cfg Config, out *Outputs) error
	// SecurityLint inspects the config for supply-chain issues and returns
	// zero or more findings. It must not execute any build steps.
	SecurityLint(cfg Config) []Finding
}

// Config carries all inputs a Builder needs to produce its artifacts.
type Config struct {
	TargetName string
	Path       string
	Fields     map[string]any
	Env        map[string]string
	Security   *SecurityConfig
}

// SecurityConfig mirrors the relevant subset of CIBuildSecurity so builders
// don't need to import the config package.
type SecurityConfig struct {
	Hardened   bool
	SBOM       bool
	Provenance string
	NonRoot    bool
}

// Outputs accumulates the artifacts produced by a single Build call.
type Outputs struct {
	Artifacts []Artifact
}

// Artifact is a single file or image produced by a builder.
type Artifact struct {
	Name     string
	Kind     string // binary | image | bundle | other
	Paths    []string
	Metadata map[string]any
}

// Finding is a security or policy issue found by SecurityLint.
type Finding struct {
	Severity string // info | warn | critical
	Message  string
	File     string
	Line     int
}
