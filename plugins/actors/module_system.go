package actors

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/supervisor"
)

// ActorSystemModule wraps a goakt ActorSystem as a workflow engine module.
type ActorSystemModule struct {
	name            string
	config          map[string]any
	shutdownTimeout time.Duration
	system          actor.ActorSystem
	logger          *slog.Logger

	// Default recovery policy
	defaultSupervisor *supervisor.Supervisor
}

// NewActorSystemModule creates a new actor system module from config.
func NewActorSystemModule(name string, cfg map[string]any) (*ActorSystemModule, error) {
	if name == "" {
		return nil, fmt.Errorf("actor.system module requires a name")
	}

	m := &ActorSystemModule{
		name:            name,
		config:          cfg,
		shutdownTimeout: 30 * time.Second,
	}

	// Parse shutdown timeout
	if v, ok := cfg["shutdownTimeout"].(string); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("actor.system %q: invalid shutdownTimeout %q: %w", name, v, err)
		}
		m.shutdownTimeout = d
	}

	// Parse default recovery policy
	if recovery, ok := cfg["defaultRecovery"].(map[string]any); ok {
		sup, err := parseRecoveryConfig(recovery)
		if err != nil {
			return nil, fmt.Errorf("actor.system %q: %w", name, err)
		}
		m.defaultSupervisor = sup
	}

	// Default supervisor if none configured
	if m.defaultSupervisor == nil {
		m.defaultSupervisor = supervisor.NewSupervisor(
			supervisor.WithStrategy(supervisor.OneForOneStrategy),
			supervisor.WithAnyErrorDirective(supervisor.RestartDirective),
			supervisor.WithRetry(5, 30*time.Second),
		)
	}

	return m, nil
}

// Name returns the module name.
func (m *ActorSystemModule) Name() string { return m.name }

// Init registers the module in the service registry.
func (m *ActorSystemModule) Init(app modular.Application) error {
	return app.RegisterService(fmt.Sprintf("actor-system:%s", m.name), m)
}

// Start creates and starts the goakt ActorSystem.
func (m *ActorSystemModule) Start(ctx context.Context) error {
	opts := []actor.Option{
		actor.WithShutdownTimeout(m.shutdownTimeout),
		actor.WithDefaultSupervisor(m.defaultSupervisor),
	}

	sys, err := actor.NewActorSystem(m.name, opts...)
	if err != nil {
		return fmt.Errorf("actor.system %q: failed to create actor system: %w", m.name, err)
	}

	if err := sys.Start(ctx); err != nil {
		return fmt.Errorf("actor.system %q: failed to start: %w", m.name, err)
	}

	m.system = sys
	return nil
}

// Stop gracefully shuts down the actor system.
func (m *ActorSystemModule) Stop(ctx context.Context) error {
	if m.system != nil {
		return m.system.Stop(ctx)
	}
	return nil
}

// ActorSystem returns the underlying goakt ActorSystem.
func (m *ActorSystemModule) ActorSystem() actor.ActorSystem {
	return m.system
}

// DefaultSupervisor returns the default supervisor for pools that don't specify their own.
func (m *ActorSystemModule) DefaultSupervisor() *supervisor.Supervisor {
	return m.defaultSupervisor
}

// parseRecoveryConfig builds a supervisor from recovery config.
func parseRecoveryConfig(cfg map[string]any) (*supervisor.Supervisor, error) {
	var opts []supervisor.SupervisorOption

	// Parse failure scope
	scope, _ := cfg["failureScope"].(string)
	switch scope {
	case "all-for-one":
		opts = append(opts, supervisor.WithStrategy(supervisor.OneForAllStrategy))
	case "isolated", "":
		opts = append(opts, supervisor.WithStrategy(supervisor.OneForOneStrategy))
	default:
		return nil, fmt.Errorf("invalid failureScope %q (use 'isolated' or 'all-for-one')", scope)
	}

	// Parse recovery action
	action, _ := cfg["action"].(string)
	switch action {
	case "restart", "":
		opts = append(opts, supervisor.WithAnyErrorDirective(supervisor.RestartDirective))
	case "stop":
		opts = append(opts, supervisor.WithAnyErrorDirective(supervisor.StopDirective))
	case "escalate":
		opts = append(opts, supervisor.WithAnyErrorDirective(supervisor.EscalateDirective))
	default:
		return nil, fmt.Errorf("invalid recovery action %q (use 'restart', 'stop', or 'escalate')", action)
	}

	// Parse retry limits
	maxRetries := uint32(5)
	if v, ok := cfg["maxRetries"]; ok {
		switch val := v.(type) {
		case int:
			if val >= 0 {
				maxRetries = uint32(val) //nolint:gosec // validated non-negative
			}
		case float64:
			if val >= 0 {
				maxRetries = uint32(val) //nolint:gosec // validated non-negative
			}
		}
	}
	retryWindow := 30 * time.Second
	if v, ok := cfg["retryWindow"].(string); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid retryWindow %q: %w", v, err)
		}
		retryWindow = d
	}
	opts = append(opts, supervisor.WithRetry(maxRetries, retryWindow))

	return supervisor.NewSupervisor(opts...), nil
}
