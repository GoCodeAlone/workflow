package module

import (
	"bytes"
	"context"
	"testing"
)

func TestCLIPrintStep_Basic(t *testing.T) {
	var buf bytes.Buffer
	factory := newCLIPrintStepFactoryWithWriters(&buf, &buf)
	step, err := factory("print", map[string]any{"message": "hello world"}, nil)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if buf.String() != "hello world\n" {
		t.Errorf("unexpected output: %q", buf.String())
	}
	if result.Output["printed"] != "hello world" {
		t.Errorf("expected printed=hello world, got %v", result.Output["printed"])
	}
}

func TestCLIPrintStep_NoNewline(t *testing.T) {
	var buf bytes.Buffer
	factory := newCLIPrintStepFactoryWithWriters(&buf, &buf)
	step, err := factory("print", map[string]any{"message": "hi", "newline": false}, nil)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}
	_, err = step.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if buf.String() != "hi" {
		t.Errorf("unexpected output: %q", buf.String())
	}
}

func TestCLIPrintStep_TemplateResolution(t *testing.T) {
	var buf bytes.Buffer
	factory := newCLIPrintStepFactoryWithWriters(&buf, &buf)
	step, err := factory("print", map[string]any{"message": "cmd: {{.command}}"}, nil)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}
	pc := NewPipelineContext(map[string]any{"command": "validate"}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if buf.String() != "cmd: validate\n" {
		t.Errorf("unexpected output: %q", buf.String())
	}
}

func TestCLIPrintStep_StderrTarget(t *testing.T) {
	var stdout, stderr bytes.Buffer
	factory := newCLIPrintStepFactoryWithWriters(&stdout, &stderr)
	step, err := factory("print", map[string]any{"message": "err msg", "target": "stderr"}, nil)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}
	_, err = step.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("expected nothing on stdout, got %q", stdout.String())
	}
	if stderr.String() != "err msg\n" {
		t.Errorf("unexpected stderr: %q", stderr.String())
	}
}

func TestCLIPrintStep_MissingMessage(t *testing.T) {
	factory := newCLIPrintStepFactoryWithWriters(nil, nil)
	_, err := factory("print", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing message")
	}
}
