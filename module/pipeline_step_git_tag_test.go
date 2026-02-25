package module

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestGitTagStep_FactoryRequiresDirectory(t *testing.T) {
	factory := NewGitTagStepFactory()
	_, err := factory("tag", map[string]any{"tag": "v1.0.0"}, nil)
	if err == nil {
		t.Fatal("expected error when directory is missing")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("expected error to mention directory, got: %v", err)
	}
}

func TestGitTagStep_FactoryRequiresTag(t *testing.T) {
	factory := NewGitTagStepFactory()
	_, err := factory("tag", map[string]any{"directory": "/tmp/repo"}, nil)
	if err == nil {
		t.Fatal("expected error when tag is missing")
	}
	if !strings.Contains(err.Error(), "tag") {
		t.Errorf("expected error to mention tag, got: %v", err)
	}
}

func TestGitTagStep_Name(t *testing.T) {
	factory := NewGitTagStepFactory()
	step, err := factory("my-tag", map[string]any{
		"directory": "/tmp/repo",
		"tag":       "v1.0.0",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}
	if step.Name() != "my-tag" {
		t.Errorf("expected name %q, got %q", "my-tag", step.Name())
	}
}

func TestGitTagStep_FactoryWithOptions(t *testing.T) {
	factory := NewGitTagStepFactory()
	raw, err := factory("tag", map[string]any{
		"directory": "/tmp/repo",
		"tag":       "v1.0.0",
		"message":   "Release v1.0.0",
		"push":      true,
		"token":     "mytoken",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}
	s := raw.(*GitTagStep)
	if s.message != "Release v1.0.0" {
		t.Errorf("expected message, got %q", s.message)
	}
	if !s.push {
		t.Error("expected push=true")
	}
}

func TestGitTagStep_Execute_LightweightTag(t *testing.T) {
	var tagArgs []string
	callCount := 0

	s := &GitTagStep{
		name:      "tag",
		directory: "/tmp/repo",
		tag:       "v1.0.0",
		message:   "", // lightweight tag
		push:      false,
		tmpl:      NewTemplateEngine(),
		execCommand: func(_ context.Context, name string, args ...string) *exec.Cmd {
			callCount++
			if callCount == 1 {
				tagArgs = append([]string{name}, args...)
				return exec.Command("true") // git tag
			}
			return exec.Command("echo", "abc123") // git rev-list
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

	if result.Output["tag"] != "v1.0.0" {
		t.Errorf("expected tag=v1.0.0, got %v", result.Output["tag"])
	}
	if result.Output["pushed"] != false {
		t.Errorf("expected pushed=false, got %v", result.Output["pushed"])
	}
	if result.Output["success"] != true {
		t.Errorf("expected success=true, got %v", result.Output["success"])
	}

	// Verify no -a flag (lightweight tag).
	argsStr := strings.Join(tagArgs, " ")
	if strings.Contains(argsStr, " -a ") {
		t.Errorf("expected no -a flag for lightweight tag, got: %v", tagArgs)
	}
}

func TestGitTagStep_Execute_AnnotatedTag(t *testing.T) {
	var tagArgs []string
	callCount := 0

	s := &GitTagStep{
		name:      "tag",
		directory: "/tmp/repo",
		tag:       "v1.0.0",
		message:   "Release v1.0.0",
		push:      false,
		tmpl:      NewTemplateEngine(),
		execCommand: func(_ context.Context, name string, args ...string) *exec.Cmd {
			callCount++
			if callCount == 1 {
				tagArgs = append([]string{name}, args...)
				return exec.Command("true") // git tag
			}
			return exec.Command("echo", "abc123") // git rev-list
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

	argsStr := strings.Join(tagArgs, " ")
	if !strings.Contains(argsStr, "-a") {
		t.Errorf("expected -a flag for annotated tag, got: %v", tagArgs)
	}
	if !strings.Contains(argsStr, "-m") {
		t.Errorf("expected -m flag for annotated tag, got: %v", tagArgs)
	}
	if !strings.Contains(argsStr, "Release v1.0.0") {
		t.Errorf("expected message in tag args, got: %v", tagArgs)
	}
}

func TestGitTagStep_Execute_TagFailure(t *testing.T) {
	s := &GitTagStep{
		name:      "tag",
		directory: "/tmp/repo",
		tag:       "v1.0.0",
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
		t.Fatal("expected error when git tag fails")
	}
	if !strings.Contains(err.Error(), "git tag") {
		t.Errorf("expected error to mention git tag, got: %v", err)
	}
}

func TestGitTagStep_Execute_WithPush(t *testing.T) {
	var gitCalls [][]string
	callCount := 0

	s := &GitTagStep{
		name:      "tag",
		directory: "/tmp/repo",
		tag:       "v1.0.0",
		message:   "Release",
		push:      true,
		tmpl:      NewTemplateEngine(),
		execCommand: func(_ context.Context, name string, args ...string) *exec.Cmd {
			callCount++
			gitCalls = append(gitCalls, append([]string{name}, args...))
			return exec.Command("true") // all succeed
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

	if result.Output["pushed"] != true {
		t.Errorf("expected pushed=true, got %v", result.Output["pushed"])
	}

	// Expect calls: git tag, git rev-list, git push origin v1.0.0
	foundPush := false
	for _, call := range gitCalls {
		argsStr := strings.Join(call, " ")
		if strings.Contains(argsStr, "push") && strings.Contains(argsStr, "v1.0.0") {
			foundPush = true
			break
		}
	}
	if !foundPush {
		t.Errorf("expected git push with tag, got calls: %v", gitCalls)
	}
}

func TestGitTagStep_Execute_PushFailure(t *testing.T) {
	callCount := 0

	s := &GitTagStep{
		name:      "tag",
		directory: "/tmp/repo",
		tag:       "v1.0.0",
		push:      true,
		tmpl:      NewTemplateEngine(),
		execCommand: func(_ context.Context, _ string, args ...string) *exec.Cmd {
			callCount++
			if callCount == 1 {
				return exec.Command("true") // git tag succeeds
			}
			return exec.Command("false") // rev-list and push fail
		},
	}

	_, err := s.Execute(context.Background(), &PipelineContext{
		Current:     map[string]any{},
		StepOutputs: map[string]map[string]any{},
		TriggerData: map[string]any{},
		Metadata:    map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error when push fails")
	}
}

func TestGitTagStep_Execute_TemplateResolution(t *testing.T) {
	var tagArgs []string
	callCount := 0

	s := &GitTagStep{
		name:      "tag",
		directory: "/tmp/workspace/{{ .repo }}",
		tag:       "v{{ .version }}",
		message:   "Release {{ .version }}",
		push:      false,
		tmpl:      NewTemplateEngine(),
		execCommand: func(_ context.Context, name string, args ...string) *exec.Cmd {
			callCount++
			if callCount == 1 {
				tagArgs = append([]string{name}, args...)
				return exec.Command("true")
			}
			return exec.Command("echo", "abc123")
		},
	}

	pc := &PipelineContext{
		Current:     map[string]any{"repo": "my-repo", "version": "1.2.0"},
		StepOutputs: map[string]map[string]any{},
		TriggerData: map[string]any{"repo": "my-repo", "version": "1.2.0"},
		Metadata:    map[string]any{},
	}

	result, err := s.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Output["tag"] != "v1.2.0" {
		t.Errorf("expected tag=v1.2.0, got %v", result.Output["tag"])
	}

	argsStr := strings.Join(tagArgs, " ")
	if !strings.Contains(argsStr, "v1.2.0") {
		t.Errorf("expected resolved tag in args, got: %v", tagArgs)
	}
	if !strings.Contains(argsStr, "/tmp/workspace/my-repo") {
		t.Errorf("expected resolved directory in args, got: %v", tagArgs)
	}
}
