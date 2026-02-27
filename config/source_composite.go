package config

import (
	"context"
	"fmt"
)

// CompositeSource layers multiple ConfigSources. Later sources override earlier ones.
// Module-level overrides are applied by name; map keys (workflows, triggers,
// pipelines, platform) from later sources replace or add to those from earlier ones.
type CompositeSource struct {
	sources []ConfigSource
}

// NewCompositeSource creates a CompositeSource from the given sources.
// Sources are applied in order: sources[0] is the base, each subsequent source
// overlays on top of the result.
func NewCompositeSource(sources ...ConfigSource) *CompositeSource {
	return &CompositeSource{sources: sources}
}

// Load loads all sources and merges them into a single WorkflowConfig.
func (s *CompositeSource) Load(ctx context.Context) (*WorkflowConfig, error) {
	if len(s.sources) == 0 {
		return nil, fmt.Errorf("composite source: no sources configured")
	}
	base, err := s.sources[0].Load(ctx)
	if err != nil {
		return nil, err
	}
	for _, src := range s.sources[1:] {
		overlay, err := src.Load(ctx)
		if err != nil {
			return nil, fmt.Errorf("composite source %s: %w", src.Name(), err)
		}
		mergeOverlay(base, overlay)
	}
	return base, nil
}

// Hash loads the merged config and returns its hash.
func (s *CompositeSource) Hash(ctx context.Context) (string, error) {
	cfg, err := s.Load(ctx)
	if err != nil {
		return "", err
	}
	return HashConfig(cfg)
}

// Name returns a human-readable identifier for this source.
func (s *CompositeSource) Name() string { return "composite" }

// mergeOverlay applies overlay's configuration on top of base in place.
// Modules: overlay modules replace base modules with the same name; new names are appended.
// Workflows/Triggers/Pipelines/Platform: overlay keys replace or add to base keys.
func mergeOverlay(base, overlay *WorkflowConfig) {
	if overlay == nil {
		return
	}

	// Replace or append modules by name.
	existing := make(map[string]int, len(base.Modules))
	for i, m := range base.Modules {
		existing[m.Name] = i
	}
	for _, m := range overlay.Modules {
		if idx, ok := existing[m.Name]; ok {
			base.Modules[idx] = m
		} else {
			base.Modules = append(base.Modules, m)
		}
	}

	// Merge map sections.
	for k, v := range overlay.Workflows {
		if base.Workflows == nil {
			base.Workflows = make(map[string]any)
		}
		base.Workflows[k] = v
	}
	for k, v := range overlay.Triggers {
		if base.Triggers == nil {
			base.Triggers = make(map[string]any)
		}
		base.Triggers[k] = v
	}
	for k, v := range overlay.Pipelines {
		if base.Pipelines == nil {
			base.Pipelines = make(map[string]any)
		}
		base.Pipelines[k] = v
	}
	for k, v := range overlay.Platform {
		if base.Platform == nil {
			base.Platform = make(map[string]any)
		}
		base.Platform[k] = v
	}
}
