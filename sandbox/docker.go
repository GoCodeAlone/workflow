// Package sandbox provides Docker-based sandboxed execution for CI/CD pipeline steps.
package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Mount describes a bind mount from host to container.
type Mount struct {
	Source   string `yaml:"source"`
	Target   string `yaml:"target"`
	ReadOnly bool   `yaml:"read_only"`
}

// SandboxConfig holds configuration for a Docker sandbox execution environment.
type SandboxConfig struct {
	// Profile is the security profile name that produced this config ("strict",
	// "standard", "permissive"). It is informational - it does not affect local
	// Docker execution but is forwarded to remote runners so they can apply their
	// own profile clamping (ADR 0019).
	Profile     string            `yaml:"profile,omitempty"`
	Image       string            `yaml:"image"`
	WorkDir     string            `yaml:"work_dir"`
	Env         map[string]string `yaml:"env"`
	Mounts      []Mount           `yaml:"mounts"`
	MemoryLimit int64             `yaml:"memory_limit"`
	CPULimit    float64           `yaml:"cpu_limit"`
	Timeout     time.Duration     `yaml:"timeout"`
	NetworkMode string            `yaml:"network_mode"`

	// Security hardening fields
	SecurityOpts    []string          `yaml:"security_opts"` // e.g., ["seccomp=default.json"]
	CapAdd          []string          `yaml:"cap_add"`       // capabilities to add
	CapDrop         []string          `yaml:"cap_drop"`      // e.g., ["ALL"]
	ReadOnlyRootfs  bool              `yaml:"read_only_rootfs"`
	NoNewPrivileges bool              `yaml:"no_new_privileges"`
	User            string            `yaml:"user"`       // e.g., "nobody:nogroup"
	PidsLimit       int64             `yaml:"pids_limit"` // max process count
	Tmpfs           map[string]string `yaml:"tmpfs"`      // e.g., {"/tmp": "size=64m,noexec"}
}

// GetProfile returns the profile name stored in the config, falling back to
// "strict" (the safest default) when the field is empty.
func (c SandboxConfig) GetProfile() string {
	if c.Profile != "" {
		return c.Profile
	}
	return "strict"
}

// DefaultSecureSandboxConfig returns a hardened SandboxConfig suitable for
// running untrusted workloads. It uses a minimal Wolfi-based image, drops all
// Linux capabilities, enables a read-only root filesystem, mounts /tmp as
// tmpfs with noexec, and disables network access.
func DefaultSecureSandboxConfig(image string) SandboxConfig {
	if image == "" {
		image = "cgr.dev/chainguard/wolfi-base:latest"
	}
	return SandboxConfig{
		Image:           image,
		MemoryLimit:     256 * 1024 * 1024, // 256MB
		CPULimit:        0.5,
		NetworkMode:     "none",
		CapDrop:         []string{"ALL"},
		NoNewPrivileges: true,
		ReadOnlyRootfs:  true,
		PidsLimit:       64,
		Tmpfs:           map[string]string{"/tmp": "size=64m,noexec"},
		Timeout:         5 * time.Minute,
	}
}

// ExecResult holds the output from a command execution inside the sandbox.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

type hostConfig struct {
	Memory         int64
	NanoCPUs       int64
	Mounts         []hostMount
	NetworkMode    string
	SecurityOpt    []string
	CapAdd         []string
	CapDrop        []string
	ReadonlyRootfs bool
	PidsLimit      *int64
	Tmpfs          map[string]string
}

type hostMount struct {
	Source   string
	Target   string
	ReadOnly bool
}

// DockerSandbox wraps the Docker CLI to execute commands in isolated containers.
type DockerSandbox struct {
	config      SandboxConfig
	containerID string // set by CreateContainer, used by CopyIn/CopyOut/RemoveContainer
}

// NewDockerSandbox creates a new DockerSandbox with the given configuration.
// It requires the Docker CLI to be present on PATH.
func NewDockerSandbox(config SandboxConfig) (*DockerSandbox, error) {
	if config.Image == "" {
		return nil, fmt.Errorf("sandbox: image is required")
	}

	if config.Timeout == 0 {
		config.Timeout = 10 * time.Minute
	}

	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("sandbox: docker CLI not found: %w", err)
	}

	return &DockerSandbox{config: config}, nil
}

// Exec creates a container, runs the given command, captures output, and removes the container.
func (s *DockerSandbox) Exec(ctx context.Context, cmd []string) (*ExecResult, error) {
	if len(cmd) == 0 {
		return nil, fmt.Errorf("sandbox: command is required")
	}

	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()

	if err := s.ensureImage(ctx); err != nil {
		return nil, fmt.Errorf("sandbox: failed to pull image: %w", err)
	}

	args := append([]string{"run", "--rm"}, s.dockerRunArgs()...)
	args = append(args, s.config.Image)
	args = append(args, cmd...)
	return runDockerResult(ctx, args)
}

