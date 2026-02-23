package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDefaultReporterConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultReporterConfig()
	if cfg.FlushInterval != 5*time.Second {
		t.Errorf("expected FlushInterval 5s, got %v", cfg.FlushInterval)
	}
	if cfg.BatchSize != 100 {
		t.Errorf("expected BatchSize 100, got %d", cfg.BatchSize)
	}
	if cfg.HeartbeatInterval != 30*time.Second {
		t.Errorf("expected HeartbeatInterval 30s, got %v", cfg.HeartbeatInterval)
	}
	if cfg.InstanceName == "" {
		t.Log("InstanceName empty (hostname unavailable), acceptable")
	}
}

func TestNewReporter_Defaults(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tests := []struct {
		name   string
		config ReporterConfig
		check  func(t *testing.T, r *Reporter)
	}{
		{
			name:   "zero flush interval gets default",
			config: ReporterConfig{},
			check: func(t *testing.T, r *Reporter) {
				if r.config.FlushInterval != 5*time.Second {
					t.Errorf("expected default FlushInterval, got %v", r.config.FlushInterval)
				}
			},
		},
		{
			name:   "zero batch size gets default",
			config: ReporterConfig{},
			check: func(t *testing.T, r *Reporter) {
				if r.config.BatchSize != 100 {
					t.Errorf("expected default BatchSize, got %d", r.config.BatchSize)
				}
			},
		},
		{
			name:   "zero heartbeat gets default",
			config: ReporterConfig{},
			check: func(t *testing.T, r *Reporter) {
				if r.config.HeartbeatInterval != 30*time.Second {
					t.Errorf("expected default HeartbeatInterval, got %v", r.config.HeartbeatInterval)
				}
			},
		},
		{
			name: "custom values preserved",
			config: ReporterConfig{
				FlushInterval:     10 * time.Second,
				BatchSize:         50,
				HeartbeatInterval: 60 * time.Second,
				InstanceName:      "test-worker",
			},
			check: func(t *testing.T, r *Reporter) {
				if r.config.FlushInterval != 10*time.Second {
					t.Errorf("expected 10s FlushInterval, got %v", r.config.FlushInterval)
				}
				if r.config.BatchSize != 50 {
					t.Errorf("expected 50 BatchSize, got %d", r.config.BatchSize)
				}
				if r.config.InstanceName != "test-worker" {
					t.Errorf("expected instance name 'test-worker', got %q", r.config.InstanceName)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := NewReporter(tt.config, logger)
			if r == nil {
				t.Fatal("NewReporter returned nil")
			}
			tt.check(t, r)
		})
	}
}

func TestReporter_BufferAndFlush(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	receivedPaths := make(map[string]int)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedPaths[r.URL.Path]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reporter := NewReporter(ReporterConfig{
		AdminURL:          server.URL,
		FlushInterval:     50 * time.Millisecond,
		BatchSize:         10,
		InstanceName:      "test",
		HeartbeatInterval: 1 * time.Hour, // don't heartbeat during test
	}, logger)

	reporter.ReportExecution(ExecutionReport{
		ID: "exec-1", WorkflowID: "wf-1", Status: "completed",
	})
	reporter.ReportLog(LogReport{
		WorkflowID: "wf-1", Level: "info", Message: "test log",
	})
	reporter.ReportEvent(EventReport{
		ExecutionID: "exec-1", EventType: "step_completed",
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	reporter.Start(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		execFlushed := receivedPaths["/api/v1/admin/ingest/executions"] > 0
		logsFlushed := receivedPaths["/api/v1/admin/ingest/logs"] > 0
		eventsFlushed := receivedPaths["/api/v1/admin/ingest/events"] > 0
		mu.Unlock()

		if execFlushed && logsFlushed && eventsFlushed {
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for reports to be flushed, got: %#v", receivedPaths)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func TestReporter_StopFlushesRemaining(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	received := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/admin/ingest/executions" {
			mu.Lock()
			received = true
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reporter := NewReporter(ReporterConfig{
		AdminURL:          server.URL,
		FlushInterval:     1 * time.Hour, // won't auto-flush
		BatchSize:         100,
		InstanceName:      "test",
		HeartbeatInterval: 1 * time.Hour,
	}, logger)

	reporter.ReportExecution(ExecutionReport{ID: "exec-final", Status: "completed"})

	ctx, cancel := context.WithCancel(context.Background())
	reporter.Start(ctx)
	cancel() // triggers final flush

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if !received {
		t.Error("expected final flush on Stop to send buffered data")
	}
}

func TestReporter_ConcurrentReports(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reporter := NewReporter(ReporterConfig{
		AdminURL:     "http://localhost:0", // will fail to send, that's ok
		BatchSize:    1000,
		InstanceName: "test",
	}, logger)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func(n int) {
			defer wg.Done()
			reporter.ReportExecution(ExecutionReport{ID: fmt.Sprintf("exec-%d", n)})
		}(i)
		go func(n int) {
			defer wg.Done()
			reporter.ReportLog(LogReport{Message: fmt.Sprintf("log-%d", n)})
		}(i)
		go func(n int) {
			defer wg.Done()
			reporter.ReportEvent(EventReport{EventType: fmt.Sprintf("event-%d", n)})
		}(i)
	}
	wg.Wait()

	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	if len(reporter.executions) != 100 {
		t.Errorf("expected 100 executions, got %d", len(reporter.executions))
	}
	if len(reporter.logs) != 100 {
		t.Errorf("expected 100 logs, got %d", len(reporter.logs))
	}
	if len(reporter.events) != 100 {
		t.Errorf("expected 100 events, got %d", len(reporter.events))
	}
}

func TestReporter_ServerError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reporter := NewReporter(ReporterConfig{
		AdminURL:     server.URL,
		BatchSize:    10,
		InstanceName: "test",
	}, logger)

	reporter.ReportExecution(ExecutionReport{ID: "exec-err"})

	// Flush should not panic on server errors
	reporter.flush(context.Background())
}

func TestReporter_Stop_NilCancel(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reporter := NewReporter(ReporterConfig{InstanceName: "test"}, logger)

	// Stop before Start — should not panic
	reporter.Stop()
}

// --- IngestHandler tests ---

type mockIngestStore struct {
	mu          sync.Mutex
	executions  int
	logs        int
	events      int
	instances   map[string]bool
	heartbeats  map[string]int
	ingestError error
}

func newMockIngestStore() *mockIngestStore {
	return &mockIngestStore{
		instances:  make(map[string]bool),
		heartbeats: make(map[string]int),
	}
}

func (s *mockIngestStore) IngestExecutions(_ context.Context, _ string, items []ExecutionReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ingestError != nil {
		return s.ingestError
	}
	s.executions += len(items)
	return nil
}

func (s *mockIngestStore) IngestLogs(_ context.Context, _ string, items []LogReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ingestError != nil {
		return s.ingestError
	}
	s.logs += len(items)
	return nil
}

func (s *mockIngestStore) IngestEvents(_ context.Context, _ string, items []EventReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ingestError != nil {
		return s.ingestError
	}
	s.events += len(items)
	return nil
}

func (s *mockIngestStore) RegisterInstance(_ context.Context, name string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instances[name] = true
	return nil
}

func (s *mockIngestStore) Heartbeat(_ context.Context, name string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heartbeats[name]++
	return nil
}

func TestIngestHandler_Executions(t *testing.T) {
	t.Parallel()

	store := newMockIngestStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewIngestHandler(store, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{"instance":"worker-1","items":[{"id":"e1","status":"completed"},{"id":"e2","status":"failed"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ingest/executions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp map[string]int
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["accepted"] != 2 {
		t.Errorf("expected 2 accepted, got %d", resp["accepted"])
	}
}

func TestIngestHandler_Logs(t *testing.T) {
	t.Parallel()

	store := newMockIngestStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewIngestHandler(store, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{"instance":"worker-1","items":[{"workflow_id":"wf1","level":"info","message":"test"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ingest/logs", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestIngestHandler_Events(t *testing.T) {
	t.Parallel()

	store := newMockIngestStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewIngestHandler(store, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{"instance":"worker-1","items":[{"execution_id":"e1","event_type":"step_done","event_data":{"step":"build"}}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ingest/events", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestIngestHandler_InvalidJSON(t *testing.T) {
	t.Parallel()

	store := newMockIngestStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewIngestHandler(store, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	endpoints := []string{
		"/api/v1/admin/ingest/executions",
		"/api/v1/admin/ingest/logs",
		"/api/v1/admin/ingest/events",
	}

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, ep, strings.NewReader("{invalid"))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for invalid JSON at %s, got %d", ep, rec.Code)
			}
		})
	}
}

func TestIngestHandler_StoreError(t *testing.T) {
	t.Parallel()

	store := newMockIngestStore()
	store.ingestError = fmt.Errorf("database unavailable")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewIngestHandler(store, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{"instance":"w1","items":[{"id":"e1","status":"done"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ingest/executions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestIngestHandler_Health(t *testing.T) {
	t.Parallel()

	store := newMockIngestStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewIngestHandler(store, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ingest/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %q", resp["status"])
	}
}

func TestIngestHandler_Register(t *testing.T) {
	t.Parallel()

	store := newMockIngestStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewIngestHandler(store, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{"instance_name":"worker-1","registered_at":"2024-01-01T00:00:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/instances/register", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if !store.instances["worker-1"] {
		t.Error("expected worker-1 to be registered")
	}
}

func TestIngestHandler_Register_InvalidTimestamp(t *testing.T) {
	t.Parallel()

	store := newMockIngestStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewIngestHandler(store, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{"instance_name":"worker-2","registered_at":"not-a-date"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/instances/register", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// Should still succeed — falls back to time.Now()
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestIngestHandler_Heartbeat(t *testing.T) {
	t.Parallel()

	store := newMockIngestStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewIngestHandler(store, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{"instance_name":"worker-1","timestamp":"2024-01-01T00:00:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/instances/heartbeat", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if store.heartbeats["worker-1"] != 1 {
		t.Errorf("expected 1 heartbeat, got %d", store.heartbeats["worker-1"])
	}
}

func TestIngestHandler_Heartbeat_InvalidJSON(t *testing.T) {
	t.Parallel()

	store := newMockIngestStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewIngestHandler(store, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/instances/heartbeat", strings.NewReader("{bad"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestIngestHandler_Register_InvalidJSON(t *testing.T) {
	t.Parallel()

	store := newMockIngestStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewIngestHandler(store, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/instances/register", strings.NewReader("{bad"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}
