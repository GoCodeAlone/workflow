package module

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// PluginSpec describes a plugin binary to include in the built image.
type PluginSpec struct {
	Name   string
	Binary string
}

// BuildFromConfigStep reads a workflow config YAML file, assembles a Docker
// build context with the server binary and any required plugin binaries,
// generates a Dockerfile, builds the image, and optionally pushes it.
type BuildFromConfigStep struct {
	name         string
	configFile   string
	baseImage    string
	serverBinary string
	tag          string
	push         bool
	plugins      []PluginSpec

	// execCommand is the function used to create exec.Cmd instances.
	// Defaults to exec.CommandContext; overridable in tests.
	execCommand func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// NewBuildFromConfigStepFactory returns a StepFactory that creates BuildFromConfigStep instances.
func NewBuildFromConfigStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		configFile, _ := config["config_file"].(string)
		if configFile == "" {
			return nil, fmt.Errorf("build_from_config step %q: 'config_file' is required", name)
		}

		tag, _ := config["tag"].(string)
		if tag == "" {
			return nil, fmt.Errorf("build_from_config step %q: 'tag' is required", name)
		}

		baseImage, _ := config["base_image"].(string)
		if baseImage == "" {
			baseImage = "ghcr.io/gocodealone/workflow-runtime:latest"
		}

		serverBinary, _ := config["server_binary"].(string)
		if serverBinary == "" {
			serverBinary = "/usr/local/bin/workflow-server"
		}

		push, _ := config["push"].(bool)

		var plugins []PluginSpec
		if pluginsRaw, ok := config["plugins"].([]any); ok {
			for i, p := range pluginsRaw {
				m, ok := p.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("build_from_config step %q: plugins[%d] must be a map", name, i)
				}
				pName, _ := m["name"].(string)
				pBinary, _ := m["binary"].(string)
				if pName == "" || pBinary == "" {
					return nil, fmt.Errorf("build_from_config step %q: plugins[%d] requires 'name' and 'binary'", name, i)
				}
				plugins = append(plugins, PluginSpec{Name: pName, Binary: pBinary})
			}
		}

		return &BuildFromConfigStep{
			name:         name,
			configFile:   configFile,
			baseImage:    baseImage,
			serverBinary: serverBinary,
			tag:          tag,
			push:         push,
			plugins:      plugins,
			execCommand:  exec.CommandContext,
		}, nil
	}
}

// Name returns the step name.
func (s *BuildFromConfigStep) Name() string { return s.name }

