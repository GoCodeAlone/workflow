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
				ModuleTypes:   []string{"platform.provider", "platform.resource", "platform.context", "platform.kubernetes", "platform.ecs", "platform.dns", "platform.networking", "platform.apigateway", "platform.autoscaling", "platform.region", "platform.region_router", "platform.doks", "platform.do_networking", "platform.do_dns", "platform.do_app", "iac.state", "app.container", "argo.workflows"},
				StepTypes:     []string{"step.platform_template", "step.k8s_plan", "step.k8s_apply", "step.k8s_status", "step.k8s_destroy", "step.ecs_plan", "step.ecs_apply", "step.ecs_status", "step.ecs_destroy", "step.iac_plan", "step.iac_apply", "step.iac_status", "step.iac_destroy", "step.iac_drift_detect", "step.dns_plan", "step.dns_apply", "step.dns_status", "step.network_plan", "step.network_apply", "step.network_status", "step.apigw_plan", "step.apigw_apply", "step.apigw_status", "step.apigw_destroy", "step.scaling_plan", "step.scaling_apply", "step.scaling_status", "step.scaling_destroy", "step.app_deploy", "step.app_status", "step.app_rollback", "step.region_deploy", "step.region_promote", "step.region_failover", "step.region_status", "step.region_weight", "step.region_sync", "step.argo_submit", "step.argo_status", "step.argo_logs", "step.argo_delete", "step.argo_list", "step.do_deploy", "step.do_status", "step.do_logs", "step.do_scale", "step.do_destroy"},
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
		"app.container": func(name string, cfg map[string]any) modular.Module {
			return module.NewAppContainerModule(name, cfg)
		},
		"platform.region": func(name string, cfg map[string]any) modular.Module {
			return module.NewMultiRegionModule(name, cfg)
		},
		"argo.workflows": func(name string, cfg map[string]any) modular.Module {
			return module.NewArgoWorkflowsModule(name, cfg)
		},
		"platform.doks": func(name string, cfg map[string]any) modular.Module {
			return module.NewPlatformDOKS(name, cfg)
		},
		"platform.do_networking": func(name string, cfg map[string]any) modular.Module {
			return module.NewPlatformDONetworking(name, cfg)
		},
		"platform.do_dns": func(name string, cfg map[string]any) modular.Module {
			return module.NewPlatformDODNS(name, cfg)
		},
		"platform.do_app": func(name string, cfg map[string]any) modular.Module {
			return module.NewPlatformDOApp(name, cfg)
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
		"step.do_deploy": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewDODeployStepFactory()(name, cfg, app)
		},
		"step.do_status": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewDOStatusStepFactory()(name, cfg, app)
		},
		"step.do_logs": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewDOLogsStepFactory()(name, cfg, app)
		},
		"step.do_scale": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewDOScaleStepFactory()(name, cfg, app)
		},
		"step.do_destroy": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewDODestroyStepFactory()(name, cfg, app)
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
		{
			Type:        "app.container",
			Label:       "App Container",
			Category:    "application",
			Description: "Application deployment abstraction that translates high-level config into platform-specific resources (Kubernetes Deployment+Service or ECS task definition)",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "environment", Label: "Environment", Type: schema.FieldTypeString, Required: true, Description: "Name of the platform.kubernetes or platform.ecs module to deploy to"},
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
				{Key: "regions", Label: "Regions", Type: schema.FieldTypeJSON, Required: true, Description: "List of region definitions (name, provider, endpoint, priority, health_check)"},
			},
		},
		{
			Type:        "platform.doks",
			Label:       "DigitalOcean Kubernetes (DOKS)",
			Category:    "infrastructure",
			Description: "Manages DigitalOcean Kubernetes Service clusters (mock or real DO backend)",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "account", Label: "Cloud Account", Type: schema.FieldTypeString, Description: "Name of the cloud.account module (provider=digitalocean)"},
				{Key: "cluster_name", Label: "Cluster Name", Type: schema.FieldTypeString, Description: "DOKS cluster name"},
				{Key: "region", Label: "Region", Type: schema.FieldTypeString, Description: "DO region slug (e.g. nyc3)"},
				{Key: "version", Label: "Kubernetes Version", Type: schema.FieldTypeString, Description: "Kubernetes version slug (e.g. 1.29.1-do.0)"},
				{Key: "node_pool", Label: "Node Pool", Type: schema.FieldTypeJSON, Description: "Node pool config (size, count, auto_scale, min_nodes, max_nodes)"},
			},
		},
		{
			Type:        "platform.do_networking",
			Label:       "DigitalOcean VPC & Firewalls",
			Category:    "infrastructure",
			Description: "Manages DigitalOcean VPCs, firewalls, and load balancers (mock or real DO backend)",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "account", Label: "Cloud Account", Type: schema.FieldTypeString, Description: "Name of the cloud.account module (provider=digitalocean)"},
				{Key: "provider", Label: "Provider", Type: schema.FieldTypeString, Description: "mock | digitalocean"},
				{Key: "vpc", Label: "VPC Config", Type: schema.FieldTypeJSON, Required: true, Description: "VPC configuration (name, region, ip_range)"},
				{Key: "firewalls", Label: "Firewalls", Type: schema.FieldTypeJSON, Description: "List of firewall definitions"},
			},
		},
		{
			Type:        "platform.do_dns",
			Label:       "DigitalOcean DNS",
			Category:    "infrastructure",
			Description: "Manages DigitalOcean domains and DNS records (mock or real DO backend)",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "account", Label: "Cloud Account", Type: schema.FieldTypeString, Description: "Name of the cloud.account module (provider=digitalocean)"},
				{Key: "provider", Label: "Provider", Type: schema.FieldTypeString, Description: "mock | digitalocean"},
				{Key: "domain", Label: "Domain", Type: schema.FieldTypeString, Required: true, Description: "Domain name (e.g. example.com)"},
				{Key: "records", Label: "Records", Type: schema.FieldTypeJSON, Description: "List of DNS record definitions (name, type, data, ttl)"},
			},
		},
		{
			Type:        "platform.do_app",
			Label:       "DigitalOcean App Platform",
			Category:    "application",
			Description: "Deploys containerized apps to DigitalOcean App Platform (mock or real DO backend)",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "account", Label: "Cloud Account", Type: schema.FieldTypeString, Description: "Name of the cloud.account module (provider=digitalocean)"},
				{Key: "provider", Label: "Provider", Type: schema.FieldTypeString, Description: "mock | digitalocean"},
				{Key: "name", Label: "App Name", Type: schema.FieldTypeString, Description: "App Platform application name"},
				{Key: "region", Label: "Region", Type: schema.FieldTypeString, Description: "DO region slug (e.g. nyc)"},
				{Key: "image", Label: "Container Image", Type: schema.FieldTypeString, Description: "Container image reference"},
				{Key: "instances", Label: "Instances", Type: schema.FieldTypeNumber, Description: "Number of instances (default: 1)"},
				{Key: "http_port", Label: "HTTP Port", Type: schema.FieldTypeNumber, Description: "Container HTTP port (default: 8080)"},
				{Key: "envs", Label: "Environment Variables", Type: schema.FieldTypeMap, Description: "Environment variables for the app"},
			},
		},
	}
}
