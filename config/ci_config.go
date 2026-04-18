package config

import (
	"errors"
	"fmt"
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
type CIBuildConfig struct {
	Binaries   []CIBinaryTarget    `json:"binaries,omitempty" yaml:"binaries,omitempty"`
	Containers []CIContainerTarget `json:"containers,omitempty" yaml:"containers,omitempty"`
	Assets     []CIAssetTarget     `json:"assets,omitempty" yaml:"assets,omitempty"`
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
		for _, bin := range c.Build.Binaries {
			if bin.Name == "" {
				errs = append(errs, fmt.Errorf("ci.build.binaries: name is required"))
			}
			if bin.Path == "" {
				errs = append(errs, fmt.Errorf("ci.build.binaries[%s]: path is required", bin.Name))
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
