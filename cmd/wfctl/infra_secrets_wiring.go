package main

import "github.com/GoCodeAlone/workflow/config"

// resolveSecretStoreForEnv loads the config at path and returns the secret store
// name that would be used for secretName in envName, per ResolveSecretStore
// priority rules (per-secret → env secretsStoreOverride → defaultStore → provider → "env").
func resolveSecretStoreForEnv(path, secretName, envName string) string {
	cfg, err := config.LoadFromFile(path)
	if err != nil {
		return "env"
	}
	return ResolveSecretStore(secretName, envName, cfg)
}
