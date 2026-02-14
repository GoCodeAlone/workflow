package webhook

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Handler provides HTTP endpoints for the dead letter dashboard.
type Handler struct {
	store   *DeadLetterStore
	manager *RetryManager
}

// NewHandler creates a new webhook HTTP handler.
func NewHandler(store *DeadLetterStore, manager *RetryManager) *Handler {
	return &Handler{store: store, manager: manager}
}

// RegisterRoutes registers webhook API routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/webhooks/dead-letter", h.listDeadLetters)
	mux.HandleFunc("GET /api/webhooks/dead-letter/stats", h.deadLetterStats)
	mux.HandleFunc("POST /api/webhooks/dead-letter/{id}/retry", h.retryDeadLetter)
	mux.HandleFunc("DELETE /api/webhooks/dead-letter/{id}", h.deleteDeadLetter)
	mux.HandleFunc("DELETE /api/webhooks/dead-letter", h.purgeDeadLetters)
}

func (h *Handler) listDeadLetters(w http.ResponseWriter, r *http.Request) {
	entries := h.store.List()
	writeJSON(w, http.StatusOK, map[string]any{
		"items": entries,
		"total": len(entries),
	})
}

func (h *Handler) deadLetterStats(w http.ResponseWriter, r *http.Request) {
	stats := h.store.Stats()
	writeJSON(w, http.StatusOK, stats)
}

func (h *Handler) retryDeadLetter(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		// Fallback for older Go versions or test routers
		id = extractLastPathSegment(r.URL.Path, "/retry")
	}
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing id"})
		return
	}

	delivery, err := h.manager.Replay(r.Context(), id)
	if err != nil {
		if delivery != nil {
			// Retry attempted but failed again
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error":    err.Error(),
				"delivery": delivery,
			})
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, delivery)
}

func (h *Handler) deleteDeadLetter(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		id = extractPathID(r.URL.Path, "/api/webhooks/dead-letter/")
	}
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing id"})
		return
	}

	if _, ok := h.store.Remove(id); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) purgeDeadLetters(w http.ResponseWriter, r *http.Request) {
	n := h.store.Purge()
	writeJSON(w, http.StatusOK, map[string]any{"purged": n})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func extractLastPathSegment(path, suffix string) string {
	path = strings.TrimSuffix(path, suffix)
	parts := strings.Split(strings.TrimRight(path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func extractPathID(path, prefix string) string {
	rest := strings.TrimPrefix(path, prefix)
	rest = strings.TrimRight(rest, "/")
	if idx := strings.Index(rest, "/"); idx >= 0 {
		return rest[:idx]
	}
	return rest
}
