package handlers

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/mock"
	"github.com/GoCodeAlone/workflow/module"
)

// TestSchedulerWorkflow tests the scheduler workflow handler
func TestSchedulerWorkflow(t *testing.T) {
	// Create the application (do NOT call Init() here; BuildFromConfig will call it)
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), &mock.Logger{LogEntries: make([]string, 0)})

	// Create test helper
	testHelper := NewSchedulerTestHelper(app)

	// Create workflow engine
	engine := workflow.NewStdEngine(app, &mock.Logger{LogEntries: make([]string, 0)})
	loadAllPlugins(t, engine)

	// Register workflow handlers
	engine.RegisterWorkflowHandler(NewSchedulerWorkflowHandler())

	// Mock job for testing using our helper
	var mockJobExecuted int32 // Use atomic int32 instead of bool
	testHelper.RegisterTestJob("test-job", func(ctx context.Context) error {
		atomic.StoreInt32(&mockJobExecuted, 1)
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
				Config: map[string]any{
					"cronExpression": "* * * * *",
				},
			},
		},
		Workflows: map[string]any{
			"scheduler": map[string]any{
				"jobs": []any{
					map[string]any{
						"scheduler": "cron-scheduler",
						"job":       "test-job",
					},
				},
			},
		},
	}

	// Add the cron scheduler module to the engine
	engine.AddModuleType("scheduler.cron", func(name string, config map[string]any) modular.Module {
		cronExpression := "* * * * *"
		if expr, ok := config["cronExpression"].(string); ok {
			cronExpression = expr
		}
		return module.NewCronScheduler(name, cronExpression)
	})

	// Build workflow
	err := engine.BuildFromConfig(cfg)
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
		if err := testHelper.TriggerJobExecution(ctx, "test-job"); err != nil {
			t.Errorf("Failed to trigger job execution: %v", err)
		}
	}()

	// Wait for job execution
	time.Sleep(1100 * time.Millisecond)

	// Stop engine
	err = engine.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop workflow: %v", err)
	}

	// Verify job executed
	if atomic.LoadInt32(&mockJobExecuted) == 0 {
		t.Errorf("Expected scheduler job to be executed")
	}
}

// The applicationServiceRegistryAdapter is now defined in test_helpers.go
