package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestBootstrapSecrets_ForceRotate_AppendsRotationResult_NameKeyMismatch
// pins the contract that surfaced as a real-world bug on 2026-05-09 in
// core-dump rotate-spaces-key staging dispatch run 25616807427:
//
//	Step 1: rotating credential "coredump-deploy-key" (type infra.spaces_key)...
//	  secret "SPACES_access_key": created
//	WFCTL_NEW_KEY_CREATED_AT=2026-05-10T01:41:58Z
//	[external-plugins] shutting down plugin "digitalocean"
//	rotate-and-prune: expected 1 rotation result, got 0
//	error: rotate-and-prune exited with code 1
//
// Root cause: `wfctl infra rotate-and-prune --name <RESOURCE_NAME>` builds
// `forceRotate := map[string]bool{name: true}` keyed by the cloud-side
// resource name (per ADR 0023 + docs/runbooks/spaces-key-prune.md), but
// `bootstrapSecrets` checks `forceRotate[gen.Key]` keyed by the canonical
// generator key. Mismatch → force-rotate code path silently bypassed →
// rotations slice never appended to → "expected 1 rotation result, got 0"
// AFTER the cloud and GH Secrets side effects already committed.
//
// This test exercises the full runInfraRotateAndPrune path with a config
// where `key != name` — the exact shape of core-dump's infra.yaml on the
// failing run. Without the fix, runInfraRotateAndPrune returns code=1 with
// "expected 1 rotation result, got 0" even though bootstrapSecrets minted
// the new credential. With the fix, the CLI translates --name→gen.Key
// (or bootstrapSecrets accepts both keying forms) and the rotation result
// surfaces correctly.
func TestRotateAndPrune_KeyNameMismatch_ReturnsRotationResult(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	tmpDir := t.TempDir()
	t.Setenv("WFCTL_STATE_DIR", tmpDir)

	// Write a config matching core-dump's shape: secrets.generate[].key=SPACES,
	// secrets.generate[].name=coredump-deploy-key. The CLI takes
	// --name coredump-deploy-key (cloud-side resource name).
	cfgPath := filepath.Join(tmpDir, "infra.yaml")
	body := `secrets:
  provider: env
  config:
    prefix: WFCTL_TEST_
  generate:
    - key: SPACES
      type: provider_credential
      source: digitalocean.spaces
      name: coredump-deploy-key
`
	if err := os.WriteFile(cfgPath, []byte(body), 0600); err != nil {
		t.Fatalf("write fixture infra.yaml: %v", err)
	}

	// Stub the underlying generator so we don't reach DO. Returns a JSON
	// blob shaped like generateDOSpacesKey's real output. The stub also
	// proves the rotation HAPPENED (it was called) so any "got 0" failure
	// is purely the keying-mismatch bug, not a missing call.
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		return `{"access_key":"AK_NEW_FROM_STUB","secret_key":"SK_NEW_FROM_STUB","created_at":"2026-05-10T01:41:58Z"}`, nil
	})

	// Pre-populate the env-provider's backing values so the force-rotate
	// path (which reads OLD access_key for revocation) finds a value to
	// "revoke" without being a hard requirement. Env provider reads from
	// process env via prefix WFCTL_TEST_<KEY>.
	t.Setenv("WFCTL_TEST_SPACES_access_key", "AK_OLD")
	t.Setenv("WFCTL_TEST_SPACES_secret_key", "SK_OLD")

	// fakeProviderEnumerableDriver returns the OLD canonical key as an
	// enumerable resource so the prune step has something to look at and
	// runs without the EnumerateAll-empty escape.
	fakeProv := &fakeProviderEnumerableDriver{
		outputs: []*interfaces.ResourceOutput{
			{
				Name:       "coredump-deploy-key",
				Type:       "infra.spaces_key",
				ProviderID: "AK_OLD",
				Outputs: map[string]any{
					"access_key": "AK_OLD",
					"created_at": "2026-05-01T00:00:00Z",
					"name":       "coredump-deploy-key",
				},
			},
			{
				Name:       "coredump-deploy-key",
				Type:       "infra.spaces_key",
				ProviderID: "AK_NEW_FROM_STUB",
				Outputs: map[string]any{
					"access_key": "AK_NEW_FROM_STUB",
					"created_at": "2026-05-10T01:41:58Z",
					"name":       "coredump-deploy-key",
				},
			},
		},
	}

	var out bytes.Buffer
	code := runInfraRotateAndPrune([]string{
		"--type", "infra.spaces_key",
		"--name", "coredump-deploy-key",
		"--config", cfgPath,
		"--confirm", "--non-interactive",
		// --prune-first=false to keep the test focused on the rotate step's
		// rotation-result contract; pre-prune is exercised by other tests.
		"--prune-first=false",
	}, fakeProv, &out)

	if code != 0 {
		t.Fatalf("rotate-and-prune failed: code=%d, out=%q\n\nThis is the regression: the rotation succeeded (stub generator was called and minted AK_NEW_FROM_STUB) but bootstrapSecrets returned an empty rotations slice because forceRotate was keyed by --name (\"coredump-deploy-key\") while bootstrapSecrets checks forceRotate[gen.Key] (\"SPACES\"). The CLI must translate --name→gen.Key.", code, out.String())
	}

	// Sanity: output should reflect rotation success (not skipped/created path).
	got := out.String()
	if !strings.Contains(got, "AK_NEW_FROM_STUB") {
		t.Errorf("output should report new access_key from rotation; got: %s", got)
	}
}

