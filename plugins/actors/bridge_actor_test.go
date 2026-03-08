package actors

import (
	"context"
	"testing"
	"time"

	"github.com/tochemey/goakt/v4/actor"
)

func TestBridgeGrain_ReceiveAndStatePersistence(t *testing.T) {
	ctx := context.Background()

	handlers := map[string]*HandlerPipeline{
		"SetValue": {
			Steps: []map[string]any{
				{
					"name": "set",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{
							"value": "{{ .message.payload.value }}",
						},
					},
				},
			},
		},
		"GetValue": {
			Steps: []map[string]any{
				{
					"name": "get",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{
							"value": "{{ .state.value }}",
						},
					},
				},
			},
		},
	}

	sys, err := actor.NewActorSystem("test-grain-sys",
		actor.WithShutdownTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("failed to create actor system: %v", err)
	}
	if err := sys.Start(ctx); err != nil {
		t.Fatalf("failed to start actor system: %v", err)
	}
	defer sys.Stop(ctx) //nolint:errcheck

	factory := func(_ context.Context) (actor.Grain, error) {
		return &BridgeGrain{
			poolName: "test-pool",
			handlers: handlers,
		}, nil
	}

	grainID, err := sys.GrainIdentity(ctx, "grain-1", factory,
		actor.WithGrainDeactivateAfter(10*time.Minute),
	)
	if err != nil {
		t.Fatalf("failed to get grain identity: %v", err)
	}

	// SetValue
	if _, err := sys.AskGrain(ctx, grainID, &ActorMessage{
		Type:    "SetValue",
		Payload: map[string]any{"value": "hello"},
	}, 5*time.Second); err != nil {
		t.Fatalf("SetValue failed: %v", err)
	}

	// GetValue — state should persist
	resp, err := sys.AskGrain(ctx, grainID, &ActorMessage{
		Type:    "GetValue",
		Payload: map[string]any{},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("GetValue failed: %v", err)
	}

	result, ok := resp.(map[string]any)
	if !ok {
		t.Fatalf("expected map response, got %T", resp)
	}
	if result["value"] != "hello" {
		t.Errorf("expected value=hello from state, got %v", result["value"])
	}
}

func TestBridgeActor_ReceiveMessage(t *testing.T) {
	ctx := context.Background()

	// Create a simple handler that echoes the message type
	handlers := map[string]*HandlerPipeline{
		"Ping": {
			Steps: []map[string]any{
				{
					"name": "echo",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{
							"pong": "true",
						},
					},
				},
			},
		},
	}

	bridge := &BridgeActor{
		poolName: "test-pool",
		identity: "test-1",
		state:    map[string]any{},
		handlers: handlers,
	}

	// Create an actor system for testing
	sys, err := actor.NewActorSystem("test-bridge",
		actor.WithShutdownTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("failed to create actor system: %v", err)
	}
	if err := sys.Start(ctx); err != nil {
		t.Fatalf("failed to start actor system: %v", err)
	}
	defer sys.Stop(ctx) //nolint:errcheck

	pid, err := sys.Spawn(ctx, "test-actor", bridge)
	if err != nil {
		t.Fatalf("failed to spawn bridge actor: %v", err)
	}

	// Ask the actor
	msg := &ActorMessage{
		Type:    "Ping",
		Payload: map[string]any{"data": "hello"},
	}
	resp, err := actor.Ask(ctx, pid, msg, 5*time.Second)
	if err != nil {
		t.Fatalf("ask failed: %v", err)
	}

	result, ok := resp.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any response, got %T", resp)
	}
	if result["pong"] != "true" {
		t.Errorf("expected pong=true, got %v", result["pong"])
	}
}

func TestBridgeActor_UnknownMessageType(t *testing.T) {
	ctx := context.Background()

	bridge := &BridgeActor{
		poolName: "test-pool",
		identity: "test-1",
		state:    map[string]any{},
		handlers: map[string]*HandlerPipeline{},
	}

	sys, err := actor.NewActorSystem("test-unknown",
		actor.WithShutdownTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("failed to create actor system: %v", err)
	}
	if err := sys.Start(ctx); err != nil {
		t.Fatalf("failed to start actor system: %v", err)
	}
	defer sys.Stop(ctx) //nolint:errcheck

	pid, err := sys.Spawn(ctx, "test-actor", bridge)
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}

	msg := &ActorMessage{Type: "Unknown", Payload: map[string]any{}}
	_, err = actor.Ask(ctx, pid, msg, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for unknown message type, got nil")
	}
}

func TestBridgeActor_StatePersistsAcrossMessages(t *testing.T) {
	ctx := context.Background()

	handlers := map[string]*HandlerPipeline{
		"SetName": {
			Steps: []map[string]any{
				{
					"name": "set",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{
							"name": "{{ .message.payload.name }}",
						},
					},
				},
			},
		},
		"GetName": {
			Steps: []map[string]any{
				{
					"name": "get",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{
							"name": "{{ .state.name }}",
						},
					},
				},
			},
		},
	}

	bridge := &BridgeActor{
		poolName: "test-pool",
		identity: "test-1",
		state:    map[string]any{},
		handlers: handlers,
	}

	sys, err := actor.NewActorSystem("test-state",
		actor.WithShutdownTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("failed to create actor system: %v", err)
	}
	if err := sys.Start(ctx); err != nil {
		t.Fatalf("failed to start actor system: %v", err)
	}
	defer sys.Stop(ctx) //nolint:errcheck

	pid, err := sys.Spawn(ctx, "test-actor", bridge)
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}

	// Send SetName
	_, err = actor.Ask(ctx, pid, &ActorMessage{
		Type:    "SetName",
		Payload: map[string]any{"name": "Alice"},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("SetName failed: %v", err)
	}

	// Send GetName — should return state from previous message
	resp, err := actor.Ask(ctx, pid, &ActorMessage{
		Type:    "GetName",
		Payload: map[string]any{},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("GetName failed: %v", err)
	}

	result, ok := resp.(map[string]any)
	if !ok {
		t.Fatalf("expected map response, got %T", resp)
	}
	if result["name"] != "Alice" {
		t.Errorf("expected name=Alice from state, got %v", result["name"])
	}
}
