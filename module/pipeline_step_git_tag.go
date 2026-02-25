package module

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// GitTagStep creates and optionally pushes a git tag.
type GitTagStep struct {
	name        string
	directory   string
	tag         string
	message     string
	push        bool
	token       string
	tmpl        *TemplateEngine
	execCommand func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// NewGitTagStepFactory returns a StepFactory that creates GitTagStep instances.
func NewGitTagStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		directory, _ := config["directory"].(string)
		if directory == "" {
			return nil, fmt.Errorf("git_tag step %q: 'directory' is required", name)
		}

		tag, _ := config["tag"].(string)
		if tag == "" {
			return nil, fmt.Errorf("git_tag step %q: 'tag' is required", name)
		}

		message, _ := config["message"].(string)
		push, _ := config["push"].(bool)
		token, _ := config["token"].(string)

		return &GitTagStep{
			name:        name,
			directory:   directory,
			tag:         tag,
			message:     message,
			push:        push,
			token:       token,
			tmpl:        NewTemplateEngine(),
			execCommand: exec.CommandContext,
		}, nil
	}
}

// Name returns the step name.
func (s *GitTagStep) Name() string { return s.name }

// Execute creates the tag and optionally pushes it.
func (s *GitTagStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	directory, err := s.tmpl.Resolve(s.directory, pc)
	if err != nil {
		return nil, fmt.Errorf("git_tag step %q: failed to resolve directory: %w", s.name, err)
	}

	tag, err := s.tmpl.Resolve(s.tag, pc)
	if err != nil {
		return nil, fmt.Errorf("git_tag step %q: failed to resolve tag: %w", s.name, err)
	}

	message, err := s.tmpl.Resolve(s.message, pc)
	if err != nil {
		return nil, fmt.Errorf("git_tag step %q: failed to resolve message: %w", s.name, err)
	}

	token, err := s.tmpl.Resolve(s.token, pc)
	if err != nil {
		return nil, fmt.Errorf("git_tag step %q: failed to resolve token: %w", s.name, err)
	}

	// Build tag args.
	tagArgs := []string{"-C", directory, "tag"}
	if message != "" {
		// Annotated tag.
		tagArgs = append(tagArgs, "-a", tag, "-m", message)
	} else {
		// Lightweight tag.
		tagArgs = append(tagArgs, tag)
	}

	var stdout, stderr bytes.Buffer
	cmd := s.execCommand(ctx, "git", tagArgs...) //nolint:gosec // G204: args from trusted pipeline config
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git_tag step %q: git tag failed: %w\nstdout: %s\nstderr: %s",
			s.name, err, stdout.String(), stderr.String())
	}

	// Get the commit SHA for the tag.
	commitSHA, err := s.getTagCommitSHA(ctx, directory, tag)
	if err != nil {
		commitSHA = ""
	}

	pushed := false
	if s.push {
		if err := s.pushTag(ctx, directory, tag, token); err != nil {
			return nil, fmt.Errorf("git_tag step %q: failed to push tag: %w", s.name, err)
		}
		pushed = true
	}

	return &StepResult{
		Output: map[string]any{
			"tag":        tag,
			"commit_sha": commitSHA,
			"pushed":     pushed,
			"success":    true,
		},
	}, nil
}

// pushTag pushes the tag to the remote origin.
func (s *GitTagStep) pushTag(ctx context.Context, dir, tag, token string) error {
	// If token is provided, inject into remote URL first.
	if token != "" {
		if err := s.injectTokenIntoRemote(ctx, dir, "origin", token); err != nil {
			return fmt.Errorf("failed to inject token: %w", err)
		}
	}

	var stdout, stderr bytes.Buffer
	cmd := s.execCommand(ctx, "git", "-C", dir, "push", "origin", tag) //nolint:gosec // G204: args from trusted pipeline config
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git push tag failed: %w\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	return nil
}

// injectTokenIntoRemote rewrites the origin remote URL to embed the token.
func (s *GitTagStep) injectTokenIntoRemote(ctx context.Context, dir, remote, token string) error {
	var stdout bytes.Buffer
	cmd := s.execCommand(ctx, "git", "-C", dir, "remote", "get-url", remote) //nolint:gosec // G204: args from trusted pipeline config
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to get remote URL: %w", err)
	}
	remoteURL := strings.TrimSpace(stdout.String())

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

// getTagCommitSHA returns the commit SHA that the tag points to.
func (s *GitTagStep) getTagCommitSHA(ctx context.Context, dir, tag string) (string, error) {
	var stdout bytes.Buffer
	cmd := s.execCommand(ctx, "git", "-C", dir, "rev-list", "-n", "1", tag) //nolint:gosec // G204: args from trusted pipeline config
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}
