package handlers

import (
	"fmt"

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
