package ai

import (
	"context"
	"testing"
)

// mockProvider implements AIProvider for testing.
type mockProvider struct {
	name          string
	models        []ModelInfo
	supportsTools bool
}

func (m *mockProvider) Name() string          { return m.name }
func (m *mockProvider) Models() []ModelInfo   { return m.models }
func (m *mockProvider) SupportsToolUse() bool { return m.supportsTools }

func (m *mockProvider) Complete(_ context.Context, req CompletionRequest) (*CompletionResponse, error) {
	return &CompletionResponse{
		ID:      "resp-1",
		Model:   req.Model,
		Content: "mock response",
		Usage:   TokenUsage{InputTokens: 10, OutputTokens: 5},
	}, nil
}

func (m *mockProvider) CompleteStream(_ context.Context, _ CompletionRequest) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 2)
	ch <- StreamChunk{Content: "hello"}
	ch <- StreamChunk{Done: true}
	close(ch)
	return ch, nil
}

func (m *mockProvider) ToolComplete(_ context.Context, req ToolCompletionRequest) (*ToolCompletionResponse, error) {
	return &ToolCompletionResponse{
		CompletionResponse: CompletionResponse{
			ID:      "resp-tool-1",
			Model:   req.Model,
			Content: "tool response",
		},
	}, nil
}

func newTestProvider(name string, modelIDs ...string) *mockProvider {
	models := make([]ModelInfo, len(modelIDs))
	for i, id := range modelIDs {
		models[i] = ModelInfo{
			ID:            id,
			Name:          id,
			Provider:      name,
			ContextWindow: 128000,
			MaxOutput:     4096,
			SupportsTools: true,
		}
	}
	return &mockProvider{name: name, models: models, supportsTools: true}
}

func TestRegisterProvider(t *testing.T) {
	reg := NewAIModelRegistry()

	provider := newTestProvider("test-provider", "model-a", "model-b")
	if err := reg.RegisterProvider(provider); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}

	// Verify provider is retrievable
	got, ok := reg.GetProvider("test-provider")
	if !ok {
		t.Fatal("GetProvider returned false for registered provider")
	}
	if got.Name() != "test-provider" {
		t.Errorf("got provider name %q, want %q", got.Name(), "test-provider")
	}

	// Verify models are indexed
	models := reg.ListModels()
	if len(models) != 2 {
		t.Errorf("ListModels: got %d models, want 2", len(models))
	}

	// Verify individual model lookup
	m, ok := reg.GetModel("model-a")
	if !ok {
		t.Fatal("GetModel returned false for model-a")
	}
	if m.Provider != "test-provider" {
		t.Errorf("model-a provider = %q, want %q", m.Provider, "test-provider")
	}
}

func TestRegisterProviderErrors(t *testing.T) {
	reg := NewAIModelRegistry()

	if err := reg.RegisterProvider(nil); err == nil {
		t.Error("expected error for nil provider")
	}

	empty := &mockProvider{name: ""}
	if err := reg.RegisterProvider(empty); err == nil {
		t.Error("expected error for empty provider name")
	}
}

func TestGetProviderNotFound(t *testing.T) {
	reg := NewAIModelRegistry()
	_, ok := reg.GetProvider("nonexistent")
	if ok {
		t.Error("GetProvider should return false for unregistered provider")
	}
}

func TestListProviders(t *testing.T) {
	reg := NewAIModelRegistry()

	_ = reg.RegisterProvider(newTestProvider("alpha", "m1"))
	_ = reg.RegisterProvider(newTestProvider("beta", "m2"))

	providers := reg.ListProviders()
	if len(providers) != 2 {
		t.Errorf("ListProviders: got %d, want 2", len(providers))
	}

	names := make(map[string]bool)
	for _, n := range providers {
		names[n] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("ListProviders: missing expected providers, got %v", providers)
	}
}

