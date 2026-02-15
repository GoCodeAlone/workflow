package workflow

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/secrets"
)

func TestExpandConfigStrings_EnvVars(t *testing.T) {
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")

	resolver := secrets.NewMultiResolver()
	cfg := map[string]any{
		"host":   "${DB_HOST}",
		"port":   "${DB_PORT}",
		"driver": "postgres",
		"count":  42,
	}

	expandConfigStrings(resolver, cfg)

	if cfg["host"] != "localhost" {
		t.Errorf("expected 'localhost', got %v", cfg["host"])
	}
	if cfg["port"] != "5432" {
		t.Errorf("expected '5432', got %v", cfg["port"])
	}
	if cfg["driver"] != "postgres" {
		t.Errorf("expected 'postgres', got %v", cfg["driver"])
	}
	if cfg["count"] != 42 {
		t.Errorf("expected 42, got %v", cfg["count"])
	}
}

func TestExpandConfigStrings_EnvScheme(t *testing.T) {
	t.Setenv("API_KEY", "key123")

	resolver := secrets.NewMultiResolver()
	cfg := map[string]any{
		"apiKey": "${env:API_KEY}",
	}

	expandConfigStrings(resolver, cfg)

	if cfg["apiKey"] != "key123" {
		t.Errorf("expected 'key123', got %v", cfg["apiKey"])
	}
}

func TestExpandConfigStrings_CustomProvider(t *testing.T) {
	resolver := secrets.NewMultiResolver()
	resolver.Register("mock", &mockSecretProvider{
		secrets: map[string]string{
			"db/password": "s3cret",
		},
	})

	cfg := map[string]any{
		"password": "${mock:db/password}",
		"host":     "plain-value",
	}

	expandConfigStrings(resolver, cfg)

	if cfg["password"] != "s3cret" {
		t.Errorf("expected 's3cret', got %v", cfg["password"])
	}
	if cfg["host"] != "plain-value" {
		t.Errorf("expected 'plain-value', got %v", cfg["host"])
	}
}

func TestExpandConfigStrings_NestedMaps(t *testing.T) {
	t.Setenv("NESTED_VAL", "resolved")

	resolver := secrets.NewMultiResolver()
	cfg := map[string]any{
		"outer": map[string]any{
			"inner": "${NESTED_VAL}",
			"plain": "no-change",
		},
	}

	expandConfigStrings(resolver, cfg)

	inner := cfg["outer"].(map[string]any)
	if inner["inner"] != "resolved" {
		t.Errorf("expected 'resolved', got %v", inner["inner"])
	}
	if inner["plain"] != "no-change" {
		t.Errorf("expected 'no-change', got %v", inner["plain"])
	}
}

func TestExpandConfigStrings_ArrayValues(t *testing.T) {
	t.Setenv("ORIGIN1", "https://example.com")

	resolver := secrets.NewMultiResolver()
	cfg := map[string]any{
		"origins": []any{"${ORIGIN1}", "https://static.example.com"},
	}

	expandConfigStrings(resolver, cfg)

	origins := cfg["origins"].([]any)
	if origins[0] != "https://example.com" {
		t.Errorf("expected 'https://example.com', got %v", origins[0])
	}
	if origins[1] != "https://static.example.com" {
		t.Errorf("expected 'https://static.example.com', got %v", origins[1])
	}
}

func TestExpandConfigStrings_UnresolvablePreserved(t *testing.T) {
	resolver := secrets.NewMultiResolver()
	cfg := map[string]any{
		"value": "${NONEXISTENT_VAR_XYZ_999}",
	}

	expandConfigStrings(resolver, cfg)

	// Should preserve original value on error
	if cfg["value"] != "${NONEXISTENT_VAR_XYZ_999}" {
		t.Errorf("expected original value preserved, got %v", cfg["value"])
	}
}

func TestExpandConfigStrings_NilResolver(t *testing.T) {
	cfg := map[string]any{
		"key": "${VALUE}",
	}

	// Should not panic
	expandConfigStrings(nil, cfg)

	if cfg["key"] != "${VALUE}" {
		t.Errorf("expected original value, got %v", cfg["key"])
	}
}

func TestExpandConfigStrings_NilConfig(t *testing.T) {
	resolver := secrets.NewMultiResolver()

	// Should not panic
	expandConfigStrings(resolver, nil)
}

func TestEngine_SecretsResolver_Available(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	resolver := engine.SecretsResolver()
	if resolver == nil {
		t.Fatal("expected non-nil secrets resolver")
	}

	// Env provider should be registered by default
	if resolver.Provider("env") == nil {
		t.Error("expected env provider to be registered by default")
	}
}

func TestEngine_SecretsResolver_Integration(t *testing.T) {
	t.Setenv("JWT_SECRET_KEY", "my-jwt-secret-value")

	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "auth",
				Type: "auth.jwt",
				Config: map[string]any{
					"secret":      "${JWT_SECRET_KEY}",
					"tokenExpiry": "1h",
				},
			},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	err := engine.BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}

	// Verify the config was expanded
	if cfg.Modules[0].Config["secret"] != "my-jwt-secret-value" {
		t.Errorf("expected JWT secret to be expanded to 'my-jwt-secret-value', got %v",
			cfg.Modules[0].Config["secret"])
	}
}

func TestEngine_SecretsResolver_CustomProviderRegistration(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	// Register a custom provider before building
	engine.SecretsResolver().Register("custom", &mockSecretProvider{
		secrets: map[string]string{
			"api-token": "custom-token-value",
		},
	})

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "auth",
				Type: "auth.jwt",
				Config: map[string]any{
					"secret":      "${custom:api-token}",
					"tokenExpiry": "1h",
				},
			},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	err := engine.BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}

	// Verify the custom provider was used
	if cfg.Modules[0].Config["secret"] != "custom-token-value" {
		t.Errorf("expected 'custom-token-value', got %v", cfg.Modules[0].Config["secret"])
	}
}

func TestEngine_SecretsResolver_MixedExpansion(t *testing.T) {
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")

	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	engine.SecretsResolver().Register("vault", &mockSecretProvider{
		secrets: map[string]string{
			"secret/db#password": "vault-password",
		},
	})

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "db",
				Type: "database.workflow",
				Config: map[string]any{
					"driver":       "postgres",
					"dsn":          "postgres://user:${vault:secret/db#password}@${DB_HOST}:${DB_PORT}/mydb",
					"maxOpenConns": float64(10),
				},
			},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	err := engine.BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}

	expected := "postgres://user:vault-password@localhost:5432/mydb"
	if cfg.Modules[0].Config["dsn"] != expected {
		t.Errorf("expected %q, got %v", expected, cfg.Modules[0].Config["dsn"])
	}
}

// mockSecretProvider is a simple in-memory provider for testing.
type mockSecretProvider struct {
	secrets map[string]string
}

func (p *mockSecretProvider) Name() string { return "mock" }

func (p *mockSecretProvider) Get(_ context.Context, key string) (string, error) {
	v, ok := p.secrets[key]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return v, nil
}

func (p *mockSecretProvider) Set(_ context.Context, key, value string) error {
	p.secrets[key] = value
	return nil
}

func (p *mockSecretProvider) Delete(_ context.Context, key string) error {
	delete(p.secrets, key)
	return nil
}

func (p *mockSecretProvider) List(_ context.Context) ([]string, error) {
	keys := make([]string, 0, len(p.secrets))
	for k := range p.secrets {
		keys = append(keys, k)
	}
	return keys, nil
}
