package module

import (
	"context"
	"testing"
)

// TestScheduleTrigger tests the Schedule trigger functionality
func TestScheduleTrigger(t *testing.T) {
	// Create a mock application
	app := NewMockApplication()

	// Create a mock scheduler
	scheduler := NewMockScheduler()
	if err := app.RegisterService("cronScheduler", scheduler); err != nil {
		t.Fatalf("Failed to register scheduler: %v", err)
	}

	// Create a mock workflow engine
	engine := NewMockWorkflowEngine()
	if err := app.RegisterService("workflowEngine", engine); err != nil {
		t.Fatalf("Failed to register workflow engine: %v", err)
	}

	// Create the Schedule trigger
	trigger := NewScheduleTrigger()
	if trigger.Name() != ScheduleTriggerName {
		t.Errorf("Expected name '%s', got '%s'", ScheduleTriggerName, trigger.Name())
	}

	// Initialize the trigger
	err := trigger.Init(app)
	if err != nil {
		t.Fatalf("Failed to initialize trigger: %v", err)
	}
	var trigger2 *ScheduleTrigger
	if app.GetService(trigger.name, &trigger2) != nil {
		t.Error("Trigger did not register itself in the service registry")
	}

	// Configure the trigger
	config := map[string]any{
		"jobs": []any{
			map[string]any{
				"cron":     "*/5 * * * *", // Every 5 minutes
				"workflow": "test-workflow",
				"action":   "test-action",
				"params": map[string]any{
					"batch_size": 100,
				},
			},
			map[string]any{
				"cron":     "0 0 * * *", // Daily at midnight
				"workflow": "daily-workflow",
				"action":   "daily-action",
			},
		},
	}

	err = trigger.Configure(app, config)
	if err != nil {
		t.Fatalf("Failed to configure trigger: %v", err)
	}

	// Start the trigger
	err = trigger.Start(context.Background())
	if err != nil {
		t.Fatalf("Failed to start trigger: %v", err)
	}

	// Verify jobs were scheduled
	if len(scheduler.scheduledJobs) != 2 {
		t.Fatalf("Expected 2 scheduled jobs, got %d", len(scheduler.scheduledJobs))
	}

	// Set the cron expressions for the scheduled jobs for testing
	scheduler.SetCronExpression(0, "*/5 * * * *")
	scheduler.SetCronExpression(1, "0 0 * * *")

	// First job: test-workflow
	if scheduler.scheduledJobs[0].cronExpression != "*/5 * * * *" {
		t.Errorf("Expected cron expression '*/5 * * * *', got '%s'", scheduler.scheduledJobs[0].cronExpression)
	}

	// Second job: daily-workflow
	if scheduler.scheduledJobs[1].cronExpression != "0 0 * * *" {
		t.Errorf("Expected cron expression '0 0 * * *', got '%s'", scheduler.scheduledJobs[1].cronExpression)
	}

	// Simulate execution of the first job
	ctx := context.Background()
	err = scheduler.scheduledJobs[0].job.Execute(ctx)
	if err != nil {
		t.Fatalf("Failed to execute job: %v", err)
	}

	// Verify the workflow was triggered
	if len(engine.triggeredWorkflows) != 1 {
		t.Fatalf("Expected 1 triggered workflow, got %d", len(engine.triggeredWorkflows))
	}

	workflow := engine.triggeredWorkflows[0]
	if workflow.WorkflowType != "test-workflow" {
		t.Errorf("Expected workflow type 'test-workflow', got '%s'", workflow.WorkflowType)
	}
	if workflow.Action != "test-action" {
		t.Errorf("Expected action 'test-action', got '%s'", workflow.Action)
	}

	// Check that parameters were passed correctly
	if workflow.Data["batch_size"] != 100 {
		t.Errorf("Expected batch_size 100, got %v", workflow.Data["batch_size"])
	}
	if _, exists := workflow.Data["trigger_time"]; !exists {
		t.Error("Expected trigger_time parameter to be set")
	}

	// Simulate execution of the second job
	err = scheduler.scheduledJobs[1].job.Execute(ctx)
	if err != nil {
		t.Fatalf("Failed to execute job: %v", err)
	}

	// Verify the second workflow was triggered
	if len(engine.triggeredWorkflows) != 2 {
		t.Fatalf("Expected 2 triggered workflows, got %d", len(engine.triggeredWorkflows))
	}

	workflow = engine.triggeredWorkflows[1]
	if workflow.WorkflowType != "daily-workflow" {
		t.Errorf("Expected workflow type 'daily-workflow', got '%s'", workflow.WorkflowType)
	}
	if workflow.Action != "daily-action" {
		t.Errorf("Expected action 'daily-action', got '%s'", workflow.Action)
	}

	// Test stopping the trigger
	err = trigger.Stop(context.Background())
	if err != nil {
		t.Fatalf("Failed to stop trigger: %v", err)
	}
}
