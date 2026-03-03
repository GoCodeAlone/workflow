package imageloader

import (
	"testing"
)

type mockLoader struct {
	runtime  Runtime
	loadErr  error
	loaded   bool
	validErr error
}

func (m *mockLoader) Type() Runtime   { return m.runtime }
func (m *mockLoader) Validate() error { return m.validErr }
func (m *mockLoader) Load(cfg *LoadConfig) error {
	m.loaded = true
	return m.loadErr
}

func TestRegistryDispatch(t *testing.T) {
	reg := NewRegistry()
	mk := &mockLoader{runtime: RuntimeMinikube}
	kd := &mockLoader{runtime: RuntimeKind}
	reg.Register(mk)
	reg.Register(kd)

	cfg := &LoadConfig{Image: "test:v1", Runtime: RuntimeMinikube}
	if err := reg.Load(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mk.loaded {
		t.Fatal("minikube loader was not called")
	}
	if kd.loaded {
		t.Fatal("kind loader should not have been called")
	}
}

func TestRegistryUnknownRuntime(t *testing.T) {
	reg := NewRegistry()
	cfg := &LoadConfig{Image: "test:v1", Runtime: "unknown"}
	if err := reg.Load(cfg); err == nil {
		t.Fatal("expected error for unknown runtime")
	}
}

func TestRegistryValidationFailure(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockLoader{
		runtime:  RuntimeKind,
		validErr: errMissing("kind"),
	})

	cfg := &LoadConfig{Image: "test:v1", Runtime: RuntimeKind}
	err := reg.Load(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func errMissing(name string) error {
	return &missingBinaryError{name: name}
}

type missingBinaryError struct{ name string }

func (e *missingBinaryError) Error() string { return e.name + " not found in PATH" }

func TestRemoteRequiresRegistry(t *testing.T) {
	r := NewRemote()
	cfg := &LoadConfig{Image: "test:v1", Runtime: RuntimeRemote}
	err := r.Load(cfg)
	if err == nil {
		t.Fatal("expected error when registry is empty")
	}
}

func TestDockerDesktopIsNoOp(t *testing.T) {
	d := NewDockerDesktop()
	if d.Type() != RuntimeDockerDesktop {
		t.Fatalf("expected %q, got %q", RuntimeDockerDesktop, d.Type())
	}
	// Load should succeed without external dependencies (no-op)
	// Only test if docker is available
	if err := d.Validate(); err != nil {
		t.Skip("docker not available")
	}
	cfg := &LoadConfig{Image: "test:v1", Runtime: RuntimeDockerDesktop}
	if err := d.Load(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
