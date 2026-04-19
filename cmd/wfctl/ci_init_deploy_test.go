package main

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func makeDeployFixture(envNames []string, requireApproval map[string]bool) *config.WorkflowConfig {
	envs := map[string]*config.CIDeployEnvironment{}
	for _, name := range envNames {
		envs[name] = &config.CIDeployEnvironment{
			Provider:        "do-app-platform",
			RequireApproval: requireApproval[name],
		}
	}
	return &config.WorkflowConfig{
		CI: &config.CIConfig{
			Build: &config.CIBuildConfig{
				Containers: []config.CIContainerTarget{
					{Name: "api", Method: "dockerfile", Dockerfile: "Dockerfile"},
				},
			},
			Registries: []config.CIRegistry{
				{
					Name: "docr",
					Type: "do",
					Path: "registry.digitalocean.com/myorg",
					Auth: &config.CIRegistryAuth{Env: "DIGITALOCEAN_TOKEN"},
				},
			},
			Deploy: &config.CIDeployConfig{
				Environments: envs,
			},
		},
	}
}

// T42: new minimal deploy.yml shape.
func TestGenerateGHADeploy_WorkflowRunTrigger(t *testing.T) {
	cfg := makeDeployFixture([]string{"staging", "prod"}, map[string]bool{"prod": true})
	content := generateGHADeploy(cfg)

	if !strings.Contains(content, "workflow_run") {
		t.Error("want workflow_run trigger, not push")
	}
	if strings.Contains(content, "on:\n  push:") {
		t.Error("should not have push trigger in deploy.yml")
	}
}

func TestGenerateGHADeploy_ConcurrencyBlock(t *testing.T) {
	cfg := makeDeployFixture([]string{"staging"}, nil)
	content := generateGHADeploy(cfg)

	if !strings.Contains(content, "concurrency") {
		t.Error("want concurrency block")
	}
	if !strings.Contains(content, "cancel-in-progress: false") {
		t.Error("want cancel-in-progress: false (don't cancel in-flight deploys)")
	}
}

func TestGenerateGHADeploy_BuildImageJob(t *testing.T) {
	cfg := makeDeployFixture([]string{"staging"}, nil)
	content := generateGHADeploy(cfg)

	if !strings.Contains(content, "build-image") {
		t.Error("want build-image job")
	}
	if !strings.Contains(content, "wfctl build --push --format json") {
		t.Error("want wfctl build --push --format json step")
	}
}

func TestGenerateGHADeploy_SHAPinning(t *testing.T) {
	cfg := makeDeployFixture([]string{"staging"}, nil)
	content := generateGHADeploy(cfg)

	if !strings.Contains(content, "workflow_run.head_sha") {
		t.Error("want github.event.workflow_run.head_sha SHA pinning")
	}
}

func TestGenerateGHADeploy_DeployJobsChained(t *testing.T) {
	cfg := makeDeployFixture([]string{"staging", "prod"}, map[string]bool{"prod": true})
	content := generateGHADeploy(cfg)

	if !strings.Contains(content, "deploy-staging") {
		t.Error("want deploy-staging job")
	}
	if !strings.Contains(content, "deploy-prod") {
		t.Error("want deploy-prod job")
	}
	if !strings.Contains(content, "wfctl ci run --phase deploy --env staging") {
		t.Error("want wfctl ci run for staging")
	}
	if !strings.Contains(content, "wfctl ci run --phase deploy --env prod") {
		t.Error("want wfctl ci run for prod")
	}
	if !strings.Contains(content, "environment: prod") {
		t.Error("want environment: prod for approval-required env")
	}
}

func TestGenerateGHADeploy_RegistryEnvVars(t *testing.T) {
	cfg := makeDeployFixture([]string{"staging"}, nil)
	content := generateGHADeploy(cfg)

	// Should expose DIGITALOCEAN_TOKEN from ci.registries[].auth.env.
	if !strings.Contains(content, "DIGITALOCEAN_TOKEN") {
		t.Error("want DIGITALOCEAN_TOKEN from registry auth.env")
	}
}

// T43: retention.yml generated when registries have retention config.
func TestGenerateRetentionYML_WithRetention(t *testing.T) {
	cfg := &config.WorkflowConfig{
		CI: &config.CIConfig{
			Registries: []config.CIRegistry{
				{
					Name: "docr",
					Type: "do",
					Path: "registry.digitalocean.com/myorg",
					Auth: &config.CIRegistryAuth{Env: "DIGITALOCEAN_TOKEN"},
					Retention: &config.CIRegistryRetention{
						KeepLatest: 20,
						Schedule:   "0 7 * * 0",
					},
				},
			},
		},
	}

	content := generateRetentionYML(cfg)
	if content == "" {
		t.Fatal("want retention.yml content, got empty string")
	}
	if !strings.Contains(content, "wfctl registry prune") {
		t.Error("want wfctl registry prune step")
	}
	if !strings.Contains(content, "0 7 * * 0") {
		t.Error("want cron schedule from registry retention config")
	}
}

func TestGenerateRetentionYML_NoRetention_EmptyString(t *testing.T) {
	cfg := &config.WorkflowConfig{
		CI: &config.CIConfig{
			Registries: []config.CIRegistry{
				{Name: "docr", Type: "do", Path: "registry.digitalocean.com/myorg"},
			},
		},
	}

	content := generateRetentionYML(cfg)
	if content != "" {
		t.Errorf("want empty string when no retention config, got: %q", content)
	}
}

func TestGenerateRetentionYML_NilCI_EmptyString(t *testing.T) {
	content := generateRetentionYML(nil)
	if content != "" {
		t.Errorf("want empty string for nil config, got: %q", content)
	}
}