// Execute assembles the build context, generates a Dockerfile, builds the
// Docker image, and optionally pushes it.
func (s *BuildFromConfigStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	// Validate that the config file exists.
	if _, err := os.Stat(s.configFile); err != nil {
		return nil, fmt.Errorf("build_from_config step %q: config_file %q not found: %w", s.name, s.configFile, err)
	}

	// Validate that the server binary exists.
	if _, err := os.Stat(s.serverBinary); err != nil {
		return nil, fmt.Errorf("build_from_config step %q: server_binary %q not found: %w", s.name, s.serverBinary, err)
	}

	// Create a temporary build context directory.
	buildDir, err := os.MkdirTemp("", "workflow-build-*")
	if err != nil {
		return nil, fmt.Errorf("build_from_config step %q: failed to create temp build dir: %w", s.name, err)
	}
	defer os.RemoveAll(buildDir)

	// Copy config file into build context as config.yaml.
	if err := copyFile(s.configFile, filepath.Join(buildDir, "config.yaml")); err != nil {
		return nil, fmt.Errorf("build_from_config step %q: failed to copy config file: %w", s.name, err)
	}

	// Copy server binary into build context as server.
	serverDst := filepath.Join(buildDir, "server")
	if err := copyFile(s.serverBinary, serverDst); err != nil {
		return nil, fmt.Errorf("build_from_config step %q: failed to copy server binary: %w", s.name, err)
	}
	if err := os.Chmod(serverDst, 0755); err != nil { //nolint:gosec // G302: intentionally executable
		return nil, fmt.Errorf("build_from_config step %q: failed to chmod server binary: %w", s.name, err)
	}

	// Copy plugin binaries into build context under plugins/<name>/.
	pluginsDir := filepath.Join(buildDir, "plugins")
	for _, plugin := range s.plugins {
		if _, err := os.Stat(plugin.Binary); err != nil {
			return nil, fmt.Errorf("build_from_config step %q: plugin %q binary %q not found: %w",
				s.name, plugin.Name, plugin.Binary, err)
		}
		pluginDir := filepath.Join(pluginsDir, plugin.Name)
		if err := os.MkdirAll(pluginDir, 0750); err != nil {
			return nil, fmt.Errorf("build_from_config step %q: failed to create plugin dir for %q: %w",
				s.name, plugin.Name, err)
		}
		pluginBinaryName := filepath.Base(plugin.Binary)
		pluginDst := filepath.Join(pluginDir, pluginBinaryName)
		if err := copyFile(plugin.Binary, pluginDst); err != nil {
			return nil, fmt.Errorf("build_from_config step %q: failed to copy plugin %q binary: %w",
				s.name, plugin.Name, err)
		}
		if err := os.Chmod(pluginDst, 0755); err != nil { //nolint:gosec // G302: intentionally executable
			return nil, fmt.Errorf("build_from_config step %q: failed to chmod plugin %q binary: %w",
				s.name, plugin.Name, err)
		}
	}

	// Generate Dockerfile content.
	dockerfileContent := s.generateDockerfile()

	// Write Dockerfile into build context.
	dockerfilePath := filepath.Join(buildDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfileContent), 0600); err != nil {
		return nil, fmt.Errorf("build_from_config step %q: failed to write Dockerfile: %w", s.name, err)
	}

	// Execute docker build.
	if err := s.runDockerBuild(ctx, buildDir); err != nil {
		return nil, fmt.Errorf("build_from_config step %q: docker build failed: %w", s.name, err)
	}

	// Optionally push the image.
	if s.push {
		if err := s.runDockerPush(ctx); err != nil {
			return nil, fmt.Errorf("build_from_config step %q: docker push failed: %w", s.name, err)
		}
	}

	return &StepResult{
		Output: map[string]any{
			"image_tag":          s.tag,
			"dockerfile_content": dockerfileContent,
		},
	}, nil
}

// generateDockerfile returns a Dockerfile string for the build context layout.
func (s *BuildFromConfigStep) generateDockerfile() string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "FROM %s\n", s.baseImage)
	sb.WriteString("COPY server /server\n")
	sb.WriteString("COPY config.yaml /app/config.yaml\n")

	if len(s.plugins) > 0 {
		sb.WriteString("COPY plugins/ /app/data/plugins/\n")
	}

	sb.WriteString("WORKDIR /app\n")
	sb.WriteString("ENTRYPOINT [\"/server\"]\n")
	sb.WriteString("CMD [\"-config\", \"/app/config.yaml\", \"-data-dir\", \"/app/data\"]\n")

	return sb.String()
}

// runDockerBuild executes "docker build -t <tag> <buildDir>".
func (s *BuildFromConfigStep) runDockerBuild(ctx context.Context, buildDir string) error {
	var stdout, stderr bytes.Buffer
	cmd := s.execCommand(ctx, "docker", "build", "-t", s.tag, buildDir) //nolint:gosec // G204: tag from trusted pipeline config
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	return nil
}

// runDockerPush executes "docker push <tag>".
func (s *BuildFromConfigStep) runDockerPush(ctx context.Context) error {
	var stdout, stderr bytes.Buffer
	cmd := s.execCommand(ctx, "docker", "push", s.tag) //nolint:gosec // G204: tag from trusted pipeline config
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	return nil
}

// copyFile copies src to dst, creating dst if it does not exist.
func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // G304: path from trusted pipeline config
	if err != nil {
		return fmt.Errorf("open %q: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create %q: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %q -> %q: %w", src, dst, err)
	}
	return nil
}
