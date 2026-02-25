package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// resolveNoSQLStore looks up a NoSQLStore from the service registry by name.
func resolveNoSQLStore(app modular.Application, storeName, stepName string) (NoSQLStore, error) {
	svc, ok := app.SvcRegistry()[storeName]
	if !ok {
		return nil, fmt.Errorf("%s: NoSQL store %q not found in service registry", stepName, storeName)
	}
	store, ok := svc.(NoSQLStore)
	if !ok {
		return nil, fmt.Errorf("%s: service %q does not implement NoSQLStore", stepName, storeName)
	}
	return store, nil
}

// ── nosql_get ────────────────────────────────────────────────────────────────

// NoSQLGetStep retrieves an item by key from a named NoSQL store.
type NoSQLGetStep struct {
	name   string
	store  string
	key    string
	output string
	missOK bool
	app    modular.Application
	tmpl   *TemplateEngine
}

// NewNoSQLGetStepFactory returns a StepFactory for step.nosql_get.
func NewNoSQLGetStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		store, _ := config["store"].(string)
		if store == "" {
			return nil, fmt.Errorf("nosql_get step %q: 'store' is required", name)
		}
		key, _ := config["key"].(string)
		if key == "" {
			return nil, fmt.Errorf("nosql_get step %q: 'key' is required", name)
		}
		output, _ := config["output"].(string)
		if output == "" {
			output = "item"
		}
		missOK := true
		if v, ok := config["miss_ok"].(bool); ok {
			missOK = v
		}
		return &NoSQLGetStep{
			name:   name,
			store:  store,
			key:    key,
			output: output,
			missOK: missOK,
			app:    app,
			tmpl:   NewTemplateEngine(),
		}, nil
	}
}

func (s *NoSQLGetStep) Name() string { return s.name }

func (s *NoSQLGetStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("nosql_get step %q: no application context", s.name)
	}
	ns, err := resolveNoSQLStore(s.app, s.store, "nosql_get step "+s.name)
	if err != nil {
		return nil, err
	}
	resolvedKey, err := s.tmpl.Resolve(s.key, pc)
	if err != nil {
		return nil, fmt.Errorf("nosql_get step %q: failed to resolve key: %w", s.name, err)
	}
	item, err := ns.Get(ctx, resolvedKey)
	if err != nil {
		return nil, fmt.Errorf("nosql_get step %q: get failed: %w", s.name, err)
	}
	if item == nil {
		if !s.missOK {
			return nil, fmt.Errorf("nosql_get step %q: key %q not found", s.name, resolvedKey)
		}
		return &StepResult{Output: map[string]any{
			s.output: map[string]any{},
			"found":  false,
		}}, nil
	}
	return &StepResult{Output: map[string]any{
		s.output: item,
		"found":  true,
	}}, nil
}

// ── nosql_put ────────────────────────────────────────────────────────────────

// NoSQLPutStep inserts or replaces an item in a named NoSQL store.
type NoSQLPutStep struct {
	name  string
	store string
	key   string
	item  string // template expression that resolves to a map key in the pipeline context
	app   modular.Application
	tmpl  *TemplateEngine
}

// NewNoSQLPutStepFactory returns a StepFactory for step.nosql_put.
func NewNoSQLPutStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		store, _ := config["store"].(string)
		if store == "" {
			return nil, fmt.Errorf("nosql_put step %q: 'store' is required", name)
		}
		key, _ := config["key"].(string)
		if key == "" {
			return nil, fmt.Errorf("nosql_put step %q: 'key' is required", name)
		}
		item, _ := config["item"].(string)
		if item == "" {
			item = "body"
		}
		return &NoSQLPutStep{
			name:  name,
			store: store,
			key:   key,
			item:  item,
			app:   app,
			tmpl:  NewTemplateEngine(),
		}, nil
	}
}

func (s *NoSQLPutStep) Name() string { return s.name }

