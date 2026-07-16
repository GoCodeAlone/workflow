package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// cleanupEnumFixture wires the bufconn-backed *typedIaCAdapter the cleanup
// dispatcher (infra_cleanup.go, dispatch site §Task 17) expects, alongside
// references to the recording mock servers so each test can assert delete
// counts, deleted refs, and per-call error injection after the run.
//
// Per ADR-0028 (Task 17 / PR 618 strict-contracts force-cutover): wfctl
// dispatch sites are pure typed-pb. Test fixtures must construct a real
// *typedIaCAdapter rather than injecting a custom interfaces.IaCProvider —
// the latter no longer satisfies the type-assert at the dispatch site.
type cleanupEnumFixture struct {
	adapter *typedIaCAdapter

	// Embedded mock servers — tests assert against these after a run.
	enumerator *recordingEnumeratorServer
	driver     *recordingResourceDriverServer
}

// newCleanupEnumFixture builds a *typedIaCAdapter that registers
// IaCProviderEnumerator (canned EnumerateByTag) + ResourceDriver
// (recording Delete) services. Mirrors the legacy fakeEnumeratingProvider
// that implemented interfaces.Enumerator + interfaces.ResourceDriver inline.
func newCleanupEnumFixture(t *testing.T, name string, resources []interfaces.ResourceRef, enumerateErr error, deleteErrors map[int]error) *cleanupEnumFixture {
	t.Helper()
	enum := &recordingEnumeratorServer{
		tagRefs:         resources,
		enumerateTagErr: enumerateErr,
	}
	drv := &recordingResourceDriverServer{
		deleteErrors: deleteErrors,
	}
	adapter := fixtureTypedAdapter{
		Required:       &fixtureRequiredServer{name: name, version: "0.0.0"},
		Enumerator:     enum,
		ResourceDriver: drv,
	}.build(t)
	return &cleanupEnumFixture{
		adapter:    adapter,
		enumerator: enum,
		driver:     drv,
	}
}

// newCleanupNonEnumFixture builds a *typedIaCAdapter that registers ONLY the
// IaCProviderRequired service — no Enumerator. The cleanup dispatcher must
// skip such providers with a structured stdout log (the
// `provider does not implement Enumerator` branch).
func newCleanupNonEnumFixture(t *testing.T, name string) *typedIaCAdapter {
	t.Helper()
	return fixtureTypedAdapter{
		Required: &fixtureRequiredServer{name: name, version: "0.0.0"},
	}.build(t)
}

// runInfraCleanupForTest invokes runInfraCleanup with a fake provider list
// and captures stdout/stderr. It overrides the cleanupLoadProviders seam so
// the test does not touch the live plugin loader / config file system.
func runInfraCleanupForTest(t *testing.T, providers []interfaces.IaCProvider, args ...string) (string, string, error) {
	t.Helper()
	orig := cleanupLoadProviders
	t.Cleanup(func() { cleanupLoadProviders = orig })
	cleanupLoadProviders = func(_ context.Context, _ *flag.FlagSet, _, _ string) ([]interfaces.IaCProvider, []io.Closer, error) {
		return providers, nil, nil
	}

	var stdout, stderr bytes.Buffer
	origOut, origErr := cleanupStdout, cleanupStderr
	t.Cleanup(func() { cleanupStdout, cleanupStderr = origOut, origErr })
	cleanupStdout = &stdout
	cleanupStderr = &stderr

	err := runInfraCleanup(args)
	return stdout.String(), stderr.String(), err
}

func TestInfraCleanup_DryRunByDefault_ListsResourcesWithoutDeleting(t *testing.T) {
	fp := newCleanupEnumFixture(t, "do-fake",
		[]interfaces.ResourceRef{
			{Name: "vpc-1", Type: "infra.vpc"},
			{Name: "db-1", Type: "infra.database"},
		}, nil, nil)

	out, _, err := runInfraCleanupForTest(t, []interfaces.IaCProvider{fp.adapter}, "--tag", "test-tag")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "vpc-1") {
		t.Errorf("missing vpc-1 in output: %s", out)
	}
	if !strings.Contains(out, "db-1") {
		t.Errorf("missing db-1 in output: %s", out)
	}
	if !strings.Contains(out, "[dry-run]") {
		t.Errorf("expected [dry-run] marker in output: %s", out)
	}
	if got := fp.driver.callCount(); got != 0 {
		t.Errorf("dry-run should not invoke Delete; got %d calls", got)
	}
}

func TestInfraCleanup_FixMode_DeletesResources(t *testing.T) {
	fp := newCleanupEnumFixture(t, "do-fake",
		[]interfaces.ResourceRef{
			{Name: "vpc-1", Type: "infra.vpc"},
			{Name: "db-1", Type: "infra.database"},
		}, nil, nil)

	out, _, err := runInfraCleanupForTest(t, []interfaces.IaCProvider{fp.adapter}, "--tag", "test-tag", "--fix")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := fp.driver.callCount(); got != 2 {
		t.Errorf("expected 2 Delete calls, got %d", got)
	}
	if !strings.Contains(out, "deleted") {
		t.Errorf("expected 'deleted' in output: %s", out)
	}
	if strings.Contains(out, "[dry-run]") {
		t.Errorf("--fix should not emit [dry-run] markers: %s", out)
	}
}

