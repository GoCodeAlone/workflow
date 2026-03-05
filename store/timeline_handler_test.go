package store

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// mockLogQuerier is a simple in-memory LogQuerier for tests.
type mockLogQuerier struct {
	logs map[string][]map[string]any // executionID → logs
}

func (m *mockLogQuerier) ListExecutionLogs(executionID string, level string, limit int) ([]map[string]any, error) {
	all := m.logs[executionID]
	var result []map[string]any
	for _, log := range all {
		if level == "" || log["level"] == level {
			result = append(result, log)
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func seedExecution(t *testing.T, store EventStore, execID uuid.UUID, pipeline string) {
	t.Helper()
	ctx := context.Background()

	_ = store.Append(ctx, execID, EventExecutionStarted, map[string]any{
		"pipeline": pipeline,
	})
	_ = store.Append(ctx, execID, EventStepStarted, map[string]any{
		"step_name": "step1",
	})
	_ = store.Append(ctx, execID, EventStepCompleted, map[string]any{
		"step_name": "step1",
	})
	_ = store.Append(ctx, execID, EventStepStarted, map[string]any{
		"step_name": "step2",
	})
	_ = store.Append(ctx, execID, EventStepCompleted, map[string]any{
		"step_name": "step2",
	})
	_ = store.Append(ctx, execID, EventExecutionCompleted, map[string]any{})
}

func TestTimelineHandler_ListExecutions(t *testing.T) {
	store := NewInMemoryEventStore()
	logger := slog.Default()

	exec1 := uuid.New()
	exec2 := uuid.New()
	seedExecution(t, store, exec1, "pipeline-a")
	seedExecution(t, store, exec2, "pipeline-b")

	h := NewTimelineHandler(store, logger)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/admin/executions", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	count := int(resp["count"].(float64))
	if count != 2 {
		t.Errorf("expected 2 executions, got %d", count)
	}
}

func TestTimelineHandler_ListExecutions_FilterPipeline(t *testing.T) {
	store := NewInMemoryEventStore()
	exec1 := uuid.New()
	exec2 := uuid.New()
	seedExecution(t, store, exec1, "pipeline-a")
	seedExecution(t, store, exec2, "pipeline-b")

	h := NewTimelineHandler(store, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/admin/executions?pipeline=pipeline-a", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	count := int(resp["count"].(float64))
	if count != 1 {
		t.Errorf("expected 1 execution for pipeline-a, got %d", count)
	}
}

func TestTimelineHandler_ListExecutions_Empty(t *testing.T) {
	store := NewInMemoryEventStore()
	h := NewTimelineHandler(store, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/admin/executions", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	count := int(resp["count"].(float64))
	if count != 0 {
		t.Errorf("expected 0 executions, got %d", count)
	}
}

func TestTimelineHandler_GetTimeline(t *testing.T) {
	store := NewInMemoryEventStore()
	execID := uuid.New()
	seedExecution(t, store, execID, "my-pipeline")

	h := NewTimelineHandler(store, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/admin/executions/"+execID.String()+"/timeline", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp MaterializedExecution
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode timeline: %v", err)
	}

	if resp.Pipeline != "my-pipeline" {
		t.Errorf("expected pipeline 'my-pipeline', got %q", resp.Pipeline)
	}
	if resp.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", resp.Status)
	}
	if len(resp.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(resp.Steps))
	}
}

func TestTimelineHandler_GetTimeline_NotFound(t *testing.T) {
	store := NewInMemoryEventStore()
	h := NewTimelineHandler(store, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/admin/executions/"+uuid.New().String()+"/timeline", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestTimelineHandler_GetTimeline_InvalidID(t *testing.T) {
	store := NewInMemoryEventStore()
	h := NewTimelineHandler(store, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/admin/executions/invalid-id/timeline", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestTimelineHandler_GetEvents(t *testing.T) {
	store := NewInMemoryEventStore()
	execID := uuid.New()
	seedExecution(t, store, execID, "test")

	h := NewTimelineHandler(store, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/admin/executions/"+execID.String()+"/events", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	count := int(resp["count"].(float64))
	if count != 6 {
		t.Errorf("expected 6 events, got %d", count)
	}
}

func TestTimelineHandler_GetEvents_TypeFilter(t *testing.T) {
	store := NewInMemoryEventStore()
	execID := uuid.New()
	seedExecution(t, store, execID, "test")

	h := NewTimelineHandler(store, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/admin/executions/"+execID.String()+"/events?type=step.started", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	count := int(resp["count"].(float64))
	if count != 2 {
		t.Errorf("expected 2 step.started events, got %d", count)
	}
}

// --- Replay API tests ---

func TestReplayHandler_ReplayExact(t *testing.T) {
	store := NewInMemoryEventStore()
	execID := uuid.New()
	seedExecution(t, store, execID, "test-pipeline")

	h := NewReplayHandler(store, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"mode": "exact"}`
	req := httptest.NewRequest("POST", "/api/v1/admin/executions/"+execID.String()+"/replay", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var result ReplayResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode replay result: %v", err)
	}

	if result.OriginalExecutionID != execID {
		t.Errorf("expected original ID %s, got %s", execID, result.OriginalExecutionID)
	}
	if result.Type != "replay" {
		t.Errorf("expected type 'replay', got %q", result.Type)
	}
	if result.Mode != "exact" {
		t.Errorf("expected mode 'exact', got %q", result.Mode)
	}
	if result.NewExecutionID == uuid.Nil {
		t.Error("expected non-nil new execution ID")
	}
}

func TestReplayHandler_ReplayWithFunc(t *testing.T) {
	store := NewInMemoryEventStore()
	execID := uuid.New()
	seedExecution(t, store, execID, "test-pipeline")

	replayExecID := uuid.New()
	h := NewReplayHandler(store, nil)
	h.ReplayFunc = func(original *MaterializedExecution, mode string, mods map[string]any) (uuid.UUID, error) {
		if original.Pipeline != "test-pipeline" {
			t.Errorf("expected pipeline 'test-pipeline', got %q", original.Pipeline)
		}
		return replayExecID, nil
	}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"mode": "exact"}`
	req := httptest.NewRequest("POST", "/api/v1/admin/executions/"+execID.String()+"/replay", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var result ReplayResult
	_ = json.Unmarshal(w.Body.Bytes(), &result)

	if result.NewExecutionID != replayExecID {
		t.Errorf("expected replay exec ID %s, got %s", replayExecID, result.NewExecutionID)
	}
	if result.Status != "started" {
		t.Errorf("expected status 'started', got %q", result.Status)
	}
}

func TestReplayHandler_ReplayNotFound(t *testing.T) {
	store := NewInMemoryEventStore()
	h := NewReplayHandler(store, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"mode": "exact"}`
	req := httptest.NewRequest("POST", "/api/v1/admin/executions/"+uuid.New().String()+"/replay", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestReplayHandler_ReplayInvalidMode(t *testing.T) {
	store := NewInMemoryEventStore()
	execID := uuid.New()
	seedExecution(t, store, execID, "test")

	h := NewReplayHandler(store, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"mode": "invalid"}`
	req := httptest.NewRequest("POST", "/api/v1/admin/executions/"+execID.String()+"/replay", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestReplayHandler_ReplayDefaultMode(t *testing.T) {
	store := NewInMemoryEventStore()
	execID := uuid.New()
	seedExecution(t, store, execID, "test")

	h := NewReplayHandler(store, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Empty body - should default to exact
	req := httptest.NewRequest("POST", "/api/v1/admin/executions/"+execID.String()+"/replay", bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var result ReplayResult
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if result.Mode != "exact" {
		t.Errorf("expected default mode 'exact', got %q", result.Mode)
	}
}

func TestReplayHandler_GetReplayInfo(t *testing.T) {
	store := NewInMemoryEventStore()
	execID := uuid.New()
	seedExecution(t, store, execID, "test")

	h := NewReplayHandler(store, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// First create a replay
	body := `{"mode": "exact"}`
	req := httptest.NewRequest("POST", "/api/v1/admin/executions/"+execID.String()+"/replay", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var result ReplayResult
	_ = json.Unmarshal(w.Body.Bytes(), &result)

	// Now get replay info for the new execution
	req = httptest.NewRequest("GET", "/api/v1/admin/executions/"+result.NewExecutionID.String()+"/replay", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var info map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &info)
	isReplay := info["is_replay"].(bool)
	if !isReplay {
		t.Error("expected is_replay to be true")
	}
}

func TestReplayHandler_GetReplayInfo_NotReplay(t *testing.T) {
	store := NewInMemoryEventStore()
	execID := uuid.New()
	seedExecution(t, store, execID, "test")

	h := NewReplayHandler(store, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/admin/executions/"+execID.String()+"/replay", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var info map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &info)
	isReplay := info["is_replay"].(bool)
	if isReplay {
		t.Error("expected is_replay to be false for non-replay execution")
	}
}

// --- Execution Logs API tests ---

func TestTimelineHandler_GetExecutionLogs(t *testing.T) {
	eventStore := NewInMemoryEventStore()
	execID := uuid.New().String()

	lq := &mockLogQuerier{
		logs: map[string][]map[string]any{
			execID: {
				{"id": 1, "level": "info", "message": "Step started", "module_name": "step1", "created_at": "2026-01-01T00:00:00Z"},
				{"id": 2, "level": "error", "message": "Something failed", "module_name": "step2", "created_at": "2026-01-01T00:00:01Z"},
				{"id": 3, "level": "info", "message": "Step completed", "module_name": "step1", "created_at": "2026-01-01T00:00:02Z"},
			},
		},
	}

	h := NewTimelineHandler(eventStore, nil).WithLogQuerier(lq)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/admin/executions/"+execID+"/logs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	count := int(resp["count"].(float64))
	if count != 3 {
		t.Errorf("expected 3 logs, got %d", count)
	}
}

func TestTimelineHandler_GetExecutionLogs_LevelFilter(t *testing.T) {
	eventStore := NewInMemoryEventStore()
	execID := uuid.New().String()

	lq := &mockLogQuerier{
		logs: map[string][]map[string]any{
			execID: {
				{"id": 1, "level": "info", "message": "Step started"},
				{"id": 2, "level": "error", "message": "Something failed"},
				{"id": 3, "level": "info", "message": "Step completed"},
			},
		},
	}

	h := NewTimelineHandler(eventStore, nil).WithLogQuerier(lq)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/admin/executions/"+execID+"/logs?level=error", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	count := int(resp["count"].(float64))
	if count != 1 {
		t.Errorf("expected 1 error log, got %d", count)
	}
}

func TestTimelineHandler_GetExecutionLogs_NoQuerier(t *testing.T) {
	eventStore := NewInMemoryEventStore()
	execID := uuid.New().String()

	h := NewTimelineHandler(eventStore, nil)
	// No WithLogQuerier call
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/admin/executions/"+execID+"/logs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501 without log querier, got %d", w.Code)
	}
}

func TestTimelineHandler_GetExecutionLogs_Empty(t *testing.T) {
	eventStore := NewInMemoryEventStore()
	execID := uuid.New().String()

	lq := &mockLogQuerier{logs: map[string][]map[string]any{}}
	h := NewTimelineHandler(eventStore, nil).WithLogQuerier(lq)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/admin/executions/"+execID+"/logs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	count := int(resp["count"].(float64))
	if count != 0 {
		t.Errorf("expected 0 logs, got %d", count)
	}
}

// TestAPI_ExplicitTrace_EndToEnd verifies the full logs-endpoint flow for an
// explicitly-traced execution: all log entries (including step I/O events) are
// returned when querying /executions/{id}/logs without a level filter.
func TestAPI_ExplicitTrace_EndToEnd(t *testing.T) {
	eventStore := NewInMemoryEventStore()
	execID := uuid.New().String()

	// Simulate the log entries that ExecutionTracker writes for an explicit trace:
	// - execution.started
	// - step.input_recorded (event)
	// - step.started        (info)
	// - step.output_recorded (event)
	// - step.completed      (info)
	// - execution.completed (info)
	lq := &mockLogQuerier{
		logs: map[string][]map[string]any{
			execID: {
				{"id": 1, "level": "event", "message": "execution.started", "module_name": ""},
				{"id": 2, "level": "event", "message": "step.input_recorded", "module_name": "step1"},
				{"id": 3, "level": "info", "message": "Step started: step1", "module_name": "step1"},
				{"id": 4, "level": "event", "message": "step.output_recorded", "module_name": "step1"},
				{"id": 5, "level": "info", "message": "Step completed: step1 (3ms)", "module_name": "step1"},
				{"id": 6, "level": "info", "message": "Execution completed (3ms)", "module_name": ""},
			},
		},
	}

	h := NewTimelineHandler(eventStore, nil).WithLogQuerier(lq)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/admin/executions/"+execID+"/logs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	count := int(resp["count"].(float64))
	if count != 6 {
		t.Errorf("expected 6 log entries (including I/O events), got %d", count)
	}

	logs, ok := resp["logs"].([]any)
	require.True(t, ok, "expected logs field to be an array")
	if len(logs) != 6 {
		t.Errorf("expected logs array length 6, got %d", len(logs))
	}

	// Verify I/O event entries are present among the returned logs.
	messages := make(map[string]bool)
	for _, entry := range logs {
		if m, ok := entry.(map[string]any)["message"].(string); ok {
			messages[m] = true
		}
	}
	if !messages["step.input_recorded"] {
		t.Error("expected step.input_recorded in logs")
	}
	if !messages["step.output_recorded"] {
		t.Error("expected step.output_recorded in logs")
	}
}
