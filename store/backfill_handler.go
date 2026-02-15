package store

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// BackfillMockDiffHandler provides HTTP endpoints for backfill, step mock,
// and execution diff management.
type BackfillMockDiffHandler struct {
	backfillStore BackfillStore
	mockStore     StepMockStore
	diffCalc      *DiffCalculator
	logger        *slog.Logger
}

// NewBackfillMockDiffHandler creates a new handler with the given stores and calculator.
func NewBackfillMockDiffHandler(
	backfillStore BackfillStore,
	mockStore StepMockStore,
	diffCalc *DiffCalculator,
	logger *slog.Logger,
) *BackfillMockDiffHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &BackfillMockDiffHandler{
		backfillStore: backfillStore,
		mockStore:     mockStore,
		diffCalc:      diffCalc,
		logger:        logger,
	}
}

// RegisterRoutes registers all backfill, mock, and diff API routes on the given mux.
func (h *BackfillMockDiffHandler) RegisterRoutes(mux *http.ServeMux) {
	// Backfill routes
	mux.HandleFunc("GET /api/v1/admin/backfill", h.handleBackfillList)
	mux.HandleFunc("POST /api/v1/admin/backfill", h.handleBackfillCreate)
	mux.HandleFunc("GET /api/v1/admin/backfill/{id}", h.handleBackfillGet)
	mux.HandleFunc("POST /api/v1/admin/backfill/{id}/cancel", h.handleBackfillCancel)

	// Mock routes
	mux.HandleFunc("GET /api/v1/admin/mocks", h.handleMockList)
	mux.HandleFunc("POST /api/v1/admin/mocks", h.handleMockSet)
	mux.HandleFunc("DELETE /api/v1/admin/mocks", h.handleMockClearAll)
	mux.HandleFunc("GET /api/v1/admin/mocks/{pipeline}", h.handleMockListPipeline)
	mux.HandleFunc("DELETE /api/v1/admin/mocks/{pipeline}/{step}", h.handleMockRemove)

	// Diff routes
	mux.HandleFunc("GET /api/v1/admin/executions/diff", h.handleExecutionDiff)
}

// ---------------------------------------------------------------------------
// Backfill handlers
// ---------------------------------------------------------------------------

func (h *BackfillMockDiffHandler) handleBackfillList(w http.ResponseWriter, r *http.Request) {
	requests, err := h.backfillStore.List(r.Context())
	if err != nil {
		h.logger.Error("list backfill requests", "error", err)
		writeHandlerError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeHandlerJSON(w, http.StatusOK, requests)
}

func (h *BackfillMockDiffHandler) handleBackfillCreate(w http.ResponseWriter, r *http.Request) {
	var req BackfillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeHandlerError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.PipelineName == "" {
		writeHandlerError(w, http.StatusBadRequest, "pipeline_name is required")
		return
	}

	if err := h.backfillStore.Create(r.Context(), &req); err != nil {
		h.logger.Error("create backfill request", "error", err)
		writeHandlerError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeHandlerJSON(w, http.StatusCreated, req)
}

func (h *BackfillMockDiffHandler) handleBackfillGet(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeHandlerError(w, http.StatusBadRequest, "invalid id")
		return
	}

	req, err := h.backfillStore.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeHandlerError(w, http.StatusNotFound, "backfill request not found")
			return
		}
		h.logger.Error("get backfill request", "error", err)
		writeHandlerError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeHandlerJSON(w, http.StatusOK, req)
}

