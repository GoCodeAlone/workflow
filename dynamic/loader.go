package dynamic

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// Loader handles loading dynamic components from various sources.
type Loader struct {
	pool     *InterpreterPool
	registry *ComponentRegistry
}

// NewLoader creates a Loader backed by the given pool and registry.
func NewLoader(pool *InterpreterPool, registry *ComponentRegistry) *Loader {
	return &Loader{
		pool:     pool,
		registry: registry,
	}
}

// ValidateSource performs a basic syntax check and verifies that only allowed
// packages are imported.
func ValidateSource(source string) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "dynamic.go", source, parser.ImportsOnly)
	if err != nil {
		return fmt.Errorf("syntax error: %w", err)
	}

	for _, imp := range f.Imports {
		// imp.Path.Value includes surrounding quotes
		pkg := strings.Trim(imp.Path.Value, `"`)
		if !IsPackageAllowed(pkg) {
			return fmt.Errorf("import %q is not allowed in dynamic components", pkg)
		}
	}
	return nil
}

// LoadFromString validates, compiles, and registers a component from source.
func (l *Loader) LoadFromString(id, source string) (*DynamicComponent, error) {
	if err := ValidateSource(source); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	comp := NewDynamicComponent(id, l.pool)
	if err := comp.LoadFromSource(source); err != nil {
		return nil, err
	}

	if err := l.registry.Register(id, comp); err != nil {
		return nil, err
	}
	return comp, nil
}

// LoadFromFile reads a .go file and loads it as a component.
// The component ID is derived from the filename (without extension) unless
// the caller provides an explicit id.
func (l *Loader) LoadFromFile(id, path string) (*DynamicComponent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}

	if id == "" {
		base := filepath.Base(path)
		id = strings.TrimSuffix(base, filepath.Ext(base))
	}

	return l.LoadFromString(id, string(data))
}

// LoadFromDirectory scans a directory for .go files and loads each one.
func (l *Loader) LoadFromDirectory(dir string) ([]*DynamicComponent, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	var components []*DynamicComponent
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		// Skip test files
		if strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		comp, err := l.LoadFromFile("", path)
		if err != nil {
			return components, fmt.Errorf("failed to load %s: %w", path, err)
		}
		components = append(components, comp)
	}
	return components, nil
}

// Reload unloads an existing component and reloads it from new source.
func (l *Loader) Reload(id, source string) (*DynamicComponent, error) {
	// Stop old component if it was running
	if old, ok := l.registry.Get(id); ok {
		info := old.Info()
		if info.Status == StatusRunning {
			// Best-effort stop
			_ = old.Stop(context.Background())
		}
	}

	if err := ValidateSource(source); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	comp := NewDynamicComponent(id, l.pool)
	if err := comp.LoadFromSource(source); err != nil {
		return nil, err
	}

	if err := l.registry.Register(id, comp); err != nil {
		return nil, err
	}
	return comp, nil
}
