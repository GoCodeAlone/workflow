package main

import (
	"fmt"
	"os"

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

// parseSecretsConfig reads the "secrets:" top-level key from a YAML file.
// Returns nil, nil if the section is absent.
func parseSecretsConfig(cfgFile string) (*SecretsConfig, error) {
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", cfgFile, err)
	}
	var parsed struct {
		Secrets *SecretsConfig `yaml:"secrets"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parse secrets config %s: %w", cfgFile, err)
	}
	return parsed.Secrets, nil
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
// ${VAR} / $VAR references in cfg.Config are expanded via os.ExpandEnv before
// the provider is constructed, so credentials can be passed through the environment
// (e.g. VAULT_TOKEN=s.xxx in CI) rather than hard-coded in YAML. cfg.Config is
// never mutated — expansion produces a deep copy.
func resolveSecretsProvider(cfg *SecretsConfig) (secrets.Provider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("no secrets config provided")
	}
	c := config.ExpandEnvInMap(cfg.Config)
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
		return secrets.NewGitHubSecretsProvider(repo, tokenVar)

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
