// Package cicd provides a plugin that registers CI/CD pipeline step types:
// shell_exec, artifact_pull, artifact_push, docker_build, docker_push,
// docker_run, scan_sast, scan_container, scan_deps, deploy, gate, build_ui,
// build_from_config.
package cicd

import (
	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// Plugin registers CI/CD pipeline step factories.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new CI/CD plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "cicd",
				PluginVersion:     "1.0.0",
				PluginDescription: "CI/CD pipeline step types (shell exec, Docker, artifact management, security scanning, deploy, gate, build from config)",
			},
			Manifest: plugin.PluginManifest{
				Name:        "cicd",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "CI/CD pipeline step types (shell exec, Docker, artifact management, security scanning, deploy, gate, build from config)",
				Tier:        plugin.TierCore,
				StepTypes: []string{
					"step.shell_exec",
					"step.artifact_pull",
					"step.artifact_push",
					"step.docker_build",
					"step.docker_push",
					"step.docker_run",
					"step.scan_sast",
					"step.scan_container",
					"step.scan_deps",
					"step.deploy",
					"step.gate",
					"step.build_ui",
					"step.build_from_config",
				},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "cicd-pipeline", Role: "provider", Priority: 50},
				},
			},
		},
	}
}

// Capabilities returns the capability contracts defined by this plugin.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "cicd-pipeline",
			Description: "CI/CD pipeline operations: shell exec, Docker, artifact management, security scanning, deploy, gate, build from config",
		},
	}
}

// StepFactories returns the CI/CD step factories.
func (p *Plugin) StepFactories() map[string]plugin.StepFactory {
	return map[string]plugin.StepFactory{
		"step.shell_exec":        wrapStepFactory(module.NewShellExecStepFactory()),
		"step.artifact_pull":     wrapStepFactory(module.NewArtifactPullStepFactory()),
		"step.artifact_push":     wrapStepFactory(module.NewArtifactPushStepFactory()),
		"step.docker_build":      wrapStepFactory(module.NewDockerBuildStepFactory()),
		"step.docker_push":       wrapStepFactory(module.NewDockerPushStepFactory()),
		"step.docker_run":        wrapStepFactory(module.NewDockerRunStepFactory()),
		"step.scan_sast":         wrapStepFactory(module.NewScanSASTStepFactory()),
		"step.scan_container":    wrapStepFactory(module.NewScanContainerStepFactory()),
		"step.scan_deps":         wrapStepFactory(module.NewScanDepsStepFactory()),
		"step.deploy":            wrapStepFactory(module.NewDeployStepFactory()),
		"step.gate":              wrapStepFactory(module.NewGateStepFactory()),
		"step.build_ui":          wrapStepFactory(module.NewBuildUIStepFactory()),
		"step.build_from_config": wrapStepFactory(module.NewBuildFromConfigStepFactory()),
	}
}

func wrapStepFactory(f module.StepFactory) plugin.StepFactory {
	return func(name string, config map[string]any, app modular.Application) (any, error) {
		return f(name, config, app)
	}
}
