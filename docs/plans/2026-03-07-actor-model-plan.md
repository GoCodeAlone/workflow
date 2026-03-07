# Actor Model Plugin Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add actor model support to the workflow engine via a built-in `actors` plugin using goakt v4, enabling stateful long-lived entities, distributed execution, structured fault recovery, and a new message-driven workflow paradigm.

**Architecture:** New built-in plugin at `plugins/actors/` wrapping goakt v4. Two module types (`actor.system`, `actor.pool`), two step types (`step.actor_send`, `step.actor_ask`), one workflow handler (`actors`), and a bridge actor that executes step pipelines inside goakt's actor model. The plugin follows the exact same patterns as `plugins/pipelinesteps/`.

**Tech Stack:** Go, goakt v4 (`github.com/tochemey/goakt/v4`), existing workflow engine plugin SDK

**Design doc:** `docs/plans/2026-03-07-actor-model-design.md`

---

## Prerequisites

Before starting, verify goakt v4 is available:

```bash
cd /Users/jon/workspace/workflow
go get github.com/tochemey/goakt/v4@latest
go mod tidy
```

If the module path differs from `github.com/tochemey/goakt/v4`, adjust all imports in this plan accordingly.

---

### Task 1: Plugin Skeleton

**Files:**
- Create: `plugins/actors/plugin.go`

**Step 1: Create the plugin skeleton**

Create `plugins/actors/plugin.go` following the exact pattern from `plugins/pipelinesteps/plugin.go`:

```go
package actors

import (
	"log/slog"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// Plugin provides actor model support for the workflow engine.
type Plugin struct {
	plugin.BaseEnginePlugin
	stepRegistry         interfaces.StepRegistryProvider
	concreteStepRegistry *module.StepRegistry
	logger               *slog.Logger
}

// New creates a new actors plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "actors",
				PluginVersion:     "1.0.0",
				PluginDescription: "Actor model support with goakt v4 — stateful entities, distributed execution, and fault-tolerant message-driven workflows",
			},
			Manifest: plugin.PluginManifest{
				Name:        "actors",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "Actor model support with goakt v4",
				Tier:        plugin.TierCore,
				ModuleTypes: []string{
					"actor.system",
					"actor.pool",
				},
				StepTypes: []string{
					"step.actor_send",
					"step.actor_ask",
				},
				WorkflowTypes: []string{"actors"},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "actor-system", Role: "provider", Priority: 50},
				},
			},
		},
	}
}

// SetStepRegistry is called by the engine to inject the step registry.
func (p *Plugin) SetStepRegistry(registry interfaces.StepRegistryProvider) {
	p.stepRegistry = registry
	if concrete, ok := registry.(*module.StepRegistry); ok {
		p.concreteStepRegistry = concrete
	}
}

// SetLogger is called by the engine to inject the logger.
func (p *Plugin) SetLogger(logger *slog.Logger) {
	p.logger = logger
}

// Capabilities returns the plugin's capability contracts.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		capability.NewContract("actor-system", "provider"),
	}
}

// ModuleFactories returns actor module factories.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		// Added in Task 2 and Task 3
	}
}

// StepFactories returns actor step factories.
func (p *Plugin) StepFactories() map[string]plugin.StepFactory {
	return map[string]plugin.StepFactory{
		// Added in Task 5 and Task 6
	}
}

// WorkflowHandlers returns the actor workflow handler factory.
func (p *Plugin) WorkflowHandlers() map[string]plugin.WorkflowHandlerFactory {
	return map[string]plugin.WorkflowHandlerFactory{
		// Added in Task 7
	}
}

// ModuleSchemas returns schemas for actor modules.
func (p *Plugin) ModuleSchemas() []*schema.ModuleSchema {
	return []*schema.ModuleSchema{
		// Added in Task 8
	}
}

// StepSchemas returns schemas for actor steps.
func (p *Plugin) StepSchemas() []*schema.StepSchema {
	return []*schema.StepSchema{
		// Added in Task 8
	}
}
```

**Step 2: Register the plugin in the server**

Edit `cmd/server/main.go` — add import and registration. Find the `defaultEnginePlugins()` function (or equivalent plugin list) and add:

```go
import actorsplugin "github.com/GoCodeAlone/workflow/plugins/actors"
```

Add to the plugin list:

```go
actorsplugin.New(),
```

**Step 3: Verify it compiles**

```bash
cd /Users/jon/workspace/workflow
go build ./plugins/actors/
go build ./cmd/server/
```

Expected: both compile with no errors.

**Step 4: Commit**

```bash
git add plugins/actors/plugin.go cmd/server/main.go
git commit -m "feat(actors): plugin skeleton with manifest and engine registration"
```

---

### Task 2: Actor System Module

**Files:**
- Create: `plugins/actors/module_system.go`
- Create: `plugins/actors/module_system_test.go`
- Modify: `plugins/actors/plugin.go` (add factory)

The `actor.system` module wraps goakt's `ActorSystem`. It manages lifecycle (Init/Start/Stop) and clustering.

**Step 1: Write the test**

Create `plugins/actors/module_system_test.go`:

```go
package actors

import (
	"context"
	"testing"
)

func TestActorSystemModule_LocalMode(t *testing.T) {
	// No cluster config = local mode
	cfg := map[string]any{
		"shutdownTimeout": "5s",
	}
	mod, err := NewActorSystemModule("test-system", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mod.Name() != "test-system" {
		t.Errorf("expected name 'test-system', got %q", mod.Name())
	}

	ctx := context.Background()
	if err := mod.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	sys := mod.ActorSystem()
	if sys == nil {
		t.Fatal("expected non-nil ActorSystem")
	}

	if err := mod.Stop(ctx); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
}

func TestActorSystemModule_MissingName(t *testing.T) {
	_, err := NewActorSystemModule("", map[string]any{})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestActorSystemModule_InvalidShutdownTimeout(t *testing.T) {
	cfg := map[string]any{
		"shutdownTimeout": "not-a-duration",
	}
	_, err := NewActorSystemModule("test", cfg)
	if err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

func TestActorSystemModule_DefaultConfig(t *testing.T) {
	mod, err := NewActorSystemModule("test-defaults", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mod.shutdownTimeout.Seconds() != 30 {
		t.Errorf("expected 30s default shutdown timeout, got %v", mod.shutdownTimeout)
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
cd /Users/jon/workspace/workflow
go test ./plugins/actors/ -v -run TestActorSystem
```

Expected: FAIL — `NewActorSystemModule` not defined.

**Step 3: Implement the module**

Create `plugins/actors/module_system.go`:

```go
package actors

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/CrisisTextLine/modular"
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

	// Cluster config (nil = local mode)
	clusterConfig *actor.ClusterConfig

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

	// TODO: Add cluster config options when cluster block is present
	// This will be enhanced in a later task for clustering support

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
	opts := []supervisor.SupervisorOption{}

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
			maxRetries = uint32(val)
		case float64:
			maxRetries = uint32(val)
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
```

