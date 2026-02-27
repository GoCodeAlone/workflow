package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// StateMachineTransitionStep triggers a state machine transition from within a pipeline.
type StateMachineTransitionStep struct {
	name         string
	statemachine string
	entityID     string
	event        string
	data         map[string]any
	failOnError  bool
	app          modular.Application
	tmpl         *TemplateEngine
}

// NewStateMachineTransitionStepFactory returns a StepFactory for step.statemachine_transition.
//
// Config:
//
//	type: step.statemachine_transition
//	config:
//	  statemachine: "order-sm"           # service name of the StateMachineEngine
//	  entity_id: "{{.order_id}}"         # which instance to transition (template)
//	  event: "approve"                   # transition name
//	  data:                              # optional data map (values may use templates)
//	    approved_by: "{{.user_id}}"
//	  fail_on_error: false               # stop pipeline on invalid transition (default: false)
//
// Outputs: transition_ok (bool), new_state (string), error (string, only on failure).
func NewStateMachineTransitionStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		sm, _ := config["statemachine"].(string)
		if sm == "" {
			return nil, fmt.Errorf("statemachine_transition step %q: 'statemachine' is required", name)
		}

		entityID, _ := config["entity_id"].(string)
		if entityID == "" {
			return nil, fmt.Errorf("statemachine_transition step %q: 'entity_id' is required", name)
		}

		event, _ := config["event"].(string)
		if event == "" {
			return nil, fmt.Errorf("statemachine_transition step %q: 'event' is required", name)
		}

		var data map[string]any
		if d, ok := config["data"].(map[string]any); ok {
			data = d
		}

		failOnError, _ := config["fail_on_error"].(bool)

		return &StateMachineTransitionStep{
			name:         name,
			statemachine: sm,
			entityID:     entityID,
			event:        event,
			data:         data,
			failOnError:  failOnError,
			app:          app,
			tmpl:         NewTemplateEngine(),
		}, nil
	}
}

// Name returns the step name.
func (s *StateMachineTransitionStep) Name() string { return s.name }

// Execute resolves templates, looks up the StateMachineEngine by service name, and
// triggers the requested transition. On success it sets transition_ok=true and
// new_state to the resulting state. On failure it sets transition_ok=false and
// error to the error message; if fail_on_error is true the pipeline is stopped.
func (s *StateMachineTransitionStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("statemachine_transition step %q: no application context", s.name)
	}

	// Resolve statemachine engine from service registry
	svc, ok := s.app.SvcRegistry()[s.statemachine]
	if !ok {
		return nil, fmt.Errorf("statemachine_transition step %q: statemachine service %q not found", s.name, s.statemachine)
	}

	engine, ok := svc.(*StateMachineEngine)
	if !ok {
		// Also accept the TransitionTrigger interface for testability / mocking
		trigger, ok := svc.(TransitionTrigger)
		if !ok {
			return nil, fmt.Errorf("statemachine_transition step %q: service %q does not implement StateMachineEngine or TransitionTrigger", s.name, s.statemachine)
		}
		return s.executeViaTrigger(ctx, pc, trigger)
	}

	return s.executeViaEngine(ctx, pc, engine)
}

func (s *StateMachineTransitionStep) executeViaEngine(ctx context.Context, pc *PipelineContext, engine *StateMachineEngine) (*StepResult, error) {
	entityID, err := s.tmpl.Resolve(s.entityID, pc)
	if err != nil {
		return nil, fmt.Errorf("statemachine_transition step %q: failed to resolve entity_id: %w", s.name, err)
	}

	event, err := s.tmpl.Resolve(s.event, pc)
	if err != nil {
		return nil, fmt.Errorf("statemachine_transition step %q: failed to resolve event: %w", s.name, err)
	}

	data, err := s.tmpl.ResolveMap(s.data, pc)
	if err != nil {
		return nil, fmt.Errorf("statemachine_transition step %q: failed to resolve data: %w", s.name, err)
	}

	transErr := engine.TriggerTransition(ctx, entityID, event, data)
	if transErr != nil {
		if s.failOnError {
			return nil, fmt.Errorf("statemachine_transition step %q: transition failed: %w", s.name, transErr)
		}
		return &StepResult{
			Output: map[string]any{
				"transition_ok": false,
				"error":         transErr.Error(),
			},
		}, nil
	}

	// Fetch the new state from the engine (read-back failure is non-fatal)
	newState := ""
	if instance, lookupErr := engine.GetInstance(entityID); lookupErr == nil {
		newState = instance.CurrentState
	}

	return &StepResult{
		Output: map[string]any{
			"transition_ok": true,
			"new_state":     newState,
		},
	}, nil
}

func (s *StateMachineTransitionStep) executeViaTrigger(ctx context.Context, pc *PipelineContext, trigger TransitionTrigger) (*StepResult, error) {
	entityID, err := s.tmpl.Resolve(s.entityID, pc)
	if err != nil {
		return nil, fmt.Errorf("statemachine_transition step %q: failed to resolve entity_id: %w", s.name, err)
	}

	event, err := s.tmpl.Resolve(s.event, pc)
	if err != nil {
		return nil, fmt.Errorf("statemachine_transition step %q: failed to resolve event: %w", s.name, err)
	}

	data, err := s.tmpl.ResolveMap(s.data, pc)
	if err != nil {
		return nil, fmt.Errorf("statemachine_transition step %q: failed to resolve data: %w", s.name, err)
	}

	transErr := trigger.TriggerTransition(ctx, entityID, event, data)
	if transErr != nil {
		if s.failOnError {
			return nil, fmt.Errorf("statemachine_transition step %q: transition failed: %w", s.name, transErr)
		}
		return &StepResult{
			Output: map[string]any{
				"transition_ok": false,
				"error":         transErr.Error(),
			},
		}, nil
	}

	return &StepResult{
		Output: map[string]any{
			"transition_ok": true,
			"new_state":     "",
		},
	}, nil
}
