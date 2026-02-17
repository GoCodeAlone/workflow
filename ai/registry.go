package ai

import (
	"fmt"
	"sync"
)

// AIModelRegistry manages AI providers, model metadata, default model
// assignments per use-case, and per-user/tenant overrides.
type AIModelRegistry struct {
	mu        sync.RWMutex
	providers map[string]AIProvider
	models    map[string]ModelInfo     // modelID -> info
	defaults  map[string]string        // use-case -> preferred modelID
	overrides map[string]ModelOverride // "tenant:modelID" -> override
}

// ModelOverride allows per-tenant or per-user overrides for model parameters.
type ModelOverride struct {
	MaxTokens   *int     `json:"maxTokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	Enabled     *bool    `json:"enabled,omitempty"`
}

// NewAIModelRegistry creates an empty registry.
func NewAIModelRegistry() *AIModelRegistry {
	return &AIModelRegistry{
		providers: make(map[string]AIProvider),
		models:    make(map[string]ModelInfo),
		defaults:  make(map[string]string),
		overrides: make(map[string]ModelOverride),
	}
}

// RegisterProvider adds an AI provider and indexes all its models.
func (r *AIModelRegistry) RegisterProvider(provider AIProvider) error {
	if provider == nil {
		return fmt.Errorf("provider is nil")
	}
	name := provider.Name()
	if name == "" {
		return fmt.Errorf("provider name is empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.providers[name] = provider
	for _, m := range provider.Models() {
		r.models[m.ID] = m
	}
	return nil
}

// GetProvider returns a registered provider by name.
func (r *AIModelRegistry) GetProvider(name string) (AIProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}

// ListProviders returns the names of all registered providers.
func (r *AIModelRegistry) ListProviders() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for n := range r.providers {
		names = append(names, n)
	}
	return names
}

// ListModels returns metadata for all indexed models across all providers.
func (r *AIModelRegistry) ListModels() []ModelInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	models := make([]ModelInfo, 0, len(r.models))
	for _, m := range r.models {
		models = append(models, m)
	}
	return models
}

// GetModel returns metadata for a specific model ID.
func (r *AIModelRegistry) GetModel(modelID string) (ModelInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.models[modelID]
	return m, ok
}

// SetDefault sets the preferred model for a use-case (e.g., "completion", "classification").
func (r *AIModelRegistry) SetDefault(useCase, modelID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.models[modelID]; !ok {
		return fmt.Errorf("model %q not found in registry", modelID)
	}
	r.defaults[useCase] = modelID
	return nil
}

// GetDefault returns the default model for a use-case, or empty string if not set.
func (r *AIModelRegistry) GetDefault(useCase string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaults[useCase]
}

// SetOverride sets a per-tenant/user override for a model.
// The key should be formatted as "tenantID:modelID".
func (r *AIModelRegistry) SetOverride(key string, override ModelOverride) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.overrides[key] = override
}

// GetOverride returns the override for a given key, if any.
func (r *AIModelRegistry) GetOverride(key string) (ModelOverride, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	o, ok := r.overrides[key]
	return o, ok
}

// RemoveOverride removes an override by key.
func (r *AIModelRegistry) RemoveOverride(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.overrides, key)
}

// ProviderForModel returns the provider that owns a given model ID.
func (r *AIModelRegistry) ProviderForModel(modelID string) (AIProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, ok := r.models[modelID]
	if !ok {
		return nil, false
	}
	p, ok := r.providers[info.Provider]
	return p, ok
}

// ApplyOverride merges an override into a CompletionRequest, returning a modified copy.
func ApplyOverride(req CompletionRequest, override ModelOverride) CompletionRequest {
	if override.MaxTokens != nil {
		req.MaxTokens = *override.MaxTokens
	}
	if override.Temperature != nil {
		req.Temperature = *override.Temperature
	}
	return req
}
