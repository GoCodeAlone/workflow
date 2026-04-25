package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

// ── wrapIaCError ──────────────────────────────────────────────────────────────

func TestWrapIaCError_Nil(t *testing.T) {
	if wrapIaCError(nil) != nil {
		t.Error("wrapIaCError(nil) should return nil")
	}
}

func TestWrapIaCError_Sentinels(t *testing.T) {
	cases := []struct {
		msg      string
		sentinel error
	}{
		// ErrResourceNotFound
		{"not found", interfaces.ErrResourceNotFound},
		{"NOT FOUND", interfaces.ErrResourceNotFound},
		{"404 returned", interfaces.ErrResourceNotFound},
		{"405 method not allowed", interfaces.ErrResourceNotFound},
		{"does not exist", interfaces.ErrResourceNotFound},
		{"Does Not Exist", interfaces.ErrResourceNotFound},
		{"no such resource", interfaces.ErrResourceNotFound},
		{"No Such Resource", interfaces.ErrResourceNotFound},
		// ErrResourceAlreadyExists
		{"app already exists", interfaces.ErrResourceAlreadyExists},
		{"ALREADY EXISTS", interfaces.ErrResourceAlreadyExists},
		{"409 conflict", interfaces.ErrResourceAlreadyExists},
		{"conflict: name taken", interfaces.ErrResourceAlreadyExists},
		// ErrRateLimited
		{"rate limit exceeded", interfaces.ErrRateLimited},
		{"Rate Limit", interfaces.ErrRateLimited},
		{"429 too many requests", interfaces.ErrRateLimited},
		{"too many requests", interfaces.ErrRateLimited},
		// ErrTransient
		{"500 internal server error", interfaces.ErrTransient},
		{"502 bad gateway", interfaces.ErrTransient},
		{"503 service unavailable", interfaces.ErrTransient},
		{"504 gateway timeout", interfaces.ErrTransient},
		{"bad gateway", interfaces.ErrTransient},
		{"gateway timeout", interfaces.ErrTransient},
		{"service unavailable", interfaces.ErrTransient},
		// ErrUnauthorized
		{"401 unauthorized", interfaces.ErrUnauthorized},
		{"unauthorized", interfaces.ErrUnauthorized},
		{"unable to authenticate", interfaces.ErrUnauthorized},
		// ErrForbidden
		{"403 forbidden", interfaces.ErrForbidden},
		{"forbidden", interfaces.ErrForbidden},
		// ErrValidation
		{"400 bad request", interfaces.ErrValidation},
		{"422 unprocessable entity", interfaces.ErrValidation},
		{"validation failed", interfaces.ErrValidation},
		{"invalid field: name", interfaces.ErrValidation},
	}
	for _, tc := range cases {
		err := wrapIaCError(fmt.Errorf("%s", tc.msg))
		if !errors.Is(err, tc.sentinel) {
			t.Errorf("msg %q: expected %v, got %v", tc.msg, tc.sentinel, err)
		}
		// Original message must be preserved.
		if !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("msg %q: original message not preserved in %q", tc.msg, err.Error())
		}
	}
}

func TestWrapIaCError_PassThrough(t *testing.T) {
	msgs := []string{
		"connection reset by peer",
		"timeout waiting for lock",
		"unexpected end of stream",
	}
	for _, msg := range msgs {
		orig := fmt.Errorf("%s", msg)
		err := wrapIaCError(orig)
		for _, s := range []error{
			interfaces.ErrResourceNotFound,
			interfaces.ErrResourceAlreadyExists,
			interfaces.ErrRateLimited,
			interfaces.ErrTransient,
			interfaces.ErrUnauthorized,
			interfaces.ErrForbidden,
			interfaces.ErrValidation,
		} {
			if errors.Is(err, s) {
				t.Errorf("msg %q: should not match %v", msg, s)
			}
		}
		if err.Error() != orig.Error() {
			t.Errorf("msg %q: error string changed: got %q", msg, err.Error())
		}
	}
}

// ── per-method wrapIaCError coverage ─────────────────────────────────────────

