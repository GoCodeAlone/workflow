package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// stubInvoker is a test double for remoteServiceInvoker that records calls
// and returns a preset response map.
type stubInvoker struct {
	method string
	args   map[string]any
	resp   map[string]any
	err    error
}

func (s *stubInvoker) InvokeService(method string, args map[string]any) (map[string]any, error) {
	s.method = method
	s.args = args
	return s.resp, s.err
}

// sampleOutputMap returns a populated ResourceOutput-shaped map for testing.
func sampleOutputMap() map[string]any {
	return map[string]any{
		"provider_id": "pid-123",
		"name":        "my-resource",
		"type":        "container_service",
		"status":      "running",
		"outputs":     map[string]any{"endpoint": "https://example.com"},
		"sensitive":   map[string]any{"endpoint": true},
	}
}

func sampleRef() interfaces.ResourceRef {
	return interfaces.ResourceRef{
		Name:       "my-resource",
		Type:       "container_service",
		ProviderID: "pid-123",
	}
}

func sampleSpec() interfaces.ResourceSpec {
	return interfaces.ResourceSpec{
		Name:   "my-resource",
		Type:   "container_service",
		Config: map[string]any{"image": "myapp:v1"},
	}
}

func newDriver(si *stubInvoker) *remoteResourceDriver {
	return &remoteResourceDriver{invoker: si, resourceType: "container_service"}
}

// ── decodeResourceOutput ──────────────────────────────────────────────────────

func TestRemoteDriver_OutputsDecoded(t *testing.T) {
	m := sampleOutputMap()
	out := decodeResourceOutput(m)
	if out.ProviderID != "pid-123" {
		t.Errorf("ProviderID: got %q", out.ProviderID)
	}
	if out.Name != "my-resource" {
		t.Errorf("Name: got %q", out.Name)
	}
	if out.Type != "container_service" {
		t.Errorf("Type: got %q", out.Type)
	}
	if out.Status != "running" {
		t.Errorf("Status: got %q", out.Status)
	}
	if out.Outputs["endpoint"] != "https://example.com" {
		t.Errorf("Outputs[endpoint]: got %v", out.Outputs["endpoint"])
	}
	if !out.Sensitive["endpoint"] {
		t.Error("Sensitive[endpoint]: expected true")
	}
}

// ── Create ────────────────────────────────────────────────────────────────────

func TestRemoteDriver_Create(t *testing.T) {
	si := &stubInvoker{resp: sampleOutputMap()}
	d := newDriver(si)
	spec := sampleSpec()

	out, err := d.Create(context.Background(), spec)
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}
	if si.method != "ResourceDriver.Create" {
		t.Errorf("method: got %q, want ResourceDriver.Create", si.method)
	}
	// Verify arg keys
	for _, key := range []string{"resource_type", "spec_name", "spec_type", "spec_config"} {
		if _, ok := si.args[key]; !ok {
			t.Errorf("missing arg key %q", key)
		}
	}
	if si.args["resource_type"] != "container_service" {
		t.Errorf("resource_type: got %v", si.args["resource_type"])
	}
	if si.args["spec_name"] != spec.Name {
		t.Errorf("spec_name: got %v", si.args["spec_name"])
	}
	if out.ProviderID != "pid-123" {
		t.Errorf("ProviderID: got %q", out.ProviderID)
	}
	if out.Outputs["endpoint"] != "https://example.com" {
		t.Errorf("Outputs not decoded: %v", out.Outputs)
	}
}

func TestRemoteDriver_Create_Error(t *testing.T) {
	si := &stubInvoker{err: fmt.Errorf("rpc error")}
	d := newDriver(si)
	_, err := d.Create(context.Background(), sampleSpec())
	if err == nil {
		t.Fatal("expected error")
	}
}

// ── Read ──────────────────────────────────────────────────────────────────────

func TestRemoteDriver_Read(t *testing.T) {
	si := &stubInvoker{resp: sampleOutputMap()}
	d := newDriver(si)
	ref := sampleRef()

	out, err := d.Read(context.Background(), ref)
	if err != nil {
		t.Fatalf("Read: unexpected error: %v", err)
	}
	if si.method != "ResourceDriver.Read" {
		t.Errorf("method: got %q, want ResourceDriver.Read", si.method)
	}
	for _, key := range []string{"resource_type", "ref_name", "ref_type", "ref_provider_id"} {
		if _, ok := si.args[key]; !ok {
			t.Errorf("missing arg key %q", key)
		}
	}
	if si.args["ref_name"] != ref.Name {
		t.Errorf("ref_name: got %v", si.args["ref_name"])
	}
	if out.ProviderID != "pid-123" {
		t.Errorf("ProviderID: got %q", out.ProviderID)
	}
}

// ── Update ────────────────────────────────────────────────────────────────────

func TestRemoteDriver_Update(t *testing.T) {
	si := &stubInvoker{resp: sampleOutputMap()}
	d := newDriver(si)
	ref := sampleRef()
	spec := sampleSpec()

	out, err := d.Update(context.Background(), ref, spec)
	if err != nil {
		t.Fatalf("Update: unexpected error: %v", err)
	}
	if si.method != "ResourceDriver.Update" {
		t.Errorf("method: got %q, want ResourceDriver.Update", si.method)
	}
	// Update must also decode outputs/sensitive
	if out.Outputs["endpoint"] != "https://example.com" {
		t.Errorf("Update: Outputs not decoded: %v", out.Outputs)
	}
	if !out.Sensitive["endpoint"] {
		t.Error("Update: Sensitive[endpoint]: expected true")
	}
}

