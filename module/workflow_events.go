package module

import (
	"context"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/CrisisTextLine/modular/modules/eventbus"
)

// Lifecycle constants for workflow and step events.
const (
	LifecycleStarted   = "started"
	LifecycleCompleted = "completed"
	LifecycleFailed    = "failed"
)

// WorkflowTopic returns the event bus topic for a workflow lifecycle event.
// Format: "workflow.<workflowType>.<lifecycle>"
func WorkflowTopic(workflowType, lifecycle string) string {
	return "workflow." + workflowType + "." + lifecycle
}

// StepTopic returns the event bus topic for a step lifecycle event.
// Format: "workflow.<workflowType>.step.<stepName>.<lifecycle>"
func StepTopic(workflowType, stepName, lifecycle string) string {
	return "workflow." + workflowType + ".step." + stepName + "." + lifecycle
}

// WorkflowLifecycleEvent is the payload published for workflow-level lifecycle events.
type WorkflowLifecycleEvent struct {
	WorkflowType string                 `json:"workflowType"`
	Action       string                 `json:"action"`
	Status       string                 `json:"status"`
	Timestamp    time.Time              `json:"timestamp"`
	Duration     time.Duration          `json:"duration,omitempty"`
	Data         map[string]interface{} `json:"data,omitempty"`
	Error        string                 `json:"error,omitempty"`
	Results      map[string]interface{} `json:"results,omitempty"`
}

// StepLifecycleEvent is the payload published for step-level lifecycle events.
type StepLifecycleEvent struct {
	WorkflowType string                 `json:"workflowType"`
	StepName     string                 `json:"stepName"`
	Connector    string                 `json:"connector"`
	Action       string                 `json:"action"`
	Status       string                 `json:"status"`
	Timestamp    time.Time              `json:"timestamp"`
	Duration     time.Duration          `json:"duration,omitempty"`
	Data         map[string]interface{} `json:"data,omitempty"`
	Error        string                 `json:"error,omitempty"`
	Results      map[string]interface{} `json:"results,omitempty"`
}

// WorkflowEventEmitter publishes workflow and step lifecycle events to the EventBus.
// All methods are safe to call when the EventBus is unavailable (nil); they
// silently become no-ops.
type WorkflowEventEmitter struct {
	eventBus *eventbus.EventBusModule
}

// NewWorkflowEventEmitter creates a new emitter. It attempts to resolve the
// "eventbus.provider" service from the application. If the service is
// unavailable the emitter still works but all Emit* calls are no-ops.
func NewWorkflowEventEmitter(app modular.Application) *WorkflowEventEmitter {
	emitter := &WorkflowEventEmitter{}
	var eb *eventbus.EventBusModule
	if err := app.GetService("eventbus.provider", &eb); err == nil && eb != nil {
		emitter.eventBus = eb
	}
	return emitter
}

// EmitWorkflowStarted publishes a "started" lifecycle event for a workflow.
func (e *WorkflowEventEmitter) EmitWorkflowStarted(ctx context.Context, workflowType, action string, data map[string]interface{}) {
	if e.eventBus == nil {
		return
	}
	event := WorkflowLifecycleEvent{
		WorkflowType: workflowType,
		Action:       action,
		Status:       LifecycleStarted,
		Timestamp:    time.Now(),
		Data:         data,
	}
	_ = e.eventBus.Publish(ctx, WorkflowTopic(workflowType, LifecycleStarted), event)
}

// EmitWorkflowCompleted publishes a "completed" lifecycle event for a workflow.
func (e *WorkflowEventEmitter) EmitWorkflowCompleted(ctx context.Context, workflowType, action string, duration time.Duration, results map[string]interface{}) {
	if e.eventBus == nil {
		return
	}
	event := WorkflowLifecycleEvent{
		WorkflowType: workflowType,
		Action:       action,
		Status:       LifecycleCompleted,
		Timestamp:    time.Now(),
		Duration:     duration,
		Results:      results,
	}
	_ = e.eventBus.Publish(ctx, WorkflowTopic(workflowType, LifecycleCompleted), event)
}

// EmitWorkflowFailed publishes a "failed" lifecycle event for a workflow.
func (e *WorkflowEventEmitter) EmitWorkflowFailed(ctx context.Context, workflowType, action string, duration time.Duration, err error) {
	if e.eventBus == nil {
		return
	}
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	event := WorkflowLifecycleEvent{
		WorkflowType: workflowType,
		Action:       action,
		Status:       LifecycleFailed,
		Timestamp:    time.Now(),
		Duration:     duration,
		Error:        errStr,
	}
	_ = e.eventBus.Publish(ctx, WorkflowTopic(workflowType, LifecycleFailed), event)
}

// EmitStepStarted publishes a "started" lifecycle event for a workflow step.
func (e *WorkflowEventEmitter) EmitStepStarted(ctx context.Context, workflowType, stepName, connector, action string) {
	if e.eventBus == nil {
		return
	}
	event := StepLifecycleEvent{
		WorkflowType: workflowType,
		StepName:     stepName,
		Connector:    connector,
		Action:       action,
		Status:       LifecycleStarted,
		Timestamp:    time.Now(),
	}
	_ = e.eventBus.Publish(ctx, StepTopic(workflowType, stepName, LifecycleStarted), event)
}

// EmitStepCompleted publishes a "completed" lifecycle event for a workflow step.
func (e *WorkflowEventEmitter) EmitStepCompleted(ctx context.Context, workflowType, stepName, connector, action string, duration time.Duration, results map[string]interface{}) {
	if e.eventBus == nil {
		return
	}
	event := StepLifecycleEvent{
		WorkflowType: workflowType,
		StepName:     stepName,
		Connector:    connector,
		Action:       action,
		Status:       LifecycleCompleted,
		Timestamp:    time.Now(),
		Duration:     duration,
		Results:      results,
	}
	_ = e.eventBus.Publish(ctx, StepTopic(workflowType, stepName, LifecycleCompleted), event)
}

// EmitStepFailed publishes a "failed" lifecycle event for a workflow step.
func (e *WorkflowEventEmitter) EmitStepFailed(ctx context.Context, workflowType, stepName, connector, action string, duration time.Duration, err error) {
	if e.eventBus == nil {
		return
	}
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	event := StepLifecycleEvent{
		WorkflowType: workflowType,
		StepName:     stepName,
		Connector:    connector,
		Action:       action,
		Status:       LifecycleFailed,
		Timestamp:    time.Now(),
		Duration:     duration,
		Error:        errStr,
	}
	_ = e.eventBus.Publish(ctx, StepTopic(workflowType, stepName, LifecycleFailed), event)
}
