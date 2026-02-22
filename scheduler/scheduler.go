package scheduler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// JobStatus represents the status of a scheduled job.
type JobStatus string

const (
	JobStatusActive  JobStatus = "active"
	JobStatusPaused  JobStatus = "paused"
	JobStatusDeleted JobStatus = "deleted"
)

// ExecutionStatus represents the result of a job execution.
type ExecutionStatus string

const (
	ExecStatusSuccess ExecutionStatus = "success"
	ExecStatusFailed  ExecutionStatus = "failed"
	ExecStatusSkipped ExecutionStatus = "skipped"
)

// ScheduledJob represents a job that runs on a cron schedule.
type ScheduledJob struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	CronExpr     string         `json:"cronExpr"`
	WorkflowType string         `json:"workflowType"`
	Action       string         `json:"action"`
	Params       map[string]any `json:"params,omitempty"`
	Status       JobStatus      `json:"status"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
	LastRunAt    *time.Time     `json:"lastRunAt,omitempty"`
	NextRunAt    *time.Time     `json:"nextRunAt,omitempty"`
}

// ExecutionRecord records the result of a single job execution.
type ExecutionRecord struct {
	ID        string          `json:"id"`
	JobID     string          `json:"jobId"`
	Status    ExecutionStatus `json:"status"`
	StartedAt time.Time       `json:"startedAt"`
	Duration  time.Duration   `json:"duration"`
	Error     string          `json:"error,omitempty"`
}

// WorkflowTrigger is the function signature for triggering workflows.
type WorkflowTrigger func(ctx context.Context, workflowType, action string, data map[string]any) error

// CronScheduler manages scheduled workflow executions.
type CronScheduler struct {
	mu        sync.RWMutex
	jobs      map[string]*ScheduledJob
	history   map[string][]*ExecutionRecord // jobID -> executions
	trigger   WorkflowTrigger
	stopChs   map[string]chan struct{} // per-job stop channels
	nextRunFn func(cronExpr string, from time.Time) (time.Time, error)
}

// NewCronScheduler creates a new CronScheduler.
func NewCronScheduler(trigger WorkflowTrigger) *CronScheduler {
	return &CronScheduler{
		jobs:      make(map[string]*ScheduledJob),
		history:   make(map[string][]*ExecutionRecord),
		trigger:   trigger,
		stopChs:   make(map[string]chan struct{}),
		nextRunFn: defaultNextRun,
	}
}

// SetNextRunFunc allows overriding the next-run calculation (useful for testing).
func (s *CronScheduler) SetNextRunFunc(fn func(string, time.Time) (time.Time, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextRunFn = fn
}

// Create adds a new scheduled job. It validates the cron expression and returns an error if invalid.
func (s *CronScheduler) Create(job *ScheduledJob) error {
	if job.Name == "" {
		return fmt.Errorf("job name is required")
	}
	if job.CronExpr == "" {
		return fmt.Errorf("cron expression is required")
	}
	if job.WorkflowType == "" {
		return fmt.Errorf("workflow type is required")
	}
	if job.Action == "" {
		return fmt.Errorf("action is required")
	}
	if err := ValidateCron(job.CronExpr); err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}

	id, err := generateID("sj")
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	job.ID = id
	job.Status = JobStatusActive
	job.CreatedAt = now
	job.UpdatedAt = now

	next, err := s.nextRunFn(job.CronExpr, now)
	if err == nil {
		job.NextRunAt = &next
	}

	s.jobs[id] = job
	return nil
}

// Get returns a job by ID.
func (s *CronScheduler) Get(id string) (*ScheduledJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	return j, ok
}

// List returns all non-deleted jobs sorted by creation time.
func (s *CronScheduler) List() []*ScheduledJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*ScheduledJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		if j.Status != JobStatusDeleted {
			result = append(result, j)
		}
	}
	sort.Slice(result, func(i, k int) bool {
		return result[i].CreatedAt.Before(result[k].CreatedAt)
	})
	return result
}

// Update modifies a scheduled job.
func (s *CronScheduler) Update(id string, name, cronExpr, workflowType, action string, params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok || job.Status == JobStatusDeleted {
		return fmt.Errorf("job %q not found", id)
	}

	if cronExpr != "" && cronExpr != job.CronExpr {
		if err := ValidateCron(cronExpr); err != nil {
			return fmt.Errorf("invalid cron expression: %w", err)
		}
		job.CronExpr = cronExpr
		next, err := s.nextRunFn(cronExpr, time.Now())
		if err == nil {
			job.NextRunAt = &next
		}
	}
	if name != "" {
		job.Name = name
	}
	if workflowType != "" {
		job.WorkflowType = workflowType
	}
	if action != "" {
		job.Action = action
	}
	if params != nil {
		job.Params = params
	}
	job.UpdatedAt = time.Now()
	return nil
}

// Delete soft-deletes a job and stops its execution loop.
func (s *CronScheduler) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok {
		return fmt.Errorf("job %q not found", id)
	}

	job.Status = JobStatusDeleted
	job.UpdatedAt = time.Now()

	if ch, ok := s.stopChs[id]; ok {
		close(ch)
		delete(s.stopChs, id)
	}
	return nil
}

// Pause pauses a job.
func (s *CronScheduler) Pause(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok || job.Status == JobStatusDeleted {
		return fmt.Errorf("job %q not found", id)
	}
	if job.Status == JobStatusPaused {
		return nil
	}

	job.Status = JobStatusPaused
	job.UpdatedAt = time.Now()

	if ch, ok := s.stopChs[id]; ok {
		close(ch)
		delete(s.stopChs, id)
	}
	return nil
}

// Resume resumes a paused job.
func (s *CronScheduler) Resume(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok || job.Status == JobStatusDeleted {
		return fmt.Errorf("job %q not found", id)
	}
	if job.Status == JobStatusActive {
		return nil
	}

	job.Status = JobStatusActive
	job.UpdatedAt = time.Now()

	next, err := s.nextRunFn(job.CronExpr, time.Now())
	if err == nil {
		job.NextRunAt = &next
	}
	return nil
}

// History returns execution records for a job, newest first.
func (s *CronScheduler) History(jobID string) []*ExecutionRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	recs := s.history[jobID]
	result := make([]*ExecutionRecord, len(recs))
	copy(result, recs)
	sort.Slice(result, func(i, j int) bool {
		return result[i].StartedAt.After(result[j].StartedAt)
	})
	return result
}

// NextRuns returns up to n upcoming execution times for a given cron expression.
func (s *CronScheduler) NextRuns(cronExpr string, n int) ([]time.Time, error) {
	if err := ValidateCron(cronExpr); err != nil {
		return nil, err
	}
	s.mu.RLock()
	fn := s.nextRunFn
	s.mu.RUnlock()

	times := make([]time.Time, 0, n)
	from := time.Now()
	for i := 0; i < n; i++ {
		next, err := fn(cronExpr, from)
		if err != nil {
			return times, err
		}
		times = append(times, next)
		from = next
	}
	return times, nil
}

// ExecuteNow triggers immediate execution of a job (bypasses schedule).
func (s *CronScheduler) ExecuteNow(ctx context.Context, id string) (*ExecutionRecord, error) {
	s.mu.RLock()
	job, ok := s.jobs[id]
	if !ok || job.Status == JobStatusDeleted {
		s.mu.RUnlock()
		return nil, fmt.Errorf("job %q not found", id)
	}
	s.mu.RUnlock()

	return s.executeJob(ctx, job), nil
}

func (s *CronScheduler) executeJob(ctx context.Context, job *ScheduledJob) *ExecutionRecord {
	start := time.Now()

	data := make(map[string]any)
	data["trigger_time"] = start.Format(time.RFC3339)
	data["job_id"] = job.ID
	data["job_name"] = job.Name
	for k, v := range job.Params {
		data[k] = v
	}

	execErr := s.trigger(ctx, job.WorkflowType, job.Action, data)

	rec := &ExecutionRecord{
		ID:        mustGenerateID("exec"),
		JobID:     job.ID,
		StartedAt: start,
		Duration:  time.Since(start),
	}
	if execErr != nil {
		rec.Status = ExecStatusFailed
		rec.Error = execErr.Error()
	} else {
		rec.Status = ExecStatusSuccess
	}

	s.mu.Lock()
	now := time.Now()
	job.LastRunAt = &now
	next, err := s.nextRunFn(job.CronExpr, now)
	if err == nil {
		job.NextRunAt = &next
	}
	s.history[job.ID] = append(s.history[job.ID], rec)
	s.mu.Unlock()

	return rec
}

func generateID(prefix string) (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(b), nil
}

func mustGenerateID(prefix string) string {
	id, err := generateID(prefix)
	if err != nil {
		return prefix + "-fallback"
	}
	return id
}

// NextRun computes the next run time for a cron expression from a given point in time.
func NextRun(cronExpr string, from time.Time) (time.Time, error) {
	return defaultNextRun(cronExpr, from)
}

// --- Simple cron parser (supports standard 5-field cron) ---

// ValidateCron validates a standard 5-field cron expression.
func ValidateCron(expr string) error {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return fmt.Errorf("expected 5 fields, got %d", len(fields))
	}

	limits := []struct{ min, max int }{
		{0, 59}, // minute
		{0, 23}, // hour
		{1, 31}, // day of month
		{1, 12}, // month
		{0, 7},  // day of week (0 and 7 are Sunday)
	}

	for i, field := range fields {
		if err := validateCronField(field, limits[i].min, limits[i].max); err != nil {
			return fmt.Errorf("field %d (%q): %w", i+1, field, err)
		}
	}
	return nil
}

func validateCronField(field string, min, max int) error {
	if field == "*" {
		return nil
	}
	// Handle */N step values
	if strings.HasPrefix(field, "*/") {
		stepStr := field[2:]
		step := 0
		for _, c := range stepStr {
			if c < '0' || c > '9' {
				return fmt.Errorf("invalid step value %q", stepStr)
			}
			step = step*10 + int(c-'0')
		}
		if step <= 0 || step > max {
			return fmt.Errorf("step %d out of range [1-%d]", step, max)
		}
		return nil
	}
	// Handle comma-separated values
	parts := strings.Split(field, ",")
	for _, part := range parts {
		// Handle ranges like 1-5
		if strings.Contains(part, "-") {
			rangeParts := strings.SplitN(part, "-", 2)
			lo, err := parseCronInt(rangeParts[0])
			if err != nil {
				return err
			}
			hi, err := parseCronInt(rangeParts[1])
			if err != nil {
				return err
			}
			if lo < min || hi > max || lo > hi {
				return fmt.Errorf("range %d-%d out of bounds [%d-%d]", lo, hi, min, max)
			}
			continue
		}
		v, err := parseCronInt(part)
		if err != nil {
			return err
		}
		if v < min || v > max {
			return fmt.Errorf("value %d out of range [%d-%d]", v, min, max)
		}
	}
	return nil
}

func parseCronInt(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty value")
	}
	v := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid number %q", s)
		}
		v = v*10 + int(c-'0')
	}
	return v, nil
}

// defaultNextRun computes the next run time for a simple cron expression.
// It supports: every-minute, hourly, daily, and step expressions for minute field.
func defaultNextRun(cronExpr string, from time.Time) (time.Time, error) {
	fields := strings.Fields(cronExpr)
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("invalid cron expression")
	}

	// Simple cases
	switch cronExpr {
	case "* * * * *":
		return from.Add(time.Minute).Truncate(time.Minute), nil
	case "0 * * * *":
		next := from.Truncate(time.Hour).Add(time.Hour)
		return next, nil
	case "0 0 * * *":
		next := time.Date(from.Year(), from.Month(), from.Day()+1, 0, 0, 0, 0, from.Location())
		return next, nil
	}

	// Handle */N minute patterns
	if strings.HasPrefix(fields[0], "*/") && fields[1] == "*" && fields[2] == "*" && fields[3] == "*" && fields[4] == "*" {
		step, _ := parseCronInt(fields[0][2:])
		if step > 0 {
			next := from.Truncate(time.Minute).Add(time.Duration(step) * time.Minute)
			return next, nil
		}
	}

	// Fallback: next minute
	return from.Add(time.Minute).Truncate(time.Minute), nil
}
