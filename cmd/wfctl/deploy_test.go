package main

import (
	"os"
	"strings"
	"testing"
)

// TestRunDeployRegistered verifies the deploy command is present in the commands map.
func TestRunDeployRegistered(t *testing.T) {
	if _, ok := commands["deploy"]; !ok {
		t.Fatal("deploy command not registered in commands map")
	}
}

// TestRunDeployMissingTarget verifies an error when no subcommand is given.
func TestRunDeployMissingTarget(t *testing.T) {
	err := runDeploy([]string{})
	if err == nil {
		t.Fatal("expected error when no deploy target given")
	}
	if !strings.Contains(err.Error(), "deploy target is required") {
		t.Errorf("expected 'deploy target is required' in error, got: %v", err)
	}
}

// TestRunDeployUnknownTarget verifies an error for an unknown target.
func TestRunDeployUnknownTarget(t *testing.T) {
	err := runDeploy([]string{"ftp"})
	if err == nil {
		t.Fatal("expected error for unknown deploy target")
	}
	if !strings.Contains(err.Error(), "deploy target is required") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

// TestRunDeployDockerFlagParsing verifies that -h is handled without panic.
func TestRunDeployDockerFlagParsing(t *testing.T) {
	// -h triggers ContinueOnError to return an error (flag.ErrHelp), not a panic.
	err := runDeployDocker([]string{"-h"})
	if err == nil {
		t.Fatal("expected error from -h (flag.ErrHelp)")
	}
}

// TestRunDeployKubernetesFlagParsing verifies that -h is handled without panic.
func TestRunDeployKubernetesFlagParsing(t *testing.T) {
	err := runDeployKubernetes([]string{"-h"})
	if err == nil {
		t.Fatal("expected error from -h (flag.ErrHelp)")
	}
}

// TestRunDeployCloudNoTarget returns usage guidance as an error.
func TestRunDeployCloudNoTarget(t *testing.T) {
	err := runDeployCloud([]string{})
	if err == nil {
		t.Fatal("expected error from cloud deploy stub")
	}
	if !strings.Contains(err.Error(), "cloud deployment requires") {
		t.Errorf("expected cloud deployment guidance in error, got: %v", err)
	}
}

// TestRunDeployCloudInvalidTarget returns an error for an unknown target name.
func TestRunDeployCloudInvalidTarget(t *testing.T) {
	err := runDeployCloud([]string{"-target", "dev"})
	if err == nil {
		t.Fatal("expected error for invalid cloud target")
	}
	if !strings.Contains(err.Error(), "must be staging or production") {
		t.Errorf("expected target validation error, got: %v", err)
	}
}

// TestRunDeployCloudValidTarget returns the stub guidance error for a valid target.
func TestRunDeployCloudValidTarget(t *testing.T) {
	err := runDeployCloud([]string{"-target", "staging"})
	if err == nil {
		t.Fatal("expected stub error from cloud deploy")
	}
	if !strings.Contains(err.Error(), "cloud deployment requires") {
		t.Errorf("expected guidance error, got: %v", err)
	}
}

// TestWriteDockerfile verifies the generated Dockerfile is non-empty and contains expected content.
func TestWriteDockerfile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/Dockerfile"
	if err := writeDockerfile(path); err != nil {
		t.Fatalf("writeDockerfile failed: %v", err)
	}

	data, err := readFileContents(path)
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	for _, want := range []string{"FROM golang", "go build", "FROM alpine", "EXPOSE 8080"} {
		if !strings.Contains(data, want) {
			t.Errorf("Dockerfile missing %q", want)
		}
	}
}

// TestWriteDockerCompose verifies the generated docker-compose.yml contains expected fields.
func TestWriteDockerCompose(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/docker-compose.yml"
	if err := writeDockerCompose(path, "workflow.yaml", "my-app:latest"); err != nil {
		t.Fatalf("writeDockerCompose failed: %v", err)
	}

	data, err := readFileContents(path)
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	for _, want := range []string{"my-app:latest", "8080:8080", "workflow.yaml", "WORKFLOW_ADDR"} {
		if !strings.Contains(data, want) {
			t.Errorf("docker-compose.yml missing %q", want)
		}
	}
}

func readFileContents(path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}
