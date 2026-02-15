package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// ===========================================================================
// TestDiffMapsIdentical
// ===========================================================================

func TestDiffMapsIdentical(t *testing.T) {
	a := map[string]any{
		"name":   "Alice",
		"age":    30.0,
		"active": true,
	}
	b := map[string]any{
		"name":   "Alice",
		"age":    30.0,
		"active": true,
	}

	changes := DiffMaps(a, b)
	if len(changes) != 0 {
		t.Errorf("expected 0 changes for identical maps, got %d: %+v", len(changes), changes)
	}
}

// ===========================================================================
// TestDiffMapsChanged
// ===========================================================================

func TestDiffMapsChanged(t *testing.T) {
	a := map[string]any{
		"name":   "Alice",
		"age":    30.0,
		"active": true,
	}
	b := map[string]any{
		"name":   "Bob",
		"age":    25.0,
		"active": true,
	}

	changes := DiffMaps(a, b)
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d: %+v", len(changes), changes)
	}

	// Changes should be sorted by path.
	changeMap := make(map[string]FieldChange)
	for _, c := range changes {
		changeMap[c.Path] = c
	}

	if c, ok := changeMap["age"]; ok {
		if c.ValueA != 30.0 || c.ValueB != 25.0 {
			t.Errorf("age change: expected 30->25, got %v->%v", c.ValueA, c.ValueB)
		}
	} else {
		t.Error("expected 'age' change")
	}

	if c, ok := changeMap["name"]; ok {
		if c.ValueA != "Alice" || c.ValueB != "Bob" {
			t.Errorf("name change: expected Alice->Bob, got %v->%v", c.ValueA, c.ValueB)
		}
	} else {
		t.Error("expected 'name' change")
	}
}

// ===========================================================================
// TestDiffMapsNested
// ===========================================================================

func TestDiffMapsNested(t *testing.T) {
	a := map[string]any{
		"user": map[string]any{
			"name": "Alice",
			"address": map[string]any{
				"city":  "NYC",
				"state": "NY",
			},
		},
		"status": "active",
	}
	b := map[string]any{
		"user": map[string]any{
			"name": "Alice",
			"address": map[string]any{
				"city":  "LA",
				"state": "CA",
			},
		},
		"status": "active",
	}

	changes := DiffMaps(a, b)
	if len(changes) != 2 {
		t.Fatalf("expected 2 nested changes, got %d: %+v", len(changes), changes)
	}

	changeMap := make(map[string]FieldChange)
	for _, c := range changes {
		changeMap[c.Path] = c
	}

	if c, ok := changeMap["user.address.city"]; ok {
		if c.ValueA != "NYC" || c.ValueB != "LA" {
			t.Errorf("city change: expected NYC->LA, got %v->%v", c.ValueA, c.ValueB)
		}
	} else {
		t.Error("expected 'user.address.city' change")
	}

	if c, ok := changeMap["user.address.state"]; ok {
		if c.ValueA != "NY" || c.ValueB != "CA" {
			t.Errorf("state change: expected NY->CA, got %v->%v", c.ValueA, c.ValueB)
		}
	} else {
		t.Error("expected 'user.address.state' change")
	}
}

// ===========================================================================
// TestDiffMapsAddedRemoved
// ===========================================================================

func TestDiffMapsAddedRemoved(t *testing.T) {
	a := map[string]any{
		"name":    "Alice",
		"removed": "old-value",
	}
	b := map[string]any{
		"name":  "Alice",
		"added": "new-value",
	}

	changes := DiffMaps(a, b)
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes (1 added, 1 removed), got %d: %+v", len(changes), changes)
	}

	changeMap := make(map[string]FieldChange)
	for _, c := range changes {
		changeMap[c.Path] = c
	}

	// Removed field: present in a, absent in b.
	if c, ok := changeMap["removed"]; ok {
		if c.ValueA != "old-value" {
			t.Errorf("removed.ValueA: expected 'old-value', got %v", c.ValueA)
		}
		if c.ValueB != nil {
			t.Errorf("removed.ValueB: expected nil, got %v", c.ValueB)
		}
	} else {
		t.Error("expected 'removed' change")
	}

	// Added field: absent in a, present in b.
	if c, ok := changeMap["added"]; ok {
		if c.ValueA != nil {
			t.Errorf("added.ValueA: expected nil, got %v", c.ValueA)
		}
		if c.ValueB != "new-value" {
			t.Errorf("added.ValueB: expected 'new-value', got %v", c.ValueB)
		}
	} else {
		t.Error("expected 'added' change")
	}
}

