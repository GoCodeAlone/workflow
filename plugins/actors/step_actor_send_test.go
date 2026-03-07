package actors

import (
	"testing"
)

func TestActorSendStep_RequiresPool(t *testing.T) {
	_, err := NewActorSendStepFactory()(
		"test-send", map[string]any{}, nil,
	)
	if err == nil {
		t.Fatal("expected error for missing pool")
	}
}

func TestActorSendStep_RequiresMessage(t *testing.T) {
	_, err := NewActorSendStepFactory()(
		"test-send",
		map[string]any{"pool": "my-pool"},
		nil,
	)
	if err == nil {
		t.Fatal("expected error for missing message")
	}
}

func TestActorSendStep_RequiresMessageType(t *testing.T) {
	_, err := NewActorSendStepFactory()(
		"test-send",
		map[string]any{
			"pool": "my-pool",
			"message": map[string]any{
				"payload": map[string]any{},
			},
		},
		nil,
	)
	if err == nil {
		t.Fatal("expected error for missing message type")
	}
}

func TestActorSendStep_ValidConfig(t *testing.T) {
	step, err := NewActorSendStepFactory()(
		"test-send",
		map[string]any{
			"pool": "my-pool",
			"message": map[string]any{
				"type":    "OrderPlaced",
				"payload": map[string]any{"id": "123"},
			},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "test-send" {
		t.Errorf("expected name 'test-send', got %q", step.Name())
	}
}
