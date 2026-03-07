package actors

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/tochemey/goakt/v4/supervisor"
)

// ActorPoolModule defines a group of actors with shared behavior, routing, and recovery.
type ActorPoolModule struct {
	name       string
	config     map[string]any
	systemName string
	mode       string // "auto-managed" or "permanent"

	// Auto-managed settings
	idleTimeout time.Duration

	// Permanent pool settings
	poolSize int

	// Routing
	routing    string // "round-robin", "random", "broadcast", "sticky"
	routingKey string // required for sticky

	// Recovery
	recovery *supervisor.Supervisor

	// Placement (cluster mode)
	placement   string
	targetRoles []string
	failover    bool

	// Resolved at Init
	system *ActorSystemModule
	logger *slog.Logger

	// Message handlers set by the actor workflow handler
	handlers map[string]any // message type -> step pipeline config
}

// NewActorPoolModule creates a new actor pool module from config.
func NewActorPoolModule(name string, cfg map[string]any) (*ActorPoolModule, error) {
	if name == "" {
		return nil, fmt.Errorf("actor.pool module requires a name")
	}

	systemName, _ := cfg["system"].(string)
	if systemName == "" {
		return nil, fmt.Errorf("actor.pool %q: 'system' is required (name of actor.system module)", name)
	}

	m := &ActorPoolModule{
		name:        name,
		config:      cfg,
		systemName:  systemName,
		mode:        "auto-managed",
		idleTimeout: 10 * time.Minute,
		poolSize:    10,
		routing:     "round-robin",
		failover:    true,
		handlers:    make(map[string]any),
	}

	// Parse mode
	if v, ok := cfg["mode"].(string); ok && v != "" {
		switch v {
		case "auto-managed", "permanent":
			m.mode = v
		default:
			return nil, fmt.Errorf("actor.pool %q: invalid mode %q (use 'auto-managed' or 'permanent')", name, v)
		}
	}

	// Parse idle timeout
	if v, ok := cfg["idleTimeout"].(string); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("actor.pool %q: invalid idleTimeout %q: %w", name, v, err)
		}
		m.idleTimeout = d
	}

	// Parse pool size
	if v, ok := cfg["poolSize"]; ok {
		switch val := v.(type) {
		case int:
			m.poolSize = val
		case float64:
			m.poolSize = int(val)
		}
	}

	// Parse routing
	if v, ok := cfg["routing"].(string); ok && v != "" {
		switch v {
		case "round-robin", "random", "broadcast", "sticky":
			m.routing = v
		default:
			return nil, fmt.Errorf("actor.pool %q: invalid routing %q (use 'round-robin', 'random', 'broadcast', or 'sticky')", name, v)
		}
	}

	// Parse routing key
	m.routingKey, _ = cfg["routingKey"].(string)
	if m.routing == "sticky" && m.routingKey == "" {
		return nil, fmt.Errorf("actor.pool %q: 'routingKey' is required when routing is 'sticky'", name)
	}

	// Parse recovery
	if recovery, ok := cfg["recovery"].(map[string]any); ok {
		sup, err := parseRecoveryConfig(recovery)
		if err != nil {
			return nil, fmt.Errorf("actor.pool %q: %w", name, err)
		}
		m.recovery = sup
	}

	// Parse placement
	m.placement, _ = cfg["placement"].(string)
	if roles, ok := cfg["targetRoles"].([]any); ok {
		for _, r := range roles {
			if s, ok := r.(string); ok {
				m.targetRoles = append(m.targetRoles, s)
			}
		}
	}
	if v, ok := cfg["failover"].(bool); ok {
		m.failover = v
	}

	return m, nil
}

// Name returns the module name.
func (m *ActorPoolModule) Name() string { return m.name }

// Init resolves the actor.system module reference.
func (m *ActorPoolModule) Init(app modular.Application) error {
	svcName := fmt.Sprintf("actor-system:%s", m.systemName)
	var sys *ActorSystemModule
	if err := app.GetService(svcName, &sys); err != nil {
		return fmt.Errorf("actor.pool %q: actor.system %q not found: %w", m.name, m.systemName, err)
	}
	m.system = sys

	// Register self in service registry for step.actor_send/ask to find
	return app.RegisterService(fmt.Sprintf("actor-pool:%s", m.name), m)
}

// Start spawns actors in the pool.
func (m *ActorPoolModule) Start(ctx context.Context) error {
	if m.system == nil || m.system.ActorSystem() == nil {
		return fmt.Errorf("actor.pool %q: actor system not started", m.name)
	}
	// Actor spawning will be implemented in Task 4 (bridge actor)
	return nil
}

// Stop is a no-op — actors are stopped when the ActorSystem shuts down.
func (m *ActorPoolModule) Stop(_ context.Context) error {
	return nil
}

// SetHandlers sets the message receive handlers (called by the actor workflow handler).
func (m *ActorPoolModule) SetHandlers(handlers map[string]any) {
	m.handlers = handlers
}

// SystemName returns the referenced actor.system module name.
func (m *ActorPoolModule) SystemName() string { return m.systemName }

// Mode returns the lifecycle mode.
func (m *ActorPoolModule) Mode() string { return m.mode }

// Routing returns the routing strategy.
func (m *ActorPoolModule) Routing() string { return m.routing }

// RoutingKey returns the sticky routing key.
func (m *ActorPoolModule) RoutingKey() string { return m.routingKey }