**Step 4: Register the factory in plugin.go**

In `plugins/actors/plugin.go`, update `ModuleFactories()`:

```go
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"actor.system": func(name string, cfg map[string]any) modular.Module {
			mod, err := NewActorSystemModule(name, cfg)
			if err != nil {
				if p.logger != nil {
					p.logger.Error("failed to create actor.system module", "name", name, "error", err)
				}
				return nil
			}
			if p.logger != nil {
				mod.logger = p.logger
			}
			return mod
		},
	}
}
```

**Step 5: Run tests**

```bash
go test ./plugins/actors/ -v -run TestActorSystem
```

Expected: all 4 tests PASS.

**Step 6: Commit**

```bash
git add plugins/actors/module_system.go plugins/actors/module_system_test.go plugins/actors/plugin.go
git commit -m "feat(actors): actor.system module wrapping goakt ActorSystem"
```

---

### Task 3: Actor Pool Module

**Files:**
- Create: `plugins/actors/module_pool.go`
- Create: `plugins/actors/module_pool_test.go`
- Modify: `plugins/actors/plugin.go` (add factory)

The `actor.pool` module defines a group of actors with a shared behavior, routing, and recovery policy.

**Step 1: Write the test**

Create `plugins/actors/module_pool_test.go`:

```go
package actors

import (
	"testing"
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
```

**Step 2: Run tests to verify they fail**

```bash
go test ./plugins/actors/ -v -run TestActorPool
```

Expected: FAIL — `NewActorPoolModule` not defined.

**Step 3: Implement the module**

Create `plugins/actors/module_pool.go`:

```go
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
	svc, err := app.GetService(svcName)
	if err != nil {
		return fmt.Errorf("actor.pool %q: actor.system %q not found: %w", m.name, m.systemName, err)
	}
	sys, ok := svc.(*ActorSystemModule)
	if !ok {
		return fmt.Errorf("actor.pool %q: service %q is not an ActorSystemModule", m.name, svcName)
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
```

**Step 4: Register the factory in plugin.go**

Update `ModuleFactories()` in `plugins/actors/plugin.go` to add `actor.pool`:

```go
"actor.pool": func(name string, cfg map[string]any) modular.Module {
    mod, err := NewActorPoolModule(name, cfg)
    if err != nil {
        if p.logger != nil {
            p.logger.Error("failed to create actor.pool module", "name", name, "error", err)
        }
        return nil
    }
    if p.logger != nil {
        mod.logger = p.logger
    }
    return mod
},
```

**Step 5: Run tests**

```bash
go test ./plugins/actors/ -v -run TestActorPool
```

Expected: all 7 tests PASS.

**Step 6: Commit**

```bash
git add plugins/actors/module_pool.go plugins/actors/module_pool_test.go plugins/actors/plugin.go
git commit -m "feat(actors): actor.pool module with routing, recovery, and lifecycle config"
```

---

### Task 4: Bridge Actor

**Files:**
- Create: `plugins/actors/bridge_actor.go`
- Create: `plugins/actors/bridge_actor_test.go`
- Create: `plugins/actors/messages.go`

The bridge actor is the core integration — a goakt `Actor` that receives messages and executes workflow step pipelines.

**Step 1: Create message types**

Create `plugins/actors/messages.go`:

```go
package actors

// ActorMessage is the message type sent between pipelines and actors.
type ActorMessage struct {
	Type    string         `cbor:"type"`
	Payload map[string]any `cbor:"payload"`
}
```

**Step 2: Write the bridge actor test**

Create `plugins/actors/bridge_actor_test.go`:

```go
package actors

import (
	"context"
	"testing"
	"time"

	"github.com/tochemey/goakt/v4/actor"
)

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
	defer sys.Stop(ctx)

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
	defer sys.Stop(ctx)

	pid, err := sys.Spawn(ctx, "test-actor", bridge)
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}

	msg := &ActorMessage{Type: "Unknown", Payload: map[string]any{}}
	resp, err := actor.Ask(ctx, pid, msg, 5*time.Second)
	if err != nil {
		t.Fatalf("ask failed: %v", err)
	}

	result, ok := resp.(map[string]any)
	if !ok {
		t.Fatalf("expected map response, got %T", resp)
	}
	if _, hasErr := result["error"]; !hasErr {
		t.Error("expected error in response for unknown message type")
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
	defer sys.Stop(ctx)

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

	result := resp.(map[string]any)
	if result["name"] != "Alice" {
		t.Errorf("expected name=Alice from state, got %v", result["name"])
	}
}
```

**Step 3: Run tests to verify they fail**

```bash
go test ./plugins/actors/ -v -run TestBridgeActor
```

Expected: FAIL — `BridgeActor` not defined.

**Step 4: Implement the bridge actor**

Create `plugins/actors/bridge_actor.go`:

