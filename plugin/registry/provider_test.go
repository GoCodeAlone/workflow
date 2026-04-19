package registry_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/plugin/registry"
)

type mockProvider struct{ name string }

func (m *mockProvider) Name() string                                              { return m.name }
func (m *mockProvider) Login(_ registry.Context, _ registry.ProviderConfig) error { return nil }
func (m *mockProvider) Push(_ registry.Context, _ registry.ProviderConfig, _ string) error {
	return nil
}
func (m *mockProvider) Prune(_ registry.Context, _ registry.ProviderConfig) error { return nil }

func TestRegistry_RegisterAndGet(t *testing.T) {
	registry.Register(&mockProvider{name: "test"})

	p, ok := registry.Get("test")
	if !ok {
		t.Fatal("want registered provider, got not found")
	}
	if p.Name() != "test" {
		t.Fatalf("want name=test, got %q", p.Name())
	}
}

func TestRegistry_GetUnknown(t *testing.T) {
	_, ok := registry.Get("no-such-provider")
	if ok {
		t.Fatal("want false for unknown provider")
	}
}

func TestRegistry_List(t *testing.T) {
	registry.Register(&mockProvider{name: "list-test"})
	providers := registry.List()
	for _, p := range providers {
		if p.Name() == "list-test" {
			return
		}
	}
	t.Fatal("registered provider not found in List()")
}
