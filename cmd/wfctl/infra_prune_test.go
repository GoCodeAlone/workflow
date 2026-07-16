package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fakeIaCProviderForPrune is the minimal IaCProvider needed to exercise
// runInfraPruneCmd's dispatcher path (smoke test for the args-passing
// fix). Implements interfaces.EnumeratorAll so the type-assert in the
// dispatcher succeeds; ResourceDriver returns a fake driver that no-ops
// Delete so the prune step succeeds without touching cloud APIs.
type fakeIaCProviderForPrune struct {
	keys    []*interfaces.ResourceOutput
	enumErr error
}

func (f *fakeIaCProviderForPrune) Name() string                                         { return "fake-do" }
func (f *fakeIaCProviderForPrune) Version() string                                      { return "0.0.0-test" }
func (f *fakeIaCProviderForPrune) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (f *fakeIaCProviderForPrune) Capabilities() []interfaces.IaCCapabilityDeclaration  { return nil }
func (f *fakeIaCProviderForPrune) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (f *fakeIaCProviderForPrune) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, nil
}
func (f *fakeIaCProviderForPrune) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (f *fakeIaCProviderForPrune) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (f *fakeIaCProviderForPrune) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (f *fakeIaCProviderForPrune) Import(_ context.Context, _, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (f *fakeIaCProviderForPrune) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (f *fakeIaCProviderForPrune) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return &fakeNoopDriver{}, nil
}
func (f *fakeIaCProviderForPrune) SupportedCanonicalKeys() []string { return nil }
func (f *fakeIaCProviderForPrune) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (f *fakeIaCProviderForPrune) Close() error { return nil }
func (f *fakeIaCProviderForPrune) EnumerateAll(_ context.Context, _ string) ([]*interfaces.ResourceOutput, error) {
	if f.enumErr != nil {
		return nil, f.enumErr
	}
	return f.keys, nil
}

// fakeNoopDriver implements interfaces.ResourceDriver as no-ops so the
// prune-dispatcher smoke test can exercise the full path without touching
// cloud APIs. Delete records nothing — the test asserts on the dispatcher
// running cleanly, not on side effects.
type fakeNoopDriver struct{}