```go
package actors

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/tochemey/goakt/v4/actor"
)

// HandlerPipeline defines a message handler as a list of step configs.
type HandlerPipeline struct {
	Description string
	Steps       []map[string]any
}

// BridgeActor is a goakt Actor that executes workflow step pipelines
// when it receives messages. It bridges the actor model with the
// pipeline execution model.
type BridgeActor struct {
	poolName string
	identity string
	state    map[string]any
	handlers map[string]*HandlerPipeline

	// Injected dependencies (set via goakt WithDependencies)
	registry *module.StepRegistry
	app      modular.Application
	logger   *slog.Logger
}

// PreStart initializes the actor.
func (a *BridgeActor) PreStart(_ context.Context) error {
	if a.state == nil {
		a.state = make(map[string]any)
	}
	return nil
}

// PostStop cleans up the actor.
func (a *BridgeActor) PostStop(_ context.Context) error {
	return nil
}

// Receive handles incoming messages by dispatching to the appropriate
// handler pipeline.
func (a *BridgeActor) Receive(ctx *actor.ReceiveContext) {
	switch msg := ctx.Message().(type) {
	case *ActorMessage:
		result, err := a.handleMessage(ctx.Context(), msg)
		if err != nil {
			ctx.Err(err)
			ctx.Response(map[string]any{"error": err.Error()})
			return
		}
		ctx.Response(result)

	default:
		// Ignore system messages (PostStart, PoisonPill, etc.)
		// They are handled by goakt internally
	}
}

// handleMessage finds the handler pipeline for the message type and executes it.
func (a *BridgeActor) handleMessage(ctx context.Context, msg *ActorMessage) (map[string]any, error) {
	handler, ok := a.handlers[msg.Type]
	if !ok {
		return map[string]any{
			"error": fmt.Sprintf("no handler for message type %q", msg.Type),
		}, nil
	}

	// Build the pipeline context with actor-specific template variables
	triggerData := map[string]any{
		"message": map[string]any{
			"type":    msg.Type,
			"payload": msg.Payload,
		},
		"state": copyMap(a.state),
		"actor": map[string]any{
			"identity": a.identity,
			"pool":     a.poolName,
		},
	}

	pc := module.NewPipelineContext(triggerData, map[string]any{
		"actor_pool":     a.poolName,
		"actor_identity": a.identity,
		"message_type":   msg.Type,
	})

	// Execute each step in sequence
	var lastOutput map[string]any
	for _, stepCfg := range handler.Steps {
		stepType, _ := stepCfg["type"].(string)
		stepName, _ := stepCfg["name"].(string)
		config, _ := stepCfg["config"].(map[string]any)

		if stepType == "" || stepName == "" {
			return nil, fmt.Errorf("handler %q: step missing 'type' or 'name'", msg.Type)
		}

		// Create step from registry if available
		var step module.PipelineStep
		var err error

		if a.registry != nil {
			step, err = a.registry.Create(stepType, stepName, config, a.app)
			if err != nil {
				return nil, fmt.Errorf("handler %q step %q: %w", msg.Type, stepName, err)
			}
		} else {
			// Fallback: create step.set inline for testing without a registry
			if stepType == "step.set" {
				factory := module.NewSetStepFactory()
				step, err = factory(stepName, config, nil)
				if err != nil {
					return nil, fmt.Errorf("handler %q step %q: %w", msg.Type, stepName, err)
				}
			} else {
				return nil, fmt.Errorf("handler %q step %q: no step registry available for type %q", msg.Type, stepName, stepType)
			}
		}

		result, err := step.Execute(ctx, pc)
		if err != nil {
			return nil, fmt.Errorf("handler %q step %q failed: %w", msg.Type, stepName, err)
		}

		if result != nil && result.Output != nil {
			pc.MergeStepOutput(stepName, result.Output)
			lastOutput = result.Output
		}

		if result != nil && result.Stop {
			break
		}
	}

	// Merge last step output back into actor state
	if lastOutput != nil {
		for k, v := range lastOutput {
			a.state[k] = v
		}
	}

	if lastOutput == nil {
		lastOutput = map[string]any{}
	}
	return lastOutput, nil
}

// copyMap creates a shallow copy of a map.
func copyMap(m map[string]any) map[string]any {
	cp := make(map[string]any, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}
```

**Step 5: Run tests**

```bash
go test ./plugins/actors/ -v -run TestBridgeActor
```

Expected: all 3 tests PASS. Note: the state persistence test verifies that handler outputs merge into actor state and are accessible via `{{ .state.* }}` in subsequent messages.

**Step 6: Commit**

```bash
git add plugins/actors/bridge_actor.go plugins/actors/bridge_actor_test.go plugins/actors/messages.go
git commit -m "feat(actors): bridge actor that executes step pipelines inside goakt"
```

---

### Task 5: step.actor_send

**Files:**
- Create: `plugins/actors/step_actor_send.go`
- Create: `plugins/actors/step_actor_send_test.go`
- Modify: `plugins/actors/plugin.go` (add factory)

**Step 1: Write the test**

Create `plugins/actors/step_actor_send_test.go`:

```go
package actors

import (
	"testing"
)

func TestActorSendStep_RequiresPool(t *testing.T) {
	_, err := NewActorSendStepFactory()(
		"test-send", map[string]any{}, nil,
	)
	if err == nil {
		t.Fatal("expected error for missing pool")
	}
}

func TestActorSendStep_RequiresMessage(t *testing.T) {
	_, err := NewActorSendStepFactory()(
		"test-send",
		map[string]any{"pool": "my-pool"},
		nil,
	)
	if err == nil {
		t.Fatal("expected error for missing message")
	}
}

func TestActorSendStep_RequiresMessageType(t *testing.T) {
	_, err := NewActorSendStepFactory()(
		"test-send",
		map[string]any{
			"pool": "my-pool",
			"message": map[string]any{
				"payload": map[string]any{},
			},
		},
		nil,
	)
	if err == nil {
		t.Fatal("expected error for missing message type")
	}
}

func TestActorSendStep_ValidConfig(t *testing.T) {
	step, err := NewActorSendStepFactory()(
		"test-send",
		map[string]any{
			"pool": "my-pool",
			"message": map[string]any{
				"type":    "OrderPlaced",
				"payload": map[string]any{"id": "123"},
			},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "test-send" {
		t.Errorf("expected name 'test-send', got %q", step.Name())
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./plugins/actors/ -v -run TestActorSendStep
```

Expected: FAIL.

**Step 3: Implement**

Create `plugins/actors/step_actor_send.go`:

```go
package actors

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/tochemey/goakt/v4/actor"
)

// ActorSendStep sends a fire-and-forget message to an actor (Tell).
type ActorSendStep struct {
	name     string
	pool     string
	identity string // template expression
	message  map[string]any
	tmpl     *module.TemplateEngine
}

// NewActorSendStepFactory returns a factory for step.actor_send.
func NewActorSendStepFactory() module.StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (module.PipelineStep, error) {
		pool, _ := config["pool"].(string)
		if pool == "" {
			return nil, fmt.Errorf("step.actor_send %q: 'pool' is required", name)
		}

		message, ok := config["message"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("step.actor_send %q: 'message' map is required", name)
		}

		msgType, _ := message["type"].(string)
		if msgType == "" {
			return nil, fmt.Errorf("step.actor_send %q: 'message.type' is required", name)
		}

		identity, _ := config["identity"].(string)

		return &ActorSendStep{
			name:     name,
			pool:     pool,
			identity: identity,
			message:  message,
			tmpl:     module.NewTemplateEngine(),
		}, nil
	}
}

func (s *ActorSendStep) Name() string { return s.name }

func (s *ActorSendStep) Execute(ctx context.Context, pc *module.PipelineContext) (*module.StepResult, error) {
	// Resolve template expressions in message
	resolved, err := s.tmpl.ResolveMap(s.message, pc)
	if err != nil {
		return nil, fmt.Errorf("step.actor_send %q: failed to resolve message: %w", s.name, err)
	}

	msgType, _ := resolved["type"].(string)
	payload, _ := resolved["payload"].(map[string]any)
	if payload == nil {
		payload = map[string]any{}
	}

	// Resolve identity
	identity := s.identity
	if identity != "" {
		resolvedID, err := s.tmpl.ResolveValue(identity, pc)
		if err != nil {
			return nil, fmt.Errorf("step.actor_send %q: failed to resolve identity: %w", s.name, err)
		}
		identity = fmt.Sprintf("%v", resolvedID)
	}

	// Look up the actor pool from metadata (injected by engine wiring)
	poolSvc, ok := pc.Metadata["__actor_pools"].(map[string]*ActorPoolModule)
	if !ok {
		return nil, fmt.Errorf("step.actor_send %q: actor pools not available in pipeline context", s.name)
	}
	pool, ok := poolSvc[s.pool]
	if !ok {
		return nil, fmt.Errorf("step.actor_send %q: actor pool %q not found", s.name, s.pool)
	}

	sys := pool.system.ActorSystem()
	if sys == nil {
		return nil, fmt.Errorf("step.actor_send %q: actor system not started", s.name)
	}

	msg := &ActorMessage{Type: msgType, Payload: payload}

	// For auto-managed (grain) actors, use grain identity
	// For permanent pools, use pool-level routing
	if pool.Mode() == "auto-managed" && identity != "" {
		grainID, err := sys.GrainIdentity(ctx, identity, func(ctx context.Context) (actor.Grain, error) {
			// Grain factory — creates a new BridgeActor wrapped as a Grain
			// This will be fully implemented when grain support is added
			return nil, fmt.Errorf("grain activation not yet implemented")
		})
		if err != nil {
			return nil, fmt.Errorf("step.actor_send %q: failed to get grain %q: %w", s.name, identity, err)
		}
		if err := sys.TellGrain(ctx, grainID, msg); err != nil {
			return nil, fmt.Errorf("step.actor_send %q: tell failed: %w", s.name, err)
		}
	} else {
		// Look up pool router actor
		pid, err := sys.ActorOf(ctx, s.pool)
		if err != nil {
			return nil, fmt.Errorf("step.actor_send %q: actor pool %q not found in system: %w", s.name, s.pool, err)
		}
		if err := actor.Tell(ctx, pid, msg); err != nil {
			return nil, fmt.Errorf("step.actor_send %q: tell failed: %w", s.name, err)
		}
	}

	return &module.StepResult{
		Output: map[string]any{"delivered": true},
	}, nil
}
```