func (h *BackfillMockDiffHandler) handleBackfillCancel(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeHandlerError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := h.backfillStore.Cancel(r.Context(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeHandlerError(w, http.StatusNotFound, "backfill request not found")
			return
		}
		if errors.Is(err, ErrConflict) {
			writeHandlerError(w, http.StatusConflict, err.Error())
			return
		}
		h.logger.Error("cancel backfill request", "error", err)
		writeHandlerError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeHandlerJSON(w, http.StatusOK, map[string]any{"status": "cancelled"})
}

// ---------------------------------------------------------------------------
// Mock handlers
// ---------------------------------------------------------------------------

func (h *BackfillMockDiffHandler) handleMockList(w http.ResponseWriter, r *http.Request) {
	// List all mocks across all pipelines — iterate known pipelines is not possible
	// with the current interface, so we use an empty pipeline to signal "all".
	// For this endpoint, the store needs to handle listing all.
	// Since the interface takes a pipeline name, we list with empty string
	// which won't match any. Instead we use a different approach: return all mocks
	// from the in-memory store.
	//
	// We call List with each unique pipeline but there's no ListAll interface method.
	// For the HTTP API, we'll list with a query parameter.
	pipeline := r.URL.Query().Get("pipeline")
	mocks, err := h.mockStore.List(r.Context(), pipeline)
	if err != nil {
		h.logger.Error("list mocks", "error", err)
		writeHandlerError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeHandlerJSON(w, http.StatusOK, mocks)
}

func (h *BackfillMockDiffHandler) handleMockListPipeline(w http.ResponseWriter, r *http.Request) {
	pipeline := r.PathValue("pipeline")
	if pipeline == "" {
		writeHandlerError(w, http.StatusBadRequest, "pipeline is required")
		return
	}

	mocks, err := h.mockStore.List(r.Context(), pipeline)
	if err != nil {
		h.logger.Error("list mocks for pipeline", "error", err)
		writeHandlerError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeHandlerJSON(w, http.StatusOK, mocks)
}

// mockSetRequest is the JSON body for the set mock endpoint.
type mockSetRequest struct {
	PipelineName  string         `json:"pipeline_name"`
	StepName      string         `json:"step_name"`
	Response      map[string]any `json:"response"`
	ErrorResponse string         `json:"error_response,omitempty"`
	Delay         time.Duration  `json:"delay,omitempty"`
	Enabled       *bool          `json:"enabled,omitempty"`
}

func (h *BackfillMockDiffHandler) handleMockSet(w http.ResponseWriter, r *http.Request) {
	var body mockSetRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeHandlerError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if body.PipelineName == "" || body.StepName == "" {
		writeHandlerError(w, http.StatusBadRequest, "pipeline_name and step_name are required")
		return
	}

	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}

	mock := &StepMock{
		PipelineName:  body.PipelineName,
		StepName:      body.StepName,
		Response:      body.Response,
		ErrorResponse: body.ErrorResponse,
		Delay:         body.Delay,
		Enabled:       enabled,
	}

	if err := h.mockStore.Set(r.Context(), mock); err != nil {
		h.logger.Error("set mock", "error", err)
		writeHandlerError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeHandlerJSON(w, http.StatusCreated, mock)
}

func (h *BackfillMockDiffHandler) handleMockRemove(w http.ResponseWriter, r *http.Request) {
	pipeline := r.PathValue("pipeline")
	step := r.PathValue("step")

	if pipeline == "" || step == "" {
		writeHandlerError(w, http.StatusBadRequest, "pipeline and step are required")
		return
	}

	if err := h.mockStore.Remove(r.Context(), pipeline, step); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeHandlerError(w, http.StatusNotFound, "mock not found")
			return
		}
		h.logger.Error("remove mock", "error", err)
		writeHandlerError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeHandlerJSON(w, http.StatusOK, map[string]any{"status": "removed"})
}

func (h *BackfillMockDiffHandler) handleMockClearAll(w http.ResponseWriter, r *http.Request) {
	if err := h.mockStore.ClearAll(r.Context()); err != nil {
		h.logger.Error("clear all mocks", "error", err)
		writeHandlerError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeHandlerJSON(w, http.StatusOK, map[string]any{"status": "cleared"})
}

// ---------------------------------------------------------------------------
// Diff handler
// ---------------------------------------------------------------------------

func (h *BackfillMockDiffHandler) handleExecutionDiff(w http.ResponseWriter, r *http.Request) {
	aStr := r.URL.Query().Get("a")
	bStr := r.URL.Query().Get("b")

	if aStr == "" || bStr == "" {
		writeHandlerError(w, http.StatusBadRequest, "query parameters 'a' and 'b' are required")
		return
	}

	execA, err := uuid.Parse(aStr)
	if err != nil {
		writeHandlerError(w, http.StatusBadRequest, "invalid execution id 'a'")
		return
	}

	execB, err := uuid.Parse(bStr)
	if err != nil {
		writeHandlerError(w, http.StatusBadRequest, "invalid execution id 'b'")
		return
	}

	diff, err := h.diffCalc.Compare(r.Context(), execA, execB)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeHandlerError(w, http.StatusNotFound, err.Error())
			return
		}
		h.logger.Error("compare executions", "error", err)
		writeHandlerError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeHandlerJSON(w, http.StatusOK, diff)
}

// ---------------------------------------------------------------------------
// Response helpers
// ---------------------------------------------------------------------------

func writeHandlerJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeHandlerError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
