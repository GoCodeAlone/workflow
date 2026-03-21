package module

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/GoCodeAlone/modular"
)

// ContainerRegistry is a service that can receive pushed container images.
// Implementations are looked up from the application service registry.
type ContainerRegistry interface {
	// PushImage pushes a locally built image (by reference) to the registry.
	// Returns the image digest on success.
	PushImage(ctx context.Context, imageRef string) (string, error)
	// RegistryURL returns the registry's base URL used to prefix image tags.
	RegistryURL() string
}

// resolveContainerRegistry looks up a ContainerRegistry from the service registry.
func resolveContainerRegistry(app modular.Application, registryName, stepName string) (ContainerRegistry, error) {
	if app == nil {
		return nil, fmt.Errorf("step %q: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[registryName]
	if !ok {
		return nil, fmt.Errorf("step %q: registry %q not found in registry", stepName, registryName)
	}
	reg, ok := svc.(ContainerRegistry)
	if !ok {
		return nil, fmt.Errorf("step %q: service %q does not implement ContainerRegistry (got %T)", stepName, registryName, svc)
	}
	return reg, nil
}

// ─── step.container_build ─────────────────────────────────────────────────────

// ContainerBuildStep builds a container image using the local docker/podman CLI,
// then pushes it to a configured registry.
type ContainerBuildStep struct {
	name        string
	dockerfile  string
	contextPath string
	registry    string
	tag         string
	buildArgs   map[string]string
	dryRun      bool
	builder     string // "docker" or "podman"
	app         modular.Application
	execCommand func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// NewContainerBuildStepFactory returns a StepFactory for step.container_build.
func NewContainerBuildStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		contextPath, _ := cfg["context"].(string)
		if contextPath == "" {
			return nil, fmt.Errorf("container_build step %q: 'context' is required", name)
		}

		tag, _ := cfg["tag"].(string)
		if tag == "" {
			return nil, fmt.Errorf("container_build step %q: 'tag' is required", name)
		}

		registryName, _ := cfg["registry"].(string)
		if registryName == "" {
			return nil, fmt.Errorf("container_build step %q: 'registry' is required", name)
		}

		dockerfile, _ := cfg["dockerfile"].(string)
		if dockerfile == "" {
			dockerfile = "Dockerfile"
		}

		buildArgs := make(map[string]string)
		if argsRaw, ok := cfg["build_args"].(map[string]any); ok {
			for k, v := range argsRaw {
				buildArgs[k] = fmt.Sprintf("%v", v)
			}
		}

		builder, _ := cfg["builder"].(string)
		if builder == "" {
			builder = "docker"
		}

		dryRun, _ := cfg["dry_run"].(bool)

		return &ContainerBuildStep{
			name:        name,
			dockerfile:  dockerfile,
			contextPath: contextPath,
			registry:    registryName,
			tag:         tag,
			buildArgs:   buildArgs,
			dryRun:      dryRun,
			builder:     builder,
			app:         app,
			execCommand: exec.CommandContext,
		}, nil
	}
}

// Name returns the step name.
func (s *ContainerBuildStep) Name() string { return s.name }

// Execute builds the container image and pushes it to the registry.
func (s *ContainerBuildStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	reg, err := resolveContainerRegistry(s.app, s.registry, s.name)
	if err != nil {
		return nil, err
	}

	// Compute the full image reference: <registry_url>/<tag>
	registryURL := reg.RegistryURL()
	fullRef := s.tag
	if registryURL != "" && !strings.HasPrefix(s.tag, registryURL) {
		fullRef = strings.TrimSuffix(registryURL, "/") + "/" + strings.TrimPrefix(s.tag, "/")
	}

	if s.dryRun {
		return &StepResult{Output: map[string]any{
			"dry_run":    true,
			"image_ref":  fullRef,
			"dockerfile": s.dockerfile,
			"context":    s.contextPath,
		}}, nil
	}

	// Build the image.
	buildArgs := s.buildBuildArgs(fullRef)
	var stdout, stderr bytes.Buffer
	cmd := s.execCommand(ctx, s.builder, buildArgs...) //nolint:gosec // G204: trusted args
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("container_build step %q: build failed: %w\nstdout: %s\nstderr: %s",
			s.name, err, stdout.String(), stderr.String())
	}

	// Push the image.
	digest, err := reg.PushImage(ctx, fullRef)
	if err != nil {
		return nil, fmt.Errorf("container_build step %q: push failed: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"success":    true,
		"image_ref":  fullRef,
		"digest":     digest,
		"dockerfile": s.dockerfile,
		"context":    s.contextPath,
		"builder":    s.builder,
	}}, nil
}

// buildBuildArgs constructs the CLI arguments for the build command.
func (s *ContainerBuildStep) buildBuildArgs(fullRef string) []string {
	args := []string{"build", "-f", s.dockerfile, "-t", fullRef}
	for k, v := range s.buildArgs {
		args = append(args, "--build-arg", k+"="+v)
	}
	args = append(args, s.contextPath)
	return args
}
