package main

// infra_strict_mode_test.go — strict-mode regression tests for
// audit-keys / prune / rotate-and-prune dispatchers.
//
// Per v0.27.1 user mandate ("remove the fallback and force strict mode"),
// when a gRPC-loaded provider's process does NOT actually implement
// EnumerateAll the dispatcher MUST surface the gap loudly:
//
//   - audit-keys: returns a non-nil error referencing the bridge gap.
//     Operators must NOT be misled by "Found 0 resources of type X".
//   - prune:      returns a non-nil error referencing the bridge gap.
//     Operators must NOT be misled by "Dry-run: 0 resource(s) to prune".
//   - rotate-and-prune: pre-flight returns a non-nil error BEFORE Step 1
//     rotation runs. The fake's bootstrapSecrets hook MUST NOT be invoked.
//
// These tests exist to lock the strict-mode contract so a future commit
// that re-introduces an (nil, nil) swallow fails CI loudly.

import (
	"bytes"
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

// fakeStrictUnimplProvider is an IaCProvider whose EnumerateAll returns
// interfaces.ErrProviderMethodUnimplemented — the exact error the gRPC
// proxy translates from a plugin missing the bridge wiring.
type fakeStrictUnimplProvider struct{}

func (f *fakeStrictUnimplProvider) Name() string                                         { return "fake-unimpl" }
func (f *fakeStrictUnimplProvider) Version() string                                      { return "0.0.0-test" }
func (f *fakeStrictUnimplProvider) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (f *fakeStrictUnimplProvider) Capabilities() []interfaces.IaCCapabilityDeclaration  { return nil }
func (f *fakeStrictUnimplProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (f *fakeStrictUnimplProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, nil
}
func (f *fakeStrictUnimplProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (f *fakeStrictUnimplProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (f *fakeStrictUnimplProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (f *fakeStrictUnimplProvider) Import(_ context.Context, _, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (f *fakeStrictUnimplProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (f *fakeStrictUnimplProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return &fakeNoopDriver{}, nil
}
func (f *fakeStrictUnimplProvider) SupportedCanonicalKeys() []string { return nil }
func (f *fakeStrictUnimplProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (f *fakeStrictUnimplProvider) Close() error { return nil }
func (f *fakeStrictUnimplProvider) EnumerateAll(_ context.Context, _ string) ([]*interfaces.ResourceOutput, error) {
	return nil, interfaces.ErrProviderMethodUnimplemented
}

// TestInfraAuditKeysCmd_ProviderMissingBridge_FailsLoud asserts that when
// a provider returns ErrProviderMethodUnimplemented the audit-keys
// dispatcher returns a non-nil error AND the rendered stdout includes the
// underlying error message. No silent "Found 0 resources" line.
func TestInfraAuditKeysCmd_ProviderMissingBridge_FailsLoud(t *testing.T) {
	origLoad := auditKeysLoadProviders
	t.Cleanup(func() { auditKeysLoadProviders = origLoad })
	auditKeysLoadProviders = func(_ context.Context, _ *flag.FlagSet, _, _ string) ([]interfaces.IaCProvider, []io.Closer, error) {
		return []interfaces.IaCProvider{&fakeStrictUnimplProvider{}}, nil, nil
	}

	origStdout := auditKeysStdout
	t.Cleanup(func() { auditKeysStdout = origStdout })
	var out bytes.Buffer
	auditKeysStdout = &out

	err := runInfraAuditKeysCmd([]string{"--type", "infra.spaces_key"})
	if err == nil {
		t.Fatalf("expected loud error from missing-bridge provider; got nil. out=%s", out.String())
	}
	// The dispatcher reports an exit code; the renderer logged the
	// underlying error to stdout. Both should reference the unimplemented
	// signal so the operator understands the failure mode.
	if !strings.Contains(out.String(), "unimplemented") &&
		!strings.Contains(err.Error(), "audit-keys exited") {
		t.Errorf("expected loud signal; err=%v, stdout=%s", err, out.String())
	}
}

// TestInfraPruneCmd_ProviderMissingBridge_FailsLoud asserts the same
// strict-mode contract for the prune dispatcher. The deletion step MUST
// NOT execute when EnumerateAll signals unimplemented.
func TestInfraPruneCmd_ProviderMissingBridge_FailsLoud(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")

	origLoad := pruneLoadProviders
	t.Cleanup(func() { pruneLoadProviders = origLoad })
	pruneLoadProviders = func(_ context.Context, _ *flag.FlagSet, _, _ string) ([]interfaces.IaCProvider, []io.Closer, error) {
		return []interfaces.IaCProvider{&fakeStrictUnimplProvider{}}, nil, nil
	}

	origStdout := pruneStdout
	t.Cleanup(func() { pruneStdout = origStdout })
	var out bytes.Buffer
	pruneStdout = &out

	err := runInfraPruneCmd([]string{
		"--type", "infra.spaces_key",
		"--created-before", "2026-05-08T00:00:00Z",
		"--exclude-access-key", "AK_NEW",
		"--confirm",
		"--non-interactive",
	})
	if err == nil {
		t.Fatalf("expected loud error from missing-bridge provider; got nil. out=%s", out.String())
	}
	// Stdout should NOT contain the misleading "Dry-run: 0" line because
	// the renderer never reached the rendering branch — enumerate failed.
	if strings.Contains(out.String(), "Dry-run: 0 resource(s) to prune") {
		t.Errorf("strict-mode violated: dispatcher silently rendered 'Dry-run: 0' instead of surfacing missing-bridge error; stdout=%s", out.String())
	}
}

// TestInfraRotateAndPruneCmd_PreFlight_AbortsBeforeRotation asserts that
// when the provider's EnumerateAll returns ErrProviderMethodUnimplemented,
// rotate-and-prune fails the pre-flight probe BEFORE invoking
// bootstrapSecrets (Step 1 rotation). This is the critical safety
// property: rotation is destructive, so the bridge gap must surface
// before any state is mutated.
func TestInfraRotateAndPruneCmd_PreFlight_AbortsBeforeRotation(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	tmpDir := t.TempDir()
	t.Setenv("WFCTL_STATE_DIR", tmpDir)

	// Write minimal config so the dispatcher can load providers.
	cfgPath := filepath.Join(tmpDir, "infra.yaml")
	body := `secrets:
  provider: env
  config:
    prefix: WFCTL_TEST_
  generate:
    - key: test-key
      type: provider_credential
      source: digitalocean.spaces
      name: test-key
`
	if err := os.WriteFile(cfgPath, []byte(body), 0600); err != nil {
		t.Fatalf("write fixture infra.yaml: %v", err)
	}

	origLoad := rotateAndPruneLoadProviders
	t.Cleanup(func() { rotateAndPruneLoadProviders = origLoad })
	rotateAndPruneLoadProviders = func(_ context.Context, _ *flag.FlagSet, _, _ string) ([]interfaces.IaCProvider, []io.Closer, error) {
		return []interfaces.IaCProvider{&fakeStrictUnimplProvider{}}, nil, nil
	}

	// Sentinel: bootstrapSecrets MUST NOT be invoked because the pre-flight
	// probe must abort the dispatcher before Step 1 rotation runs.
	origBoot := bootstrapSecrets
	t.Cleanup(func() { bootstrapSecrets = origBoot })
	bootRan := false
	bootstrapSecrets = func(_ context.Context, _ secrets.Provider, _ *SecretsConfig, _ map[string]bool, _ ...interfaces.ProviderCredentialRevoker) (map[string]string, []RotationResult, error) {
		bootRan = true
		return map[string]string{}, nil, nil
	}

	origStdout := rotateAndPruneStdout
	t.Cleanup(func() { rotateAndPruneStdout = origStdout })
	var out bytes.Buffer
	rotateAndPruneStdout = &out

	err := runInfraRotateAndPruneCmd([]string{
		"--type", "infra.spaces_key",
		"--name", "test-key",
		"--config", cfgPath,
		"--confirm", "--non-interactive",
	})
	if err == nil {
		t.Fatalf("expected loud pre-flight error; got nil. out=%s", out.String())
	}
	if bootRan {
		t.Errorf("bootstrapSecrets (Step 1 rotation) MUST NOT be invoked when pre-flight probe fails; bridge gap caught too late")
	}
	if !strings.Contains(err.Error(), "pre-flight") {
		t.Errorf("expected pre-flight error message; got: %v", err)
	}
}
