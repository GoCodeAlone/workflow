package module

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// GitPushStep pushes commits to a remote repository.
type GitPushStep struct {
	name        string
	directory   string
	remote      string
	branch      string
	force       bool
	tags        bool
	token       string
	tmpl        *TemplateEngine
	execCommand func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// NewGitPushStepFactory returns a StepFactory that creates GitPushStep instances.
func NewGitPushStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		directory, _ := config["directory"].(string)
		if directory == "" {
			return nil, fmt.Errorf("git_push step %q: 'directory' is required", name)
		}

		remote, _ := config["remote"].(string)
		if remote == "" {
			remote = "origin"
		}

		branch, _ := config["branch"].(string)
		force, _ := config["force"].(bool)
		tags, _ := config["tags"].(bool)
		token, _ := config["token"].(string)

		return &GitPushStep{
			name:        name,
			directory:   directory,
			remote:      remote,
			branch:      branch,
			force:       force,
			tags:        tags,
			token:       token,
			tmpl:        NewTemplateEngine(),
			execCommand: exec.CommandContext,
		}, nil
	}
}

// Name returns the step name.
func (s *GitPushStep) Name() string { return s.name }

// Execute pushes commits to the configured remote.
func (s *GitPushStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	directory, err := s.tmpl.Resolve(s.directory, pc)
	if err != nil {
		return nil, fmt.Errorf("git_push step %q: failed to resolve directory: %w", s.name, err)
	}

	remote, err := s.tmpl.Resolve(s.remote, pc)
	if err != nil {
		return nil, fmt.Errorf("git_push step %q: failed to resolve remote: %w", s.name, err)
	}

	branch, err := s.tmpl.Resolve(s.branch, pc)
	if err != nil {
		return nil, fmt.Errorf("git_push step %q: failed to resolve branch: %w", s.name, err)
	}

	token, err := s.tmpl.Resolve(s.token, pc)
	if err != nil {
		return nil, fmt.Errorf("git_push step %q: failed to resolve token: %w", s.name, err)
	}

	// If a token is provided, rewrite the remote URL to embed the token.
	if token != "" {
		if err := s.injectTokenIntoRemote(ctx, directory, remote, token); err != nil {
			return nil, fmt.Errorf("git_push step %q: failed to inject token into remote: %w", s.name, err)
		}
	}

	// Resolve the current branch if not specified.
	resolvedBranch := branch
	if resolvedBranch == "" {
		resolvedBranch, err = s.getCurrentBranch(ctx, directory)
		if err != nil {
			return nil, fmt.Errorf("git_push step %q: failed to get current branch: %w", s.name, err)
		}
	}

	// Build push args.
	args := []string{"-C", directory, "push", remote, resolvedBranch}
	if s.force {
		args = append(args, "--force")
	}
	if s.tags {
		args = append(args, "--tags")
	}

	var stdout, stderr bytes.Buffer
	cmd := s.execCommand(ctx, "git", args...) //nolint:gosec // G204: args from trusted pipeline config
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git_push step %q: git push failed: %w\nstdout: %s\nstderr: %s",
			s.name, err, stdout.String(), stderr.String())
	}

	return &StepResult{
		Output: map[string]any{
			"remote":  remote,
			"branch":  resolvedBranch,
			"success": true,
		},
	}, nil
}

// injectTokenIntoRemote rewrites the remote URL to embed the token.
func (s *GitPushStep) injectTokenIntoRemote(ctx context.Context, dir, remote, token string) error {
	// Get current remote URL.
	var stdout bytes.Buffer
	cmd := s.execCommand(ctx, "git", "-C", dir, "remote", "get-url", remote) //nolint:gosec // G204: args from trusted pipeline config
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to get remote URL: %w", err)
	}
	remoteURL := strings.TrimSpace(stdout.String())

	// Inject token into HTTPS URL.
	if strings.HasPrefix(remoteURL, "https://") {
		newURL := strings.Replace(remoteURL, "https://", "https://"+token+"@", 1)
		var stderr bytes.Buffer
		setCmd := s.execCommand(ctx, "git", "-C", dir, "remote", "set-url", remote, newURL) //nolint:gosec // G204: args from trusted pipeline config
		setCmd.Stderr = &stderr
		if err := setCmd.Run(); err != nil {
			return fmt.Errorf("failed to set remote URL: %w\nstderr: %s", err, stderr.String())
		}
	}
	return nil
}

// getCurrentBranch returns the current branch name for the given directory.
func (s *GitPushStep) getCurrentBranch(ctx context.Context, dir string) (string, error) {
	var stdout bytes.Buffer
	cmd := s.execCommand(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD") //nolint:gosec // G204: args from trusted pipeline config
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}
