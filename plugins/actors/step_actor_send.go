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

// Name returns the step name.
func (s *ActorSendStep) Name() string { return s.name }

// Execute sends a fire-and-forget message to an actor pool.
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

	// Resolve identity template
	identity := s.identity
	if identity != "" {
		resolvedID, err := s.tmpl.Resolve(identity, pc)
		if err != nil {
			return nil, fmt.Errorf("step.actor_send %q: failed to resolve identity: %w", s.name, err)
		}
		identity = resolvedID
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

	if pool.Mode() == "auto-managed" && identity != "" {
		grainID, err := sys.GrainIdentity(ctx, identity, func(_ context.Context) (actor.Grain, error) {
			return nil, fmt.Errorf("grain activation not yet implemented")
		})
		if err != nil {
			return nil, fmt.Errorf("step.actor_send %q: failed to get grain %q: %w", s.name, identity, err)
		}
		if err := sys.TellGrain(ctx, grainID, msg); err != nil {
			return nil, fmt.Errorf("step.actor_send %q: tell failed: %w", s.name, err)
		}
	} else {
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
