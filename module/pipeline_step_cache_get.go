package module

import (
	"context"
	"errors"
	"fmt"

	"github.com/CrisisTextLine/modular"
	"github.com/redis/go-redis/v9"
)

// CacheGetStep reads a value from a named CacheModule and stores it in the
// pipeline context under a configurable output field.
type CacheGetStep struct {
	name   string
	cache  string // service name of the CacheModule
	key    string // key template, e.g. "user:{{.user_id}}"
	output string // output field name (default: "value")
	missOK bool   // when true a cache miss is not an error
	app    modular.Application
	tmpl   *TemplateEngine
}

// NewCacheGetStepFactory returns a StepFactory that creates CacheGetStep instances.
func NewCacheGetStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		cache, _ := config["cache"].(string)
		if cache == "" {
			return nil, fmt.Errorf("cache_get step %q: 'cache' is required", name)
		}

		key, _ := config["key"].(string)
		if key == "" {
			return nil, fmt.Errorf("cache_get step %q: 'key' is required", name)
		}

		output, _ := config["output"].(string)
		if output == "" {
			output = "value"
		}

		missOK := true
		if v, ok := config["miss_ok"].(bool); ok {
			missOK = v
		}

		return &CacheGetStep{
			name:   name,
			cache:  cache,
			key:    key,
			output: output,
			missOK: missOK,
			app:    app,
			tmpl:   NewTemplateEngine(),
		}, nil
	}
}

func (s *CacheGetStep) Name() string { return s.name }

func (s *CacheGetStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("cache_get step %q: no application context", s.name)
	}

	cm, err := s.resolveCache()
	if err != nil {
		return nil, err
	}

	resolvedKey, err := s.tmpl.Resolve(s.key, pc)
	if err != nil {
		return nil, fmt.Errorf("cache_get step %q: failed to resolve key template: %w", s.name, err)
	}

	val, err := cm.Get(ctx, resolvedKey)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// Cache miss
			if !s.missOK {
				return nil, fmt.Errorf("cache_get step %q: cache miss for key %q", s.name, resolvedKey)
			}
			return &StepResult{Output: map[string]any{
				s.output:    "",
				"cache_hit": false,
			}}, nil
		}
		return nil, fmt.Errorf("cache_get step %q: get failed: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		s.output:    val,
		"cache_hit": true,
	}}, nil
}

func (s *CacheGetStep) resolveCache() (CacheModule, error) {
	svc, ok := s.app.SvcRegistry()[s.cache]
	if !ok {
		return nil, fmt.Errorf("cache_get step %q: cache service %q not found", s.name, s.cache)
	}
	cm, ok := svc.(CacheModule)
	if !ok {
		return nil, fmt.Errorf("cache_get step %q: service %q does not implement CacheModule", s.name, s.cache)
	}
	return cm, nil
}
