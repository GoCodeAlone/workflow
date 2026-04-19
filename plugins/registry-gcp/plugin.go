// Package registrygcp is a stub registry provider for Google Artifact Registry / GCR.
// Full implementation tracked at https://github.com/GoCodeAlone/workflow/issues.
package registrygcp

import (
	"github.com/GoCodeAlone/workflow/plugin/registry"
)

func init() {
	registry.Register(New())
}

type GCPProvider struct{}

func New() registry.RegistryProvider { return &GCPProvider{} }

func (g *GCPProvider) Name() string { return "gcp" }

func (g *GCPProvider) Login(_ registry.Context, _ registry.ProviderConfig) error {
	return registry.ErrNotImplemented
}

func (g *GCPProvider) Push(_ registry.Context, _ registry.ProviderConfig, _ string) error {
	return registry.ErrNotImplemented
}

func (g *GCPProvider) Prune(_ registry.Context, _ registry.ProviderConfig) error {
	return registry.ErrNotImplemented
}
