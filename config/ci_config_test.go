package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCIConfig_ParseYAML(t *testing.T) {
	yamlStr := `
ci:
  build:
    binaries:
      - name: server
        path: ./cmd/server
        os: [linux]
        arch: [amd64, arm64]
  test:
    unit:
      command: go test ./... -race
  deploy:
    environments:
      staging:
        provider: aws-ecs
        strategy: rolling
secrets:
  provider: env
  entries:
    - name: DATABASE_URL
      description: PostgreSQL connection string
environments:
  local:
    provider: docker
    envVars:
      LOG_LEVEL: debug
`
	var cfg WorkflowConfig
	if err := yaml.Unmarshal([]byte(yamlStr), &cfg); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if cfg.CI == nil {
		t.Fatal("ci section missing")
	}
	if len(cfg.CI.Build.Targets) != 1 {
		t.Fatalf("expected 1 target (coerced from binaries:), got %d", len(cfg.CI.Build.Targets))
	}
	if cfg.CI.Build.Targets[0].Name != "server" {
		t.Errorf("expected 'server', got %q", cfg.CI.Build.Targets[0].Name)
	}
	if cfg.CI.Test.Unit.Command != "go test ./... -race" {
		t.Errorf("unexpected test command: %q", cfg.CI.Test.Unit.Command)
	}
	if cfg.CI.Deploy.Environments["staging"].Provider != "aws-ecs" {
		t.Errorf("unexpected provider: %q", cfg.CI.Deploy.Environments["staging"].Provider)
	}
	if cfg.Secrets == nil {
		t.Fatal("secrets section missing")
	}
	if cfg.Secrets.Provider != "env" {
		t.Errorf("expected env provider, got %q", cfg.Secrets.Provider)
	}
	if len(cfg.Secrets.Entries) != 1 {
		t.Fatalf("expected 1 secret entry, got %d", len(cfg.Secrets.Entries))
	}
	if cfg.Environments == nil {
		t.Fatal("environments section missing")
	}
	if cfg.Environments["local"].Provider != "docker" {
		t.Errorf("expected docker provider, got %q", cfg.Environments["local"].Provider)
	}
	if cfg.Environments["local"].EnvVars["LOG_LEVEL"] != "debug" {
		t.Errorf("expected debug log level, got %q", cfg.Environments["local"].EnvVars["LOG_LEVEL"])
	}
}

func TestCIMigrationsConfigParsesValidationOptions(t *testing.T) {
	data := []byte(`
version: 1
ci:
  migrations:
    - name: app
      plugin: workflow-plugin-migrations
      driver: golang-migrate
      source_dir: migrations
      database:
        env: DATABASE_URL
      baseline:
        ref: origin/main
        mode: apply-before-candidate
      validation:
        lint: true
        fresh_cycle: true
        baseline_candidate: true
        forbid_dirty: true
      environments:
        staging:
          source_dir: migrations/staging
          database:
            env: STAGING_DATABASE_URL
`)
	var cfg WorkflowConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	got := cfg.CI.Migrations[0]
	if got.Name != "app" || got.Plugin != "workflow-plugin-migrations" || got.Driver != "golang-migrate" {
		t.Fatalf("unexpected migration config: %+v", got)
	}
	if got.Database.Env != "DATABASE_URL" {
		t.Fatalf("database env = %q", got.Database.Env)
	}
	if !got.Validation.FreshCycle || !got.Validation.BaselineCandidate || !got.Validation.ForbidDirty {
		t.Fatalf("validation flags not parsed: %+v", got.Validation)
	}
	if got.Environments["staging"].SourceDir != "migrations/staging" {
		t.Fatalf("staging source_dir = %q", got.Environments["staging"].SourceDir)
	}
	if got.Environments["staging"].Database.Env != "STAGING_DATABASE_URL" {
		t.Fatalf("staging database env = %q", got.Environments["staging"].Database.Env)
	}
}

func TestCIConfig_Validate(t *testing.T) {
	t.Run("nil config is valid", func(t *testing.T) {
		var c *CIConfig
		if err := c.Validate(); err != nil {
			t.Errorf("nil CIConfig.Validate() should return nil, got %v", err)
		}
	})

	t.Run("binary missing name", func(t *testing.T) {
		c := &CIConfig{
			Build: &CIBuildConfig{
				Targets: []CITarget{
					{Name: "", Type: "go", Path: "./cmd/server"},
				},
			},
		}
		if err := c.Validate(); err == nil {
			t.Error("expected error for binary with empty name")
		}
	})

	t.Run("binary missing path", func(t *testing.T) {
		c := &CIConfig{
			Build: &CIBuildConfig{
				Targets: []CITarget{
					{Name: "server", Type: "go", Path: ""},
				},
			},
		}
		if err := c.Validate(); err == nil {
			t.Error("expected error for binary with empty path")
		}
	})

	t.Run("deploy env missing provider", func(t *testing.T) {
		c := &CIConfig{
			Deploy: &CIDeployConfig{
				Environments: map[string]*CIDeployEnvironment{
					"staging": {Provider: ""},
				},
			},
		}
		if err := c.Validate(); err == nil {
			t.Error("expected error for deploy env with empty provider")
		}
	})

	t.Run("valid config passes", func(t *testing.T) {
		c := &CIConfig{
			Build: &CIBuildConfig{
				Targets: []CITarget{
					{Name: "server", Type: "go", Path: "./cmd/server"},
				},
			},
			Deploy: &CIDeployConfig{
				Environments: map[string]*CIDeployEnvironment{
					"staging": {Provider: "aws-ecs"},
				},
			},
		}
		if err := c.Validate(); err != nil {
			t.Errorf("unexpected error for valid config: %v", err)
		}
	})
}

func TestSecretsRotationConfig_Strategy(t *testing.T) {
	yamlStr := `
secrets:
  provider: vault
  rotation:
    enabled: true
    interval: 30d
    strategy: dual-credential
`
	var cfg WorkflowConfig
	if err := yaml.Unmarshal([]byte(yamlStr), &cfg); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if cfg.Secrets == nil {
		t.Fatal("secrets section missing")
	}
	if cfg.Secrets.Rotation == nil {
		t.Fatal("rotation config missing")
	}
	if cfg.Secrets.Rotation.Strategy != "dual-credential" {
		t.Errorf("expected dual-credential strategy, got %q", cfg.Secrets.Rotation.Strategy)
	}
}