// ===========================================================================
// TestDiffMapsEmpty
// ===========================================================================

func TestDiffMapsEmpty(t *testing.T) {
	// Both nil/empty.
	changes := DiffMaps(nil, nil)
	if len(changes) != 0 {
		t.Errorf("expected 0 changes for nil maps, got %d", len(changes))
	}

	changes = DiffMaps(map[string]any{}, map[string]any{})
	if len(changes) != 0 {
		t.Errorf("expected 0 changes for empty maps, got %d", len(changes))
	}

	// One nil, one with data.
	changes = DiffMaps(nil, map[string]any{"key": "value"})
	if len(changes) != 1 {
		t.Errorf("expected 1 change, got %d", len(changes))
	}

	changes = DiffMaps(map[string]any{"key": "value"}, nil)
	if len(changes) != 1 {
		t.Errorf("expected 1 change, got %d", len(changes))
	}
}

// ===========================================================================
// TestCompareExecutions
// ===========================================================================

func TestCompareExecutions(t *testing.T) {
	es := NewInMemoryEventStore()
	ctx := context.Background()

	// Create execution A: validate -> process -> complete
	execA := uuid.New()
	appendStarted(t, es, execA, "order-pipeline", "tenant-1")

	appendStepStarted(t, es, execA, "validate")
	if err := es.Append(ctx, execA, EventStepOutputRecorded, map[string]any{
		"step_name": "validate",
		"output":    map[string]any{"valid": true, "score": 95.0},
	}); err != nil {
		t.Fatal(err)
	}
	appendStepCompleted(t, es, execA, "validate")

	appendStepStarted(t, es, execA, "process")
	if err := es.Append(ctx, execA, EventStepOutputRecorded, map[string]any{
		"step_name": "process",
		"output":    map[string]any{"order_id": "123", "status": "processed"},
	}); err != nil {
		t.Fatal(err)
	}
	appendStepCompleted(t, es, execA, "process")

	appendCompleted(t, es, execA)

	// Create execution B: validate (different output) -> process (same output) -> notify (new step)
	execB := uuid.New()
	appendStarted(t, es, execB, "order-pipeline", "tenant-1")

	appendStepStarted(t, es, execB, "validate")
	if err := es.Append(ctx, execB, EventStepOutputRecorded, map[string]any{
		"step_name": "validate",
		"output":    map[string]any{"valid": false, "score": 30.0},
	}); err != nil {
		t.Fatal(err)
	}
	appendStepCompleted(t, es, execB, "validate")

	appendStepStarted(t, es, execB, "process")
	if err := es.Append(ctx, execB, EventStepOutputRecorded, map[string]any{
		"step_name": "process",
		"output":    map[string]any{"order_id": "123", "status": "processed"},
	}); err != nil {
		t.Fatal(err)
	}
	appendStepCompleted(t, es, execB, "process")

	appendStepStarted(t, es, execB, "notify")
	if err := es.Append(ctx, execB, EventStepOutputRecorded, map[string]any{
		"step_name": "notify",
		"output":    map[string]any{"sent": true},
	}); err != nil {
		t.Fatal(err)
	}
	appendStepCompleted(t, es, execB, "notify")

	appendCompleted(t, es, execB)

	// Compare.
	calc := NewDiffCalculator(es)
	diff, err := calc.Compare(ctx, execA, execB)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}

	if diff.ExecutionA != execA {
		t.Errorf("expected ExecutionA %v, got %v", execA, diff.ExecutionA)
	}
	if diff.ExecutionB != execB {
		t.Errorf("expected ExecutionB %v, got %v", execB, diff.ExecutionB)
	}

	// Summary.
	if diff.Summary.TotalSteps != 3 {
		t.Errorf("expected 3 total steps, got %d", diff.Summary.TotalSteps)
	}
	if diff.Summary.SameSteps != 1 {
		t.Errorf("expected 1 same step (process), got %d", diff.Summary.SameSteps)
	}
	if diff.Summary.DiffSteps != 1 {
		t.Errorf("expected 1 different step (validate), got %d", diff.Summary.DiffSteps)
	}
	if diff.Summary.AddedSteps != 1 {
		t.Errorf("expected 1 added step (notify), got %d", diff.Summary.AddedSteps)
	}
	if diff.Summary.RemovedSteps != 0 {
		t.Errorf("expected 0 removed steps, got %d", diff.Summary.RemovedSteps)
	}

	// Verify step diffs.
	stepDiffMap := make(map[string]StepDiff)
	for _, sd := range diff.StepDiffs {
		stepDiffMap[sd.StepName] = sd
	}

	// notify should be "added".
	if sd, ok := stepDiffMap["notify"]; ok {
		if sd.Status != "added" {
			t.Errorf("notify: expected status 'added', got %q", sd.Status)
		}
		if sd.OutputA != nil {
			t.Error("notify: expected nil OutputA")
		}
		if sd.OutputB == nil {
			t.Error("notify: expected non-nil OutputB")
		}
	} else {
		t.Error("expected 'notify' in step diffs")
	}

	// validate should be "different".
	if sd, ok := stepDiffMap["validate"]; ok {
		if sd.Status != "different" {
			t.Errorf("validate: expected status 'different', got %q", sd.Status)
		}
		if len(sd.Changes) != 2 {
			t.Errorf("validate: expected 2 changes, got %d", len(sd.Changes))
		}
	} else {
		t.Error("expected 'validate' in step diffs")
	}

	// process should be "same".
	if sd, ok := stepDiffMap["process"]; ok {
		if sd.Status != "same" {
			t.Errorf("process: expected status 'same', got %q", sd.Status)
		}
		if len(sd.Changes) != 0 {
			t.Errorf("process: expected 0 changes, got %d", len(sd.Changes))
		}
	} else {
		t.Error("expected 'process' in step diffs")
	}
}

