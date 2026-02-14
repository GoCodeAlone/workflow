package mock

import (
	"context"
)

// CronScheduler is a mock implementation of a cron scheduler
type CronScheduler struct {
	Jobs               []string
	ExecuteImmediately bool
}

// NewCronScheduler creates a new instance of the mock scheduler
func NewCronScheduler() *CronScheduler {
	return &CronScheduler{
		Jobs: make([]string, 0),
	}
}

// AddJob adds a job to the scheduler
func (c *CronScheduler) AddJob(spec string, job any) error {
	c.Jobs = append(c.Jobs, spec)
	return nil
}

// Start starts the scheduler
func (c *CronScheduler) Start(ctx context.Context) error {
	return nil
}

// Stop stops the scheduler
func (c *CronScheduler) Stop(ctx context.Context) error {
	return nil
}

// SetExecuteImmediately sets whether jobs should execute immediately when added
func (c *CronScheduler) SetExecuteImmediately(immediate bool) {
	c.ExecuteImmediately = immediate
}