// CopyIn copies a file from the host into the active container.
// Call CreateContainer first to set the active container ID.
func (s *DockerSandbox) CopyIn(ctx context.Context, srcPath, destPath string) error {
	if s.containerID == "" {
		return fmt.Errorf("sandbox: CopyIn requires an active container; call CreateContainer first")
	}
	return s.copyToContainer(ctx, s.containerID, srcPath, destPath)
}

// CopyOut copies a file out of the active container. Returns a ReadCloser with
// a tar archive containing the file contents, matching Docker copy semantics.
// Call CreateContainer first to set the active container ID.
func (s *DockerSandbox) CopyOut(ctx context.Context, srcPath string) (io.ReadCloser, error) {
	if s.containerID == "" {
		return nil, fmt.Errorf("sandbox: CopyOut requires an active container; call CreateContainer first")
	}
	return s.copyFromContainer(ctx, s.containerID, srcPath)
}

// CreateContainer creates a container, storing its ID for use with CopyIn/CopyOut.
// Call RemoveContainer when done to clean up.
func (s *DockerSandbox) CreateContainer(ctx context.Context, cmd []string) error {
	args := append([]string{"create"}, s.dockerRunArgs()...)
	args = append(args, s.config.Image)
	args = append(args, cmd...)

	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput() // #nosec G204 - sandbox intentionally maps config into Docker CLI args.
	if err != nil {
		return fmt.Errorf("sandbox: create container: %w: %s", err, strings.TrimSpace(string(out)))
	}
	s.containerID = strings.TrimSpace(string(out))
	return nil
}

// RemoveContainer stops and removes the active container.
func (s *DockerSandbox) RemoveContainer(ctx context.Context) error {
	if s.containerID == "" {
		return nil
	}
	id := s.containerID
	s.containerID = ""
	return exec.CommandContext(ctx, "docker", "rm", "-f", id).Run() // #nosec G204 - container ID comes from Docker create output.
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

	args := append([]string{"create"}, s.dockerRunArgs()...)
	args = append(args, s.config.Image)
	args = append(args, cmd...)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput() // #nosec G204 - sandbox intentionally maps config into Docker CLI args.
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox: failed to create container: %w: %s", err, strings.TrimSpace(string(out)))
	}
	containerID := strings.TrimSpace(string(out))

	defer func() {
		removeCtx, removeCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer removeCancel()
		_ = exec.CommandContext(removeCtx, "docker", "rm", "-f", containerID).Run() // #nosec G204 - container ID comes from Docker create output.
	}()

	for hostPath, containerPath := range copyIn {
		if err := s.copyToContainer(ctx, containerID, hostPath, containerPath); err != nil {
			return nil, nil, fmt.Errorf("sandbox: failed to copy %s to container: %w", hostPath, err)
		}
	}

	result, err := runDockerResult(ctx, []string{"start", "-a", containerID})
	if err != nil {
		return nil, nil, err
	}

	outputs := make(map[string]io.ReadCloser)
	for _, path := range copyOutPaths {
		reader, err := s.copyFromContainer(ctx, containerID, path)
		if err != nil {
			for _, r := range outputs {
				_ = r.Close()
			}
			return nil, nil, fmt.Errorf("sandbox: failed to copy %s from container: %w", path, err)
		}
		outputs[path] = reader
	}

	return result, outputs, nil
}

// Close cleans up resources held by the sandbox.
func (s *DockerSandbox) Close() error {
	return nil
}

