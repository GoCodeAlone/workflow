package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// StateMachineGetStep reads the current state of a workflow instance.
type StateMachineGetStep struct {
	name         string
	statemachine string
	entityID     string
	app          modular.Application
	tmpl         *TemplateEngine
}

// NewStateMachineGetStepFactory returns a StepFactory for step.statemachine_get.
//
// Config:
//
//	type: step.statemachine_get
//	config:
//	  statemachine: "order-sm"       # service name of the StateMachineEngine
//	  entity_id: "{{.order_id}}"     # which instance to look up (template)
//
// Outputs: current_state (string), entity_id (string).
// Returns an error (stopping the pipeline) when the instance is not found.
func NewStateMachineGetStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		sm, _ := config["statemachine"].(string)
		if sm == "" {
			return nil, fmt.Errorf("statemachine_get step %q: 'statemachine' is required", name)
		}

		entityID, _ := config["entity_id"].(string)
		if entityID == "" {
			return nil, fmt.Errorf("statemachine_get step %q: 'entity_id' is required", name)
		}

		return &StateMachineGetStep{
			name:         name,
			statemachine: sm,
			entityID:     entityID,
			app:          app,
			tmpl:         NewTemplateEngine(),
		}, nil
	}
}

// Name returns the step name.
func (s *StateMachineGetStep) Name() string { return s.name }

// Execute resolves the entity_id template, looks up the StateMachineEngine, and
// returns the current state of the workflow instance.
func (s *StateMachineGetStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("statemachine_get step %q: no application context", s.name)
	}

	svc, ok := s.app.SvcRegistry()[s.statemachine]
	if !ok {
		return nil, fmt.Errorf("statemachine_get step %q: statemachine service %q not found", s.name, s.statemachine)
	}

	engine, ok := svc.(*StateMachineEngine)
	if !ok {
		return nil, fmt.Errorf("statemachine_get step %q: service %q is not a StateMachineEngine", s.name, s.statemachine)
	}

	entityID, err := s.tmpl.Resolve(s.entityID, pc)
	if err != nil {
		return nil, fmt.Errorf("statemachine_get step %q: failed to resolve entity_id: %w", s.name, err)
	}

	instance, err := engine.GetInstance(entityID)
	if err != nil {
		return nil, fmt.Errorf("statemachine_get step %q: instance not found: %w", s.name, err)
	}

	return &StepResult{
		Output: map[string]any{
			"current_state": instance.CurrentState,
			"entity_id":     entityID,
		},
	}, nil
}
