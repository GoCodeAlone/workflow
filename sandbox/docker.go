// Package sandbox provides Docker-based sandboxed execution for CI/CD pipeline steps.
package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// Mount describes a bind mount from host to container.
type Mount struct {
	Source   string `yaml:"source"`
	Target   string `yaml:"target"`
	ReadOnly bool   `yaml:"read_only"`
}

// SandboxConfig holds configuration for a Docker sandbox execution environment.
type SandboxConfig struct {
	Image       string            `yaml:"image"`
	WorkDir     string            `yaml:"work_dir"`
	Env         map[string]string `yaml:"env"`
	Mounts      []Mount           `yaml:"mounts"`
	MemoryLimit int64             `yaml:"memory_limit"`
	CPULimit    float64           `yaml:"cpu_limit"`
	Timeout     time.Duration     `yaml:"timeout"`
	NetworkMode string            `yaml:"network_mode"`
}

// ExecResult holds the output from a command execution inside the sandbox.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// DockerSandbox wraps the Docker Engine SDK to execute commands in isolated containers.
type DockerSandbox struct {
	client *client.Client
	config SandboxConfig
}

// NewDockerSandbox creates a new DockerSandbox with the given configuration.
// It initializes a Docker client using environment variables (DOCKER_HOST, etc.).
func NewDockerSandbox(config SandboxConfig) (*DockerSandbox, error) {
	if config.Image == "" {
		return nil, fmt.Errorf("sandbox: image is required")
	}

	if config.Timeout == 0 {
		config.Timeout = 10 * time.Minute
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("sandbox: failed to create Docker client: %w", err)
	}

	return &DockerSandbox{
		client: cli,
		config: config,
	}, nil
}

// newDockerSandboxWithClient creates a DockerSandbox with an injected client (for testing).
func newDockerSandboxWithClient(cli *client.Client, config SandboxConfig) *DockerSandbox {
	return &DockerSandbox{
		client: cli,
		config: config,
	}
}

// Exec creates a container, runs the given command, captures output, and removes the container.
func (s *DockerSandbox) Exec(ctx context.Context, cmd []string) (*ExecResult, error) {
	if len(cmd) == 0 {
		return nil, fmt.Errorf("sandbox: command is required")
	}

	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()

	// Pull image if not present locally
	if err := s.ensureImage(ctx); err != nil {
		return nil, fmt.Errorf("sandbox: failed to pull image: %w", err)
	}

	// Build container config
	containerConfig := &container.Config{
		Image:      s.config.Image,
		Cmd:        cmd,
		Env:        s.buildEnv(),
		WorkingDir: s.config.WorkDir,
	}

	hostConfig := s.buildHostConfig()

	// Create container
	resp, err := s.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, "")
	if err != nil {
		return nil, fmt.Errorf("sandbox: failed to create container: %w", err)
	}
	containerID := resp.ID

	// Always remove the container when done
	defer func() {
		removeCtx, removeCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer removeCancel()
		_ = s.client.ContainerRemove(removeCtx, containerID, container.RemoveOptions{Force: true})
	}()

	// Start container
	if err := s.client.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("sandbox: failed to start container: %w", err)
	}

	// Wait for container to finish
	statusCh, errCh := s.client.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	var exitCode int
	select {
	case err := <-errCh:
		if err != nil {
			return nil, fmt.Errorf("sandbox: error waiting for container: %w", err)
		}
	case status := <-statusCh:
		exitCode = int(status.StatusCode)
	case <-ctx.Done():
		// Timeout: stop the container
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = s.client.ContainerStop(stopCtx, containerID, container.StopOptions{})
		return nil, fmt.Errorf("sandbox: execution timed out after %s", s.config.Timeout)
	}

	// Capture stdout/stderr
	stdout, stderr, err := s.getLogs(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("sandbox: failed to capture logs: %w", err)
	}

	return &ExecResult{
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
	}, nil
}

// CopyIn copies a file from the host into a running or created container.
func (s *DockerSandbox) CopyIn(ctx context.Context, srcPath, destPath string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("sandbox: failed to open source file: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("sandbox: failed to stat source file: %w", err)
	}

	// Create a tar archive containing the file
	tarReader, err := createTarFromFile(f, stat)
	if err != nil {
		return fmt.Errorf("sandbox: failed to create tar archive: %w", err)
	}

	// We need a container to copy into. This method is intended to be used
	// with a container that has been created but the caller manages its lifecycle.
	// For the typical use case, the Exec method handles the full lifecycle.
	// This is a lower-level utility for advanced usage.
	_ = destPath
	_ = tarReader
	return fmt.Errorf("sandbox: CopyIn requires an active container ID; use Exec for typical workflows")
}

// CopyOut copies a file out of a container. Returns a ReadCloser with the file contents.
func (s *DockerSandbox) CopyOut(ctx context.Context, srcPath string) (io.ReadCloser, error) {
	// Similar to CopyIn, this requires an active container.
	_ = srcPath
	return nil, fmt.Errorf("sandbox: CopyOut requires an active container ID; use Exec for typical workflows")
}

