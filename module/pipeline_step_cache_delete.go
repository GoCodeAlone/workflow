package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// CacheDeleteStep removes a key from a named CacheModule.
type CacheDeleteStep struct {
	name  string
	cache string // service name of the CacheModule
	key   string // key template
	app   modular.Application
	tmpl  *TemplateEngine
}

// NewCacheDeleteStepFactory returns a StepFactory that creates CacheDeleteStep instances.
func NewCacheDeleteStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		cache, _ := config["cache"].(string)
		if cache == "" {
			return nil, fmt.Errorf("cache_delete step %q: 'cache' is required", name)
		}

		key, _ := config["key"].(string)
		if key == "" {
			return nil, fmt.Errorf("cache_delete step %q: 'key' is required", name)
		}

		return &CacheDeleteStep{
			name:  name,
			cache: cache,
			key:   key,
			app:   app,
			tmpl:  NewTemplateEngine(),
		}, nil
	}
}

func (s *CacheDeleteStep) Name() string { return s.name }

func (s *CacheDeleteStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("cache_delete step %q: no application context", s.name)
	}

	cm, err := s.resolveCache()
	if err != nil {
		return nil, err
	}

	resolvedKey, err := s.tmpl.Resolve(s.key, pc)
	if err != nil {
		return nil, fmt.Errorf("cache_delete step %q: failed to resolve key template: %w", s.name, err)
	}

	if err := cm.Delete(ctx, resolvedKey); err != nil {
		return nil, fmt.Errorf("cache_delete step %q: delete failed: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"deleted": true,
	}}, nil
}

func (s *CacheDeleteStep) resolveCache() (CacheModule, error) {
	svc, ok := s.app.SvcRegistry()[s.cache]
	if !ok {
		return nil, fmt.Errorf("cache_delete step %q: cache service %q not found", s.name, s.cache)
	}
	cm, ok := svc.(CacheModule)
	if !ok {
		return nil, fmt.Errorf("cache_delete step %q: service %q does not implement CacheModule", s.name, s.cache)
	}
	return cm, nil
}
