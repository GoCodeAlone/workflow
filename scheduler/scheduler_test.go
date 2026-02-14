package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func mockTrigger(err error) WorkflowTrigger {
	return func(ctx context.Context, wfType, action string, data map[string]any) error {
		return err
	}
}

func TestValidateCron(t *testing.T) {
	valid := []string{
		"* * * * *",
		"0 * * * *",
		"0 0 * * *",
		"*/5 * * * *",
		"0 0 1 1 *",
		"30 4 1-15 * 1,3,5",
	}
	for _, expr := range valid {
		if err := ValidateCron(expr); err != nil {
			t.Errorf("expected %q to be valid, got: %v", expr, err)
		}
	}

	invalid := []string{
		"",
		"* * *",
		"60 * * * *",
		"* 25 * * *",
		"* * 32 * *",
		"* * * 13 *",
		"abc * * * *",
	}
	for _, expr := range invalid {
		if err := ValidateCron(expr); err == nil {
			t.Errorf("expected %q to be invalid", expr)
		}
	}
}

func TestCronScheduler_Create(t *testing.T) {
	s := NewCronScheduler(mockTrigger(nil))

	job := &ScheduledJob{
		Name:         "test-job",
		CronExpr:     "*/5 * * * *",
		WorkflowType: "http",
		Action:       "process",
	}

	if err := s.Create(job); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if job.ID == "" {
		t.Error("expected ID to be set")
	}
	if job.Status != JobStatusActive {
		t.Errorf("expected active, got %s", job.Status)
	}
	if job.NextRunAt == nil {
		t.Error("expected NextRunAt to be set")
	}
}