// TestBootstrapSecrets_ForceRotate_NameOnlyMatch_StillRotatesAndAppends
// is the unit-level lock for the keying contract: when force-rotate
// is keyed by secrets.generate[].Key for a successful provider_credential
// force-rotate that hits an empty store, rotation must run and a
// RotationResult must be appended. Without this, the rotate-and-prune
// integration is fragile to internal CLI changes.
//
// Strict contract enforced by this PR:
//   - forceRotate MUST be keyed by secrets.generate[].Key (canonical key).
//   - bootstrapSecrets ignores entries that don't match a known generator
//     Key (it does NOT accept generator .Name as a fallback key).
//   - Name→Key translation is the CLI layer's responsibility — see
//     buildRotateAndPruneForceRotateSet in infra_rotate_and_prune.go,
//     which also enforces single-match + provider_credential constraints
//     so this contract is unviolable from the operator side.
//
// Invariant pinned by this test: a successful force-rotate where Set
// committed MUST produce a RotationResult (not the false-negative the
// staging run hit on 2026-05-09).
func TestBootstrapSecrets_ForceRotate_AppendsRotationResultEvenWhenStoreEmpty(t *testing.T) {
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		return `{"access_key":"AK_NEW","secret_key":"SK_NEW","created_at":"2026-05-10T01:41:58Z"}`, nil
	})

	// Empty store — same shape as the staging-dispatch case where the OLD
	// canonical sub-keys had been wiped before the dispatch (or never
	// existed under the canonical-key shape because the prior config used
	// the misconfigured suffixed-key shape). The bug-side surface is:
	// even with an empty store, forceRotate[gen.Key]=true MUST drive the
	// rotation appendpath, NOT the "doesn't exist → mint fresh" path that
	// bypasses the rotations slice append.
	p := newTrackingProvider(map[string]string{})
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces", Name: "coredump-deploy-key"},
		},
	}
	forceRotate := map[string]bool{"SPACES": true}

	_, rotations, err := bootstrapSecrets(context.Background(), p, cfg, forceRotate)
	if err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}

	// The contract: force-rotate that successfully Set sub-keys MUST
	// surface a RotationResult. Side effects committed without a
	// RotationResult is the false-negative the staging run hit.
	if len(rotations) != 1 {
		t.Fatalf("expected len(rotations)==1 after successful force-rotate; got %d. Side effects committed (Set calls=%v) but rotations slice is empty — same false-negative as staging run 25616807427.", len(rotations), p.setCalls)
	}
	if rotations[0].Key != "SPACES" {
		t.Errorf("rotations[0].Key = %q, want \"SPACES\"", rotations[0].Key)
	}
	if rotations[0].AccessKey != "AK_NEW" {
		t.Errorf("rotations[0].AccessKey = %q, want \"AK_NEW\"", rotations[0].AccessKey)
	}
	if rotations[0].CreatedAt != "2026-05-10T01:41:58Z" {
		t.Errorf("rotations[0].CreatedAt = %q, want \"2026-05-10T01:41:58Z\"", rotations[0].CreatedAt)
	}
	// And the side effects must have happened (proves the rotation ran).
	if !containsSlice(p.setCalls, "SPACES_access_key") {
		t.Errorf("Set(SPACES_access_key) must be called; setCalls=%v", p.setCalls)
	}
	if !containsSlice(p.setCalls, "SPACES_secret_key") {
		t.Errorf("Set(SPACES_secret_key) must be called; setCalls=%v", p.setCalls)
	}
}
