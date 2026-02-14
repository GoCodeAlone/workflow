package sentiment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type mockProvider struct {
	result *Analysis
	err    error
}

func (m *mockProvider) AnalyzeSentiment(_ context.Context, _, _ string) (*Analysis, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func TestScoreLabel(t *testing.T) {
	tests := []struct {
		score Score
		want  string
	}{
		{-1.0, "very_negative"},
		{-0.7, "very_negative"},
		{-0.5, "negative"},
		{-0.1, "neutral"},
		{0.0, "neutral"},
		{0.1, "neutral"},
		{0.3, "positive"},
		{0.5, "positive"},
		{0.8, "very_positive"},
		{1.0, "very_positive"},
	}

	for _, tt := range tests {
		got := tt.score.Label()
		if got != tt.want {
			t.Errorf("Score(%f).Label() = %q, want %q", tt.score, got, tt.want)
		}
	}
}

func TestSentimentAnalyzer_Negative(t *testing.T) {
	analyzer := NewSentimentAnalyzer(nil)

	result, err := analyzer.Analyze(context.Background(), "I feel so sad and hopeless")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score >= 0 {
		t.Errorf("expected negative score, got %f", result.Score)
	}
	if result.Label != "negative" && result.Label != "very_negative" {
		t.Errorf("expected negative label, got %q", result.Label)
	}
}

func TestSentimentAnalyzer_Positive(t *testing.T) {
	analyzer := NewSentimentAnalyzer(nil)

	result, err := analyzer.Analyze(context.Background(), "I feel great and happy today, things are getting better")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score <= 0 {
		t.Errorf("expected positive score, got %f", result.Score)
	}
}

func TestSentimentAnalyzer_Neutral(t *testing.T) {
	analyzer := NewSentimentAnalyzer(nil)

	result, err := analyzer.Analyze(context.Background(), "the weather is cloudy today and there are some trees")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score < -0.3 || result.Score > 0.3 {
		t.Errorf("expected neutral-ish score, got %f", result.Score)
	}
}

func TestSentimentAnalyzer_Negation(t *testing.T) {
	analyzer := NewSentimentAnalyzer(nil)

	result, err := analyzer.Analyze(context.Background(), "I'm not happy at all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "not happy" should be negative
	if result.Score > 0 {
		t.Errorf("expected negative score for negated positive, got %f", result.Score)
	}
}

func TestSentimentAnalyzer_Intensifier(t *testing.T) {
	analyzer := NewSentimentAnalyzer(nil)

	r1, err := analyzer.Analyze(context.Background(), "sad")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r2, err := analyzer.Analyze(context.Background(), "very sad")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// "very sad" should be more negative than just "sad"
	if r2.Score >= r1.Score {
		t.Errorf("expected intensified score to be more negative: %f vs %f", r2.Score, r1.Score)
	}
}

func TestSentimentAnalyzer_EmptyText(t *testing.T) {
	analyzer := NewSentimentAnalyzer(nil)
	_, err := analyzer.Analyze(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty text")
	}
}

func TestSentimentAnalyzer_LLMProvider(t *testing.T) {
	provider := &mockProvider{
		result: &Analysis{
			Score:      -0.8,
			Label:      "very_negative",
			Confidence: 0.95,
		},
	}
	analyzer := NewSentimentAnalyzer(provider)

	result, err := analyzer.Analyze(context.Background(), "test text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score != -0.8 {
		t.Errorf("expected LLM score -0.8, got %f", result.Score)
	}
}

func TestSentimentAnalyzer_LLMFallback(t *testing.T) {
	provider := &mockProvider{
		err: context.DeadlineExceeded,
	}
	analyzer := NewSentimentAnalyzer(provider)

	result, err := analyzer.Analyze(context.Background(), "I feel sad and hopeless")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score >= 0 {
		t.Errorf("expected negative score from lexicon fallback, got %f", result.Score)
	}
}

func TestSentimentAnalyzer_AnalyzeMessages(t *testing.T) {
	analyzer := NewSentimentAnalyzer(nil)

	messages := []Message{
		{ID: "1", Body: "I'm feeling terrible", Role: "texter", Timestamp: time.Now()},
		{ID: "2", Body: "Tell me more", Role: "counselor", Timestamp: time.Now()},
		{ID: "3", Body: "Things are getting better", Role: "texter", Timestamp: time.Now()},
	}

	results, err := analyzer.AnalyzeMessages(context.Background(), messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only texter messages should be analyzed
	if len(results) != 2 {
		t.Errorf("expected 2 results (texter only), got %d", len(results))
	}
}

func TestSentimentAnalyzer_AnalyzeMessages_Empty(t *testing.T) {
	analyzer := NewSentimentAnalyzer(nil)
	_, err := analyzer.AnalyzeMessages(context.Background(), nil)
	if err == nil {
		t.Error("expected error for empty messages")
	}
}

// --- TrendDetector Tests ---

func TestTrendDetector_TrackConversation(t *testing.T) {
	analyzer := NewSentimentAnalyzer(nil)
	detector := NewTrendDetector(analyzer, DefaultTrendConfig())

	messages := []Message{
		{ID: "1", Body: "I'm feeling really good today", Role: "texter", Timestamp: time.Now().Add(-3 * time.Minute)},
		{ID: "2", Body: "Thanks", Role: "counselor", Timestamp: time.Now().Add(-2 * time.Minute)},
		{ID: "3", Body: "Things are getting better and I'm happy", Role: "texter", Timestamp: time.Now().Add(-1 * time.Minute)},
	}

	trend, err := detector.TrackConversation(context.Background(), "conv-1", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if trend.ConversationID != "conv-1" {
		t.Errorf("expected conv-1, got %s", trend.ConversationID)
	}
	if len(trend.Points) != 2 {
		t.Errorf("expected 2 points (texter only), got %d", len(trend.Points))
	}
}

func TestTrendDetector_SharpDrop(t *testing.T) {
	analyzer := NewSentimentAnalyzer(nil)

	var alertFired bool
	var alertConvID string
	detector := NewTrendDetector(analyzer, TrendConfig{
		SharpDropThreshold: 0.4,
		AlertCallback: func(conversationID string, trend *Trend) {
			alertFired = true
			alertConvID = conversationID
		},
	})

	messages := []Message{
		{ID: "1", Body: "I'm feeling really happy and great and wonderful", Role: "texter", Timestamp: time.Now().Add(-2 * time.Minute)},
		{ID: "2", Body: "I want to die everything is hopeless and terrible and awful", Role: "texter", Timestamp: time.Now().Add(-1 * time.Minute)},
	}

	trend, err := detector.TrackConversation(context.Background(), "conv-drop", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !trend.SharpDrop {
		t.Error("expected sharp drop to be detected")
	}
	if !alertFired {
		t.Error("expected alert callback to fire")
	}
	if alertConvID != "conv-drop" {
		t.Errorf("expected conv-drop in alert, got %s", alertConvID)
	}
}

func TestTrendDetector_Direction(t *testing.T) {
	analyzer := NewSentimentAnalyzer(nil)
	detector := NewTrendDetector(analyzer, DefaultTrendConfig())

	// Improving trend
	messages := []Message{
		{ID: "1", Body: "I feel terrible and hopeless", Role: "texter", Timestamp: time.Now().Add(-3 * time.Minute)},
		{ID: "2", Body: "I'm feeling okay now", Role: "texter", Timestamp: time.Now().Add(-2 * time.Minute)},
		{ID: "3", Body: "I'm feeling great and happy", Role: "texter", Timestamp: time.Now().Add(-1 * time.Minute)},
	}

	trend, err := detector.TrackConversation(context.Background(), "conv-improving", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if trend.Direction != "improving" {
		t.Errorf("expected improving direction, got %s", trend.Direction)
	}
}

func TestTrendDetector_GetTrend(t *testing.T) {
	analyzer := NewSentimentAnalyzer(nil)
	detector := NewTrendDetector(analyzer, DefaultTrendConfig())

	_, ok := detector.GetTrend("nonexistent")
	if ok {
		t.Error("expected false for nonexistent conversation")
	}

	messages := []Message{
		{ID: "1", Body: "test message", Role: "texter", Timestamp: time.Now()},
	}
	_, err := detector.TrackConversation(context.Background(), "conv-get", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	trend, ok := detector.GetTrend("conv-get")
	if !ok {
		t.Error("expected true for tracked conversation")
	}
	if trend.ConversationID != "conv-get" {
		t.Errorf("expected conv-get, got %s", trend.ConversationID)
	}
}

func TestTrendDetector_EmptyMessages(t *testing.T) {
	analyzer := NewSentimentAnalyzer(nil)
	detector := NewTrendDetector(analyzer, DefaultTrendConfig())

	_, err := detector.TrackConversation(context.Background(), "conv-empty", nil)
	if err == nil {
		t.Error("expected error for empty messages")
	}
}

func TestTrendDetector_OnlyCounselorMessages(t *testing.T) {
	analyzer := NewSentimentAnalyzer(nil)
	detector := NewTrendDetector(analyzer, DefaultTrendConfig())

	messages := []Message{
		{ID: "1", Body: "How are you?", Role: "counselor", Timestamp: time.Now()},
	}

	trend, err := detector.TrackConversation(context.Background(), "conv-counselor", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if trend.Direction != "stable" {
		t.Errorf("expected stable for no texter messages, got %s", trend.Direction)
	}
}

// --- HTTP Handler Tests ---

func TestHandleGetSentiment_NoData(t *testing.T) {
	analyzer := NewSentimentAnalyzer(nil)
	detector := NewTrendDetector(analyzer, DefaultTrendConfig())
	handler := NewHandler(analyzer, detector)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/conversations/conv-miss/sentiment", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if _, ok := resp["message"]; !ok {
		t.Error("expected message field for missing data")
	}
}

func TestHandlePostSentiment(t *testing.T) {
	analyzer := NewSentimentAnalyzer(nil)
	detector := NewTrendDetector(analyzer, DefaultTrendConfig())
	handler := NewHandler(analyzer, detector)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{"messages":[
		{"id":"1","body":"I feel terrible","role":"texter","timestamp":"2024-01-01T12:00:00Z"},
		{"id":"2","body":"Things are better now","role":"texter","timestamp":"2024-01-01T12:05:00Z"}
	]}`
	req := httptest.NewRequest("POST", "/api/conversations/conv-post/sentiment", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp["conversationId"] != "conv-post" {
		t.Errorf("expected conv-post, got %v", resp["conversationId"])
	}
	if resp["trend"] == nil {
		t.Error("expected trend in response")
	}
}

func TestHandlePostSentiment_EmptyMessages(t *testing.T) {
	analyzer := NewSentimentAnalyzer(nil)
	detector := NewTrendDetector(analyzer, DefaultTrendConfig())
	handler := NewHandler(analyzer, detector)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{"messages":[]}`
	req := httptest.NewRequest("POST", "/api/conversations/conv-empty/sentiment", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleGetSentiment_WithData(t *testing.T) {
	analyzer := NewSentimentAnalyzer(nil)
	detector := NewTrendDetector(analyzer, DefaultTrendConfig())

	messages := []Message{
		{ID: "1", Body: "I feel sad", Role: "texter", Timestamp: time.Now()},
	}
	_, _ = detector.TrackConversation(context.Background(), "conv-data", messages)

	handler := NewHandler(analyzer, detector)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/conversations/conv-data/sentiment", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp["trend"] == nil {
		t.Error("expected trend in response")
	}
}
