package deploy

import (
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

type mockSidecarProvider struct {
	typeName    string
	validateErr error
	resolveSpec *SidecarSpec
}

func (m *mockSidecarProvider) Type() string { return m.typeName }
func (m *mockSidecarProvider) Validate(_ config.SidecarConfig) error {
	return m.validateErr
}
func (m *mockSidecarProvider) Resolve(cfg config.SidecarConfig, _ string) (*SidecarSpec, error) {
	if m.resolveSpec != nil {
		return m.resolveSpec, nil
	}
	return &SidecarSpec{Name: cfg.Name}, nil
}

func TestSidecarRegistry_RegisterAndGet(t *testing.T) {
	r := NewSidecarRegistry()
	p := &mockSidecarProvider{typeName: "sidecar.test"}
	r.Register(p)

	got, ok := r.Get("sidecar.test")
	if !ok {
		t.Fatal("expected to find registered provider")
	}
	if got.Type() != "sidecar.test" {
		t.Errorf("got type %q, want %q", got.Type(), "sidecar.test")
	}
}

func TestSidecarRegistry_GetNotFound(t *testing.T) {
	r := NewSidecarRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Fatal("expected not to find unregistered provider")
	}
}

func TestSidecarRegistry_RegisterNil(t *testing.T) {
	r := NewSidecarRegistry()
	r.Register(nil) // should not panic
}

func TestSidecarRegistry_Resolve(t *testing.T) {
	r := NewSidecarRegistry()
	r.Register(&mockSidecarProvider{
		typeName:    "sidecar.test",
		resolveSpec: &SidecarSpec{Name: "my-sidecar", K8s: &K8sSidecarSpec{Image: "test:v1"}},
	})

	sidecars := []config.SidecarConfig{
		{Name: "my-sidecar", Type: "sidecar.test", Config: map[string]any{"key": "val"}},
	}

	specs, err := r.Resolve(sidecars, "kubernetes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	if specs[0].Name != "my-sidecar" {
		t.Errorf("got name %q, want %q", specs[0].Name, "my-sidecar")
	}
	if specs[0].K8s == nil || specs[0].K8s.Image != "test:v1" {
		t.Error("expected K8s spec with image test:v1")
	}
}

func TestSidecarRegistry_ResolveUnknownType(t *testing.T) {
	r := NewSidecarRegistry()
	sidecars := []config.SidecarConfig{
		{Name: "bad", Type: "sidecar.unknown"},
	}
	_, err := r.Resolve(sidecars, "kubernetes")
	if err == nil {
		t.Fatal("expected error for unknown sidecar type")
	}
}
