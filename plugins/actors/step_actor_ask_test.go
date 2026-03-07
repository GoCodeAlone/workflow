package actors

import (
	"testing"
)

func TestActorAskStep_RequiresPool(t *testing.T) {
	_, err := NewActorAskStepFactory()(
		"test-ask", map[string]any{}, nil,
	)
	if err == nil {
		t.Fatal("expected error for missing pool")
	}
}

func TestActorAskStep_RequiresMessage(t *testing.T) {
	_, err := NewActorAskStepFactory()(
		"test-ask",
		map[string]any{"pool": "my-pool"},
		nil,
	)
	if err == nil {
		t.Fatal("expected error for missing message")
	}
}

func TestActorAskStep_RequiresMessageType(t *testing.T) {
	_, err := NewActorAskStepFactory()(
		"test-ask",
		map[string]any{
			"pool": "my-pool",
			"message": map[string]any{
				"payload": map[string]any{},
			},
		},
		nil,
	)
	if err == nil {
		t.Fatal("expected error for missing message.type")
	}
}

func TestActorAskStep_DefaultTimeout(t *testing.T) {
	step, err := NewActorAskStepFactory()(
		"test-ask",
		map[string]any{
			"pool": "my-pool",
			"message": map[string]any{
				"type":    "GetStatus",
				"payload": map[string]any{},
			},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	askStep := step.(*ActorAskStep)
	if askStep.timeout.Seconds() != 10 {
		t.Errorf("expected 10s default timeout, got %v", askStep.timeout)
	}
}

func TestActorAskStep_CustomTimeout(t *testing.T) {
	step, err := NewActorAskStepFactory()(
		"test-ask",
		map[string]any{
			"pool":    "my-pool",
			"timeout": "30s",
			"message": map[string]any{
				"type":    "GetStatus",
				"payload": map[string]any{},
			},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	askStep := step.(*ActorAskStep)
	if askStep.timeout.Seconds() != 30 {
		t.Errorf("expected 30s timeout, got %v", askStep.timeout)
	}
}

func TestActorAskStep_InvalidTimeout(t *testing.T) {
	_, err := NewActorAskStepFactory()(
		"test-ask",
		map[string]any{
			"pool":    "my-pool",
			"timeout": "not-a-duration",
			"message": map[string]any{
				"type":    "GetStatus",
				"payload": map[string]any{},
			},
		},
		nil,
	)
	if err == nil {
		t.Fatal("expected error for invalid timeout")
	}
}
