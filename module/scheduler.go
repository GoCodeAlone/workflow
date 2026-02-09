package module

import (
	"context"
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
)

// Job represents a scheduled job
type Job interface {
	Execute(ctx context.Context) error
}

// Scheduler represents a job scheduler
type Scheduler interface {
	Schedule(job Job) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// CronScheduler implements a cron-based scheduler
type CronScheduler struct {
	name           string
	cronExpression string
	jobs           []Job
	running        bool
	ticker         *time.Ticker
	stopCh         chan struct{}
}

// NewCronScheduler creates a new cron scheduler
func NewCronScheduler(name string, cronExpression string) *CronScheduler {
	return &CronScheduler{
		name:           name,
		cronExpression: cronExpression,
		jobs:           make([]Job, 0),
		stopCh:         make(chan struct{}),
	}
}

// Name returns the module name
func (s *CronScheduler) Name() string {
	return s.name
}

// Init initializes the scheduler
func (s *CronScheduler) Init(app modular.Application) error {
	// Register ourselves in the service registry
	return app.RegisterService(s.name, s)
}

// Start starts the scheduler
func (s *CronScheduler) Start(ctx context.Context) error {
	if s.running {
		return nil
	}

	// For testing purposes, we'll use a simple ticker with fixed intervals
	// In a real implementation, this would parse the cron expression and schedule accordingly
	interval := time.Minute
	switch s.cronExpression {
	case "* * * * *": // every minute
		interval = time.Minute
	case "0 * * * *": // every hour
		interval = time.Hour
	case "0 0 * * *": // every day
		interval = 24 * time.Hour
	}

	s.ticker = time.NewTicker(interval)
	s.running = true

	go func() {
		for {
			select {
			case <-s.ticker.C:
				// Run all scheduled jobs
				for _, job := range s.jobs {
					go func(j Job) {
						if err := j.Execute(ctx); err != nil {
							fmt.Printf("Job execution failed: %v\n", err)
						}
					}(job)
				}
			case <-s.stopCh:
				return
			case <-ctx.Done():
				s.ticker.Stop()
				s.running = false
				return
			}
		}
	}()

	return nil
}

// Stop stops the scheduler
func (s *CronScheduler) Stop(ctx context.Context) error {
	if !s.running {
		return nil
	}

	s.ticker.Stop()
	s.stopCh <- struct{}{}
	s.running = false
	return nil
}

// Schedule adds a job to the scheduler
func (s *CronScheduler) Schedule(job Job) error {
	s.jobs = append(s.jobs, job)
	return nil
}

// FunctionJob is a Job implementation that executes a function
type FunctionJob struct {
	fn func(context.Context) error
}

// NewFunctionJob creates a new job from a function
func NewFunctionJob(fn func(context.Context) error) *FunctionJob {
	return &FunctionJob{
		fn: fn,
	}
}

// Execute runs the job function
func (j *FunctionJob) Execute(ctx context.Context) error {
	return j.fn(ctx)
}

// MessageHandlerJobAdapter adapts a MessageHandler to the Job interface
type MessageHandlerJobAdapter struct {
	handler MessageHandler
}

// NewMessageHandlerJobAdapter creates a new adapter from MessageHandler to Job
func NewMessageHandlerJobAdapter(handler MessageHandler) *MessageHandlerJobAdapter {
	return &MessageHandlerJobAdapter{
		handler: handler,
	}
}

// Execute runs the job by calling HandleMessage with an empty message
func (a *MessageHandlerJobAdapter) Execute(ctx context.Context) error {
	// Create an empty JSON message payload
	payload := []byte("{}")
	return a.handler.HandleMessage(payload)
}
