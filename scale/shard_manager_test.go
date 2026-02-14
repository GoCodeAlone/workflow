package scale

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestShardManagerStartStop(t *testing.T) {
	cfg := ShardManagerConfig{
		ShardCount: 3,
		Replicas:   50,
		PoolConfig: WorkerPoolConfig{
			MinWorkers: 1,
			MaxWorkers: 2,
			QueueSize:  16,
		},
	}

	m := NewShardManager(cfg)

	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if m.ShardCount() != 3 {
		t.Errorf("expected 3 shards, got %d", m.ShardCount())
	}

	stats := m.ShardStats()
	if len(stats) != 3 {
		t.Errorf("expected 3 shard stats, got %d", len(stats))
	}

	for id, info := range stats {
		if info.State != "active" {
			t.Errorf("shard %s state = %s, want active", id, info.State)
		}
	}

	if err := m.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestShardManagerRouteTask(t *testing.T) {
	cfg := ShardManagerConfig{
		ShardCount: 2,
		Replicas:   50,
		PoolConfig: WorkerPoolConfig{
			MinWorkers: 1,
			MaxWorkers: 4,
			QueueSize:  64,
		},
	}

	m := NewShardManager(cfg)
	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer m.Stop()

	var executed atomic.Int64

	const taskCount = 10
	for i := 0; i < taskCount; i++ {
		err := m.RouteTask(fmt.Sprintf("tenant-%d", i%3), Task{
			ID:       fmt.Sprintf("task-%d", i),
			TenantID: fmt.Sprintf("tenant-%d", i%3),
			Execute: func(ctx context.Context) error {
				executed.Add(1)
				return nil
			},
		})
		if err != nil {
			t.Fatalf("RouteTask failed: %v", err)
		}
	}

	// Wait for execution
	deadline := time.After(5 * time.Second)
	for executed.Load() < taskCount {
		select {
		case <-deadline:
			t.Fatalf("timed out, executed %d/%d", executed.Load(), taskCount)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestShardManagerConsistentRouting(t *testing.T) {
	cfg := ShardManagerConfig{
		ShardCount: 4,
		Replicas:   100,
		PoolConfig: WorkerPoolConfig{
			MinWorkers: 1,
			MaxWorkers: 2,
			QueueSize:  16,
		},
	}

	m := NewShardManager(cfg)
	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer m.Stop()

	// Verify the hash ring consistently routes the same key to the same shard.
	// We only check the ring (no task execution) to avoid result channel backpressure.
	keys := []string{"tenant-x", "tenant-y", "tenant-z", "conversation-123"}
	for _, key := range keys {
		first, err := m.ring.GetNode(key)
		if err != nil {
			t.Fatalf("GetNode(%q) failed: %v", key, err)
		}
		for i := 0; i < 100; i++ {
			node, _ := m.ring.GetNode(key)
			if node != first {
				t.Errorf("inconsistent routing for %s: was %s, now %s on iteration %d", key, first, node, i)
			}
		}
	}
}

func TestShardManagerAddRemoveShard(t *testing.T) {
	cfg := ShardManagerConfig{
		ShardCount: 2,
		Replicas:   50,
		PoolConfig: WorkerPoolConfig{
			MinWorkers: 1,
			MaxWorkers: 2,
			QueueSize:  16,
		},
	}

	m := NewShardManager(cfg)
	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer m.Stop()

	if m.ShardCount() != 2 {
		t.Errorf("expected 2 shards, got %d", m.ShardCount())
	}

	// Add a shard
	if err := m.AddShard("shard-extra"); err != nil {
		t.Fatalf("AddShard failed: %v", err)
	}
	if m.ShardCount() != 3 {
		t.Errorf("expected 3 shards, got %d", m.ShardCount())
	}

	// Duplicate add should fail
	if err := m.AddShard("shard-extra"); err == nil {
		t.Error("expected error on duplicate shard add")
	}

	// Remove a shard
	if err := m.RemoveShard("shard-extra"); err != nil {
		t.Fatalf("RemoveShard failed: %v", err)
	}
	if m.ShardCount() != 2 {
		t.Errorf("expected 2 shards, got %d", m.ShardCount())
	}

	// Remove nonexistent should fail
	if err := m.RemoveShard("shard-999"); err == nil {
		t.Error("expected error removing nonexistent shard")
	}
}

func TestShardManagerDefaultConfig(t *testing.T) {
	cfg := DefaultShardManagerConfig()
	if cfg.ShardCount <= 0 {
		t.Error("ShardCount should be positive")
	}
	if cfg.Replicas <= 0 {
		t.Error("Replicas should be positive")
	}
}

func TestShardStateString(t *testing.T) {
	tests := []struct {
		state ShardState
		want  string
	}{
		{ShardActive, "active"},
		{ShardDraining, "draining"},
		{ShardInactive, "inactive"},
		{ShardState(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("ShardState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}
