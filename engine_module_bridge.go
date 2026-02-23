package workflow

// engine_module_bridge.go bridges the engine core (engine.go) with concrete
// module implementations that cannot yet be abstracted away without a larger
// refactor. This file intentionally imports the module package so that
// engine.go itself need not import it for these specific operations.
//
// Remaining blockers preventing full engine.go ↔ module decoupling:
//
//  1. module.StepRegistry / module.StepFactory / module.PipelineStep — the
//     StepFactory signature returns (PipelineStep, error), and PipelineStep.Execute
//     takes *PipelineContext, both of which are concrete types in the module package.
//     Moving them to interfaces would require updating 70+ files in module/. Deferred.
//
//  2. module.Pipeline struct construction (configurePipelines,
//     configureRoutePipelines, buildPipelineSteps) — depends on module.PipelineStep
//     slices and module.ErrorStrategy constants. These functions live in engine.go
//     and are candidates for moving here once StepFactory is abstracted.
//
// What HAS been cleaned up in this phase:
//   - module.Trigger → interfaces.Trigger (type alias in module; engine uses interfaces)
//   - module.WorkflowEventEmitter → interfaces.EventEmitter (engine field + bridge ctor)
//   - module.TriggerRegistry → interfaces.TriggerRegistrar (engine field + bridge ctor)
//   - recordWorkflowMetrics → uses interfaces.MetricsRecorder (no concrete *MetricsCollector)
//   - Plugin step/trigger wiring → bridge helpers (registerPluginSteps, registerPluginTrigger)

import (
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
)

// newTriggerRegistrar creates the default concrete trigger registry.
// Called from NewStdEngine so that engine.go need not import module.
func newTriggerRegistrar() interfaces.TriggerRegistrar {
	return module.NewTriggerRegistry()
}

// newStepRegistry creates the default concrete step registry.
// *module.StepRegistry satisfies interfaces.StepRegistryProvider and is used
// as a concrete type in engine.go until StepFactory is fully abstracted.
func newStepRegistry() *module.StepRegistry {
	return module.NewStepRegistry()
}

// newEventEmitter creates a WorkflowEventEmitter from the application service
// registry. Called from BuildFromConfig after app.Init().
func newEventEmitter(app modular.Application) interfaces.EventEmitter {
	return module.NewWorkflowEventEmitter(app)
}

// recordWorkflowMetrics records workflow execution metrics if the metrics
// collector service is available. Uses interfaces.MetricsRecorder so that
// engine.go need not hold a concrete *module.MetricsCollector pointer.
func (e *StdEngine) recordWorkflowMetrics(workflowType, action, status string, duration time.Duration) {
	var mr interfaces.MetricsRecorder
	if err := e.app.GetService("metrics.collector", &mr); err == nil && mr != nil {
		mr.RecordWorkflowExecution(workflowType, action, status)
		mr.RecordWorkflowDuration(workflowType, action, duration)
	}
}

// registerPluginSteps wires step factories from a plugin into the engine's
// step registry. Lives here (instead of LoadPlugin in engine.go) because it
// type-asserts the factory result to module.PipelineStep.
func (e *StdEngine) registerPluginSteps(typeName string, stepFactory func(name string, cfg map[string]any, app modular.Application) (any, error)) {
	capturedType := typeName
	e.stepRegistry.Register(typeName, func(name string, cfg map[string]any, app modular.Application) (module.PipelineStep, error) {
		result, err := stepFactory(name, cfg, app)
		if err != nil {
			return nil, err
		}
		if step, ok := result.(module.PipelineStep); ok {
			return step, nil
		}
		return nil, fmt.Errorf("step factory for %q returned non-PipelineStep type", capturedType)
	})
}

// registerPluginTrigger wires a trigger from a plugin into the engine.
// Lives here to avoid a direct module.Trigger type assertion in engine.go.
// Since module.Trigger is now an alias for interfaces.Trigger, the assertion
// uses the canonical interface type.
func (e *StdEngine) registerPluginTrigger(triggerType string, factory func() any) {
	result := factory()
	if trigger, ok := result.(interfaces.Trigger); ok {
		e.triggerTypeMap[triggerType] = trigger.Name()
		e.RegisterTrigger(trigger)
	}
}
