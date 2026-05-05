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
	HealthCheck    *DryRunHealthCheck    `json:"health_check,omitempty"`
	Secrets        []DryRunSecretRef     `json:"secrets,omitempty"`
	Services       []DryRunServiceEntry  `json:"services,omitempty"`
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
func runDeployPhaseDryRun(
	deploy *config.CIDeployConfig,
	envName string,
	wfCfg *config.WorkflowConfig,
	services map[string]*config.ServiceConfig,
	format string,
) error {
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
	imageTag := os.Getenv("IMAGE_TAG")
	imageTagSource := "IMAGE_TAG env var"
	if imageTag == "" {
		imageTag = "(not set)"
		imageTagSource = "IMAGE_TAG env var (not set)"
	}

	deployTarget := envName
	if env.Cluster != "" {
		deployTarget = env.Cluster
	}

	// Collect secret keys that would be required.
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
		return printDeployDryRunTable(envName, env, strategy, imageTag, imageTagSource, deployTarget, secretRefs, serviceEntries)
	}
}

func printDeployDryRunTable(
	envName string,
	env *config.CIDeployEnvironment,
	strategy, imageTag, imageTagSource, deployTarget string,
	secretRefs []DryRunSecretRef,
	services []DryRunServiceEntry,
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
	fmt.Printf("To deploy, run: wfctl ci run --phase deploy --env %s\n", envName)
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
		store := ""
		if entry.Store != "" {
			store = entry.Store
		}
		refs = append(refs, DryRunSecretRef{
			Key:      entry.Name,
			Store:    store,
			Required: true,
		})
	}
	return refs
}
