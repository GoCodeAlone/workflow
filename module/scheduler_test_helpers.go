package module

import (
	"context"

	"github.com/CrisisTextLine/modular"
)

// ScheduledJobInfo captures information about a scheduled job
type ScheduledJobInfo struct {
	job            Job
	cronExpression string
}

// MockScheduler is a mock implementation of the Scheduler interface
type MockScheduler struct {
	scheduledJobs []ScheduledJobInfo
}

func NewMockScheduler() *MockScheduler {
	return &MockScheduler{
		scheduledJobs: make([]ScheduledJobInfo, 0),
	}
}

func (s *MockScheduler) Name() string {
	return "mock-scheduler"
}

func (s *MockScheduler) Schedule(job Job) error {
	// For testing purposes, we'll store the job with a default cron expression
	s.scheduledJobs = append(s.scheduledJobs, ScheduledJobInfo{
		job:            job,
		cronExpression: "* * * * *", // Default to every minute
	})
	return nil
}

// For our tests, we'll add this method to set the cron expression for a scheduled job
func (s *MockScheduler) SetCronExpression(index int, cronExpression string) {
	if index < len(s.scheduledJobs) {
		s.scheduledJobs[index].cronExpression = cronExpression
	}
}

func (s *MockScheduler) Start(ctx context.Context) error {
	return nil
}

func (s *MockScheduler) Stop(ctx context.Context) error {
	return nil
}

func (s *MockScheduler) Init(registry modular.ServiceRegistry) error {
	return nil
}
