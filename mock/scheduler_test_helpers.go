package mock

import (
	"context"
)

// TestScheduler is a mock implementation for testing schedulers
type TestScheduler struct {
	SchedulerName     string // Changed from Name to SchedulerName to avoid conflict
	CronExpression    string
	JobsAdded         []string
	JobFunctions      []any
	Started           bool
	Stopped           bool
	StartError        error
	StopError         error
	AddJobError       error
	InitializerCalled bool
}

// NewTestScheduler creates a new test scheduler
func NewTestScheduler(name, cronExpression string) *TestScheduler {
	return &TestScheduler{
		SchedulerName:  name, // Use SchedulerName instead of Name
		CronExpression: cronExpression,
		JobsAdded:      make([]string, 0),
		JobFunctions:   make([]any, 0),
	}
}

// Init initializes the scheduler
func (s *TestScheduler) Init(registry map[string]any) error {
	s.InitializerCalled = true
	return nil
}

// Name returns the name of the scheduler
func (s *TestScheduler) Name() string {
	return s.SchedulerName // Return SchedulerName instead of Name
}

// AddJob adds a job to the scheduler
func (s *TestScheduler) AddJob(spec string, job any) error {
	if s.AddJobError != nil {
		return s.AddJobError
	}
	s.JobsAdded = append(s.JobsAdded, spec)
	s.JobFunctions = append(s.JobFunctions, job)
	return nil
}

// Start starts the scheduler
func (s *TestScheduler) Start(ctx context.Context) error {
	if s.StartError != nil {
		return s.StartError
	}
	s.Started = true
	return nil
}

// Stop stops the scheduler
func (s *TestScheduler) Stop(ctx context.Context) error {
	if s.StopError != nil {
		return s.StopError
	}
	s.Stopped = true
	return nil
}
