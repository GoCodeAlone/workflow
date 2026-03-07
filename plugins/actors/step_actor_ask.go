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

// Name returns the step name.
func (s *ActorAskStep) Name() string { return s.name }

// Execute sends a request-response message to an actor and returns the response.
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

	// Resolve identity template
	identity := s.identity
	if identity != "" {
		resolvedID, err := s.tmpl.Resolve(identity, pc)
		if err != nil {
			return nil, fmt.Errorf("step.actor_ask %q: failed to resolve identity: %w", s.name, err)
		}
		identity = resolvedID
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

	// Use identity-based actor spawn for auto-managed pools; pool-level actor for permanent
	if pool.Mode() == "auto-managed" && identity != "" {
		pid, err := pool.GetOrSpawnActor(ctx, identity)
		if err != nil {
			return nil, fmt.Errorf("step.actor_ask %q: failed to get actor %q: %w", s.name, identity, err)
		}
		resp, err = actor.Ask(ctx, pid, msg, s.timeout)
		if err != nil {
			return nil, fmt.Errorf("step.actor_ask %q: ask failed: %w", s.name, err)
		}
	} else {
		if pool.system == nil || pool.system.ActorSystem() == nil {
			return nil, fmt.Errorf("step.actor_ask %q: actor system not started", s.name)
		}
		pid, err := pool.system.ActorSystem().ActorOf(ctx, s.pool)
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
