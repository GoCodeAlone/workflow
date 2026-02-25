package module

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestGitCommitStep_FactoryRequiresDirectory(t *testing.T) {
	factory := NewGitCommitStepFactory()
	_, err := factory("commit", map[string]any{"message": "update config"}, nil)
	if err == nil {
		t.Fatal("expected error when directory is missing")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("expected error to mention directory, got: %v", err)
	}
}

func TestGitCommitStep_FactoryRequiresMessage(t *testing.T) {
	factory := NewGitCommitStepFactory()
	_, err := factory("commit", map[string]any{"directory": "/tmp/repo"}, nil)
	if err == nil {
		t.Fatal("expected error when message is missing")
	}
	if !strings.Contains(err.Error(), "message") {
		t.Errorf("expected error to mention message, got: %v", err)
	}
}

func TestGitCommitStep_FactoryAddFilesInvalidEntry(t *testing.T) {
	factory := NewGitCommitStepFactory()
	_, err := factory("commit", map[string]any{
		"directory": "/tmp/repo",
		"message":   "update config",
		"add_files": []any{42},
	}, nil)
	if err == nil {
		t.Fatal("expected error for non-string add_files entry")
	}
}

func TestGitCommitStep_Name(t *testing.T) {
	factory := NewGitCommitStepFactory()
	step, err := factory("my-commit", map[string]any{
		"directory": "/tmp/repo",
		"message":   "update config",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}
	if step.Name() != "my-commit" {
		t.Errorf("expected name %q, got %q", "my-commit", step.Name())
	}
}

func TestGitCommitStep_Execute_AddAllAndCommit(t *testing.T) {
	var gitCalls [][]string

	s := &GitCommitStep{
		name:      "commit",
		directory: "/tmp/repo",
		message:   "update config",
		addAll:    true,
		tmpl:      NewTemplateEngine(),
		execCommand: func(_ context.Context, name string, args ...string) *exec.Cmd {
			gitCalls = append(gitCalls, append([]string{name}, args...))
			// First call: git add -A — succeed.
			// Second call: git commit — return output with "1 file changed".
			// Third call: git rev-parse HEAD.
			switch len(gitCalls) {
			case 1:
				return exec.Command("true") // git add -A
			case 2:
				return exec.Command("echo", "1 file changed, 5 insertions(+)") // git commit
			default:
				return exec.Command("echo", "abc123def456") // rev-parse HEAD
			}
		},
	}

	result, err := s.Execute(context.Background(), &PipelineContext{
		Current:     map[string]any{},
		StepOutputs: map[string]map[string]any{},
		TriggerData: map[string]any{},
		Metadata:    map[string]any{},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Output["success"] != true {
		t.Errorf("expected success=true, got %v", result.Output["success"])
	}

	// Verify git add -A was called.
	foundAddAll := false
	for _, call := range gitCalls {
		argsStr := strings.Join(call, " ")
		if strings.Contains(argsStr, "add") && strings.Contains(argsStr, "-A") {
			foundAddAll = true
			break
		}
	}
	if !foundAddAll {
		t.Errorf("expected git add -A call, got: %v", gitCalls)
	}
}

func TestGitCommitStep_Execute_AddSpecificFiles(t *testing.T) {
	var gitCalls [][]string

	s := &GitCommitStep{
		name:      "commit",
		directory: "/tmp/repo",
		message:   "update config",
		addAll:    false,
		addFiles:  []string{"config/app.yaml", "generated/"},
		tmpl:      NewTemplateEngine(),
		execCommand: func(_ context.Context, name string, args ...string) *exec.Cmd {
			gitCalls = append(gitCalls, append([]string{name}, args...))
			switch len(gitCalls) {
			case 1:
				return exec.Command("true") // git add -- files
			case 2:
				return exec.Command("echo", "2 files changed") // git commit
			default:
				return exec.Command("echo", "abc123") // rev-parse HEAD
			}
		},
	}

	_, err := s.Execute(context.Background(), &PipelineContext{
		Current:     map[string]any{},
		StepOutputs: map[string]map[string]any{},
		TriggerData: map[string]any{},
		Metadata:    map[string]any{},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify specific files were passed to git add.
	foundSpecificAdd := false
	for _, call := range gitCalls {
		argsStr := strings.Join(call, " ")
		if strings.Contains(argsStr, "add") && strings.Contains(argsStr, "config/app.yaml") {
			foundSpecificAdd = true
			break
		}
	}
	if !foundSpecificAdd {
		t.Errorf("expected git add with specific files, got: %v", gitCalls)
	}
}

func TestGitCommitStep_Execute_NothingToCommit(t *testing.T) {
	callCount := 0

	s := &GitCommitStep{
		name:      "commit",
		directory: "/tmp/repo",
		message:   "update config",
		addAll:    true,
		tmpl:      NewTemplateEngine(),
		execCommand: func(_ context.Context, _ string, _ ...string) *exec.Cmd {
			callCount++
			if callCount == 1 {
				return exec.Command("true") // git add -A
			}
			// git commit outputs "nothing to commit" and exits non-zero.
			return exec.Command("bash", "-c", "echo 'nothing to commit, working tree clean'; exit 1")
		},
	}

	result, err := s.Execute(context.Background(), &PipelineContext{
		Current:     map[string]any{},
		StepOutputs: map[string]map[string]any{},
		TriggerData: map[string]any{},
		Metadata:    map[string]any{},
	})
	if err != nil {
		t.Fatalf("expected no error for nothing to commit, got: %v", err)
	}
	if result.Output["success"] != true {
		t.Errorf("expected success=true, got %v", result.Output["success"])
	}
	if result.Output["files_changed"] != 0 {
		t.Errorf("expected files_changed=0, got %v", result.Output["files_changed"])
	}
}

func TestGitCommitStep_Execute_CommitFailure(t *testing.T) {
	callCount := 0

	s := &GitCommitStep{
		name:      "commit",
		directory: "/tmp/repo",
		message:   "update config",
		addAll:    true,
		tmpl:      NewTemplateEngine(),
		execCommand: func(_ context.Context, _ string, _ ...string) *exec.Cmd {
			callCount++
			if callCount == 1 {
				return exec.Command("true") // git add -A
			}
			return exec.Command("false") // git commit fails
		},
	}

	_, err := s.Execute(context.Background(), &PipelineContext{
		Current:     map[string]any{},
		StepOutputs: map[string]map[string]any{},
		TriggerData: map[string]any{},
		Metadata:    map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error when git commit fails")
	}
	if !strings.Contains(err.Error(), "git commit") {
		t.Errorf("expected error to mention git commit, got: %v", err)
	}
}

func TestGitCommitStep_Execute_WithAuthor(t *testing.T) {
	var commitArgs []string
	callCount := 0

	s := &GitCommitStep{
		name:        "commit",
		directory:   "/tmp/repo",
		message:     "update config",
		authorName:  "Workflow Bot",
		authorEmail: "bot@workflow.dev",
		addAll:      true,
		tmpl:        NewTemplateEngine(),
		execCommand: func(_ context.Context, name string, args ...string) *exec.Cmd {
			callCount++
			if callCount == 1 {
				return exec.Command("true") // git add -A
			}
			if callCount == 2 {
				commitArgs = append([]string{name}, args...)
				return exec.Command("echo", "1 file changed")
			}
			return exec.Command("echo", "abc123")
		},
	}

	_, err := s.Execute(context.Background(), &PipelineContext{
		Current:     map[string]any{},
		StepOutputs: map[string]map[string]any{},
		TriggerData: map[string]any{},
		Metadata:    map[string]any{},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	argsStr := strings.Join(commitArgs, " ")
	if !strings.Contains(argsStr, "--author") {
		t.Errorf("expected --author in commit args, got: %v", commitArgs)
	}
	if !strings.Contains(argsStr, "Workflow Bot") {
		t.Errorf("expected author name in commit args, got: %v", commitArgs)
	}
}

func TestGitCommitStep_Execute_TemplateResolution(t *testing.T) {
	var commitArgs []string
	callCount := 0

	s := &GitCommitStep{
		name:      "commit",
		directory: "/tmp/workspace/{{ .repo }}",
		message:   "Update config for {{ .version }}",
		addAll:    true,
		tmpl:      NewTemplateEngine(),
		execCommand: func(_ context.Context, name string, args ...string) *exec.Cmd {
			callCount++
			if callCount == 1 {
				return exec.Command("true") // git add -A
			}
			if callCount == 2 {
				commitArgs = append([]string{name}, args...)
				return exec.Command("echo", "1 file changed")
			}
			return exec.Command("echo", "abc123")
		},
	}

	pc := &PipelineContext{
		Current:     map[string]any{"repo": "my-repo", "version": "v1.2.0"},
		StepOutputs: map[string]map[string]any{},
		TriggerData: map[string]any{"repo": "my-repo", "version": "v1.2.0"},
		Metadata:    map[string]any{},
	}

	_, err := s.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	argsStr := strings.Join(commitArgs, " ")
	if !strings.Contains(argsStr, "/tmp/workspace/my-repo") {
		t.Errorf("expected resolved directory in args, got: %v", commitArgs)
	}
	if !strings.Contains(argsStr, "Update config for v1.2.0") {
		t.Errorf("expected resolved message in commit args, got: %v", commitArgs)
	}
}

func TestParseFilesChanged(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{" 3 files changed, 10 insertions(+)", 3},
		{" 1 file changed, 5 insertions(+), 2 deletions(-)", 1},
		{"nothing to commit", 0},
		{"", 0},
	}

	for _, tc := range tests {
		got := parseFilesChanged(tc.input)
		if got != tc.expected {
			t.Errorf("parseFilesChanged(%q) = %d, want %d", tc.input, got, tc.expected)
		}
	}
}
