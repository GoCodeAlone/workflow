package deploy

import (
	"context"
	"fmt"
	"sync"

	"github.com/GoCodeAlone/workflow/provider"
)

// Executor bridges deployment strategies with cloud providers.
// It looks up the appropriate strategy and provider, validates the plan,
// and delegates execution to the cloud provider.
type Executor struct {
	strategies *StrategyRegistry
	mu         sync.RWMutex
	providers  map[string]provider.CloudProvider
}

// NewExecutor creates an Executor backed by the given strategy registry.
func NewExecutor(strategies *StrategyRegistry) *Executor {
	return &Executor{
		strategies: strategies,
		providers:  make(map[string]provider.CloudProvider),
	}
}

// RegisterProvider adds a cloud provider under the given name.
func (e *Executor) RegisterProvider(name string, p provider.CloudProvider) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.providers[name] = p
}

// GetProvider returns the cloud provider with the given name, or false if not found.
func (e *Executor) GetProvider(name string) (provider.CloudProvider, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	p, ok := e.providers[name]
	return p, ok
}

// Deploy executes a deployment through the named provider, using the strategy
// identified in the request. It validates the strategy config, deploys via the
// provider, and handles rollback on failure when configured.
func (e *Executor) Deploy(ctx context.Context, providerName string, req provider.DeployRequest) (*provider.DeployResult, error) {
	// Look up strategy
	strategy, ok := e.strategies.Get(req.Strategy)
	if !ok {
		return nil, fmt.Errorf("executor: unknown deployment strategy %q", req.Strategy)
	}

	// Look up provider
	p, ok := e.GetProvider(providerName)
	if !ok {
		return nil, fmt.Errorf("executor: unknown provider %q", providerName)
	}

	// Validate strategy config
	if err := strategy.Validate(req.Config); err != nil {
		return nil, fmt.Errorf("executor: invalid config for strategy %q: %w", req.Strategy, err)
	}

	// Execute deployment via provider
	result, err := p.Deploy(ctx, req)
	if err != nil {
		// Attempt rollback if configured
		rollback, _ := req.Config["rollback_on_failure"].(bool)
		if rollback && result != nil && result.DeployID != "" {
			if rbErr := p.Rollback(ctx, result.DeployID); rbErr != nil {
				return result, fmt.Errorf("executor: deploy failed (%w) and rollback also failed: %v", err, rbErr)
			}
			result.Status = "rolled_back"
			result.Message = fmt.Sprintf("deployment failed and was rolled back: %v", err)
			return result, nil
		}
		return result, fmt.Errorf("executor: deployment via provider %q failed: %w", providerName, err)
	}

	return result, nil
}
