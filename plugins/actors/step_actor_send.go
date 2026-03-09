package actors

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
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
	app      modular.Application
}

// NewActorSendStepFactory returns a factory for step.actor_send.
func NewActorSendStepFactory() module.StepFactory {
	return func(name string, config map[string]any, app modular.Application) (module.PipelineStep, error) {
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
			app:      app,
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

	// Look up the actor pool via service registry
	var pool *ActorPoolModule
	svcName := fmt.Sprintf("actor-pool:%s", s.pool)
	if s.app != nil {
		if err := s.app.GetService(svcName, &pool); err != nil {
			return nil, fmt.Errorf("step.actor_send %q: actor pool %q not found: %w", s.name, s.pool, err)
		}
	} else {
		return nil, fmt.Errorf("step.actor_send %q: no application context available to resolve actor pool", s.name)
	}

	if pool.system == nil || pool.system.ActorSystem() == nil {
		return nil, fmt.Errorf("step.actor_send %q: actor system not started", s.name)
	}
	sys := pool.system.ActorSystem()

	msg := &ActorMessage{Type: msgType, Payload: payload}

	// Auto-managed pools require an identity to address a specific grain
	if pool.Mode() == "auto-managed" && identity == "" {
		return nil, fmt.Errorf("step.actor_send %q: 'identity' is required for auto-managed pool %q", s.name, s.pool)
	}

	// Use Grain API for auto-managed pools; routed actor selection for permanent pools
	if pool.Mode() == "auto-managed" && identity != "" {
		grainID, err := pool.GetGrainIdentity(ctx, identity)
		if err != nil {
			return nil, fmt.Errorf("step.actor_send %q: failed to get grain %q: %w", s.name, identity, err)
		}
		if err := sys.TellGrain(ctx, grainID, msg); err != nil {
			return nil, fmt.Errorf("step.actor_send %q: tell failed: %w", s.name, err)
		}
	} else {
		pids, err := pool.SelectActor(msg)
		if err != nil {
			return nil, fmt.Errorf("step.actor_send %q: %w", s.name, err)
		}
		for _, pid := range pids {
			if err := actor.Tell(ctx, pid, msg); err != nil {
				return nil, fmt.Errorf("step.actor_send %q: tell failed: %w", s.name, err)
			}
		}
	}

	return &module.StepResult{
		Output: map[string]any{"delivered": true},
	}, nil
}