// ===========================================================================
// TestCompareExecutions_NotFound
// ===========================================================================

func TestCompareExecutions_NotFound(t *testing.T) {
	es := NewInMemoryEventStore()
	ctx := context.Background()
	calc := NewDiffCalculator(es)

	// Both missing.
	_, err := calc.Compare(ctx, uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error for missing executions")
	}

	// Only A exists.
	execA := uuid.New()
	appendStarted(t, es, execA, "pipeline", "")
	appendCompleted(t, es, execA)

	_, err = calc.Compare(ctx, execA, uuid.New())
	if err == nil {
		t.Fatal("expected error when execution B is missing")
	}
}

// ===========================================================================
// TestCompareExecutions_RemovedStep
// ===========================================================================

func TestCompareExecutions_RemovedStep(t *testing.T) {
	es := NewInMemoryEventStore()
	ctx := context.Background()

	// Execution A has step1 and step2.
	execA := uuid.New()
	appendStarted(t, es, execA, "pipeline", "")
	appendStepStarted(t, es, execA, "step1")
	if err := es.Append(ctx, execA, EventStepOutputRecorded, map[string]any{
		"step_name": "step1",
		"output":    map[string]any{"result": "ok"},
	}); err != nil {
		t.Fatal(err)
	}
	appendStepCompleted(t, es, execA, "step1")
	appendStepStarted(t, es, execA, "step2")
	if err := es.Append(ctx, execA, EventStepOutputRecorded, map[string]any{
		"step_name": "step2",
		"output":    map[string]any{"result": "done"},
	}); err != nil {
		t.Fatal(err)
	}
	appendStepCompleted(t, es, execA, "step2")
	appendCompleted(t, es, execA)

	// Execution B only has step1.
	execB := uuid.New()
	appendStarted(t, es, execB, "pipeline", "")
	appendStepStarted(t, es, execB, "step1")
	if err := es.Append(ctx, execB, EventStepOutputRecorded, map[string]any{
		"step_name": "step1",
		"output":    map[string]any{"result": "ok"},
	}); err != nil {
		t.Fatal(err)
	}
	appendStepCompleted(t, es, execB, "step1")
	appendCompleted(t, es, execB)

	calc := NewDiffCalculator(es)
	diff, err := calc.Compare(ctx, execA, execB)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}

	if diff.Summary.RemovedSteps != 1 {
		t.Errorf("expected 1 removed step, got %d", diff.Summary.RemovedSteps)
	}
	if diff.Summary.SameSteps != 1 {
		t.Errorf("expected 1 same step, got %d", diff.Summary.SameSteps)
	}

	// Verify step2 is marked as removed.
	for _, sd := range diff.StepDiffs {
		if sd.StepName == "step2" {
			if sd.Status != "removed" {
				t.Errorf("step2: expected status 'removed', got %q", sd.Status)
			}
			if sd.OutputA == nil {
				t.Error("step2: expected non-nil OutputA")
			}
			if sd.OutputB != nil {
				t.Error("step2: expected nil OutputB")
			}
		}
	}
}
