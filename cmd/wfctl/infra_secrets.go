package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/secrets"
	"gopkg.in/yaml.v3"
)

// SecretsConfig, SecretGen and InfraConfig are type aliases for the canonical
// definitions in the config package. Aliases keep all existing cmd/wfctl code
// (including test files) working without renaming, while ensuring that any
// round-trip through config.WorkflowConfig preserves the generate[] and
// infra.auto_bootstrap fields (which were previously dropped because the local
// struct definitions were not reflected in config.WorkflowConfig).
type SecretsConfig = config.SecretsConfig
type SecretGen = config.SecretGen
type InfraConfig = config.InfraConfig

// parseSecretsConfig reads the "secrets:" top-level key from a YAML file,
// honoring any imports: directives so that the merged secrets section
// (entries, defaultStore, generate, etc.) is visible to callers.
// Returns nil, nil if the section is absent after merging.
func parseSecretsConfig(cfgFile string) (*SecretsConfig, error) {
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("load config %s: %w", cfgFile, err)
	}
	return cfg.Secrets, nil
}

// parseInfraConfig reads the "infra:" top-level section from a YAML file.
// Returns nil, nil if the section is absent.
func parseInfraConfig(cfgFile string) (*InfraConfig, error) {
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", cfgFile, err)
	}
	var parsed struct {
		Infra *InfraConfig `yaml:"infra"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parse infra config %s: %w", cfgFile, err)
	}
	return parsed.Infra, nil
}

// resolveSecretsProvider constructs the appropriate secrets.Provider from cfg.
// ${VAR} / $VAR references in cfg.Config are expanded before the provider is
// constructed, so credentials can be passed through the environment (e.g.
// VAULT_TOKEN=s.xxx in CI) rather than hard-coded in YAML. cfg.Config is never
// mutated — expansion produces a deep copy.
func resolveSecretsProvider(cfg *SecretsConfig) (secrets.Provider, error) {
	return resolveSecretsProviderForEnv(cfg, "")
}

func resolveSecretsProviderForEnv(cfg *SecretsConfig, envName string) (secrets.Provider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("no secrets config provided")
	}
	c := expandSecretsConfigForEnv(cfg.Config, envName)
	if c == nil {
		c = map[string]any{}
	}
	switch cfg.Provider {
	case "github":
		repo, _ := c["repo"].(string)
		tokenVar, _ := c["token_env"].(string)
		if tokenVar == "" {
			tokenVar = "GITHUB_TOKEN" //nolint:gosec // G101: this is an env var name, not a credential
		}
		provider, err := secrets.NewGitHubSecretsProvider(repo, tokenVar)
		if err != nil {
			return nil, err
		}
		if environment, _ := c["environment"].(string); environment != "" {
			provider.SetEnvironment(environment)
		}
		return provider, nil

	case "vault":
		addr, _ := c["address"].(string)
		token, _ := c["token"].(string)
		mount, _ := c["mount_path"].(string)
		ns, _ := c["namespace"].(string)
		return secrets.NewVaultProvider(secrets.VaultConfig{
			Address:   addr,
			Token:     token,
			MountPath: mount,
			Namespace: ns,
		})

	case "aws":
		region, _ := c["region"].(string)
		return secrets.NewAWSSecretsManagerProvider(secrets.AWSConfig{Region: region})

	case "env":
		prefix, _ := c["prefix"].(string)
		return secrets.NewEnvProvider(prefix), nil

	case "keychain":
		service, _ := c["service"].(string)
		if service == "" {
			return nil, fmt.Errorf("secrets.keychain: 'service' is required")
		}
		return secrets.NewKeychainProvider(service)

	default:
		return nil, fmt.Errorf("unknown secrets provider %q (supported: github, vault, aws, env, keychain)", cfg.Provider)
	}
}

func expandSecretsConfigForEnv(m map[string]any, envName string) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = expandSecretConfigValueForEnv(v, envName)
	}
	return out
}

func expandSecretConfigValueForEnv(v any, envName string) any {
	switch val := v.(type) {
	case string:
		return os.Expand(val, func(key string) string {
			if key == "WORKFLOW_ENV" {
				return envName
			}
			return os.Getenv(key)
		})
	case map[string]any:
		return expandSecretsConfigForEnv(val, envName)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = expandSecretConfigValueForEnv(item, envName)
		}
		return out
	default:
		return v
	}
}

func secretsConfigFromStore(store *config.SecretStoreConfig) *SecretsConfig {
	if store == nil {
		return nil
	}
	provider := strings.TrimSpace(store.Provider)
	switch provider {
	case "aws-secrets-manager":
		provider = "aws"
	case "github-actions":
		provider = "github"
	}
	return &SecretsConfig{
		Provider: provider,
		Config:   store.Config,
	}
}

// buildAdhocProvider constructs a secrets.Provider for ad-hoc operations without
// requiring an app.yaml secrets block. Supports keychain, env, and aws.
// vault and github require explicit config via the app.yaml secrets block.
func buildAdhocProvider(name, service string) (secrets.Provider, error) {
	switch name {
	case "keychain":
		return secrets.NewKeychainProvider(service)
	case "env":
		return secrets.NewEnvProvider(service), nil
	case "aws":
		return secrets.NewAWSSecretsManagerProvider(secrets.AWSConfig{})
	case "vault", "github":
		return nil, fmt.Errorf("provider %q requires explicit config; use app.yaml secrets block", name)
	default:
		return nil, fmt.Errorf("unknown ad-hoc provider %q (supported: keychain, env, aws)", name)
	}
}
