package module

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestGitCloneStep_FactoryRequiresRepository(t *testing.T) {
	factory := NewGitCloneStepFactory()
	_, err := factory("clone", map[string]any{"directory": "/tmp/repo"}, nil)
	if err == nil {
		t.Fatal("expected error when repository is missing")
	}
	if !strings.Contains(err.Error(), "repository") {
		t.Errorf("expected error to mention repository, got: %v", err)
	}
}

func TestGitCloneStep_FactoryRequiresDirectory(t *testing.T) {
	factory := NewGitCloneStepFactory()
	_, err := factory("clone", map[string]any{"repository": "https://github.com/org/repo.git"}, nil)
	if err == nil {
		t.Fatal("expected error when directory is missing")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("expected error to mention directory, got: %v", err)
	}
}

func TestGitCloneStep_Name(t *testing.T) {
	factory := NewGitCloneStepFactory()
	step, err := factory("my-clone", map[string]any{
		"repository": "https://github.com/org/repo.git",
		"directory":  "/tmp/repo",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}
	if step.Name() != "my-clone" {
		t.Errorf("expected name %q, got %q", "my-clone", step.Name())
	}
}

func TestGitCloneStep_FactoryDefaults(t *testing.T) {
	factory := NewGitCloneStepFactory()
	raw, err := factory("clone", map[string]any{
		"repository": "https://github.com/org/repo.git",
		"directory":  "/tmp/repo",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}
	s := raw.(*GitCloneStep)
	if s.branch != "" {
		t.Errorf("expected empty default branch, got %q", s.branch)
	}
	if s.depth != 0 {
		t.Errorf("expected depth 0, got %d", s.depth)
	}
}

func TestGitCloneStep_FactoryWithAllOptions(t *testing.T) {
	factory := NewGitCloneStepFactory()
	raw, err := factory("clone", map[string]any{
		"repository": "https://github.com/org/repo.git",
		"directory":  "/tmp/repo",
		"branch":     "main",
		"depth":      1,
		"token":      "mytoken",
		"ssh_key":    "mykeydata",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}
	s := raw.(*GitCloneStep)
	if s.branch != "main" {
		t.Errorf("expected branch main, got %q", s.branch)
	}
	if s.depth != 1 {
		t.Errorf("expected depth 1, got %d", s.depth)
	}
	if s.token != "mytoken" {
		t.Errorf("expected token mytoken, got %q", s.token)
	}
}

func TestGitCloneStep_Execute_Success(t *testing.T) {
	var capturedArgs [][]string
	callCount := 0

	s := &GitCloneStep{
		name:       "clone",
		repository: "https://github.com/org/repo.git",
		directory:  "/tmp/repo",
		tmpl:       NewTemplateEngine(),
		execCommand: func(_ context.Context, name string, args ...string) *exec.Cmd {
			callCount++
			capturedArgs = append(capturedArgs, append([]string{name}, args...))
			if callCount == 1 {
				// git clone — succeed
				return exec.Command("true")
			}
			// git rev-parse HEAD and --abbrev-ref HEAD — echo a fake SHA/branch
			if len(args) > 0 && args[len(args)-1] == "HEAD" {
				return exec.Command("echo", "abc123def456")
			}
			return exec.Command("echo", "main")
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
	if result.Output["clone_dir"] != "/tmp/repo" {
		t.Errorf("expected clone_dir=/tmp/repo, got %v", result.Output["clone_dir"])
	}

	// Verify git clone was called.
	if callCount < 1 {
		t.Error("expected at least one exec call")
	}
	if len(capturedArgs) < 1 || capturedArgs[0][0] != "git" {
		t.Error("expected first call to be git")
	}
	cloneArgs := capturedArgs[0]
	foundClone := false
	for _, a := range cloneArgs {
		if a == "clone" {
			foundClone = true
			break
		}
	}
	if !foundClone {
		t.Errorf("expected git clone in first call args, got %v", cloneArgs)
	}
}

func TestGitCloneStep_Execute_CloneFailure(t *testing.T) {
	s := &GitCloneStep{
		name:       "clone",
		repository: "https://github.com/org/repo.git",
		directory:  "/tmp/repo",
		tmpl:       NewTemplateEngine(),
		execCommand: func(_ context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.Command("false")
		},
	}

	_, err := s.Execute(context.Background(), &PipelineContext{
		Current:     map[string]any{},
		StepOutputs: map[string]map[string]any{},
		TriggerData: map[string]any{},
		Metadata:    map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error when git clone fails")
	}
	if !strings.Contains(err.Error(), "git clone") {
		t.Errorf("expected error to mention git clone, got: %v", err)
	}
}

func TestGitCloneStep_Execute_TokenInjectedIntoURL(t *testing.T) {
	var capturedArgs []string
	callCount := 0

	s := &GitCloneStep{
		name:       "clone",
		repository: "https://github.com/org/repo.git",
		directory:  "/tmp/repo",
		token:      "secret-token",
		tmpl:       NewTemplateEngine(),
		execCommand: func(_ context.Context, name string, args ...string) *exec.Cmd {
			callCount++
			if callCount == 1 {
				capturedArgs = append([]string{name}, args...)
				return exec.Command("true")
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

	// Check that the token-injected URL appears in the clone args.
	foundToken := false
	for _, a := range capturedArgs {
		if strings.Contains(a, "secret-token@") {
			foundToken = true
			break
		}
	}
	if !foundToken {
		t.Errorf("expected token to be injected into URL, args: %v", capturedArgs)
	}
}

func TestGitCloneStep_Execute_WithBranchAndDepth(t *testing.T) {
	var cloneArgs []string
	callCount := 0

	s := &GitCloneStep{
		name:       "clone",
		repository: "https://github.com/org/repo.git",
		directory:  "/tmp/repo",
		branch:     "develop",
		depth:      1,
		tmpl:       NewTemplateEngine(),
		execCommand: func(_ context.Context, name string, args ...string) *exec.Cmd {
			callCount++
			if callCount == 1 {
				cloneArgs = append([]string{name}, args...)
				return exec.Command("true")
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

	argsStr := strings.Join(cloneArgs, " ")
	if !strings.Contains(argsStr, "--branch") || !strings.Contains(argsStr, "develop") {
		t.Errorf("expected --branch develop in args, got: %v", cloneArgs)
	}
	if !strings.Contains(argsStr, "--depth") || !strings.Contains(argsStr, "1") {
		t.Errorf("expected --depth 1 in args, got: %v", cloneArgs)
	}
}

func TestGitCloneStep_Execute_TemplateResolution(t *testing.T) {
	var capturedDir string
	callCount := 0

	s := &GitCloneStep{
		name:       "clone",
		repository: "https://github.com/org/{{ .repo }}.git",
		directory:  "/tmp/workspace/{{ .repo }}",
		tmpl:       NewTemplateEngine(),
		execCommand: func(_ context.Context, name string, args ...string) *exec.Cmd {
			callCount++
			if callCount == 1 {
				// Last arg before the URL should be the directory.
				for i, a := range args {
					if a == "/tmp/workspace/my-repo" {
						_ = i
						capturedDir = a
					}
				}
				return exec.Command("true")
			}
			return exec.Command("echo", "abc123")
		},
	}

	pc := &PipelineContext{
		Current:     map[string]any{"repo": "my-repo"},
		StepOutputs: map[string]map[string]any{},
		TriggerData: map[string]any{"repo": "my-repo"},
		Metadata:    map[string]any{},
	}

	result, err := s.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Output["clone_dir"] != "/tmp/workspace/my-repo" {
		t.Errorf("expected clone_dir=/tmp/workspace/my-repo, got %v", result.Output["clone_dir"])
	}
	if capturedDir != "/tmp/workspace/my-repo" {
		t.Errorf("expected directory /tmp/workspace/my-repo in git args, got %q", capturedDir)
	}
}
