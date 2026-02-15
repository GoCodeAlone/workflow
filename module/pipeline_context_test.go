package module

import "testing"

func TestNewPipelineContext_InitialState(t *testing.T) {
	triggerData := map[string]any{
		"order_id": "ORD-123",
		"amount":   42.5,
	}
	metadata := map[string]any{
		"pipeline": "test-pipeline",
	}

	pc := NewPipelineContext(triggerData, metadata)

	// TriggerData should be a copy of the input
	if pc.TriggerData["order_id"] != "ORD-123" {
		t.Errorf("expected TriggerData[order_id] = ORD-123, got %v", pc.TriggerData["order_id"])
	}

	// Current should start as a copy of trigger data
	if pc.Current["order_id"] != "ORD-123" {
		t.Errorf("expected Current[order_id] = ORD-123, got %v", pc.Current["order_id"])
	}
	if pc.Current["amount"] != 42.5 {
		t.Errorf("expected Current[amount] = 42.5, got %v", pc.Current["amount"])
	}

	// Metadata should be a copy of the input
	if pc.Metadata["pipeline"] != "test-pipeline" {
		t.Errorf("expected Metadata[pipeline] = test-pipeline, got %v", pc.Metadata["pipeline"])
	}

	// StepOutputs should be initialized but empty
	if pc.StepOutputs == nil {
		t.Fatal("expected StepOutputs to be initialized")
	}
	if len(pc.StepOutputs) != 0 {
		t.Errorf("expected StepOutputs to be empty, got %d entries", len(pc.StepOutputs))
	}
}

func TestNewPipelineContext_NilInputs(t *testing.T) {
	pc := NewPipelineContext(nil, nil)

	if pc.TriggerData == nil {
		t.Fatal("expected TriggerData to be initialized even with nil input")
	}
	if pc.Current == nil {
		t.Fatal("expected Current to be initialized even with nil input")
	}
	if pc.Metadata == nil {
		t.Fatal("expected Metadata to be initialized even with nil input")
	}
	if pc.StepOutputs == nil {
		t.Fatal("expected StepOutputs to be initialized")
	}
}

func TestNewPipelineContext_IsolatesFromOriginalMap(t *testing.T) {
	triggerData := map[string]any{"key": "original"}
	metadata := map[string]any{"meta": "original"}

	pc := NewPipelineContext(triggerData, metadata)

	// Mutating the original maps should not affect the context
	triggerData["key"] = "mutated"
	metadata["meta"] = "mutated"

	if pc.TriggerData["key"] != "original" {
		t.Errorf("TriggerData was mutated by changing original map")
	}
	if pc.Current["key"] != "original" {
		t.Errorf("Current was mutated by changing original map")
	}
	if pc.Metadata["meta"] != "original" {
		t.Errorf("Metadata was mutated by changing original map")
	}
}

func TestPipelineContext_MergeStepOutput(t *testing.T) {
	pc := NewPipelineContext(map[string]any{"order_id": "ORD-1"}, nil)

	output := map[string]any{
		"validated": true,
		"score":     99,
	}

	pc.MergeStepOutput("validate", output)

	// StepOutputs should contain the step output
	stepOut, ok := pc.StepOutputs["validate"]
	if !ok {
		t.Fatal("expected StepOutputs to contain 'validate'")
	}
	if stepOut["validated"] != true {
		t.Errorf("expected StepOutputs[validate][validated] = true, got %v", stepOut["validated"])
	}

	// Current should contain both trigger data and step output
	if pc.Current["order_id"] != "ORD-1" {
		t.Errorf("expected Current[order_id] = ORD-1, got %v", pc.Current["order_id"])
	}
	if pc.Current["validated"] != true {
		t.Errorf("expected Current[validated] = true, got %v", pc.Current["validated"])
	}
	if pc.Current["score"] != 99 {
		t.Errorf("expected Current[score] = 99, got %v", pc.Current["score"])
	}
}

func TestPipelineContext_MergeStepOutput_NilOutput(t *testing.T) {
	pc := NewPipelineContext(map[string]any{"key": "val"}, nil)

	pc.MergeStepOutput("step1", nil)

	// Nil output should be a no-op
	if _, ok := pc.StepOutputs["step1"]; ok {
		t.Error("expected nil output to be ignored in StepOutputs")
	}
	if pc.Current["key"] != "val" {
		t.Errorf("expected Current to remain unchanged after nil merge")
	}
}

func TestPipelineContext_MultipleStepOutputs_NoClobber(t *testing.T) {
	pc := NewPipelineContext(map[string]any{"initial": "data"}, nil)

	// First step output
	pc.MergeStepOutput("step1", map[string]any{
		"result_a": "alpha",
		"shared":   "from_step1",
	})

	// Second step output
	pc.MergeStepOutput("step2", map[string]any{
		"result_b": "beta",
		"shared":   "from_step2",
	})

	// Both step outputs should be preserved independently
	if pc.StepOutputs["step1"]["result_a"] != "alpha" {
		t.Errorf("step1 output lost after step2 merge")
	}
	if pc.StepOutputs["step1"]["shared"] != "from_step1" {
		t.Errorf("step1 'shared' was clobbered in StepOutputs")
	}
	if pc.StepOutputs["step2"]["result_b"] != "beta" {
		t.Errorf("step2 output missing")
	}
	if pc.StepOutputs["step2"]["shared"] != "from_step2" {
		t.Errorf("step2 'shared' missing from StepOutputs")
	}

	// Current should have all keys, with later step winning on conflicts
	if pc.Current["initial"] != "data" {
		t.Errorf("initial trigger data lost")
	}
	if pc.Current["result_a"] != "alpha" {
		t.Errorf("result_a missing from Current")
	}
	if pc.Current["result_b"] != "beta" {
		t.Errorf("result_b missing from Current")
	}
	if pc.Current["shared"] != "from_step2" {
		t.Errorf("expected Current[shared] = from_step2 (latest wins), got %v", pc.Current["shared"])
	}
}

func TestPipelineContext_MergeStepOutput_IsolatesFromOriginal(t *testing.T) {
	pc := NewPipelineContext(nil, nil)

	output := map[string]any{"key": "original"}
	pc.MergeStepOutput("step1", output)

	// Mutating the original output map should not affect stored step output
	output["key"] = "mutated"

	if pc.StepOutputs["step1"]["key"] != "original" {
		t.Errorf("StepOutputs was mutated by changing original output map")
	}
}
