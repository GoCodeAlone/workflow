package main

import (
	"fmt"
	"os"

	"github.com/GoCodeAlone/workflow/secrets"
	"gopkg.in/yaml.v3"
)

// SecretsConfig is the "secrets:" section of an infra config file.
type SecretsConfig struct {
	Provider string         `yaml:"provider"`
	Config   map[string]any `yaml:"config"`
	Generate []SecretGen    `yaml:"generate"`
}

// SecretGen describes a secret to generate and store.
type SecretGen struct {
	Key    string `yaml:"key"`
	Type   string `yaml:"type"`   // e.g. "random_hex", "provider_credential"
	Length int    `yaml:"length"` // for random generators
	Source string `yaml:"source"` // for provider_credential
}

// InfraConfig is the "infra:" top-level section of an infra config file.
type InfraConfig struct {
	AutoBootstrap *bool `yaml:"auto_bootstrap"`
}

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
func resolveSecretsProvider(cfg *SecretsConfig) (secrets.Provider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("no secrets config provided")
	}
	c := cfg.Config
	if c == nil {
		c = map[string]any{}
	}
	switch cfg.Provider {
	case "github":
		repo, _ := c["repo"].(string)
		tokenVar, _ := c["token_env"].(string)
		if tokenVar == "" {
			tokenVar = "GITHUB_TOKEN"
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

	default:
		return nil, fmt.Errorf("unknown secrets provider %q (supported: github, vault, aws, env)", cfg.Provider)
	}
}
