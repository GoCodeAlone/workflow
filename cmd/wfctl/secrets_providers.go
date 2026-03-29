package main

import (
	"context"
	"fmt"
	"os"
	"time"
)

// SecretState describes the accessibility of a secret in its backing store.
type SecretState int

const (
	// SecretSet means the secret has a non-empty value in the store.
	SecretSet SecretState = iota
	// SecretNotSet means the secret key exists in the store but has an empty value.
	SecretNotSet
	// SecretNoAccess means the store is configured but the caller cannot read the secret
	// (e.g. insufficient IAM permissions).
	SecretNoAccess
	// SecretFetchError means an unexpected error occurred when checking the secret.
	SecretFetchError
	// SecretUnconfigured means no store is configured for this secret.
	SecretUnconfigured
)

// SecretsProvider is the interface for secret storage backends.
type SecretsProvider interface {
	Get(ctx context.Context, name string) (string, error)
	Set(ctx context.Context, name, value string) error
	// Check returns the SecretState for the named secret without returning the value.
	Check(ctx context.Context, name string) (SecretState, error)
	List(ctx context.Context) ([]SecretStatus, error)
	Delete(ctx context.Context, name string) error
}

// SecretStatus reports the state of a single secret in the provider.
type SecretStatus struct {
	Name        string
	Store       string
	State       SecretState
	Error       string
	LastRotated time.Time
	// IsSet is kept for backward compatibility — true when State == SecretSet.
	IsSet bool
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

func (p *envProvider) Check(_ context.Context, name string) (SecretState, error) {
	val := os.Getenv(name)
	if val != "" {
		return SecretSet, nil
	}
	return SecretNotSet, nil
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
					State: SecretSet,
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
