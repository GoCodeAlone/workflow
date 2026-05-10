package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// ── fixtures for refresh tests ──────────────────────────────────────────────────

// newRefreshDriftFixture builds a *typedIaCAdapter that registers the typed
// IaCProviderDriftDetector service so the apply-refresh dispatch site
// (cmd/wfctl/infra_apply_refresh.go, dispatch site §Task 17) reaches the
// adapter's interfaces.IaCProvider.DetectDrift method via the typed RPC.
//
// Per ADR-0028 (Task 17 / PR 618 strict-contracts force-cutover): wfctl
// dispatch sites are pure typed-pb. The dispatch type-asserts
// `provider.(*typedIaCAdapter)` first; legacy fakes that satisfied
// interfaces.IaCProvider directly no longer reach the dispatch path. The
// migrated fixture wraps a recordingDriftDetectorServer in a real adapter
// so the test exercises the same wire shape production sees.
//
// Note: DriftConfigDetector is intentionally NOT registered. Tests pass
// either nil states (no AppliedConfig present → buildAppliedSpecMap returns
// nil) or states with no AppliedConfigSource ("apply") provenance (also
// returns nil). The dispatcher therefore falls back to the required-side
// DetectDrift path which the IaCProviderDriftDetector service backs.
func newRefreshDriftFixture(t *testing.T, driftResults []interfaces.DriftResult, driftErr error) interfaces.IaCProvider {
	t.Helper()
	return fixtureTypedAdapter{
		Required: &fixtureRequiredServer{name: "fake-refresh", version: "0.0.0"},
		DriftDetector: &recordingDriftDetectorServer{
			driftResults: driftResults,
			driftErr:     driftErr,
		},
	}.build(t)
}

// countingStore is an infraStateStore that counts DeleteResource calls and
// records the deleted names.
type countingStore struct {
	deleteCount  int
	deletedNames []string
}

func (c *countingStore) ListResources(_ context.Context) ([]interfaces.ResourceState, error) {
	return nil, nil
}
func (c *countingStore) SaveResource(_ context.Context, _ interfaces.ResourceState) error {
	return nil
}
func (c *countingStore) DeleteResource(_ context.Context, name string) error {
	c.deleteCount++
	c.deletedNames = append(c.deletedNames, name)
	return nil
}

// stateWithProtected returns []ResourceState where resourceName has protected=true in Outputs.
func stateWithProtected(resourceName string) []interfaces.ResourceState {
	return []interfaces.ResourceState{
		{
			ID:      resourceName,
			Name:    resourceName,
			Type:    "infra.vpc",
			Outputs: map[string]any{"protected": true},
		},
	}
}

// ── tests ──────────────────────────────────────────────────────────────────────