// ── Delete ────────────────────────────────────────────────────────────────────

func TestRemoteDriver_Delete(t *testing.T) {
	si := &stubInvoker{resp: map[string]any{}}
	d := newDriver(si)
	ref := sampleRef()

	err := d.Delete(context.Background(), ref)
	if err != nil {
		t.Fatalf("Delete: unexpected error: %v", err)
	}
	if si.method != "ResourceDriver.Delete" {
		t.Errorf("method: got %q, want ResourceDriver.Delete", si.method)
	}
	for _, key := range []string{"resource_type", "ref_name", "ref_type", "ref_provider_id"} {
		if _, ok := si.args[key]; !ok {
			t.Errorf("missing arg key %q", key)
		}
	}
}

func TestRemoteDriver_Delete_Error(t *testing.T) {
	si := &stubInvoker{err: fmt.Errorf("not found")}
	d := newDriver(si)
	err := d.Delete(context.Background(), sampleRef())
	if err == nil {
		t.Fatal("expected error")
	}
}

// ── Diff ──────────────────────────────────────────────────────────────────────

func TestRemoteDriver_Diff(t *testing.T) {
	diffResp := map[string]any{
		"needs_update":  true,
		"needs_replace": false,
		"changes": []any{
			map[string]any{
				"path":      "config.image",
				"old":       "myapp:v1",
				"new":       "myapp:v2",
				"force_new": false,
			},
		},
	}
	si := &stubInvoker{resp: diffResp}
	d := newDriver(si)
	spec := sampleSpec()
	current := &interfaces.ResourceOutput{
		Name:       "my-resource",
		Type:       "container_service",
		ProviderID: "pid-123",
		Status:     "running",
		Outputs:    map[string]any{"image": "myapp:v1"},
		Sensitive:  map[string]bool{"password": true},
	}

	result, err := d.Diff(context.Background(), spec, current)
	if err != nil {
		t.Fatalf("Diff: unexpected error: %v", err)
	}
	if si.method != "ResourceDriver.Diff" {
		t.Errorf("method: got %q, want ResourceDriver.Diff", si.method)
	}
	// Check that both spec and current fields were sent
	for _, key := range []string{"resource_type", "spec_name", "spec_type", "spec_config",
		"current_name", "current_type", "current_provider_id", "current_status"} {
		if _, ok := si.args[key]; !ok {
			t.Errorf("missing arg key %q", key)
		}
	}
	if !result.NeedsUpdate {
		t.Error("NeedsUpdate: expected true")
	}
	if result.NeedsReplace {
		t.Error("NeedsReplace: expected false")
	}
	if len(result.Changes) != 1 {
		t.Fatalf("Changes: expected 1, got %d", len(result.Changes))
	}
	if result.Changes[0].Path != "config.image" {
		t.Errorf("Changes[0].Path: got %q", result.Changes[0].Path)
	}
}

// ── Scale ─────────────────────────────────────────────────────────────────────

func TestRemoteDriver_Scale(t *testing.T) {
	si := &stubInvoker{resp: sampleOutputMap()}
	d := newDriver(si)
	ref := sampleRef()

	out, err := d.Scale(context.Background(), ref, 3)
	if err != nil {
		t.Fatalf("Scale: unexpected error: %v", err)
	}
	if si.method != "ResourceDriver.Scale" {
		t.Errorf("method: got %q, want ResourceDriver.Scale", si.method)
	}
	for _, key := range []string{"resource_type", "ref_name", "ref_type", "ref_provider_id", "replicas"} {
		if _, ok := si.args[key]; !ok {
			t.Errorf("missing arg key %q", key)
		}
	}
	if si.args["replicas"] != 3 {
		t.Errorf("replicas: got %v", si.args["replicas"])
	}
	if out.ProviderID != "pid-123" {
		t.Errorf("ProviderID: got %q", out.ProviderID)
	}
}

// ── SensitiveKeys ─────────────────────────────────────────────────────────────

func TestRemoteDriver_SensitiveKeys(t *testing.T) {
	si := &stubInvoker{resp: map[string]any{
		"keys": []any{"password", "token"},
	}}
	d := newDriver(si)

	keys := d.SensitiveKeys()
	if si.method != "ResourceDriver.SensitiveKeys" {
		t.Errorf("method: got %q, want ResourceDriver.SensitiveKeys", si.method)
	}
	if si.args["resource_type"] != "container_service" {
		t.Errorf("resource_type: got %v", si.args["resource_type"])
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d: %v", len(keys), keys)
	}
	if keys[0] != "password" || keys[1] != "token" {
		t.Errorf("keys: got %v", keys)
	}
}

func TestRemoteDriver_SensitiveKeys_Empty(t *testing.T) {
	si := &stubInvoker{resp: map[string]any{}}
	d := newDriver(si)
	keys := d.SensitiveKeys()
	if len(keys) != 0 {
		t.Errorf("expected empty keys, got %v", keys)
	}
}

func TestRemoteDriver_SensitiveKeys_Error(t *testing.T) {
	si := &stubInvoker{err: fmt.Errorf("rpc error")}
	d := newDriver(si)
	// SensitiveKeys returns []string (no error); on invoker error it should return nil/empty
	keys := d.SensitiveKeys()
	if len(keys) != 0 {
		t.Errorf("expected empty keys on error, got %v", keys)
	}
}
