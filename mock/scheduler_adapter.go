package mock

// CronSchedulerInterface defines the interface that matches the module.CronScheduler
// allowing us to avoid the direct import of the module package
type CronSchedulerInterface interface {
	Name() string
	Init() error
	Start() error
	Stop() error
	Schedule(jobName, cronSchedule string, jobFunc func())
}

// MockCronScheduler is our implementation that can be used in tests
type MockCronScheduler struct {
	name string
	jobs map[string]struct {
		schedule string
		jobFunc  func()
	}
}

// NewMockCronScheduler creates a new mock scheduler implementation
func NewMockCronScheduler(name string) *MockCronScheduler {
	return &MockCronScheduler{
		name: name,
		jobs: make(map[string]struct {
			schedule string
			jobFunc  func()
		}),
	}
}

// Name returns the scheduler name
func (m *MockCronScheduler) Name() string {
	return m.name
}

// Init initializes the scheduler
func (m *MockCronScheduler) Init() error {
	return nil
}

// Start starts the scheduler
func (m *MockCronScheduler) Start() error {
	return nil
}

// Stop stops the scheduler
func (m *MockCronScheduler) Stop() error {
	return nil
}

// Schedule adds a job to the scheduler
func (m *MockCronScheduler) Schedule(jobName, cronSchedule string, jobFunc func()) {
	m.jobs[jobName] = struct {
		schedule string
		jobFunc  func()
	}{
		schedule: cronSchedule,
		jobFunc:  jobFunc,
	}
}

// GetScheduler returns a mock scheduler instance that implements the interface
func GetScheduler() CronSchedulerInterface {
	return NewMockCronScheduler("mock-scheduler")
}

// RunJob executes a scheduled job by name (for testing)
func (m *MockCronScheduler) RunJob(jobName string) bool {
	if job, exists := m.jobs[jobName]; exists {
		job.jobFunc()
		return true
	}
	return false
}