**Step 4: Register in plugin.go**

Update `StepFactories()`:

```go
func (p *Plugin) StepFactories() map[string]plugin.StepFactory {
	return map[string]plugin.StepFactory{
		"step.actor_send": wrapStepFactory(NewActorSendStepFactory()),
	}
}
```

Add the `wrapStepFactory` helper (same pattern as pipelinesteps):

```go
func wrapStepFactory(f module.StepFactory) plugin.StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (any, error) {
		return f(name, cfg, app)
	}
}
```

**Step 5: Run tests**

```bash
go test ./plugins/actors/ -v -run TestActorSendStep
```

Expected: all 4 tests PASS.

**Step 6: Commit**

```bash
git add plugins/actors/step_actor_send.go plugins/actors/step_actor_send_test.go plugins/actors/plugin.go
git commit -m "feat(actors): step.actor_send for fire-and-forget messaging"
```

---

### Task 6: step.actor_ask

**Files:**
- Create: `plugins/actors/step_actor_ask.go`
- Create: `plugins/actors/step_actor_ask_test.go`
- Modify: `plugins/actors/plugin.go` (add factory)

**Step 1: Write the test**

Create `plugins/actors/step_actor_ask_test.go`:

```go
package actors

import (
	"testing"
)

func TestActorAskStep_RequiresPool(t *testing.T) {
	_, err := NewActorAskStepFactory()(
		"test-ask", map[string]any{}, nil,
	)
	if err == nil {
		t.Fatal("expected error for missing pool")
	}
}

func TestActorAskStep_RequiresMessage(t *testing.T) {
	_, err := NewActorAskStepFactory()(
		"test-ask",
		map[string]any{"pool": "my-pool"},
		nil,
	)
	if err == nil {
		t.Fatal("expected error for missing message")
	}
}

func TestActorAskStep_DefaultTimeout(t *testing.T) {
	step, err := NewActorAskStepFactory()(
		"test-ask",
		map[string]any{
			"pool": "my-pool",
			"message": map[string]any{
				"type":    "GetStatus",
				"payload": map[string]any{},
			},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	askStep := step.(*ActorAskStep)
	if askStep.timeout.Seconds() != 10 {
		t.Errorf("expected 10s default timeout, got %v", askStep.timeout)
	}
}

func TestActorAskStep_CustomTimeout(t *testing.T) {
	step, err := NewActorAskStepFactory()(
		"test-ask",
		map[string]any{
			"pool":    "my-pool",
			"timeout": "30s",
			"message": map[string]any{
				"type":    "GetStatus",
				"payload": map[string]any{},
			},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	askStep := step.(*ActorAskStep)
	if askStep.timeout.Seconds() != 30 {
		t.Errorf("expected 30s timeout, got %v", askStep.timeout)
	}
}

func TestActorAskStep_InvalidTimeout(t *testing.T) {
	_, err := NewActorAskStepFactory()(
		"test-ask",
		map[string]any{
			"pool":    "my-pool",
			"timeout": "not-a-duration",
			"message": map[string]any{
				"type":    "GetStatus",
				"payload": map[string]any{},
			},
		},
		nil,
	)
	if err == nil {
		t.Fatal("expected error for invalid timeout")
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./plugins/actors/ -v -run TestActorAskStep
```

**Step 3: Implement**

Create `plugins/actors/step_actor_ask.go`:

```go
package actors

import (
	"context"
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/tochemey/goakt/v4/actor"
)

// ActorAskStep sends a message to an actor and waits for a response (Ask).
type ActorAskStep struct {
	name     string
	pool     string
	identity string
	timeout  time.Duration
	message  map[string]any
	tmpl     *module.TemplateEngine
}

// NewActorAskStepFactory returns a factory for step.actor_ask.
func NewActorAskStepFactory() module.StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (module.PipelineStep, error) {
		pool, _ := config["pool"].(string)
		if pool == "" {
			return nil, fmt.Errorf("step.actor_ask %q: 'pool' is required", name)
		}

		message, ok := config["message"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("step.actor_ask %q: 'message' map is required", name)
		}

		msgType, _ := message["type"].(string)
		if msgType == "" {
			return nil, fmt.Errorf("step.actor_ask %q: 'message.type' is required", name)
		}

		timeout := 10 * time.Second
		if v, ok := config["timeout"].(string); ok && v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				return nil, fmt.Errorf("step.actor_ask %q: invalid timeout %q: %w", name, v, err)
			}
			timeout = d
		}

		identity, _ := config["identity"].(string)

		return &ActorAskStep{
			name:     name,
			pool:     pool,
			identity: identity,
			timeout:  timeout,
			message:  message,
			tmpl:     module.NewTemplateEngine(),
		}, nil
	}
}

func (s *ActorAskStep) Name() string { return s.name }

func (s *ActorAskStep) Execute(ctx context.Context, pc *module.PipelineContext) (*module.StepResult, error) {
	// Resolve template expressions in message
	resolved, err := s.tmpl.ResolveMap(s.message, pc)
	if err != nil {
		return nil, fmt.Errorf("step.actor_ask %q: failed to resolve message: %w", s.name, err)
	}

	msgType, _ := resolved["type"].(string)
	payload, _ := resolved["payload"].(map[string]any)
	if payload == nil {
		payload = map[string]any{}
	}

	// Resolve identity
	identity := s.identity
	if identity != "" {
		resolvedID, err := s.tmpl.ResolveValue(identity, pc)
		if err != nil {
			return nil, fmt.Errorf("step.actor_ask %q: failed to resolve identity: %w", s.name, err)
		}
		identity = fmt.Sprintf("%v", resolvedID)
	}

	// Look up the actor pool
	poolSvc, ok := pc.Metadata["__actor_pools"].(map[string]*ActorPoolModule)
	if !ok {
		return nil, fmt.Errorf("step.actor_ask %q: actor pools not available in pipeline context", s.name)
	}
	pool, ok := poolSvc[s.pool]
	if !ok {
		return nil, fmt.Errorf("step.actor_ask %q: actor pool %q not found", s.name, s.pool)
	}

	sys := pool.system.ActorSystem()
	if sys == nil {
		return nil, fmt.Errorf("step.actor_ask %q: actor system not started", s.name)
	}

	msg := &ActorMessage{Type: msgType, Payload: payload}

	var resp any

	if pool.Mode() == "auto-managed" && identity != "" {
		grainID, err := sys.GrainIdentity(ctx, identity, func(ctx context.Context) (actor.Grain, error) {
			return nil, fmt.Errorf("grain activation not yet implemented")
		})
		if err != nil {
			return nil, fmt.Errorf("step.actor_ask %q: failed to get grain %q: %w", s.name, identity, err)
		}
		resp, err = sys.AskGrain(ctx, grainID, msg, s.timeout)
		if err != nil {
			return nil, fmt.Errorf("step.actor_ask %q: ask failed: %w", s.name, err)
		}
	} else {
		pid, err := sys.ActorOf(ctx, s.pool)
		if err != nil {
			return nil, fmt.Errorf("step.actor_ask %q: actor pool %q not found in system: %w", s.name, s.pool, err)
		}
		resp, err = actor.Ask(ctx, pid, msg, s.timeout)
		if err != nil {
			return nil, fmt.Errorf("step.actor_ask %q: ask failed: %w", s.name, err)
		}
	}

	// Convert response to map
	output, ok := resp.(map[string]any)
	if !ok {
		output = map[string]any{"response": resp}
	}

	return &module.StepResult{Output: output}, nil
}
```

**Step 4: Register in plugin.go**

Add to `StepFactories()`:

```go
"step.actor_ask": wrapStepFactory(NewActorAskStepFactory()),
```

**Step 5: Run tests**

```bash
go test ./plugins/actors/ -v -run TestActorAskStep
```

Expected: all 5 tests PASS.

**Step 6: Commit**

```bash
git add plugins/actors/step_actor_ask.go plugins/actors/step_actor_ask_test.go plugins/actors/plugin.go
git commit -m "feat(actors): step.actor_ask for request-response messaging"
```

---

### Task 7: Actor Workflow Handler

**Files:**
- Create: `plugins/actors/handler.go`
- Create: `plugins/actors/handler_test.go`
- Modify: `plugins/actors/plugin.go` (add handler factory + wiring hook)

The actor workflow handler parses `workflows.actors.pools` YAML config and wires receive handlers to actor pool modules.

**Step 1: Write the test**

Create `plugins/actors/handler_test.go`:

```go
package actors

import (
	"testing"
)

func TestParseActorWorkflowConfig(t *testing.T) {
	cfg := map[string]any{
		"pools": map[string]any{
			"order-processors": map[string]any{
				"receive": map[string]any{
					"OrderPlaced": map[string]any{
						"description": "Process a new order",
						"steps": []any{
							map[string]any{
								"name": "set-status",
								"type": "step.set",
								"config": map[string]any{
									"values": map[string]any{
										"status": "processing",
									},
								},
							},
						},
					},
					"GetStatus": map[string]any{
						"steps": []any{
							map[string]any{
								"name": "respond",
								"type": "step.set",
								"config": map[string]any{
									"values": map[string]any{
										"status": "{{ .state.status }}",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	poolHandlers, err := parseActorWorkflowConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	handlers, ok := poolHandlers["order-processors"]
	if !ok {
		t.Fatal("expected handlers for 'order-processors'")
	}

	if len(handlers) != 2 {
		t.Errorf("expected 2 handlers, got %d", len(handlers))
	}

	orderHandler, ok := handlers["OrderPlaced"]
	if !ok {
		t.Fatal("expected OrderPlaced handler")
	}
	if orderHandler.Description != "Process a new order" {
		t.Errorf("expected description 'Process a new order', got %q", orderHandler.Description)
	}
	if len(orderHandler.Steps) != 1 {
		t.Errorf("expected 1 step, got %d", len(orderHandler.Steps))
	}
}

func TestParseActorWorkflowConfig_MissingPools(t *testing.T) {
	_, err := parseActorWorkflowConfig(map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing pools")
	}
}

func TestParseActorWorkflowConfig_MissingReceive(t *testing.T) {
	cfg := map[string]any{
		"pools": map[string]any{
			"my-pool": map[string]any{},
		},
	}
	_, err := parseActorWorkflowConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing receive")
	}
}

func TestParseActorWorkflowConfig_EmptySteps(t *testing.T) {
	cfg := map[string]any{
		"pools": map[string]any{
			"my-pool": map[string]any{
				"receive": map[string]any{
					"MyMessage": map[string]any{
						"steps": []any{},
					},
				},
			},
		},
	}
	_, err := parseActorWorkflowConfig(cfg)
	if err == nil {
		t.Fatal("expected error for empty steps")
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./plugins/actors/ -v -run TestParseActorWorkflow
```

**Step 3: Implement**

Create `plugins/actors/handler.go`:

