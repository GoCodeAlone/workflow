package sentiment

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Handler provides HTTP handlers for the sentiment API.
type Handler struct {
	analyzer *SentimentAnalyzer
	detector *TrendDetector
}

// NewHandler creates a new sentiment API handler.
func NewHandler(analyzer *SentimentAnalyzer, detector *TrendDetector) *Handler {
	return &Handler{
		analyzer: analyzer,
		detector: detector,
	}
}

// RegisterRoutes registers the sentiment API routes on a ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/conversations/{id}/sentiment", h.HandleGetSentiment)
	mux.HandleFunc("POST /api/conversations/{id}/sentiment", h.HandleAnalyzeSentiment)
}

type sentimentRequest struct {
	Messages []messageInput `json:"messages"`
}

type messageInput struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	Role      string `json:"role"`
	Timestamp string `json:"timestamp,omitempty"`
}

// HandleGetSentiment handles GET /api/conversations/{id}/sentiment.
// Returns the stored trend for a conversation.
func (h *Handler) HandleGetSentiment(w http.ResponseWriter, r *http.Request) {
	conversationID := r.PathValue("id")
	if conversationID == "" {
		writeError(w, http.StatusBadRequest, "conversation id is required")
		return
	}

	trend, ok := h.detector.GetTrend(conversationID)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"conversationId": conversationID,
			"message":        "no sentiment data available, POST messages to analyze",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"conversationId": conversationID,
		"trend":          trend,
	})
}

// HandleAnalyzeSentiment handles POST /api/conversations/{id}/sentiment.
// Analyzes messages and returns sentiment timeline with trend.
func (h *Handler) HandleAnalyzeSentiment(w http.ResponseWriter, r *http.Request) {
	conversationID := r.PathValue("id")
	if conversationID == "" {
		writeError(w, http.StatusBadRequest, "conversation id is required")
		return
	}

	var req sentimentRequest
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

	trend, err := h.detector.TrackConversation(r.Context(), conversationID, messages)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sentiment analysis failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"conversationId": conversationID,
		"trend":          trend,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
