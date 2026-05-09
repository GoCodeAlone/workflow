package main

import (
	"bytes"
	"context"
	"flag"
	"io"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fakeProviderEnumeratorAll is a test double implementing
// interfaces.EnumeratorAll. It returns a fixed list of *ResourceOutput per
// the workflow contract — full metadata so the audit-keys CLI can render
// without re-reading from the cloud.
type fakeProviderEnumeratorAll struct {
	keys []*interfaces.ResourceOutput
	// lastType records the resourceType passed to EnumerateAll so tests can
	// assert the CLI forwarded the --type flag correctly.
	lastType string
}

func (f *fakeProviderEnumeratorAll) EnumerateAll(_ context.Context, resourceType string) ([]*interfaces.ResourceOutput, error) {
	f.lastType = resourceType
	return f.keys, nil
}

// fakeIaCProviderForAuditKeys is the minimal IaCProvider implementation
// required to exercise runInfraAuditKeysCmd's dispatcher path. It
// implements interfaces.EnumeratorAll (so the dispatcher's type-assert
// succeeds) plus stubs of every other IaCProvider method (returning nil/
// zero values) so go's type system is satisfied. Real cloud calls are
// never reached because audit-keys only invokes EnumerateAll.
type fakeIaCProviderForAuditKeys struct {
	keys []*interfaces.ResourceOutput
}

func (f *fakeIaCProviderForAuditKeys) Name() string    { return "fake-do" }
func (f *fakeIaCProviderForAuditKeys) Version() string { return "0.0.0-test" }
func (f *fakeIaCProviderForAuditKeys) Initialize(_ context.Context, _ map[string]any) error {
	return nil
}
func (f *fakeIaCProviderForAuditKeys) Capabilities() []interfaces.IaCCapabilityDeclaration {
	return nil
}
func (f *fakeIaCProviderForAuditKeys) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (f *fakeIaCProviderForAuditKeys) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, nil
}
func (f *fakeIaCProviderForAuditKeys) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (f *fakeIaCProviderForAuditKeys) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (f *fakeIaCProviderForAuditKeys) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (f *fakeIaCProviderForAuditKeys) Import(_ context.Context, _, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (f *fakeIaCProviderForAuditKeys) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (f *fakeIaCProviderForAuditKeys) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return nil, nil
}
func (f *fakeIaCProviderForAuditKeys) SupportedCanonicalKeys() []string { return nil }
func (f *fakeIaCProviderForAuditKeys) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (f *fakeIaCProviderForAuditKeys) Close() error { return nil }
func (f *fakeIaCProviderForAuditKeys) EnumerateAll(_ context.Context, _ string) ([]*interfaces.ResourceOutput, error) {
	return f.keys, nil
}

// TestInfraAuditKeysCmd_AcceptsConfigFlag is the smoke-test sentinel for
// the args-passing contract documented on runInfraAuditKeysCmd: the
// dispatcher MUST synthesize a clean inner-args slice with only flags
// runInfraAuditKeys understands. Forwarding the raw args slice (which
// includes --config / --env) would error inside runInfraAuditKeys with
// "flag provided but not defined: -config".
//
// Regression guard for the bug where unit tests of runInfraAuditKeys
// passed only synthetic []string{"--type", ...} and never exercised the
// dispatcher's args-passing path — real CLI invocations would error.
func TestInfraAuditKeysCmd_AcceptsConfigFlag(t *testing.T) {
	// Stub the provider loader so we don't need a real iac.provider in a
	// real infra.yaml — any path is fine since the seam returns our fake.
	origLoad := auditKeysLoadProviders
	t.Cleanup(func() { auditKeysLoadProviders = origLoad })
	auditKeysLoadProviders = func(_ context.Context, _ *flag.FlagSet, _, _ string) ([]interfaces.IaCProvider, []io.Closer, error) {
		return []interfaces.IaCProvider{&fakeIaCProviderForAuditKeys{
			keys: []*interfaces.ResourceOutput{
				{Name: "k", Type: "infra.spaces_key", ProviderID: "AK", Outputs: map[string]any{"name": "k", "access_key": "AK"}},
			},
		}}, nil, nil
	}

	// Capture stdout via the seam so we can assert the CLI rendered output.
	origStdout := auditKeysStdout
	t.Cleanup(func() { auditKeysStdout = origStdout })
	var out bytes.Buffer
	auditKeysStdout = &out

	// The fix-under-test: --config + --env are in args. Pre-fix behavior
	// errored with "flag provided but not defined: -config" because the
	// inner FlagSet only declared --type.
	if err := runInfraAuditKeysCmd([]string{
		"--type", "infra.spaces_key",
		"--config", "/tmp/some-infra.yaml",
		"--env", "staging",
	}); err != nil {
		t.Fatalf("runInfraAuditKeysCmd failed; --config / --env should be accepted by the dispatcher: %v", err)
	}
	if !strings.Contains(out.String(), "AK") {
		t.Errorf("expected dispatcher to reach EnumerateAll + render output; got: %s", out.String())
	}
}

// TestInfraAuditKeys_ListsAll verifies that `wfctl infra audit-keys --type
// <T>` delegates to the provider's EnumeratorAll, then renders every
// returned key's identifying fields (Name, ProviderID/access_key) into the
// writer. This is the failing test for Task 16 of the spaces-key-iac-resource
// plan (PR5). Until Task 17 implements runInfraAuditKeys + the registration
// of `wfctl infra audit-keys`, this test fails with `undefined:
// runInfraAuditKeys`.
func TestInfraAuditKeys_ListsAll(t *testing.T) {
	fakeProv := &fakeProviderEnumeratorAll{
		keys: []*interfaces.ResourceOutput{
			{
				Name:       "key-a",
				Type:       "infra.spaces_key",
				ProviderID: "AK_A",
				Outputs: map[string]any{
					"name":       "key-a",
					"access_key": "AK_A",
					"created_at": "2026-05-01T00:00:00Z",
				},
			},
			{
				Name:       "key-b",
				Type:       "infra.spaces_key",
				ProviderID: "AK_B",
				Outputs: map[string]any{
					"name":       "key-b",
					"access_key": "AK_B",
					"created_at": "2026-05-02T00:00:00Z",
				},
			},
		},
	}

	var out bytes.Buffer
	exitCode := runInfraAuditKeys([]string{"--type", "infra.spaces_key"}, fakeProv, &out)
	if exitCode != 0 {
		t.Fatalf("expected zero exit; got %d\nout=%s", exitCode, out.String())
	}
	if !strings.Contains(out.String(), "key-a") || !strings.Contains(out.String(), "key-b") {
		t.Errorf("expected both keys in output; got: %s", out.String())
	}
	if !strings.Contains(out.String(), "AK_A") || !strings.Contains(out.String(), "AK_B") {
		t.Errorf("expected access_keys in output; got: %s", out.String())
	}
	// CLI must have forwarded the --type flag to the enumerator.
	if fakeProv.lastType != "infra.spaces_key" {
		t.Errorf("EnumerateAll resourceType = %q, want %q", fakeProv.lastType, "infra.spaces_key")
	}
}