func TestCronScheduler_CreateValidation(t *testing.T) {
	s := NewCronScheduler(mockTrigger(nil))

	tests := []struct {
		name string
		job  ScheduledJob
	}{
		{"missing name", ScheduledJob{CronExpr: "* * * * *", WorkflowType: "http", Action: "a"}},
		{"missing cron", ScheduledJob{Name: "n", WorkflowType: "http", Action: "a"}},
		{"missing workflow", ScheduledJob{Name: "n", CronExpr: "* * * * *", Action: "a"}},
		{"missing action", ScheduledJob{Name: "n", CronExpr: "* * * * *", WorkflowType: "http"}},
		{"invalid cron", ScheduledJob{Name: "n", CronExpr: "bad", WorkflowType: "http", Action: "a"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.Create(&tc.job); err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestCronScheduler_CRUD(t *testing.T) {
	s := NewCronScheduler(mockTrigger(nil))

	job := &ScheduledJob{
		Name:         "my-job",
		CronExpr:     "0 * * * *",
		WorkflowType: "http",
		Action:       "run",
		Params:       map[string]any{"key": "val"},
	}
	if err := s.Create(job); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Get
	got, ok := s.Get(job.ID)
	if !ok {
		t.Fatal("get: not found")
	}
	if got.Name != "my-job" {
		t.Errorf("expected my-job, got %s", got.Name)
	}

	// List
	jobs := s.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	// Update
	if err := s.Update(job.ID, "updated-job", "*/10 * * * *", "", "", nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = s.Get(job.ID)
	if got.Name != "updated-job" {
		t.Errorf("expected updated-job, got %s", got.Name)
	}
	if got.CronExpr != "*/10 * * * *" {
		t.Errorf("expected */10, got %s", got.CronExpr)
	}

	// Delete
	if err := s.Delete(job.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	jobs = s.List()
	if len(jobs) != 0 {
		t.Errorf("expected 0 after delete, got %d", len(jobs))
	}
}

func TestCronScheduler_PauseResume(t *testing.T) {
	s := NewCronScheduler(mockTrigger(nil))

	job := &ScheduledJob{
		Name:         "pausable",
		CronExpr:     "* * * * *",
		WorkflowType: "http",
		Action:       "run",
	}
	_ = s.Create(job)

	if err := s.Pause(job.ID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	got, _ := s.Get(job.ID)
	if got.Status != JobStatusPaused {
		t.Errorf("expected paused, got %s", got.Status)
	}

	// Pause again is idempotent
	if err := s.Pause(job.ID); err != nil {
		t.Fatalf("second pause: %v", err)
	}

	if err := s.Resume(job.ID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	got, _ = s.Get(job.ID)
	if got.Status != JobStatusActive {
		t.Errorf("expected active, got %s", got.Status)
	}

	// Resume again is idempotent
	if err := s.Resume(job.ID); err != nil {
		t.Fatalf("second resume: %v", err)
	}
}

func TestCronScheduler_ExecuteNow(t *testing.T) {
	var called bool
	trigger := func(ctx context.Context, wfType, action string, data map[string]any) error {
		called = true
		if wfType != "http" || action != "run" {
			return fmt.Errorf("unexpected workflow=%s action=%s", wfType, action)
		}
		return nil
	}

	s := NewCronScheduler(trigger)
	job := &ScheduledJob{
		Name:         "exec-test",
		CronExpr:     "0 0 * * *",
		WorkflowType: "http",
		Action:       "run",
	}
	_ = s.Create(job)

	rec, err := s.ExecuteNow(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !called {
		t.Error("trigger not called")
	}
	if rec.Status != ExecStatusSuccess {
		t.Errorf("expected success, got %s", rec.Status)
	}
	if rec.Duration <= 0 {
		t.Error("expected positive duration")
	}

	// Check history
	history := s.History(job.ID)
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}
}

func TestCronScheduler_ExecuteNow_Failure(t *testing.T) {
	s := NewCronScheduler(mockTrigger(fmt.Errorf("workflow error")))

	job := &ScheduledJob{
		Name:         "fail-exec",
		CronExpr:     "* * * * *",
		WorkflowType: "http",
		Action:       "run",
	}
	_ = s.Create(job)

	rec, err := s.ExecuteNow(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if rec.Status != ExecStatusFailed {
		t.Errorf("expected failed, got %s", rec.Status)
	}
	if rec.Error == "" {
		t.Error("expected error message")
	}
}

func TestCronScheduler_NextRuns(t *testing.T) {
	s := NewCronScheduler(mockTrigger(nil))

	times, err := s.NextRuns("*/5 * * * *", 3)
	if err != nil {
		t.Fatalf("next runs: %v", err)
	}
	if len(times) != 3 {
		t.Fatalf("expected 3 times, got %d", len(times))
	}
	for i := 1; i < len(times); i++ {
		if !times[i].After(times[i-1]) {
			t.Errorf("expected times[%d] after times[%d]", i, i-1)
		}
	}
}

func TestCronScheduler_NextRunsInvalidCron(t *testing.T) {
	s := NewCronScheduler(mockTrigger(nil))
	_, err := s.NextRuns("bad cron", 3)
	if err == nil {
		t.Error("expected error for invalid cron")
	}
}

func TestCronScheduler_DeleteNotFound(t *testing.T) {
	s := NewCronScheduler(mockTrigger(nil))
	if err := s.Delete("nonexistent"); err == nil {
		t.Error("expected error")
	}
}

func TestCronScheduler_ExecuteNotFound(t *testing.T) {
	s := NewCronScheduler(mockTrigger(nil))
	_, err := s.ExecuteNow(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error")
	}
}

func TestCronScheduler_UpdateInvalidCron(t *testing.T) {
	s := NewCronScheduler(mockTrigger(nil))
	job := &ScheduledJob{Name: "x", CronExpr: "* * * * *", WorkflowType: "http", Action: "run"}
	_ = s.Create(job)

	if err := s.Update(job.ID, "", "bad", "", "", nil); err == nil {
		t.Error("expected error for invalid cron update")
	}
}

// --- HTTP handler tests ---

func TestHandler_ListJobs(t *testing.T) {
	s := NewCronScheduler(mockTrigger(nil))
	_ = s.Create(&ScheduledJob{Name: "j1", CronExpr: "* * * * *", WorkflowType: "http", Action: "run"})

	h := NewHandler(s)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/schedules", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp["total"].(float64) != 1 {
		t.Errorf("expected 1 total, got %v", resp["total"])
	}
}

func TestHandler_CreateJob(t *testing.T) {
	s := NewCronScheduler(mockTrigger(nil))
	h := NewHandler(s)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"name":"new-job","cronExpr":"*/5 * * * *","workflowType":"http","action":"process"}`
	req := httptest.NewRequest("POST", "/api/schedules", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_CreateJobValidationError(t *testing.T) {
	s := NewCronScheduler(mockTrigger(nil))
	h := NewHandler(s)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"name":"bad","cronExpr":"invalid"}`
	req := httptest.NewRequest("POST", "/api/schedules", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandler_GetJob(t *testing.T) {
	s := NewCronScheduler(mockTrigger(nil))
	job := &ScheduledJob{Name: "get-test", CronExpr: "* * * * *", WorkflowType: "http", Action: "run"}
	_ = s.Create(job)

	h := NewHandler(s)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/schedules/"+job.ID, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandler_DeleteJob(t *testing.T) {
	s := NewCronScheduler(mockTrigger(nil))
	job := &ScheduledJob{Name: "del-test", CronExpr: "* * * * *", WorkflowType: "http", Action: "run"}
	_ = s.Create(job)

	h := NewHandler(s)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("DELETE", "/api/schedules/"+job.ID, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandler_PauseResume(t *testing.T) {
	s := NewCronScheduler(mockTrigger(nil))
	job := &ScheduledJob{Name: "pr-test", CronExpr: "* * * * *", WorkflowType: "http", Action: "run"}
	_ = s.Create(job)

	h := NewHandler(s)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Pause
	req := httptest.NewRequest("POST", "/api/schedules/"+job.ID+"/pause", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pause: expected 200, got %d", rec.Code)
	}

	// Resume
	req = httptest.NewRequest("POST", "/api/schedules/"+job.ID+"/resume", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("resume: expected 200, got %d", rec.Code)
	}
}

func TestHandler_ExecuteJob(t *testing.T) {
	s := NewCronScheduler(mockTrigger(nil))
	job := &ScheduledJob{Name: "exec-test", CronExpr: "* * * * *", WorkflowType: "http", Action: "run"}
	_ = s.Create(job)

	h := NewHandler(s)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/schedules/"+job.ID+"/execute", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_JobHistory(t *testing.T) {
	s := NewCronScheduler(mockTrigger(nil))
	job := &ScheduledJob{Name: "hist-test", CronExpr: "* * * * *", WorkflowType: "http", Action: "run"}
	_ = s.Create(job)
	_, _ = s.ExecuteNow(context.Background(), job.ID)

	h := NewHandler(s)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/schedules/"+job.ID+"/history", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp["total"].(float64) != 1 {
		t.Errorf("expected 1 history entry, got %v", resp["total"])
	}
}

func TestHandler_PreviewNextRuns(t *testing.T) {
	s := NewCronScheduler(mockTrigger(nil))
	h := NewHandler(s)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/schedules/preview?cron=*/5+*+*+*+*&count=3", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	runs := resp["nextRuns"].([]any)
	if len(runs) != 3 {
		t.Errorf("expected 3 runs, got %d", len(runs))
	}
}

func TestHandler_PreviewMissingCron(t *testing.T) {
	s := NewCronScheduler(mockTrigger(nil))
	h := NewHandler(s)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/schedules/preview", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandler_UpdateJob(t *testing.T) {
	s := NewCronScheduler(mockTrigger(nil))
	job := &ScheduledJob{Name: "update-test", CronExpr: "* * * * *", WorkflowType: "http", Action: "run"}
	_ = s.Create(job)

	h := NewHandler(s)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"name":"updated","cronExpr":"0 * * * *"}`
	req := httptest.NewRequest("PUT", "/api/schedules/"+job.ID, strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDefaultNextRun(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		expr     string
		expected time.Time
	}{
		{"* * * * *", time.Date(2025, 1, 1, 12, 1, 0, 0, time.UTC)},
		{"0 * * * *", time.Date(2025, 1, 1, 13, 0, 0, 0, time.UTC)},
		{"0 0 * * *", time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)},
		{"*/5 * * * *", time.Date(2025, 1, 1, 12, 5, 0, 0, time.UTC)},
	}

	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			next, err := defaultNextRun(tc.expr, now)
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if !next.Equal(tc.expected) {
				t.Errorf("expected %v, got %v", tc.expected, next)
			}
		})
	}
}
