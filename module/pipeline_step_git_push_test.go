package module

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestGitPushStep_FactoryRequiresDirectory(t *testing.T) {
	factory := NewGitPushStepFactory()
	_, err := factory("push", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error when directory is missing")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("expected error to mention directory, got: %v", err)
	}
}

func TestGitPushStep_FactoryDefaultRemote(t *testing.T) {
	factory := NewGitPushStepFactory()
	raw, err := factory("push", map[string]any{"directory": "/tmp/repo"}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}
	s := raw.(*GitPushStep)
	if s.remote != "origin" {
		t.Errorf("expected default remote origin, got %q", s.remote)
	}
}

func TestGitPushStep_Name(t *testing.T) {
	factory := NewGitPushStepFactory()
	step, err := factory("my-push", map[string]any{"directory": "/tmp/repo"}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}
	if step.Name() != "my-push" {
		t.Errorf("expected name %q, got %q", "my-push", step.Name())
	}
}

func TestGitPushStep_FactoryWithOptions(t *testing.T) {
	factory := NewGitPushStepFactory()
	raw, err := factory("push", map[string]any{
		"directory": "/tmp/repo",
		"remote":    "upstream",
		"branch":    "main",
		"force":     true,
		"tags":      true,
		"token":     "mytoken",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}
	s := raw.(*GitPushStep)
	if s.remote != "upstream" {
		t.Errorf("expected remote upstream, got %q", s.remote)
	}
	if s.branch != "main" {
		t.Errorf("expected branch main, got %q", s.branch)
	}
	if !s.force {
		t.Error("expected force=true")
	}
	if !s.tags {
		t.Error("expected tags=true")
	}
}

func TestGitPushStep_Execute_Success(t *testing.T) {
	var pushArgs []string
	callCount := 0

	s := &GitPushStep{
		name:      "push",
		directory: "/tmp/repo",
		remote:    "origin",
		branch:    "main",
		tmpl:      NewTemplateEngine(),
		execCommand: func(_ context.Context, name string, args ...string) *exec.Cmd {
			callCount++
			if callCount == 1 {
				// git push
				pushArgs = append([]string{name}, args...)
				return exec.Command("true")
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
	if result.Output["remote"] != "origin" {
		t.Errorf("expected remote=origin, got %v", result.Output["remote"])
	}
	if result.Output["branch"] != "main" {
		t.Errorf("expected branch=main, got %v", result.Output["branch"])
	}

	argsStr := strings.Join(pushArgs, " ")
	if !strings.Contains(argsStr, "push") {
		t.Errorf("expected push in args, got: %v", pushArgs)
	}
}

func TestGitPushStep_Execute_PushFailure(t *testing.T) {
	s := &GitPushStep{
		name:      "push",
		directory: "/tmp/repo",
		remote:    "origin",
		branch:    "main",
		tmpl:      NewTemplateEngine(),
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
		t.Fatal("expected error when git push fails")
	}
	if !strings.Contains(err.Error(), "git push") {
		t.Errorf("expected error to mention git push, got: %v", err)
	}
}

func TestGitPushStep_Execute_ForceAndTags(t *testing.T) {
	var pushArgs []string
	callCount := 0

	s := &GitPushStep{
		name:      "push",
		directory: "/tmp/repo",
		remote:    "origin",
		branch:    "main",
		force:     true,
		tags:      true,
		tmpl:      NewTemplateEngine(),
		execCommand: func(_ context.Context, name string, args ...string) *exec.Cmd {
			callCount++
			if callCount == 1 {
				pushArgs = append([]string{name}, args...)
				return exec.Command("true")
			}
			return exec.Command("echo", "main")
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

	argsStr := strings.Join(pushArgs, " ")
	if !strings.Contains(argsStr, "--force") {
		t.Errorf("expected --force in push args, got: %v", pushArgs)
	}
	if !strings.Contains(argsStr, "--tags") {
		t.Errorf("expected --tags in push args, got: %v", pushArgs)
	}
}

func TestGitPushStep_Execute_InfersBranch(t *testing.T) {
	callCount := 0

	s := &GitPushStep{
		name:      "push",
		directory: "/tmp/repo",
		remote:    "origin",
		branch:    "", // not set — should infer from current branch
		tmpl:      NewTemplateEngine(),
		execCommand: func(_ context.Context, name string, args ...string) *exec.Cmd {
			callCount++
			if callCount == 1 {
				// rev-parse --abbrev-ref HEAD — return "feature-branch"
				return exec.Command("echo", "feature-branch")
			}
			// git push
			return exec.Command("true")
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

	if result.Output["branch"] != "feature-branch" {
		t.Errorf("expected branch=feature-branch, got %v", result.Output["branch"])
	}
}

func TestGitPushStep_Execute_TemplateResolution(t *testing.T) {
	var pushArgs []string
	callCount := 0

	s := &GitPushStep{
		name:      "push",
		directory: "/tmp/workspace/{{ .repo }}",
		remote:    "origin",
		branch:    "{{ .branch }}",
		tmpl:      NewTemplateEngine(),
		execCommand: func(_ context.Context, name string, args ...string) *exec.Cmd {
			callCount++
			if callCount == 1 {
				pushArgs = append([]string{name}, args...)
				return exec.Command("true")
			}
			return exec.Command("echo", "main")
		},
	}

	pc := &PipelineContext{
		Current:     map[string]any{"repo": "my-repo", "branch": "release/1.0"},
		StepOutputs: map[string]map[string]any{},
		TriggerData: map[string]any{"repo": "my-repo", "branch": "release/1.0"},
		Metadata:    map[string]any{},
	}

	result, err := s.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	argsStr := strings.Join(pushArgs, " ")
	if !strings.Contains(argsStr, "/tmp/workspace/my-repo") {
		t.Errorf("expected resolved directory in args, got: %v", pushArgs)
	}
	if result.Output["branch"] != "release/1.0" {
		t.Errorf("expected branch=release/1.0, got %v", result.Output["branch"])
	}
}
