package module

import (
	"context"
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
)

// CacheSetStep writes a value to a named CacheModule.
type CacheSetStep struct {
	name  string
	cache string        // service name of the CacheModule
	key   string        // key template
	value string        // value template
	ttl   time.Duration // 0 means use the module default
	app   modular.Application
	tmpl  *TemplateEngine
}

// NewCacheSetStepFactory returns a StepFactory that creates CacheSetStep instances.
func NewCacheSetStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		cache, _ := config["cache"].(string)
		if cache == "" {
			return nil, fmt.Errorf("cache_set step %q: 'cache' is required", name)
		}

		key, _ := config["key"].(string)
		if key == "" {
			return nil, fmt.Errorf("cache_set step %q: 'key' is required", name)
		}

		value, _ := config["value"].(string)
		if value == "" {
			return nil, fmt.Errorf("cache_set step %q: 'value' is required", name)
		}

		var ttl time.Duration
		if ttlStr, ok := config["ttl"].(string); ok && ttlStr != "" {
			parsed, err := time.ParseDuration(ttlStr)
			if err != nil {
				return nil, fmt.Errorf("cache_set step %q: invalid 'ttl' %q: %w", name, ttlStr, err)
			}
			ttl = parsed
		}

		return &CacheSetStep{
			name:  name,
			cache: cache,
			key:   key,
			value: value,
			ttl:   ttl,
			app:   app,
			tmpl:  NewTemplateEngine(),
		}, nil
	}
}

func (s *CacheSetStep) Name() string { return s.name }

func (s *CacheSetStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("cache_set step %q: no application context", s.name)
	}

	cm, err := s.resolveCache()
	if err != nil {
		return nil, err
	}

	resolvedKey, err := s.tmpl.Resolve(s.key, pc)
	if err != nil {
		return nil, fmt.Errorf("cache_set step %q: failed to resolve key template: %w", s.name, err)
	}

	resolvedValue, err := s.tmpl.Resolve(s.value, pc)
	if err != nil {
		return nil, fmt.Errorf("cache_set step %q: failed to resolve value template: %w", s.name, err)
	}

	if err := cm.Set(ctx, resolvedKey, resolvedValue, s.ttl); err != nil {
		return nil, fmt.Errorf("cache_set step %q: set failed: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"cached": true,
	}}, nil
}

func (s *CacheSetStep) resolveCache() (CacheModule, error) {
	svc, ok := s.app.SvcRegistry()[s.cache]
	if !ok {
		return nil, fmt.Errorf("cache_set step %q: cache service %q not found", s.name, s.cache)
	}
	cm, ok := svc.(CacheModule)
	if !ok {
		return nil, fmt.Errorf("cache_set step %q: service %q does not implement CacheModule", s.name, s.cache)
	}
	return cm, nil
}
