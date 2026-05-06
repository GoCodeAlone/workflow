package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

// DryRunDeployPlan is the structured dry-run output for ci run --phase deploy.
type DryRunDeployPlan struct {
	Command        string               `json:"command"`
	Environment    string               `json:"environment"`
	Provider       string               `json:"provider"`
	Strategy       string               `json:"strategy"`
	PreDeploy      []string             `json:"pre_deploy,omitempty"`
	DeployTarget   string               `json:"deploy_target"`
	ImageRef       string               `json:"image_ref"`
	ImageTagSource string               `json:"image_tag_source"`
	HealthCheck    *DryRunHealthCheck   `json:"health_check,omitempty"`
	Secrets        []DryRunSecretRef    `json:"secrets,omitempty"`
	Services       []DryRunServiceEntry `json:"services,omitempty"`
}

// DryRunHealthCheck summarizes health check configuration.
type DryRunHealthCheck struct {
	Path    string `json:"path"`
	Timeout string `json:"timeout,omitempty"`
}

// DryRunServiceEntry describes a service that would be deployed.
type DryRunServiceEntry struct {
	Name     string `json:"name"`
	ImageTag string `json:"image_tag,omitempty"`
}

// runDeployPhaseDryRun prints what the deploy phase would execute without
// performing any provider mutations, secret injection, or health polling.
// configFile is the path to the workflow config; it is used for the
// follow-up command hint and to honor top-level imports: when non-empty.
func runDeployPhaseDryRun(
	deploy *config.CIDeployConfig,
	envName string,
	wfCfg *config.WorkflowConfig,
	services map[string]*config.ServiceConfig,
	format string,
	configFile string,
) error {
	if format != "table" && format != "json" {
		return fmt.Errorf("unknown --format %q: supported values are table, json", format)
	}
	if deploy == nil {
		return fmt.Errorf("no deploy configuration")
	}
	env, ok := deploy.Environments[envName]
	if !ok {
		available := make([]string, 0, len(deploy.Environments))
		for k := range deploy.Environments {
			available = append(available, k)
		}
		sort.Strings(available)
		return fmt.Errorf("environment %q not found (available: %s)", envName, strings.Join(available, ", "))
	}

	strategy := cmp(env.Strategy, "rolling")

	// Resolve deploy target and image source.
	// Priority for deploy target: env-resolved infra module name (same as
	// newPluginDeployProvider) → env.Cluster → envName.
	// Priority for image: IMAGE_TAG env var → module config image → "(not set)".
	info := resolveDeployInfoFromConfig(wfCfg, envName, env.Provider)

	imageTag := os.Getenv("IMAGE_TAG")
	imageTagSource := "IMAGE_TAG env var"
	if imageTag == "" && info.Image != "" {
		imageTag = info.Image
		imageTagSource = "module config image field"
	} else if imageTag == "" {
		imageTag = "(not set)"
		imageTagSource = "IMAGE_TAG env var (not set; no module config image found)"
	}

	deployTarget := info.Target
	if deployTarget == "" {
		// Fall back to cluster name or env name when no matching infra module found.
		deployTarget = env.Cluster
		if deployTarget == "" {
			deployTarget = envName
		}
	}

	// Collect secret keys that would be required, resolving stores via the
	// same priority order (per-secret → env override → defaultStore → "env")
	// that the real deploy path uses.
	secretRefs := collectDeploySecretRefs(wfCfg, envName)

	// Collect services.
	var serviceEntries []DryRunServiceEntry
	if len(services) > 0 {
		for name := range services {
			serviceEntries = append(serviceEntries, DryRunServiceEntry{
				Name:     name,
				ImageTag: imageTag,
			})
		}
		sort.Slice(serviceEntries, func(i, j int) bool {
			return serviceEntries[i].Name < serviceEntries[j].Name
		})
	}

	switch format {
	case "json":
		return printDeployDryRunJSON(envName, env, strategy, imageTag, imageTagSource, deployTarget, secretRefs, serviceEntries)
	default:
		return printDeployDryRunTable(envName, env, strategy, imageTag, imageTagSource, deployTarget, secretRefs, serviceEntries, configFile)
	}
}