// methodSentinelCases lists (error message, expected sentinel) pairs used
// across all driver method wrapping tests below.
var methodSentinelCases = []struct {
	msg      string
	sentinel error
}{
	{"404 not found", interfaces.ErrResourceNotFound},
	{"already exists", interfaces.ErrResourceAlreadyExists},
	{"429 too many requests", interfaces.ErrRateLimited},
	{"503 service unavailable", interfaces.ErrTransient},
	{"401 unauthorized", interfaces.ErrUnauthorized},
	{"403 forbidden", interfaces.ErrForbidden},
	{"422 validation failed", interfaces.ErrValidation},
}

func TestRemoteDriver_Create_WrapsAllSentinels(t *testing.T) {
	for _, tc := range methodSentinelCases {
		si := &stubInvoker{err: fmt.Errorf("%s", tc.msg)}
		d := newDriver(si)
		_, err := d.Create(context.Background(), sampleSpec())
		if err == nil {
			t.Fatalf("Create %q: expected error", tc.msg)
		}
		if !errors.Is(err, tc.sentinel) {
			t.Errorf("Create %q: expected %v, got %v", tc.msg, tc.sentinel, err)
		}
	}
}

func TestRemoteDriver_Read_WrapsAllSentinels(t *testing.T) {
	for _, tc := range methodSentinelCases {
		si := &stubInvoker{err: fmt.Errorf("%s", tc.msg)}
		d := newDriver(si)
		_, err := d.Read(context.Background(), sampleRef())
		if err == nil {
			t.Fatalf("Read %q: expected error", tc.msg)
		}
		if !errors.Is(err, tc.sentinel) {
			t.Errorf("Read %q: expected %v, got %v", tc.msg, tc.sentinel, err)
		}
	}
}

func TestRemoteDriver_Update_WrapsAllSentinels(t *testing.T) {
	for _, tc := range methodSentinelCases {
		si := &stubInvoker{err: fmt.Errorf("%s", tc.msg)}
		d := newDriver(si)
		_, err := d.Update(context.Background(), sampleRef(), sampleSpec())
		if err == nil {
			t.Fatalf("Update %q: expected error", tc.msg)
		}
		if !errors.Is(err, tc.sentinel) {
			t.Errorf("Update %q: expected %v, got %v", tc.msg, tc.sentinel, err)
		}
	}
}

func TestRemoteDriver_Delete_WrapsAllSentinels(t *testing.T) {
	for _, tc := range methodSentinelCases {
		si := &stubInvoker{err: fmt.Errorf("%s", tc.msg)}
		d := newDriver(si)
		err := d.Delete(context.Background(), sampleRef())
		if err == nil {
			t.Fatalf("Delete %q: expected error", tc.msg)
		}
		if !errors.Is(err, tc.sentinel) {
			t.Errorf("Delete %q: expected %v, got %v", tc.msg, tc.sentinel, err)
		}
	}
}

func TestRemoteDriver_Diff_WrapsAllSentinels(t *testing.T) {
	current := &interfaces.ResourceOutput{ProviderID: "pid-123"}
	for _, tc := range methodSentinelCases {
		si := &stubInvoker{err: fmt.Errorf("%s", tc.msg)}
		d := newDriver(si)
		_, err := d.Diff(context.Background(), sampleSpec(), current)
		if err == nil {
			t.Fatalf("Diff %q: expected error", tc.msg)
		}
		if !errors.Is(err, tc.sentinel) {
			t.Errorf("Diff %q: expected %v, got %v", tc.msg, tc.sentinel, err)
		}
	}
}

func TestRemoteDriver_Scale_WrapsAllSentinels(t *testing.T) {
	for _, tc := range methodSentinelCases {
		si := &stubInvoker{err: fmt.Errorf("%s", tc.msg)}
		d := newDriver(si)
		_, err := d.Scale(context.Background(), sampleRef(), 2)
		if err == nil {
			t.Fatalf("Scale %q: expected error", tc.msg)
		}
		if !errors.Is(err, tc.sentinel) {
			t.Errorf("Scale %q: expected %v, got %v", tc.msg, tc.sentinel, err)
		}
	}
}

