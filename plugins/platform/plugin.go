// Package platform provides an EnginePlugin that registers all platform-related
// module types, the platform workflow handler, the reconciliation trigger,
// and the platform template step.
package platform

import (
	"context"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// iacProviderApplyFn is the apply dispatch function passed to
// NewIaCProviderApplyStepFactory. It wraps wfctlhelpers.ApplyPlanWithHooks
// with an empty hooks struct so the step's function signature
// (ctx, provider, plan) is satisfied without the step importing wfctlhelpers.
func iacProviderApplyFn(ctx context.Context, p interfaces.IaCProvider, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return wfctlhelpers.ApplyPlanWithHooks(ctx, p, plan, wfctlhelpers.ApplyPlanHooks{})
}

// Plugin is the platform EnginePlugin.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new platform plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "platform",
				PluginVersion:     "1.0.0",
				PluginDescription: "Platform infrastructure modules, workflow handler, reconciliation trigger, and template step",
			},
			Manifest: plugin.PluginManifest{
				Name:          "platform",
				Version:       "1.0.0",
				Author:        "GoCodeAlone",
				Description:   "Platform infrastructure modules, workflow handler, reconciliation trigger, and template step",
				Tier:          plugin.TierCore,
				ModuleTypes:   []string{"platform.provider", "platform.resource", "platform.context", "platform.kubernetes", "platform.dns", "platform.region", "platform.region_router", "iac.state", "app.container", "argo.workflows"},
				StepTypes:     []string{"step.platform_template", "step.k8s_plan", "step.k8s_apply", "step.k8s_status", "step.k8s_destroy", "step.iac_plan", "step.iac_apply", "step.iac_status", "step.iac_destroy", "step.iac_drift_detect", "step.iac_provider_list", "step.iac_provider_catalog", "step.iac_provider_plan", "step.iac_provider_apply", "step.iac_provider_destroy", "step.iac_provider_drift", "step.iac_secret_reachability", "step.dns_plan", "step.dns_apply", "step.dns_status", "step.app_deploy", "step.app_status", "step.app_rollback", "step.region_deploy", "step.region_promote", "step.region_failover", "step.region_status", "step.region_weight", "step.region_sync", "step.argo_submit", "step.argo_status", "step.argo_logs", "step.argo_delete", "step.argo_list"},
				TriggerTypes:  []string{"reconciliation"},
				WorkflowTypes: []string{"platform"},
			},
		},
	}
}

// ModuleFactories returns factory functions for platform module types.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"platform.kubernetes": func(name string, cfg map[string]any) modular.Module {
			return module.NewPlatformKubernetes(name, cfg)
		},
		"platform.dns": func(name string, cfg map[string]any) modular.Module {
			return module.NewPlatformDNS(name, cfg)
		},
		"iac.state": func(name string, cfg map[string]any) modular.Module {
			return module.NewIaCModule(name, cfg)
		},
		"platform.provider": func(name string, cfg map[string]any) modular.Module {
			providerName := ""
			if pn, ok := cfg["name"].(string); ok {
				providerName = pn
			}
			svcName := "platform.provider"
			if providerName != "" {
				svcName = "platform.provider." + providerName
			}
			return module.NewServiceModule(name, map[string]any{
				"provider_name": providerName,
				"service_name":  svcName,
				"config":        cfg,
			})
		},
		"platform.resource": func(name string, cfg map[string]any) modular.Module {
			return module.NewServiceModule(name, cfg)
		},
		"platform.context": func(name string, cfg map[string]any) modular.Module {
			return module.NewServiceModule(name, cfg)
		},
		"app.container": func(name string, cfg map[string]any) modular.Module {
			return module.NewAppContainerModule(name, cfg)
		},
		"platform.region": func(name string, cfg map[string]any) modular.Module {
			return module.NewMultiRegionModule(name, cfg)
		},
		"argo.workflows": func(name string, cfg map[string]any) modular.Module {
			return module.NewArgoWorkflowsModule(name, cfg)
		},
		"platform.region_router": func(name string, cfg map[string]any) modular.Module {
			return module.NewMultiRegionRoutingModule(name, cfg)
		},
	}
}

