// Package platform provides an EnginePlugin that registers all platform-related
// module types, the platform workflow handler, the reconciliation trigger,
// and the platform template step.
package platform

import (
	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

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
				ModuleTypes:   []string{"platform.provider", "platform.resource", "platform.context", "platform.kubernetes", "platform.ecs", "platform.dns", "platform.networking", "platform.apigateway", "platform.autoscaling", "iac.state"},
				StepTypes:     []string{"step.platform_template", "step.k8s_plan", "step.k8s_apply", "step.k8s_status", "step.k8s_destroy", "step.ecs_plan", "step.ecs_apply", "step.ecs_status", "step.ecs_destroy", "step.iac_plan", "step.iac_apply", "step.iac_status", "step.iac_destroy", "step.iac_drift_detect", "step.dns_plan", "step.dns_apply", "step.dns_status", "step.network_plan", "step.network_apply", "step.network_status", "step.apigw_plan", "step.apigw_apply", "step.apigw_status", "step.apigw_destroy", "step.scaling_plan", "step.scaling_apply", "step.scaling_status", "step.scaling_destroy"},
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
		"platform.ecs": func(name string, cfg map[string]any) modular.Module {
			return module.NewPlatformECS(name, cfg)
		},
		"platform.dns": func(name string, cfg map[string]any) modular.Module {
			return module.NewPlatformDNS(name, cfg)
		},
		"platform.networking": func(name string, cfg map[string]any) modular.Module {
			return module.NewPlatformNetworking(name, cfg)
		},
		"platform.apigateway": func(name string, cfg map[string]any) modular.Module {
			return module.NewPlatformAPIGateway(name, cfg)
		},
		"platform.autoscaling": func(name string, cfg map[string]any) modular.Module {
			return module.NewPlatformAutoscaling(name, cfg)
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
		"step.ecs_plan": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewECSPlanStepFactory()(name, cfg, app)
		},
		"step.ecs_apply": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewECSApplyStepFactory()(name, cfg, app)
		},
		"step.ecs_status": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewECSStatusStepFactory()(name, cfg, app)
		},
		"step.ecs_destroy": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewECSDestroyStepFactory()(name, cfg, app)
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
		"step.dns_plan": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewDNSPlanStepFactory()(name, cfg, app)
		},
		"step.dns_apply": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewDNSApplyStepFactory()(name, cfg, app)
		},
		"step.dns_status": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewDNSStatusStepFactory()(name, cfg, app)
		},
		"step.network_plan": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewNetworkPlanStepFactory()(name, cfg, app)
		},
		"step.network_apply": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewNetworkApplyStepFactory()(name, cfg, app)
		},
		"step.network_status": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewNetworkStatusStepFactory()(name, cfg, app)
		},
		"step.apigw_plan": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewApigwPlanStepFactory()(name, cfg, app)
		},
		"step.apigw_apply": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewApigwApplyStepFactory()(name, cfg, app)
		},
		"step.apigw_status": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewApigwStatusStepFactory()(name, cfg, app)
		},
		"step.apigw_destroy": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewApigwDestroyStepFactory()(name, cfg, app)
		},
		"step.scaling_plan": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewScalingPlanStepFactory()(name, cfg, app)
		},
		"step.scaling_apply": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewScalingApplyStepFactory()(name, cfg, app)
		},
		"step.scaling_status": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewScalingStatusStepFactory()(name, cfg, app)
		},
		"step.scaling_destroy": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewScalingDestroyStepFactory()(name, cfg, app)
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
				{Key: "nodeGroups", Label: "Node Groups", Type: schema.FieldTypeJSON, Description: "Node group definitions"},
			},
		},
		{
			Type:        "platform.ecs",
			Label:       "ECS Fargate Service",
			Category:    "infrastructure",
			Description: "AWS ECS/Fargate service with task definitions and ALB target group config",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "account", Label: "Cloud Account", Type: schema.FieldTypeString, Description: "Name of the cloud.account module (optional for mock)"},
				{Key: "cluster", Label: "ECS Cluster", Type: schema.FieldTypeString, Required: true, Description: "ECS cluster name"},
				{Key: "region", Label: "AWS Region", Type: schema.FieldTypeString, Description: "AWS region (e.g. us-east-1)"},
				{Key: "launch_type", Label: "Launch Type", Type: schema.FieldTypeString, Description: "FARGATE or EC2 (default: FARGATE)"},
				{Key: "desired_count", Label: "Desired Count", Type: schema.FieldTypeString, Description: "Number of tasks to run (default: 1)"},
				{Key: "vpc_subnets", Label: "VPC Subnets", Type: schema.FieldTypeJSON, Description: "List of subnet IDs"},
				{Key: "security_groups", Label: "Security Groups", Type: schema.FieldTypeJSON, Description: "List of security group IDs"},
			},
		},
		{
			Type:        "platform.dns",
			Label:       "DNS Zone Manager",
			Category:    "infrastructure",
			Description: "Manages DNS zones and records (mock or Route53/aws backend)",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "account", Label: "Cloud Account", Type: schema.FieldTypeString, Description: "Name of the cloud.account module (optional for mock)"},
				{Key: "provider", Label: "Provider", Type: schema.FieldTypeString, Description: "mock | aws (Route53)"},
				{Key: "zone", Label: "Zone Config", Type: schema.FieldTypeJSON, Required: true, Description: "Zone configuration (name, comment, private, vpcId)"},
				{Key: "records", Label: "DNS Records", Type: schema.FieldTypeJSON, Description: "List of DNS record definitions"},
			},
		},
		{
			Type:        "platform.networking",
			Label:       "VPC Networking",
			Category:    "infrastructure",
			Description: "Manages VPC, subnets, NAT gateway, and security groups (mock or AWS backend)",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "account", Label: "Cloud Account", Type: schema.FieldTypeString, Description: "Name of the cloud.account module (optional for mock)"},
				{Key: "provider", Label: "Provider", Type: schema.FieldTypeString, Description: "mock | aws"},
				{Key: "vpc", Label: "VPC Config", Type: schema.FieldTypeJSON, Required: true, Description: "VPC configuration (cidr, name)"},
				{Key: "subnets", Label: "Subnets", Type: schema.FieldTypeJSON, Description: "List of subnet definitions"},
				{Key: "nat_gateway", Label: "NAT Gateway", Type: schema.FieldTypeBool, Description: "Provision a NAT gateway"},
				{Key: "security_groups", Label: "Security Groups", Type: schema.FieldTypeJSON, Description: "List of security group definitions"},
			},
		},
		{
			Type:        "platform.apigateway",
			Label:       "API Gateway",
			Category:    "infrastructure",
			Description: "Manages API gateway provisioning with routes, stages, and rate limiting (mock or AWS API Gateway v2)",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "account", Label: "Cloud Account", Type: schema.FieldTypeString, Description: "Name of the cloud.account module (optional for mock)"},
				{Key: "provider", Label: "Provider", Type: schema.FieldTypeString, Description: "mock | aws"},
				{Key: "name", Label: "Gateway Name", Type: schema.FieldTypeString, Required: true, Description: "API gateway name"},
				{Key: "stage", Label: "Stage", Type: schema.FieldTypeString, Description: "Deployment stage (dev, staging, prod)"},
				{Key: "cors", Label: "CORS Config", Type: schema.FieldTypeJSON, Description: "CORS configuration (allow_origins, allow_methods, allow_headers)"},
				{Key: "routes", Label: "Routes", Type: schema.FieldTypeJSON, Description: "Route definitions (path, method, target, rate_limit, auth_type)"},
			},
		},
		{
			Type:        "platform.autoscaling",
			Label:       "Autoscaling Policies",
			Category:    "infrastructure",
			Description: "Manages autoscaling policies (target tracking, step, scheduled) for AWS or mock resources",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "account", Label: "Cloud Account", Type: schema.FieldTypeString, Description: "Name of the cloud.account module (optional for mock)"},
				{Key: "provider", Label: "Provider", Type: schema.FieldTypeString, Description: "mock | aws"},
				{Key: "policies", Label: "Policies", Type: schema.FieldTypeJSON, Required: true, Description: "Scaling policy definitions (name, type, target_resource, min_capacity, max_capacity, ...)"},
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
	}
}
