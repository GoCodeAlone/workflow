package main

import (
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestRunBuildPhase_NilConfig(t *testing.T) {
	if err := runBuildPhase(nil, false); err != nil {
		t.Fatalf("nil build config should not error: %v", err)
	}
}

func TestRunTestPhase_NilConfig(t *testing.T) {
	if err := runTestPhase(nil, false); err != nil {
		t.Fatalf("nil test config should not error: %v", err)
	}
}

func TestRunDeployPhase_NilConfig(t *testing.T) {
	err := runDeployPhase(nil, "staging", false)
	if err == nil {
		t.Fatal("expected error for nil deploy config")
	}
}

func TestRunDeployPhase_MissingEnv(t *testing.T) {
	deploy := &config.CIDeployConfig{
		Environments: map[string]*config.CIDeployEnvironment{
			"staging": {Provider: "aws-ecs"},
		},
	}
	err := runDeployPhase(deploy, "production", false)
	if err == nil {
		t.Fatal("expected error for missing environment")
	}
}

func TestRunDeployPhase_RequiresApproval(t *testing.T) {
	deploy := &config.CIDeployConfig{
		Environments: map[string]*config.CIDeployEnvironment{
			"prod": {Provider: "aws-ecs", RequireApproval: true},
		},
	}
	if err := runDeployPhase(deploy, "prod", false); err != nil {
		t.Fatalf("approval skip should not error: %v", err)
	}
}

func TestRunDeployPhase_Placeholder(t *testing.T) {
	deploy := &config.CIDeployConfig{
		Environments: map[string]*config.CIDeployEnvironment{
			"staging": {Provider: "aws-ecs", Strategy: "rolling"},
		},
	}
	if err := runDeployPhase(deploy, "staging", false); err != nil {
		t.Fatalf("placeholder deploy should not error: %v", err)
	}
}

func TestRunBuildPhase_EmptyBuild(t *testing.T) {
	build := &config.CIBuildConfig{}
	if err := runBuildPhase(build, false); err != nil {
		t.Fatalf("empty build config should not error: %v", err)
	}
}

func TestRunTestPhase_EmptyTest(t *testing.T) {
	test := &config.CITestConfig{}
	if err := runTestPhase(test, false); err != nil {
		t.Fatalf("empty test config should not error: %v", err)
	}
}
