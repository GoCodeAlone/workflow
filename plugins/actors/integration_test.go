package actors

import (
	"context"
	"testing"
	"time"

	"github.com/tochemey/goakt/v4/actor"
)

func TestIntegration_FullActorLifecycle(t *testing.T) {
	ctx := context.Background()

	// 1. Create actor system module
	sysMod, err := NewActorSystemModule("test-system", map[string]any{
		"shutdownTimeout": "5s",
	})
	if err != nil {
		t.Fatalf("failed to create system module: %v", err)
	}

	// Start system
	if err := sysMod.Start(ctx); err != nil {
		t.Fatalf("failed to start system: %v", err)
	}
	defer sysMod.Stop(ctx) //nolint:errcheck

	sys := sysMod.ActorSystem()
	if sys == nil {
		t.Fatal("actor system is nil")
	}

	// 2. Create a bridge actor with handlers
	handlers := map[string]*HandlerPipeline{
		"Increment": {
			Description: "Increment a counter",
			Steps: []map[string]any{
				{
					"name": "inc",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{
							"count": "incremented",
						},
					},
				},
			},
		},
		"GetCount": {
			Description: "Get the counter value",
			Steps: []map[string]any{
				{
					"name": "get",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{
							"count": "{{ .state.count }}",
						},
					},
				},
			},
		},
	}

	bridge := &BridgeActor{
		poolName: "counters",
		identity: "counter-1",
		state:    map[string]any{"count": "0"},
		handlers: handlers,
	}

	// 3. Spawn the actor
	pid, err := sys.Spawn(ctx, "counter-1", bridge)
	if err != nil {
		t.Fatalf("failed to spawn actor: %v", err)
	}

	// 4. Send Increment message
	resp, err := actor.Ask(ctx, pid, &ActorMessage{
		Type:    "Increment",
		Payload: map[string]any{},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("Increment failed: %v", err)
	}
	result, ok := resp.(map[string]any)
	if !ok {
		t.Fatalf("expected map response, got %T", resp)
	}
	if result["count"] != "incremented" {
		t.Errorf("expected count=incremented, got %v", result["count"])
	}

	// 5. Send GetCount — should reflect state from Increment
	resp, err = actor.Ask(ctx, pid, &ActorMessage{
		Type:    "GetCount",
		Payload: map[string]any{},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("GetCount failed: %v", err)
	}
	result, ok = resp.(map[string]any)
	if !ok {
		t.Fatalf("expected map response, got %T", resp)
	}
	if result["count"] != "incremented" {
		t.Errorf("expected count=incremented from state, got %v", result["count"])
	}

	// 6. Verify actor is running
	found, err := sys.ActorExists(ctx, "counter-1")
	if err != nil {
		t.Fatalf("ActorExists failed: %v", err)
	}
	if !found {
		t.Error("expected actor to exist")
	}
}

func TestIntegration_MultipleActorsIndependentState(t *testing.T) {
	ctx := context.Background()

	sysMod, err := NewActorSystemModule("test-multi", map[string]any{})
	if err != nil {
		t.Fatalf("failed to create system: %v", err)
	}
	if err := sysMod.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer sysMod.Stop(ctx) //nolint:errcheck

	sys := sysMod.ActorSystem()

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

	// Spawn two independent actors
	actor1 := &BridgeActor{poolName: "kv", identity: "a", state: map[string]any{}, handlers: handlers}
	actor2 := &BridgeActor{poolName: "kv", identity: "b", state: map[string]any{}, handlers: handlers}

	pid1, _ := sys.Spawn(ctx, "actor-a", actor1)
	pid2, _ := sys.Spawn(ctx, "actor-b", actor2)

	// Set different values
	if _, err := actor.Ask(ctx, pid1, &ActorMessage{Type: "SetValue", Payload: map[string]any{"value": "alpha"}}, 5*time.Second); err != nil {
		t.Fatalf("SetValue for actor-a failed: %v", err)
	}
	if _, err := actor.Ask(ctx, pid2, &ActorMessage{Type: "SetValue", Payload: map[string]any{"value": "beta"}}, 5*time.Second); err != nil {
		t.Fatalf("SetValue for actor-b failed: %v", err)
	}

	// Verify independent state
	resp1, err := actor.Ask(ctx, pid1, &ActorMessage{Type: "GetValue", Payload: map[string]any{}}, 5*time.Second)
	if err != nil {
		t.Fatalf("GetValue for actor-a failed: %v", err)
	}
	resp2, err := actor.Ask(ctx, pid2, &ActorMessage{Type: "GetValue", Payload: map[string]any{}}, 5*time.Second)
	if err != nil {
		t.Fatalf("GetValue for actor-b failed: %v", err)
	}

	r1, ok := resp1.(map[string]any)
	if !ok {
		t.Fatalf("expected map response for actor-a, got %T", resp1)
	}
	r2, ok := resp2.(map[string]any)
	if !ok {
		t.Fatalf("expected map response for actor-b, got %T", resp2)
	}

	if r1["value"] != "alpha" {
		t.Errorf("actor-a: expected value=alpha, got %v", r1["value"])
	}
	if r2["value"] != "beta" {
		t.Errorf("actor-b: expected value=beta, got %v", r2["value"])
	}
}
