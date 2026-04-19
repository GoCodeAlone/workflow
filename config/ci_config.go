package config

import (
	"errors"
	"fmt"
	"time"
)

// CIConfig holds the ci: section of a workflow config — build, test, and deploy lifecycle.
type CIConfig struct {
	Build      *CIBuildConfig  `json:"build,omitempty" yaml:"build,omitempty"`
	Test       *CITestConfig   `json:"test,omitempty" yaml:"test,omitempty"`
	Deploy     *CIDeployConfig `json:"deploy,omitempty" yaml:"deploy,omitempty"`
	Infra      *CIInfraConfig  `json:"infra,omitempty" yaml:"infra,omitempty"`
	Registries []CIRegistry    `json:"registries,omitempty" yaml:"registries,omitempty"`
}

// CIBuildConfig defines what artifacts the build phase produces.
// UnmarshalYAML is implemented in ci_target.go to handle the binaries:→targets: migration.
type CIBuildConfig struct {
	// Targets is the canonical field (type-dispatched). Populated from binaries: (legacy) or targets:.
	Targets    []CITarget          `json:"targets,omitempty" yaml:"targets,omitempty"`
	Containers []CIContainerTarget `json:"containers,omitempty" yaml:"containers,omitempty"`
	Assets     []CIAssetTarget     `json:"assets,omitempty" yaml:"assets,omitempty"`
	Security   *CIBuildSecurity    `json:"security,omitempty" yaml:"security,omitempty"`
}

// CIBinaryTarget is a Go binary to compile.
type CIBinaryTarget struct {
	Name    string            `json:"name" yaml:"name"`
	Path    string            `json:"path" yaml:"path"`
	OS      []string          `json:"os,omitempty" yaml:"os,omitempty"`
	Arch    []string          `json:"arch,omitempty" yaml:"arch,omitempty"`
	LDFlags string            `json:"ldflags,omitempty" yaml:"ldflags,omitempty"`
	Env     map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
}

