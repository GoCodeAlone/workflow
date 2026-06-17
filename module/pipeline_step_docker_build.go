package module

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

	args := []string{"build", "--rm", "-f", s.dockerfile}
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
	args = append(args, s.contextPath)

	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker_build step %q: build failed: %w: %s", s.name, err, string(out))
	}

	imageID := ""
	if len(s.tags) > 0 {
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

func inspectDockerImageID(ctx context.Context, ref string) string {
	out, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", ref).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// createBuildContext creates a tar archive of the build context directory.
func createBuildContext(contextPath string) (io.Reader, error) {
	absPath, err := filepath.Abs(contextPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve context path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("context path %q does not exist: %w", absPath, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("context path %q is not a directory", absPath)
	}

	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(archiveDirectory(absPath, pw))
	}()

	return pr, nil
}

// archiveDirectory creates a tar archive of a directory and writes it to w.
func archiveDirectory(dir string, w io.Writer) error {
	tw := tar.NewWriter(w)
	defer tw.Close()

	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		// Use forward slashes for tar paths
		relPath = filepath.ToSlash(relPath)

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relPath

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(tw, f)
		return err
	})
}

// parseBuildOutput reads the Docker build JSON stream and extracts the image ID.
func parseBuildOutput(r io.Reader) (string, error) {
	decoder := json.NewDecoder(r)
	var imageID string

	for {
		var msg struct {
			Stream string `json:"stream"`
			Aux    struct {
				ID string `json:"ID"`
			} `json:"aux"`
			Error string `json:"error"`
		}
		if err := decoder.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			return "", fmt.Errorf("failed to parse build output: %w", err)
		}
		if msg.Error != "" {
			return "", fmt.Errorf("build error: %s", msg.Error)
		}
		if msg.Aux.ID != "" {
			imageID = msg.Aux.ID
		}
	}

	return imageID, nil
}