func (d *fakeNoopDriver) Create(_ context.Context, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeNoopDriver) Read(_ context.Context, _ interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeNoopDriver) Update(_ context.Context, _ interfaces.ResourceRef, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeNoopDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, _ *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	return &interfaces.DiffResult{}, nil
}
func (d *fakeNoopDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error { return nil }
func (d *fakeNoopDriver) HealthCheck(_ context.Context, _ interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return nil, nil
}
func (d *fakeNoopDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeNoopDriver) SensitiveKeys() []string { return nil }

// TestInfraPruneCmd_AcceptsConfigFlag is the smoke-test sentinel for the
// args-passing contract on runInfraPruneCmd: the dispatcher MUST
// synthesize a clean inner-args slice so runInfraPrune's narrow FlagSet
// doesn't error on --config / --env (which only the dispatcher itself
// understands). Regression guard for the same bug class team-lead caught
// in audit-keys (Copilot 3212662894).
func TestInfraPruneCmd_AcceptsConfigFlag(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")

	origLoad := pruneLoadProviders
	t.Cleanup(func() { pruneLoadProviders = origLoad })
	pruneLoadProviders = func(_ context.Context, _ *flag.FlagSet, _, _ string) ([]interfaces.IaCProvider, []io.Closer, error) {
		return []interfaces.IaCProvider{&fakeIaCProviderForPrune{
			// One eligible old key; --created-before will catch it; --exclude-access-key
			// preserves "AK_NEW" (which isn't in this list, so all listed keys are eligible).
			keys: []*interfaces.ResourceOutput{
				{Name: "k", Type: "infra.spaces_key", ProviderID: "AK_OLD", Outputs: map[string]any{
					"access_key": "AK_OLD", "created_at": "2026-05-01T00:00:00Z", "name": "k",
				}},
			},
		}}, nil, nil
	}

	origStdout := pruneStdout
	t.Cleanup(func() { pruneStdout = origStdout })
	var out bytes.Buffer
	pruneStdout = &out

	if err := runInfraPruneCmd([]string{
		"--type", "infra.spaces_key",
		"--config", "/tmp/some-infra.yaml",
		"--env", "staging",
		"--created-before", "2026-05-08T00:00:00Z",
		"--exclude-access-key", "AK_NEW",
		"--confirm",
		"--non-interactive",
	}); err != nil {
		t.Fatalf("runInfraPruneCmd failed; --config / --env should be accepted by the dispatcher: %v", err)
	}
}

func TestProviderPruneDispatchersSuppressPreflightProviderErrors(t *testing.T) {
	provider := &fakeIaCProviderForPrune{enumErr: errors.New("SENSITIVE_PREFLIGHT")}
	originalPruneLoader := pruneLoadProviders
	pruneLoadProviders = func(context.Context, *flag.FlagSet, string, string) ([]interfaces.IaCProvider, []io.Closer, error) {
		return []interfaces.IaCProvider{provider}, nil, nil
	}
	t.Cleanup(func() { pruneLoadProviders = originalPruneLoader })
	if err := runInfraPruneCmd([]string{"--type", "infra.example_credential"}); err == nil || strings.Contains(err.Error(), "SENSITIVE_PREFLIGHT") || !strings.Contains(err.Error(), "provider error text suppressed") {
		t.Fatalf("prune dispatcher error=%v", err)
	}

	originalRotateLoader := rotateAndPruneLoadProviders
	rotateAndPruneLoadProviders = func(context.Context, *flag.FlagSet, string, string) ([]interfaces.IaCProvider, []io.Closer, error) {
		return []interfaces.IaCProvider{provider}, nil, nil
	}
	t.Cleanup(func() { rotateAndPruneLoadProviders = originalRotateLoader })
	if err := runInfraRotateAndPruneCmd([]string{"--type", "infra.example_credential"}); err == nil || strings.Contains(err.Error(), "SENSITIVE_PREFLIGHT") || !strings.Contains(err.Error(), "provider error text suppressed") {
		t.Fatalf("rotate-and-prune dispatcher error=%v", err)
	}

	pruneLoadProviders = func(context.Context, *flag.FlagSet, string, string) ([]interfaces.IaCProvider, []io.Closer, error) {
		return nil, nil, errors.New("SENSITIVE_LOAD")
	}
	if err := runInfraPruneCmd([]string{"--type", "infra.example_credential"}); err == nil || strings.Contains(err.Error(), "SENSITIVE_LOAD") || !strings.Contains(err.Error(), "provider error text suppressed") {
		t.Fatalf("prune loader error=%v", err)
	}
	rotateAndPruneLoadProviders = func(context.Context, *flag.FlagSet, string, string) ([]interfaces.IaCProvider, []io.Closer, error) {
		return nil, nil, errors.New("SENSITIVE_LOAD")
	}
	if err := runInfraRotateAndPruneCmd([]string{"--type", "infra.example_credential"}); err == nil || strings.Contains(err.Error(), "SENSITIVE_LOAD") || !strings.Contains(err.Error(), "provider error text suppressed") {
		t.Fatalf("rotate-and-prune loader error=%v", err)
	}
}

func TestProviderPruneDispatchersUseSignalAwareContextForPreflight(t *testing.T) {
	originalCommandContext := providerCommandContext
	providerCommandContext = func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, func() {}
	}
	t.Cleanup(func() { providerCommandContext = originalCommandContext })

	originalPruneLoader := pruneLoadProviders
	var pruneLoaderCanceled, pruneLoaderDeadline bool
	pruneLoadProviders = func(ctx context.Context, _ *flag.FlagSet, _, _ string) ([]interfaces.IaCProvider, []io.Closer, error) {
		pruneLoaderCanceled = errors.Is(ctx.Err(), context.Canceled)
		_, pruneLoaderDeadline = ctx.Deadline()
		return nil, nil, ctx.Err()
	}
	t.Cleanup(func() { pruneLoadProviders = originalPruneLoader })
	if err := runInfraPruneCmd([]string{"--type", "infra.example_credential"}); err == nil || !pruneLoaderCanceled || !pruneLoaderDeadline {
		t.Fatalf("prune loader canceled=%v deadline=%v error=%v", pruneLoaderCanceled, pruneLoaderDeadline, err)
	}

	originalRotateLoader := rotateAndPruneLoadProviders
	var rotateLoaderCanceled, rotateLoaderDeadline bool
	rotateAndPruneLoadProviders = func(ctx context.Context, _ *flag.FlagSet, _, _ string) ([]interfaces.IaCProvider, []io.Closer, error) {
		rotateLoaderCanceled = errors.Is(ctx.Err(), context.Canceled)
		_, rotateLoaderDeadline = ctx.Deadline()
		return nil, nil, ctx.Err()
	}
	t.Cleanup(func() { rotateAndPruneLoadProviders = originalRotateLoader })
	if err := runInfraRotateAndPruneCmd([]string{"--type", "infra.example_credential"}); err == nil || !rotateLoaderCanceled || !rotateLoaderDeadline {
		t.Fatalf("rotate-and-prune loader canceled=%v deadline=%v error=%v", rotateLoaderCanceled, rotateLoaderDeadline, err)
	}
}