// CIContainerTarget is a container image to build.
type CIContainerTarget struct {
	Name       string `json:"name" yaml:"name"`
	Dockerfile string `json:"dockerfile,omitempty" yaml:"dockerfile,omitempty"`
	Context    string `json:"context,omitempty" yaml:"context,omitempty"`
	Registry   string `json:"registry,omitempty" yaml:"registry,omitempty"`
	Tag        string `json:"tag,omitempty" yaml:"tag,omitempty"`

	// Method selects the build driver: "dockerfile" (default) or "ko".
	Method      string              `json:"method,omitempty" yaml:"method,omitempty"`
	KoPackage   string              `json:"ko_package,omitempty" yaml:"ko_package,omitempty"`
	KoBaseImage string              `json:"ko_base_image,omitempty" yaml:"ko_base_image,omitempty"`
	KoBare      bool                `json:"ko_bare,omitempty" yaml:"ko_bare,omitempty"`
	Platforms   []string            `json:"platforms,omitempty" yaml:"platforms,omitempty"`
	BuildArgs   map[string]string   `json:"build_args,omitempty" yaml:"build_args,omitempty"`
	Secrets     []CIContainerSecret `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	Cache       *CIContainerCache   `json:"cache,omitempty" yaml:"cache,omitempty"`
	Target      string              `json:"target,omitempty" yaml:"target,omitempty"`
	Labels      map[string]string   `json:"labels,omitempty" yaml:"labels,omitempty"`
	ExtraFlags  []string            `json:"extra_flags,omitempty" yaml:"extra_flags,omitempty"`
	External    bool                `json:"external,omitempty" yaml:"external,omitempty"`
	Source      *CIExternalSource   `json:"source,omitempty" yaml:"source,omitempty"`
	PushTo      []string            `json:"push_to,omitempty" yaml:"push_to,omitempty"`
}

// CIContainerSecret passes a BuildKit secret into a docker build step.
type CIContainerSecret struct {
	ID  string `json:"id" yaml:"id"`
	Env string `json:"env,omitempty" yaml:"env,omitempty"`
	Src string `json:"src,omitempty" yaml:"src,omitempty"`
}

// CIContainerCache configures BuildKit layer cache import/export.
type CIContainerCache struct {
	From []CIContainerCacheRef `json:"from,omitempty" yaml:"from,omitempty"`
	To   []CIContainerCacheRef `json:"to,omitempty" yaml:"to,omitempty"`
}

// CIContainerCacheRef is a single cache reference (type + ref).
type CIContainerCacheRef struct {
	Type string `json:"type,omitempty" yaml:"type,omitempty"` // registry | local | gha
	Ref  string `json:"ref,omitempty" yaml:"ref,omitempty"`
}

// CIExternalSource is an upstream image to pull and re-push rather than build locally.
type CIExternalSource struct {
	Ref     string         `json:"ref" yaml:"ref"`
	TagFrom []TagFromEntry `json:"tag_from,omitempty" yaml:"tag_from,omitempty"`
}

// TagFromEntry is one step in a tag resolution chain.
// The first non-empty result wins; Command is run via sh -c.
type TagFromEntry struct {
	Env     string `json:"env,omitempty" yaml:"env,omitempty"`
	Command string `json:"command,omitempty" yaml:"command,omitempty"`
}

// CIAssetTarget is a non-binary build artifact (e.g., frontend bundle).
type CIAssetTarget struct {
	Name  string `json:"name" yaml:"name"`
	Build string `json:"build" yaml:"build"`
	Path  string `json:"path" yaml:"path"`
}

// CITestConfig defines test phases.
type CITestConfig struct {
	Unit        *CITestPhase `json:"unit,omitempty" yaml:"unit,omitempty"`
	Integration *CITestPhase `json:"integration,omitempty" yaml:"integration,omitempty"`
	E2E         *CITestPhase `json:"e2e,omitempty" yaml:"e2e,omitempty"`
}

// CITestPhase is a single test phase.
type CITestPhase struct {
	Command  string   `json:"command" yaml:"command"`
	Coverage bool     `json:"coverage,omitempty" yaml:"coverage,omitempty"`
	Needs    []string `json:"needs,omitempty" yaml:"needs,omitempty"`
}

// CIDeployConfig defines deployment environments.
type CIDeployConfig struct {
	Environments map[string]*CIDeployEnvironment `json:"environments,omitempty" yaml:"environments,omitempty"`
}

// CIDeployEnvironment is a single deployment target.
type CIDeployEnvironment struct {
	Provider        string         `json:"provider" yaml:"provider"`
	Cluster         string         `json:"cluster,omitempty" yaml:"cluster,omitempty"`
	Namespace       string         `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Region          string         `json:"region,omitempty" yaml:"region,omitempty"`
	Strategy        string         `json:"strategy,omitempty" yaml:"strategy,omitempty"`
	RequireApproval bool           `json:"requireApproval,omitempty" yaml:"requireApproval,omitempty"`
	PreDeploy       []string       `json:"preDeploy,omitempty" yaml:"preDeploy,omitempty"`
	HealthCheck     *CIHealthCheck `json:"healthCheck,omitempty" yaml:"healthCheck,omitempty"`
}

