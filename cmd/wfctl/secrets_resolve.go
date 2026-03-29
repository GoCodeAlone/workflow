package main

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/workflow/config"
)

// ResolveSecretStore determines which named store a secret should be fetched
// from, applying the following priority order:
//
//  1. Per-secret Store field (highest priority)
//  2. Environment-level SecretsStoreOverride
//  3. SecretsConfig.DefaultStore
//  4. Legacy SecretsConfig.Provider field
//  5. "env" fallback
func ResolveSecretStore(secretName string, envName string, cfg *config.WorkflowConfig) string {
	if cfg == nil {
		return "env"
	}

	// 1. Per-secret explicit store
	if cfg.Secrets != nil {
		for _, entry := range cfg.Secrets.Entries {
			if entry.Name == secretName && entry.Store != "" {
				return entry.Store
			}
		}
	}

	// 2. Environment-level override
	if envName != "" {
		if env, ok := cfg.Environments[envName]; ok && env != nil && env.SecretsStoreOverride != "" {
			return env.SecretsStoreOverride
		}
	}

	// 3. Default store
	if cfg.Secrets != nil && cfg.Secrets.DefaultStore != "" {
		return cfg.Secrets.DefaultStore
	}

	// 4. Legacy provider field
	if cfg.Secrets != nil && cfg.Secrets.Provider != "" {
		return cfg.Secrets.Provider
	}

	return "env"
}

// getProviderForStore returns the SecretsProvider for the named store.
// It looks up the store in cfg.SecretStores; if not found, treats the store
// name as a provider name (for backward compat and the "env" fallback).
func getProviderForStore(storeName string, cfg *config.WorkflowConfig) (SecretsProvider, error) {
	if cfg != nil {
		if store, ok := cfg.SecretStores[storeName]; ok && store != nil {
			return newSecretsProvider(store.Provider)
		}
	}
	// Store name not in SecretStores map — treat as a direct provider name.
	return newSecretsProvider(storeName)
}

// buildSecretStatuses returns access-aware SecretStatus for all declared entries
// in the config, routing each to the correct store for the given environment.
func buildSecretStatuses(ctx context.Context, envName string, cfg *config.WorkflowConfig) ([]SecretStatus, error) {
	if cfg == nil || cfg.Secrets == nil {
		return nil, nil
	}

	statuses := make([]SecretStatus, 0, len(cfg.Secrets.Entries))
	for _, entry := range cfg.Secrets.Entries {
		storeName := ResolveSecretStore(entry.Name, envName, cfg)
		provider, err := getProviderForStore(storeName, cfg)
		if err != nil {
			statuses = append(statuses, SecretStatus{
				Name:  entry.Name,
				Store: storeName,
				State: SecretUnconfigured,
				Error: fmt.Sprintf("store %q: %v", storeName, err),
			})
			continue
		}

		state, checkErr := provider.Check(ctx, entry.Name)
		status := SecretStatus{
			Name:  entry.Name,
			Store: storeName,
			State: state,
			IsSet: state == SecretSet,
		}
		if checkErr != nil {
			status.Error = checkErr.Error()
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}