// StepFactories returns the platform template step factory.
func (p *Plugin) StepFactories() map[string]plugin.StepFactory {
	return map[string]plugin.StepFactory{
		"step.platform_template": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewPlatformTemplateStepFactory()(name, cfg, app)
		},
		"step.k8s_plan": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewK8sPlanStepFactory()(name, cfg, app)
		},
		"step.k8s_apply": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewK8sApplyStepFactory()(name, cfg, app)
		},
		"step.k8s_status": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewK8sStatusStepFactory()(name, cfg, app)
		},
		"step.k8s_destroy": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewK8sDestroyStepFactory()(name, cfg, app)
		},
		"step.iac_plan": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewIaCPlanStepFactory()(name, cfg, app)
		},
		"step.iac_apply": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewIaCApplyStepFactory()(name, cfg, app)
		},
		"step.iac_status": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewIaCStatusStepFactory()(name, cfg, app)
		},
		"step.iac_destroy": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewIaCDestroyStepFactory()(name, cfg, app)
		},
		"step.iac_drift_detect": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewIaCDriftDetectStepFactory()(name, cfg, app)
		},
		// IaCProvider steps (general, provider-agnostic).
		"step.iac_provider_list": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewIaCProviderListStepFactory()(name, cfg, app)
		},
		"step.iac_provider_catalog": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewIaCProviderCatalogStepFactory()(name, cfg, app)
		},
		"step.iac_provider_plan": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewIaCProviderPlanStepFactory()(name, cfg, app)
		},
		"step.iac_provider_apply": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewIaCProviderApplyStepFactory(iacProviderApplyFn)(name, cfg, app)
		},
		"step.iac_provider_destroy": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewIaCProviderDestroyStepFactory()(name, cfg, app)
		},
		"step.iac_provider_drift": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewIaCProviderDriftStepFactory()(name, cfg, app)
		},
		"step.iac_secret_reachability": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewIaCSecretReachabilityStepFactory()(name, cfg, app)
		},
		"step.dns_plan": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewDNSPlanStepFactory()(name, cfg, app)
		},
		"step.dns_apply": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewDNSApplyStepFactory()(name, cfg, app)
		},
		"step.dns_status": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewDNSStatusStepFactory()(name, cfg, app)
		},
		"step.app_deploy": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewAppDeployStepFactory()(name, cfg, app)
		},
		"step.app_status": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewAppStatusStepFactory()(name, cfg, app)
		},
		"step.app_rollback": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewAppRollbackStepFactory()(name, cfg, app)
		},
		"step.region_deploy": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewRegionDeployStepFactory()(name, cfg, app)
		},
		"step.region_promote": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewRegionPromoteStepFactory()(name, cfg, app)
		},
		"step.region_failover": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewRegionFailoverStepFactory()(name, cfg, app)
		},
		"step.region_status": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewRegionStatusStepFactory()(name, cfg, app)
		},
		"step.region_weight": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewRegionWeightStepFactory()(name, cfg, app)
		},
		"step.region_sync": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewRegionSyncStepFactory()(name, cfg, app)
		},
		"step.argo_submit": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewArgoSubmitStepFactory()(name, cfg, app)
		},
		"step.argo_status": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewArgoStatusStepFactory()(name, cfg, app)
		},
		"step.argo_logs": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewArgoLogsStepFactory()(name, cfg, app)
		},
		"step.argo_delete": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewArgoDeleteStepFactory()(name, cfg, app)
		},
		"step.argo_list": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewArgoListStepFactory()(name, cfg, app)
		},
	}
}

// TriggerFactories returns the reconciliation trigger factory.
func (p *Plugin) TriggerFactories() map[string]plugin.TriggerFactory {
	return map[string]plugin.TriggerFactory{
		"reconciliation": func() any {
			return module.NewReconciliationTrigger()
		},
	}
}

// WorkflowHandlers returns the platform workflow handler factory.
func (p *Plugin) WorkflowHandlers() map[string]plugin.WorkflowHandlerFactory {
	return map[string]plugin.WorkflowHandlerFactory{
		"platform": func() any {
			return handlers.NewPlatformWorkflowHandler()
		},
	}
}

