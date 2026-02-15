package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockAPI sets up an httptest server that simulates the workflow engine API.
func mockAPI(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	// GET /api/v1/workflows
	mux.HandleFunc("GET /api/v1/workflows", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":         "wf-1",
				"name":       "order-pipeline",
				"version":    1,
				"status":     "active",
				"config":     map[string]any{},
				"created_at": "2026-01-01T00:00:00Z",
				"updated_at": "2026-01-01T00:00:00Z",
			},
		})
	})

	// GET /api/v1/workflows/{id}
	mux.HandleFunc("GET /api/v1/workflows/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "not-found" {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":"not found"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":         id,
			"name":       "test-workflow",
			"version":    1,
			"status":     "active",
			"config":     map[string]any{},
			"created_at": "2026-01-01T00:00:00Z",
			"updated_at": "2026-01-01T00:00:00Z",
		})
	})

	// POST /api/v1/workflows
	mux.HandleFunc("POST /api/v1/workflows", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"id":         "wf-new",
			"name":       "new-workflow",
			"version":    1,
			"status":     "draft",
			"config":     body,
			"created_at": "2026-01-01T00:00:00Z",
			"updated_at": "2026-01-01T00:00:00Z",
		})
	})

	// DELETE /api/v1/workflows/{id}
	mux.HandleFunc("DELETE /api/v1/workflows/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// POST /api/v1/workflows/{id}/execute
	mux.HandleFunc("POST /api/v1/workflows/{id}/execute", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "exec-1",
			"workflow_id": id,
			"status":      "running",
			"input":       map[string]any{"order_id": "123"},
			"started_at":  "2026-01-01T00:00:00Z",
			"steps":       []any{},
		})
	})

	// GET /api/v1/executions/{id}
	mux.HandleFunc("GET /api/v1/executions/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          id,
			"workflow_id": "wf-1",
			"status":      "completed",
			"input":       map[string]any{},
			"started_at":  "2026-01-01T00:00:00Z",
			"steps": []map[string]any{
				{
					"name":       "validate",
					"status":     "completed",
					"started_at": "2026-01-01T00:00:00Z",
				},
			},
		})
	})

	// GET /api/v1/executions
	mux.HandleFunc("GET /api/v1/executions", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":          "exec-1",
				"workflow_id": "wf-1",
				"status":      "completed",
				"input":       map[string]any{},
				"started_at":  "2026-01-01T00:00:00Z",
				"steps":       []any{},
			},
		})
	})

	// GET /api/v1/executions/{id}/stream (SSE)
	mux.HandleFunc("GET /api/v1/executions/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		events := []SSEEvent{
			{ID: "1", Event: "step.started", Data: `{"step":"validate"}`},
			{ID: "2", Event: "step.completed", Data: `{"step":"validate","duration_ms":10}`},
			{ID: "3", Event: "step.started", Data: `{"step":"process"}`},
		}

		for _, evt := range events {
			fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", evt.ID, evt.Event, evt.Data)
			flusher.Flush()
		}
	})

	// GET /api/v1/dlq
	mux.HandleFunc("GET /api/v1/dlq", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":           "dlq-1",
				"workflow_id":  "wf-1",
				"execution_id": "exec-1",
				"error":        "timeout",
				"payload":      map[string]any{},
				"retry_count":  2,
				"max_retries":  5,
				"created_at":   "2026-01-01T00:00:00Z",
			},
		})
	})

	// POST /api/v1/dlq/{id}/retry
	mux.HandleFunc("POST /api/v1/dlq/{id}/retry", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// GET /healthz
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "healthy",
			"checks": map[string]any{
				"database": map[string]any{"status": "healthy", "message": "connected"},
			},
		})
	})

	return httptest.NewServer(mux)
}

