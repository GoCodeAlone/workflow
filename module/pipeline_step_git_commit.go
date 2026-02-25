package module

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// GitCommitStep creates a git commit in a local repository.
type GitCommitStep struct {
	name        string
	directory   string
	message     string
	authorName  string
	authorEmail string
	addAll      bool
	addFiles    []string
	tmpl        *TemplateEngine
	execCommand func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// NewGitCommitStepFactory returns a StepFactory that creates GitCommitStep instances.
func NewGitCommitStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		directory, _ := config["directory"].(string)
		if directory == "" {
			return nil, fmt.Errorf("git_commit step %q: 'directory' is required", name)
		}

		message, _ := config["message"].(string)
		if message == "" {
			return nil, fmt.Errorf("git_commit step %q: 'message' is required", name)
		}

		authorName, _ := config["author_name"].(string)
		authorEmail, _ := config["author_email"].(string)
		addAll, _ := config["add_all"].(bool)

		var addFiles []string
		if filesRaw, ok := config["add_files"].([]any); ok {
			for i, f := range filesRaw {
				s, ok := f.(string)
				if !ok {
					return nil, fmt.Errorf("git_commit step %q: add_files[%d] must be a string", name, i)
				}
				addFiles = append(addFiles, s)
			}
		}

		return &GitCommitStep{
			name:        name,
			directory:   directory,
			message:     message,
			authorName:  authorName,
			authorEmail: authorEmail,
			addAll:      addAll,
			addFiles:    addFiles,
			tmpl:        NewTemplateEngine(),
			execCommand: exec.CommandContext,
		}, nil
	}
}

// Name returns the step name.
func (s *GitCommitStep) Name() string { return s.name }

// Execute stages files and creates a commit.
func (s *GitCommitStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	directory, err := s.tmpl.Resolve(s.directory, pc)
	if err != nil {
		return nil, fmt.Errorf("git_commit step %q: failed to resolve directory: %w", s.name, err)
	}

	message, err := s.tmpl.Resolve(s.message, pc)
	if err != nil {
		return nil, fmt.Errorf("git_commit step %q: failed to resolve message: %w", s.name, err)
	}

	// Stage files.
	if s.addAll {
		if err := s.runGit(ctx, directory, "add", "-A"); err != nil {
			return nil, fmt.Errorf("git_commit step %q: git add -A failed: %w", s.name, err)
		}
	} else if len(s.addFiles) > 0 {
		addArgs := append([]string{"add", "--"}, s.addFiles...)
		if err := s.runGit(ctx, directory, addArgs...); err != nil {
			return nil, fmt.Errorf("git_commit step %q: git add failed: %w", s.name, err)
		}
	}

	// Build commit args.
	commitArgs := []string{"commit", "-m", message}
	if s.authorName != "" && s.authorEmail != "" {
		commitArgs = append(commitArgs, "--author", fmt.Sprintf("%s <%s>", s.authorName, s.authorEmail))
	}

	// Run commit.
	var stdout, stderr bytes.Buffer
	cmd := s.execCommand(ctx, "git", append([]string{"-C", directory}, commitArgs...)...) //nolint:gosec // G204: args from trusted pipeline config
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Check if "nothing to commit" — not an error.
		combined := stdout.String() + stderr.String()
		if strings.Contains(combined, "nothing to commit") ||
			strings.Contains(combined, "nothing added to commit") {
			return &StepResult{
				Output: map[string]any{
					"commit_sha":    "",
					"message":       message,
					"files_changed": 0,
					"success":       true,
				},
			}, nil
		}
		return nil, fmt.Errorf("git_commit step %q: git commit failed: %w\nstdout: %s\nstderr: %s",
			s.name, err, stdout.String(), stderr.String())
	}

	// Parse commit SHA.
	commitSHA, err := s.getCommitSHA(ctx, directory)
	if err != nil {
		commitSHA = ""
	}

	// Count files changed from commit output.
	filesChanged := parseFilesChanged(stdout.String())

	return &StepResult{
		Output: map[string]any{
			"commit_sha":    commitSHA,
			"message":       message,
			"files_changed": filesChanged,
			"success":       true,
		},
	}, nil
}

// runGit runs a git subcommand in the given directory.
func (s *GitCommitStep) runGit(ctx context.Context, dir string, args ...string) error {
	fullArgs := append([]string{"-C", dir}, args...)
	var stdout, stderr bytes.Buffer
	cmd := s.execCommand(ctx, "git", fullArgs...) //nolint:gosec // G204: args from trusted pipeline config
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	return nil
}

// getCommitSHA returns the HEAD commit SHA for the given directory.
func (s *GitCommitStep) getCommitSHA(ctx context.Context, dir string) (string, error) {
	var stdout bytes.Buffer
	cmd := s.execCommand(ctx, "git", "-C", dir, "rev-parse", "HEAD") //nolint:gosec // G204: args from trusted pipeline config
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

// parseFilesChanged extracts the number of files changed from git commit output.
// e.g. " 3 files changed, 10 insertions(+)"
func parseFilesChanged(output string) int {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "file") && strings.Contains(line, "changed") {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				if n, err := strconv.Atoi(fields[0]); err == nil {
					return n
				}
			}
		}
	}
	return 0
}
