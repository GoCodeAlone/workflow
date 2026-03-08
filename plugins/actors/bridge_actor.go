package actors

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/module"
	goaktactor "github.com/tochemey/goakt/v4/actor"
)

// NewBridgeActor creates a BridgeActor ready to be spawned into a permanent pool.
func NewBridgeActor(poolName, identity string, handlers map[string]*HandlerPipeline, registry *module.StepRegistry, app modular.Application, logger *slog.Logger) *BridgeActor {
	return &BridgeActor{
		poolName: poolName,
		identity: identity,
		handlers: handlers,
		registry: registry,
		app:      app,
		logger:   logger,
	}
}

// State returns a copy of the actor's current internal state (for testing/inspection).
func (a *BridgeActor) State() map[string]any { return copyMap(a.state) }

// BridgeActor is a goakt Actor (PreStart/Receive/PostStop) used for permanent pools.
// It executes workflow step pipelines when it receives messages.
type BridgeActor struct {
	poolName string
	identity string
	state    map[string]any
	handlers map[string]*HandlerPipeline

	// Injected dependencies (set before spawning)
	registry *module.StepRegistry
	app      modular.Application
	logger   *slog.Logger
}

// PreStart initializes the actor.
func (a *BridgeActor) PreStart(_ *goaktactor.Context) error {
	if a.state == nil {
		a.state = make(map[string]any)
	}
	return nil
}

// PostStop cleans up the actor.
func (a *BridgeActor) PostStop(_ *goaktactor.Context) error {
	return nil
}

// Receive handles incoming messages by dispatching to the appropriate handler pipeline.
func (a *BridgeActor) Receive(ctx *goaktactor.ReceiveContext) {
	switch msg := ctx.Message().(type) {
	case *ActorMessage:
		result, err := executePipeline(ctx.Context(), msg, a.poolName, a.identity, a.state, a.handlers, a.registry, a.app)
		if err != nil {
			ctx.Err(err)
			return
		}
		ctx.Response(result)
	default:
		// Ignore system messages (PostStart, PoisonPill, etc.)
		_ = msg
	}
}

// BridgeGrain is a goakt Grain (OnActivate/OnReceive/OnDeactivate) used for auto-managed pools.
// Grains are virtual actors: activated on first message, passivated after idleTimeout.
type BridgeGrain struct {
	poolName string
	state    map[string]any
	handlers map[string]*HandlerPipeline

	registry *module.StepRegistry
	app      modular.Application
	logger   *slog.Logger
}

// OnActivate initializes grain state when the grain is loaded into memory.
func (g *BridgeGrain) OnActivate(_ context.Context, _ *goaktactor.GrainProps) error {
	if g.state == nil {
		g.state = make(map[string]any)
	}
	return nil
}

// OnReceive dispatches an ActorMessage to the matching handler pipeline.
func (g *BridgeGrain) OnReceive(ctx *goaktactor.GrainContext) {
	msg, ok := ctx.Message().(*ActorMessage)
	if !ok {
		ctx.Unhandled()
		return
	}
	identity := ctx.Self().Name()
	result, err := executePipeline(ctx.Context(), msg, g.poolName, identity, g.state, g.handlers, g.registry, g.app)
	if err != nil {
		ctx.Err(err)
		return
	}
	ctx.Response(result)
}

// OnDeactivate is called when the grain is passivated (idle timeout reached).
func (g *BridgeGrain) OnDeactivate(_ context.Context, _ *goaktactor.GrainProps) error {
	return nil
}

// buildStep creates a step instance from a step config map.
func buildStep(stepType, stepName string, stepCfg map[string]any, registry *module.StepRegistry, app modular.Application) (module.PipelineStep, error) {
	config, _ := stepCfg["config"].(map[string]any)
	switch {
	case registry != nil:
		return registry.Create(stepType, stepName, config, app)
	case stepType == "step.set":
		factory := module.NewSetStepFactory()
		return factory(stepName, config, nil)
	default:
		return nil, fmt.Errorf("no step registry available for type %q", stepType)
	}
}

// executePipeline finds the handler for msg.Type, runs the step pipeline, updates state,
// and returns the accumulated output. Shared by BridgeActor and BridgeGrain.
func executePipeline(ctx context.Context, msg *ActorMessage, poolName, identity string, state map[string]any, handlers map[string]*HandlerPipeline, registry *module.StepRegistry, app modular.Application) (map[string]any, error) {
	handler, ok := handlers[msg.Type]
	if !ok {
		return nil, fmt.Errorf("no handler for message type %q", msg.Type)
	}

	triggerData := map[string]any{
		"message": map[string]any{
			"type":    msg.Type,
			"payload": msg.Payload,
		},
		"state": copyMap(state),
		"actor": map[string]any{
			"identity": identity,
			"pool":     poolName,
		},
	}

	pc := module.NewPipelineContext(triggerData, map[string]any{
		"actor_pool":     poolName,
		"actor_identity": identity,
		"message_type":   msg.Type,
	})

	output := map[string]any{}
	for _, stepCfg := range handler.Steps {
		stepType, _ := stepCfg["type"].(string)
		stepName, _ := stepCfg["name"].(string)

		if stepType == "" || stepName == "" {
			return nil, fmt.Errorf("handler %q: step missing 'type' or 'name'", msg.Type)
		}

		// Build a fresh step instance per execution to avoid sharing mutable
		// state across concurrent actors in the same pool.
		step, err := buildStep(stepType, stepName, stepCfg, registry, app)
		if err != nil {
			return nil, fmt.Errorf("handler %q step %q: %w", msg.Type, stepName, err)
		}

		result, err := step.Execute(ctx, pc)
		if err != nil {
			return nil, fmt.Errorf("handler %q step %q failed: %w", msg.Type, stepName, err)
		}

		if result != nil && result.Output != nil {
			pc.MergeStepOutput(stepName, result.Output)
			// Accumulate all step outputs into the combined output
			for k, v := range result.Output {
				output[k] = v
			}
		}

		if result != nil && result.Stop {
			break
		}
	}

	// Merge accumulated output into actor state
	for k, v := range output {
		state[k] = v
	}

	return output, nil
}

// copyMap creates a shallow copy of a map.
func copyMap(m map[string]any) map[string]any {
	cp := make(map[string]any, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}
