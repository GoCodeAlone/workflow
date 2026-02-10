package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GoCodeAlone/workflow/ai"
	"github.com/GoCodeAlone/workflow/config"
)

func TestNewClient_NoAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, err := NewClient(ClientConfig{})
	if err == nil {
		t.Error("expected error for missing API key")
	}
}

func TestNewClient_ExplicitAPIKey(t *testing.T) {
	client, err := NewClient(ClientConfig{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if client.apiKey != "test-key" {
		t.Errorf("expected apiKey 'test-key', got '%s'", client.apiKey)
	}
}

func TestNewClient_EnvAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "env-key")
	client, err := NewClient(ClientConfig{})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if client.apiKey != "env-key" {
		t.Errorf("expected apiKey 'env-key', got '%s'", client.apiKey)
	}
}

func TestNewClient_Defaults(t *testing.T) {
	client, err := NewClient(ClientConfig{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if client.model != defaultModel {
		t.Errorf("expected model '%s', got '%s'", defaultModel, client.model)
	}
	if client.baseURL != defaultBaseURL {
		t.Errorf("expected baseURL '%s', got '%s'", defaultBaseURL, client.baseURL)
	}
}

func TestNewClient_CustomConfig(t *testing.T) {
	client, err := NewClient(ClientConfig{
		APIKey:  "key",
		Model:   "custom-model",
		BaseURL: "https://custom.api.com",
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if client.model != "custom-model" {
		t.Errorf("expected model 'custom-model', got '%s'", client.model)
	}
	if client.baseURL != "https://custom.api.com" {
		t.Errorf("expected baseURL 'https://custom.api.com', got '%s'", client.baseURL)
	}
}

func TestClient_Call_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/messages" {
			t.Errorf("expected path '/v1/messages', got '%s'", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("expected x-api-key 'test-key', got '%s'", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != apiVersion {
			t.Errorf("expected version '%s', got '%s'", apiVersion, r.Header.Get("anthropic-version"))
		}

		resp := apiResponse{
			ID: "msg_123",
			Content: []contentBlock{
				{Type: "text", Text: "Hello!"},
			},
			StopReason: "end_turn",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", BaseURL: server.URL})

	userMsg, _ := json.Marshal("Hello")
	resp, err := client.call(context.Background(), "system prompt", []message{
		{Role: "user", Content: userMsg},
	}, nil)

	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if resp.ID != "msg_123" {
		t.Errorf("expected ID 'msg_123', got '%s'", resp.ID)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "Hello!" {
		t.Errorf("unexpected content: %v", resp.Content)
	}
}

func TestClient_Call_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"error": {"message": "bad request"}}`)
	}))
	defer server.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", BaseURL: server.URL})

	userMsg, _ := json.Marshal("test")
	_, err := client.call(context.Background(), "", []message{
		{Role: "user", Content: userMsg},
	}, nil)

	if err == nil {
		t.Error("expected error for API error response")
	}
}

func TestClient_Call_InvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, "not json")
	}))
	defer server.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", BaseURL: server.URL})

	userMsg, _ := json.Marshal("test")
	_, err := client.call(context.Background(), "", []message{
		{Role: "user", Content: userMsg},
	}, nil)

	if err == nil {
		t.Error("expected error for invalid JSON response")
	}
}

func TestClient_GenerateWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := apiResponse{
			ID: "msg_gen",
			Content: []contentBlock{
				{Type: "text", Text: `{"workflow": {"modules": [{"name": "server", "type": "http.server"}]}, "explanation": "A simple server"}`},
			},
			StopReason: "end_turn",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", BaseURL: server.URL})

	resp, err := client.GenerateWorkflow(context.Background(), ai.GenerateRequest{
		Intent: "Create a simple HTTP server",
	})
	if err != nil {
		t.Fatalf("GenerateWorkflow failed: %v", err)
	}
	if resp.Workflow == nil {
		t.Fatal("expected non-nil workflow")
	}
	if len(resp.Workflow.Modules) != 1 {
		t.Errorf("expected 1 module, got %d", len(resp.Workflow.Modules))
	}
	if resp.Explanation != "A simple server" {
		t.Errorf("expected explanation 'A simple server', got '%s'", resp.Explanation)
	}
}

func TestClient_GenerateComponent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := apiResponse{
			ID: "msg_comp",
			Content: []contentBlock{
				{Type: "text", Text: "```go\npackage main\n\nfunc main() {}\n```"},
			},
			StopReason: "end_turn",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", BaseURL: server.URL})

	code, err := client.GenerateComponent(context.Background(), ai.ComponentSpec{
		Name: "MyHandler",
		Type: "http.handler",
	})
	if err != nil {
		t.Fatalf("GenerateComponent failed: %v", err)
	}
	if code == "" {
		t.Error("expected non-empty code")
	}
	if code != "package main\n\nfunc main() {}" {
		t.Errorf("unexpected code: %s", code)
	}
}

func TestClient_SuggestWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := apiResponse{
			ID: "msg_sug",
			Content: []contentBlock{
				{Type: "text", Text: `[{"name": "API Gateway", "description": "Simple API gateway"}]`},
			},
			StopReason: "end_turn",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", BaseURL: server.URL})

	suggestions, err := client.SuggestWorkflow(context.Background(), "web API")
	if err != nil {
		t.Fatalf("SuggestWorkflow failed: %v", err)
	}
	if len(suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(suggestions))
	}
	if suggestions[0].Name != "API Gateway" {
		t.Errorf("expected name 'API Gateway', got '%s'", suggestions[0].Name)
	}
}

func TestClient_IdentifyMissingComponents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := apiResponse{
			ID: "msg_miss",
			Content: []contentBlock{
				{Type: "text", Text: `[{"name": "custom.handler", "type": "custom"}]`},
			},
			StopReason: "end_turn",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", BaseURL: server.URL})

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "custom", Type: "custom.handler"},
		},
	}

	specs, err := client.IdentifyMissingComponents(context.Background(), cfg)
	if err != nil {
		t.Fatalf("IdentifyMissingComponents failed: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
}

func TestClient_IdentifyMissingComponents_NoMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := apiResponse{
			ID: "msg_none",
			Content: []contentBlock{
				{Type: "text", Text: "No missing components found."},
			},
			StopReason: "end_turn",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", BaseURL: server.URL})

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "server", Type: "http.server"},
		},
	}

	specs, err := client.IdentifyMissingComponents(context.Background(), cfg)
	if err != nil {
		t.Fatalf("IdentifyMissingComponents failed: %v", err)
	}
	if specs != nil {
		t.Errorf("expected nil specs, got %v", specs)
	}
}

func TestClient_CallWithToolLoop_MaxRounds(t *testing.T) {
	round := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		round++
		// Always respond with tool_use to trigger loop
		resp := apiResponse{
			ID: "msg_loop",
			Content: []contentBlock{
				{
					Type:  "tool_use",
					ID:    fmt.Sprintf("tool_%d", round),
					Name:  "list_components",
					Input: json.RawMessage(`{}`),
				},
			},
			StopReason: "tool_use",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", BaseURL: server.URL})

	_, err := client.callWithToolLoop(context.Background(), "system", "test prompt")
	if err == nil {
		t.Error("expected error for max rounds exceeded")
	}
}

func TestParseGenerateResponse(t *testing.T) {
	text := `{"workflow": {"modules": [{"name": "s", "type": "http.server"}]}, "explanation": "test"}`
	resp, err := parseGenerateResponse(text)
	if err != nil {
		t.Fatalf("parseGenerateResponse failed: %v", err)
	}
	if resp.Workflow == nil {
		t.Fatal("expected non-nil workflow")
	}
	if resp.Explanation != "test" {
		t.Errorf("expected explanation 'test', got '%s'", resp.Explanation)
	}
}

func TestParseGenerateResponse_NoJSON(t *testing.T) {
	_, err := parseGenerateResponse("no json here")
	if err == nil {
		t.Error("expected error for no JSON")
	}
}

func TestParseSuggestions(t *testing.T) {
	text := `[{"name": "Test", "description": "A test suggestion"}]`
	suggestions, err := parseSuggestions(text)
	if err != nil {
		t.Fatalf("parseSuggestions failed: %v", err)
	}
	if len(suggestions) != 1 {
		t.Errorf("expected 1 suggestion, got %d", len(suggestions))
	}
}

func TestParseSuggestions_NoJSON(t *testing.T) {
	_, err := parseSuggestions("no json")
	if err == nil {
		t.Error("expected error for no JSON")
	}
}

func TestParseMissingComponents(t *testing.T) {
	text := `[{"name": "custom", "type": "custom.handler"}]`
	specs, err := parseMissingComponents(text)
	if err != nil {
		t.Fatalf("parseMissingComponents failed: %v", err)
	}
	if len(specs) != 1 {
		t.Errorf("expected 1 spec, got %d", len(specs))
	}
}

func TestParseMissingComponents_NoJSON(t *testing.T) {
	specs, err := parseMissingComponents("no json here")
	if err != nil {
		t.Error("expected no error for no JSON (means no missing components)")
	}
	if specs != nil {
		t.Errorf("expected nil specs, got %v", specs)
	}
}

func TestParseGenerateResponse_YAMLWorkflow(t *testing.T) {
	// When the LLM returns a YAML string as the workflow value instead of a JSON object.
	yamlCfg := "modules:\n  - name: srv\n    type: http.server"
	raw, _ := json.Marshal(yamlCfg) // produces a JSON string
	text := fmt.Sprintf(`{"workflow": %s, "explanation": "yaml variant"}`, string(raw))

	resp, err := parseGenerateResponse(text)
	if err != nil {
		t.Fatalf("parseGenerateResponse: %v", err)
	}
	if resp.Workflow == nil {
		t.Fatal("expected non-nil workflow from YAML string")
	}
	if len(resp.Workflow.Modules) != 1 {
		t.Errorf("expected 1 module, got %d", len(resp.Workflow.Modules))
	}
	if resp.Workflow.Modules[0].Type != "http.server" {
		t.Errorf("expected type http.server, got %s", resp.Workflow.Modules[0].Type)
	}
	if resp.Explanation != "yaml variant" {
		t.Errorf("expected explanation 'yaml variant', got %q", resp.Explanation)
	}
}

func TestExtractJSON_NoMatch(t *testing.T) {
	result := ExtractJSON("no json here at all")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestExtractJSON_CodeBlockNonJSON(t *testing.T) {
	// A code block that doesn't start with { or [ should not be returned
	text := "```\nsome plain text\n```"
	result := ExtractJSON(text)
	if result != "" {
		t.Errorf("expected empty string for non-JSON code block, got %q", result)
	}
}

func TestExtractCode_MultipleBlocks(t *testing.T) {
	// Should extract the first go block
	text := "Here is code:\n```go\npackage main\n```\nAnd more:\n```go\npackage other\n```"
	got := ExtractCode(text)
	if got != "package main" {
		t.Errorf("expected 'package main', got %q", got)
	}
}

func TestClient_GenerateWorkflow_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"error": {"message": "server error"}}`)
	}))
	defer server.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", BaseURL: server.URL})

	_, err := client.GenerateWorkflow(context.Background(), ai.GenerateRequest{Intent: "test"})
	if err == nil {
		t.Error("expected error from GenerateWorkflow on API error")
	}
}

func TestClient_GenerateComponent_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"error": {"message": "server error"}}`)
	}))
	defer server.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", BaseURL: server.URL})

	_, err := client.GenerateComponent(context.Background(), ai.ComponentSpec{Name: "test", Type: "test"})
	if err == nil {
		t.Error("expected error from GenerateComponent on API error")
	}
}

func TestClient_SuggestWorkflow_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"error": {"message": "server error"}}`)
	}))
	defer server.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", BaseURL: server.URL})

	_, err := client.SuggestWorkflow(context.Background(), "test")
	if err == nil {
		t.Error("expected error from SuggestWorkflow on API error")
	}
}

func TestClient_CallWithToolLoop_TextResponse(t *testing.T) {
	// Verify that callWithToolLoop returns text when stop_reason is not tool_use.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := apiResponse{
			ID: "msg_text",
			Content: []contentBlock{
				{Type: "text", Text: "Hello "},
				{Type: "text", Text: "World"},
			},
			StopReason: "end_turn",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", BaseURL: server.URL})

	result, err := client.callWithToolLoop(context.Background(), "system", "test")
	if err != nil {
		t.Fatalf("callWithToolLoop: %v", err)
	}
	if result != "Hello \nWorld" {
		t.Errorf("expected 'Hello \\nWorld', got %q", result)
	}
}
