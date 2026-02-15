package connector

import (
	"context"
	"fmt"
	"sync"
)

// Registry manages connector factories and instances.
type Registry struct {
	sources   map[string]SourceFactory
	sinks     map[string]SinkFactory
	instances map[string]any // running instances keyed by name
	mu        sync.RWMutex
}

// NewRegistry creates an empty connector registry.
func NewRegistry() *Registry {
	return &Registry{
		sources:   make(map[string]SourceFactory),
		sinks:     make(map[string]SinkFactory),
		instances: make(map[string]any),
	}
}

// RegisterSource registers a SourceFactory for the given connector type.
func (r *Registry) RegisterSource(connectorType string, factory SourceFactory) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.sources[connectorType]; exists {
		return fmt.Errorf("source factory already registered for type %q", connectorType)
	}
	r.sources[connectorType] = factory
	return nil
}

// RegisterSink registers a SinkFactory for the given connector type.
func (r *Registry) RegisterSink(connectorType string, factory SinkFactory) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.sinks[connectorType]; exists {
		return fmt.Errorf("sink factory already registered for type %q", connectorType)
	}
	r.sinks[connectorType] = factory
	return nil
}

// CreateSource creates and tracks a new EventSource instance.
func (r *Registry) CreateSource(connectorType, name string, config map[string]any) (EventSource, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	factory, ok := r.sources[connectorType]
	if !ok {
		return nil, fmt.Errorf("unknown source type %q", connectorType)
	}

	if _, exists := r.instances[name]; exists {
		return nil, fmt.Errorf("connector instance %q already exists", name)
	}

	source, err := factory(name, config)
	if err != nil {
		return nil, fmt.Errorf("create source %q (type %s): %w", name, connectorType, err)
	}

	r.instances[name] = source
	return source, nil
}

// CreateSink creates and tracks a new EventSink instance.
func (r *Registry) CreateSink(connectorType, name string, config map[string]any) (EventSink, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	factory, ok := r.sinks[connectorType]
	if !ok {
		return nil, fmt.Errorf("unknown sink type %q", connectorType)
	}

	if _, exists := r.instances[name]; exists {
		return nil, fmt.Errorf("connector instance %q already exists", name)
	}

	sink, err := factory(name, config)
	if err != nil {
		return nil, fmt.Errorf("create sink %q (type %s): %w", name, connectorType, err)
	}

	r.instances[name] = sink
	return sink, nil
}

// ListSources returns the registered source connector type names.
func (r *Registry) ListSources() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.sources))
	for t := range r.sources {
		types = append(types, t)
	}
	return types
}

// ListSinks returns the registered sink connector type names.
func (r *Registry) ListSinks() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.sinks))
	for t := range r.sinks {
		types = append(types, t)
	}
	return types
}

// GetInstance returns a running connector instance by name.
func (r *Registry) GetInstance(name string) (any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	inst, ok := r.instances[name]
	return inst, ok
}

// StopAll stops all running connector instances and clears the instance map.
func (r *Registry) StopAll(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var lastErr error
	for name, inst := range r.instances {
		switch c := inst.(type) {
		case EventSource:
			if err := c.Stop(ctx); err != nil {
				lastErr = fmt.Errorf("stop source %q: %w", name, err)
			}
		case EventSink:
			if err := c.Stop(ctx); err != nil {
				lastErr = fmt.Errorf("stop sink %q: %w", name, err)
			}
		}
	}

	r.instances = make(map[string]any)
	return lastErr
}
