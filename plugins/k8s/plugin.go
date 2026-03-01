package k8s

import (
	"github.com/GoCodeAlone/workflow/deploy"
	"github.com/GoCodeAlone/workflow/deploy/sidecars"
	k8starget "github.com/GoCodeAlone/workflow/pkg/k8s"
	"github.com/GoCodeAlone/workflow/plugin"
)

// Plugin registers the Kubernetes deploy target and sidecar providers.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new Kubernetes deploy plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "kubernetes-deploy",
				PluginVersion:     "1.0.0",
				PluginDescription: "Kubernetes deployment via client-go with server-side apply",
			},
			Manifest: plugin.PluginManifest{
				Name:        "kubernetes-deploy",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "Native Kubernetes deployment support using client-go. Provides generate, apply, destroy, status, diff, and logs operations without requiring kubectl or Helm.",
				Tier:        plugin.TierCore,
			},
		},
	}
}

// DeployTargets returns the kubernetes deploy target.
func (p *Plugin) DeployTargets() map[string]deploy.DeployTarget {
	target := k8starget.NewDeployTarget()
	return map[string]deploy.DeployTarget{
		"kubernetes": target,
		"k8s":        target,
	}
}

// SidecarProviders returns the built-in sidecar providers.
func (p *Plugin) SidecarProviders() map[string]deploy.SidecarProvider {
	return map[string]deploy.SidecarProvider{
		"sidecar.tailscale": sidecars.NewTailscale(),
		"sidecar.generic":   sidecars.NewGeneric(),
	}
}
