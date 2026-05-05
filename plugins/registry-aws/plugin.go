// Package registryaws is a stub registry provider for Amazon ECR.
// Full implementation tracked in the issue tracker.
package registryaws

import (
	"github.com/GoCodeAlone/workflow/plugin/registry"
)

func init() {
	registry.Register(New())
}

type AWSProvider struct{}

func New() registry.RegistryProvider { return &AWSProvider{} }

func (a *AWSProvider) Name() string { return "aws" }

func (a *AWSProvider) Login(_ registry.Context, _ registry.ProviderConfig) error {
	return registry.ErrNotImplemented
}

func (a *AWSProvider) Logout(_ registry.Context, _ registry.ProviderConfig) error {
	return registry.ErrNotImplemented
}

func (a *AWSProvider) Push(_ registry.Context, _ registry.ProviderConfig, _ string) error {
	return registry.ErrNotImplemented
}

func (a *AWSProvider) Prune(_ registry.Context, _ registry.ProviderConfig) error {
	return registry.ErrNotImplemented
}