// ExecInContainer creates a container, copies files in, runs the command, and allows file extraction.
// This is the higher-level API that manages the full container lifecycle with file I/O.
func (s *DockerSandbox) ExecInContainer(ctx context.Context, cmd []string, copyIn map[string]string, copyOutPaths []string) (*ExecResult, map[string]io.ReadCloser, error) {
	if len(cmd) == 0 {
		return nil, nil, fmt.Errorf("sandbox: command is required")
	}

	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()

	if err := s.ensureImage(ctx); err != nil {
		return nil, nil, fmt.Errorf("sandbox: failed to pull image: %w", err)
	}

	containerConfig := &container.Config{
		Image:      s.config.Image,
		Cmd:        cmd,
		Env:        s.buildEnv(),
		WorkingDir: s.config.WorkDir,
	}

	hostConfig := s.buildHostConfig()

	resp, err := s.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, "")
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox: failed to create container: %w", err)
	}
	containerID := resp.ID

	defer func() {
		removeCtx, removeCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer removeCancel()
		_ = s.client.ContainerRemove(removeCtx, containerID, container.RemoveOptions{Force: true})
	}()

	// Copy files into container before starting
	for hostPath, containerPath := range copyIn {
		if err := s.copyToContainer(ctx, containerID, hostPath, containerPath); err != nil {
			return nil, nil, fmt.Errorf("sandbox: failed to copy %s to container: %w", hostPath, err)
		}
	}

	if err := s.client.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return nil, nil, fmt.Errorf("sandbox: failed to start container: %w", err)
	}

	statusCh, errCh := s.client.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	var exitCode int
	select {
	case err := <-errCh:
		if err != nil {
			return nil, nil, fmt.Errorf("sandbox: error waiting for container: %w", err)
		}
	case status := <-statusCh:
		exitCode = int(status.StatusCode)
	case <-ctx.Done():
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = s.client.ContainerStop(stopCtx, containerID, container.StopOptions{})
		return nil, nil, fmt.Errorf("sandbox: execution timed out after %s", s.config.Timeout)
	}

	stdout, stderr, err := s.getLogs(ctx, containerID)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox: failed to capture logs: %w", err)
	}

	// Copy files out of container
	outputs := make(map[string]io.ReadCloser)
	for _, path := range copyOutPaths {
		reader, _, err := s.client.CopyFromContainer(ctx, containerID, path)
		if err != nil {
			// Close any already-opened readers
			for _, r := range outputs {
				r.Close()
			}
			return nil, nil, fmt.Errorf("sandbox: failed to copy %s from container: %w", path, err)
		}
		outputs[path] = reader
	}

	result := &ExecResult{
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
	}

	return result, outputs, nil
}

// Close cleans up the Docker client.
func (s *DockerSandbox) Close() error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// ensureImage pulls the image if it is not available locally.
func (s *DockerSandbox) ensureImage(ctx context.Context) error {
	_, _, err := s.client.ImageInspectWithRaw(ctx, s.config.Image)
	if err == nil {
		return nil // Image already present
	}

	reader, err := s.client.ImagePull(ctx, s.config.Image, image.PullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()

	// Consume the pull output to completion
	_, err = io.Copy(io.Discard, reader)
	return err
}

// buildEnv converts the config env map into Docker's KEY=VALUE format.
func (s *DockerSandbox) buildEnv() []string {
	if len(s.config.Env) == 0 {
		return nil
	}
	env := make([]string, 0, len(s.config.Env))
	for k, v := range s.config.Env {
		env = append(env, k+"="+v)
	}
	return env
}

// buildHostConfig creates the Docker HostConfig from SandboxConfig.
func (s *DockerSandbox) buildHostConfig() *container.HostConfig {
	hc := &container.HostConfig{}

	// Resource limits
	if s.config.MemoryLimit > 0 {
		hc.Resources.Memory = s.config.MemoryLimit
	}
	if s.config.CPULimit > 0 {
		// Docker uses NanoCPUs (1 CPU = 1e9 NanoCPUs)
		hc.Resources.NanoCPUs = int64(s.config.CPULimit * 1e9)
	}

	// Mounts
	if len(s.config.Mounts) > 0 {
		mounts := make([]mount.Mount, len(s.config.Mounts))
		for i, m := range s.config.Mounts {
			mounts[i] = mount.Mount{
				Type:     mount.TypeBind,
				Source:   m.Source,
				Target:   m.Target,
				ReadOnly: m.ReadOnly,
			}
		}
		hc.Mounts = mounts
	}

	// Network mode
	if s.config.NetworkMode != "" {
		hc.NetworkMode = container.NetworkMode(s.config.NetworkMode)
	}

	return hc
}

// getLogs captures stdout and stderr from a container.
func (s *DockerSandbox) getLogs(ctx context.Context, containerID string) (string, string, error) {
	logReader, err := s.client.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		return "", "", err
	}
	defer logReader.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	_, err = stdcopy.StdCopy(&stdoutBuf, &stderrBuf, logReader)
	if err != nil {
		return "", "", err
	}

	return strings.TrimSpace(stdoutBuf.String()), strings.TrimSpace(stderrBuf.String()), nil
}

// copyToContainer copies a host file into the container at destPath.
func (s *DockerSandbox) copyToContainer(ctx context.Context, containerID, hostPath, destPath string) error {
	f, err := os.Open(hostPath)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", hostPath, err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", hostPath, err)
	}

	tarReader, err := createTarFromFile(f, stat)
	if err != nil {
		return err
	}

	return s.client.CopyToContainer(ctx, containerID, destPath, tarReader, container.CopyToContainerOptions{})
}