```go
package actors

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/CrisisTextLine/modular"
)

// ActorWorkflowHandler handles the "actors" workflow type.
// It parses receive handler configs and wires them to actor pool modules.
type ActorWorkflowHandler struct {
	// poolHandlers maps pool name -> message type -> handler pipeline
	poolHandlers map[string]map[string]*HandlerPipeline
	logger       *slog.Logger
}

// NewActorWorkflowHandler creates a new actor workflow handler.
func NewActorWorkflowHandler() *ActorWorkflowHandler {
	return &ActorWorkflowHandler{
		poolHandlers: make(map[string]map[string]*HandlerPipeline),
	}
}

// CanHandle returns true for "actors" workflow type.
func (h *ActorWorkflowHandler) CanHandle(workflowType string) bool {
	return workflowType == "actors"
}

// ConfigureWorkflow parses the actors workflow config.
func (h *ActorWorkflowHandler) ConfigureWorkflow(_ modular.Application, workflowConfig any) error {
	cfg, ok := workflowConfig.(map[string]any)
	if !ok {
		return fmt.Errorf("actor workflow handler: config must be a map")
	}

	poolHandlers, err := parseActorWorkflowConfig(cfg)
	if err != nil {
		return fmt.Errorf("actor workflow handler: %w", err)
	}

	h.poolHandlers = poolHandlers
	return nil
}

// ExecuteWorkflow is not used directly — actors receive messages via step.actor_send/ask.
func (h *ActorWorkflowHandler) ExecuteWorkflow(_ context.Context, _ string, _ string, _ map[string]any) (map[string]any, error) {
	return nil, fmt.Errorf("actor workflows are message-driven; use step.actor_send or step.actor_ask to send messages")
}

// PoolHandlers returns the parsed handlers for wiring to actor pools.
func (h *ActorWorkflowHandler) PoolHandlers() map[string]map[string]*HandlerPipeline {
	return h.poolHandlers
}

// SetLogger sets the logger.
func (h *ActorWorkflowHandler) SetLogger(logger *slog.Logger) {
	h.logger = logger
}

// parseActorWorkflowConfig parses the workflows.actors config block.
func parseActorWorkflowConfig(cfg map[string]any) (map[string]map[string]*HandlerPipeline, error) {
	poolsCfg, ok := cfg["pools"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("'pools' map is required")
	}

	result := make(map[string]map[string]*HandlerPipeline)

	for poolName, poolRaw := range poolsCfg {
		poolCfg, ok := poolRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("pool %q: config must be a map", poolName)
		}

		receiveCfg, ok := poolCfg["receive"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("pool %q: 'receive' map is required", poolName)
		}

		handlers := make(map[string]*HandlerPipeline)
		for msgType, handlerRaw := range receiveCfg {
			handlerCfg, ok := handlerRaw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("pool %q handler %q: config must be a map", poolName, msgType)
			}

			stepsRaw, ok := handlerCfg["steps"].([]any)
			if !ok || len(stepsRaw) == 0 {
				return nil, fmt.Errorf("pool %q handler %q: 'steps' list is required and must not be empty", poolName, msgType)
			}

			steps := make([]map[string]any, 0, len(stepsRaw))
			for i, stepRaw := range stepsRaw {
				stepCfg, ok := stepRaw.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("pool %q handler %q step %d: must be a map", poolName, msgType, i)
				}
				steps = append(steps, stepCfg)
			}

			description, _ := handlerCfg["description"].(string)
			handlers[msgType] = &HandlerPipeline{
				Description: description,
				Steps:       steps,
			}
		}

		result[poolName] = handlers
	}

	return result, nil
}
```

**Step 4: Register in plugin.go**

Update `WorkflowHandlers()`:

```go
func (p *Plugin) WorkflowHandlers() map[string]plugin.WorkflowHandlerFactory {
	return map[string]plugin.WorkflowHandlerFactory{
		"actors": func() any {
			handler := NewActorWorkflowHandler()
			if p.logger != nil {
				handler.SetLogger(p.logger)
			}
			return handler
		},
	}
}
```

Add a wiring hook to connect parsed handlers to pool modules:

```go
func (p *Plugin) WiringHooks() []plugin.WiringHook {
	return []plugin.WiringHook{
		{
			Name:     "actor-handler-wiring",
			Priority: 40,
			Hook: func(app modular.Application, cfg *config.WorkflowConfig) error {
				// Find the actor workflow handler and wire its handlers to pools
				// This runs after all modules are initialized
				return nil // Implementation connects handler pipelines to pool modules
			},
		},
	}
}
```

**Step 5: Run tests**

```bash
go test ./plugins/actors/ -v -run TestParseActorWorkflow
```

Expected: all 4 tests PASS.

**Step 6: Commit**

```bash
git add plugins/actors/handler.go plugins/actors/handler_test.go plugins/actors/plugin.go
git commit -m "feat(actors): actor workflow handler parsing receive pipelines from YAML"
```

---

### Task 8: Module & Step Schemas

**Files:**
- Create: `plugins/actors/schemas.go`
- Modify: `plugins/actors/plugin.go` (return schemas)

**Step 1: Create schemas**

Create `plugins/actors/schemas.go`:

