package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fakeEnumeratingProvider is an IaCProvider that ALSO implements Enumerator.
// EnumerateByTag returns a canned slice; ResourceDriver returns a fake driver
// that records Delete calls (and may return an error per index).
type fakeEnumeratingProvider struct {
	stubIaCProvider
	resources       []interfaces.ResourceRef
	deleteCallCount int
	deleteErrors    map[int]error
	enumerateErr    error
}

func (f *fakeEnumeratingProvider) EnumerateByTag(_ context.Context, _ string) ([]interfaces.ResourceRef, error) {
	if f.enumerateErr != nil {
		return nil, f.enumerateErr
	}
	return f.resources, nil
}

func (f *fakeEnumeratingProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return &fakeDeleteDriver{owner: f}, nil
}

// fakeDeleteDriver implements just enough of interfaces.ResourceDriver for
// the cleanup dispatch path: Delete is the only method exercised. Other
// methods are no-op stubs to satisfy the interface.
type fakeDeleteDriver struct{ owner *fakeEnumeratingProvider }

func (d *fakeDeleteDriver) Create(context.Context, interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeDeleteDriver) Read(context.Context, interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeDeleteDriver) Update(context.Context, interfaces.ResourceRef, interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeDeleteDriver) Diff(context.Context, interfaces.ResourceSpec, *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	return nil, nil
}
func (d *fakeDeleteDriver) HealthCheck(context.Context, interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return nil, nil
}
func (d *fakeDeleteDriver) Scale(context.Context, interfaces.ResourceRef, int) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeDeleteDriver) SensitiveKeys() []string { return nil }
func (d *fakeDeleteDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error {
	idx := d.owner.deleteCallCount
	d.owner.deleteCallCount++
	if d.owner.deleteErrors != nil {
		if err, ok := d.owner.deleteErrors[idx]; ok {
			return err
		}
	}
	return nil
}

// fakeNonEnumeratingProvider is an IaCProvider that does NOT implement
// Enumerator (uses the bare stubIaCProvider). The cleanup dispatcher must
// skip it with a structured stdout log line rather than failing.
type fakeNonEnumeratingProvider struct{ stubIaCProvider }

// runInfraCleanupForTest invokes runInfraCleanup with a fake provider list
// and captures stdout/stderr. It overrides the cleanupLoadProviders seam so
// the test does not touch the live plugin loader / config file system.
func runInfraCleanupForTest(t *testing.T, providers []interfaces.IaCProvider, args ...string) (string, string, error) {
	t.Helper()
	orig := cleanupLoadProviders
	t.Cleanup(func() { cleanupLoadProviders = orig })
	cleanupLoadProviders = func(_ context.Context, _, _ string) ([]interfaces.IaCProvider, []io.Closer, error) {
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
	fp := &fakeEnumeratingProvider{
		stubIaCProvider: stubIaCProvider{name: "do-fake"},
		resources: []interfaces.ResourceRef{
			{Name: "vpc-1", Type: "infra.vpc"},
			{Name: "db-1", Type: "infra.database"},
		},
	}

	out, _, err := runInfraCleanupForTest(t, []interfaces.IaCProvider{fp}, "--tag", "test-tag")
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
	if fp.deleteCallCount != 0 {
		t.Errorf("dry-run should not invoke Delete; got %d calls", fp.deleteCallCount)
	}
}

func TestInfraCleanup_FixMode_DeletesResources(t *testing.T) {
	fp := &fakeEnumeratingProvider{
		stubIaCProvider: stubIaCProvider{name: "do-fake"},
		resources: []interfaces.ResourceRef{
			{Name: "vpc-1", Type: "infra.vpc"},
			{Name: "db-1", Type: "infra.database"},
		},
	}

	out, _, err := runInfraCleanupForTest(t, []interfaces.IaCProvider{fp}, "--tag", "test-tag", "--fix")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fp.deleteCallCount != 2 {
		t.Errorf("expected 2 Delete calls, got %d", fp.deleteCallCount)
	}
	if !strings.Contains(out, "deleted") {
		t.Errorf("expected 'deleted' in output: %s", out)
	}
	if strings.Contains(out, "[dry-run]") {
		t.Errorf("--fix should not emit [dry-run] markers: %s", out)
	}
}

func TestInfraCleanup_NonEnumeratorProvider_SkipsWithStructuredLog(t *testing.T) {
	fp := &fakeNonEnumeratingProvider{stubIaCProvider: stubIaCProvider{name: "non-enum"}}

	out, _, err := runInfraCleanupForTest(t, []interfaces.IaCProvider{fp}, "--tag", "test-tag")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "skipped") || !strings.Contains(out, "non-enum") || !strings.Contains(out, "Enumerator") {
		t.Errorf("expected skip log mentioning provider and Enumerator; got: %s", out)
	}
}

func TestInfraCleanup_PartialFailure_ReturnsError(t *testing.T) {
	fp := &fakeEnumeratingProvider{
		stubIaCProvider: stubIaCProvider{name: "do-fake"},
		resources: []interfaces.ResourceRef{
			{Name: "vpc-1", Type: "infra.vpc"},
			{Name: "db-1", Type: "infra.database"},
		},
		// Second Delete fails (idx 1).
		deleteErrors: map[int]error{1: errors.New("simulated failure")},
	}

	_, _, err := runInfraCleanupForTest(t, []interfaces.IaCProvider{fp}, "--tag", "test-tag", "--fix")
	if err == nil {
		t.Errorf("expected non-nil error on partial failure")
	}
	if fp.deleteCallCount != 2 {
		t.Errorf("expected dispatcher to attempt all 2 deletes despite mid-run failure; got %d", fp.deleteCallCount)
	}
}

func TestInfraCleanup_EnumerateError_ReturnsErrorAndContinuesOtherProviders(t *testing.T) {
	failing := &fakeEnumeratingProvider{
		stubIaCProvider: stubIaCProvider{name: "fail"},
		enumerateErr:    errors.New("simulated enumerate fail"),
	}
	working := &fakeEnumeratingProvider{
		stubIaCProvider: stubIaCProvider{name: "ok"},
		resources:       []interfaces.ResourceRef{{Name: "ok-1", Type: "infra.compute"}},
	}

	out, _, err := runInfraCleanupForTest(t, []interfaces.IaCProvider{failing, working}, "--tag", "test-tag")
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
