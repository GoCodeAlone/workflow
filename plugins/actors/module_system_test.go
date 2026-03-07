package actors

import (
	"context"
	"testing"
)

func TestActorSystemModule_LocalMode(t *testing.T) {
	// No cluster config = local mode
	cfg := map[string]any{
		"shutdownTimeout": "5s",
	}
	mod, err := NewActorSystemModule("test-system", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mod.Name() != "test-system" {
		t.Errorf("expected name 'test-system', got %q", mod.Name())
	}

	ctx := context.Background()
	if err := mod.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	sys := mod.ActorSystem()
	if sys == nil {
		t.Fatal("expected non-nil ActorSystem")
	}

	if err := mod.Stop(ctx); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
}

func TestActorSystemModule_MissingName(t *testing.T) {
	_, err := NewActorSystemModule("", map[string]any{})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestActorSystemModule_InvalidShutdownTimeout(t *testing.T) {
	cfg := map[string]any{
		"shutdownTimeout": "not-a-duration",
	}
	_, err := NewActorSystemModule("test", cfg)
	if err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

func TestActorSystemModule_DefaultConfig(t *testing.T) {
	mod, err := NewActorSystemModule("test-defaults", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mod.shutdownTimeout.Seconds() != 30 {
		t.Errorf("expected 30s default shutdown timeout, got %v", mod.shutdownTimeout)
	}
}
