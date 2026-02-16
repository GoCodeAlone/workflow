package module

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/CrisisTextLine/modular"
	"github.com/docker/docker/api/types/image"
	dockerclient "github.com/docker/docker/client"
)

// DockerPushStep pushes a Docker image to a remote registry.
type DockerPushStep struct {
	name         string
	image        string
	registry     string
	authProvider string
}

// NewDockerPushStepFactory returns a StepFactory that creates DockerPushStep instances.
func NewDockerPushStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		img, _ := config["image"].(string)
		if img == "" {
			return nil, fmt.Errorf("docker_push step %q: 'image' is required", name)
		}

		registry, _ := config["registry"].(string)
		authProvider, _ := config["auth_provider"].(string)

		return &DockerPushStep{
			name:         name,
			image:        img,
			registry:     registry,
			authProvider: authProvider,
		}, nil
	}
}

// Name returns the step name.
func (s *DockerPushStep) Name() string { return s.name }

// Execute pushes the image to the configured registry.
func (s *DockerPushStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker_push step %q: failed to create Docker client: %w", s.name, err)
	}
	defer cli.Close()

	// Determine the full image reference
	ref := s.image
	if s.registry != "" {
		ref = s.registry + "/" + s.image
	}

	opts := image.PushOptions{}

	reader, err := cli.ImagePush(ctx, ref, opts)
	if err != nil {
		return nil, fmt.Errorf("docker_push step %q: push failed: %w", s.name, err)
	}
	defer reader.Close()

	// Read push output to completion and extract the digest
	digest, err := parsePushOutput(reader)
	if err != nil {
		return nil, fmt.Errorf("docker_push step %q: %w", s.name, err)
	}

	return &StepResult{
		Output: map[string]any{
			"image":         s.image,
			"registry":      s.registry,
			"digest":        digest,
			"auth_provider": s.authProvider,
		},
	}, nil
}

// parsePushOutput reads the Docker push JSON stream and extracts the digest.
func parsePushOutput(r io.Reader) (string, error) {
	decoder := json.NewDecoder(r)
	var digest string

	for {
		var msg struct {
			Status string `json:"status"`
			Aux    struct {
				Tag    string `json:"Tag"`
				Digest string `json:"Digest"`
				Size   int64  `json:"Size"`
			} `json:"aux"`
			Error string `json:"error"`
		}
		if err := decoder.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			return "", fmt.Errorf("failed to parse push output: %w", err)
		}
		if msg.Error != "" {
			return "", fmt.Errorf("push error: %s", msg.Error)
		}
		if msg.Aux.Digest != "" {
			digest = msg.Aux.Digest
		}
	}

	return digest, nil
}
