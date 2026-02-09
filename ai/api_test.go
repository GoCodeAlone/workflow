package ai

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func setupTestHandler() *Handler {
	svc := NewService()
	mock := &MockGenerator{}
	svc.RegisterGenerator(ProviderAnthropic, mock)
	return NewHandler(svc)
}

func TestHandleGenerate(t *testing.T) {
	h := setupTestHandler()

	t.Run("valid request", func(t *testing.T) {
		body, _ := json.Marshal(GenerateRequest{Intent: "Create a REST API"})
		req := httptest.NewRequest(http.MethodPost, "/api/ai/generate", bytes.NewReader(body))
		w := httptest.NewRecorder()

		h.HandleGenerate(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp GenerateResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if resp.Workflow == nil {
			t.Error("expected workflow in response")
		}
	})

	t.Run("empty intent", func(t *testing.T) {
		body, _ := json.Marshal(GenerateRequest{Intent: ""})
		req := httptest.NewRequest(http.MethodPost, "/api/ai/generate", bytes.NewReader(body))
		w := httptest.NewRecorder()

		h.HandleGenerate(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("invalid body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/ai/generate", bytes.NewReader([]byte("invalid")))
		w := httptest.NewRecorder()

		h.HandleGenerate(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}

func TestHandleComponent(t *testing.T) {
	h := setupTestHandler()

	t.Run("valid request", func(t *testing.T) {
		body, _ := json.Marshal(ComponentSpec{
			Name:      "test",
			Interface: "modular.Module",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/ai/component", bytes.NewReader(body))
		w := httptest.NewRecorder()

		h.HandleComponent(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		body, _ := json.Marshal(ComponentSpec{Name: "test"})
		req := httptest.NewRequest(http.MethodPost, "/api/ai/component", bytes.NewReader(body))
		w := httptest.NewRecorder()

		h.HandleComponent(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}

func TestHandleSuggest(t *testing.T) {
	h := setupTestHandler()

	t.Run("valid request", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"useCase": "Monitor uptime"})
		req := httptest.NewRequest(http.MethodPost, "/api/ai/suggest", bytes.NewReader(body))
		w := httptest.NewRecorder()

		h.HandleSuggest(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var suggestions []WorkflowSuggestion
		if err := json.Unmarshal(w.Body.Bytes(), &suggestions); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if len(suggestions) == 0 {
			t.Error("expected at least one suggestion")
		}
	})

	t.Run("empty use case", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"useCase": ""})
		req := httptest.NewRequest(http.MethodPost, "/api/ai/suggest", bytes.NewReader(body))
		w := httptest.NewRecorder()

		h.HandleSuggest(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}

func TestHandleProviders(t *testing.T) {
	h := setupTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/ai/providers", nil)
	w := httptest.NewRecorder()

	h.HandleProviders(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if _, ok := resp["providers"]; !ok {
		t.Error("response missing providers field")
	}
}
