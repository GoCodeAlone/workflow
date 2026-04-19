// Package registryazure is a stub registry provider for Azure Container Registry.
// Full implementation tracked at https://github.com/GoCodeAlone/workflow/issues.
package registryazure

import (
	"github.com/GoCodeAlone/workflow/plugin/registry"
)

func init() {
	registry.Register(New())
}

type AzureProvider struct{}

func New() registry.RegistryProvider { return &AzureProvider{} }

func (a *AzureProvider) Name() string { return "azure" }

func (a *AzureProvider) Login(_ registry.Context, _ registry.ProviderConfig) error {
	return registry.ErrNotImplemented
}

func (a *AzureProvider) Push(_ registry.Context, _ registry.ProviderConfig, _ string) error {
	return registry.ErrNotImplemented
}

func (a *AzureProvider) Prune(_ registry.Context, _ registry.ProviderConfig) error {
	return registry.ErrNotImplemented
}
