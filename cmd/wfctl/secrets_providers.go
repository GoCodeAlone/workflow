package main

import (
	"context"
	"fmt"
	"os"
	"time"
)

// SecretsProvider is the interface for secret storage backends.
type SecretsProvider interface {
	Get(ctx context.Context, name string) (string, error)
	Set(ctx context.Context, name, value string) error
	List(ctx context.Context) ([]SecretStatus, error)
	Delete(ctx context.Context, name string) error
}

// SecretStatus reports the state of a single secret in the provider.
type SecretStatus struct {
	Name        string
	IsSet       bool
	LastRotated time.Time
}

// newSecretsProvider constructs the provider matching the given name.
func newSecretsProvider(providerName string) (SecretsProvider, error) {
	switch providerName {
	case "env", "":
		return &envProvider{}, nil
	default:
		return nil, fmt.Errorf("unknown secrets provider %q (supported: env)", providerName)
	}
}

// envProvider reads/writes secrets as OS environment variables.
// NOTE: Set/Delete write to the current process environment only — they do not
// persist across process boundaries. This provider is primarily useful for
// local development and CI environments that inject secrets via env vars.
type envProvider struct{}

func (p *envProvider) Get(_ context.Context, name string) (string, error) {
	return os.Getenv(name), nil
}

func (p *envProvider) Set(_ context.Context, name, value string) error {
	return os.Setenv(name, value)
}

func (p *envProvider) List(_ context.Context) ([]SecretStatus, error) {
	// Return all environment variables as secrets — caller filters by declared entries.
	env := os.Environ()
	statuses := make([]SecretStatus, 0, len(env))
	for _, e := range env {
		for i := 0; i < len(e); i++ {
			if e[i] == '=' {
				statuses = append(statuses, SecretStatus{
					Name:  e[:i],
					IsSet: true,
				})
				break
			}
		}
	}
	return statuses, nil
}

func (p *envProvider) Delete(_ context.Context, name string) error {
	return os.Unsetenv(name)
}
