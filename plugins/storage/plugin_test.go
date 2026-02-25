package storage

import (
	"testing"

	"github.com/GoCodeAlone/workflow/plugin"
)

func TestPluginImplementsEnginePlugin(t *testing.T) {
	p := New()
	var _ plugin.EnginePlugin = p
}

func TestPluginManifest(t *testing.T) {
	p := New()
	m := p.EngineManifest()

	if err := m.Validate(); err != nil {
		t.Fatalf("manifest validation failed: %v", err)
	}
	if m.Name != "storage" {
		t.Errorf("expected name %q, got %q", "storage", m.Name)
	}
	if len(m.ModuleTypes) != 7 {
		t.Errorf("expected 7 module types, got %d", len(m.ModuleTypes))
	}
	if len(m.StepTypes) != 0 {
		t.Errorf("expected 0 step types, got %d", len(m.StepTypes))
	}
}

func TestPluginCapabilities(t *testing.T) {
	p := New()
	caps := p.Capabilities()
	if len(caps) != 4 {
		t.Fatalf("expected 4 capabilities, got %d", len(caps))
	}
	names := map[string]bool{}
	for _, c := range caps {
		names[c.Name] = true
	}
	for _, expected := range []string{"storage", "database", "persistence", "cache"} {
		if !names[expected] {
			t.Errorf("missing capability %q", expected)
		}
	}
}

func TestModuleFactories(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	expectedTypes := []string{
		"storage.s3", "storage.local", "storage.gcs",
		"storage.sqlite", "database.workflow", "persistence.store",
		"cache.redis",
	}
	for _, typ := range expectedTypes {
		factory, ok := factories[typ]
		if !ok {
			t.Errorf("missing factory for %q", typ)
			continue
		}
		mod := factory("test-"+typ, map[string]any{})
		if mod == nil {
			t.Errorf("factory for %q returned nil", typ)
		}
	}
}

func TestModuleFactoryWithConfig(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	// storage.s3 with config
	mod := factories["storage.s3"]("s3-test", map[string]any{
		"bucket":   "test-bucket",
		"region":   "eu-west-1",
		"endpoint": "http://localhost:9000",
	})
	if mod == nil {
		t.Fatal("storage.s3 factory returned nil with config")
	}

	// storage.sqlite with config
	mod = factories["storage.sqlite"]("sqlite-test", map[string]any{
		"dbPath":         "test.db",
		"maxConnections": float64(10),
		"walMode":        false,
	})
	if mod == nil {
		t.Fatal("storage.sqlite factory returned nil with config")
	}

	// database.workflow with config
	mod = factories["database.workflow"]("db-test", map[string]any{
		"driver":       "sqlite3",
		"dsn":          ":memory:",
		"maxOpenConns": float64(10),
		"maxIdleConns": float64(2),
	})
	if mod == nil {
		t.Fatal("database.workflow factory returned nil with config")
	}

	// persistence.store with config
	mod = factories["persistence.store"]("persist-test", map[string]any{
		"database": "my-db",
	})
	if mod == nil {
		t.Fatal("persistence.store factory returned nil with config")
	}

	// storage.gcs with config
	mod = factories["storage.gcs"]("gcs-test", map[string]any{
		"bucket":          "test-bucket",
		"project":         "test-project",
		"credentialsFile": "/tmp/creds.json",
	})
	if mod == nil {
		t.Fatal("storage.gcs factory returned nil with config")
	}
}

func TestStepFactories(t *testing.T) {
	p := New()
	stepFactories := p.StepFactories()

	if len(stepFactories) != 0 {
		t.Fatalf("expected 0 step factories (moved to pipelinesteps plugin), got %d", len(stepFactories))
	}
}

func TestModuleSchemas(t *testing.T) {
	p := New()
	schemas := p.ModuleSchemas()
	if len(schemas) != 7 {
		t.Fatalf("expected 7 module schemas, got %d", len(schemas))
	}

	types := map[string]bool{}
	for _, s := range schemas {
		types[s.Type] = true
	}
	expectedTypes := []string{
		"storage.s3", "storage.local", "storage.gcs",
		"storage.sqlite", "database.workflow", "persistence.store",
		"cache.redis",
	}
	for _, expected := range expectedTypes {
		if !types[expected] {
			t.Errorf("missing schema for %q", expected)
		}
	}
}

func TestCacheRedisFactory(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	factory, ok := factories["cache.redis"]
	if !ok {
		t.Fatal("missing factory for cache.redis")
	}

	// Default config
	mod := factory("cache", map[string]any{})
	if mod == nil {
		t.Fatal("cache.redis factory returned nil with empty config")
	}

	// Full config
	mod = factory("cache", map[string]any{
		"address":    "redis:6379",
		"password":   "secret",
		"db":         float64(1),
		"prefix":     "myapp:",
		"defaultTTL": "30m",
	})
	if mod == nil {
		t.Fatal("cache.redis factory returned nil with full config")
	}
}
