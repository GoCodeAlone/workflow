package modularcompat

import (
	"testing"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

func TestNew(t *testing.T) {
	p := New()
	if p.Name() != "modular-compat" {
		t.Fatalf("expected name modular-compat, got %s", p.Name())
	}
	if p.Version() != "1.0.0" {
		t.Fatalf("expected version 1.0.0, got %s", p.Version())
	}
}

func TestManifestValidates(t *testing.T) {
	p := New()
	m := p.EngineManifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("manifest validation failed: %v", err)
	}
}

func TestModuleFactories(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	for _, name := range []string{
		"cache.modular",
		"chimux.router",
		"httpclient.modular",
		"httpserver.modular",
		"jsonschema.modular",
		"letsencrypt.modular",
		"logmasker.modular",
		"scheduler.modular",
	} {
		if _, ok := factories[name]; !ok {
			t.Errorf("missing module factory: %s", name)
		}
	}
	if len(factories) != 8 {
		t.Errorf("expected 8 module factories, got %d", len(factories))
	}
}

func TestSchedulerModuleFactory(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()
	mod := factories["scheduler.modular"]("test-sched", nil)
	if mod == nil {
		t.Fatal("scheduler.modular factory returned nil")
	}
}

func TestCacheModuleFactory(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()
	mod := factories["cache.modular"]("test-cache", nil)
	if mod == nil {
		t.Fatal("cache.modular factory returned nil")
	}
}

func TestChimuxModuleFactory(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()
	mod := factories["chimux.router"]("test-chimux", nil)
	if mod == nil {
		t.Fatal("chimux.router factory returned nil")
	}
}

func TestHTTPClientModuleFactory(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()
	mod := factories["httpclient.modular"]("test-httpclient", nil)
	if mod == nil {
		t.Fatal("httpclient.modular factory returned nil")
	}
}

func TestHTTPServerModuleFactory(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()
	mod := factories["httpserver.modular"]("test-httpserver", nil)
	if mod == nil {
		t.Fatal("httpserver.modular factory returned nil")
	}
}

func TestJSONSchemaModuleFactory(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()
	mod := factories["jsonschema.modular"]("test-jsonschema", nil)
	if mod == nil {
		t.Fatal("jsonschema.modular factory returned nil")
	}
}

func TestLetsEncryptModuleFactory(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()
	mod := factories["letsencrypt.modular"]("test-letsencrypt", map[string]any{
		"email":        "test@example.com",
		"storage_path": "/tmp/certs",
		"use_staging":  true,
		"domains":      []any{"example.com"},
	})
	if mod == nil {
		t.Fatal("letsencrypt.modular factory returned nil")
	}
}

func TestLogMaskerModuleFactory(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()
	mod := factories["logmasker.modular"]("test-logmasker", nil)
	if mod == nil {
		t.Fatal("logmasker.modular factory returned nil")
	}
}

func TestPluginLoads(t *testing.T) {
	p := New()
	loader := plugin.NewPluginLoader(capability.NewRegistry(), schema.NewModuleSchemaRegistry())
	if err := loader.LoadPlugin(p); err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}

	modules := loader.ModuleFactories()
	if len(modules) != 8 {
		t.Fatalf("expected 8 module factories after load, got %d", len(modules))
	}
}
