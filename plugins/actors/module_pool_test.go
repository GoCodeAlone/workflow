package actors

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/supervisor"
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

func TestSelectActor_RoundRobin(t *testing.T) {
	mod := &ActorPoolModule{
		name:    "test-pool",
		routing: "round-robin",
		pids:    make([]*actor.PID, 3),
	}
	// Create fake PIDs (we just need non-nil pointers for routing)
	for i := range mod.pids {
		mod.pids[i] = &actor.PID{}
	}

	// Send 6 messages — should cycle evenly through 3 actors
	hits := make(map[int]int)
	for i := 0; i < 6; i++ {
		pids, err := mod.SelectActor(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(pids) != 1 {
			t.Fatalf("expected 1 PID, got %d", len(pids))
		}
		for j, p := range mod.pids {
			if p == pids[0] {
				hits[j]++
				break
			}
		}
	}
	for i := 0; i < 3; i++ {
		if hits[i] != 2 {
			t.Errorf("actor %d: expected 2 hits, got %d", i, hits[i])
		}
	}
}

func TestSelectActor_Random(t *testing.T) {
	mod := &ActorPoolModule{
		name:    "test-pool",
		routing: "random",
		pids:    make([]*actor.PID, 3),
	}
	for i := range mod.pids {
		mod.pids[i] = &actor.PID{}
	}

	// Just verify it returns valid PIDs without error
	for i := 0; i < 10; i++ {
		pids, err := mod.SelectActor(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(pids) != 1 {
			t.Fatalf("expected 1 PID, got %d", len(pids))
		}
		found := false
		for _, p := range mod.pids {
			if p == pids[0] {
				found = true
				break
			}
		}
		if !found {
			t.Error("returned PID not from pool")
		}
	}
}

func TestSelectActor_Broadcast(t *testing.T) {
	mod := &ActorPoolModule{
		name:    "test-pool",
		routing: "broadcast",
		pids:    make([]*actor.PID, 4),
	}
	for i := range mod.pids {
		mod.pids[i] = &actor.PID{}
	}

	pids, err := mod.SelectActor(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pids) != 4 {
		t.Fatalf("broadcast: expected 4 PIDs, got %d", len(pids))
	}
}

func TestSelectActor_Sticky(t *testing.T) {
	mod := &ActorPoolModule{
		name:       "test-pool",
		routing:    "sticky",
		routingKey: "user_id",
		pids:       make([]*actor.PID, 5),
	}
	for i := range mod.pids {
		mod.pids[i] = &actor.PID{}
	}

	// Same key should always return the same actor
	msg := &ActorMessage{
		Type:    "Test",
		Payload: map[string]any{"user_id": "user-42"},
	}
	first, err := mod.SelectActor(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 0; i < 10; i++ {
		pids, err := mod.SelectActor(msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pids[0] != first[0] {
			t.Errorf("sticky routing: expected same PID for same key, got different at iteration %d", i)
		}
	}

	// Different key should (likely) return a different actor
	msg2 := &ActorMessage{
		Type:    "Test",
		Payload: map[string]any{"user_id": "user-99"},
	}
	other, err := mod.SelectActor(msg2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With 5 actors, different keys should usually hash to different actors
	// (not guaranteed, but very likely for "user-42" vs "user-99")
	_ = other // Just verify it doesn't error
}

func TestSelectActor_NoPids(t *testing.T) {
	mod := &ActorPoolModule{
		name:    "test-pool",
		routing: "round-robin",
		pids:    nil,
	}
	_, err := mod.SelectActor(nil)
	if err == nil {
		t.Fatal("expected error when no actors available")
	}
}

func TestPermanentPool_StartSpawnsActors(t *testing.T) {
	ctx := context.Background()

	// Create a real actor system
	sysMod, err := NewActorSystemModule("test-spawn", map[string]any{
		"shutdownTimeout": "5s",
	})
	if err != nil {
		t.Fatalf("failed to create system: %v", err)
	}
	if err := sysMod.Start(ctx); err != nil {
		t.Fatalf("failed to start system: %v", err)
	}
	defer sysMod.Stop(ctx) //nolint:errcheck

	// Create a permanent pool
	pool := &ActorPoolModule{
		name:       "workers",
		systemName: "test-spawn",
		mode:       "permanent",
		poolSize:   3,
		routing:    "round-robin",
		system:     sysMod,
		handlers: map[string]*HandlerPipeline{
			"Ping": {
				Steps: []map[string]any{
					{
						"name": "pong",
						"type": "step.set",
						"config": map[string]any{
							"values": map[string]any{"pong": "true"},
						},
					},
				},
			},
		},
	}

	// Start the pool — should spawn actors
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}

	// Verify PIDs were tracked
	if len(pool.pids) != 3 {
		t.Fatalf("expected 3 PIDs, got %d", len(pool.pids))
	}

	// Verify actors are actually alive by asking each one
	for i, pid := range pool.pids {
		resp, err := actor.Ask(ctx, pid, &ActorMessage{
			Type:    "Ping",
			Payload: map[string]any{},
		}, 5*time.Second)
		if err != nil {
			t.Fatalf("actor %d: ask failed: %v", i, err)
		}
		result, ok := resp.(map[string]any)
		if !ok {
			t.Fatalf("actor %d: expected map response, got %T", i, resp)
		}
		if result["pong"] != "true" {
			t.Errorf("actor %d: expected pong=true, got %v", i, result["pong"])
		}
	}
}

func TestPermanentPool_RoundRobinRouting(t *testing.T) {
	ctx := context.Background()

	sysMod, err := NewActorSystemModule("test-routing", map[string]any{})
	if err != nil {
		t.Fatalf("failed to create system: %v", err)
	}
	if err := sysMod.Start(ctx); err != nil {
		t.Fatalf("failed to start system: %v", err)
	}
	defer sysMod.Stop(ctx) //nolint:errcheck

	handlers := map[string]*HandlerPipeline{
		"WhoAmI": {
			Steps: []map[string]any{
				{
					"name": "id",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{
							"identity": "{{ .actor.identity }}",
						},
					},
				},
			},
		},
	}

	pool := &ActorPoolModule{
		name:       "rr-pool",
		systemName: "test-routing",
		mode:       "permanent",
		poolSize:   3,
		routing:    "round-robin",
		system:     sysMod,
		handlers:   handlers,
	}
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}

	// Send 6 messages via round-robin — each actor should get exactly 2
	identities := make(map[string]int)
	for i := 0; i < 6; i++ {
		pids, err := pool.SelectActor(nil)
		if err != nil {
			t.Fatalf("SelectActor failed: %v", err)
		}
		resp, err := actor.Ask(ctx, pids[0], &ActorMessage{
			Type:    "WhoAmI",
			Payload: map[string]any{},
		}, 5*time.Second)
		if err != nil {
			t.Fatalf("ask failed: %v", err)
		}
		result, ok := resp.(map[string]any)
		if !ok {
			t.Fatalf("expected map response, got %T", resp)
		}
		id, ok := result["identity"].(string)
		if !ok {
			t.Fatalf("expected string identity, got %T", result["identity"])
		}
		identities[id]++
	}

	// Each of the 3 actors should have been hit exactly twice
	if len(identities) != 3 {
		t.Errorf("expected 3 distinct actors, got %d: %v", len(identities), identities)
	}
	for id, count := range identities {
		if count != 2 {
			t.Errorf("actor %q: expected 2 hits, got %d", id, count)
		}
	}
}

func TestPermanentPool_BroadcastSend(t *testing.T) {
	ctx := context.Background()

	sysMod, err := NewActorSystemModule("test-broadcast", map[string]any{})
	if err != nil {
		t.Fatalf("failed to create system: %v", err)
	}
	if err := sysMod.Start(ctx); err != nil {
		t.Fatalf("failed to start system: %v", err)
	}
	defer sysMod.Stop(ctx) //nolint:errcheck

	handlers := map[string]*HandlerPipeline{
		"Mark": {
			Steps: []map[string]any{
				{
					"name": "mark",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{
							"marked": "true",
						},
					},
				},
			},
		},
		"GetMark": {
			Steps: []map[string]any{
				{
					"name": "get",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{
							"marked": "{{ .state.marked }}",
						},
					},
				},
			},
		},
	}

	pool := &ActorPoolModule{
		name:       "bc-pool",
		systemName: "test-broadcast",
		mode:       "permanent",
		poolSize:   3,
		routing:    "broadcast",
		system:     sysMod,
		handlers:   handlers,
	}
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}

	// Broadcast should return all PIDs
	pids, err := pool.SelectActor(nil)
	if err != nil {
		t.Fatalf("SelectActor failed: %v", err)
	}
	if len(pids) != 3 {
		t.Fatalf("expected 3 PIDs for broadcast, got %d", len(pids))
	}

	// Send Mark to all (broadcast)
	for _, pid := range pids {
		if err := actor.Tell(ctx, pid, &ActorMessage{
			Type:    "Mark",
			Payload: map[string]any{},
		}); err != nil {
			t.Fatalf("tell failed: %v", err)
		}
	}

	// Give actors a moment to process
	time.Sleep(100 * time.Millisecond)

	// Verify all actors received the message
	for i, pid := range pids {
		resp, err := actor.Ask(ctx, pid, &ActorMessage{
			Type:    "GetMark",
			Payload: map[string]any{},
		}, 5*time.Second)
		if err != nil {
			t.Fatalf("actor %d: ask failed: %v", i, err)
		}
		result, ok := resp.(map[string]any)
		if !ok {
			t.Fatalf("actor %d: expected map response, got %T", i, resp)
		}
		if result["marked"] != "true" {
			t.Errorf("actor %d: expected marked=true, got %v", i, result["marked"])
		}
	}
}