func TestListWorkflows(t *testing.T) {
	server := mockAPI(t)
	defer server.Close()

	client := NewClient(server.URL)
	workflows, err := client.ListWorkflows(context.Background())
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}

	if len(workflows) != 1 {
		t.Fatalf("expected 1 workflow, got %d", len(workflows))
	}
	if workflows[0].ID != "wf-1" {
		t.Errorf("expected ID 'wf-1', got %q", workflows[0].ID)
	}
	if workflows[0].Name != "order-pipeline" {
		t.Errorf("expected name 'order-pipeline', got %q", workflows[0].Name)
	}
}

func TestGetWorkflow(t *testing.T) {
	server := mockAPI(t)
	defer server.Close()

	client := NewClient(server.URL)
	wf, err := client.GetWorkflow(context.Background(), "wf-1")
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.ID != "wf-1" {
		t.Errorf("expected ID 'wf-1', got %q", wf.ID)
	}
}

func TestGetWorkflowNotFound(t *testing.T) {
	server := mockAPI(t)
	defer server.Close()

	client := NewClient(server.URL)
	_, err := client.GetWorkflow(context.Background(), "not-found")
	if err == nil {
		t.Fatal("expected error for not-found workflow")
	}

	var wfErr *WorkflowError
	if ok := isWorkflowError(err, &wfErr); !ok {
		t.Fatalf("expected WorkflowError, got %T: %v", err, err)
	}
	if wfErr.StatusCode != 404 {
		t.Errorf("expected status 404, got %d", wfErr.StatusCode)
	}
}

func TestCreateWorkflow(t *testing.T) {
	server := mockAPI(t)
	defer server.Close()

	client := NewClient(server.URL)
	wf, err := client.CreateWorkflow(context.Background(), map[string]any{
		"name": "test",
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if wf.ID != "wf-new" {
		t.Errorf("expected ID 'wf-new', got %q", wf.ID)
	}
}

func TestDeleteWorkflow(t *testing.T) {
	server := mockAPI(t)
	defer server.Close()

	client := NewClient(server.URL)
	err := client.DeleteWorkflow(context.Background(), "wf-1")
	if err != nil {
		t.Fatalf("DeleteWorkflow: %v", err)
	}
}

func TestExecuteWorkflow(t *testing.T) {
	server := mockAPI(t)
	defer server.Close()

	client := NewClient(server.URL)
	exec, err := client.ExecuteWorkflow(context.Background(), "wf-1", map[string]any{
		"order_id": "12345",
	})
	if err != nil {
		t.Fatalf("ExecuteWorkflow: %v", err)
	}
	if exec.ID != "exec-1" {
		t.Errorf("expected execution ID 'exec-1', got %q", exec.ID)
	}
	if exec.Status != "running" {
		t.Errorf("expected status 'running', got %q", exec.Status)
	}
}

func TestGetExecution(t *testing.T) {
	server := mockAPI(t)
	defer server.Close()

	client := NewClient(server.URL)
	exec, err := client.GetExecution(context.Background(), "exec-1")
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	if exec.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", exec.Status)
	}
	if len(exec.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(exec.Steps))
	}
	if exec.Steps[0].Name != "validate" {
		t.Errorf("expected step name 'validate', got %q", exec.Steps[0].Name)
	}
}

func TestListExecutions(t *testing.T) {
	server := mockAPI(t)
	defer server.Close()

	client := NewClient(server.URL)
	execs, err := client.ListExecutions(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(execs) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(execs))
	}
}

func TestListExecutionsWithFilter(t *testing.T) {
	server := mockAPI(t)
	defer server.Close()

	client := NewClient(server.URL)
	execs, err := client.ListExecutions(context.Background(), &ExecutionFilter{
		WorkflowID: "wf-1",
		Status:     "completed",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("ListExecutions with filter: %v", err)
	}
	if len(execs) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(execs))
	}
}