// resolvedDeployInfo holds the env-resolved deploy target and image values
// extracted from the workflow config modules.
type resolvedDeployInfo struct {
	// Target is the env-resolved name of the infra module used for deployment
	// (e.g. "bmw-staging" when the base module name is "bmw-app" and the
	// staging environment overrides the name).
	Target string
	// Image is the image field from the resolved module config, used as a
	// fallback when IMAGE_TAG is not set in the environment.
	Image string
}

// resolveDeployInfoFromConfig replicates the provider and target-module
// resolution that newPluginDeployProvider performs so the dry-run output
// reflects the same resource identity as a real deploy.
//
// If wfCfg is nil or contains no matching modules, an empty result is
// returned (callers fall back to cluster name or env name).
func resolveDeployInfoFromConfig(wfCfg *config.WorkflowConfig, envName, providerName string) resolvedDeployInfo {
	if wfCfg == nil || len(wfCfg.Modules) == 0 || providerName == "" {
		return resolvedDeployInfo{}
	}

	// resolveModule mirrors the inner helper in newPluginDeployProvider.
	resolveModule := func(m *config.ModuleConfig) (*config.ResolvedModule, bool) {
		if envName == "" {
			return &config.ResolvedModule{Name: m.Name, Type: m.Type, Config: m.Config}, true
		}
		return m.ResolveForEnv(envName)
	}

	// Step 1: find the iac.provider module whose config.provider or module
	// name matches the requested provider name.
	var providerModName string
	for i := range wfCfg.Modules {
		m := &wfCfg.Modules[i]
		if m.Type != "iac.provider" {
			continue
		}
		resolved, ok := resolveModule(m)
		if !ok {
			continue
		}
		cfgProvider, _ := resolved.Config["provider"].(string)
		if cfgProvider == providerName || resolved.Name == providerName {
			providerModName = resolved.Name
			break
		}
	}
	if providerModName == "" {
		return resolvedDeployInfo{}
	}

	// Step 2: find the deploy-target module — same priority list as
	// newPluginDeployProvider — then fall back to any infra.* module that
	// references the provider.
	deployTargetTypes := []string{
		"infra.container_service",
		"platform.do_app",
		"platform.app_platform",
		"infra.k8s_cluster",
	}

	findByType := func(targetType string) (resolvedDeployInfo, bool) {
		for i := range wfCfg.Modules {
			m := &wfCfg.Modules[i]
			if m.Type != targetType {
				continue
			}
			resolved, ok := resolveModule(m)
			if !ok {
				continue
			}
			if p, _ := resolved.Config["provider"].(string); p == providerModName {
				img, _ := resolved.Config["image"].(string)
				return resolvedDeployInfo{Target: resolved.Name, Image: img}, true
			}
		}
		return resolvedDeployInfo{}, false
	}

	for _, t := range deployTargetTypes {
		if info, ok := findByType(t); ok {
			return info
		}
	}

	// Fallback: first infra.* module with a matching provider reference.
	for i := range wfCfg.Modules {
		m := &wfCfg.Modules[i]
		if m.Type == "iac.provider" || !strings.HasPrefix(m.Type, "infra.") {
			continue
		}
		resolved, ok := resolveModule(m)
		if !ok {
			continue
		}
		if p, _ := resolved.Config["provider"].(string); p == providerModName {
			img, _ := resolved.Config["image"].(string)
			return resolvedDeployInfo{Target: resolved.Name, Image: img}
		}
	}

	return resolvedDeployInfo{}
}

