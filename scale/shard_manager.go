package scale

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ShardState represents the lifecycle state of a shard.
type ShardState int

const (
	ShardActive ShardState = iota
	ShardDraining
	ShardInactive
)

func (s ShardState) String() string {
	switch s {
	case ShardActive:
		return "active"
	case ShardDraining:
		return "draining"
	case ShardInactive:
		return "inactive"
	default:
		return "unknown"
	}
}

// Shard represents a processing partition.
type Shard struct {
	ID          string
	State       ShardState
	Pool        *WorkerPool
	ActiveTasks int64
	CreatedAt   time.Time
}

// ShardManagerConfig configures the shard manager.
type ShardManagerConfig struct {
	// ShardCount is the initial number of shards.
	ShardCount int
	// Replicas is the number of virtual nodes per shard in the hash ring.
	Replicas int
	// PoolConfig is the worker pool config for each shard.
	PoolConfig WorkerPoolConfig
}

// DefaultShardManagerConfig returns sensible defaults.
func DefaultShardManagerConfig() ShardManagerConfig {
	return ShardManagerConfig{
		ShardCount: 4,
		Replicas:   100,
		PoolConfig: DefaultWorkerPoolConfig(),
	}
}

// ShardManager manages a set of shards and routes tasks to them
// using consistent hashing on the tenant/conversation key.
type ShardManager struct {
	cfg    ShardManagerConfig
	ring   *ConsistentHash
	shards map[string]*Shard
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
}

// NewShardManager creates a new shard manager.
func NewShardManager(cfg ShardManagerConfig) *ShardManager {
	if cfg.ShardCount <= 0 {
		cfg.ShardCount = 4
	}
	if cfg.Replicas <= 0 {
		cfg.Replicas = 100
	}

	return &ShardManager{
		cfg:    cfg,
		ring:   NewConsistentHash(cfg.Replicas),
		shards: make(map[string]*Shard),
	}
}

// Start initializes all shards and their worker pools.
func (m *ShardManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ctx, m.cancel = context.WithCancel(ctx)

	for i := 0; i < m.cfg.ShardCount; i++ {
		id := fmt.Sprintf("shard-%d", i)
		if err := m.addShardLocked(id); err != nil {
			// Clean up already-started shards
			for _, s := range m.shards {
				_ = s.Pool.Stop()
			}
			return fmt.Errorf("failed to start shard %s: %w", id, err)
		}
	}

	return nil
}

// Stop gracefully shuts down all shards.
func (m *ShardManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cancel != nil {
		m.cancel()
	}

	var lastErr error
	for id, shard := range m.shards {
		shard.State = ShardDraining
		if err := shard.Pool.Stop(); err != nil {
			lastErr = fmt.Errorf("failed to stop shard %s: %w", id, err)
		}
		shard.State = ShardInactive
	}
	return lastErr
}

// RouteTask routes a task to the appropriate shard based on the partition key.
func (m *ShardManager) RouteTask(partitionKey string, task Task) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	node, err := m.ring.GetNode(partitionKey)
	if err != nil {
		return fmt.Errorf("routing failed: %w", err)
	}

	shard, ok := m.shards[node]
	if !ok {
		return fmt.Errorf("shard %s not found", node)
	}

	if shard.State != ShardActive {
		return fmt.Errorf("shard %s is %s", node, shard.State)
	}

	return shard.Pool.Submit(task)
}

// AddShard dynamically adds a new shard.
func (m *ShardManager) AddShard(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.addShardLocked(id)
}

func (m *ShardManager) addShardLocked(id string) error {
	if _, exists := m.shards[id]; exists {
		return fmt.Errorf("shard %s already exists", id)
	}

	pool := NewWorkerPool(m.cfg.PoolConfig)
	if err := pool.Start(m.ctx); err != nil {
		return err
	}

	shard := &Shard{
		ID:        id,
		State:     ShardActive,
		Pool:      pool,
		CreatedAt: time.Now(),
	}

	m.shards[id] = shard
	m.ring.AddNode(id)
	return nil
}

// RemoveShard drains and removes a shard.
func (m *ShardManager) RemoveShard(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	shard, ok := m.shards[id]
	if !ok {
		return fmt.Errorf("shard %s not found", id)
	}

	shard.State = ShardDraining
	m.ring.RemoveNode(id)

	if err := shard.Pool.Stop(); err != nil {
		return fmt.Errorf("failed to stop shard %s pool: %w", id, err)
	}

	shard.State = ShardInactive
	delete(m.shards, id)
	return nil
}

// ShardStats returns statistics for all shards.
func (m *ShardManager) ShardStats() map[string]ShardInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info := make(map[string]ShardInfo, len(m.shards))
	for id, shard := range m.shards {
		stats := shard.Pool.Stats()
		info[id] = ShardInfo{
			ID:            id,
			State:         shard.State.String(),
			ActiveWorkers: stats.ActiveWorkers,
			PendingTasks:  stats.PendingTasks,
			CompletedOK:   stats.CompletedOK,
			CompletedErr:  stats.CompletedErr,
			CreatedAt:     shard.CreatedAt,
		}
	}
	return info
}

// ShardInfo holds read-only information about a shard.
type ShardInfo struct {
	ID            string
	State         string
	ActiveWorkers int
	PendingTasks  int
	CompletedOK   int64
	CompletedErr  int64
	CreatedAt     time.Time
}

// ShardCount returns the number of active shards.
func (m *ShardManager) ShardCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.shards)
}
