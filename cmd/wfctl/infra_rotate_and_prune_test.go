package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

// fakeProviderEnumerableDriver is the test double for `runInfraRotateAndPrune`.
// It implements the EnumerateAll + DeleteResource surface that the rotate-and-
// prune CLI needs (same shape as the existing `pruneProvider` interface in
// infra_prune.go), plus a deleteErr hook so tests can force the prune step to
// fail and exercise the recovery-file-retained branch.
//
// outputs is the slice returned by EnumerateAll, deleted is the running list
// of ProviderIDs the CLI has DeleteResource'd, and deleteErr (if non-nil) is
// returned from every DeleteResource call so tests can simulate transient
// network or godo failures.
type fakeProviderEnumerableDriver struct {
	outputs   []*interfaces.ResourceOutput
	deleted   []string
	deleteErr error
}

func (f *fakeProviderEnumerableDriver) EnumerateAll(_ context.Context, _ string) ([]*interfaces.ResourceOutput, error) {
	return f.outputs, nil
}

func (f *fakeProviderEnumerableDriver) DeleteResource(_ context.Context, ref interfaces.ResourceRef) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, ref.ProviderID)
	return nil
}

// rotateAndPruneContains is a local helper rather than relying on a shared
// `contains` to avoid cross-file collisions with sibling test helpers.
func rotateAndPruneContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// TestInfraRotateAndPrune_HappyPath verifies the all-in-one flow:
//
//  1. The rotate primitive (`bootstrapSecrets` package-level test hook —
//     same pattern as `generateSecret`) returns a single RotationResult
//     describing the newly-minted credential.
//  2. The CLI persists a recovery record at $WFCTL_STATE_DIR/last-rotation.json
//     BEFORE the prune step, so a mid-prune failure can be recovered without
//     re-rotating (which would leak yet another key).
//  3. The CLI prunes every key for the same Type/Name whose ProviderID is
//     NOT the new AccessKey (AK_OLD must be deleted; AK_NEW must be preserved).
//  4. On full success, the recovery file is deleted (no leftover state).
//
// Until Task 21 lands runInfraRotateAndPrune in infra_rotate_and_prune.go,
// this test fails to compile with `undefined: runInfraRotateAndPrune` — the
// failing-side signal Task 20 is supposed to produce.
func TestInfraRotateAndPrune_HappyPath(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	tmpDir := t.TempDir()
	t.Setenv("WFCTL_STATE_DIR", tmpDir)

	// Stub bootstrapSecrets — the package-level test hook (defined in
	// infra_bootstrap.go:507 as `var bootstrapSecrets = func(...)`).
	origBoot := bootstrapSecrets
	defer func() { bootstrapSecrets = origBoot }()
	bootstrapSecrets = func(_ context.Context, _ secrets.Provider, _ *SecretsConfig, _ map[string]bool, _ ...interfaces.ProviderCredentialRevoker) (map[string]string, []RotationResult, error) {
		return map[string]string{}, []RotationResult{
			{Key: "test-key", Source: "digitalocean.spaces", AccessKey: "AK_NEW", CreatedAt: "2026-05-08T11:00:00Z"},
		}, nil
	}

	fakeProv := &fakeProviderEnumerableDriver{
		outputs: []*interfaces.ResourceOutput{
			{
				Name:       "test-key",
				Type:       "infra.spaces_key",
				ProviderID: "AK_OLD",
				Outputs: map[string]any{
					"access_key": "AK_OLD",
					"created_at": "2026-05-01T00:00:00Z",
					"name":       "test-key",
				},
			},
			{
				Name:       "test-key",
				Type:       "infra.spaces_key",
				ProviderID: "AK_NEW",
				Outputs: map[string]any{
					"access_key": "AK_NEW",
					"created_at": "2026-05-08T11:00:00Z",
					"name":       "test-key",
				},
			},
		},
	}

	var out bytes.Buffer
	code := runInfraRotateAndPrune([]string{
		"--type", "infra.spaces_key",
		"--name", "test-key",
		"--confirm", "--non-interactive",
	}, fakeProv, &out)
	if code != 0 {
		t.Fatalf("rotate-and-prune failed: code=%d, out=%s", code, out.String())
	}

	// Recovery file is deleted on full success — the only durable evidence
	// left behind is the new credential itself in the secrets store + the
	// pruned-key state.
	if _, err := os.Stat(filepath.Join(tmpDir, "last-rotation.json")); !os.IsNotExist(err) {
		t.Errorf("recovery file should be deleted after successful prune; stat err = %v", err)
	}

	// AK_OLD must be deleted (older than AK_NEW + matches Name); AK_NEW
	// must be preserved (it's the freshly-rotated key).
	if !rotateAndPruneContains(fakeProv.deleted, "AK_OLD") {
		t.Errorf("AK_OLD must be deleted; deleted = %v", fakeProv.deleted)
	}
	if rotateAndPruneContains(fakeProv.deleted, "AK_NEW") {
		t.Errorf("AK_NEW must be preserved (just rotated in); deleted = %v", fakeProv.deleted)
	}
}

// TestInfraRotateAndPrune_RecoveryFileWrittenWithCorrectPerms verifies the
// partial-failure semantics: when the prune step fails (here: simulated
// network error from DeleteResource), the recovery record at
// $WFCTL_STATE_DIR/last-rotation.json must:
//
//  1. Be retained (NOT deleted), so a follow-up `wfctl infra prune
//     --recovery-from-last-rotation` invocation can complete the prune
//     without re-rotating.
//  2. Have permissions 0600 (owner-readable only) — the file contains
//     enough metadata (access_key + name) to reconstruct which key the
//     prune-by-time filter would target.
//
// This test is the safety-net side of Task 21's design; it pins the
// invariant that a partial-failure rotation never silently loses the
// recovery information the operator needs to finish cleanup.
func TestInfraRotateAndPrune_RecoveryFileWrittenWithCorrectPerms(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	tmpDir := t.TempDir()
	t.Setenv("WFCTL_STATE_DIR", tmpDir)

	origBoot := bootstrapSecrets
	defer func() { bootstrapSecrets = origBoot }()
	bootstrapSecrets = func(_ context.Context, _ secrets.Provider, _ *SecretsConfig, _ map[string]bool, _ ...interfaces.ProviderCredentialRevoker) (map[string]string, []RotationResult, error) {
		return map[string]string{}, []RotationResult{
			{Key: "test-key", Source: "digitalocean.spaces", AccessKey: "AK_NEW", CreatedAt: "2026-05-08T11:00:00Z"},
		}, nil
	}

	fakeProv := &fakeProviderEnumerableDriver{
		deleteErr: errors.New("simulated network failure"),
		outputs: []*interfaces.ResourceOutput{
			{
				Name:       "test-key",
				Type:       "infra.spaces_key",
				ProviderID: "AK_OLD",
				Outputs: map[string]any{
					"access_key": "AK_OLD",
					"created_at": "2026-05-01T00:00:00Z",
					"name":       "test-key",
				},
			},
		},
	}

	// Run; ignore exit code (we expect non-zero from the prune-failure path,
	// but the contract under test is the recovery file).
	_ = runInfraRotateAndPrune([]string{
		"--type", "infra.spaces_key",
		"--name", "test-key",
		"--confirm", "--non-interactive",
	}, fakeProv, io.Discard)

	info, err := os.Stat(filepath.Join(tmpDir, "last-rotation.json"))
	if err != nil {
		t.Fatalf("recovery file should be retained on failure; stat err = %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("recovery file perms must be 0600 (owner-only); got %o", perm)
	}
}
