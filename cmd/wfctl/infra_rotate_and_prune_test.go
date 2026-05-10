package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// writeMinimalRotationConfig writes a minimal infra.yaml fixture sufficient
// for runInfraRotateAndPrune to load without erroring before reaching the
// stubbed `bootstrapSecrets` hook. The config provides a `secrets:` block
// (so parseSecretsConfig returns non-nil) with provider=env so
// resolveSecretsProvider builds without external dependencies.
//
// Two generate entries are declared so callers can use either --name
// "test-key" (key + name both) or --name "canonical-name" (cloud-side
// name; key="canonical-key") — the latter exercises the name→key
// translation in buildRotateAndPruneForceRotateSet introduced to fix
// the staging-dispatch false-negative (run 25616807427, 2026-05-09).
//
// Returns the fixture's full path for use as `--config` arg.
func writeMinimalRotationConfig(t *testing.T, tmpDir string) string {
	t.Helper()
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
    - key: canonical-key
      type: provider_credential
      source: digitalocean.spaces
      name: canonical-name
`
	if err := os.WriteFile(cfgPath, []byte(body), 0600); err != nil {
		t.Fatalf("write fixture infra.yaml: %v", err)
	}
	return cfgPath
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
	cfgPath := writeMinimalRotationConfig(t, tmpDir)

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
		"--config", cfgPath,
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
	cfgPath := writeMinimalRotationConfig(t, tmpDir)

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
		"--config", cfgPath,
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

// quotaCappedFakeProvider simulates a cloud account at the per-resource-type
// quota (e.g., DO Spaces 200-key limit). EnumerateAll returns the current
// `outputs` slice; DeleteResource shrinks `outputs` (removes the matching
// ProviderID) and records the delete in `deleted`.
//
// `createOnRotate` is the metadata the bootstrapSecrets stub will return for
// the new key. The test driver invokes the stub which appends this to
// `outputs`; if `len(outputs) >= quota` at append time the stub errors with
// quotaErr to simulate the at-quota Create failure. Tests assert that with
// --prune-first=true the orphan deletes happen FIRST, freeing room before
// the stub appends, so the rotation succeeds.
type quotaCappedFakeProvider struct {
	outputs []*interfaces.ResourceOutput
	deleted []string
}

func (f *quotaCappedFakeProvider) EnumerateAll(_ context.Context, _ string) ([]*interfaces.ResourceOutput, error) {
	// Return a copy to mimic the stable-snapshot semantics the dispatcher's
	// cachedPruneProvider provides in production. A test that mutates the
	// returned slice should not affect subsequent EnumerateAll calls.
	out := make([]*interfaces.ResourceOutput, len(f.outputs))
	copy(out, f.outputs)
	return out, nil
}

func (f *quotaCappedFakeProvider) DeleteResource(_ context.Context, ref interfaces.ResourceRef) error {
	for i, o := range f.outputs {
		if o.ProviderID == ref.ProviderID {
			f.outputs = append(f.outputs[:i], f.outputs[i+1:]...)
			f.deleted = append(f.deleted, ref.ProviderID)
			return nil
		}
	}
	return errors.New("not found: " + ref.ProviderID)
}

// TestRotateAndPrune_PruneFirst_HappyPath_AtQuota verifies the at-quota
// chicken-and-egg fix (ADR 0023): when the cloud account has orphan
// resources whose names don't match the canonical `--name`, --prune-first=true
// (the default) deletes them BEFORE the rotate step. Without this flip,
// rotation's mint-new step would fail with "quota exceeded" before the
// post-rotation prune ever runs to free quota.
//
// The test models the at-quota condition by stuffing the fake provider with
// orphan keys (names != canonical), running rotate-and-prune with the
// default flag (--prune-first=true), and asserting:
//
//  1. The pre-prune step deleted every orphan key (names != "canonical-name"
//     and not matching --preserve-names).
//  2. The rotate step ran successfully (bootstrapSecrets stub returned its
//     RotationResult, indicating no quota error).
//  3. The post-prune defensive sweep ran cleanly (no error).
//  4. The recovery file was deleted on full success.
func TestRotateAndPrune_PruneFirst_HappyPath_AtQuota(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	tmpDir := t.TempDir()
	t.Setenv("WFCTL_STATE_DIR", tmpDir)
	cfgPath := writeMinimalRotationConfig(t, tmpDir)

	origBoot := bootstrapSecrets
	defer func() { bootstrapSecrets = origBoot }()
	bootstrapSecrets = func(_ context.Context, _ secrets.Provider, _ *SecretsConfig, _ map[string]bool, _ ...interfaces.ProviderCredentialRevoker) (map[string]string, []RotationResult, error) {
		return map[string]string{}, []RotationResult{
			{Key: "canonical-name", Source: "digitalocean.spaces", AccessKey: "AK_NEW", CreatedAt: "2026-05-09T11:00:00Z"},
		}, nil
	}

	// Two orphan keys (names != canonical) + one key matching the canonical
	// name (the OLD canonical credential the rotate step replaces in-place).
	// Pre-prune must delete the orphans, skip the canonical-named key.
	fakeProv := &quotaCappedFakeProvider{
		outputs: []*interfaces.ResourceOutput{
			{
				Name:       "orphan-1",
				Type:       "infra.spaces_key",
				ProviderID: "AK_ORPHAN_1",
				Outputs: map[string]any{
					"access_key": "AK_ORPHAN_1",
					"created_at": "2026-04-01T00:00:00Z",
					"name":       "orphan-1",
				},
			},
			{
				Name:       "orphan-2",
				Type:       "infra.spaces_key",
				ProviderID: "AK_ORPHAN_2",
				Outputs: map[string]any{
					"access_key": "AK_ORPHAN_2",
					"created_at": "2026-04-15T00:00:00Z",
					"name":       "orphan-2",
				},
			},
			{
				Name:       "canonical-name",
				Type:       "infra.spaces_key",
				ProviderID: "AK_OLD_CANONICAL",
				Outputs: map[string]any{
					"access_key": "AK_OLD_CANONICAL",
					"created_at": "2026-05-01T00:00:00Z",
					"name":       "canonical-name",
				},
			},
		},
	}

	var out bytes.Buffer
	code := runInfraRotateAndPrune([]string{
		"--type", "infra.spaces_key",
		"--name", "canonical-name",
		"--config", cfgPath,
		"--confirm", "--non-interactive",
		// --prune-first defaulted to true; explicitly omitted to verify the
		// default path is the at-quota-safe path.
	}, fakeProv, &out)
	if code != 0 {
		t.Fatalf("rotate-and-prune (--prune-first default) failed: code=%d, out=%s", code, out.String())
	}

	// Pre-prune deleted both orphans (names != canonical, not in preserve regex).
	if !rotateAndPruneContains(fakeProv.deleted, "AK_ORPHAN_1") {
		t.Errorf("AK_ORPHAN_1 must be pre-pruned; deleted = %v; out=%s", fakeProv.deleted, out.String())
	}
	if !rotateAndPruneContains(fakeProv.deleted, "AK_ORPHAN_2") {
		t.Errorf("AK_ORPHAN_2 must be pre-pruned; deleted = %v; out=%s", fakeProv.deleted, out.String())
	}

	// Post-prune deleted the OLD canonical key (created before AK_NEW;
	// access_key != AK_NEW).
	if !rotateAndPruneContains(fakeProv.deleted, "AK_OLD_CANONICAL") {
		t.Errorf("AK_OLD_CANONICAL must be post-pruned (older than rotation); deleted = %v; out=%s", fakeProv.deleted, out.String())
	}

	// Output banners include both Step 0 and Step 2 (defensive sweep) markers.
	got := out.String()
	if !strings.Contains(got, "Step 0") || !strings.Contains(got, "--prune-first") {
		t.Errorf("output should narrate Step 0 (--prune-first); got: %s", got)
	}
	if !strings.Contains(got, "defensive sweep") {
		t.Errorf("output should narrate Step 2 as defensive sweep when prune-first=true; got: %s", got)
	}

	// Recovery file deleted on full success.
	if _, err := os.Stat(filepath.Join(tmpDir, "last-rotation.json")); !os.IsNotExist(err) {
		t.Errorf("recovery file should be deleted on full success; stat err = %v", err)
	}
}

// TestRotateAndPrune_PruneFirst_DefaultTrue locks in the ADR 0023 default
// flip: omitting --prune-first must use the safer behavior. Without this
// regression sentinel, a future refactor could silently revert the default
// to false and reintroduce the at-quota chicken-and-egg.
//
// The test observes the default by checking that with NO --prune-first arg
// passed and orphan resources in the fake provider, the pre-prune step
// runs and deletes the orphans (vs. the legacy ordering, where they
// would still be present after rotation because the post-prune time
// filter catches only same-name keys).
func TestRotateAndPrune_PruneFirst_DefaultTrue(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	tmpDir := t.TempDir()
	t.Setenv("WFCTL_STATE_DIR", tmpDir)
	cfgPath := writeMinimalRotationConfig(t, tmpDir)

	origBoot := bootstrapSecrets
	defer func() { bootstrapSecrets = origBoot }()
	bootstrapSecrets = func(_ context.Context, _ secrets.Provider, _ *SecretsConfig, _ map[string]bool, _ ...interfaces.ProviderCredentialRevoker) (map[string]string, []RotationResult, error) {
		return map[string]string{}, []RotationResult{
			{Key: "canonical-name", Source: "digitalocean.spaces", AccessKey: "AK_NEW", CreatedAt: "2026-05-09T11:00:00Z"},
		}, nil
	}

	// One orphan with a created_at NEWER than AK_NEW. Under legacy ordering
	// the post-prune time filter would skip it (created_at >= cutoff); under
	// the new default the pre-prune name filter targets it.
	fakeProv := &quotaCappedFakeProvider{
		outputs: []*interfaces.ResourceOutput{
			{
				Name:       "orphan-future",
				Type:       "infra.spaces_key",
				ProviderID: "AK_ORPHAN_FUTURE",
				Outputs: map[string]any{
					"access_key": "AK_ORPHAN_FUTURE",
					"created_at": "2027-01-01T00:00:00Z", // newer than AK_NEW's 2026-05-09
					"name":       "orphan-future",
				},
			},
		},
	}

	var out bytes.Buffer
	code := runInfraRotateAndPrune([]string{
		"--type", "infra.spaces_key",
		"--name", "canonical-name",
		"--config", cfgPath,
		"--confirm", "--non-interactive",
	}, fakeProv, &out)
	if code != 0 {
		t.Fatalf("rotate-and-prune failed: code=%d, out=%s", code, out.String())
	}

	// The default-true flag means the pre-prune ran and deleted the orphan,
	// even though its created_at is newer than the rotation timestamp.
	if !rotateAndPruneContains(fakeProv.deleted, "AK_ORPHAN_FUTURE") {
		t.Errorf("AK_ORPHAN_FUTURE must be pre-pruned by default (--prune-first=true); deleted = %v", fakeProv.deleted)
	}
}

// TestRotateAndPrune_PruneFirst_False_LegacyOrder verifies the opt-out path:
// --prune-first=false preserves the v0.27.1 ordering (rotate first, then
// post-rotation prune only). The test asserts that with one orphan whose
// `created_at` is NEWER than the rotation timestamp, --prune-first=false
// does NOT delete it (the post-rotation time filter skips it because it's
// not older than the cutoff). Under the new default it would be deleted.
func TestRotateAndPrune_PruneFirst_False_LegacyOrder(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	tmpDir := t.TempDir()
	t.Setenv("WFCTL_STATE_DIR", tmpDir)
	cfgPath := writeMinimalRotationConfig(t, tmpDir)

	origBoot := bootstrapSecrets
	defer func() { bootstrapSecrets = origBoot }()
	bootstrapSecrets = func(_ context.Context, _ secrets.Provider, _ *SecretsConfig, _ map[string]bool, _ ...interfaces.ProviderCredentialRevoker) (map[string]string, []RotationResult, error) {
		return map[string]string{}, []RotationResult{
			{Key: "canonical-name", Source: "digitalocean.spaces", AccessKey: "AK_NEW", CreatedAt: "2026-05-09T11:00:00Z"},
		}, nil
	}

	fakeProv := &quotaCappedFakeProvider{
		outputs: []*interfaces.ResourceOutput{
			{
				Name:       "orphan-future",
				Type:       "infra.spaces_key",
				ProviderID: "AK_ORPHAN_FUTURE",
				Outputs: map[string]any{
					"access_key": "AK_ORPHAN_FUTURE",
					"created_at": "2027-01-01T00:00:00Z",
					"name":       "orphan-future",
				},
			},
		},
	}

	var out bytes.Buffer
	code := runInfraRotateAndPrune([]string{
		"--type", "infra.spaces_key",
		"--name", "canonical-name",
		"--config", cfgPath,
		"--confirm", "--non-interactive",
		"--prune-first=false",
	}, fakeProv, &out)
	if code != 0 {
		t.Fatalf("rotate-and-prune (--prune-first=false) failed: code=%d, out=%s", code, out.String())
	}

	// AK_ORPHAN_FUTURE has created_at > rotation timestamp, so the post-
	// rotation time filter skips it. Under legacy ordering it survives.
	if rotateAndPruneContains(fakeProv.deleted, "AK_ORPHAN_FUTURE") {
		t.Errorf("AK_ORPHAN_FUTURE should NOT be deleted under --prune-first=false (newer than cutoff); deleted = %v", fakeProv.deleted)
	}

	// Output should NOT mention Step 0 / pre-rotation when the flag is off.
	got := out.String()
	if strings.Contains(got, "Step 0") {
		t.Errorf("output should NOT narrate Step 0 when --prune-first=false; got: %s", got)
	}
	if strings.Contains(got, "defensive sweep") {
		t.Errorf("output should NOT label Step 2 as defensive sweep when --prune-first=false; got: %s", got)
	}
}

// TestRotateAndPrune_PruneFirst_PreservesCanonicalName verifies the
// canonical-name protection in the pre-rotation prune step: the resource
// whose Name == --name MUST be skipped during pre-prune, even when its
// access_key/created_at otherwise look like an orphan. This protects the
// active credential during the brief window between Step 0 and Step 1
// (rotation) — deleting it pre-rotation would leave the cloud account
// without an active credential while the rotate step's mint-new call
// runs.
//
// The preserve-names regex is also asserted: a resource matching the
// regex must be preserved even if its name != canonical.
func TestRotateAndPrune_PruneFirst_PreservesCanonicalName(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	tmpDir := t.TempDir()
	t.Setenv("WFCTL_STATE_DIR", tmpDir)
	cfgPath := writeMinimalRotationConfig(t, tmpDir)

	origBoot := bootstrapSecrets
	defer func() { bootstrapSecrets = origBoot }()
	bootstrapSecrets = func(_ context.Context, _ secrets.Provider, _ *SecretsConfig, _ map[string]bool, _ ...interfaces.ProviderCredentialRevoker) (map[string]string, []RotationResult, error) {
		return map[string]string{}, []RotationResult{
			{Key: "canonical-name", Source: "digitalocean.spaces", AccessKey: "AK_NEW", CreatedAt: "2026-05-09T11:00:00Z"},
		}, nil
	}

	fakeProv := &quotaCappedFakeProvider{
		outputs: []*interfaces.ResourceOutput{
			{
				// Canonical name — pre-prune must skip this even though
				// its created_at is old. Post-prune may or may not delete
				// it depending on the time filter; this test asserts the
				// PRE-prune behavior.
				Name:       "canonical-name",
				Type:       "infra.spaces_key",
				ProviderID: "AK_CANONICAL",
				Outputs: map[string]any{
					"access_key": "AK_CANONICAL",
					"created_at": "2026-05-01T00:00:00Z",
					"name":       "canonical-name",
				},
			},
			{
				// Matches preserve regex — pre-prune must skip.
				Name:       "preserved-fixture",
				Type:       "infra.spaces_key",
				ProviderID: "AK_PRESERVED",
				Outputs: map[string]any{
					"access_key": "AK_PRESERVED",
					"created_at": "2026-04-01T00:00:00Z",
					"name":       "preserved-fixture",
				},
			},
			{
				// Plain orphan — pre-prune must delete.
				Name:       "orphan-x",
				Type:       "infra.spaces_key",
				ProviderID: "AK_ORPHAN_X",
				Outputs: map[string]any{
					"access_key": "AK_ORPHAN_X",
					"created_at": "2026-04-01T00:00:00Z",
					"name":       "orphan-x",
				},
			},
		},
	}

	// Snapshot deletes seen during rotate so we can assert AK_CANONICAL
	// was not deleted in the PRE step (the post step will delete it).
	// We do this by recording the length of `deleted` before invoking
	// runInfraRotateAndPrune and after pre-prune via a stub. The
	// quotaCappedFakeProvider's DeleteResource appends in order; pre-
	// prune deletes happen FIRST, then rotation, then post-prune. So
	// the first N entries in fakeProv.deleted are the pre-prune results.
	// We assert AK_CANONICAL is not in the prefix where N = number of
	// entries that ALSO appear in the orphan set.

	var out bytes.Buffer
	code := runInfraRotateAndPrune([]string{
		"--type", "infra.spaces_key",
		"--name", "canonical-name",
		"--config", cfgPath,
		"--preserve-names", "^preserved-",
		"--confirm", "--non-interactive",
	}, fakeProv, &out)
	if code != 0 {
		t.Fatalf("rotate-and-prune failed: code=%d, out=%s", code, out.String())
	}

	// Orphan was deleted (some pass — pre or post).
	if !rotateAndPruneContains(fakeProv.deleted, "AK_ORPHAN_X") {
		t.Errorf("orphan AK_ORPHAN_X must be deleted; deleted = %v", fakeProv.deleted)
	}
	// Preserved fixture must NEVER be deleted (regex match).
	if rotateAndPruneContains(fakeProv.deleted, "AK_PRESERVED") {
		t.Errorf("AK_PRESERVED must be preserved (matches --preserve-names); deleted = %v", fakeProv.deleted)
	}

	// Canonical's OLD access_key may be deleted by the POST-prune step
	// (it's older than AK_NEW), but never by PRE-prune. We assert by
	// checking the output narrates Step 0's count: with the canonical
	// + preserved fixture skipped, only the orphan should be in the
	// pre-rotation dry-run.
	got := out.String()
	if !strings.Contains(got, "Pre-rotation dry-run: 1 orphan") {
		t.Errorf("pre-rotation dry-run must list exactly 1 orphan (canonical + preserved skipped); got: %s", got)
	}
}
