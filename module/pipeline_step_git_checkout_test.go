package module

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestGitCheckoutStep_FactoryRequiresDirectory(t *testing.T) {
	factory := NewGitCheckoutStepFactory()
	_, err := factory("checkout", map[string]any{"branch": "main"}, nil)
	if err == nil {
		t.Fatal("expected error when directory is missing")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("expected error to mention directory, got: %v", err)
	}
}

func TestGitCheckoutStep_FactoryRequiresBranch(t *testing.T) {
	factory := NewGitCheckoutStepFactory()
	_, err := factory("checkout", map[string]any{"directory": "/tmp/repo"}, nil)
	if err == nil {
		t.Fatal("expected error when branch is missing")
	}
	if !strings.Contains(err.Error(), "branch") {
		t.Errorf("expected error to mention branch, got: %v", err)
	}
}

func TestGitCheckoutStep_Name(t *testing.T) {
	factory := NewGitCheckoutStepFactory()
	step, err := factory("my-checkout", map[string]any{
		"directory": "/tmp/repo",
		"branch":    "main",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}
	if step.Name() != "my-checkout" {
		t.Errorf("expected name %q, got %q", "my-checkout", step.Name())
	}
}

func TestGitCheckoutStep_FactoryWithCreate(t *testing.T) {
	factory := NewGitCheckoutStepFactory()
	raw, err := factory("checkout", map[string]any{
		"directory": "/tmp/repo",
		"branch":    "feature/new",
		"create":    true,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}
	s := raw.(*GitCheckoutStep)
	if !s.create {
		t.Error("expected create=true")
	}
}

func TestGitCheckoutStep_Execute_CheckoutExistingBranch(t *testing.T) {
	var checkoutArgs []string
	callCount := 0

	s := &GitCheckoutStep{
		name:      "checkout",
		directory: "/tmp/repo",
		branch:    "main",
		create:    false,
		tmpl:      NewTemplateEngine(),
		execCommand: func(_ context.Context, name string, args ...string) *exec.Cmd {
			callCount++
			if callCount == 1 {
				checkoutArgs = append([]string{name}, args...)
				return exec.Command("true") // git checkout
			}
			return exec.Command("echo", "abc123") // git rev-parse HEAD
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

	if result.Output["branch"] != "main" {
		t.Errorf("expected branch=main, got %v", result.Output["branch"])
	}
	if result.Output["success"] != true {
		t.Errorf("expected success=true, got %v", result.Output["success"])
	}
	if result.Output["created"] != false {
		t.Errorf("expected created=false, got %v", result.Output["created"])
	}

	argsStr := strings.Join(checkoutArgs, " ")
	if !strings.Contains(argsStr, "checkout") {
		t.Errorf("expected checkout in args, got: %v", checkoutArgs)
	}
	if strings.Contains(argsStr, "-b") {
		t.Errorf("expected no -b flag for existing branch checkout, got: %v", checkoutArgs)
	}
}

func TestGitCheckoutStep_Execute_CreateNewBranch(t *testing.T) {
	var checkoutArgs []string
	callCount := 0

	s := &GitCheckoutStep{
		name:      "checkout",
		directory: "/tmp/repo",
		branch:    "feature/new",
		create:    true,
		tmpl:      NewTemplateEngine(),
		execCommand: func(_ context.Context, name string, args ...string) *exec.Cmd {
			callCount++
			if callCount == 1 {
				checkoutArgs = append([]string{name}, args...)
				return exec.Command("true") // git checkout -b
			}
			return exec.Command("echo", "abc123") // git rev-parse HEAD
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

	if result.Output["created"] != true {
		t.Errorf("expected created=true, got %v", result.Output["created"])
	}

	argsStr := strings.Join(checkoutArgs, " ")
	if !strings.Contains(argsStr, "-b") {
		t.Errorf("expected -b flag for new branch, got: %v", checkoutArgs)
	}
	if !strings.Contains(argsStr, "feature/new") {
		t.Errorf("expected branch name in args, got: %v", checkoutArgs)
	}
}

func TestGitCheckoutStep_Execute_CheckoutFailure(t *testing.T) {
	s := &GitCheckoutStep{
		name:      "checkout",
		directory: "/tmp/repo",
		branch:    "nonexistent",
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
		t.Fatal("expected error when git checkout fails")
	}
	if !strings.Contains(err.Error(), "git checkout") {
		t.Errorf("expected error to mention git checkout, got: %v", err)
	}
}

func TestGitCheckoutStep_Execute_TemplateResolution(t *testing.T) {
	var checkoutArgs []string
	callCount := 0

	s := &GitCheckoutStep{
		name:      "checkout",
		directory: "/tmp/workspace/{{ .repo }}",
		branch:    "feature/{{ .feature_name }}",
		create:    true,
		tmpl:      NewTemplateEngine(),
		execCommand: func(_ context.Context, name string, args ...string) *exec.Cmd {
			callCount++
			if callCount == 1 {
				checkoutArgs = append([]string{name}, args...)
				return exec.Command("true")
			}
			return exec.Command("echo", "abc123")
		},
	}

	pc := &PipelineContext{
		Current:     map[string]any{"repo": "my-repo", "feature_name": "add-logging"},
		StepOutputs: map[string]map[string]any{},
		TriggerData: map[string]any{"repo": "my-repo", "feature_name": "add-logging"},
		Metadata:    map[string]any{},
	}

	result, err := s.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Output["branch"] != "feature/add-logging" {
		t.Errorf("expected branch=feature/add-logging, got %v", result.Output["branch"])
	}

	argsStr := strings.Join(checkoutArgs, " ")
	if !strings.Contains(argsStr, "/tmp/workspace/my-repo") {
		t.Errorf("expected resolved directory in args, got: %v", checkoutArgs)
	}
	if !strings.Contains(argsStr, "feature/add-logging") {
		t.Errorf("expected resolved branch in args, got: %v", checkoutArgs)
	}
}
