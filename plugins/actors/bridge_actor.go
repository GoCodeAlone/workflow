package actors

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

	"github.com/GoCodeAlone/workflow/module"
	"github.com/tochemey/goakt/v4/actor"
)

// BridgeActor implements the goakt Actor interface and bridges actor messages
// to workflow engine pipeline step execution. Each actor instance maintains
// its own state that persists across messages.
type BridgeActor struct {
	// poolName is the name of the actor pool this actor belongs to.
	poolName string
	// identity is the unique key for this actor instance (e.g. order ID).
	identity string
	// handlers maps message type -> HandlerPipeline.
	handlers map[string]*HandlerPipeline
	// stepRegistry is used to build pipeline steps from configs.
	stepRegistry *module.StepRegistry
	// state is the persistent actor state across messages.
	state map[string]any
	// logger is the application logger.
	logger *slog.Logger
}

// NewBridgeActor creates a new bridge actor with the given handlers.
func NewBridgeActor(
	poolName string,
	identity string,
	handlers map[string]*HandlerPipeline,
	stepRegistry *module.StepRegistry,
	logger *slog.Logger,
) *BridgeActor {
	return &BridgeActor{
		poolName:     poolName,
		identity:     identity,
		handlers:     handlers,
		stepRegistry: stepRegistry,
		state:        make(map[string]any),
		logger:       logger,
	}
}

// PreStart is called once before the actor starts processing messages.
func (b *BridgeActor) PreStart(_ *actor.Context) error {
	if b.logger != nil {
		b.logger.Debug("bridge actor starting", "pool", b.poolName, "identity", b.identity)
	}
	return nil
}

// Receive handles all messages sent to this actor's mailbox.
func (b *BridgeActor) Receive(ctx *actor.ReceiveContext) {
	msg, ok := ctx.Message().(*ActorMessage)
	if !ok {
		// Ignore non-ActorMessage messages silently.
		return
	}

	handler, exists := b.handlers[msg.Type]
	if !exists {
		err := fmt.Errorf("bridge actor %q: no handler for message type %q", b.poolName+"/"+b.identity, msg.Type)
		if b.logger != nil {
			b.logger.Warn("unhandled message type", "pool", b.poolName, "identity", b.identity, "type", msg.Type)
		}
		ctx.Response(&ActorResponse{
			Type:  msg.Type,
			Error: err.Error(),
		})
		return
	}

	// Build trigger data: merge actor state + message payload.
	triggerData := make(map[string]any)
	maps.Copy(triggerData, b.state)
	if msg.Payload != nil {
		maps.Copy(triggerData, msg.Payload)
	}
	triggerData["_actor_pool"] = b.poolName
	triggerData["_actor_identity"] = b.identity
	triggerData["_message_type"] = msg.Type

	// Execute the handler pipeline.
	result, err := b.executePipeline(ctx.Context(), msg.Type, triggerData, handler)
	if err != nil {
		if b.logger != nil {
			b.logger.Error("bridge actor handler failed",
				"pool", b.poolName, "identity", b.identity, "type", msg.Type, "error", err)
		}
		ctx.Response(&ActorResponse{
			Type:  msg.Type,
			Error: err.Error(),
		})
		return
	}

	// Persist state: merge pipeline output back into actor state.
	if result != nil {
		maps.Copy(b.state, result)
	}

	ctx.Response(&ActorResponse{
		Type:   msg.Type,
		Result: result,
	})
}

// PostStop is called when the actor is about to shut down.
func (b *BridgeActor) PostStop(_ *actor.Context) error {
	if b.logger != nil {
		b.logger.Debug("bridge actor stopping", "pool", b.poolName, "identity", b.identity)
	}
	return nil
}

// executePipeline builds and executes a pipeline from a HandlerPipeline config.
func (b *BridgeActor) executePipeline(ctx context.Context, name string, triggerData map[string]any, handler *HandlerPipeline) (map[string]any, error) {
	if b.stepRegistry == nil {
		return triggerData, nil
	}

	// Build pipeline steps from config.
	var steps []module.PipelineStep
	for i, stepCfg := range handler.Steps {
		stepType, _ := stepCfg["type"].(string)
		if stepType == "" {
			return nil, fmt.Errorf("step %d in handler %q missing 'type'", i, name)
		}
		stepName, _ := stepCfg["name"].(string)
		if stepName == "" {
			stepName = fmt.Sprintf("%s-step-%d", name, i)
		}
		step, err := b.stepRegistry.Create(stepType, stepName, stepCfg, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create step %q (type %q): %w", stepName, stepType, err)
		}
		steps = append(steps, step)
	}

	pipeline := &module.Pipeline{
		Name:   fmt.Sprintf("actor-%s-%s", b.poolName, name),
		Steps:  steps,
		Logger: b.logger,
	}

	pc, err := pipeline.Execute(ctx, triggerData)
	if err != nil {
		return nil, err
	}
	return pc.Current, nil
}

// State returns a copy of the actor's current state (for testing).
func (b *BridgeActor) State() map[string]any {
	copy := make(map[string]any)
	maps.Copy(copy, b.state)
	return copy
}
