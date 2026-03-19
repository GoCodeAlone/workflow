package module

import (
	"context"
	"testing"
)

func TestPipelineOutputStep_Source(t *testing.T) {
	factory := NewPipelineOutputStepFactory()
	step, err := factory("result", map[string]any{
		"source": "steps.fetch",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.StepOutputs["fetch"] = map[string]any{
		"gameId": "abc-123",
		"status": "active",
	}
	pc.Current["steps"] = pc.StepOutputs

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true")
	}
	if result.Output["gameId"] != "abc-123" {
		t.Errorf("expected gameId=abc-123, got %v", result.Output["gameId"])
	}
	if result.Output["status"] != "active" {
		t.Errorf("expected status=active, got %v", result.Output["status"])
	}

	// Verify _pipeline_output is set in metadata
	pipeOut, ok := pc.Metadata["_pipeline_output"].(map[string]any)
	if !ok {
		t.Fatal("expected _pipeline_output in metadata")
	}
	if pipeOut["gameId"] != "abc-123" {
		t.Errorf("expected _pipeline_output gameId=abc-123, got %v", pipeOut["gameId"])
	}
}

func TestPipelineOutputStep_SourceNestedField(t *testing.T) {
	factory := NewPipelineOutputStepFactory()
	step, err := factory("result", map[string]any{
		"source": "steps.fetch.row",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.StepOutputs["fetch"] = map[string]any{
		"row": map[string]any{
			"id":   "123",
			"name": "test",
		},
		"found": true,
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true")
	}

	pipeOut, ok := pc.Metadata["_pipeline_output"].(map[string]any)
	if !ok {
		t.Fatal("expected _pipeline_output in metadata")
	}
	if pipeOut["id"] != "123" {
		t.Errorf("expected id=123, got %v", pipeOut["id"])
	}
}

func TestPipelineOutputStep_Values(t *testing.T) {
	factory := NewPipelineOutputStepFactory()
	step, err := factory("result", map[string]any{
		"values": map[string]any{
			"gameId": "{{ .gameId }}",
			"turn":   "{{ index .steps \"state\" \"turnNumber\" }}",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"gameId": "game-42",
	}, nil)
	pc.StepOutputs["state"] = map[string]any{
		"turnNumber": "5",
	}
	pc.Current["steps"] = pc.StepOutputs

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true")
	}
	if result.Output["gameId"] != "game-42" {
		t.Errorf("expected gameId=game-42, got %v", result.Output["gameId"])
	}
	if result.Output["turn"] != "5" {
		t.Errorf("expected turn=5, got %v", result.Output["turn"])
	}

	pipeOut := pc.Metadata["_pipeline_output"].(map[string]any)
	if pipeOut["gameId"] != "game-42" {
		t.Errorf("expected _pipeline_output gameId=game-42, got %v", pipeOut["gameId"])
	}
}

func TestPipelineOutputStep_RequiresSourceOrValues(t *testing.T) {
	factory := NewPipelineOutputStepFactory()
	_, err := factory("result", map[string]any{}, nil)
	if err == nil {
		t.Error("expected error when neither source nor values is provided")
	}
}

func TestPipelineOutputStep_SourceAndValuesMutuallyExclusive(t *testing.T) {
	factory := NewPipelineOutputStepFactory()
	_, err := factory("result", map[string]any{
		"source": "steps.fetch",
		"values": map[string]any{"key": "val"},
	}, nil)
	if err == nil {
		t.Error("expected error when both source and values are provided")
	}
}

func TestPipelineOutputStep_ValuesTemplateError(t *testing.T) {
	factory := NewPipelineOutputStepFactory()
	step, err := factory("result", map[string]any{
		"values": map[string]any{
			"bad": "{{ .nonexistent.deep.path }}",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Error("expected error on template resolution failure")
	}
}

func TestPipelineOutputStep_SourceNotFound(t *testing.T) {
	factory := NewPipelineOutputStepFactory()
	step, err := factory("result", map[string]any{
		"source": "steps.nonexistent",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	// Should return empty output, not error
	if !result.Stop {
		t.Error("expected Stop=true")
	}
}