func TestSetAndGetDefault(t *testing.T) {
	reg := NewAIModelRegistry()
	_ = reg.RegisterProvider(newTestProvider("p", "model-x"))

	// Setting default for known model should work
	if err := reg.SetDefault("completion", "model-x"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}

	got := reg.GetDefault("completion")
	if got != "model-x" {
		t.Errorf("GetDefault = %q, want %q", got, "model-x")
	}

	// Setting default for unknown model should error
	if err := reg.SetDefault("completion", "unknown-model"); err == nil {
		t.Error("expected error for unknown model")
	}

	// Getting unset default returns empty string
	if d := reg.GetDefault("unset-use-case"); d != "" {
		t.Errorf("GetDefault for unset use-case = %q, want empty", d)
	}
}

func TestOverrides(t *testing.T) {
	reg := NewAIModelRegistry()

	maxTok := 200
	temp := 0.5
	override := ModelOverride{
		MaxTokens:   &maxTok,
		Temperature: &temp,
	}

	key := "tenant1:model-a"
	reg.SetOverride(key, override)

	got, ok := reg.GetOverride(key)
	if !ok {
		t.Fatal("GetOverride returned false for set override")
	}
	if *got.MaxTokens != 200 {
		t.Errorf("MaxTokens = %d, want 200", *got.MaxTokens)
	}
	if *got.Temperature != 0.5 {
		t.Errorf("Temperature = %f, want 0.5", *got.Temperature)
	}

	// Remove and verify
	reg.RemoveOverride(key)
	_, ok = reg.GetOverride(key)
	if ok {
		t.Error("GetOverride should return false after RemoveOverride")
	}
}

func TestApplyOverride(t *testing.T) {
	req := CompletionRequest{
		Model:       "m1",
		MaxTokens:   1000,
		Temperature: 0.7,
	}

	maxTok := 500
	temp := 0.3
	override := ModelOverride{
		MaxTokens:   &maxTok,
		Temperature: &temp,
	}

	result := ApplyOverride(req, override)
	if result.MaxTokens != 500 {
		t.Errorf("MaxTokens = %d, want 500", result.MaxTokens)
	}
	if result.Temperature != 0.3 {
		t.Errorf("Temperature = %f, want 0.3", result.Temperature)
	}

	// Original should be unchanged
	if req.MaxTokens != 1000 {
		t.Error("original request was mutated")
	}
}

func TestApplyOverridePartial(t *testing.T) {
	req := CompletionRequest{
		MaxTokens:   1000,
		Temperature: 0.7,
	}

	// Only override temperature
	temp := 0.1
	override := ModelOverride{Temperature: &temp}

	result := ApplyOverride(req, override)
	if result.MaxTokens != 1000 {
		t.Errorf("MaxTokens should be unchanged, got %d", result.MaxTokens)
	}
	if result.Temperature != 0.1 {
		t.Errorf("Temperature = %f, want 0.1", result.Temperature)
	}
}

func TestProviderForModel(t *testing.T) {
	reg := NewAIModelRegistry()
	_ = reg.RegisterProvider(newTestProvider("anthropic", "claude-3"))
	_ = reg.RegisterProvider(newTestProvider("openai", "gpt-4"))

	p, ok := reg.ProviderForModel("claude-3")
	if !ok {
		t.Fatal("ProviderForModel returned false for claude-3")
	}
	if p.Name() != "anthropic" {
		t.Errorf("provider = %q, want %q", p.Name(), "anthropic")
	}

	p, ok = reg.ProviderForModel("gpt-4")
	if !ok {
		t.Fatal("ProviderForModel returned false for gpt-4")
	}
	if p.Name() != "openai" {
		t.Errorf("provider = %q, want %q", p.Name(), "openai")
	}

	_, ok = reg.ProviderForModel("nonexistent")
	if ok {
		t.Error("ProviderForModel should return false for unknown model")
	}
}

func TestMultipleProvidersModelIndexing(t *testing.T) {
	reg := NewAIModelRegistry()

	_ = reg.RegisterProvider(newTestProvider("p1", "m1", "m2"))
	_ = reg.RegisterProvider(newTestProvider("p2", "m3"))

	models := reg.ListModels()
	if len(models) != 3 {
		t.Errorf("ListModels: got %d, want 3", len(models))
	}

	for _, id := range []string{"m1", "m2", "m3"} {
		if _, ok := reg.GetModel(id); !ok {
			t.Errorf("model %q not found", id)
		}
	}
}
