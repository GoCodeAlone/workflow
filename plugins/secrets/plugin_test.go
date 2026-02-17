package secrets

import (
	"testing"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

func TestNew(t *testing.T) {
	p := New()
	if p.Name() != "secrets" {
		t.Fatalf("expected name secrets, got %s", p.Name())
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

	for _, name := range []string{"secrets.vault", "secrets.aws"} {
		if _, ok := factories[name]; !ok {
			t.Errorf("missing module factory: %s", name)
		}
	}
	if len(factories) != 2 {
		t.Errorf("expected 2 module factories, got %d", len(factories))
	}
}

func TestVaultModuleFactory(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()
	factory := factories["secrets.vault"]

	mod := factory("my-vault", map[string]any{
		"address":   "http://localhost:8200",
		"token":     "test-token",
		"mountPath": "kv",
		"namespace": "test-ns",
	})
	if mod == nil {
		t.Fatal("vault factory returned nil")
	}
	if mod.Name() != "my-vault" {
		t.Errorf("expected name my-vault, got %s", mod.Name())
	}
}

func TestAWSModuleFactory(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()
	factory := factories["secrets.aws"]

	mod := factory("my-aws", map[string]any{
		"region":          "us-west-2",
		"accessKeyId":     "AKIA-test",
		"secretAccessKey": "secret-test",
	})
	if mod == nil {
		t.Fatal("aws factory returned nil")
	}
	if mod.Name() != "my-aws" {
		t.Errorf("expected name my-aws, got %s", mod.Name())
	}
}

func TestPluginLoads(t *testing.T) {
	p := New()
	loader := plugin.NewPluginLoader(capability.NewRegistry(), schema.NewModuleSchemaRegistry())
	if err := loader.LoadPlugin(p); err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}

	modules := loader.ModuleFactories()
	if len(modules) != 2 {
		t.Fatalf("expected 2 module factories after load, got %d", len(modules))
	}
}