func TestStreamExecution(t *testing.T) {
	server := mockAPI(t)
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := client.StreamExecution(ctx, "exec-1")
	if err != nil {
		t.Fatalf("StreamExecution: %v", err)
	}

	var events []SSEEvent
	for event := range ch {
		events = append(events, event)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 SSE events, got %d", len(events))
	}

	// Verify first event
	if events[0].ID != "1" {
		t.Errorf("event 0: expected ID '1', got %q", events[0].ID)
	}
	if events[0].Event != "step.started" {
		t.Errorf("event 0: expected event 'step.started', got %q", events[0].Event)
	}
	if !strings.Contains(events[0].Data, "validate") {
		t.Errorf("event 0: expected data to contain 'validate', got %q", events[0].Data)
	}

	// Verify second event
	if events[1].Event != "step.completed" {
		t.Errorf("event 1: expected event 'step.completed', got %q", events[1].Event)
	}

	// Verify third event
	if events[2].Event != "step.started" {
		t.Errorf("event 2: expected event 'step.started', got %q", events[2].Event)
	}
}

func TestStreamExecutionContextCancel(t *testing.T) {
	// Create a server that keeps the SSE connection open
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		// Send one event then hold the connection
		fmt.Fprint(w, "id: 1\nevent: step.started\ndata: {}\n\n")
		flusher.Flush()

		// Block until client disconnects
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := client.StreamExecution(ctx, "exec-1")
	if err != nil {
		t.Fatalf("StreamExecution: %v", err)
	}

	// Read first event
	select {
	case event := <-ch:
		if event.ID != "1" {
			t.Errorf("expected event ID '1', got %q", event.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first event")
	}

	// Cancel context
	cancel()

	// Channel should close
	select {
	case _, ok := <-ch:
		if ok {
			// May get a few more events that were buffered
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel not closed after context cancellation")
	}
}

func TestListDLQEntries(t *testing.T) {
	server := mockAPI(t)
	defer server.Close()

	client := NewClient(server.URL)
	entries, err := client.ListDLQEntries(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListDLQEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 DLQ entry, got %d", len(entries))
	}
	if entries[0].ID != "dlq-1" {
		t.Errorf("expected DLQ ID 'dlq-1', got %q", entries[0].ID)
	}
}

func TestRetryDLQEntry(t *testing.T) {
	server := mockAPI(t)
	defer server.Close()

	client := NewClient(server.URL)
	err := client.RetryDLQEntry(context.Background(), "dlq-1")
	if err != nil {
		t.Fatalf("RetryDLQEntry: %v", err)
	}
}

func TestHealth(t *testing.T) {
	server := mockAPI(t)
	defer server.Close()

	client := NewClient(server.URL)
	status, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if status.Status != "healthy" {
		t.Errorf("expected status 'healthy', got %q", status.Status)
	}
	if check, ok := status.Checks["database"]; !ok {
		t.Error("expected 'database' check")
	} else if check.Status != "healthy" {
		t.Errorf("expected database check 'healthy', got %q", check.Status)
	}
}

func TestWithAPIKey(t *testing.T) {
	// Verify the API key is sent in requests
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "healthy",
			"checks": map[string]any{},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, WithAPIKey("test-key-123"))
	_, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if receivedAuth != "Bearer test-key-123" {
		t.Errorf("expected Authorization 'Bearer test-key-123', got %q", receivedAuth)
	}
}

func TestConcurrentRequests(t *testing.T) {
	server := mockAPI(t)
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()

	var wg sync.WaitGroup
	errCh := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := client.ListWorkflows(ctx)
			if err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent request failed: %v", err)
	}
}

func TestWorkflowErrorMessage(t *testing.T) {
	err := &WorkflowError{
		StatusCode: 404,
		Message:    "404 Not Found",
		Body:       `{"error":"not found"}`,
	}
	msg := err.Error()
	if !strings.Contains(msg, "404") {
		t.Errorf("error message should contain status code: %q", msg)
	}
	if !strings.Contains(msg, "not found") {
		t.Errorf("error message should contain body: %q", msg)
	}
}

// isWorkflowError checks if err is a *WorkflowError and extracts it.
func isWorkflowError(err error, target **WorkflowError) bool {
	if wfErr, ok := err.(*WorkflowError); ok {
		*target = wfErr
		return true
	}
	return false
}
