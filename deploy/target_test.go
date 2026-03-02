package deploy

import (
	"context"
	"io"
	"testing"
)

// mockDeployTarget implements DeployTarget for testing.
type mockDeployTarget struct {
	name string
}

func (m *mockDeployTarget) Name() string { return m.name }
func (m *mockDeployTarget) Generate(_ context.Context, _ *DeployRequest) (*DeployArtifacts, error) {
	return &DeployArtifacts{Target: m.name, AppName: "test"}, nil
}
func (m *mockDeployTarget) Apply(_ context.Context, _ *DeployArtifacts, _ ApplyOpts) (*DeployResult, error) {
	return &DeployResult{Status: "success"}, nil
}
func (m *mockDeployTarget) Destroy(_ context.Context, _, _ string) error { return nil }
func (m *mockDeployTarget) Status(_ context.Context, _, _ string) (*DeployStatus, error) {
	return &DeployStatus{Phase: "Running"}, nil
}
func (m *mockDeployTarget) Diff(_ context.Context, _ *DeployArtifacts) (string, error) {
	return "no changes\n", nil
}
func (m *mockDeployTarget) Logs(_ context.Context, _, _ string, _ LogOpts) (io.ReadCloser, error) {
	return nil, nil
}

func TestDeployTargetRegistry_RegisterAndGet(t *testing.T) {
	r := NewDeployTargetRegistry()
	target := &mockDeployTarget{name: "test-target"}
	r.Register(target)

	got, ok := r.Get("test-target")
	if !ok {
		t.Fatal("expected to find registered target")
	}
	if got.Name() != "test-target" {
		t.Errorf("got name %q, want %q", got.Name(), "test-target")
	}
}

func TestDeployTargetRegistry_GetNotFound(t *testing.T) {
	r := NewDeployTargetRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Fatal("expected not to find unregistered target")
	}
}

func TestDeployTargetRegistry_List(t *testing.T) {
	r := NewDeployTargetRegistry()
	r.Register(&mockDeployTarget{name: "b"})
	r.Register(&mockDeployTarget{name: "a"})
	names := r.List()
	if len(names) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(names))
	}
}

func TestDeployTargetRegistry_RegisterNil(t *testing.T) {
	r := NewDeployTargetRegistry()
	r.Register(nil) // should not panic
	if len(r.List()) != 0 {
		t.Fatal("expected empty registry after registering nil")
	}
}

func TestDeployTargetRegistry_Generate(t *testing.T) {
	r := NewDeployTargetRegistry()
	r.Register(&mockDeployTarget{name: "test"})

	artifacts, err := r.Generate(context.Background(), "test", &DeployRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if artifacts.Target != "test" {
		t.Errorf("got target %q, want %q", artifacts.Target, "test")
	}
}

func TestDeployTargetRegistry_GenerateUnknown(t *testing.T) {
	r := NewDeployTargetRegistry()
	_, err := r.Generate(context.Background(), "unknown", &DeployRequest{})
	if err == nil {
		t.Fatal("expected error for unknown target")
	}
}