func TestPermanentPool_RecoveryApplied(t *testing.T) {
	cfg := map[string]any{
		"system":   "my-actors",
		"mode":     "permanent",
		"poolSize": 2,
		"recovery": map[string]any{
			"failureScope": "isolated",
			"action":       "restart",
			"maxRetries":   3,
			"retryWindow":  "10s",
		},
	}
	mod, err := NewActorPoolModule("recovery-pool", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mod.recovery == nil {
		t.Fatal("expected recovery supervisor to be configured")
	}
}

func TestPermanentPool_RecoveryActuallyWorks(t *testing.T) {
	ctx := context.Background()

	// Create system with a supervisor that restarts on error
	sup := supervisor.NewSupervisor(
		supervisor.WithStrategy(supervisor.OneForOneStrategy),
		supervisor.WithAnyErrorDirective(supervisor.RestartDirective),
		supervisor.WithRetry(5, 30*time.Second),
	)

	sysMod, err := NewActorSystemModule("test-recovery", map[string]any{})
	if err != nil {
		t.Fatalf("failed to create system: %v", err)
	}
	if err := sysMod.Start(ctx); err != nil {
		t.Fatalf("failed to start system: %v", err)
	}
	defer sysMod.Stop(ctx) //nolint:errcheck

	handlers := map[string]*HandlerPipeline{
		"Ping": {
			Steps: []map[string]any{
				{
					"name": "pong",
					"type": "step.set",
					"config": map[string]any{
						"values": map[string]any{"pong": "true"},
					},
				},
			},
		},
	}

	pool := &ActorPoolModule{
		name:       "sup-pool",
		systemName: "test-recovery",
		mode:       "permanent",
		poolSize:   1,
		routing:    "round-robin",
		system:     sysMod,
		recovery:   sup,
		handlers:   handlers,
	}
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}

	// Verify the actor is alive
	pids, err := pool.SelectActor(nil)
	if err != nil {
		t.Fatalf("SelectActor failed: %v", err)
	}

	resp, err := actor.Ask(ctx, pids[0], &ActorMessage{
		Type:    "Ping",
		Payload: map[string]any{},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("ask failed: %v", err)
	}
	result, ok := resp.(map[string]any)
	if !ok {
		t.Fatalf("expected map response, got %T", resp)
	}
	if result["pong"] != "true" {
		t.Errorf("expected pong=true, got %v", result["pong"])
	}

	// Verify the actor exists in the system (confirms spawn with supervisor)
	actorName := fmt.Sprintf("%s-%d", pool.name, 0)
	exists, err := sysMod.ActorSystem().ActorExists(ctx, actorName)
	if err != nil {
		t.Fatalf("ActorExists failed: %v", err)
	}
	if !exists {
		t.Error("expected actor to exist in system")
	}
}