func TestRemoteDriver_HealthCheck_WrapsAllSentinels(t *testing.T) {
	for _, tc := range methodSentinelCases {
		si := &stubInvoker{err: fmt.Errorf("%s", tc.msg)}
		d := newDriver(si)
		_, err := d.HealthCheck(context.Background(), sampleRef())
		if err == nil {
			t.Fatalf("HealthCheck %q: expected error", tc.msg)
		}
		if !errors.Is(err, tc.sentinel) {
			t.Errorf("HealthCheck %q: expected %v, got %v", tc.msg, tc.sentinel, err)
		}
	}
}

// ── Troubleshoot ──────────────────────────────────────────────────────────────

func TestRemoteDriver_Troubleshoot_Success(t *testing.T) {
	si := &stubInvoker{
		resp: map[string]any{
			"diagnostics": []any{
				map[string]any{
					"id": "dep-1", "phase": "pre_deploy",
					"cause": "exit 1", "at": "2026-04-24T00:00:00Z",
					"detail": "log tail",
				},
			},
		},
	}
	d := newDriver(si)
	ref := interfaces.ResourceRef{Name: "bmw-staging", Type: "app_platform", ProviderID: "abc-123"}
	diags, err := d.Troubleshoot(context.Background(), ref, "boom")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(diags) != 1 || diags[0].Cause != "exit 1" {
		t.Fatalf("unexpected diags: %+v", diags)
	}
	if si.method != "ResourceDriver.Troubleshoot" {
		t.Errorf("wrong svc: %s", si.method)
	}
	if diags[0].Detail != "log tail" {
		t.Errorf("Detail: got %q", diags[0].Detail)
	}

	// Assert args are flat primitives — structpb.NewStruct (gRPC transport)
	// cannot encode Go structs; ref must be decomposed to scalar fields.
	if si.args["ref_name"] != ref.Name {
		t.Errorf("args[ref_name] = %q, want %q", si.args["ref_name"], ref.Name)
	}
	if si.args["ref_type"] != ref.Type {
		t.Errorf("args[ref_type] = %q, want %q", si.args["ref_type"], ref.Type)
	}
	if si.args["ref_provider_id"] != ref.ProviderID {
		t.Errorf("args[ref_provider_id] = %q, want %q", si.args["ref_provider_id"], ref.ProviderID)
	}
	if si.args["failure_msg"] != "boom" {
		t.Errorf("args[failure_msg] = %q, want %q", si.args["failure_msg"], "boom")
	}
	if _, hasOldRef := si.args["ref"]; hasOldRef {
		t.Error("args must not contain a nested 'ref' struct — structpb cannot encode it")
	}
	// resource_type assertion lives in TestRemoteDriver_AllMethodsSendResourceType
}

func TestRemoteDriver_Troubleshoot_UnimplementedSilent(t *testing.T) {
	si := &stubInvoker{err: status.Error(codes.Unimplemented, "method not implemented")}
	d := newDriver(si)
	diags, err := d.Troubleshoot(context.Background(), interfaces.ResourceRef{Name: "x"}, "boom")
	if err != nil {
		t.Fatalf("Unimplemented should not surface: %v", err)
	}
	if diags != nil {
		t.Fatalf("expected nil diags, got %+v", diags)
	}
}

func TestRemoteDriver_Troubleshoot_OtherErrorSurfaces(t *testing.T) {
	si := &stubInvoker{err: errors.New("network oops")}
	d := newDriver(si)
	_, err := d.Troubleshoot(context.Background(), interfaces.ResourceRef{Name: "x"}, "boom")
	if err == nil {
		t.Fatal("expected error to surface")
	}
}