// CIHealthCheck defines how to verify a deployment is healthy.
type CIHealthCheck struct {
	Path    string `json:"path" yaml:"path"`
	Timeout string `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}

// CIInfraConfig defines infrastructure provisioning for CI.
type CIInfraConfig struct {
	Provision    bool                  `json:"provision" yaml:"provision"`
	StateBackend string                `json:"stateBackend,omitempty" yaml:"stateBackend,omitempty"`
	Resources    []InfraResourceConfig `json:"resources,omitempty" yaml:"resources,omitempty"`
}

// Validate checks the CIConfig for required fields.
func (c *CIConfig) Validate() error {
	if c == nil {
		return nil
	}
	var errs []error
	if c.Build != nil {
		for _, target := range c.Build.Targets {
			if target.Name == "" {
				errs = append(errs, fmt.Errorf("ci.build.targets: name is required"))
			}
			if target.Path == "" && target.Type != "custom" {
				errs = append(errs, fmt.Errorf("ci.build.targets[%s]: path is required", target.Name))
			}
		}
	}
	if c.Build != nil {
		for i := range c.Build.Containers {
			ctr := &c.Build.Containers[i]
			method := ctr.Method
			switch method {
			case "dockerfile", "":
				if ctr.Dockerfile == "" {
					errs = append(errs, fmt.Errorf("ci.build.containers[%d] (%s): method=dockerfile requires dockerfile field", i, ctr.Name))
				}
			case "ko":
				if ctr.KoPackage == "" {
					errs = append(errs, fmt.Errorf("ci.build.containers[%d] (%s): method=ko requires ko_package", i, ctr.Name))
				}
			default:
				errs = append(errs, fmt.Errorf("ci.build.containers[%d] (%s): unknown method %q (allowed: dockerfile, ko)", i, ctr.Name, method))
			}
		}

		const knownTargetTypes = "go nodejs rust python custom"
		for i, target := range c.Build.Targets {
			if target.Type != "" && !containsWord(knownTargetTypes, target.Type) {
				errs = append(errs, fmt.Errorf("ci.build.targets[%d] (%s): unknown type %q (allowed: go, nodejs, rust, python, custom)", i, target.Name, target.Type))
			}
		}
	}

	// Build registry name index for push_to cross-reference validation.
	registryNames := make(map[string]bool, len(c.Registries))
	for i, reg := range c.Registries {
		if registryNames[reg.Name] {
			errs = append(errs, fmt.Errorf("ci.registries[%d]: duplicate name %q", i, reg.Name))
		}
		registryNames[reg.Name] = true

		const knownRegistryTypes = "do ghcr ecr gcr dockerhub acr"
		if reg.Type != "" && !containsWord(knownRegistryTypes, reg.Type) {
			errs = append(errs, fmt.Errorf("ci.registries[%d] (%s): unknown type %q (allowed: do, ghcr, ecr, gcr, dockerhub, acr)", i, reg.Name, reg.Type))
		}

		if reg.Retention != nil {
			if reg.Retention.KeepLatest < 1 {
				errs = append(errs, fmt.Errorf("ci.registries[%d] (%s): retention.keep_latest must be ≥ 1", i, reg.Name))
			}
			if reg.Retention.UntaggedTTL != "" {
				if _, err := time.ParseDuration(reg.Retention.UntaggedTTL); err != nil {
					errs = append(errs, fmt.Errorf("ci.registries[%d] (%s): retention.untagged_ttl %q is not a valid duration", i, reg.Name, reg.Retention.UntaggedTTL))
				}
			}
		}
	}

	// Validate push_to references declared registries.
	if c.Build != nil {
		for i := range c.Build.Containers {
			ctr := &c.Build.Containers[i]
			for _, ref := range ctr.PushTo {
				if !registryNames[ref] {
					errs = append(errs, fmt.Errorf("ci.build.containers[%d] (%s): push_to %q references undeclared registry", i, ctr.Name, ref))
				}
			}
		}
	}

	if c.Deploy != nil {
		for name, env := range c.Deploy.Environments {
			if env.Provider == "" {
				errs = append(errs, fmt.Errorf("ci.deploy.environments[%s]: provider is required", name))
			}
		}
	}
	return errors.Join(errs...)
}

// ValidateWithWarnings runs Validate and additionally collects non-fatal
// supply-chain warnings (e.g. security.hardened=false opt-out).
func (c *CIConfig) ValidateWithWarnings() (warnings []string, err error) {
	if c == nil {
		return nil, nil
	}
	err = c.Validate()

	if c.Build != nil && c.Build.Security != nil && !c.Build.Security.Hardened {
		warnings = append(warnings, "hardened defaults disabled — images may not meet supply-chain baseline")
	}
	return warnings, err
}

// applyBuildDefaults fills in opinionated secure defaults for ci.build.security
// when the field is absent or partially specified. Called automatically by LoadFromFile.
func (cfg *WorkflowConfig) applyBuildDefaults() {
	if cfg.CI == nil || cfg.CI.Build == nil {
		return
	}
	cfg.CI.Build.Security = cfg.CI.Build.Security.ApplyDefaults()
}

// containsWord reports whether word appears as a whitespace-separated token in s.
func containsWord(s, word string) bool {
	for len(s) > 0 {
		var tok string
		if i := len(s); i == 0 {
			break
		}
		for i, c := range s {
			if c == ' ' {
				tok, s = s[:i], s[i+1:]
				break
			} else if i == len(s)-1 {
				tok, s = s, ""
				break
			}
		}
		if tok == word {
			return true
		}
	}
	return false
}
