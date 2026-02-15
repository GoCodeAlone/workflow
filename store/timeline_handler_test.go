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
)

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
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
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
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
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
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
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
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
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
