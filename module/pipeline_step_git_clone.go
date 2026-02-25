package module

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// GitCloneStep clones a git repository to a local directory.
type GitCloneStep struct {
	name        string
	repository  string
	branch      string
	depth       int
	directory   string
	token       string
	sshKey      string
	tmpl        *TemplateEngine
	execCommand func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// NewGitCloneStepFactory returns a StepFactory that creates GitCloneStep instances.
func NewGitCloneStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		repository, _ := config["repository"].(string)
		if repository == "" {
			return nil, fmt.Errorf("git_clone step %q: 'repository' is required", name)
		}

		directory, _ := config["directory"].(string)
		if directory == "" {
			return nil, fmt.Errorf("git_clone step %q: 'directory' is required", name)
		}

		branch, _ := config["branch"].(string)
		token, _ := config["token"].(string)
		sshKey, _ := config["ssh_key"].(string)

		depth := 0
		if d, ok := config["depth"].(int); ok {
			depth = d
		}

		return &GitCloneStep{
			name:        name,
			repository:  repository,
			branch:      branch,
			depth:       depth,
			directory:   directory,
			token:       token,
			sshKey:      sshKey,
			tmpl:        NewTemplateEngine(),
			execCommand: exec.CommandContext,
		}, nil
	}
}

// Name returns the step name.
func (s *GitCloneStep) Name() string { return s.name }

// Execute clones the repository to the configured directory.
func (s *GitCloneStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	repository, err := s.tmpl.Resolve(s.repository, pc)
	if err != nil {
		return nil, fmt.Errorf("git_clone step %q: failed to resolve repository: %w", s.name, err)
	}

	directory, err := s.tmpl.Resolve(s.directory, pc)
	if err != nil {
		return nil, fmt.Errorf("git_clone step %q: failed to resolve directory: %w", s.name, err)
	}

	branch, err := s.tmpl.Resolve(s.branch, pc)
	if err != nil {
		return nil, fmt.Errorf("git_clone step %q: failed to resolve branch: %w", s.name, err)
	}

	token, err := s.tmpl.Resolve(s.token, pc)
	if err != nil {
		return nil, fmt.Errorf("git_clone step %q: failed to resolve token: %w", s.name, err)
	}

	sshKey, err := s.tmpl.Resolve(s.sshKey, pc)
	if err != nil {
		return nil, fmt.Errorf("git_clone step %q: failed to resolve ssh_key: %w", s.name, err)
	}

	// Inject token into HTTPS URL if provided.
	cloneURL := repository
	if token != "" && strings.HasPrefix(repository, "https://") {
		cloneURL = strings.Replace(repository, "https://", "https://"+token+"@", 1)
	}

	// Build git clone args.
	args := []string{"clone"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	if s.depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", s.depth))
	}
	args = append(args, cloneURL, directory)

	var stdout, stderr bytes.Buffer
	cmd := s.execCommand(ctx, "git", args...) //nolint:gosec // G204: args from trusted pipeline config
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set up SSH key if provided.
	if sshKey != "" {
		keyFile, err := os.CreateTemp("", "git-ssh-key-*")
		if err != nil {
			return nil, fmt.Errorf("git_clone step %q: failed to create SSH key temp file: %w", s.name, err)
		}
		defer os.Remove(keyFile.Name())

		if _, err := keyFile.WriteString(sshKey); err != nil {
			keyFile.Close()
			return nil, fmt.Errorf("git_clone step %q: failed to write SSH key: %w", s.name, err)
		}
		keyFile.Close()

		if err := os.Chmod(keyFile.Name(), 0600); err != nil {
			return nil, fmt.Errorf("git_clone step %q: failed to chmod SSH key: %w", s.name, err)
		}

		cmd.Env = append(os.Environ(),
			fmt.Sprintf("GIT_SSH_COMMAND=ssh -i %s -o StrictHostKeyChecking=no", keyFile.Name()),
		)
	}

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git_clone step %q: git clone failed: %w\nstdout: %s\nstderr: %s",
			s.name, err, stdout.String(), stderr.String())
	}

	// Get commit SHA and branch from cloned repo.
	commitSHA, err := s.getCommitSHA(ctx, directory)
	if err != nil {
		// Non-fatal: return success without SHA.
		commitSHA = ""
	}

	resolvedBranch := branch
	if resolvedBranch == "" {
		resolvedBranch, _ = s.getCurrentBranch(ctx, directory)
	}

	return &StepResult{
		Output: map[string]any{
			"clone_dir":  directory,
			"commit_sha": commitSHA,
			"branch":     resolvedBranch,
			"success":    true,
		},
	}, nil
}

// getCommitSHA returns the HEAD commit SHA in the given directory.
func (s *GitCloneStep) getCommitSHA(ctx context.Context, dir string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := s.execCommand(ctx, "git", "-C", dir, "rev-parse", "HEAD")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("rev-parse HEAD failed: %w\nstderr: %s", err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// getCurrentBranch returns the current branch name in the given directory.
func (s *GitCloneStep) getCurrentBranch(ctx context.Context, dir string) (string, error) {
	var stdout bytes.Buffer
	cmd := s.execCommand(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}