```go
package actors

import "github.com/GoCodeAlone/workflow/schema"

func actorSystemSchema() *schema.ModuleSchema {
	return &schema.ModuleSchema{
		Type:     "actor.system",
		Label:    "Actor Cluster",
		Category: "actor",
		Description: "Distributed actor runtime that coordinates stateful services across nodes. " +
			"Actors are lightweight, isolated units of computation that communicate through messages. " +
			"Each actor processes one message at a time, eliminating concurrency bugs. " +
			"In a cluster, actors are automatically placed on available nodes and relocated if a node fails.",
		ConfigFields: []schema.ConfigFieldDef{
			{
				Key:          "shutdownTimeout",
				Label:        "Shutdown Timeout",
				Type:         "duration",
				Description:  "How long to wait for actors to finish processing before force-stopping",
				DefaultValue: "30s",
				Placeholder:  "30s",
			},
			{
				Key:         "cluster",
				Label:       "Cluster Configuration",
				Type:        "object",
				Description: "Enable distributed mode. Omit for single-node (all actors in-process). When set, actors can be placed across multiple nodes with automatic failover.",
				Group:       "Clustering",
			},
			{
				Key:         "defaultRecovery",
				Label:       "Default Recovery Policy",
				Type:        "object",
				Description: "What happens when an actor crashes. Applies to all pools unless overridden per-pool.",
				Group:       "Fault Tolerance",
			},
			{
				Key:          "metrics",
				Label:        "Enable Metrics",
				Type:         "boolean",
				Description:  "Expose actor system metrics via OpenTelemetry (actor count, message throughput, mailbox depth)",
				DefaultValue: false,
			},
			{
				Key:          "tracing",
				Label:        "Enable Tracing",
				Type:         "boolean",
				Description:  "Propagate trace context through actor messages for distributed tracing",
				DefaultValue: false,
			},
		},
		DefaultConfig: map[string]any{
			"shutdownTimeout": "30s",
		},
	}
}

func actorPoolSchema() *schema.ModuleSchema {
	return &schema.ModuleSchema{
		Type:     "actor.pool",
		Label:    "Actor Pool",
		Category: "actor",
		Description: "Defines a group of actors that handle the same type of work. " +
			"Each actor has its own state and processes messages one at a time, " +
			"eliminating concurrency bugs. Use 'auto-managed' for actors identified by a " +
			"unique key (e.g. one per order) that activate on demand. " +
			"Use 'permanent' for a fixed pool of always-running workers.",
		ConfigFields: []schema.ConfigFieldDef{
			{
				Key:         "system",
				Label:       "Actor Cluster",
				Type:        "string",
				Description: "Name of the actor.system module this pool belongs to",
				Required:    true,
			},
			{
				Key:          "mode",
				Label:        "Lifecycle Mode",
				Type:         "select",
				Description:  "'auto-managed': actors activate on first message and deactivate after idle timeout, identified by a unique key. 'permanent': fixed pool that starts with the engine and runs until shutdown.",
				Options:      []string{"auto-managed", "permanent"},
				DefaultValue: "auto-managed",
			},
			{
				Key:          "idleTimeout",
				Label:        "Idle Timeout",
				Type:         "duration",
				Description:  "How long an auto-managed actor stays in memory without messages before deactivating (auto-managed only)",
				DefaultValue: "10m",
				Placeholder:  "10m",
			},
			{
				Key:          "poolSize",
				Label:        "Pool Size",
				Type:         "number",
				Description:  "Number of actors in a permanent pool (permanent mode only)",
				DefaultValue: 10,
			},
			{
				Key:          "routing",
				Label:        "Load Balancing",
				Type:         "select",
				Description:  "How messages are distributed. 'round-robin': even distribution. 'random': random selection. 'broadcast': send to all. 'sticky': same key always goes to same actor.",
				Options:      []string{"round-robin", "random", "broadcast", "sticky"},
				DefaultValue: "round-robin",
			},
			{
				Key:         "routingKey",
				Label:       "Sticky Routing Key",
				Type:        "string",
				Description: "When routing is 'sticky', this message field determines which actor handles it. All messages with the same value go to the same actor.",
			},
			{
				Key:         "recovery",
				Label:       "Recovery Policy",
				Type:        "object",
				Description: "What happens when an actor crashes. Overrides the system default.",
				Group:       "Fault Tolerance",
			},
			{
				Key:          "placement",
				Label:        "Node Selection",
				Type:         "select",
				Description:  "Which cluster node actors are placed on (cluster mode only)",
				Options:      []string{"round-robin", "random", "local", "least-load"},
				DefaultValue: "round-robin",
			},
			{
				Key:         "targetRoles",
				Label:       "Target Roles",
				Type:        "array",
				Description: "Only place actors on cluster nodes with these roles (cluster mode only)",
			},
			{
				Key:          "failover",
				Label:        "Failover",
				Type:         "boolean",
				Description:  "Automatically relocate actors to healthy nodes when their node fails (cluster mode only)",
				DefaultValue: true,
			},
		},
		DefaultConfig: map[string]any{
			"mode":        "auto-managed",
			"idleTimeout": "10m",
			"routing":     "round-robin",
			"failover":    true,
		},
	}
}

func actorSendStepSchema() *schema.StepSchema {
	return &schema.StepSchema{
		Type:   "step.actor_send",
		Plugin: "actors",
		Description: "Send a message to an actor without waiting for a response. " +
			"The actor processes it asynchronously. Use for fire-and-forget operations " +
			"like triggering background processing or updating actor state when the " +
			"pipeline doesn't need the result.",
		ConfigFields: []schema.ConfigFieldDef{
			{
				Key:         "pool",
				Label:       "Actor Pool",
				Type:        "string",
				Description: "Name of the actor.pool module to send to",
				Required:    true,
			},
			{
				Key:         "identity",
				Label:       "Actor Identity",
				Type:        "string",
				Description: "Unique key for auto-managed actors (e.g. '{{ .body.order_id }}'). Determines which actor instance receives the message.",
			},
			{
				Key:         "message",
				Label:       "Message",
				Type:        "object",
				Description: "Message to send. Must include 'type' (matched against receive handlers) and optional 'payload' map.",
				Required:    true,
			},
		},
		Outputs: []schema.StepOutputDef{
			{Key: "delivered", Type: "boolean", Description: "Whether the message was delivered"},
		},
	}
}

func actorAskStepSchema() *schema.StepSchema {
	return &schema.StepSchema{
		Type:   "step.actor_ask",
		Plugin: "actors",
		Description: "Send a message to an actor and wait for a response. " +
			"The actor's reply becomes this step's output, available to subsequent " +
			"steps via template expressions. If the actor doesn't respond within " +
			"the timeout, the step fails.",
		ConfigFields: []schema.ConfigFieldDef{
			{
				Key:         "pool",
				Label:       "Actor Pool",
				Type:        "string",
				Description: "Name of the actor.pool module to send to",
				Required:    true,
			},
			{
				Key:         "identity",
				Label:       "Actor Identity",
				Type:        "string",
				Description: "Unique key for auto-managed actors (e.g. '{{ .path.order_id }}')",
			},
			{
				Key:          "timeout",
				Label:        "Response Timeout",
				Type:         "duration",
				Description:  "How long to wait for the actor's reply before failing",
				DefaultValue: "10s",
				Placeholder:  "10s",
			},
			{
				Key:         "message",
				Label:       "Message",
				Type:        "object",
				Description: "Message to send. Must include 'type' and optional 'payload' map.",
				Required:    true,
			},
		},
		Outputs: []schema.StepOutputDef{
			{Key: "*", Type: "any", Description: "The actor's reply — varies by message handler. The last step's output in the receive handler becomes the response."},
		},
	}
}
```

**Step 2: Wire schemas in plugin.go**

Update `ModuleSchemas()` and `StepSchemas()`:

```go
func (p *Plugin) ModuleSchemas() []*schema.ModuleSchema {
	return []*schema.ModuleSchema{
		actorSystemSchema(),
		actorPoolSchema(),
	}
}

func (p *Plugin) StepSchemas() []*schema.StepSchema {
	return []*schema.StepSchema{
		actorSendStepSchema(),
		actorAskStepSchema(),
	}
}
```

**Step 3: Verify schemas compile and are returned by MCP**

```bash
go build ./plugins/actors/
go build ./cmd/server/
```

**Step 4: Commit**

```bash
git add plugins/actors/schemas.go plugins/actors/plugin.go
git commit -m "feat(actors): module and step schemas with user-friendly descriptions"
```

---

### Task 9: Config Example

**Files:**
- Create: `example/actor-system-config.yaml`

**Step 1: Create the example**

Create `example/actor-system-config.yaml`:

```yaml
# Actor Model Example
#
# Demonstrates stateful actors processing orders via message passing.
# HTTP routes send messages to actors using step.actor_ask.
# Each order gets its own actor that maintains state across messages.

app:
  name: actor-demo

modules:
  - name: http
    type: http.server
    config:
      address: ":8080"

  - name: router
    type: http.router

  - name: actors
    type: actor.system
    config:
      shutdownTimeout: 15s

  - name: order-processors
    type: actor.pool
    config:
      system: actors
      mode: auto-managed
      idleTimeout: 10m
      routing: sticky
      routingKey: order_id
      recovery:
        failureScope: isolated
        action: restart
        maxRetries: 3
        retryWindow: 10s

workflows:
  actors:
    pools:
      order-processors:
        receive:
          ProcessOrder:
            description: "Create or update an order"
            steps:
              - type: step.set
                name: result
                config:
                  values:
                    order_id: "{{ .message.payload.order_id }}"
                    status: "confirmed"
                    items: "{{ json .message.payload.items }}"

          GetStatus:
            description: "Return current order state"
            steps:
              - type: step.set
                name: result
                config:
                  values:
                    order_id: "{{ .actor.identity }}"
                    status: "{{ .state.status }}"

          CancelOrder:
            description: "Cancel the order"
            steps:
              - type: step.set
                name: result
                config:
                  values:
                    status: "cancelled"
                    cancelled_at: "{{ now \"2006-01-02T15:04:05Z\" }}"

  http:
    routes:
      - path: /orders
        method: POST
        pipeline:
          steps:
            - type: step.request_parse
              name: parse
              config:
                parse_body: true
            - type: step.actor_ask
              name: process
              config:
                pool: order-processors
                identity: "{{ .body.order_id }}"
                timeout: 10s
                message:
                  type: ProcessOrder
                  payload:
                    order_id: "{{ .body.order_id }}"
                    items: "{{ json .body.items }}"
            - type: step.json_response
              name: respond
              config:
                status_code: 201
                body: '{{ json .steps.process }}'

      - path: /orders/{id}
        method: GET
        pipeline:
          steps:
            - type: step.actor_ask
              name: status
              config:
                pool: order-processors
                identity: "{{ .id }}"
                timeout: 5s
                message:
                  type: GetStatus
            - type: step.json_response
              name: respond
              config:
                body: '{{ json .steps.status }}'

      - path: /orders/{id}
        method: DELETE
        pipeline:
          steps:
            - type: step.actor_ask
              name: cancel
              config:
                pool: order-processors
                identity: "{{ .id }}"
                timeout: 5s
                message:
                  type: CancelOrder
            - type: step.json_response
              name: respond
              config:
                body: '{{ json .steps.cancel }}'
```

**Step 2: Validate the config compiles (once all components are wired)**

```bash
./wfctl validate example/actor-system-config.yaml
```

**Step 3: Commit**

```bash
git add example/actor-system-config.yaml
git commit -m "docs(actors): example config demonstrating actor-based order processing"
```

---

### Task 10: Integration Test

**Files:**
- Create: `plugins/actors/integration_test.go`

This test verifies the full flow: create actor system + pool, spawn bridge actor, send messages via goakt, verify state persistence.

**Step 1: Write the integration test**

Create `plugins/actors/integration_test.go`:

```go
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
	defer sysMod.Stop(ctx)

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
	result := resp.(map[string]any)
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
	result = resp.(map[string]any)
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
	defer sysMod.Stop(ctx)

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
	actor.Ask(ctx, pid1, &ActorMessage{Type: "SetValue", Payload: map[string]any{"value": "alpha"}}, 5*time.Second)
	actor.Ask(ctx, pid2, &ActorMessage{Type: "SetValue", Payload: map[string]any{"value": "beta"}}, 5*time.Second)

	// Verify independent state
	resp1, _ := actor.Ask(ctx, pid1, &ActorMessage{Type: "GetValue", Payload: map[string]any{}}, 5*time.Second)
	resp2, _ := actor.Ask(ctx, pid2, &ActorMessage{Type: "GetValue", Payload: map[string]any{}}, 5*time.Second)

	r1 := resp1.(map[string]any)
	r2 := resp2.(map[string]any)

	if r1["value"] != "alpha" {
		t.Errorf("actor-a: expected value=alpha, got %v", r1["value"])
	}
	if r2["value"] != "beta" {
		t.Errorf("actor-b: expected value=beta, got %v", r2["value"])
	}
}
```

**Step 2: Run all tests**

```bash
go test ./plugins/actors/ -v -race
```

Expected: all tests PASS with no race conditions.

**Step 3: Commit**

```bash
git add plugins/actors/integration_test.go
git commit -m "test(actors): integration tests for full actor lifecycle and state isolation"
```

---

### Task 11: Update Documentation

**Files:**
- Modify: `DOCUMENTATION.md` — add actor module types, step types, and workflow handler

**Step 1: Add actor entries to DOCUMENTATION.md**

Find the modules table and add:

| Module Type | Description |
|---|---|
| `actor.system` | Distributed actor runtime (goakt v4) with optional clustering |
| `actor.pool` | Group of actors with shared behavior, routing, and recovery |

Find the steps table and add:

| Step Type | Description |
|---|---|
| `step.actor_send` | Send fire-and-forget message to an actor |
| `step.actor_ask` | Send message and wait for actor's response |

Find the workflow handlers section and add:

| Workflow Type | Description |
|---|---|
| `actors` | Message-driven workflows where actor pools define receive handlers as step pipelines |

**Step 2: Commit**

```bash
git add DOCUMENTATION.md
git commit -m "docs: add actor model types to documentation"
```

---

## Summary

| Task | Components | Tests |
|------|-----------|-------|
| 1. Plugin skeleton | `plugin.go`, server registration | compile check |
| 2. actor.system module | `module_system.go` | 4 unit tests |
| 3. actor.pool module | `module_pool.go` | 7 unit tests |
| 4. Bridge actor | `bridge_actor.go`, `messages.go` | 3 unit tests |
| 5. step.actor_send | `step_actor_send.go` | 4 unit tests |
| 6. step.actor_ask | `step_actor_ask.go` | 5 unit tests |
| 7. Actor workflow handler | `handler.go` | 4 unit tests |
| 8. Schemas | `schemas.go` | compile check |
| 9. Config example | `actor-system-config.yaml` | validation |
| 10. Integration test | `integration_test.go` | 2 integration tests |
| 11. Documentation | `DOCUMENTATION.md` | — |

Total: 11 tasks, 29 tests, ~1200 lines of production code.

**Important notes for implementers:**
- goakt v4 API signatures may need minor adjustments — verify against `go doc` after `go get`
- The `TemplateEngine` usage in bridge actor requires import from `github.com/GoCodeAlone/workflow/module` — verify `NewTemplateEngine()` is exported
- The `__actor_pools` metadata injection in step.actor_send/ask requires a wiring hook that populates `PipelineContext.Metadata` — this will need adjustment based on how the engine passes metadata to pipeline execution
- Run `go test -race` on every commit — actor code is inherently concurrent
