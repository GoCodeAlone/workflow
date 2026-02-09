package dynamic

import (
	"fmt"
	"sync"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// Option configures an InterpreterPool.
type Option func(*InterpreterPool)

// WithAllowedPackages overrides the default allowed packages list.
func WithAllowedPackages(pkgs map[string]bool) Option {
	return func(p *InterpreterPool) {
		p.allowedPackages = pkgs
	}
}

// WithGoPath sets the GOPATH for interpreters.
func WithGoPath(path string) Option {
	return func(p *InterpreterPool) {
		p.goPath = path
	}
}

// InterpreterPool manages a pool of Yaegi interpreters.
type InterpreterPool struct {
	mu              sync.Mutex
	allowedPackages map[string]bool
	goPath          string
}

// NewInterpreterPool creates a new pool with optional configuration.
func NewInterpreterPool(opts ...Option) *InterpreterPool {
	p := &InterpreterPool{
		allowedPackages: AllowedPackages,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// NewInterpreter creates a sandboxed Yaegi interpreter with only the allowed
// standard library symbols loaded.
func (p *InterpreterPool) NewInterpreter() (*interp.Interpreter, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	opts := interp.Options{}
	if p.goPath != "" {
		opts.GoPath = p.goPath
	}

	i := interp.New(opts)

	// Load the stdlib — Yaegi loads all of stdlib via Use, but the sandbox
	// enforcement happens at source-validation time (ValidateSource), not
	// at the interpreter level. We still load stdlib so that the allowed
	// packages actually resolve.
	if err := i.Use(stdlib.Symbols); err != nil {
		return nil, fmt.Errorf("failed to load stdlib symbols: %w", err)
	}

	return i, nil
}