// TestRemoteDriver_AllMethodsSendResourceType is a regression gate for the
// class of bug where a new or modified ResourceDriver method omits
// "resource_type" from its InvokeService args map.  Every method on
// remoteResourceDriver must include "resource_type": d.resourceType so the
// plugin side can dispatch to the correct driver implementation.
//
// The Troubleshoot method regressed in v0.18.11 (missing resource_type);
// this table test ensures the invariant holds across all 9 public methods.
func TestRemoteDriver_AllMethodsSendResourceType(t *testing.T) {
	ref := sampleRef()
	spec := sampleSpec()
	current := &interfaces.ResourceOutput{
		ProviderID: "pid-123",
		Name:       "my-resource",
		Type:       "container_service",
		Status:     "running",
		Outputs:    map[string]any{"endpoint": "https://example.com"},
		Sensitive:  map[string]bool{},
	}

	type testCase struct {
		name string
		call func(d *remoteResourceDriver, si *stubInvoker)
		resp map[string]any
	}

	cases := []testCase{
		{
			name: "Create",
			resp: sampleOutputMap(),
			call: func(d *remoteResourceDriver, _ *stubInvoker) {
				_, _ = d.Create(context.Background(), spec)
			},
		},
		{
			name: "Read",
			resp: sampleOutputMap(),
			call: func(d *remoteResourceDriver, _ *stubInvoker) {
				_, _ = d.Read(context.Background(), ref)
			},
		},
		{
			name: "Update",
			resp: sampleOutputMap(),
			call: func(d *remoteResourceDriver, _ *stubInvoker) {
				_, _ = d.Update(context.Background(), ref, spec)
			},
		},
		{
			name: "Delete",
			resp: map[string]any{},
			call: func(d *remoteResourceDriver, _ *stubInvoker) {
				_ = d.Delete(context.Background(), ref)
			},
		},
		{
			name: "Diff",
			resp: map[string]any{"needs_update": false, "needs_replace": false},
			call: func(d *remoteResourceDriver, _ *stubInvoker) {
				_, _ = d.Diff(context.Background(), spec, current)
			},
		},
		{
			name: "HealthCheck",
			resp: map[string]any{"healthy": true, "message": "ok"},
			call: func(d *remoteResourceDriver, _ *stubInvoker) {
				_, _ = d.HealthCheck(context.Background(), ref)
			},
		},
		{
			name: "Scale",
			resp: sampleOutputMap(),
			call: func(d *remoteResourceDriver, _ *stubInvoker) {
				_, _ = d.Scale(context.Background(), ref, 3)
			},
		},
		{
			name: "SensitiveKeys",
			resp: map[string]any{"keys": []any{"secret"}},
			call: func(d *remoteResourceDriver, _ *stubInvoker) {
				_ = d.SensitiveKeys()
			},
		},
		{
			name: "Troubleshoot",
			resp: map[string]any{"diagnostics": []any{}},
			call: func(d *remoteResourceDriver, _ *stubInvoker) {
				_, _ = d.Troubleshoot(context.Background(), ref, "boom")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			si := &stubInvoker{resp: tc.resp}
			d := newDriver(si)
			tc.call(d, si)
			got, ok := si.args["resource_type"]
			if !ok {
				t.Errorf("%s: args missing \"resource_type\" — plugin will return 'missing resource_type arg'", tc.name)
				return
			}
			if got != "container_service" {
				t.Errorf("%s: resource_type = %q, want %q", tc.name, got, "container_service")
			}
		})
	}
}

func TestRemoteDriver_PassThroughUnknownErrors(t *testing.T) {
	msg := "connection reset by peer"
	for _, method := range []string{"create", "read", "update", "delete", "diff", "scale", "hc"} {
		var err error
		si := &stubInvoker{err: fmt.Errorf("%s", msg)}
		d := newDriver(si)
		current := &interfaces.ResourceOutput{ProviderID: "pid-123"}
		switch method {
		case "create":
			_, err = d.Create(context.Background(), sampleSpec())
		case "read":
			_, err = d.Read(context.Background(), sampleRef())
		case "update":
			_, err = d.Update(context.Background(), sampleRef(), sampleSpec())
		case "delete":
			err = d.Delete(context.Background(), sampleRef())
		case "diff":
			_, err = d.Diff(context.Background(), sampleSpec(), current)
		case "scale":
			_, err = d.Scale(context.Background(), sampleRef(), 2)
		case "hc":
			_, err = d.HealthCheck(context.Background(), sampleRef())
		}
		if err == nil {
			t.Fatalf("%s: expected error", method)
		}
		for _, s := range []error{
			interfaces.ErrResourceNotFound, interfaces.ErrResourceAlreadyExists,
			interfaces.ErrRateLimited, interfaces.ErrTransient,
			interfaces.ErrUnauthorized, interfaces.ErrForbidden, interfaces.ErrValidation,
		} {
			if errors.Is(err, s) {
				t.Errorf("%s: unknown error %q should not match sentinel %v", method, msg, s)
			}
		}
	}
}