func TestApplyRefresh_DryRunPrintsPrunesWithoutMutating(t *testing.T) {
	ghost := interfaces.DriftResult{
		Name:    "test-vpc",
		Type:    "infra.vpc",
		Drifted: true,
		Class:   interfaces.DriftClassGhost,
	}
	provider := newRefreshDriftFixture(t, []interfaces.DriftResult{ghost}, nil)
	store := &countingStore{}
	refs := []interfaces.ResourceRef{{Name: "test-vpc", Type: "infra.vpc"}}

	var stdout, stderr bytes.Buffer
	err := runInfraApplyRefreshPhase(context.Background(), provider, refs, store,
		false /* autoApprove */, false /* allowProtectedPrune */, nil /* states */, &stdout, &stderr)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.deleteCount != 0 {
		t.Errorf("dry-run: expected 0 deletes, got %d", store.deleteCount)
	}
	if !strings.Contains(stdout.String(), "would prune") {
		t.Errorf("dry-run: expected 'would prune' in stdout, got:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "test-vpc") {
		t.Errorf("dry-run: expected resource name in output, got:\n%s", stdout.String())
	}
}

func TestApplyRefresh_AutoApprovePrunesAndApplies(t *testing.T) {
	ghost := interfaces.DriftResult{
		Name:    "test-vpc",
		Type:    "infra.vpc",
		Drifted: true,
		Class:   interfaces.DriftClassGhost,
	}
	provider := newRefreshDriftFixture(t, []interfaces.DriftResult{ghost}, nil)
	store := &countingStore{}
	refs := []interfaces.ResourceRef{{Name: "test-vpc", Type: "infra.vpc"}}

	var stdout, stderr bytes.Buffer
	err := runInfraApplyRefreshPhase(context.Background(), provider, refs, store,
		true /* autoApprove */, false /* allowProtectedPrune */, nil /* states */, &stdout, &stderr)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.deleteCount != 1 {
		t.Errorf("auto-approve: expected 1 delete, got %d", store.deleteCount)
	}
	if len(store.deletedNames) == 0 || store.deletedNames[0] != "test-vpc" {
		t.Errorf("auto-approve: expected deleted name=test-vpc, got %v", store.deletedNames)
	}
	// Audit log must appear on stderr
	if !strings.Contains(stderr.String(), "test-vpc") {
		t.Errorf("auto-approve: expected audit log on stderr mentioning test-vpc, got:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "state mutation prune") {
		t.Errorf("auto-approve: expected 'state mutation prune' operation keyword in audit log on stderr, got:\n%s", stderr.String())
	}
}

func TestApplyRefresh_ProtectedResourceBlockedWithoutFlag(t *testing.T) {
	ghost := interfaces.DriftResult{
		Name:    "protected-vpc",
		Type:    "infra.vpc",
		Drifted: true,
		Class:   interfaces.DriftClassGhost,
	}
	provider := newRefreshDriftFixture(t, []interfaces.DriftResult{ghost}, nil)
	store := &countingStore{}
	refs := []interfaces.ResourceRef{{Name: "protected-vpc", Type: "infra.vpc"}}
	states := stateWithProtected("protected-vpc")

	var stdout, stderr bytes.Buffer
	err := runInfraApplyRefreshPhase(context.Background(), provider, refs, store,
		true /* autoApprove */, false /* allowProtectedPrune — NOT set */, states, &stdout, &stderr)

	if err == nil {
		t.Fatal("expected error for protected resource without flag, got nil")
	}
	if !strings.Contains(err.Error(), "protected") {
		t.Errorf("expected error to mention 'protected', got: %v", err)
	}
	if store.deleteCount != 0 {
		t.Errorf("protected: expected 0 deletes, got %d", store.deleteCount)
	}
}

func TestApplyRefresh_ProtectedResourcePrunedWithFlag(t *testing.T) {
	ghost := interfaces.DriftResult{
		Name:    "protected-vpc",
		Type:    "infra.vpc",
		Drifted: true,
		Class:   interfaces.DriftClassGhost,
	}
	provider := newRefreshDriftFixture(t, []interfaces.DriftResult{ghost}, nil)
	store := &countingStore{}
	refs := []interfaces.ResourceRef{{Name: "protected-vpc", Type: "infra.vpc"}}
	states := stateWithProtected("protected-vpc")

	var stdout, stderr bytes.Buffer
	err := runInfraApplyRefreshPhase(context.Background(), provider, refs, store,
		true /* autoApprove */, true /* allowProtectedPrune */, states, &stdout, &stderr)

	if err != nil {
		t.Fatalf("unexpected error with allow-protected-prune: %v", err)
	}
	if store.deleteCount != 1 {
		t.Errorf("allow-protected-prune: expected 1 delete, got %d", store.deleteCount)
	}
	// Audit log must appear on stderr mentioning the resource
	if !strings.Contains(stderr.String(), "protected-vpc") {
		t.Errorf("allow-protected-prune: expected audit log on stderr, got:\n%s", stderr.String())
	}
}

func TestApplyRefresh_TransientErrorDoesNotPrune(t *testing.T) {
	transientErr := errors.New("DO API rate limit exceeded")
	provider := newRefreshDriftFixture(t, nil, transientErr)
	store := &countingStore{}
	refs := []interfaces.ResourceRef{{Name: "test-vpc", Type: "infra.vpc"}}

	var stdout, stderr bytes.Buffer
	err := runInfraApplyRefreshPhase(context.Background(), provider, refs, store,
		true /* autoApprove */, false /* allowProtectedPrune */, nil /* states */, &stdout, &stderr)

	if err == nil {
		t.Fatal("expected transient error to propagate, got nil")
	}
	// The transient error string must propagate through the gRPC wire +
	// translateRPCErr. Since translateRPCErr only converts gRPC
	// codes.Unimplemented (the fake here returns a plain non-gRPC error
	// which gRPC will wrap with codes.Unknown), the original message text
	// is preserved in the wrapped error chain — we assert against the
	// substring rather than errors.Is on the local sentinel because the
	// gRPC wire boundary doesn't preserve identity across processes.
	if !strings.Contains(err.Error(), "DO API rate limit exceeded") {
		t.Errorf("expected wrapped transient error (substring match across gRPC wire); got: %v", err)
	}
	if store.deleteCount != 0 {
		t.Errorf("transient error: expected 0 deletes, got %d", store.deleteCount)
	}
}

func TestApplyRefresh_InSyncResourceSkipped(t *testing.T) {
	inSync := interfaces.DriftResult{
		Name:    "test-vpc",
		Type:    "infra.vpc",
		Drifted: false,
		Class:   interfaces.DriftClassInSync,
	}
	provider := newRefreshDriftFixture(t, []interfaces.DriftResult{inSync}, nil)
	store := &countingStore{}
	refs := []interfaces.ResourceRef{{Name: "test-vpc", Type: "infra.vpc"}}

	var stdout, stderr bytes.Buffer
	err := runInfraApplyRefreshPhase(context.Background(), provider, refs, store,
		true /* autoApprove */, false /* allowProtectedPrune */, nil, &stdout, &stderr)

	if err != nil {
		t.Fatalf("unexpected error for in-sync: %v", err)
	}
	if store.deleteCount != 0 {
		t.Errorf("in-sync: expected 0 deletes, got %d", store.deleteCount)
	}
}

// TestApplyRefresh_MultipleGhostsAllOrNothing verifies that when a list of
// drift results contains at least one protected ghost without --allow-protected-prune,
// NO state mutations happen (deleteCount==0) even if other ghosts are unprotected.
// The error must mention all blocked resource names.
func TestApplyRefresh_MultipleGhostsAllOrNothing(t *testing.T) {
	ghosts := []interfaces.DriftResult{
		{Name: "unprotected-vpc", Type: "infra.vpc", Drifted: true, Class: interfaces.DriftClassGhost},
		{Name: "protected-db", Type: "infra.database", Drifted: true, Class: interfaces.DriftClassGhost},
		{Name: "another-unprotected", Type: "infra.vpc", Drifted: true, Class: interfaces.DriftClassGhost},
		{Name: "protected-cache", Type: "infra.cache", Drifted: true, Class: interfaces.DriftClassGhost},
	}
	// Only the two protected resources are in states
	states := []interfaces.ResourceState{
		{ID: "protected-db", Name: "protected-db", Type: "infra.database", Outputs: map[string]any{"protected": true}},
		{ID: "protected-cache", Name: "protected-cache", Type: "infra.cache", Outputs: map[string]any{"protected": true}},
	}
	provider := newRefreshDriftFixture(t, ghosts, nil)
	store := &countingStore{}
	refs := []interfaces.ResourceRef{
		{Name: "unprotected-vpc", Type: "infra.vpc"},
		{Name: "protected-db", Type: "infra.database"},
		{Name: "another-unprotected", Type: "infra.vpc"},
		{Name: "protected-cache", Type: "infra.cache"},
	}

	var stdout, stderr bytes.Buffer
	err := runInfraApplyRefreshPhase(context.Background(), provider, refs, store,
		true /* autoApprove */, false /* allowProtectedPrune — NOT set */, states, &stdout, &stderr)

	if err == nil {
		t.Fatal("expected error for protected resources without flag, got nil")
	}
	// Error must list ALL blocked names
	if !strings.Contains(err.Error(), "protected-db") {
		t.Errorf("expected error to mention 'protected-db', got: %v", err)
	}
	if !strings.Contains(err.Error(), "protected-cache") {
		t.Errorf("expected error to mention 'protected-cache', got: %v", err)
	}
	// Critical: zero mutations — unprotected ghosts must NOT have been pruned
	if store.deleteCount != 0 {
		t.Errorf("all-or-nothing: expected 0 deletes before error, got %d (deleted: %v)",
			store.deleteCount, store.deletedNames)
	}
}

// TestApplyRefresh_UnprotectedThenProtected_NoPartialPrune is a minimal
// regression test for the atomicity fix: when the first ghost is unprotected
// and the second is protected, the single-pass implementation would prune the
// first before discovering the blocker. The two-pass implementation must return
// an error AND leave deleteCount==0.
func TestApplyRefresh_UnprotectedThenProtected_NoPartialPrune(t *testing.T) {
	ghosts := []interfaces.DriftResult{
		{Name: "vpc-a", Type: "infra.vpc", Drifted: true, Class: interfaces.DriftClassGhost},
		{Name: "db-staging", Type: "infra.database", Drifted: true, Class: interfaces.DriftClassGhost},
	}
	states := []interfaces.ResourceState{
		{ID: "db-staging", Name: "db-staging", Type: "infra.database", Outputs: map[string]any{"protected": true}},
	}
	provider := newRefreshDriftFixture(t, ghosts, nil)
	store := &countingStore{}
	refs := []interfaces.ResourceRef{
		{Name: "vpc-a", Type: "infra.vpc"},
		{Name: "db-staging", Type: "infra.database"},
	}

	var stdout, stderr bytes.Buffer
	err := runInfraApplyRefreshPhase(context.Background(), provider, refs, store,
		true /* autoApprove */, false /* allowProtectedPrune — NOT set */, states, &stdout, &stderr)

	if err == nil {
		t.Fatal("expected error for protected resource without flag, got nil")
	}
	if !strings.Contains(err.Error(), "db-staging") {
		t.Errorf("expected error to name the blocked resource 'db-staging', got: %v", err)
	}
	// The critical invariant: vpc-a must NOT have been pruned before the blocker was found.
	if store.deleteCount != 0 {
		t.Errorf("partial-prune: expected 0 deletes (all-or-nothing), got %d (deleted: %v)",
			store.deleteCount, store.deletedNames)
	}
}

// TestApplyRefresh_AllGhostsUnprotectedPrunesAll verifies that when all ghosts
// are unprotected, the pre-scan passes and all mutations proceed normally.
func TestApplyRefresh_AllGhostsUnprotectedPrunesAll(t *testing.T) {
	ghosts := []interfaces.DriftResult{
		{Name: "ghost-1", Type: "infra.vpc", Drifted: true, Class: interfaces.DriftClassGhost},
		{Name: "ghost-2", Type: "infra.database", Drifted: true, Class: interfaces.DriftClassGhost},
	}
	provider := newRefreshDriftFixture(t, ghosts, nil)
	store := &countingStore{}
	refs := []interfaces.ResourceRef{
		{Name: "ghost-1", Type: "infra.vpc"},
		{Name: "ghost-2", Type: "infra.database"},
	}

	var stdout, stderr bytes.Buffer
	err := runInfraApplyRefreshPhase(context.Background(), provider, refs, store,
		true /* autoApprove */, false /* allowProtectedPrune */, nil /* no states */, &stdout, &stderr)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.deleteCount != 2 {
		t.Errorf("expected 2 deletes, got %d", store.deleteCount)
	}
}
