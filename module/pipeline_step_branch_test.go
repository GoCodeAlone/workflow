package module

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// recordingStep is a test helper that records whether Execute was called.
type recordingStep struct {
	name   string
	called bool
	output map[string]any
	err    error
}

func (r *recordingStep) Name() string { return r.name }
func (r *recordingStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	r.called = true
	if r.err != nil {
		return nil, r.err
	}
	out := r.output
	if out == nil {
		out = map[string]any{"step": r.name}
	}
	return &StepResult{Output: out}, nil
}

// buildBranchStepDirect constructs a BranchStep from pre-built PipelineSteps,
// bypassing the factory/registry. Used for unit tests.
func buildBranchStepDirect(name, field string, branches map[string][]PipelineStep, defaultSteps []PipelineStep, mergeStep string) *BranchStep {
	return &BranchStep{
		name:         name,
		field:        field,
		branches:     branches,
		defaultSteps: defaultSteps,
		mergeStep:    mergeStep,
		tmpl:         NewTemplateEngine(),
	}
}

func TestBranch_MatchesRoute(t *testing.T) {
	spawnStep := &recordingStep{name: "dm_spawn"}
	narrateStep := &recordingStep{name: "dm_narrate"}

	step := buildBranchStepDirect("route", "command",
		map[string][]PipelineStep{
			"spawn":   {spawnStep},
			"narrate": {narrateStep},
		},
		nil, "")

	pc := NewPipelineContext(map[string]any{"command": "spawn"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !spawnStep.called {
		t.Error("expected spawn branch step to be called")
	}
	if narrateStep.called {
		t.Error("narrate branch step should NOT be called")
	}
	if result.Output["matched_value"] != "spawn" {
		t.Errorf("expected matched_value=spawn, got %v", result.Output["matched_value"])
	}
	if result.Output["branch"] != "spawn" {
		t.Errorf("expected branch=spawn, got %v", result.Output["branch"])
	}
}

func TestBranch_Default(t *testing.T) {
	spawnStep := &recordingStep{name: "dm_spawn"}
	defaultStep := &recordingStep{name: "dm_default"}

	step := buildBranchStepDirect("route", "command",
		map[string][]PipelineStep{
			"spawn": {spawnStep},
		},
		[]PipelineStep{defaultStep}, "")

	pc := NewPipelineContext(map[string]any{"command": "unknown"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spawnStep.called {
		t.Error("spawn branch step should NOT be called for unmatched value")
	}
	if !defaultStep.called {
		t.Error("expected default branch step to be called")
	}
	if result.Output["matched_value"] != "unknown" {
		t.Errorf("expected matched_value=unknown, got %v", result.Output["matched_value"])
	}
}

func TestBranch_NoMatch(t *testing.T) {
	spawnStep := &recordingStep{name: "dm_spawn"}

	step := buildBranchStepDirect("route", "command",
		map[string][]PipelineStep{
			"spawn": {spawnStep},
		},
		nil, "")

	pc := NewPipelineContext(map[string]any{"command": "unknown"}, nil)
	_, err := step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when no branch matches and no default configured")
	}
	if !strings.Contains(err.Error(), "not found in branches") {
		t.Errorf("expected 'not found in branches' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Errorf("expected the unmatched value in error, got: %v", err)
	}
}

func TestBranch_MergeStep(t *testing.T) {
	spawnStep := &recordingStep{name: "dm_spawn"}

	step := buildBranchStepDirect("route", "command",
		map[string][]PipelineStep{
			"spawn": {spawnStep},
		},
		nil, "broadcast")

	pc := NewPipelineContext(map[string]any{"command": "spawn"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.NextStep != "broadcast" {
		t.Errorf("expected NextStep=broadcast, got %q", result.NextStep)
	}
}

func TestBranch_MultipleBranchSteps(t *testing.T) {
	step1 := &recordingStep{name: "action1", output: map[string]any{"action1": true}}
	step2 := &recordingStep{name: "action2", output: map[string]any{"action2": true}}
	otherStep := &recordingStep{name: "other"}

	step := buildBranchStepDirect("route", "command",
		map[string][]PipelineStep{
			"multi": {step1, step2},
			"other": {otherStep},
		},
		nil, "done")

	pc := NewPipelineContext(map[string]any{"command": "multi"}, nil)
	_, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !step1.called || !step2.called {
		t.Error("expected both branch steps to be called")
	}
	if otherStep.called {
		t.Error("other branch step should NOT be called")
	}

	// Both outputs should be merged into the pipeline context
	if _, ok := pc.StepOutputs["action1"]; !ok {
		t.Error("expected action1 output in pipeline context")
	}
	if _, ok := pc.StepOutputs["action2"]; !ok {
		t.Error("expected action2 output in pipeline context")
	}
}

func TestBranch_SubStepError(t *testing.T) {
	failStep := &recordingStep{name: "fail", err: fmt.Errorf("step failed")}

	step := buildBranchStepDirect("route", "command",
		map[string][]PipelineStep{
			"spawn": {failStep},
		},
		nil, "done")

	pc := NewPipelineContext(map[string]any{"command": "spawn"}, nil)
	_, err := step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error from failing sub-step")
	}
	if !strings.Contains(err.Error(), "step failed") {
		t.Errorf("expected original error in output, got: %v", err)
	}
}

func TestBranch_FactoryRejectsMissingField(t *testing.T) {
	registry := NewStepRegistry()
	factory := NewBranchStepFactory(func() *StepRegistry { return registry })

	_, err := factory("bad-branch", map[string]any{
		"branches": map[string]any{
			"a": []any{map[string]any{"name": "s1", "type": "step.log"}},
		},
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing 'field'")
	}
	if !strings.Contains(err.Error(), "'field' is required") {
		t.Errorf("expected 'field is required' in error, got: %v", err)
	}
}

func TestBranch_FactoryRejectsMissingBranches(t *testing.T) {
	registry := NewStepRegistry()
	factory := NewBranchStepFactory(func() *StepRegistry { return registry })

	_, err := factory("bad-branch", map[string]any{
		"field": "command",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing 'branches'")
	}
	if !strings.Contains(err.Error(), "'branches' map is required") {
		t.Errorf("expected 'branches map is required' in error, got: %v", err)
	}
}

func TestBranch_FieldFromStepOutput(t *testing.T) {
	spawnStep := &recordingStep{name: "dm_spawn"}

	step := buildBranchStepDirect("route", "steps.parse.command",
		map[string][]PipelineStep{
			"spawn": {spawnStep},
		},
		nil, "done")

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("parse", map[string]any{"command": "spawn"})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !spawnStep.called {
		t.Error("expected spawn branch step to be called")
	}
	if result.NextStep != "done" {
		t.Errorf("expected NextStep=done, got %q", result.NextStep)
	}
}

func TestBranch_NoMergeStep(t *testing.T) {
	spawnStep := &recordingStep{name: "dm_spawn"}

	// No merge_step configured — NextStep should be empty (continue sequentially)
	step := buildBranchStepDirect("route", "command",
		map[string][]PipelineStep{
			"spawn": {spawnStep},
		},
		nil, "") // empty merge_step

	pc := NewPipelineContext(map[string]any{"command": "spawn"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NextStep != "" {
		t.Errorf("expected empty NextStep when no merge_step configured, got %q", result.NextStep)
	}
}
