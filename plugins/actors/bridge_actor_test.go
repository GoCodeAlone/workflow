package actors

import (
	"context"
	"testing"
	"time"

	"github.com/tochemey/goakt/v4/actor"
)

func setupTestSystem(t *testing.T) (actor.ActorSystem, func()) {
	t.Helper()
	sys, err := actor.NewActorSystem("test-bridge",
		actor.WithLoggingDisabled(),
	)
	if err != nil {
		t.Fatalf("create system: %v", err)
	}
	ctx := context.Background()
	if err := sys.Start(ctx); err != nil {
		t.Fatalf("start system: %v", err)
	}
	return sys, func() {
		_ = sys.Stop(ctx)
	}
}

func TestBridgeActor_ReceiveMessage(t *testing.T) {
	sys, cleanup := setupTestSystem(t)
	defer cleanup()

	handlers := map[string]*HandlerPipeline{
		"greet": {
			Description: "simple greeting",
			Steps:       []map[string]any{},
		},
	}

	ba := NewBridgeActor("test-pool", "actor-1", handlers, nil, nil)
	pid, err := sys.Spawn(context.Background(), "actor-1", ba)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	resp, err := actor.Ask(context.Background(), pid, &ActorMessage{
		Type:    "greet",
		Payload: map[string]any{"name": "world"},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("ask: %v", err)
	}

	ar, ok := resp.(*ActorResponse)
	if !ok {
		t.Fatalf("expected *ActorResponse, got %T", resp)
	}
	if ar.Error != "" {
		t.Errorf("unexpected error in response: %s", ar.Error)
	}
	if ar.Type != "greet" {
		t.Errorf("expected type 'greet', got %q", ar.Type)
	}
}

func TestBridgeActor_UnknownMessageType(t *testing.T) {
	sys, cleanup := setupTestSystem(t)
	defer cleanup()

	ba := NewBridgeActor("test-pool", "actor-2", map[string]*HandlerPipeline{}, nil, nil)
	pid, err := sys.Spawn(context.Background(), "actor-2", ba)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	resp, err := actor.Ask(context.Background(), pid, &ActorMessage{
		Type: "unknown-type",
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("ask: %v", err)
	}

	ar, ok := resp.(*ActorResponse)
	if !ok {
		t.Fatalf("expected *ActorResponse, got %T", resp)
	}
	if ar.Error == "" {
		t.Error("expected error for unknown message type")
	}
}

func TestBridgeActor_StatePersistsAcrossMessages(t *testing.T) {
	sys, cleanup := setupTestSystem(t)
	defer cleanup()

	handlers := map[string]*HandlerPipeline{
		"update": {
			Steps: []map[string]any{},
		},
	}

	ba := NewBridgeActor("test-pool", "actor-3", handlers, nil, nil)
	pid, err := sys.Spawn(context.Background(), "actor-3", ba)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	// First message — sends payload that should be merged into state.
	_, err = actor.Ask(context.Background(), pid, &ActorMessage{
		Type:    "update",
		Payload: map[string]any{"counter": 1},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("first ask: %v", err)
	}

	// Second message — with empty payload.
	resp, err := actor.Ask(context.Background(), pid, &ActorMessage{
		Type:    "update",
		Payload: map[string]any{"counter": 2},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("second ask: %v", err)
	}

	ar, ok := resp.(*ActorResponse)
	if !ok {
		t.Fatalf("expected *ActorResponse, got %T", resp)
	}
	if ar.Error != "" {
		t.Errorf("unexpected error: %s", ar.Error)
	}

	// State should have the latest counter value.
	if ba.State()["counter"] != 2 {
		t.Errorf("expected state counter=2, got %v", ba.State()["counter"])
	}
}