func printDeployDryRunTable(
	envName string,
	env *config.CIDeployEnvironment,
	strategy, imageTag, imageTagSource, deployTarget string,
	secretRefs []DryRunSecretRef,
	services []DryRunServiceEntry,
	configFile string,
) error {
	fmt.Printf("Dry Run — ci deploy\n")
	fmt.Printf("====================\n")
	fmt.Printf("Environment:    %s\n", envName)
	fmt.Printf("Provider:       %s\n", env.Provider)
	fmt.Printf("Strategy:       %s\n", strategy)
	fmt.Printf("Deploy Target:  %s\n", deployTarget)
	fmt.Printf("Image Ref:      %s\n", imageTag)
	fmt.Printf("Image Source:   %s\n", imageTagSource)
	if env.Region != "" {
		fmt.Printf("Region:         %s\n", env.Region)
	}
	if env.Namespace != "" {
		fmt.Printf("Namespace:      %s\n", env.Namespace)
	}
	fmt.Println()

	if len(env.PreDeploy) > 0 {
		fmt.Printf("Pre-Deploy Steps:\n")
		for _, step := range env.PreDeploy {
			fmt.Printf("  - %s\n", step)
		}
		fmt.Println()
	}

	if len(services) > 0 {
		fmt.Printf("Services:\n")
		for _, s := range services {
			fmt.Printf("  - %s (image: %s)\n", s.Name, s.ImageTag)
		}
		fmt.Println()
	}

	if env.HealthCheck != nil {
		fmt.Printf("Health Check:\n")
		fmt.Printf("  Path:    %s\n", env.HealthCheck.Path)
		if env.HealthCheck.Timeout != "" {
			fmt.Printf("  Timeout: %s\n", env.HealthCheck.Timeout)
		}
		fmt.Println()
	}

	if len(secretRefs) > 0 {
		fmt.Printf("Required Secrets:\n")
		for _, s := range secretRefs {
			store := ""
			if s.Store != "" {
				store = fmt.Sprintf(" (store: %s)", s.Store)
			}
			fmt.Printf("  - %s%s\n", s.Key, store)
		}
		fmt.Println()
	}

	if env.RequireApproval {
		fmt.Printf("NOTE: This environment requires approval before deployment.\n\n")
	}

	fmt.Printf("Dry run complete. No deployment was executed.\n")
	deployCmd := fmt.Sprintf("wfctl ci run --phase deploy --env %s", envName)
	if configFile != "" {
		deployCmd += fmt.Sprintf(" --config %s", configFile)
	}
	fmt.Printf("To deploy, run: %s\n", deployCmd)
	return nil
}

func printDeployDryRunJSON(
	envName string,
	env *config.CIDeployEnvironment,
	strategy, imageTag, imageTagSource, deployTarget string,
	secretRefs []DryRunSecretRef,
	services []DryRunServiceEntry,
) error {
	var hc *DryRunHealthCheck
	if env.HealthCheck != nil {
		hc = &DryRunHealthCheck{
			Path:    env.HealthCheck.Path,
			Timeout: env.HealthCheck.Timeout,
		}
	}

	output := DryRunDeployPlan{
		Command:        "ci run --phase deploy",
		Environment:    envName,
		Provider:       env.Provider,
		Strategy:       strategy,
		PreDeploy:      env.PreDeploy,
		DeployTarget:   deployTarget,
		ImageRef:       imageTag,
		ImageTagSource: imageTagSource,
		HealthCheck:    hc,
		Secrets:        secretRefs,
		Services:       services,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

func collectDeploySecretRefs(cfg *config.WorkflowConfig, envName string) []DryRunSecretRef {
	if cfg == nil || cfg.Secrets == nil {
		return nil
	}

	var refs []DryRunSecretRef
	for _, entry := range cfg.Secrets.Entries {
		// Use ResolveSecretStore so env-level overrides (secretsStoreOverride),
		// defaultStore, and legacy provider fields are applied — matching the
		// priority order the real deploy path uses when it calls injectSecrets.
		store := ResolveSecretStore(entry.Name, envName, cfg)
		refs = append(refs, DryRunSecretRef{
			Key:      entry.Name,
			Store:    store,
			Required: true,
		})
	}
	return refs
}
