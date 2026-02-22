package module

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	wscheduler "github.com/GoCodeAlone/workflow/scheduler"
)

func TestNewCronScheduler(t *testing.T) {
	s := NewCronScheduler("test-scheduler", "* * * * *")
	if s.Name() != "test-scheduler" {
		t.Errorf("expected name 'test-scheduler', got '%s'", s.Name())
	}
	if s.cronExpression != "* * * * *" {
		t.Errorf("expected cron '* * * * *', got '%s'", s.cronExpression)
	}
	if s.running {
		t.Error("expected running=false initially")
	}
}

func TestCronScheduler_Schedule(t *testing.T) {
	s := NewCronScheduler("test", "* * * * *")

	job := NewFunctionJob(func(ctx context.Context) error {
		return nil
	})

	err := s.Schedule(job)
	if err != nil {
		t.Fatalf("Schedule failed: %v", err)
	}
	if len(s.jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(s.jobs))
	}

	// Schedule another
	_ = s.Schedule(job)
	if len(s.jobs) != 2 {
		t.Errorf("expected 2 jobs, got %d", len(s.jobs))
	}
}

func TestCronScheduler_Init(t *testing.T) {
	app := CreateIsolatedApp(t)
	s := NewCronScheduler("test-scheduler", "* * * * *")

	err := s.Init(app)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
}

func TestCronScheduler_StartStop(t *testing.T) {
	s := NewCronScheduler("test", "* * * * *")

	ctx := t.Context()

	err := s.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if !s.running {
		t.Error("expected running=true after Start")
	}

	// Start again should be a no-op
	err = s.Start(ctx)
	if err != nil {
		t.Fatalf("second Start failed: %v", err)
	}

	err = s.Stop(ctx)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if s.running {
		t.Error("expected running=false after Stop")
	}
}

func TestCronScheduler_StopNotRunning(t *testing.T) {
	s := NewCronScheduler("test", "* * * * *")

	err := s.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop when not running should not fail: %v", err)
	}
}

func TestCronScheduler_CronExpressions(t *testing.T) {
	tests := []struct {
		cron string
	}{
		{"* * * * *"},
		{"*/5 * * * *"},
		{"0 * * * *"},
		{"0 0 * * *"},
		{"0 9 * * 1-5"},
	}

	for _, tc := range tests {
		t.Run(tc.cron, func(t *testing.T) {
			s := NewCronScheduler("test", tc.cron)
			ctx, cancel := context.WithCancel(context.Background())

			err := s.Start(ctx)
			if err != nil {
				t.Errorf("Start failed for %q: %v", tc.cron, err)
			}
			if !s.running {
				t.Error("expected running=true after Start")
			}
			cancel()
			// Give the goroutine time to handle context cancellation
			time.Sleep(10 * time.Millisecond)
		})
	}
}

func TestCronScheduler_InvalidExpression(t *testing.T) {
	s := NewCronScheduler("test", "not-a-cron")
	err := s.Start(context.Background())
	if err == nil {
		t.Error("expected error for invalid cron expression")
	}
	if s.running {
		t.Error("expected running=false for invalid expression")
	}
}

func TestCronScheduler_NextRunTimes(t *testing.T) {
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		cron     string
		expected time.Time
	}{
		{"* * * * *", from.Add(time.Minute).Truncate(time.Minute)},
		{"*/5 * * * *", from.Add(5 * time.Minute).Truncate(time.Minute)},
	}

	for _, tc := range tests {
		t.Run(tc.cron, func(t *testing.T) {
			next, err := wscheduler.NextRun(tc.cron, from)
			if err != nil {
				t.Fatalf("NextRun failed: %v", err)
			}
			if !next.Equal(tc.expected) {
				t.Errorf("expected next run %v, got %v", tc.expected, next)
			}
		})
	}
}

func TestFunctionJob_Execute(t *testing.T) {
	var called bool
	job := NewFunctionJob(func(ctx context.Context) error {
		called = true
		return nil
	})

	err := job.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !called {
		t.Error("expected function to be called")
	}
}

func TestFunctionJob_Execute_Error(t *testing.T) {
	job := NewFunctionJob(func(ctx context.Context) error {
		return fmt.Errorf("job error")
	})

	err := job.Execute(context.Background())
	if err == nil {
		t.Error("expected error from job")
	}
}

func TestMessageHandlerJobAdapter(t *testing.T) {
	var received bool
	handler := &mockMessageHandler{
		handleFunc: func(msg []byte) error {
			received = true
			if string(msg) != "{}" {
				t.Errorf("expected empty JSON, got '%s'", string(msg))
			}
			return nil
		},
	}

	adapter := NewMessageHandlerJobAdapter(handler)

	err := adapter.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !received {
		t.Error("expected handler to be called")
	}
}

// mockMessageHandler implements MessageHandler for testing
type mockMessageHandler struct {
	handleFunc func(msg []byte) error
}

func (m *mockMessageHandler) HandleMessage(message []byte) error {
	return m.handleFunc(message)
}

func TestCronScheduler_ExecutesJobs(t *testing.T) {
	s := NewCronScheduler("test", "* * * * *")

	var executed atomic.Int32
	job := NewFunctionJob(func(ctx context.Context) error {
		executed.Add(1)
		return nil
	})
	_ = s.Schedule(job)

	// Manually trigger jobs instead of waiting for the ticker
	ctx := context.Background()
	for _, j := range s.jobs {
		_ = j.Execute(ctx)
	}

	if executed.Load() != 1 {
		t.Errorf("expected job to be executed once, got %d", executed.Load())
	}
}
