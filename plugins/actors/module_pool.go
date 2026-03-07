package actors

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/tochemey/goakt/v4/actor"
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
	pids     []*actor.PID // tracked PIDs for routing

	// Routing
	routing    string // "round-robin", "random", "broadcast", "sticky"
	routingKey string // required for sticky
	rrCounter  atomic.Uint64

	// Recovery
	recovery *supervisor.Supervisor

	// Resolved at Init
	system *ActorSystemModule
	logger *slog.Logger

	// Message handlers set by the actor workflow handler
	handlers map[string]*HandlerPipeline

	// Step registry for building pipeline steps inside actors
	stepRegistry *module.StepRegistry
	app          modular.Application
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
		handlers:    make(map[string]*HandlerPipeline),
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

// preBuildSteps creates step instances for all handler pipelines upfront.
// This must be called before spawning actors so that the shared handler configs
// are fully initialized and no concurrent map writes occur at runtime.
func (m *ActorPoolModule) preBuildSteps() {
	for _, handler := range m.handlers {
		handler.BuiltSteps = make([]module.PipelineStep, len(handler.Steps))
		for i, stepCfg := range handler.Steps {
			stepType, _ := stepCfg["type"].(string)
			stepName, _ := stepCfg["name"].(string)
			config, _ := stepCfg["config"].(map[string]any)
			if stepType == "" || stepName == "" {
				continue
			}
			var step module.PipelineStep
			var err error
			if m.stepRegistry != nil {
				step, err = m.stepRegistry.Create(stepType, stepName, config, m.app)
			} else if stepType == "step.set" {
				factory := module.NewSetStepFactory()
				step, err = factory(stepName, config, nil)
			}
			if err == nil && step != nil {
				handler.BuiltSteps[i] = step
			}
		}
	}
}

// Start spawns actors in the pool.
func (m *ActorPoolModule) Start(ctx context.Context) error {
	if m.system == nil || m.system.ActorSystem() == nil {
		return fmt.Errorf("actor.pool %q: actor system not started", m.name)
	}

	// Pre-build step instances before spawning actors to avoid concurrent writes
	m.preBuildSteps()

	if m.mode == "permanent" {
		sys := m.system.ActorSystem()
		m.pids = make([]*actor.PID, 0, m.poolSize)

		// Build spawn options: apply per-pool recovery supervisor if configured
		var spawnOpts []actor.SpawnOption
		if m.recovery != nil {
			spawnOpts = append(spawnOpts, actor.WithSupervisor(m.recovery))
		}

		for i := 0; i < m.poolSize; i++ {
			actorName := fmt.Sprintf("%s-%d", m.name, i)
			bridge := NewBridgeActor(m.name, actorName, m.handlers, m.stepRegistry, m.app, m.logger)
			pid, err := sys.Spawn(ctx, actorName, bridge, spawnOpts...)
			if err != nil {
				return fmt.Errorf("actor.pool %q: failed to spawn actor %q: %w", m.name, actorName, err)
			}
			m.pids = append(m.pids, pid)
		}
		if m.logger != nil {
			m.logger.Info("permanent actor pool started", "pool", m.name, "size", m.poolSize, "routing", m.routing)
		}
	}

	return nil
}

// Stop is a no-op — actors are stopped when the ActorSystem shuts down.
func (m *ActorPoolModule) Stop(_ context.Context) error {
	return nil
}

// SelectActor picks one or more PIDs from the permanent pool based on the routing strategy.
// For broadcast, returns all PIDs. For other strategies, returns a single PID.
// The msg parameter is used for sticky routing to extract the routing key.
func (m *ActorPoolModule) SelectActor(msg *ActorMessage) ([]*actor.PID, error) {
	if len(m.pids) == 0 {
		return nil, fmt.Errorf("actor.pool %q: no actors available", m.name)
	}

	switch m.routing {
	case "broadcast":
		return m.pids, nil

	case "random":
		idx := rand.Intn(len(m.pids)) //nolint:gosec
		return []*actor.PID{m.pids[idx]}, nil

	case "sticky":
		key := ""
		if msg != nil && msg.Payload != nil && m.routingKey != "" {
			if v, ok := msg.Payload[m.routingKey]; ok {
				key = fmt.Sprintf("%v", v)
			}
		}
		h := fnv.New32a()
		h.Write([]byte(key))
		idx := int(h.Sum32()) % len(m.pids)
		return []*actor.PID{m.pids[idx]}, nil

	default: // round-robin
		idx := m.rrCounter.Add(1) - 1
		return []*actor.PID{m.pids[idx%uint64(len(m.pids))]}, nil
	}
}

// SetHandlers sets the message receive handlers (called by the actor workflow handler).
func (m *ActorPoolModule) SetHandlers(handlers map[string]*HandlerPipeline) {
	m.handlers = handlers
}

// SetStepRegistry injects the step registry and app for actor spawn-time pipeline building.
func (m *ActorPoolModule) SetStepRegistry(registry *module.StepRegistry, app modular.Application) {
	m.stepRegistry = registry
	m.app = app
}

// GetGrainIdentity retrieves or activates a grain for the given identity.
// The grain system handles lifecycle automatically: it activates on first use
// and passivates after idleTimeout of inactivity.
func (m *ActorPoolModule) GetGrainIdentity(ctx context.Context, identity string) (*actor.GrainIdentity, error) {
	if m.system == nil || m.system.ActorSystem() == nil {
		return nil, fmt.Errorf("actor.pool %q: actor system not started", m.name)
	}

	factory := func(_ context.Context) (actor.Grain, error) {
		return &BridgeGrain{
			poolName: m.name,
			handlers: m.handlers,
			registry: m.stepRegistry,
			app:      m.app,
			logger:   m.logger,
		}, nil
	}

	return m.system.ActorSystem().GrainIdentity(ctx, identity, factory,
		actor.WithGrainDeactivateAfter(m.idleTimeout),
	)
}

// SystemName returns the referenced actor.system module name.
func (m *ActorPoolModule) SystemName() string { return m.systemName }

// Mode returns the lifecycle mode.
func (m *ActorPoolModule) Mode() string { return m.mode }

// Routing returns the routing strategy.
func (m *ActorPoolModule) Routing() string { return m.routing }

// RoutingKey returns the sticky routing key.
func (m *ActorPoolModule) RoutingKey() string { return m.routingKey }
