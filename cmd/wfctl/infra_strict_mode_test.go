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
	// The dispatcher iterates all providers; when every candidate's
	// EnumerateAll returns Unimplemented it must surface the bridge gap
	// in the error message (NOT a silent "Found 0 resources" stdout).
	if !strings.Contains(err.Error(), "unimplemented") {
		t.Errorf("expected error to reference 'unimplemented'; got: %v", err)
	}
	// Stdout must NOT include the "Found 0 resources" misleading line
	// because no provider succeeded — the renderer was never reached.
	if strings.Contains(out.String(), "Found 0") {
		t.Errorf("strict-mode violated: dispatcher silently rendered 'Found 0' instead of surfacing missing-bridge error; stdout=%s", out.String())
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

// fakeStrictWorkingProvider is an IaCProvider whose EnumerateAll returns a
// canned slice. Used alongside fakeStrictUnimplProvider to construct a
// loaded-providers scenario that exercises the multi-provider
// continue-on-Unimplemented dispatcher loop introduced for PR #589.
type fakeStrictWorkingProvider struct {
	keys     []*interfaces.ResourceOutput
	lastType string
}

func (f *fakeStrictWorkingProvider) Name() string                                         { return "fake-working" }
func (f *fakeStrictWorkingProvider) Version() string                                      { return "0.0.0-test" }
func (f *fakeStrictWorkingProvider) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (f *fakeStrictWorkingProvider) Capabilities() []interfaces.IaCCapabilityDeclaration  { return nil }
func (f *fakeStrictWorkingProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (f *fakeStrictWorkingProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, nil
}
func (f *fakeStrictWorkingProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (f *fakeStrictWorkingProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (f *fakeStrictWorkingProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (f *fakeStrictWorkingProvider) Import(_ context.Context, _, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (f *fakeStrictWorkingProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (f *fakeStrictWorkingProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return &fakeNoopDriver{}, nil
}
func (f *fakeStrictWorkingProvider) SupportedCanonicalKeys() []string { return nil }
func (f *fakeStrictWorkingProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (f *fakeStrictWorkingProvider) Close() error { return nil }
func (f *fakeStrictWorkingProvider) EnumerateAll(_ context.Context, resourceType string) ([]*interfaces.ResourceOutput, error) {
	f.lastType = resourceType
	return f.keys, nil
}

// TestInfraAuditKeysCmd_MultiProvider_ContinuesPastUnimplemented asserts the
// fix for PR #589 Copilot Thread 1: when N providers are loaded and the
// FIRST provider's EnumerateAll returns ErrProviderMethodUnimplemented (the
// gRPC bridge is wired but the plugin process doesn't implement the method),
// the dispatcher MUST continue to the next provider rather than aborting
// the whole run. Bridging EnumerateAll into *remoteIaCProvider made every
// remote provider satisfy the type-assert, so the pre-fix behavior would
// pick provider 1 and never reach provider 2.
func TestInfraAuditKeysCmd_MultiProvider_ContinuesPastUnimplemented(t *testing.T) {
	working := &fakeStrictWorkingProvider{
		keys: []*interfaces.ResourceOutput{
			{Name: "key-from-second-provider", Type: "infra.spaces_key", ProviderID: "AK_SECOND"},
		},
	}

	origLoad := auditKeysLoadProviders
	t.Cleanup(func() { auditKeysLoadProviders = origLoad })
	auditKeysLoadProviders = func(_ context.Context, _ *flag.FlagSet, _, _ string) ([]interfaces.IaCProvider, []io.Closer, error) {
		return []interfaces.IaCProvider{
			&fakeStrictUnimplProvider{},
			working,
		}, nil, nil
	}

	origStdout := auditKeysStdout
	t.Cleanup(func() { auditKeysStdout = origStdout })
	var out bytes.Buffer
	auditKeysStdout = &out

	if err := runInfraAuditKeysCmd([]string{"--type", "infra.spaces_key"}); err != nil {
		t.Fatalf("multi-provider dispatcher must continue past Unimplemented; err=%v stdout=%s", err, out.String())
	}
	if !strings.Contains(out.String(), "AK_SECOND") {
		t.Errorf("expected output from second (working) provider; got stdout=%s", out.String())
	}
	if working.lastType != "infra.spaces_key" {
		t.Errorf("working provider EnumerateAll never called or wrong type; got %q", working.lastType)
	}
}

// TestInfraPruneCmd_MultiProvider_ContinuesPastUnimplemented mirrors the
// audit-keys test for prune. Same architectural concern (PR #589 Copilot
// Thread 1): every gRPC-loaded provider satisfies interfaces.EnumeratorAll
// at the type level, so the prune dispatcher must probe and continue past
// providers whose plugin process returns Unimplemented.
func TestInfraPruneCmd_MultiProvider_ContinuesPastUnimplemented(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")

	// Working provider returns one key — older than --created-before, NOT
	// matching --exclude-access-key — so prune will mark it for deletion.
	// fakeNoopDriver.Delete is a no-op so the test runs without cloud calls.
	working := &fakeStrictWorkingProvider{
		keys: []*interfaces.ResourceOutput{
			{
				Name:       "old-key",
				Type:       "infra.spaces_key",
				ProviderID: "AK_OLD",
				Outputs: map[string]any{
					"name":       "old-key",
					"access_key": "AK_OLD",
					"created_at": "2026-04-01T00:00:00Z",
				},
			},
		},
	}

	origLoad := pruneLoadProviders
	t.Cleanup(func() { pruneLoadProviders = origLoad })
	pruneLoadProviders = func(_ context.Context, _ *flag.FlagSet, _, _ string) ([]interfaces.IaCProvider, []io.Closer, error) {
		return []interfaces.IaCProvider{
			&fakeStrictUnimplProvider{},
			working,
		}, nil, nil
	}

	origStdout := pruneStdout
	t.Cleanup(func() { pruneStdout = origStdout })
	var out bytes.Buffer
	pruneStdout = &out

	err := runInfraPruneCmd([]string{
		"--type", "infra.spaces_key",
		"--created-before", "2026-05-01T00:00:00Z",
		"--exclude-access-key", "AK_NEW",
		"--confirm",
		"--non-interactive",
	})
	if err != nil {
		t.Fatalf("multi-provider dispatcher must continue past Unimplemented; err=%v stdout=%s", err, out.String())
	}
	if !strings.Contains(out.String(), "old-key") {
		t.Errorf("expected output to reference key from second (working) provider; got stdout=%s", out.String())
	}
}

// TestInfraRotateAndPruneCmd_MultiProvider_ContinuesPastUnimplemented is
// the rotate-and-prune analogue of the multi-provider tests above. The
// pre-flight probe MUST continue past providers whose EnumerateAll returns
// Unimplemented and reach a working provider, so a heterogeneous provider
// set (DO+AWS+GCP) doesn't fail the rotate just because the first one
// can't enumerate.
//
// Unlike audit-keys/prune, rotate-and-prune Step 1 is destructive
// (bootstrapSecrets) — the test stubs bootstrapSecrets to return success
// without touching real secrets so we can assert the dispatcher reached
// it via the second provider.
func TestInfraRotateAndPruneCmd_MultiProvider_ContinuesPastUnimplemented(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	tmpDir := t.TempDir()
	t.Setenv("WFCTL_STATE_DIR", tmpDir)

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

	working := &fakeStrictWorkingProvider{
		keys: []*interfaces.ResourceOutput{},
	}
	origLoad := rotateAndPruneLoadProviders
	t.Cleanup(func() { rotateAndPruneLoadProviders = origLoad })
	rotateAndPruneLoadProviders = func(_ context.Context, _ *flag.FlagSet, _, _ string) ([]interfaces.IaCProvider, []io.Closer, error) {
		return []interfaces.IaCProvider{
			&fakeStrictUnimplProvider{},
			working,
		}, nil, nil
	}

	origBoot := bootstrapSecrets
	t.Cleanup(func() { bootstrapSecrets = origBoot })
	bootRan := false
	bootstrapSecrets = func(ctx context.Context, _ secrets.Provider, _ *SecretsConfig, _ map[string]bool, _ ...interfaces.ProviderCredentialRevoker) (map[string]string, []RotationResult, error) {
		bootRan = true
		if err := runStubbedCredentialPreparation(ctx); err != nil {
			return nil, nil, err
		}
		return map[string]string{}, []RotationResult{
			{AccessKey: "AK_NEW", CreatedAt: "2026-05-09T00:00:00Z", Source: "digitalocean.spaces"},
		}, nil
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
	if err != nil {
		t.Fatalf("multi-provider dispatcher must continue past Unimplemented; err=%v stdout=%s", err, out.String())
	}
	if !bootRan {
		t.Errorf("dispatcher never reached working provider; bootstrapSecrets (Step 1) was not invoked")
	}
	if working.lastType != "infra.spaces_key" {
		t.Errorf("working provider pre-flight EnumerateAll never called or wrong type; got %q", working.lastType)
	}
}

// TestInfraCleanup_MultiProvider_ContinuesPastUnimplemented asserts the
// existing infra_cleanup.go pattern (try-each + skip-on-non-registration)
// remains correct in the presence of a heterogeneous loaded-providers
// list. Cleanup's design predates PR #589 but the same architectural
// concern applies — the dispatcher must distinguish "plugin doesn't
// register the Enumerator service" from "real enumerate failure".
//
// Per Task 17 / PR 618 (ADR-0028), the strict-typed dispatch surfaces
// "plugin didn't advertise the optional service" via adapter.Enumerator()
// returning nil — the typed analogue of the legacy
// `errors.Is(err, ErrProviderMethodUnimplemented)` skip path. Provider A
// here is built without an Enumerator service registered (the bufconn
// server only registers IaCProviderRequired); the dispatch site logs
// `skipped fake-a: provider does not implement Enumerator` and proceeds
// to provider B.
func TestInfraCleanup_MultiProvider_ContinuesPastUnimplemented(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte("# empty config\n"), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// Provider A: does NOT register IaCProviderEnumerator, so the typed
	// adapter's Enumerator() accessor returns nil → cleanup skips with the
	// "provider does not implement Enumerator" log line.
	a := newCleanupNonEnumFixture(t, "fake-a")
	// Provider B: registers Enumerator with a canned ref.
	b := newCleanupEnumFixture(t, "fake-b",
		[]interfaces.ResourceRef{
			{Name: "r-from-b", Type: "infra.spaces_key", ProviderID: "AK_B"},
		}, nil, nil)
	origLoad := cleanupLoadProviders
	t.Cleanup(func() { cleanupLoadProviders = origLoad })
	cleanupLoadProviders = func(_ context.Context, _ *flag.FlagSet, _, _ string) ([]interfaces.IaCProvider, []io.Closer, error) {
		return []interfaces.IaCProvider{a, b.adapter}, nil, nil
	}

	origStdout := cleanupStdout
	t.Cleanup(func() { cleanupStdout = origStdout })
	var out bytes.Buffer
	cleanupStdout = &out

	err := runInfraCleanup([]string{
		"--config", cfgPath,
		"--tag", "test",
	})
	if err != nil {
		t.Fatalf("cleanup must skip non-registered Enumerator and continue; err=%v stdout=%s", err, out.String())
	}
	// Provider A must surface a structured "skipped" log line.
	if !strings.Contains(out.String(), "skipped fake-a") {
		t.Errorf("expected 'skipped fake-a' log line for non-registered Enumerator; stdout=%s", out.String())
	}
	// Provider B must reach the dry-run / list path with its ref.
	if !strings.Contains(out.String(), "r-from-b") {
		t.Errorf("expected output from second (working) provider; stdout=%s", out.String())
	}
}
