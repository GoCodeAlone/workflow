package module

import (
	"context"
	"fmt"
	"time"

	"github.com/GoCodeAlone/modular"
)

const (
	// ScheduleTriggerName is the standard name for schedule triggers
	ScheduleTriggerName = "trigger.schedule"
)

// ScheduleTriggerConfig represents the configuration for a schedule trigger
type ScheduleTriggerConfig struct {
	Jobs []ScheduleTriggerJob `json:"jobs" yaml:"jobs"`
}

// ScheduleTriggerJob represents a single scheduled job configuration
type ScheduleTriggerJob struct {
	Cron     string                 `json:"cron" yaml:"cron"`
	Workflow string                 `json:"workflow" yaml:"workflow"`
	Action   string                 `json:"action" yaml:"action"`
	Params   map[string]interface{} `json:"params,omitempty" yaml:"params,omitempty"`
}

// ScheduleTrigger implements a trigger that starts workflows based on a schedule
type ScheduleTrigger struct {
	name      string
	namespace ModuleNamespaceProvider
	jobs      []ScheduleTriggerJob
	engine    WorkflowEngine
	scheduler Scheduler
}

// NewScheduleTrigger creates a new schedule trigger
func NewScheduleTrigger() *ScheduleTrigger {
	return NewScheduleTriggerWithNamespace(nil)
}

// NewScheduleTriggerWithNamespace creates a new schedule trigger with namespace support
func NewScheduleTriggerWithNamespace(namespace ModuleNamespaceProvider) *ScheduleTrigger {
	// Default to standard namespace if none provided
	if namespace == nil {
		namespace = NewStandardNamespace("", "")
	}

	return &ScheduleTrigger{
		name:      namespace.FormatName(ScheduleTriggerName),
		namespace: namespace,
		jobs:      make([]ScheduleTriggerJob, 0),
	}
}

// Name returns the name of this trigger
func (t *ScheduleTrigger) Name() string {
	return t.name
}

// Init initializes the trigger
func (t *ScheduleTrigger) Init(app modular.Application) error {
	return app.RegisterService(t.name, t)
}

// Start starts the trigger
func (t *ScheduleTrigger) Start(ctx context.Context) error {
	// If no scheduler is set, we can't start
	if t.scheduler == nil {
		return fmt.Errorf("scheduler not configured for schedule trigger")
	}

	// If no engine is set, we can't start
	if t.engine == nil {
		return fmt.Errorf("workflow engine not configured for schedule trigger")
	}

	// Register all jobs with the scheduler
	for _, job := range t.jobs {
		// Create a job that will trigger the workflow
		scheduledJob := t.createJob(job)

		// Schedule the job
		if err := t.scheduler.Schedule(scheduledJob); err != nil {
			return fmt.Errorf("failed to schedule job for workflow '%s': %w", job.Workflow, err)
		}
	}

	return nil
}

// Stop stops the trigger
func (t *ScheduleTrigger) Stop(ctx context.Context) error {
	// Nothing to do here as the scheduler will be stopped elsewhere
	return nil
}

// Configure sets up the trigger from configuration
func (t *ScheduleTrigger) Configure(app modular.Application, triggerConfig interface{}) error {
	// Convert the generic config to schedule trigger config
	config, ok := triggerConfig.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid schedule trigger configuration format")
	}

	// Extract jobs from configuration
	jobsConfig, ok := config["jobs"].([]interface{})
	if !ok {
		return fmt.Errorf("jobs not found in schedule trigger configuration")
	}

	// Find the scheduler
	var scheduler Scheduler
	schedulerNames := []string{"cronScheduler", "scheduler"}

	for _, name := range schedulerNames {
		var svc interface{}
		if err := app.GetService(name, &svc); err == nil && svc != nil {
			if s, ok := svc.(Scheduler); ok {
				scheduler = s
				break
			}
		}
	}

	if scheduler == nil {
		return fmt.Errorf("scheduler not found")
	}

	// Find the workflow engine
	var engine WorkflowEngine
	engineNames := []string{"workflowEngine", "engine"}

	for _, name := range engineNames {
		var svc interface{}
		if err := app.GetService(name, &svc); err == nil && svc != nil {
			if e, ok := svc.(WorkflowEngine); ok {
				engine = e
				break
			}
		}
	}

	if engine == nil {
		return fmt.Errorf("workflow engine not found")
	}

	// Store scheduler and engine references
	t.scheduler = scheduler
	t.engine = engine

	// Parse jobs
	for i, jc := range jobsConfig {
		jobMap, ok := jc.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid job configuration at index %d", i)
		}

		cron, _ := jobMap["cron"].(string)
		workflow, _ := jobMap["workflow"].(string)
		action, _ := jobMap["action"].(string)

		if cron == "" || workflow == "" || action == "" {
			return fmt.Errorf("incomplete job configuration at index %d: cron, workflow and action are required", i)
		}

		// Get optional params
		params, _ := jobMap["params"].(map[string]interface{})

		// Add the job
		t.jobs = append(t.jobs, ScheduleTriggerJob{
			Cron:     cron,
			Workflow: workflow,
			Action:   action,
			Params:   params,
		})
	}

	return nil
}

// createJob creates a job for a specific scheduled trigger
func (t *ScheduleTrigger) createJob(job ScheduleTriggerJob) Job {
	return NewFunctionJob(func(ctx context.Context) error {
		// Create the data to pass to the workflow
		data := make(map[string]interface{})

		// Add current timestamp
		data["trigger_time"] = time.Now().Format(time.RFC3339)

		// Add any static params from the job configuration
		for k, v := range job.Params {
			data[k] = v
		}

		// Call the workflow engine to trigger the workflow
		return t.engine.TriggerWorkflow(ctx, job.Workflow, job.Action, data)
	})
}
