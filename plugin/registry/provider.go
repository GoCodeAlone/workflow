// Package registry defines the RegistryProvider interface and global registry.
package registry

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/GoCodeAlone/workflow/config"
)

// Context carries request-scoped values for registry operations.
type Context struct {
	context.Context
	out    io.Writer
	dryRun bool
}

// NewContext creates a Context with an output writer and dry-run flag.
func NewContext(parent context.Context, out io.Writer, dryRun bool) Context {
	return Context{Context: parent, out: out, dryRun: dryRun}
}

// Out returns the output writer.
func (c Context) Out() io.Writer { return c.out }

// DryRun returns whether this is a dry-run invocation.
func (c Context) DryRun() bool { return c.dryRun }

// ProviderConfig carries the registry config for a single operation.
type ProviderConfig struct {
	Registry config.CIRegistry
}

// RegistryProvider is implemented by each container registry backend.
type RegistryProvider interface {
	Name() string
	Login(ctx Context, cfg ProviderConfig) error
	Logout(ctx Context, cfg ProviderConfig) error
	Push(ctx Context, cfg ProviderConfig, imageRef string) error
	Prune(ctx Context, cfg ProviderConfig) error
}

var (
	mu        sync.RWMutex
	providers = map[string]RegistryProvider{}
)

// Register adds p to the global registry. Safe to call from init().
func Register(p RegistryProvider) {
	mu.Lock()
	defer mu.Unlock()
	providers[p.Name()] = p
}

// Get returns the provider registered under name.
func Get(name string) (RegistryProvider, bool) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := providers[name]
	return p, ok
}

// List returns all registered providers.
func List() []RegistryProvider {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]RegistryProvider, 0, len(providers))
	for _, p := range providers {
		out = append(out, p)
	}
	return out
}

// ErrNotImplemented is returned by stub providers.
var ErrNotImplemented = fmt.Errorf("not implemented")