// ensureImage pulls the image if it is not available locally.
func (s *DockerSandbox) ensureImage(ctx context.Context) error {
	if err := exec.CommandContext(ctx, "docker", "image", "inspect", s.config.Image).Run(); err == nil { // #nosec G204 - sandbox image is an explicit execution input.
		return nil
	}
	cmd := exec.CommandContext(ctx, "docker", "pull", s.config.Image) // #nosec G204 - sandbox image is an explicit execution input.
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
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

// buildHostConfig creates an internal HostConfig from SandboxConfig.
func (s *DockerSandbox) buildHostConfig() *hostConfig {
	hc := &hostConfig{}

	if s.config.MemoryLimit > 0 {
		hc.Memory = s.config.MemoryLimit
	}
	if s.config.CPULimit > 0 {
		hc.NanoCPUs = int64(s.config.CPULimit * 1e9)
	}
	if s.config.PidsLimit > 0 {
		limit := s.config.PidsLimit
		hc.PidsLimit = &limit
	}

	if len(s.config.Mounts) > 0 {
		hc.Mounts = make([]hostMount, len(s.config.Mounts))
		for i, m := range s.config.Mounts {
			hc.Mounts[i] = hostMount(m)
		}
	}

	if s.config.NetworkMode != "" {
		hc.NetworkMode = s.config.NetworkMode
	}

	secOpts := make([]string, len(s.config.SecurityOpts))
	copy(secOpts, s.config.SecurityOpts)
	if s.config.NoNewPrivileges {
		secOpts = append(secOpts, "no-new-privileges:true")
	}
	if len(secOpts) > 0 {
		hc.SecurityOpt = secOpts
	}

	if len(s.config.CapAdd) > 0 {
		hc.CapAdd = s.config.CapAdd
	}
	if len(s.config.CapDrop) > 0 {
		hc.CapDrop = s.config.CapDrop
	}

	hc.ReadonlyRootfs = s.config.ReadOnlyRootfs
	if len(s.config.Tmpfs) > 0 {
		hc.Tmpfs = s.config.Tmpfs
	}

	return hc
}

func (s *DockerSandbox) dockerRunArgs() []string {
	hc := s.buildHostConfig()
	args := make([]string, 0, 32)
	if s.config.WorkDir != "" {
		args = append(args, "-w", s.config.WorkDir)
	}
	if s.config.User != "" {
		args = append(args, "-u", s.config.User)
	}
	for _, env := range s.buildEnv() {
		args = append(args, "-e", env)
	}
	for _, mount := range hc.Mounts {
		spec := "type=bind,src=" + mount.Source + ",dst=" + mount.Target
		if mount.ReadOnly {
			spec += ",readonly"
		}
		args = append(args, "--mount", spec)
	}
	if hc.Memory > 0 {
		args = append(args, "--memory", strconv.FormatInt(hc.Memory, 10))
	}
	if hc.NanoCPUs > 0 {
		args = append(args, "--cpus", strconv.FormatFloat(float64(hc.NanoCPUs)/1e9, 'f', -1, 64))
	}
	if hc.PidsLimit != nil {
		args = append(args, "--pids-limit", strconv.FormatInt(*hc.PidsLimit, 10))
	}
	if hc.NetworkMode != "" {
		args = append(args, "--network", hc.NetworkMode)
	}
	for _, opt := range hc.SecurityOpt {
		args = append(args, "--security-opt", opt)
	}
	for _, cap := range hc.CapAdd {
		args = append(args, "--cap-add", cap)
	}
	for _, cap := range hc.CapDrop {
		args = append(args, "--cap-drop", cap)
	}
	if hc.ReadonlyRootfs {
		args = append(args, "--read-only")
	}
	for path, spec := range hc.Tmpfs {
		args = append(args, "--tmpfs", path+":"+spec)
	}
	return args
}

func runDockerResult(ctx context.Context, args []string) (*ExecResult, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", args...) // #nosec G204 - sandbox intentionally maps config into Docker CLI args.
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("sandbox: docker command failed: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
	}

	return &ExecResult{
		ExitCode: exitCode,
		Stdout:   strings.TrimSpace(stdout.String()),
		Stderr:   strings.TrimSpace(stderr.String()),
	}, nil
}

func (s *DockerSandbox) copyFromContainer(ctx context.Context, containerID, srcPath string) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, "docker", "cp", containerID+":"+srcPath, "-") // #nosec G204 - container ID comes from Docker create output; path is caller-selected copy target.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}

	pr, pw := io.Pipe()
	go func() {
		_, copyErr := io.Copy(pw, stdout)
		waitErr := cmd.Wait()
		switch {
		case copyErr != nil:
			_ = pw.CloseWithError(copyErr)
		case waitErr != nil:
			msg := strings.TrimSpace(stderr.String())
			if msg != "" {
				_ = pw.CloseWithError(fmt.Errorf("%w: %s", waitErr, msg))
			} else {
				_ = pw.CloseWithError(waitErr)
			}
		default:
			_ = pw.Close()
		}
	}()

	return pr, nil
}

func (s *DockerSandbox) copyToContainer(ctx context.Context, containerID, hostPath, destPath string) error {
	if _, err := os.Stat(hostPath); err != nil {
		return fmt.Errorf("failed to stat %s: %w", hostPath, err)
	}
	out, err := exec.CommandContext(ctx, "docker", "cp", hostPath, containerID+":"+destPath).CombinedOutput() // #nosec G204 - caller-selected copy paths are the sandbox API contract.
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}
