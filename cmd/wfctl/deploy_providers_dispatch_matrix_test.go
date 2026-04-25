package main

// dispatch_matrix_test.go — comprehensive IaCProvider + ResourceDriver gRPC dispatch matrix.
//
// For every method on remoteResourceDriver and remoteIaCProvider this file verifies:
//  1. The exact RPC method name passed to InvokeService.
//  2. All required arg keys are present in the args map.
//  3. Zero-value inputs (empty strings, nil slices) still produce the key — they must
//     not be silently dropped.
//  4. Error class matches when the invoker returns a sentinel-triggering message.
//
// Invariant proof: temporarily comment out one key in the real dispatcher and this
// test will fail for the corresponding row. Restore to make it pass again.
//
// Run: go test ./cmd/wfctl/... -run Dispatch -v

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// ── recordingInvoker ─────────────────────────────────────────────────────────

// recordingInvoker captures every InvokeService call (method + args) and
// returns a configurable response. Unlike stubInvoker it stores the last call
// only; that is sufficient because all matrix cases call InvokeService once.
type recordingInvoker struct {
	capturedMethod string
	capturedArgs   map[string]any
	resp           map[string]any
	err            error
}

func (r *recordingInvoker) InvokeService(method string, args map[string]any) (map[string]any, error) {
	r.capturedMethod = method
	r.capturedArgs = args
	return r.resp, r.err
}

func newRecorder(resp map[string]any, err error) *recordingInvoker {
	return &recordingInvoker{resp: resp, err: err}
}

// ── assertion helpers ─────────────────────────────────────────────────────────

// assertMethod fails unless the captured method equals want.
func assertMethod(t *testing.T, ri *recordingInvoker, want string) {
	t.Helper()
	if ri.capturedMethod != want {
		t.Errorf("RPC method = %q, want %q", ri.capturedMethod, want)
	}
}

