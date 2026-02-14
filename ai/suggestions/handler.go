package suggestions

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Handler provides HTTP handlers for the suggestions API.
type Handler struct {
	engine *SuggestionEngine
}

// NewHandler creates a new suggestions API handler.
func NewHandler(engine *SuggestionEngine) *Handler {
	return &Handler{engine: engine}
}

// RegisterRoutes registers the suggestions API routes on a ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/conversations/{id}/suggestions", h.HandleGetSuggestions)
	mux.HandleFunc("POST /api/conversations/{id}/suggestions", h.HandleGetSuggestionsWithMessages)
	mux.HandleFunc("DELETE /api/conversations/{id}/suggestions/cache", h.HandleInvalidateCache)
}

// suggestionsRequest is the body for POST suggestions with explicit messages.
type suggestionsRequest struct {
	Messages []messageInput `json:"messages"`
}

type messageInput struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	Role      string `json:"role"`
	Timestamp string `json:"timestamp,omitempty"`
}

// HandleGetSuggestions handles GET /api/conversations/{id}/suggestions.
// Returns cached suggestions or template-based fallback.
func (h *Handler) HandleGetSuggestions(w http.ResponseWriter, r *http.Request) {
	conversationID := r.PathValue("id")
	if conversationID == "" {
		writeError(w, http.StatusBadRequest, "conversation id is required")
		return
	}

	// For GET, return cached suggestions or a prompt to POST messages
	h.engine.cacheMu.RLock()
	cached, ok := h.engine.cache[conversationID]
	h.engine.cacheMu.RUnlock()

	if ok && time.Now().Before(cached.expiresAt) {
		writeJSON(w, http.StatusOK, map[string]any{
			"conversationId": conversationID,
			"suggestions":    cached.suggestions,
			"cached":         true,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"conversationId": conversationID,
		"suggestions":    []Suggestion{},
		"cached":         false,
		"message":        "POST messages to generate suggestions",
	})
}

// HandleGetSuggestionsWithMessages handles POST /api/conversations/{id}/suggestions.
// Generates fresh suggestions from the provided messages.
func (h *Handler) HandleGetSuggestionsWithMessages(w http.ResponseWriter, r *http.Request) {
	conversationID := r.PathValue("id")
	if conversationID == "" {
		writeError(w, http.StatusBadRequest, "conversation id is required")
		return
	}

	var req suggestionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages are required")
		return
	}

	messages := make([]Message, len(req.Messages))
	for i, m := range req.Messages {
		ts := time.Now()
		if m.Timestamp != "" {
			if parsed, err := time.Parse(time.RFC3339, m.Timestamp); err == nil {
				ts = parsed
			}
		}
		messages[i] = Message{
			ID:        m.ID,
			Body:      m.Body,
			Role:      strings.ToLower(m.Role),
			Timestamp: ts,
		}
	}

	suggestions, err := h.engine.GetSuggestions(r.Context(), conversationID, messages)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate suggestions: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"conversationId": conversationID,
		"suggestions":    suggestions,
	})
}

// HandleInvalidateCache handles DELETE /api/conversations/{id}/suggestions/cache.
func (h *Handler) HandleInvalidateCache(w http.ResponseWriter, r *http.Request) {
	conversationID := r.PathValue("id")
	if conversationID == "" {
		writeError(w, http.StatusBadRequest, "conversation id is required")
		return
	}

	h.engine.InvalidateConversation(conversationID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "cache invalidated"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
