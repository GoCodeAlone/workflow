package handlers

import (
	"context"
	"fmt"
	"testing"

	"github.com/CrisisTextLine/modular"
	workflowmodule "github.com/GoCodeAlone/workflow/module"
)

// mockJob implements workflowmodule.Job
type mockJob struct {
	executeFn func(ctx context.Context) error
}

func (j *mockJob) Execute(ctx context.Context) error {
	if j.executeFn != nil {
		return j.executeFn(ctx)
	}
	return nil
}

// mockScheduler implements workflowmodule.Scheduler
type mockScheduler struct {
	jobs []workflowmodule.Job
}

func (s *mockScheduler) Schedule(job workflowmodule.Job) error {
	s.jobs = append(s.jobs, job)
	return nil
}

func (s *mockScheduler) Start(ctx context.Context) error { return nil }
func (s *mockScheduler) Stop(ctx context.Context) error  { return nil }

// mockMsgHandler implements workflowmodule.MessageHandler
type mockMsgHandler struct {
	lastMsg []byte
}

func (m *mockMsgHandler) HandleMessage(message []byte) error {
	m.lastMsg = message
	return nil
}

func TestSchedulerExecuteWorkflow_JobExecution(t *testing.T) {
	h := NewSchedulerWorkflowHandler()
	app := newMockApp()

	executed := false
	job := &mockJob{executeFn: func(ctx context.Context) error {
		executed = true
		return nil
	}}
	app.services["my-job"] = job

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	result, err := h.ExecuteWorkflow(ctx, "scheduler", "my-job", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !executed {
		t.Error("expected job to be executed")
	}
	if result["success"] != true {
		t.Errorf("expected success=true, got %v", result["success"])
	}
}

func TestSchedulerExecuteWorkflow_JobExecutionWithParams(t *testing.T) {
	h := NewSchedulerWorkflowHandler()
	app := newMockApp()

	var capturedCtx context.Context
	job := &mockJob{executeFn: func(ctx context.Context) error {
		capturedCtx = ctx
		return nil
	}}
	app.services["my-job"] = job

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	data := map[string]any{
		"params": map[string]any{
			"key": "value",
		},
	}

	_, err := h.ExecuteWorkflow(ctx, "scheduler", "my-job", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the params context was set
	if capturedCtx == nil {
		t.Fatal("expected context to be captured")
	}
}

func TestSchedulerExecuteWorkflow_JobExecutionError(t *testing.T) {
	h := NewSchedulerWorkflowHandler()
	app := newMockApp()

	job := &mockJob{executeFn: func(ctx context.Context) error {
		return fmt.Errorf("job failed")
	}}
	app.services["my-job"] = job

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	result, err := h.ExecuteWorkflow(ctx, "scheduler", "my-job", map[string]any{})
	if err != nil {
		t.Fatalf("expected no error from ExecuteWorkflow, got: %v", err)
	}
	if result["success"] != false {
		t.Errorf("expected success=false, got %v", result["success"])
	}
	if result["error"] == nil {
		t.Error("expected error in result")
	}
}

func TestSchedulerExecuteWorkflow_MessageHandler(t *testing.T) {
	h := NewSchedulerWorkflowHandler()
	app := newMockApp()

	handler := &mockMsgHandler{}
	app.services["my-handler"] = handler

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	_, err := h.ExecuteWorkflow(ctx, "scheduler", "my-handler", map[string]any{
		"key": "value",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handler.lastMsg == nil {
		t.Error("expected message handler to be called")
	}
}

func TestSchedulerExecuteWorkflow_ColonSeparatedAction(t *testing.T) {
	h := NewSchedulerWorkflowHandler()
	app := newMockApp()

	sched := &mockScheduler{}
	app.services["my-sched"] = sched

	executed := false
	job := &mockJob{executeFn: func(ctx context.Context) error {
		executed = true
		return nil
	}}
	app.services["my-job"] = job

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	_, err := h.ExecuteWorkflow(ctx, "scheduler", "my-sched:my-job", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !executed {
		t.Error("expected job to be executed")
	}
}

func TestSchedulerExecuteWorkflow_SchedulerFromData(t *testing.T) {
	h := NewSchedulerWorkflowHandler()
	app := newMockApp()

	sched := &mockScheduler{}
	app.services["my-sched"] = sched

	job := &mockJob{}
	app.services["my-job"] = job

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	_, err := h.ExecuteWorkflow(ctx, "scheduler", "my-job", map[string]any{
		"scheduler": "my-sched",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSchedulerExecuteWorkflow_NoAppContext(t *testing.T) {
	h := NewSchedulerWorkflowHandler()
	ctx := context.Background()

	_, err := h.ExecuteWorkflow(ctx, "scheduler", "action", map[string]any{})
	if err == nil || err.Error() != "application context not available" {
		t.Fatalf("expected app context error, got: %v", err)
	}
}

func TestSchedulerExecuteWorkflow_EmptyJobName(t *testing.T) {
	h := NewSchedulerWorkflowHandler()
	app := newMockApp()
	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	// Use ":" to get empty job name after split
	_, err := h.ExecuteWorkflow(ctx, "scheduler", "sched:", map[string]any{})
	if err == nil || err.Error() != "job name not specified" {
		t.Fatalf("expected job name error, got: %v", err)
	}
}

func TestSchedulerExecuteWorkflow_SchedulerNotFound(t *testing.T) {
	h := NewSchedulerWorkflowHandler()
	app := newMockApp()

	job := &mockJob{}
	app.services["my-job"] = job

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	_, err := h.ExecuteWorkflow(ctx, "scheduler", "missing-sched:my-job", map[string]any{})
	if err == nil {
		t.Fatal("expected scheduler not found error")
	}
}

func TestSchedulerExecuteWorkflow_JobNotFound(t *testing.T) {
	h := NewSchedulerWorkflowHandler()
	app := newMockApp()

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	_, err := h.ExecuteWorkflow(ctx, "scheduler", "missing-job", map[string]any{})
	if err == nil {
		t.Fatal("expected job not found error")
	}
}

func TestSchedulerExecuteWorkflow_HelperFallback(t *testing.T) {
	h := NewSchedulerWorkflowHandler()
	app := newMockApp()

	// Register something that's neither a Job nor MessageHandler
	app.services["weird-svc"] = "just-a-string"

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	result, err := h.ExecuteWorkflow(ctx, "scheduler", "weird-svc", map[string]any{})
	if err != nil {
		t.Fatalf("expected no error (helper fallback), got: %v", err)
	}
	if result["handlerType"] != "helper" {
		t.Errorf("expected handlerType=helper, got %v", result["handlerType"])
	}
}