// ModuleSchemas returns UI schema definitions for platform module types.
func (p *Plugin) ModuleSchemas() []*schema.ModuleSchema {
	return []*schema.ModuleSchema{
		{
			Type:        "iac.state",
			Label:       "IaC State Store",
			Category:    "infrastructure",
			Description: "Tracks infrastructure provisioning state (memory or filesystem backend)",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "backend", Label: "Backend", Type: schema.FieldTypeString, Description: "memory or filesystem"},
				{Key: "directory", Label: "Directory", Type: schema.FieldTypeString, Description: "State directory (filesystem backend only)"},
			},
		},
		{
			Type:        "platform.kubernetes",
			Label:       "Kubernetes Cluster",
			Category:    "infrastructure",
			Description: "Managed Kubernetes cluster (kind/k3s for local, EKS/GKE/AKS stubs for cloud)",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "account", Label: "Cloud Account", Type: schema.FieldTypeString, Description: "Name of the cloud.account module (optional for kind)"},
				{Key: "type", Label: "Cluster Type", Type: schema.FieldTypeString, Required: true, Description: "eks | gke | aks | kind | k3s"},
				{Key: "version", Label: "Kubernetes Version", Type: schema.FieldTypeString, Description: "e.g. 1.29"},
				{Key: "nodeGroups", Label: "Node Groups", Type: schema.FieldTypeArray, Description: "Node group definitions"},
			},
		},
		{
			Type:        "platform.dns",
			Label:       "DNS Zone Manager",
			Category:    "infrastructure",
			Description: "Manages DNS zones and records (mock or Route53/aws backend)",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "account", Label: "Cloud Account", Type: schema.FieldTypeString, Description: "Name of the cloud.account module (optional for mock)"},
				{Key: "provider", Label: "Provider", Type: schema.FieldTypeString, Description: "mock | aws (aws Route53 backend removed in v0.53.0; use infra.dns + workflow-plugin-aws)"},
				{Key: "zone", Label: "Zone Config", Type: schema.FieldTypeMap, Required: true, Description: "Zone configuration (name, comment, private, vpcId)"},
				{Key: "records", Label: "DNS Records", Type: schema.FieldTypeArray, Description: "List of DNS record definitions"},
			},
		},
		{
			Type:        "platform.provider",
			Label:       "Platform Provider",
			Category:    "infrastructure",
			Description: "Cloud infrastructure provider (e.g., Terraform, Pulumi)",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "name", Label: "Provider Name", Type: schema.FieldTypeString, Required: true, Description: "Name of the platform provider"},
			},
		},
		{
			Type:        "platform.resource",
			Label:       "Platform Resource",
			Category:    "infrastructure",
			Description: "Infrastructure resource managed by a platform provider",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "type", Label: "Resource Type", Type: schema.FieldTypeString, Required: true, Description: "Type of infrastructure resource"},
			},
		},
		{
			Type:        "platform.context",
			Label:       "Platform Context",
			Category:    "infrastructure",
			Description: "Execution context for platform operations",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "path", Label: "Context Path", Type: schema.FieldTypeString, Required: true, Description: "Path identifying this context"},
			},
		},
		{
			Type:        "app.container",
			Label:       "App Container",
			Category:    "application",
			Description: "Application deployment abstraction that translates high-level config into platform-specific resources (Kubernetes Deployment+Service)",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "environment", Label: "Environment", Type: schema.FieldTypeString, Required: true, Description: "Name of the platform.kubernetes module to deploy to"},
				{Key: "image", Label: "Container Image", Type: schema.FieldTypeString, Required: true, Description: "Container image reference (e.g. registry.example.com/my-api:v1.0.0)"},
				{Key: "replicas", Label: "Replicas", Type: schema.FieldTypeNumber, Description: "Desired replica count (default: 1)"},
				{Key: "ports", Label: "Ports", Type: schema.FieldTypeArray, Description: "List of container port numbers"},
				{Key: "cpu", Label: "CPU", Type: schema.FieldTypeString, Description: "CPU request/limit (e.g. 500m; default: 256m)"},
				{Key: "memory", Label: "Memory", Type: schema.FieldTypeString, Description: "Memory request/limit (e.g. 512Mi; default: 512Mi)"},
				{Key: "env", Label: "Environment Variables", Type: schema.FieldTypeMap, Description: "Environment variables injected into the container"},
				{Key: "health_path", Label: "Health Path", Type: schema.FieldTypeString, Description: "HTTP health check path (default: /healthz)"},
				{Key: "health_port", Label: "Health Port", Type: schema.FieldTypeNumber, Description: "HTTP health check port (default: first port or 8080)"},
			},
		},
		{
			Type:        "platform.region",
			Label:       "Multi-Region Deployment",
			Category:    "infrastructure",
			Description: "Manages multi-region tenant deployments with failover, health checking, and traffic weight routing (mock or cloud provider backend)",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "provider", Label: "Provider", Type: schema.FieldTypeString, Description: "mock (default)"},
				{Key: "regions", Label: "Regions", Type: schema.FieldTypeArray, Required: true, Description: "List of region definitions (name, provider, endpoint, priority, health_check)"},
			},
		},
	}
}
