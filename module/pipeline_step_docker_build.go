package module

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/modular"
)

// DockerBuildStep builds a Docker image from a context directory and Dockerfile.
type DockerBuildStep struct {
	name        string
	contextPath string
	dockerfile  string
	tags        []string
	buildArgs   map[string]*string
	cacheFrom   []string
}

// NewDockerBuildStepFactory returns a StepFactory that creates DockerBuildStep instances.
func NewDockerBuildStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		contextPath, _ := config["context"].(string)
		if contextPath == "" {
			return nil, fmt.Errorf("docker_build step %q: 'context' is required", name)
		}

		dockerfile, _ := config["dockerfile"].(string)
		if dockerfile == "" {
			dockerfile = "Dockerfile"
		}

		var tags []string
		if tagsRaw, ok := config["tags"].([]any); ok {
			for i, t := range tagsRaw {
				s, ok := t.(string)
				if !ok {
					return nil, fmt.Errorf("docker_build step %q: tag %d must be a string", name, i)
				}
				tags = append(tags, s)
			}
		}

		buildArgs := make(map[string]*string)
		if argsRaw, ok := config["build_args"].(map[string]any); ok {
			for k, v := range argsRaw {
				s := fmt.Sprintf("%v", v)
				buildArgs[k] = &s
			}
		}

		var cacheFrom []string
		if cfRaw, ok := config["cache_from"].([]any); ok {
			for _, c := range cfRaw {
				if s, ok := c.(string); ok {
					cacheFrom = append(cacheFrom, s)
				}
			}
		}

		return &DockerBuildStep{
			name:        name,
			contextPath: contextPath,
			dockerfile:  dockerfile,
			tags:        tags,
			buildArgs:   buildArgs,
			cacheFrom:   cacheFrom,
		}, nil
	}
}

// Name returns the step name.
func (s *DockerBuildStep) Name() string { return s.name }

// Execute builds a Docker image using the Docker CLI.
func (s *DockerBuildStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker_build step %q: docker CLI not found: %w", s.name, err)
	}

	contextPath, err := filepath.Abs(s.contextPath)
	if err != nil {
		return nil, fmt.Errorf("docker_build step %q: resolve context path: %w", s.name, err)
	}
	dockerfile := s.dockerfilePath(contextPath)

	iidFile, err := os.CreateTemp("", "workflow-docker-build-iid-*")
	if err != nil {
		return nil, fmt.Errorf("docker_build step %q: create iidfile: %w", s.name, err)
	}
	iidPath := iidFile.Name()
	_ = iidFile.Close()
	defer os.Remove(iidPath)

	args := []string{"build", "--rm", "--iidfile", iidPath, "-f", dockerfile}
	for _, tag := range s.tags {
		args = append(args, "-t", tag)
	}
	for key, value := range s.buildArgs {
		arg := key + "="
		if value != nil {
			arg += *value
		}
		args = append(args, "--build-arg", arg)
	}
	for _, ref := range s.cacheFrom {
		args = append(args, "--cache-from", ref)
	}
	args = append(args, contextPath)

	cmd := exec.CommandContext(ctx, "docker", args...) // #nosec G204,G702 - workflow docker_build intentionally executes user-configured Docker CLI args.
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker_build step %q: build failed: %w: %s", s.name, err, string(out))
	}

	imageID := readDockerIIDFile(iidPath)
	if imageID == "" && len(s.tags) > 0 {
		imageID = inspectDockerImageID(ctx, s.tags[0])
	}
	return &StepResult{
		Output: map[string]any{
			"image_id": imageID,
			"tags":     s.tags,
			"context":  s.contextPath,
		},
	}, nil
}

func (s *DockerBuildStep) dockerfilePath(absContextPath string) string {
	if filepath.IsAbs(s.dockerfile) {
		return s.dockerfile
	}
	return filepath.Join(absContextPath, s.dockerfile)
}

func readDockerIIDFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func inspectDockerImageID(ctx context.Context, ref string) string {
	out, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", ref).Output() // #nosec G204,G702 - ref is the configured image tag passed to Docker.
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
