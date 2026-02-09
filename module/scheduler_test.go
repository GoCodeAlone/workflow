package module

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
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
	s.Schedule(job)
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
		cron     string
		expected time.Duration
	}{
		{"* * * * *", time.Minute},
		{"0 * * * *", time.Hour},
		{"0 0 * * *", 24 * time.Hour},
		{"custom", time.Minute}, // default
	}

	for _, tc := range tests {
		t.Run(tc.cron, func(t *testing.T) {
			s := NewCronScheduler("test", tc.cron)
			ctx, cancel := context.WithCancel(context.Background())

			s.Start(ctx)
			// Verify the ticker was created
			if s.ticker == nil {
				t.Error("expected ticker to be created")
			}
			cancel()
			// Give the goroutine time to handle context cancellation
			time.Sleep(10 * time.Millisecond)
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
	s.Schedule(job)

	// Manually trigger jobs instead of waiting for the ticker
	ctx := context.Background()
	for _, j := range s.jobs {
		j.Execute(ctx)
	}

	if executed.Load() != 1 {
		t.Errorf("expected job to be executed once, got %d", executed.Load())
	}
}
