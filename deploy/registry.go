package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
)

// StrategyRegistry holds the available deployment strategies.
type StrategyRegistry struct {
	mu         sync.RWMutex
	strategies map[string]DeploymentStrategy
}

// NewStrategyRegistry creates a registry pre-loaded with built-in strategies.
func NewStrategyRegistry(logger *slog.Logger) *StrategyRegistry {
	if logger == nil {
		logger = slog.Default()
	}
	r := &StrategyRegistry{
		strategies: make(map[string]DeploymentStrategy),
	}
	// Register built-in strategies.
	r.Register(NewBlueGreenStrategy(logger))
	r.Register(NewCanaryStrategy(logger))
	r.Register(NewRollingStrategy(logger))
	return r
}

// Get returns the strategy with the given name, or false if not found.
func (r *StrategyRegistry) Get(name string) (DeploymentStrategy, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.strategies[name]
	return s, ok
}

// Register adds a strategy to the registry, replacing any existing one with the same name.
func (r *StrategyRegistry) Register(s DeploymentStrategy) {
	if s == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.strategies[s.Name()] = s
}

// List returns the sorted names of all registered strategies.
func (r *StrategyRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.strategies))
	for name := range r.strategies {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Execute is a convenience method that looks up a strategy and executes a plan.
func (r *StrategyRegistry) Execute(plan *DeploymentPlan) (*DeploymentResult, error) {
	if plan == nil {
		return nil, fmt.Errorf("deployment plan is nil")
	}
	s, ok := r.Get(plan.Strategy)
	if !ok {
		return nil, fmt.Errorf("unknown deployment strategy %q", plan.Strategy)
	}
	if err := s.Validate(plan.Config); err != nil {
		return nil, fmt.Errorf("invalid config for strategy %q: %w", plan.Strategy, err)
	}
	return s.Execute(context.Background(), plan)
}
