package ai

import (
	"context"
	"fmt"
	"sync"

	"github.com/GoCodeAlone/workflow/config"
)

// Service coordinates multiple AI generator backends with provider selection,
// caching, and rate limiting.
type Service struct {
	generators map[Provider]WorkflowGenerator
	preferred  Provider
	mu         sync.RWMutex

	// Simple in-memory suggestion cache
	cache   map[string][]WorkflowSuggestion
	cacheMu sync.RWMutex
}

// NewService creates a new AI service.
func NewService() *Service {
	return &Service{
		generators: make(map[Provider]WorkflowGenerator),
		preferred:  ProviderAuto,
		cache:      make(map[string][]WorkflowSuggestion),
	}
}

// RegisterGenerator registers an AI generator for a provider.
func (s *Service) RegisterGenerator(provider Provider, gen WorkflowGenerator) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generators[provider] = gen
}

// SetPreferred sets the preferred provider. Use ProviderAuto to auto-select.
func (s *Service) SetPreferred(provider Provider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.preferred = provider
}

// Providers returns the list of registered provider names.
func (s *Service) Providers() []Provider {
	s.mu.RLock()
	defer s.mu.RUnlock()
	providers := make([]Provider, 0, len(s.generators))
	for p := range s.generators {
		providers = append(providers, p)
	}
	return providers
}

func (s *Service) generator() (WorkflowGenerator, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.preferred != ProviderAuto {
		gen, ok := s.generators[s.preferred]
		if !ok {
			return nil, fmt.Errorf("provider %q not registered", s.preferred)
		}
		return gen, nil
	}

	// Auto-select: prefer anthropic, then copilot, then any
	for _, p := range []Provider{ProviderAnthropic, ProviderCopilot} {
		if gen, ok := s.generators[p]; ok {
			return gen, nil
		}
	}

	// Fall back to any registered generator
	for _, gen := range s.generators {
		return gen, nil
	}

	return nil, fmt.Errorf("no AI generators registered")
}

// GenerateWorkflow creates a workflow config from a natural language request.
func (s *Service) GenerateWorkflow(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	gen, err := s.generator()
	if err != nil {
		return nil, err
	}
	return gen.GenerateWorkflow(ctx, req)
}

// GenerateComponent generates Go source code for a component.
func (s *Service) GenerateComponent(ctx context.Context, spec ComponentSpec) (string, error) {
	gen, err := s.generator()
	if err != nil {
		return "", err
	}
	return gen.GenerateComponent(ctx, spec)
}

// SuggestWorkflow returns cached or fresh suggestions for a use case.
func (s *Service) SuggestWorkflow(ctx context.Context, useCase string) ([]WorkflowSuggestion, error) {
	// Check cache first
	s.cacheMu.RLock()
	if cached, ok := s.cache[useCase]; ok {
		s.cacheMu.RUnlock()
		return cached, nil
	}
	s.cacheMu.RUnlock()

	gen, err := s.generator()
	if err != nil {
		return nil, err
	}

	suggestions, err := gen.SuggestWorkflow(ctx, useCase)
	if err != nil {
		return nil, err
	}

	// Cache the result
	s.cacheMu.Lock()
	s.cache[useCase] = suggestions
	s.cacheMu.Unlock()

	return suggestions, nil
}

// IdentifyMissingComponents analyzes a config for non-built-in module types.
func (s *Service) IdentifyMissingComponents(ctx context.Context, cfg *config.WorkflowConfig) ([]ComponentSpec, error) {
	gen, err := s.generator()
	if err != nil {
		return nil, err
	}
	return gen.IdentifyMissingComponents(ctx, cfg)
}

// ClearCache clears the suggestion cache.
func (s *Service) ClearCache() {
	s.cacheMu.Lock()
	s.cache = make(map[string][]WorkflowSuggestion)
	s.cacheMu.Unlock()
}
