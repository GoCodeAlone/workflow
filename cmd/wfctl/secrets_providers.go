package main

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/GoCodeAlone/workflow/secrets"
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

// secretsProviderAdapter wraps a secrets.Provider and implements SecretsProvider.
// It upgrades to MetadataProvider/AccessChecker when the underlying provider
// supports them.
type secretsProviderAdapter struct {
	p secrets.Provider
}

func (a secretsProviderAdapter) Get(ctx context.Context, name string) (string, error) {
	return a.p.Get(ctx, name)
}

func (a secretsProviderAdapter) Set(ctx context.Context, name, value string) error {
	return a.p.Set(ctx, name, value)
}

func (a secretsProviderAdapter) Delete(ctx context.Context, name string) error {
	return a.p.Delete(ctx, name)
}

// Check returns the SecretState for the named secret.
//
// Resolution order, most-precise first:
//  1. MetadataProvider.StatAll — when it succeeds, membership + presence is authoritative.
//  2. Get(name)-presence — when StatAll is unavailable/errored but Get works
//     (env, file, vault, aws). A non-empty value is Set; an empty value or
//     ErrNotFound is NotSet.
//  3. List()-membership — only when Get itself reports ErrUnsupported
//     (write-only stores like github, where reading a value is impossible).
func (a secretsProviderAdapter) Check(ctx context.Context, name string) (SecretState, error) {
	if mp, ok := a.p.(secrets.MetadataProvider); ok {
		metas, err := mp.StatAll(ctx)
		if err == nil {
			for _, m := range metas {
				if m.Name == name {
					if m.Exists {
						return SecretSet, nil
					}
					return SecretNotSet, nil
				}
			}
			return SecretNotSet, nil
		}
		// StatAll failed (including ErrUnsupported) — fall through to Get/List.
	}

	// Get-presence check.
	v, err := a.p.Get(ctx, name)
	if err == nil {
		if v != "" {
			return SecretSet, nil
		}
		return SecretNotSet, nil
	}
	if errors.Is(err, secrets.ErrNotFound) {
		return SecretNotSet, nil
	}
	if !errors.Is(err, secrets.ErrUnsupported) {
		// Unexpected Get error (e.g. permission denied) — surface as fetch error.
		return SecretFetchError, err
	}

	// Get is unsupported (write-only store) — fall back to List() membership.
	names, listErr := a.p.List(ctx)
	if listErr != nil {
		if errors.Is(listErr, secrets.ErrUnsupported) {
			return SecretFetchError, nil
		}
		return SecretFetchError, listErr
	}
	for _, n := range names {
		if n == name {
			return SecretSet, nil
		}
	}
	return SecretNotSet, nil
}

// List returns SecretStatus entries from the provider.
//
// It prefers MetadataProvider.StatAll (which carries LastRotated from
// SecretMeta.UpdatedAt). When StatAll is unavailable or errors, it falls back to
// the plain List() names with presence-only statuses. A store that supports
// neither (e.g. env with no prefix) yields an empty list rather than an error,
// so callers that only need per-entry Check semantics are unaffected.
func (a secretsProviderAdapter) List(ctx context.Context) ([]SecretStatus, error) {
	if mp, ok := a.p.(secrets.MetadataProvider); ok {
		metas, err := mp.StatAll(ctx)
		if err == nil {
			statuses := make([]SecretStatus, len(metas))
			for i, m := range metas {
				state := SecretNotSet
				if m.Exists {
					state = SecretSet
				}
				statuses[i] = SecretStatus{
					Name:        m.Name,
					State:       state,
					IsSet:       m.Exists,
					LastRotated: m.UpdatedAt,
				}
			}
			return statuses, nil
		}
		// StatAll failed (including ErrUnsupported) — fall through to List() below.
	}
	// Fall back to plain List() with presence-only statuses.
	names, err := a.p.List(ctx)
	if err != nil {
		if errors.Is(err, secrets.ErrUnsupported) {
			// Store cannot enumerate — return empty rather than erroring.
			return nil, nil
		}
		return nil, err
	}
	statuses := make([]SecretStatus, len(names))
	for i, n := range names {
		statuses[i] = SecretStatus{
			Name:  n,
			State: SecretSet,
			IsSet: true,
		}
	}
	return statuses, nil
}

// checkAccess calls CheckAccess on the underlying provider if it implements
// AccessChecker. Returns nil when the provider does not implement the interface.
func (a secretsProviderAdapter) checkAccess(ctx context.Context) error {
	if ac, ok := a.p.(secrets.AccessChecker); ok {
		return ac.CheckAccess(ctx)
	}
	return nil
}

// newSecretsProvider constructs the provider matching the given name.
// It now supports all 5 backends (env, github, vault, aws, keychain) by
// delegating to resolveSecretsProvider, then wrapping the result in the adapter.
func newSecretsProvider(providerName string) (SecretsProvider, error) {
	name := providerName
	if name == "" {
		name = "env"
	}
	p, err := resolveSecretsProvider(&SecretsConfig{Provider: name})
	if err != nil {
		return nil, err
	}
	return secretsProviderAdapter{p}, nil
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
