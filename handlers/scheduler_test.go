package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/mock"
	"github.com/GoCodeAlone/workflow/module"
)

// TestSchedulerWorkflow tests the scheduler workflow handler
func TestSchedulerWorkflow(t *testing.T) {
	// Create and initialize the application properly
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), &mock.Logger{LogEntries: make([]string, 0)})
	err := app.Init()
	if err != nil {
		t.Fatalf("Failed to initialize app: %v", err)
	}

	// Create test helper
	testHelper := NewSchedulerTestHelper(app)

	// Create workflow engine
	engine = workflow.NewStdEngine(app, &mock.Logger{LogEntries: make([]string, 0)})

	// Register workflow handlers
	engine.RegisterWorkflowHandler(NewSchedulerWorkflowHandler())

	// Mock job for testing using our helper
	mockJobExecuted := false
	testHelper.RegisterTestJob("test-job", func(ctx context.Context) error {
		mockJobExecuted = true
		return nil
	})

	// Create and register the scheduler
	app.RegisterModule(module.NewCronScheduler("cron-scheduler", "* * * * *"))

	// Create a minimal scheduler workflow configuration
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "cron-scheduler",
				Type: "scheduler.cron",
				Config: map[string]interface{}{
					"cronExpression": "* * * * *",
				},
			},
		},
		Workflows: map[string]interface{}{
			"scheduler": map[string]interface{}{
				"jobs": []interface{}{
					map[string]interface{}{
						"scheduler": "cron-scheduler",
						"job":       "test-job",
					},
				},
			},
		},
	}

	// Add the cron scheduler module to the engine
	engine.AddModuleType("scheduler.cron", func(name string, config map[string]interface{}) modular.Module {
		cronExpression := "* * * * *"
		if expr, ok := config["cronExpression"].(string); ok {
			cronExpression = expr
		}
		return module.NewCronScheduler(name, cronExpression)
	})

	// Build workflow
	err = engine.BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to build workflow: %v", err)
	}

	// Start engine
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = engine.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start workflow: %v", err)
	}

	// Manually trigger the job instead of relying on ExecuteImmediately
	go func() {
		testHelper.TriggerJobExecution(ctx, "test-job")
	}()

	// Wait for job execution
	time.Sleep(1100 * time.Millisecond)

	// Stop engine
	err = engine.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop workflow: %v", err)
	}

	// Verify job executed
	if !mockJobExecuted {
		t.Errorf("Expected scheduler job to be executed")
	}
}

// The applicationServiceRegistryAdapter is now defined in test_helpers.go