// assertKeys fails if any required key is absent from the captured args map,
// including when the value is the zero value (empty string, nil, false, 0).
// The check is presence-only — it does NOT assert the value.
func assertKeys(t *testing.T, ri *recordingInvoker, requiredKeys ...string) {
	t.Helper()
	for _, k := range requiredKeys {
		if _, ok := ri.capturedArgs[k]; !ok {
			t.Errorf("required arg key %q missing from InvokeService call; got keys: %v",
				k, mapKeys(ri.capturedArgs))
		}
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ── ResourceDriver dispatch matrix ───────────────────────────────────────────

// TestDispatchMatrix_RemoteResourceDriver exercises every method on
// remoteResourceDriver, verifying RPC method name and required arg keys.
func TestDispatchMatrix_RemoteResourceDriver(t *testing.T) {
	ctx := context.Background()
	const rt = "container_service"

	// emptyRef / emptySpec use zero values to prove keys aren't silently dropped.
	emptyRef := interfaces.ResourceRef{}
	emptySpec := interfaces.ResourceSpec{}
	zeroOutput := &interfaces.ResourceOutput{}

	cases := []struct {
		name         string
		wantMethod   string
		requiredKeys []string
		invoke       func(d *remoteResourceDriver, ri *recordingInvoker) error
	}{
		{
			name:         "Create",
			wantMethod:   "ResourceDriver.Create",
			requiredKeys: []string{"resource_type", "spec_name", "spec_type", "spec_config"},
			invoke: func(d *remoteResourceDriver, ri *recordingInvoker) error {
				ri.resp = map[string]any{}
				_, err := d.Create(ctx, emptySpec)
				return err
			},
		},
		{
			name:         "Read",
			wantMethod:   "ResourceDriver.Read",
			requiredKeys: []string{"resource_type", "ref_name", "ref_type", "ref_provider_id"},
			invoke: func(d *remoteResourceDriver, ri *recordingInvoker) error {
				ri.resp = map[string]any{}
				_, err := d.Read(ctx, emptyRef)
				return err
			},
		},
		{
			name:       "Update",
			wantMethod: "ResourceDriver.Update",
			requiredKeys: []string{
				"resource_type",
				"ref_name", "ref_type", "ref_provider_id",
				"spec_name", "spec_type", "spec_config",
			},
			invoke: func(d *remoteResourceDriver, ri *recordingInvoker) error {
				ri.resp = map[string]any{}
				_, err := d.Update(ctx, emptyRef, emptySpec)
				return err
			},
		},
		{
			name:         "Delete",
			wantMethod:   "ResourceDriver.Delete",
			requiredKeys: []string{"resource_type", "ref_name", "ref_type", "ref_provider_id"},
			invoke: func(d *remoteResourceDriver, ri *recordingInvoker) error {
				return d.Delete(ctx, emptyRef)
			},
		},
		{
			name:       "Diff",
			wantMethod: "ResourceDriver.Diff",
			requiredKeys: []string{
				"resource_type",
				"spec_name", "spec_type", "spec_config",
				"current_name", "current_type", "current_provider_id", "current_status",
			},
			invoke: func(d *remoteResourceDriver, ri *recordingInvoker) error {
				ri.resp = map[string]any{}
				_, err := d.Diff(ctx, emptySpec, zeroOutput)
				return err
			},
		},
		{
			name:         "HealthCheck",
			wantMethod:   "ResourceDriver.HealthCheck",
			requiredKeys: []string{"resource_type", "ref_name", "ref_type", "ref_provider_id"},
			invoke: func(d *remoteResourceDriver, ri *recordingInvoker) error {
				ri.resp = map[string]any{}
				_, err := d.HealthCheck(ctx, emptyRef)
				return err
			},
		},
		{
			name:         "Scale",
			wantMethod:   "ResourceDriver.Scale",
			requiredKeys: []string{"resource_type", "ref_name", "ref_type", "ref_provider_id", "replicas"},
			invoke: func(d *remoteResourceDriver, ri *recordingInvoker) error {
				ri.resp = map[string]any{}
				_, err := d.Scale(ctx, emptyRef, 0)
				return err
			},
		},
		{
			name:         "SensitiveKeys",
			wantMethod:   "ResourceDriver.SensitiveKeys",
			requiredKeys: []string{"resource_type"},
			invoke: func(d *remoteResourceDriver, ri *recordingInvoker) error {
				ri.resp = map[string]any{}
				_ = d.SensitiveKeys()
				return nil
			},
		},
		{
			// Task #80 regression: resource_type was missing from Troubleshoot args.
			// This entry will FAIL until that bug is fixed (resource_type is now required).
			name:         "Troubleshoot",
			wantMethod:   "ResourceDriver.Troubleshoot",
			requiredKeys: []string{"resource_type", "ref_name", "ref_type", "ref_provider_id", "failure_msg"},
			invoke: func(d *remoteResourceDriver, ri *recordingInvoker) error {
				ri.resp = map[string]any{}
				_, err := d.Troubleshoot(ctx, emptyRef, "")
				return err
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ri := newRecorder(nil, nil)
			d := &remoteResourceDriver{invoker: ri, resourceType: rt}
			_ = tc.invoke(d, ri)
			assertMethod(t, ri, tc.wantMethod)
			assertKeys(t, ri, tc.requiredKeys...)
		})
	}
}

// TestDispatchMatrix_RemoteResourceDriver_ZeroValues proves that zero-value
// inputs still emit the arg keys (no silent omission).
func TestDispatchMatrix_RemoteResourceDriver_ZeroValues(t *testing.T) {
	ctx := context.Background()

	// resource_type is empty string — key must still be present.
	ri := newRecorder(map[string]any{}, nil)
	d := &remoteResourceDriver{invoker: ri, resourceType: ""}
	_, _ = d.Read(ctx, interfaces.ResourceRef{})
	if _, ok := ri.capturedArgs["resource_type"]; !ok {
		t.Error("resource_type key missing when resourceType is empty string")
	}
	if ri.capturedArgs["resource_type"] != "" {
		t.Errorf("resource_type = %q, want empty string", ri.capturedArgs["resource_type"])
	}
}

// ── IaCProvider dispatch matrix ───────────────────────────────────────────────

// TestDispatchMatrix_RemoteIaCProvider exercises every method on remoteIaCProvider,
// verifying RPC method name and required arg keys.
func TestDispatchMatrix_RemoteIaCProvider(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name         string
		wantMethod   string
		requiredKeys []string
		invoke       func(p *remoteIaCProvider, ri *recordingInvoker) error
	}{
		{
			name:         "Plan",
			wantMethod:   "IaCProvider.Plan",
			requiredKeys: []string{"desired", "current"},
			invoke: func(p *remoteIaCProvider, ri *recordingInvoker) error {
				ri.resp = map[string]any{}
				_, err := p.Plan(ctx, nil, nil)
				return err
			},
		},
		{
			name:         "Apply",
			wantMethod:   "IaCProvider.Apply",
			requiredKeys: []string{"plan"},
			invoke: func(p *remoteIaCProvider, ri *recordingInvoker) error {
				ri.resp = map[string]any{}
				_, err := p.Apply(ctx, &interfaces.IaCPlan{})
				return err
			},
		},
		{
			name:         "Destroy",
			wantMethod:   "IaCProvider.Destroy",
			requiredKeys: []string{"refs"},
			invoke: func(p *remoteIaCProvider, ri *recordingInvoker) error {
				ri.resp = map[string]any{}
				_, err := p.Destroy(ctx, nil)
				return err
			},
		},
		{
			name:         "Status",
			wantMethod:   "IaCProvider.Status",
			requiredKeys: []string{"refs"},
			invoke: func(p *remoteIaCProvider, ri *recordingInvoker) error {
				ri.resp = map[string]any{}
				_, err := p.Status(ctx, nil)
				return err
			},
		},
		{
			name:         "DetectDrift",
			wantMethod:   "IaCProvider.DetectDrift",
			requiredKeys: []string{"refs"},
			invoke: func(p *remoteIaCProvider, ri *recordingInvoker) error {
				ri.resp = map[string]any{}
				_, err := p.DetectDrift(ctx, nil)
				return err
			},
		},
		{
			name:         "Import",
			wantMethod:   "IaCProvider.Import",
			requiredKeys: []string{"provider_id", "resource_type"},
			invoke: func(p *remoteIaCProvider, ri *recordingInvoker) error {
				ri.resp = map[string]any{}
				_, err := p.Import(ctx, "", "")
				return err
			},
		},
		{
			name:         "ResolveSizing",
			wantMethod:   "IaCProvider.ResolveSizing",
			requiredKeys: []string{"resource_type", "size", "hints"},
			invoke: func(p *remoteIaCProvider, ri *recordingInvoker) error {
				ri.resp = map[string]any{}
				_, err := p.ResolveSizing("", "", nil)
				return err
			},
		},
		{
			// BootstrapStateBackend sends cfg directly as the args map (no wrapper key).
			// When cfg is nil/empty InvokeService must still be called (args may be nil).
			name:         "BootstrapStateBackend_nilCfg",
			wantMethod:   "IaCProvider.BootstrapStateBackend",
			requiredKeys: []string{}, // flat cfg — no fixed wrapper key to assert
			invoke: func(p *remoteIaCProvider, ri *recordingInvoker) error {
				ri.resp = nil
				_, err := p.BootstrapStateBackend(ctx, nil)
				return err
			},
		},
		{
			// BootstrapStateBackend with populated cfg: keys from cfg pass through flat.
			name:         "BootstrapStateBackend_withCfg",
			wantMethod:   "IaCProvider.BootstrapStateBackend",
			requiredKeys: []string{"bucket", "region"}, // caller-supplied cfg keys
			invoke: func(p *remoteIaCProvider, ri *recordingInvoker) error {
				ri.resp = map[string]any{}
				cfg := map[string]any{"bucket": "my-bucket", "region": "nyc3"}
				_, err := p.BootstrapStateBackend(ctx, cfg)
				return err
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ri := newRecorder(nil, nil)
			p := &remoteIaCProvider{invoker: ri}
			_ = tc.invoke(p, ri)
			assertMethod(t, ri, tc.wantMethod)
			assertKeys(t, ri, tc.requiredKeys...)
		})
	}
}

// TestDispatchMatrix_RemoteIaCProvider_ZeroValueKeys proves that zero-value
// string args still appear as keys (not omitted when value is "").
func TestDispatchMatrix_RemoteIaCProvider_ZeroValueKeys(t *testing.T) {
	ri := newRecorder(map[string]any{}, nil)
	p := &remoteIaCProvider{invoker: ri}
	_, _ = p.Import(context.Background(), "", "")

	for _, k := range []string{"provider_id", "resource_type"} {
		if _, ok := ri.capturedArgs[k]; !ok {
			t.Errorf("Import: key %q missing when value is empty string", k)
		}
	}
}

// ── Error classification matrix ───────────────────────────────────────────────

// TestDispatchMatrix_ErrorClassification verifies that wrapIaCError wraps
// plugin error strings into the correct sentinel errors.
func TestDispatchMatrix_ErrorClassification(t *testing.T) {
	cases := []struct {
		msg     string
		wantErr error
	}{
		{"resource not found", interfaces.ErrResourceNotFound},
		{"404 not found", interfaces.ErrResourceNotFound},
		{"does not exist", interfaces.ErrResourceNotFound},
		{"already exists", interfaces.ErrResourceAlreadyExists},
		{"409 conflict", interfaces.ErrResourceAlreadyExists},
		{"rate limit exceeded", interfaces.ErrRateLimited},
		{"429 too many requests", interfaces.ErrRateLimited},
		{"500 internal server error", interfaces.ErrTransient},
		{"503 service unavailable", interfaces.ErrTransient},
		{"401 unauthorized", interfaces.ErrUnauthorized},
		{"unable to authenticate", interfaces.ErrUnauthorized},
		{"403 forbidden", interfaces.ErrForbidden},
		{"400 validation failed", interfaces.ErrValidation},
		{"422 invalid input", interfaces.ErrValidation},
		{"some unknown error", nil}, // no sentinel — returned unchanged
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.msg, func(t *testing.T) {
			wrapped := wrapIaCError(errors.New(tc.msg))
			if tc.wantErr == nil {
				if wrapped == nil || wrapped.Error() != tc.msg {
					t.Errorf("expected unchanged error %q, got %q", tc.msg, wrapped)
				}
				return
			}
			if !errors.Is(wrapped, tc.wantErr) {
				t.Errorf("errors.Is(%q) = false for sentinel %v", tc.msg, tc.wantErr)
			}
		})
	}
}

// TestDispatchMatrix_RemoteResourceDriver_ErrorPropagation verifies that
// InvokeService errors are wrapped via wrapIaCError before returning to caller.
func TestDispatchMatrix_RemoteResourceDriver_ErrorPropagation(t *testing.T) {
	ctx := context.Background()
	ri := newRecorder(nil, errors.New("resource not found"))
	d := &remoteResourceDriver{invoker: ri, resourceType: "container_service"}

	_, err := d.Read(ctx, interfaces.ResourceRef{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, interfaces.ErrResourceNotFound) {
		t.Errorf("err = %v; want errors.Is(ErrResourceNotFound) = true", err)
	}
}
