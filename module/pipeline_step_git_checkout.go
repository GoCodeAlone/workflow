package module

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// GitCheckoutStep checks out a branch, tag, or creates a new branch.
type GitCheckoutStep struct {
	name        string
	directory   string
	branch      string
	create      bool
	tmpl        *TemplateEngine
	execCommand func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// NewGitCheckoutStepFactory returns a StepFactory that creates GitCheckoutStep instances.
func NewGitCheckoutStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		directory, _ := config["directory"].(string)
		if directory == "" {
			return nil, fmt.Errorf("git_checkout step %q: 'directory' is required", name)
		}

		branch, _ := config["branch"].(string)
		if branch == "" {
			return nil, fmt.Errorf("git_checkout step %q: 'branch' is required", name)
		}

		create, _ := config["create"].(bool)

		return &GitCheckoutStep{
			name:        name,
			directory:   directory,
			branch:      branch,
			create:      create,
			tmpl:        NewTemplateEngine(),
			execCommand: exec.CommandContext,
		}, nil
	}
}

// Name returns the step name.
func (s *GitCheckoutStep) Name() string { return s.name }

// Execute checks out the configured branch or creates it.
func (s *GitCheckoutStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	directory, err := s.tmpl.Resolve(s.directory, pc)
	if err != nil {
		return nil, fmt.Errorf("git_checkout step %q: failed to resolve directory: %w", s.name, err)
	}

	branch, err := s.tmpl.Resolve(s.branch, pc)
	if err != nil {
		return nil, fmt.Errorf("git_checkout step %q: failed to resolve branch: %w", s.name, err)
	}

	// Build checkout args.
	args := []string{"-C", directory, "checkout"}
	if s.create {
		args = append(args, "-b")
	}
	args = append(args, branch)

	var stdout, stderr bytes.Buffer
	cmd := s.execCommand(ctx, "git", args...) //nolint:gosec // G204: args from trusted pipeline config
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git_checkout step %q: git checkout failed: %w\nstdout: %s\nstderr: %s",
			s.name, err, stdout.String(), stderr.String())
	}

	// Get commit SHA after checkout.
	commitSHA, err := s.getCommitSHA(ctx, directory)
	if err != nil {
		commitSHA = ""
	}

	return &StepResult{
		Output: map[string]any{
			"branch":     branch,
			"commit_sha": commitSHA,
			"created":    s.create,
			"success":    true,
		},
	}, nil
}

// getCommitSHA returns the HEAD commit SHA for the given directory.
func (s *GitCheckoutStep) getCommitSHA(ctx context.Context, dir string) (string, error) {
	var stdout bytes.Buffer
	cmd := s.execCommand(ctx, "git", "-C", dir, "rev-parse", "HEAD") //nolint:gosec // G204: args from trusted pipeline config
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}
