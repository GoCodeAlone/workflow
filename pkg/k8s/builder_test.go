package k8s

import (
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/deploy"
	"github.com/GoCodeAlone/workflow/manifest"
)

func TestBuild_MinimalRequest(t *testing.T) {
	req := &deploy.DeployRequest{
		AppName:   "test-app",
		Image:     "test:v1",
		Namespace: "default",
		Config:    &config.WorkflowConfig{},
	}

	ms, err := Build(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ms.Objects) == 0 {
		t.Fatal("expected at least one object")
	}

	// Should have ConfigMap, Deployment, Service (no Namespace for "default")
	kinds := make(map[string]int)
	for _, obj := range ms.Objects {
		kinds[obj.GetKind()]++
	}

	if kinds["Deployment"] != 1 {
		t.Errorf("expected 1 Deployment, got %d", kinds["Deployment"])
	}
	if kinds["Service"] != 1 {
		t.Errorf("expected 1 Service, got %d", kinds["Service"])
	}
	if kinds["Namespace"] != 0 {
		t.Error("did not expect Namespace for 'default' namespace")
	}
}

func TestBuild_NonDefaultNamespace(t *testing.T) {
	req := &deploy.DeployRequest{
		AppName:   "test-app",
		Image:     "test:v1",
		Namespace: "prod",
		Config:    &config.WorkflowConfig{},
	}

	ms, err := Build(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hasNamespace := false
	for _, obj := range ms.Objects {
		if obj.GetKind() == "Namespace" {
			hasNamespace = true
			if obj.GetName() != "prod" {
				t.Errorf("got namespace name %q, want %q", obj.GetName(), "prod")
			}
		}
	}
	if !hasNamespace {
		t.Error("expected Namespace object for non-default namespace")
	}
}

func TestBuild_WithDatabases(t *testing.T) {
	req := &deploy.DeployRequest{
		AppName:   "test-app",
		Image:     "test:v1",
		Namespace: "default",
		Config:    &config.WorkflowConfig{},
		Manifest: &manifest.WorkflowManifest{
			Databases: []manifest.DatabaseRequirement{
				{ModuleName: "main-db", Driver: "postgres", EstCapacityMB: 512},
			},
			Ports: []manifest.PortRequirement{
				{Port: 8080, Protocol: "http"},
			},
			ResourceEst: manifest.ResourceEstimate{CPUCores: 0.5, MemoryMB: 256},
		},
	}

	ms, err := Build(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hasPVC := false
	for _, obj := range ms.Objects {
		if obj.GetKind() == "PersistentVolumeClaim" {
			hasPVC = true
		}
	}
	if !hasPVC {
		t.Error("expected PVC for database requirement")
	}
}

func TestBuild_WithSecret(t *testing.T) {
	req := &deploy.DeployRequest{
		AppName:   "test-app",
		Image:     "test:v1",
		Namespace: "default",
		SecretRef: "app-secrets",
		Config:    &config.WorkflowConfig{},
	}

	ms, err := Build(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hasSecret := false
	for _, obj := range ms.Objects {
		if obj.GetKind() == "Secret" {
			hasSecret = true
		}
	}
	if !hasSecret {
		t.Error("expected Secret when SecretRef is set")
	}
}

func TestBuild_MissingAppName(t *testing.T) {
	req := &deploy.DeployRequest{
		Image: "test:v1",
	}
	_, err := Build(req)
	if err == nil {
		t.Error("expected error for missing appName")
	}
}

func TestBuild_MissingImage(t *testing.T) {
	req := &deploy.DeployRequest{
		AppName: "test",
	}
	_, err := Build(req)
	if err == nil {
		t.Error("expected error for missing image")
	}
}

func TestBuild_ResourceOrdering(t *testing.T) {
	req := &deploy.DeployRequest{
		AppName:   "test-app",
		Image:     "test:v1",
		Namespace: "prod",
		SecretRef: "secrets",
		Config:    &config.WorkflowConfig{},
	}

	ms, err := Build(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// After Sort(), Namespace should come first, then ConfigMap, Secret, Deployment, Service
	if len(ms.Objects) < 2 {
		t.Fatal("expected at least 2 objects")
	}
	if ms.Objects[0].GetKind() != "Namespace" {
		t.Errorf("expected first object to be Namespace, got %s", ms.Objects[0].GetKind())
	}
}
