package config

import (
	"log"

	"gopkg.in/yaml.v3"
)

// CITarget is a generalized build target with a type: discriminator.
// It supersedes the legacy CIBinaryTarget; old configs using binaries:
// are automatically coerced via the CIBuildConfig.UnmarshalYAML shim.
type CITarget struct {
	Name         string                      `json:"name" yaml:"name"`
	Type         string                      `json:"type" yaml:"type"` // go | nodejs | rust | python | custom
	Path         string                      `json:"path,omitempty" yaml:"path,omitempty"`
	Config       map[string]any              `json:"config,omitempty" yaml:"config,omitempty"`
	Environments map[string]*CITargetOverride `json:"environments,omitempty" yaml:"environments,omitempty"`
}

// CITargetOverride holds per-environment config overrides for a CITarget.
type CITargetOverride struct {
	Config map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}

// UnmarshalYAML implements the backcompat shim: if binaries: is present
// (and targets: is absent), coerce each CIBinaryTarget entry to a CITarget
// with type: go, emitting a deprecation warning.
func (b *CIBuildConfig) UnmarshalYAML(value *yaml.Node) error {
	// Use an alias to avoid infinite recursion. The inline embed decodes all
	// CIBuildConfig fields including Targets; we only need to intercept Binaries.
	type buildAlias CIBuildConfig
	var raw struct {
		buildAlias `yaml:",inline"`
		Binaries   []ciBinaryTargetRaw `yaml:"binaries"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*b = CIBuildConfig(raw.buildAlias)

	if len(b.Targets) > 0 {
		// targets: was decoded by buildAlias inline — nothing more to do.
	} else if len(raw.Binaries) > 0 {
		log.Println("WARN: ci.build.binaries is deprecated; migrate to ci.build.targets with type: go")
		b.Targets = make([]CITarget, 0, len(raw.Binaries))
		for _, bin := range raw.Binaries {
			cfg := map[string]any{}
			if bin.LDFlags != "" {
				cfg["ldflags"] = bin.LDFlags
			}
			if len(bin.OS) > 0 {
				cfg["os"] = bin.OS
			}
			if len(bin.Arch) > 0 {
				cfg["arch"] = bin.Arch
			}
			if len(bin.Env) > 0 {
				cfg["env"] = bin.Env
			}
			t := CITarget{
				Name: bin.Name,
				Type: "go",
				Path: bin.Path,
			}
			if len(cfg) > 0 {
				t.Config = cfg
			}
			b.Targets = append(b.Targets, t)
		}
	}
	return nil
}

// ciBinaryTargetRaw mirrors CIBinaryTarget for YAML decoding in the shim.
type ciBinaryTargetRaw struct {
	Name    string            `yaml:"name"`
	Path    string            `yaml:"path"`
	OS      []string          `yaml:"os"`
	Arch    []string          `yaml:"arch"`
	LDFlags string            `yaml:"ldflags"`
	Env     map[string]string `yaml:"env"`
}
