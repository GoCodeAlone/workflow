package store

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// DLQHandler provides HTTP endpoints for dead letter queue management.
type DLQHandler struct {
	store  DLQStore
	logger *slog.Logger
}

// NewDLQHandler creates a new DLQHandler.
func NewDLQHandler(store DLQStore, logger *slog.Logger) *DLQHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &DLQHandler{
		store:  store,
		logger: logger,
	}
}

// RegisterRoutes registers the DLQ API routes on the given mux.
func (h *DLQHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/dlq", h.handleList)
	mux.HandleFunc("GET /api/v1/admin/dlq/stats", h.handleStats)
	mux.HandleFunc("GET /api/v1/admin/dlq/{id}", h.handleGet)
	mux.HandleFunc("POST /api/v1/admin/dlq/{id}/retry", h.handleRetry)
	mux.HandleFunc("POST /api/v1/admin/dlq/{id}/discard", h.handleDiscard)
	mux.HandleFunc("POST /api/v1/admin/dlq/{id}/resolve", h.handleResolve)
	mux.HandleFunc("DELETE /api/v1/admin/dlq/purge", h.handlePurge)
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (h *DLQHandler) handleList(w http.ResponseWriter, r *http.Request) {
	filter := DLQFilter{
		PipelineName: r.URL.Query().Get("pipeline"),
		StepName:     r.URL.Query().Get("step"),
		ErrorType:    r.URL.Query().Get("error_type"),
	}

	if s := r.URL.Query().Get("status"); s != "" {
		filter.Status = DLQStatus(s)
	}

	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			filter.Limit = n
		}
	}
	if filter.Limit == 0 {
		filter.Limit = 50
	}

	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			filter.Offset = n
		}
	}

	entries, err := h.store.List(r.Context(), filter)
	if err != nil {
		h.logger.Error("list dlq entries", "error", err)
		writeDLQError(w, http.StatusInternalServerError, "internal error")
		return
	}

	total, err := h.store.Count(r.Context(), filter)
	if err != nil {
		h.logger.Error("count dlq entries", "error", err)
		writeDLQError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeDLQJSON(w, http.StatusOK, map[string]any{
		"entries": entries,
		"total":   total,
		"limit":   filter.Limit,
		"offset":  filter.Offset,
	})
}

func (h *DLQHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeDLQError(w, http.StatusBadRequest, "invalid id")
		return
	}

	entry, err := h.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeDLQError(w, http.StatusNotFound, "entry not found")
			return
		}
		h.logger.Error("get dlq entry", "error", err)
		writeDLQError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeDLQJSON(w, http.StatusOK, entry)
}

func (h *DLQHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	statuses := []DLQStatus{DLQStatusPending, DLQStatusRetrying, DLQStatusResolved, DLQStatusDiscarded}
	byStatus := make(map[string]int64)

	for _, status := range statuses {
		count, err := h.store.Count(ctx, DLQFilter{Status: status})
		if err != nil {
			h.logger.Error("count dlq by status", "status", status, "error", err)
			writeDLQError(w, http.StatusInternalServerError, "internal error")
			return
		}
		byStatus[string(status)] = count
	}

	// Get total count.
	total, err := h.store.Count(ctx, DLQFilter{})
	if err != nil {
		h.logger.Error("count dlq total", "error", err)
		writeDLQError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeDLQJSON(w, http.StatusOK, map[string]any{
		"total":     total,
		"by_status": byStatus,
	})
}

func (h *DLQHandler) handleRetry(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeDLQError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := h.store.Retry(r.Context(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeDLQError(w, http.StatusNotFound, "entry not found")
			return
		}
		h.logger.Error("retry dlq entry", "error", err)
		writeDLQError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeDLQJSON(w, http.StatusOK, map[string]any{"status": "retrying"})
}

func (h *DLQHandler) handleDiscard(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeDLQError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := h.store.Discard(r.Context(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeDLQError(w, http.StatusNotFound, "entry not found")
			return
		}
		h.logger.Error("discard dlq entry", "error", err)
		writeDLQError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeDLQJSON(w, http.StatusOK, map[string]any{"status": "discarded"})
}

func (h *DLQHandler) handleResolve(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeDLQError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := h.store.Resolve(r.Context(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeDLQError(w, http.StatusNotFound, "entry not found")
			return
		}
		h.logger.Error("resolve dlq entry", "error", err)
		writeDLQError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeDLQJSON(w, http.StatusOK, map[string]any{"status": "resolved"})
}

func (h *DLQHandler) handlePurge(w http.ResponseWriter, r *http.Request) {
	hours := 720 // default: 30 days
	if v := r.URL.Query().Get("older_than_hours"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			hours = n
		}
	}

	dur := time.Duration(hours) * time.Hour
	count, err := h.store.Purge(r.Context(), dur)
	if err != nil {
		h.logger.Error("purge dlq entries", "error", err)
		writeDLQError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeDLQJSON(w, http.StatusOK, map[string]any{
		"purged":           count,
		"older_than_hours": hours,
	})
}

// ---------------------------------------------------------------------------
// Response helpers
// ---------------------------------------------------------------------------

func writeDLQJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeDLQError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
