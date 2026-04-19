// Package registrygitlab is a stub registry provider for GitLab Container Registry.
// Full implementation tracked in the issue tracker.
package registrygitlab

import (
	"github.com/GoCodeAlone/workflow/plugin/registry"
)

func init() {
	registry.Register(New())
}

type GitLabProvider struct{}

func New() registry.RegistryProvider { return &GitLabProvider{} }

func (g *GitLabProvider) Name() string { return "gitlab" }

func (g *GitLabProvider) Login(_ registry.Context, _ registry.ProviderConfig) error {
	return registry.ErrNotImplemented
}

func (g *GitLabProvider) Logout(_ registry.Context, _ registry.ProviderConfig) error {
	return registry.ErrNotImplemented
}

func (g *GitLabProvider) Push(_ registry.Context, _ registry.ProviderConfig, _ string) error {
	return registry.ErrNotImplemented
}

func (g *GitLabProvider) Prune(_ registry.Context, _ registry.ProviderConfig) error {
	return registry.ErrNotImplemented
}
