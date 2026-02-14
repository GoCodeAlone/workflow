package suggestions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockProvider implements LLMProvider for tests.
type mockProvider struct {
	suggestions []Suggestion
	err         error
}

func (m *mockProvider) GenerateSuggestions(_ context.Context, _, _ string) ([]Suggestion, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.suggestions, nil
}

func TestNewSuggestionEngine(t *testing.T) {
	engine := NewSuggestionEngine(nil, DefaultConfig())
	if engine == nil {
		t.Fatal("expected non-nil engine")
	}
	if len(engine.templates) == 0 {
		t.Error("expected default templates to be loaded")
	}
}

func TestGetSuggestions_TemplatesFallback(t *testing.T) {
	engine := NewSuggestionEngine(nil, DefaultConfig())

	messages := []Message{
		{ID: "1", Body: "I feel so sad and hopeless", Role: "texter", Timestamp: time.Now()},
	}

	suggestions, err := engine.GetSuggestions(context.Background(), "conv-1", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suggestions) == 0 {
		t.Fatal("expected at least one suggestion")
	}

	// Should match empathy category for "sad"/"hopeless"
	found := false
	for _, s := range suggestions {
		if s.Category == "empathy" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected empathy category suggestion for sad/hopeless message")
	}
}

func TestGetSuggestions_SafetyPatterns(t *testing.T) {
	engine := NewSuggestionEngine(nil, DefaultConfig())

	messages := []Message{
		{ID: "1", Body: "I want to kill myself", Role: "texter", Timestamp: time.Now()},
	}

	suggestions, err := engine.GetSuggestions(context.Background(), "conv-2", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, s := range suggestions {
		if s.Category == "safety" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected safety category suggestion for suicidal content")
	}
}

func TestGetSuggestions_LLMProvider(t *testing.T) {
	provider := &mockProvider{
		suggestions: []Suggestion{
			{Text: "LLM suggestion", Category: "empathy", Confidence: 0.9},
		},
	}
	engine := NewSuggestionEngine(provider, DefaultConfig())

	messages := []Message{
		{ID: "1", Body: "I'm struggling", Role: "texter", Timestamp: time.Now()},
	}

	suggestions, err := engine.GetSuggestions(context.Background(), "conv-3", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suggestions) != 1 || suggestions[0].Text != "LLM suggestion" {
		t.Errorf("expected LLM suggestion, got %v", suggestions)
	}
}

func TestGetSuggestions_LLMFallbackToTemplates(t *testing.T) {
	provider := &mockProvider{
		err: context.DeadlineExceeded,
	}
	engine := NewSuggestionEngine(provider, DefaultConfig())

	messages := []Message{
		{ID: "1", Body: "I feel anxious and scared", Role: "texter", Timestamp: time.Now()},
	}

	suggestions, err := engine.GetSuggestions(context.Background(), "conv-4", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suggestions) == 0 {
		t.Fatal("expected template fallback suggestions")
	}
}

func TestGetSuggestions_Caching(t *testing.T) {
	callCount := 0
	provider := &mockProvider{
		suggestions: []Suggestion{
			{Text: "Cached response", Category: "empathy", Confidence: 0.8},
		},
	}

	engine := NewSuggestionEngine(provider, Config{CacheTTL: 1 * time.Minute})

	messages := []Message{
		{ID: "1", Body: "Hello", Role: "texter", Timestamp: time.Now()},
	}

	// First call
	s1, err := engine.GetSuggestions(context.Background(), "conv-5", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Track if provider is called again by replacing provider
	engine.provider = &mockProvider{
		suggestions: []Suggestion{
			{Text: "Different response", Category: "question", Confidence: 0.7},
		},
	}

	// Second call should return cached result
	s2, err := engine.GetSuggestions(context.Background(), "conv-5", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s1[0].Text != s2[0].Text {
		t.Errorf("expected cached result %q, got %q", s1[0].Text, s2[0].Text)
	}
	_ = callCount
}

func TestGetSuggestions_NoMessages(t *testing.T) {
	engine := NewSuggestionEngine(nil, DefaultConfig())
	_, err := engine.GetSuggestions(context.Background(), "conv-6", nil)
	if err == nil {
		t.Error("expected error for empty messages")
	}
}

func TestInvalidateConversation(t *testing.T) {
	provider := &mockProvider{
		suggestions: []Suggestion{
			{Text: "Original", Category: "empathy", Confidence: 0.8},
		},
	}
	engine := NewSuggestionEngine(provider, Config{CacheTTL: 10 * time.Minute})

	messages := []Message{
		{ID: "1", Body: "test", Role: "texter", Timestamp: time.Now()},
	}

	_, err := engine.GetSuggestions(context.Background(), "conv-7", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Change provider response
	engine.provider = &mockProvider{
		suggestions: []Suggestion{
			{Text: "New response", Category: "question", Confidence: 0.7},
		},
	}

	// Invalidate cache
	engine.InvalidateConversation("conv-7")

	// Should get new response
	s, err := engine.GetSuggestions(context.Background(), "conv-7", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s[0].Text != "New response" {
		t.Errorf("expected new response after cache invalidation, got %q", s[0].Text)
	}
}

func TestClearCache(t *testing.T) {
	engine := NewSuggestionEngine(nil, DefaultConfig())
	engine.cacheMu.Lock()
	engine.cache["test"] = cachedSuggestion{
		suggestions: []Suggestion{{Text: "cached"}},
		expiresAt:   time.Now().Add(time.Hour),
	}
	engine.cacheMu.Unlock()

	engine.ClearCache()

	engine.cacheMu.RLock()
	if len(engine.cache) != 0 {
		t.Error("expected empty cache after ClearCache")
	}
	engine.cacheMu.RUnlock()
}

func TestGetSuggestions_GenericFallback(t *testing.T) {
	engine := NewSuggestionEngine(nil, DefaultConfig())

	messages := []Message{
		{ID: "1", Body: "zzzzz no keywords match here xyzzy", Role: "texter", Timestamp: time.Now()},
	}

	suggestions, err := engine.GetSuggestions(context.Background(), "conv-generic", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suggestions) == 0 {
		t.Fatal("expected at least one generic fallback suggestion")
	}
	if suggestions[0].Category != "empathy" {
		t.Errorf("expected empathy fallback, got %q", suggestions[0].Category)
	}
}

func TestGetSuggestions_CounselorOnlyMessages(t *testing.T) {
	engine := NewSuggestionEngine(nil, DefaultConfig())

	messages := []Message{
		{ID: "1", Body: "How can I help you?", Role: "counselor", Timestamp: time.Now()},
	}

	suggestions, err := engine.GetSuggestions(context.Background(), "conv-counselor", messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return generic suggestion since no texter message
	if len(suggestions) == 0 {
		t.Fatal("expected at least one suggestion")
	}
}

// --- HTTP Handler Tests ---

func TestHandleGetSuggestions_Cached(t *testing.T) {
	engine := NewSuggestionEngine(nil, DefaultConfig())
	engine.cacheMu.Lock()
	engine.cache["conv-http"] = cachedSuggestion{
		suggestions: []Suggestion{{Text: "cached", Category: "empathy", Confidence: 0.8}},
		expiresAt:   time.Now().Add(time.Hour),
	}
	engine.cacheMu.Unlock()

	handler := NewHandler(engine)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/conversations/conv-http/suggestions", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["cached"] != true {
		t.Error("expected cached: true")
	}
}

func TestHandlePostSuggestions(t *testing.T) {
	engine := NewSuggestionEngine(nil, DefaultConfig())
	handler := NewHandler(engine)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{"messages": [{"id":"1","body":"I feel so anxious","role":"texter"}]}`
	req := httptest.NewRequest("POST", "/api/conversations/conv-post/suggestions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	suggestions, ok := resp["suggestions"].([]any)
	if !ok || len(suggestions) == 0 {
		t.Error("expected non-empty suggestions")
	}
}

func TestHandlePostSuggestions_EmptyMessages(t *testing.T) {
	engine := NewSuggestionEngine(nil, DefaultConfig())
	handler := NewHandler(engine)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{"messages": []}`
	req := httptest.NewRequest("POST", "/api/conversations/conv-empty/suggestions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleInvalidateCache(t *testing.T) {
	engine := NewSuggestionEngine(nil, DefaultConfig())
	engine.cacheMu.Lock()
	engine.cache["conv-del"] = cachedSuggestion{
		suggestions: []Suggestion{{Text: "to delete"}},
		expiresAt:   time.Now().Add(time.Hour),
	}
	engine.cacheMu.Unlock()

	handler := NewHandler(engine)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("DELETE", "/api/conversations/conv-del/suggestions/cache", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	engine.cacheMu.RLock()
	_, ok := engine.cache["conv-del"]
	engine.cacheMu.RUnlock()
	if ok {
		t.Error("expected cache entry to be removed")
	}
}

func TestHandleGetSuggestions_NoCached(t *testing.T) {
	engine := NewSuggestionEngine(nil, DefaultConfig())
	handler := NewHandler(engine)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/conversations/conv-miss/suggestions", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["cached"] != false {
		t.Error("expected cached: false")
	}
}
