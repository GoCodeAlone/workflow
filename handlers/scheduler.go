package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/GoCodeAlone/modular"
	workflowmodule "github.com/GoCodeAlone/workflow/module"
)

// ScheduledJobConfig represents a job scheduler configuration
type ScheduledJobConfig struct {
	Scheduler string                 `json:"scheduler" yaml:"scheduler"`
	Job       string                 `json:"job" yaml:"job"`
	Config    map[string]interface{} `json:"config,omitempty" yaml:"config,omitempty"`
}

// SchedulerWorkflowHandler handles scheduler-based workflows
type SchedulerWorkflowHandler struct{}

// NewSchedulerWorkflowHandler creates a new scheduler workflow handler
func NewSchedulerWorkflowHandler() *SchedulerWorkflowHandler {
	return &SchedulerWorkflowHandler{}
}

// CanHandle returns true if this handler can process the given workflow type
func (h *SchedulerWorkflowHandler) CanHandle(workflowType string) bool {
	return workflowType == "scheduler"
}

// ConfigureWorkflow sets up the workflow from configuration
func (h *SchedulerWorkflowHandler) ConfigureWorkflow(app modular.Application, workflowConfig interface{}) error {
	// Convert the generic config to scheduler-specific config
	schedConfig, ok := workflowConfig.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid scheduler workflow configuration")
	}

	// Extract jobs from the configuration
	jobConfigs, ok := schedConfig["jobs"].([]interface{})
	if !ok {
		return fmt.Errorf("jobs not found in scheduler workflow configuration")
	}

	// Find all scheduler services
	schedulers := make(map[string]workflowmodule.Scheduler)

	// Use helper similar to what we did in messaging handler
	helper := GetServiceHelper(app)
	services := helper.Services() // Use the correct method name
	for name, svc := range services {
		if scheduler, ok := svc.(workflowmodule.Scheduler); ok {
			schedulers[name] = scheduler
		}
	}

	if len(schedulers) == 0 {
		return fmt.Errorf("no scheduler services found")
	}

	// Configure each scheduled job
	for i, jc := range jobConfigs {
		jobMap, ok := jc.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid job configuration at index %d", i)
		}

		schedulerName, _ := jobMap["scheduler"].(string)
		jobName, _ := jobMap["job"].(string)

		if schedulerName == "" || jobName == "" {
			return fmt.Errorf("incomplete job configuration at index %d: scheduler and job are required", i)
		}

		// Get scheduler
		scheduler, ok := schedulers[schedulerName]
		if !ok {
			return fmt.Errorf("scheduler service '%s' not found for job", schedulerName)
		}

		// Get job handler
		var jobSvc interface{}
		_ = app.GetService(jobName, &jobSvc)
		if jobSvc == nil {
			return fmt.Errorf("job handler service '%s' not found", jobName)
		}

		job, ok := jobSvc.(workflowmodule.Job)
		if !ok {
			// Try adapting a message handler to a job
			if msgHandler, ok := jobSvc.(workflowmodule.MessageHandler); ok {
				job = workflowmodule.NewMessageHandlerJobAdapter(msgHandler)
			} else {
				return fmt.Errorf("service '%s' does not implement Job interface", jobName)
			}
		}

		// Schedule the job
		if err := scheduler.Schedule(job); err != nil {
			return fmt.Errorf("failed to schedule job '%s': %w", jobName, err)
		}
	}

	return nil
}

// ExecuteWorkflow executes a workflow with the given action and input data
func (h *SchedulerWorkflowHandler) ExecuteWorkflow(ctx context.Context, workflowType string, action string, data map[string]interface{}) (map[string]interface{}, error) {
	// For scheduler workflows, the action represents either a scheduler:job or just a job name
	// The data contains parameters for the job

	// Parse the scheduler and job from the action
	schedulerName := ""
	jobName := action

	if parts := strings.Split(action, ":"); len(parts) > 1 {
		schedulerName = parts[0]
		jobName = parts[1]
	}

	// Extract the scheduler from data if not in action
	if schedulerName == "" {
		schedulerName, _ = data["scheduler"].(string)
	}

	// Get the application from context
	var app modular.Application
	if appVal := ctx.Value("application"); appVal != nil {
		app = appVal.(modular.Application)
	} else {
		return nil, fmt.Errorf("application context not available")
	}

	if jobName == "" {
		return nil, fmt.Errorf("job name not specified")
	}

	// Get the scheduler helper for access to scheduler functions
	helper := &SchedulerTestHelper{App: app}

	// Alternatively, if a specific scheduler is specified, use it
	var scheduler workflowmodule.Scheduler
	if schedulerName != "" {
		err := app.GetService(schedulerName, &scheduler)
		if err != nil || scheduler == nil {
			return nil, fmt.Errorf("scheduler service '%s' not found: %v", schedulerName, err)
		}
	}

	// Directly get the job
	var jobSvc interface{}
	err := app.GetService(jobName, &jobSvc)
	if err != nil || jobSvc == nil {
		return nil, fmt.Errorf("job '%s' not found: %v", jobName, err)
	}

	// Try to execute the job
	var execErr error
	var result map[string]interface{}

	if job, ok := jobSvc.(workflowmodule.Job); ok {
		// Execute the job with the provided context
		// If data contains a "params" field, add it to the context
		var execCtx context.Context = ctx
		if params, ok := data["params"].(map[string]interface{}); ok {
			// Create a context with the parameters
			execCtx = context.WithValue(ctx, "params", params)
		}

		execErr = job.Execute(execCtx)

		// Prepare result
		result = map[string]interface{}{
			"success": execErr == nil,
			"job":     jobName,
		}

		if execErr != nil {
			result["error"] = execErr.Error()
		}
	} else if mh, ok := jobSvc.(workflowmodule.MessageHandler); ok {
		// If it's a message handler, create a message with the data
		var payload []byte
		payload, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize job data: %w", err)
		}

		execErr = mh.HandleMessage(payload)

		// Prepare result
		result = map[string]interface{}{
			"success":     execErr == nil,
			"job":         jobName,
			"handlerType": "messageHandler",
			"payloadSize": len(payload),
		}

		if execErr != nil {
			result["error"] = execErr.Error()
		}
	} else {
		// Try to use the helper to execute the job
		execErr = helper.TriggerJobExecution(ctx, jobName)

		// Prepare result
		result = map[string]interface{}{
			"success":     execErr == nil,
			"job":         jobName,
			"handlerType": "helper",
		}

		if execErr != nil {
			result["error"] = execErr.Error()
		}
	}

	return result, nil
}
