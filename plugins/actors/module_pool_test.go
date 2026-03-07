package actors

import (
	"testing"
)

func TestActorPoolModule_AutoManaged(t *testing.T) {
	cfg := map[string]any{
		"system":      "my-actors",
		"mode":        "auto-managed",
		"idleTimeout": "10m",
		"routing":     "sticky",
		"routingKey":  "order_id",
	}
	mod, err := NewActorPoolModule("order-pool", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mod.Name() != "order-pool" {
		t.Errorf("expected name 'order-pool', got %q", mod.Name())
	}
	if mod.mode != "auto-managed" {
		t.Errorf("expected mode 'auto-managed', got %q", mod.mode)
	}
	if mod.routing != "sticky" {
		t.Errorf("expected routing 'sticky', got %q", mod.routing)
	}
}

func TestActorPoolModule_Permanent(t *testing.T) {
	cfg := map[string]any{
		"system":   "my-actors",
		"mode":     "permanent",
		"poolSize": 5,
		"routing":  "round-robin",
	}
	mod, err := NewActorPoolModule("worker-pool", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mod.mode != "permanent" {
		t.Errorf("expected mode 'permanent', got %q", mod.mode)
	}
	if mod.poolSize != 5 {
		t.Errorf("expected poolSize 5, got %d", mod.poolSize)
	}
}

func TestActorPoolModule_RequiresSystem(t *testing.T) {
	cfg := map[string]any{
		"mode": "auto-managed",
	}
	_, err := NewActorPoolModule("test", cfg)
	if err == nil {
		t.Fatal("expected error for missing system")
	}
}

func TestActorPoolModule_InvalidMode(t *testing.T) {
	cfg := map[string]any{
		"system": "my-actors",
		"mode":   "invalid",
	}
	_, err := NewActorPoolModule("test", cfg)
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestActorPoolModule_InvalidRouting(t *testing.T) {
	cfg := map[string]any{
		"system":  "my-actors",
		"routing": "invalid",
	}
	_, err := NewActorPoolModule("test", cfg)
	if err == nil {
		t.Fatal("expected error for invalid routing")
	}
}

func TestActorPoolModule_StickyRequiresRoutingKey(t *testing.T) {
	cfg := map[string]any{
		"system":  "my-actors",
		"routing": "sticky",
	}
	_, err := NewActorPoolModule("test", cfg)
	if err == nil {
		t.Fatal("expected error: sticky routing requires routingKey")
	}
}

func TestActorPoolModule_DefaultValues(t *testing.T) {
	cfg := map[string]any{
		"system": "my-actors",
	}
	mod, err := NewActorPoolModule("test", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mod.mode != "auto-managed" {
		t.Errorf("expected default mode 'auto-managed', got %q", mod.mode)
	}
	if mod.routing != "round-robin" {
		t.Errorf("expected default routing 'round-robin', got %q", mod.routing)
	}
}
