package main

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestGenerateGHABootstrap_NoConfig(t *testing.T) {
	content := generateGHABootstrap(nil)
	if !strings.Contains(content, "name: CI/CD") {
		t.Error("expected 'name: CI/CD' in output")
	}
	if !strings.Contains(content, "wfctl ci run --phase build,test") {
		t.Error("expected wfctl ci run --phase build,test in output")
	}
	if !strings.Contains(content, "GoCodeAlone/setup-wfctl@bcd880980f5bbe8d192d0c20ff6279d25331f956 # v1") {
		t.Error("expected SHA-pinned setup-wfctl action in output")
	}
}

func TestGenerateGHABootstrap_WithEnvironments(t *testing.T) {
	cfg := &config.WorkflowConfig{
		CI: &config.CIConfig{
			Deploy: &config.CIDeployConfig{
				Environments: map[string]*config.CIDeployEnvironment{
					"staging": {Provider: "aws-ecs"},
					"prod":    {Provider: "aws-ecs", RequireApproval: true},
				},
			},
		},
	}
	content := generateGHABootstrap(cfg)
	if !strings.Contains(content, "deploy-staging") {
		t.Error("expected deploy-staging job in output")
	}
	if !strings.Contains(content, "deploy-prod") {
		t.Error("expected deploy-prod job in output")
	}
	if !strings.Contains(content, "environment: prod") {
		t.Error("expected environment: prod for approval-required env")
	}
	if !strings.Contains(content, "wfctl ci run --phase deploy --env staging") {
		t.Error("expected deploy command for staging")
	}
}

func TestGenerateGHABootstrap_WithMigrationsAddsDeployGuard(t *testing.T) {
	cfg := &config.WorkflowConfig{
		CI: &config.CIConfig{
			Deploy: &config.CIDeployConfig{Environments: map[string]*config.CIDeployEnvironment{
				"staging": {Provider: "aws-ecs"},
			}},
			Migrations: []config.CIMigrationConfig{{Name: "app", SourceDir: "migrations"}},
		},
	}
	content := generateGHABootstrap(cfg)
	if !strings.Contains(content, "wfctl migrations validate --env staging --commit ${{ github.sha }} --result-file .wfctl/migrations-result.json --format json") {
		t.Fatalf("expected migration validate result before deploy:\n%s", content)
	}
	if strings.Contains(content, "wfctl migrations ci-check --env staging") {
		t.Fatalf("expected ci run deploy to perform migration ci-check once:\n%s", content)
	}
}

func TestGenerateGitLabCIBootstrap_WithMigrationsAddsDeployGuard(t *testing.T) {
	cfg := &config.WorkflowConfig{
		CI: &config.CIConfig{
			Deploy: &config.CIDeployConfig{Environments: map[string]*config.CIDeployEnvironment{
				"staging": {Provider: "k8s"},
			}},
			Migrations: []config.CIMigrationConfig{{Name: "app", SourceDir: "migrations"}},
		},
	}
	content := generateGitLabCIBootstrap(cfg)
	if !strings.Contains(content, "wfctl migrations validate --env staging --commit $CI_COMMIT_SHA --result-file .wfctl/migrations-result.json --format json") {
		t.Fatalf("expected migration validate result before deploy:\n%s", content)
	}
	if strings.Contains(content, "wfctl migrations ci-check --env staging") {
		t.Fatalf("expected ci run deploy to perform migration ci-check once:\n%s", content)
	}
}

func TestGenerateGitLabCIBootstrap_NoConfig(t *testing.T) {
	content := generateGitLabCIBootstrap(nil)
	if !strings.Contains(content, "stages:") {
		t.Error("expected stages: in output")
	}
	if !strings.Contains(content, "wfctl ci run --phase build") {
		t.Error("expected build command in output")
	}
	if !strings.Contains(content, "wfctl ci run --phase test") {
		t.Error("expected test command in output")
	}
}

func TestGenerateGitLabCIBootstrap_WithEnvironments(t *testing.T) {
	cfg := &config.WorkflowConfig{
		CI: &config.CIConfig{
			Deploy: &config.CIDeployConfig{
				Environments: map[string]*config.CIDeployEnvironment{
					"staging": {Provider: "k8s"},
					"prod":    {Provider: "k8s", RequireApproval: true},
				},
			},
		},
	}
	content := generateGitLabCIBootstrap(cfg)
	if !strings.Contains(content, "deploy-staging") {
		t.Error("expected deploy-staging job")
	}
	if !strings.Contains(content, "deploy-prod") {
		t.Error("expected deploy-prod job")
	}
	// prod requires approval so it should have "when: manual"
	if !strings.Contains(content, "when: manual") {
		t.Error("expected when: manual for approval-required environment")
	}
}

func TestDirOf(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{".github/workflows/ci.yml", ".github/workflows"},
		{".gitlab-ci.yml", ""},
		{"ci.yml", ""},
		{"a/b/c.yml", "a/b"},
	}
	for _, tt := range tests {
		got := dirOf(tt.path)
		if got != tt.want {
			t.Errorf("dirOf(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
