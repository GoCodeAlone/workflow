package actors

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/module"
	goaktactor "github.com/tochemey/goakt/v4/actor"
)

// NewBridgeActor creates a BridgeActor ready to be spawned.
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

// BridgeActor is a goakt Actor that executes workflow step pipelines
// when it receives messages. It bridges the actor model with the
// pipeline execution model.
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

// Receive handles incoming messages by dispatching to the appropriate
// handler pipeline.
func (a *BridgeActor) Receive(ctx *goaktactor.ReceiveContext) {
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
		_ = msg
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