func (s *NoSQLPutStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("nosql_put step %q: no application context", s.name)
	}
	ns, err := resolveNoSQLStore(s.app, s.store, "nosql_put step "+s.name)
	if err != nil {
		return nil, err
	}
	resolvedKey, err := s.tmpl.Resolve(s.key, pc)
	if err != nil {
		return nil, fmt.Errorf("nosql_put step %q: failed to resolve key: %w", s.name, err)
	}

	// Resolve item from pipeline context data
	var itemData map[string]any
	rawItem := pc.Current[s.item]
	if m, ok := rawItem.(map[string]any); ok {
		itemData = m
	} else if rawItem != nil {
		itemData = map[string]any{"value": rawItem}
	} else {
		// fallback: use all current context data
		itemData = map[string]any{}
		for k, v := range pc.Current {
			itemData[k] = v
		}
	}

	if err := ns.Put(ctx, resolvedKey, itemData); err != nil {
		return nil, fmt.Errorf("nosql_put step %q: put failed: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"stored": true,
		"key":    resolvedKey,
	}}, nil
}

// ── nosql_delete ─────────────────────────────────────────────────────────────

// NoSQLDeleteStep deletes an item by key from a named NoSQL store.
type NoSQLDeleteStep struct {
	name  string
	store string
	key   string
	app   modular.Application
	tmpl  *TemplateEngine
}

// NewNoSQLDeleteStepFactory returns a StepFactory for step.nosql_delete.
func NewNoSQLDeleteStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		store, _ := config["store"].(string)
		if store == "" {
			return nil, fmt.Errorf("nosql_delete step %q: 'store' is required", name)
		}
		key, _ := config["key"].(string)
		if key == "" {
			return nil, fmt.Errorf("nosql_delete step %q: 'key' is required", name)
		}
		return &NoSQLDeleteStep{
			name:  name,
			store: store,
			key:   key,
			app:   app,
			tmpl:  NewTemplateEngine(),
		}, nil
	}
}

func (s *NoSQLDeleteStep) Name() string { return s.name }

func (s *NoSQLDeleteStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("nosql_delete step %q: no application context", s.name)
	}
	ns, err := resolveNoSQLStore(s.app, s.store, "nosql_delete step "+s.name)
	if err != nil {
		return nil, err
	}
	resolvedKey, err := s.tmpl.Resolve(s.key, pc)
	if err != nil {
		return nil, fmt.Errorf("nosql_delete step %q: failed to resolve key: %w", s.name, err)
	}
	if err := ns.Delete(ctx, resolvedKey); err != nil {
		return nil, fmt.Errorf("nosql_delete step %q: delete failed: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		"deleted": true,
		"key":     resolvedKey,
	}}, nil
}

// ── nosql_query ──────────────────────────────────────────────────────────────

// NoSQLQueryStep queries items from a named NoSQL store with optional filters.
type NoSQLQueryStep struct {
	name   string
	store  string
	prefix string
	output string
	app    modular.Application
	tmpl   *TemplateEngine
}

// NewNoSQLQueryStepFactory returns a StepFactory for step.nosql_query.
func NewNoSQLQueryStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		store, _ := config["store"].(string)
		if store == "" {
			return nil, fmt.Errorf("nosql_query step %q: 'store' is required", name)
		}
		prefix, _ := config["prefix"].(string)
		output, _ := config["output"].(string)
		if output == "" {
			output = "items"
		}
		return &NoSQLQueryStep{
			name:   name,
			store:  store,
			prefix: prefix,
			output: output,
			app:    app,
			tmpl:   NewTemplateEngine(),
		}, nil
	}
}

func (s *NoSQLQueryStep) Name() string { return s.name }

func (s *NoSQLQueryStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("nosql_query step %q: no application context", s.name)
	}
	ns, err := resolveNoSQLStore(s.app, s.store, "nosql_query step "+s.name)
	if err != nil {
		return nil, err
	}

	resolvedPrefix, err := s.tmpl.Resolve(s.prefix, pc)
	if err != nil {
		return nil, fmt.Errorf("nosql_query step %q: failed to resolve prefix: %w", s.name, err)
	}

	params := map[string]any{}
	if resolvedPrefix != "" {
		params["prefix"] = resolvedPrefix
	}

	items, err := ns.Query(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("nosql_query step %q: query failed: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{
		s.output: items,
		"count":  len(items),
	}}, nil
}
