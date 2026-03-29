package main

import (
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestRunMultiServiceBuild_Empty(t *testing.T) {
	if err := runMultiServiceBuild(nil, false); err != nil {
		t.Fatalf("nil services should not error: %v", err)
	}
	if err := runMultiServiceBuild(map[string]*config.ServiceConfig{}, false); err != nil {
		t.Fatalf("empty services should not error: %v", err)
	}
}

func TestRunMultiServiceBuild_NoBinary(t *testing.T) {
	// Services without Binary are silently skipped.
	services := map[string]*config.ServiceConfig{
		"ui": {Description: "frontend", Binary: ""},
	}
	if err := runMultiServiceBuild(services, false); err != nil {
		t.Fatalf("service without binary should not error: %v", err)
	}
}

func TestRunMultiServiceDeploy_NilDeploy(t *testing.T) {
	err := runMultiServiceDeploy(nil, "staging", nil, map[string]*config.ServiceConfig{}, false)
	if err == nil {
		t.Fatal("expected error for nil deploy config")
	}
}

func TestRunMultiServiceDeploy_RequireApproval(t *testing.T) {
	deploy := &config.CIDeployConfig{
		Environments: map[string]*config.CIDeployEnvironment{
			"prod": {Provider: "kubernetes", RequireApproval: true},
		},
	}
	services := map[string]*config.ServiceConfig{
		"api":    {Binary: "./cmd/api"},
		"worker": {Binary: "./cmd/worker"},
	}
	if err := runMultiServiceDeploy(deploy, "prod", nil, services, false); err != nil {
		t.Fatalf("approval skip should not error: %v", err)
	}
}

func TestRunMultiServiceDeploy_AWSECS(t *testing.T) {
	deploy := &config.CIDeployConfig{
		Environments: map[string]*config.CIDeployEnvironment{
			"staging": {Provider: "aws-ecs", Region: "us-east-1"},
		},
	}
	services := map[string]*config.ServiceConfig{
		"api": {
			Binary:  "./cmd/api",
			Scaling: &config.ScalingConfig{Replicas: 2},
			Expose:  []config.ExposeConfig{{Port: 8080}},
		},
	}
	if err := runMultiServiceDeploy(deploy, "staging", nil, services, false); err != nil {
		t.Fatalf("aws-ecs multi-service deploy should not error: %v", err)
	}
}

func TestBuildServiceBinary_BadPath(t *testing.T) {
	svc := &config.ServiceConfig{Binary: "./nonexistent/cmd/nope"}
	err := buildServiceBinary("test-svc", svc, false)
	if err == nil {
		t.Fatal("expected error building nonexistent binary path")
	}
}