func TestInfraCleanup_NonEnumeratorProvider_SkipsWithStructuredLog(t *testing.T) {
	fp := newCleanupNonEnumFixture(t, "non-enum")

	out, _, err := runInfraCleanupForTest(t, []interfaces.IaCProvider{fp}, "--tag", "test-tag")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "skipped") || !strings.Contains(out, "non-enum") || !strings.Contains(out, "Enumerator") {
		t.Errorf("expected skip log mentioning provider and Enumerator; got: %s", out)
	}
}

func TestInfraCleanup_PartialFailure_ReturnsError(t *testing.T) {
	fp := newCleanupEnumFixture(t, "do-fake",
		[]interfaces.ResourceRef{
			{Name: "vpc-1", Type: "infra.vpc"},
			{Name: "db-1", Type: "infra.database"},
		},
		nil,
		// Second Delete fails (idx 1).
		map[int]error{1: errors.New("simulated failure")})

	_, _, err := runInfraCleanupForTest(t, []interfaces.IaCProvider{fp.adapter}, "--tag", "test-tag", "--fix")
	if err == nil {
		t.Errorf("expected non-nil error on partial failure")
	}
	if got := fp.driver.callCount(); got != 2 {
		t.Errorf("expected dispatcher to attempt all 2 deletes despite mid-run failure; got %d", got)
	}
}

func TestInfraCleanup_EnumerateError_ReturnsErrorAndContinuesOtherProviders(t *testing.T) {
	failing := newCleanupEnumFixture(t, "fail", nil, errors.New("simulated enumerate fail"), nil)
	working := newCleanupEnumFixture(t, "ok",
		[]interfaces.ResourceRef{{Name: "ok-1", Type: "infra.compute"}},
		nil, nil)

	out, _, err := runInfraCleanupForTest(t, []interfaces.IaCProvider{failing.adapter, working.adapter}, "--tag", "test-tag")
	if err == nil {
		t.Errorf("expected non-nil error from failing enumerator")
	}
	if !strings.Contains(out, "ok-1") {
		t.Errorf("expected output to include resources from the working provider despite the failing one: %s", out)
	}
}

func TestInfraCleanup_TagRequired(t *testing.T) {
	_, _, err := runInfraCleanupForTest(t, nil)
	if err == nil {
		t.Errorf("expected non-nil error when --tag is missing")
	}
	if err != nil && !strings.Contains(err.Error(), "--tag") {
		t.Errorf("expected error to mention --tag; got: %v", err)
	}
}

func TestDefaultCleanupLoadProvidersSuppressesLegacyLoaderErrorText(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "infra.yaml")
	configData := []byte(`modules:
  - name: example-provider
    type: iac.provider
    config:
      provider: example
`)
	if err := os.WriteFile(configPath, configData, 0o600); err != nil {
		t.Fatal(err)
	}

	originalResolver := resolveIaCProvider
	resolveIaCProvider = func(context.Context, string, map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		return nil, nil, errors.New("SENTINEL_PROVIDER_DETAIL")
	}
	t.Cleanup(func() { resolveIaCProvider = originalResolver })
	originalStderr := cleanupStderr
	var stderr bytes.Buffer
	cleanupStderr = &stderr
	t.Cleanup(func() { cleanupStderr = originalStderr })

	ctx := withProviderCapabilityDiagnosticsSuppressed(context.Background())
	providers, closers, err := defaultCleanupLoadProviders(ctx, flag.NewFlagSet("cleanup", flag.ContinueOnError), configPath, "")
	if err != nil || len(providers) != 0 || len(closers) != 0 {
		t.Fatalf("providers=%v closers=%v error=%v", providers, closers, err)
	}
	if got := stderr.String(); strings.Contains(got, "SENTINEL_PROVIDER_DETAIL") || !strings.Contains(got, "provider error text suppressed") {
		t.Fatalf("legacy loader diagnostic=%q", got)
	}
}

// TestInfraCleanup_SafeDefault_DryRunFalseWithoutFixStillSkipsDelete pins the
// safe-default invariant: passing --dry-run=false WITHOUT --fix must NOT
// delete resources. Cleanup is destructive; mutation requires the explicit
// --fix opt-in regardless of any --dry-run override. A future refactor that
// accidentally honors --dry-run=false alone would silently start deleting
// production resources from a flag a user thought was a preview toggle.
func TestInfraCleanup_SafeDefault_DryRunFalseWithoutFixStillSkipsDelete(t *testing.T) {
	fp := newCleanupEnumFixture(t, "do-fake",
		[]interfaces.ResourceRef{
			{Name: "vpc-1", Type: "infra.vpc"},
		}, nil, nil)

	out, _, err := runInfraCleanupForTest(t, []interfaces.IaCProvider{fp.adapter},
		"--tag", "test-tag", "--dry-run=false")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := fp.driver.callCount(); got != 0 {
		t.Errorf("safe-default invariant violated: --dry-run=false without --fix invoked Delete %d times; expected 0", got)
	}
	if !strings.Contains(out, "[dry-run]") {
		t.Errorf("expected [dry-run] marker even with --dry-run=false (because --fix is absent); got: %s", out)
	}
}