func TestReadRecoveryFileRejectsUnsafeFilesystemState(t *testing.T) {
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WFCTL_STATE_DIR", stateDir)
	path := filepath.Join(stateDir, "last-rotation.json")
	data := []byte(`{"type":"infra.example_credential","access_key":"credential-1","created_at":"2026-05-08T11:00:00Z"}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		if _, err := readRecoveryFile(); err != nil {
			t.Fatalf("Windows recovery state rejected using non-ACL FileMode bits: %v", err)
		}
	} else {
		if _, err := readRecoveryFile(); err == nil || !strings.Contains(err.Error(), "private regular file") {
			t.Fatalf("permissive recovery read error=%v", err)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := readRecoveryFile(); err == nil || !strings.Contains(err.Error(), "private regular file") {
		t.Fatalf("directory recovery read error=%v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	target := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(target, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := readRecoveryFile(); err == nil || !strings.Contains(err.Error(), "private regular file") {
		t.Fatalf("symlink recovery read error=%v", err)
	}
}

func TestInfraPruneRecoverySharesGlobalRecoveryStateLock(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	t.Setenv("WFCTL_STATE_DIR", t.TempDir())
	if err := writeRecoveryRecord(recoveryRecord{
		Type: "infra.example_credential", AccessKey: "new-id", CreatedAt: "2026-05-08T11:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	lockDir, err := credentialOperationLockDir()
	if err != nil {
		t.Fatal(err)
	}
	release, err := acquireCredentialOperationLock(lockDir, "wfctl.rotate-and-prune-recovery", "global")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	provider := &fakeProviderWithDelete{}
	var output bytes.Buffer
	code := runInfraPrune([]string{
		"--type", "infra.example_credential", "--recovery-from-last-rotation", "--confirm", "--non-interactive",
	}, provider, &output)
	if code == 0 || provider.lastType != "" || !strings.Contains(output.String(), "locked") {
		t.Fatalf("code=%d enumerated type=%q output=%s", code, provider.lastType, output.String())
	}
}

func TestInfraPruneCanceledContextStopsBeforeProviderInventory(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	provider := &fakeProviderWithDelete{}
	var output bytes.Buffer
	code := runInfraPruneWithOptions([]string{
		"--type", "infra.example_credential", "--created-before", "2026-05-08T11:00:00Z",
		"--exclude-access-key", "new-id", "--confirm", "--non-interactive",
	}, provider, &output, infraPruneOptions{Context: ctx})
	if code == 0 || provider.lastType != "" || provider.sawCanceledContext || !strings.Contains(output.String(), "cancelled before provider inventory") {
		t.Fatalf("code=%d enumerated type=%q canceled context observed=%v output=%s", code, provider.lastType, provider.sawCanceledContext, output.String())
	}
}

func TestInfraPruneUsesBoundedContextAndStopsAfterCancellation(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	originalCommandContext := providerCommandContext
	var cancel context.CancelFunc
	providerCommandContext = func() (context.Context, context.CancelFunc) {
		ctx, commandCancel := context.WithCancel(context.Background())
		cancel = commandCancel
		return ctx, commandCancel
	}
	t.Cleanup(func() { providerCommandContext = originalCommandContext })
	provider := &fakeProviderWithDelete{
		keys: []*interfaces.ResourceOutput{
			{Name: "first", Type: "infra.example_credential", ProviderID: "first-id", Outputs: map[string]any{"created_at": "2026-05-01T00:00:00Z"}},
			{Name: "second", Type: "infra.example_credential", ProviderID: "second-id", Outputs: map[string]any{"created_at": "2026-05-02T00:00:00Z"}},
		},
		cancelAfterDelete: func() { cancel() },
	}
	var output bytes.Buffer
	code := runInfraPrune([]string{
		"--type", "infra.example_credential", "--created-before", "2026-05-08T11:00:00Z",
		"--exclude-access-key", "new-id", "--confirm", "--non-interactive",
	}, provider, &output)
	if code == 0 || !provider.sawDeadline || len(provider.deleted) != 1 {
		t.Fatalf("code=%d deadline=%v deleted=%v output=%s", code, provider.sawDeadline, provider.deleted, output.String())
	}
}

func TestPreRotationPruneStopsAfterCancellation(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	provider := &fakeProviderWithDelete{
		keys: []*interfaces.ResourceOutput{
			{Name: "first", Type: "infra.example_credential", ProviderID: "first-id"},
			{Name: "second", Type: "infra.example_credential", ProviderID: "second-id"},
		},
		cancelAfterDelete: cancel,
	}
	var output bytes.Buffer
	code := runPreRotationPrune(ctx, provider, "infra.example_credential", "canonical", "", true, false, &output)
	if code == 0 || len(provider.deleted) != 1 {
		t.Fatalf("code=%d deleted=%v output=%s", code, provider.deleted, output.String())
	}
}

// fakeProviderWithDelete is a test double implementing the narrow
// pruneProvider interface (EnumerateAll + DeleteResource). Tracks deleted
// resources by their ProviderID (the cloud-side access_key for spaces keys)
// so tests can assert exactly which resources the prune CLI removed.
type fakeProviderWithDelete struct {
	keys               []*interfaces.ResourceOutput
	deleted            []string // ProviderIDs of resources passed to DeleteResource
	deletedRefs        []interfaces.ResourceRef
	lastType           string
	enumErr            error
	deleteErr          error
	sawCanceledContext bool
	sawDeadline        bool
	cancelAfterDelete  func()
}

func (f *fakeProviderWithDelete) EnumerateAll(ctx context.Context, resourceType string) ([]*interfaces.ResourceOutput, error) {
	f.lastType = resourceType
	_, f.sawDeadline = ctx.Deadline()
	if ctx.Err() != nil {
		f.sawCanceledContext = true
		return nil, ctx.Err()
	}
	if f.enumErr != nil {
		return nil, f.enumErr
	}
	return f.keys, nil
}

func (f *fakeProviderWithDelete) DeleteResource(_ context.Context, ref interfaces.ResourceRef) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, ref.ProviderID)
	f.deletedRefs = append(f.deletedRefs, ref)
	if f.cancelAfterDelete != nil {
		cancel := f.cancelAfterDelete
		f.cancelAfterDelete = nil
		cancel()
	}
	return nil
}

// pruneContains is a local helper so the test file doesn't depend on a
// shared 'contains' that might collide with another package-level helper.
func pruneContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// TestInfraPrune_RequiresTwoKeyOptIn locks in the two-factor opt-in for
// destructive prune: BOTH `--confirm` flag AND WFCTL_CONFIRM_PRUNE=1
// environment variable must be present, otherwise the command must reject
// the request before touching the cloud. This is the safety guard that
// prevents `prune` from running by accident.
func TestInfraPrune_RequiresTwoKeyOptIn(t *testing.T) {
	// Without --confirm flag → reject (env var not set either)
	var out bytes.Buffer
	code := runInfraPrune([]string{
		"--type", "infra.spaces_key",
		"--created-before", "2026-05-08T00:00:00Z",
		"--exclude-access-key", "AK_NEW",
	}, nil, &out)
	if code == 0 {
		t.Errorf("expected non-zero exit without --confirm; got 0; out=%s", out.String())
	}

	// With --confirm but WFCTL_CONFIRM_PRUNE not set → still reject
	out.Reset()
	code = runInfraPrune([]string{
		"--type", "infra.spaces_key",
		"--created-before", "2026-05-08T00:00:00Z",
		"--exclude-access-key", "AK_NEW",
		"--confirm",
	}, nil, &out)
	if code == 0 {
		t.Errorf("expected non-zero exit without WFCTL_CONFIRM_PRUNE=1; got 0; out=%s", out.String())
	}
}

// TestInfraPrune_RequiresExcludeAccessKey verifies the safety guard that
// prevents pruning every key in the account by accident: --exclude-access-key
// is mandatory so the operator MUST name the active credential they want to
// preserve. Without it, prune refuses even with both opt-ins.
func TestInfraPrune_RequiresExcludeAccessKey(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	var out bytes.Buffer
	code := runInfraPrune([]string{
		"--type", "infra.spaces_key",
		"--created-before", "2026-05-08T00:00:00Z",
		"--confirm",
		"--non-interactive",
	}, nil, &out)
	if code == 0 {
		t.Errorf("expected non-zero exit without --exclude-access-key; got 0")
	}
	if !strings.Contains(out.String(), "exclude-access-key") {
		t.Errorf("error message must mention --exclude-access-key requirement; got: %s", out.String())
	}
}

// TestInfraPrune_FiltersByTimeAndExcludesAccessKey is the happy-path
// regression sentinel for the prune filter: with both opt-ins satisfied
// (env + flag), --exclude-access-key naming the active credential, and
// --created-before set to "right now", prune must delete all OLD keys
// while preserving the active one. Tracks deletions by ProviderID
// (access_key) on the fake provider.
func TestInfraPrune_FiltersByTimeAndExcludesAccessKey(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	fakeProv := &fakeProviderWithDelete{
		keys: []*interfaces.ResourceOutput{
			{
				Name:       "old-1",
				Type:       "infra.spaces_key",
				ProviderID: "AK_OLD_1",
				Outputs: map[string]any{
					"access_key": "AK_OLD_1",
					"created_at": "2026-05-01T00:00:00Z",
					"name":       "old-1",
				},
			},
			{
				Name:       "old-2",
				Type:       "infra.spaces_key",
				ProviderID: "AK_OLD_2",
				Outputs: map[string]any{
					"access_key": "AK_OLD_2",
					"created_at": "2026-05-02T00:00:00Z",
					"name":       "old-2",
				},
			},
			{
				Name:       "new",
				Type:       "infra.spaces_key",
				ProviderID: "AK_NEW",
				Outputs: map[string]any{
					"access_key": "AK_NEW",
					"created_at": "2026-05-08T11:00:00Z",
					"name":       "new",
				},
			},
		},
	}
	var out bytes.Buffer
	code := runInfraPrune([]string{
		"--type", "infra.spaces_key",
		"--created-before", "2026-05-08T11:00:00Z",
		"--exclude-access-key", "AK_NEW",
		"--confirm",
		"--non-interactive",
	}, fakeProv, &out)
	if code != 0 {
		t.Fatalf("prune failed: code=%d, out=%s", code, out.String())
	}
	// AK_OLD_1, AK_OLD_2 must be deleted (older than --created-before).
	if !pruneContains(fakeProv.deleted, "AK_OLD_1") || !pruneContains(fakeProv.deleted, "AK_OLD_2") {
		t.Errorf("expected AK_OLD_1 + AK_OLD_2 deleted; got %v", fakeProv.deleted)
	}
	// AK_NEW must NOT be deleted (excluded via --exclude-access-key).
	if pruneContains(fakeProv.deleted, "AK_NEW") {
		t.Errorf("AK_NEW must NOT be deleted (excluded); got %v", fakeProv.deleted)
	}
}

func TestInfraPruneValidatesAndNormalizesInventoryBeforeDelete(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	args := []string{
		"--type", "infra.example_credential", "--created-before", "2026-05-08T11:00:00Z",
		"--exclude-access-key", "new-id", "--confirm", "--non-interactive",
	}
	valid := &interfaces.ResourceOutput{
		Name: "old", Type: "infra.example_credential", ProviderID: "old-id",
		Outputs: map[string]any{"created_at": "2026-05-01T00:00:00Z"},
	}
	for _, test := range []struct {
		name      string
		malformed *interfaces.ResourceOutput
	}{
		{name: "nil record", malformed: nil},
		{name: "empty type", malformed: &interfaces.ResourceOutput{Name: "other", ProviderID: "other-id", Outputs: map[string]any{"created_at": "2026-05-01T00:00:00Z"}}},
		{name: "mismatched type", malformed: &interfaces.ResourceOutput{Name: "other", Type: "infra.other", ProviderID: "other-id", Outputs: map[string]any{"created_at": "2026-05-01T00:00:00Z"}}},
		{name: "missing identity", malformed: &interfaces.ResourceOutput{Type: "infra.example_credential", Outputs: map[string]any{"created_at": "2026-05-01T00:00:00Z"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := &fakeProviderWithDelete{keys: []*interfaces.ResourceOutput{valid, test.malformed}}
			var output bytes.Buffer
			if code := runInfraPrune(args, provider, &output); code == 0 || len(provider.deletedRefs) != 0 {
				t.Fatalf("code=%d deletes=%+v output=%s", code, provider.deletedRefs, output.String())
			}
			if !strings.Contains(output.String(), "inventory") || !strings.Contains(output.String(), "provider error text suppressed") {
				t.Fatalf("inventory rejection output=%s", output.String())
			}
		})
	}

	provider := &fakeProviderWithDelete{keys: []*interfaces.ResourceOutput{{
		Type: "infra.example_credential",
		Outputs: map[string]any{
			"name": "legacy-name", "access_key": "legacy-id", "created_at": "2026-05-01T00:00:00Z",
		},
	}}}
	var output bytes.Buffer
	if code := runInfraPrune(args, provider, &output); code != 0 {
		t.Fatalf("legacy inventory code=%d output=%s", code, output.String())
	}
	if len(provider.deletedRefs) != 1 || provider.deletedRefs[0] != (interfaces.ResourceRef{
		Type: "infra.example_credential", Name: "legacy-name", ProviderID: "legacy-id",
	}) {
		t.Fatalf("normalized delete refs=%+v", provider.deletedRefs)
	}
}

func TestPreRotationPruneValidatesAndNormalizesInventoryBeforeDelete(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	valid := &interfaces.ResourceOutput{Name: "old", Type: "infra.example_credential", ProviderID: "old-id"}
	for _, test := range []struct {
		name      string
		malformed *interfaces.ResourceOutput
	}{
		{name: "nil record", malformed: nil},
		{name: "empty type", malformed: &interfaces.ResourceOutput{Name: "other", ProviderID: "other-id"}},
		{name: "mismatched type", malformed: &interfaces.ResourceOutput{Name: "other", Type: "infra.other", ProviderID: "other-id"}},
		{name: "missing identity", malformed: &interfaces.ResourceOutput{Type: "infra.example_credential"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := &fakeProviderWithDelete{keys: []*interfaces.ResourceOutput{valid, test.malformed}}
			var output bytes.Buffer
			code := -1
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						t.Errorf("inventory validation panicked: %v", recovered)
					}
				}()
				code = runPreRotationPrune(context.Background(), provider, "infra.example_credential", "canonical", "", true, false, &output)
			}()
			if code == 0 || len(provider.deletedRefs) != 0 {
				t.Fatalf("code=%d deletes=%+v output=%s", code, provider.deletedRefs, output.String())
			}
		})
	}

	provider := &fakeProviderWithDelete{keys: []*interfaces.ResourceOutput{{
		Type: "infra.example_credential", Outputs: map[string]any{"name": "legacy-name", "access_key": "legacy-id"},
	}}}
	var output bytes.Buffer
	if code := runPreRotationPrune(context.Background(), provider, "infra.example_credential", "canonical", "", true, false, &output); code != 0 {
		t.Fatalf("legacy inventory code=%d output=%s", code, output.String())
	}
	if len(provider.deletedRefs) != 1 || provider.deletedRefs[0] != (interfaces.ResourceRef{
		Type: "infra.example_credential", Name: "legacy-name", ProviderID: "legacy-id",
	}) {
		t.Fatalf("normalized delete refs=%+v", provider.deletedRefs)
	}
}

func TestInfraPruneDerivesTypedCutoffFromFreshExcludedInventory(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	inner := &fakeProviderWithDelete{keys: []*interfaces.ResourceOutput{
		{Name: "old", Type: "infra.example_credential", ProviderID: "old-id", Outputs: map[string]any{"created_at": "2026-05-01T00:00:00Z"}},
		{Name: "new", Type: "infra.example_credential", ProviderID: "new-id", Outputs: map[string]any{"created_at": "2026-05-08T11:00:00Z"}},
	}}
	provider := &cachedPruneProvider{
		cached:       []*interfaces.ResourceOutput{{Name: "old", Type: "infra.example_credential", ProviderID: "old-id", Outputs: map[string]any{"created_at": "2026-05-01T00:00:00Z"}}},
		resourceType: "infra.example_credential",
		inner:        inner,
	}
	var output bytes.Buffer
	code := runInfraPruneWithOptions([]string{
		"--type", "infra.example_credential", "--exclude-access-key", "new-id", "--confirm", "--non-interactive",
	}, provider, &output, infraPruneOptions{CutoffFromExcludedInventory: true})
	if code != 0 {
		t.Fatalf("code=%d output=%s", code, output.String())
	}
	if got := strings.Join(inner.deleted, ","); got != "old-id" {
		t.Fatalf("deleted=%q output=%s", got, output.String())
	}
}

func TestInfraPruneTypedInventoryCutoffFailsClosed(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	originalAttempts, originalDelay := pruneInventoryCutoffAttempts, pruneInventoryCutoffDelay
	pruneInventoryCutoffAttempts, pruneInventoryCutoffDelay = 1, 0
	t.Cleanup(func() {
		pruneInventoryCutoffAttempts, pruneInventoryCutoffDelay = originalAttempts, originalDelay
	})
	old := &interfaces.ResourceOutput{Name: "old", Type: "infra.example_credential", ProviderID: "old-id", Outputs: map[string]any{"created_at": "2026-05-01T00:00:00Z"}}
	for _, test := range []struct {
		name string
		keys []*interfaces.ResourceOutput
	}{
		{name: "missing", keys: []*interfaces.ResourceOutput{old}},
		{name: "malformed nil record", keys: []*interfaces.ResourceOutput{old, nil}},
		{name: "invalid timestamp", keys: []*interfaces.ResourceOutput{old, {Name: "new", Type: "infra.example_credential", ProviderID: "SENSITIVE_NEW", Outputs: map[string]any{"created_at": "not-a-time"}}}},
		{name: "duplicate", keys: []*interfaces.ResourceOutput{old,
			{Name: "new-a", Type: "infra.example_credential", ProviderID: "SENSITIVE_NEW", Outputs: map[string]any{"created_at": "2026-05-08T11:00:00Z"}},
			{Name: "new-b", Type: "infra.example_credential", ProviderID: "SENSITIVE_NEW", Outputs: map[string]any{"created_at": "2026-05-08T11:00:00Z"}},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := &fakeProviderWithDelete{keys: test.keys}
			var output bytes.Buffer
			code := runInfraPruneWithOptions([]string{
				"--type", "infra.example_credential", "--exclude-access-key", "SENSITIVE_NEW", "--confirm", "--non-interactive",
			}, provider, &output, infraPruneOptions{IdentifierSensitive: true, CutoffFromExcludedInventory: true})
			if code == 0 || len(provider.deleted) != 0 {
				t.Fatalf("code=%d deleted=%v output=%s", code, provider.deleted, output.String())
			}
			if strings.Contains(output.String(), "SENSITIVE_NEW") {
				t.Fatalf("sensitive excluded identifier leaked: %s", output.String())
			}
		})
	}
}

func TestInfraPruneRecoveryRetainsTypedInventoryCutoffMode(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	t.Setenv("WFCTL_STATE_DIR", t.TempDir())
	if err := writeRecoveryRecord(recoveryRecord{
		Type: "infra.example_credential", Name: "new", AccessKey: "new-id",
		CutoffFromExcludedInventory: true, CreatedAt: "provider-owned-non-rfc3339-value",
	}); err != nil {
		t.Fatal(err)
	}
	provider := &fakeProviderWithDelete{keys: []*interfaces.ResourceOutput{
		{Name: "old", Type: "infra.example_credential", ProviderID: "old-id", Outputs: map[string]any{"created_at": "2026-05-01T00:00:00Z"}},
		{Name: "new", Type: "infra.example_credential", ProviderID: "new-id", Outputs: map[string]any{"created_at": "2026-05-08T11:00:00Z"}},
	}}
	var output bytes.Buffer
	if code := runInfraPrune([]string{
		"--type", "infra.example_credential", "--recovery-from-last-rotation", "--confirm", "--non-interactive",
	}, provider, &output); code != 0 {
		t.Fatalf("code=%d output=%s", code, output.String())
	}
	if got := strings.Join(provider.deleted, ","); got != "old-id" {
		t.Fatalf("deleted=%q output=%s", got, output.String())
	}
}

func TestInfraPruneCanceledBeforeInventoryRetainsRecovery(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	stateRoot := t.TempDir()
	t.Setenv("WFCTL_STATE_DIR", stateRoot)
	if err := writeRecoveryRecord(recoveryRecord{
		Type: "infra.example_credential", Name: "new", AccessKey: "new-id", CreatedAt: "2026-05-08T11:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	provider := &fakeProviderWithDelete{}
	var output bytes.Buffer
	code := runInfraPruneWithOptions([]string{
		"--type", "infra.example_credential", "--recovery-from-last-rotation", "--confirm", "--non-interactive",
	}, provider, &output, infraPruneOptions{Context: ctx})
	if code == 0 || provider.lastType != "" || len(provider.deleted) != 0 {
		t.Fatalf("code=%d inventory type=%q deletes=%v output=%s", code, provider.lastType, provider.deleted, output.String())
	}
	if _, err := os.Stat(filepath.Join(stateRoot, "last-rotation.json")); err != nil {
		t.Fatalf("recovery file was not retained: %v", err)
	}
}

func TestInfraPruneSuccessfulZeroDeleteRecoveryRemovesRecord(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	stateDir := t.TempDir()
	t.Setenv("WFCTL_STATE_DIR", stateDir)
	if err := writeRecoveryRecord(recoveryRecord{
		Type: "infra.example_credential", Name: "new", AccessKey: "new-id", CreatedAt: "2026-05-08T11:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	provider := &fakeProviderWithDelete{keys: []*interfaces.ResourceOutput{{
		Name: "new", Type: "infra.example_credential", ProviderID: "new-id",
		Outputs: map[string]any{"created_at": "2026-05-08T11:00:00Z"},
	}}}
	var output bytes.Buffer
	if code := runInfraPrune([]string{
		"--type", "infra.example_credential", "--recovery-from-last-rotation", "--confirm", "--non-interactive",
	}, provider, &output); code != 0 {
		t.Fatalf("code=%d output=%s", code, output.String())
	}
	if _, err := os.Stat(filepath.Join(stateDir, "last-rotation.json")); !os.IsNotExist(err) {
		t.Fatalf("successful zero-delete recovery retained record: %v", err)
	}
}

func TestInfraPruneSuppressesProviderErrorText(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	args := []string{
		"--type", "infra.example_credential", "--created-before", "2026-05-08T11:00:00Z",
		"--exclude-access-key", "new-id", "--confirm", "--non-interactive",
	}
	for _, test := range []struct {
		name     string
		provider *fakeProviderWithDelete
	}{
		{name: "enumerate", provider: &fakeProviderWithDelete{enumErr: errors.New("SENSITIVE_ENUMERATE")}},
		{name: "delete", provider: &fakeProviderWithDelete{
			keys: []*interfaces.ResourceOutput{{
				Name: "old", Type: "infra.example_credential", ProviderID: "old-id",
				Outputs: map[string]any{"created_at": "2026-05-01T00:00:00Z"},
			}},
			deleteErr: errors.New("SENSITIVE_DELETE"),
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			if code := runInfraPrune(args, test.provider, &output); code == 0 {
				t.Fatalf("expected provider failure: %s", output.String())
			}
			if strings.Contains(output.String(), "SENSITIVE_") || !strings.Contains(output.String(), "provider error text suppressed") {
				t.Fatalf("provider error output=%s", output.String())
			}
		})
	}
}
