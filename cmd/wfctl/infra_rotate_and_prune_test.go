package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
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
	enumErr   error
	deleteErr error
}

func (f *fakeProviderEnumerableDriver) EnumerateAll(_ context.Context, _ string) ([]*interfaces.ResourceOutput, error) {
	if f.enumErr != nil {
		return nil, f.enumErr
	}
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

func runStubbedCredentialPreparation(ctx context.Context) error {
	options := credentialIssuerOptionsFromContext(ctx)
	if options.BeforeIssue == nil {
		return nil
	}
	return options.BeforeIssue(ctx, false)
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
	bootstrapSecrets = func(ctx context.Context, _ secrets.Provider, _ *SecretsConfig, _ map[string]bool, _ ...interfaces.ProviderCredentialRevoker) (map[string]string, []RotationResult, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("rotate-and-prune provider context is not bounded")
		}
		if err := runStubbedCredentialPreparation(ctx); err != nil {
			return nil, nil, err
		}
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

func TestInfraRotateAndPruneThreadsTypedCredentialIssuerOptions(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	stateRoot := t.TempDir()
	t.Setenv("WFCTL_STATE_DIR", stateRoot)
	cfgPath := writeMinimalRotationConfig(t, stateRoot)
	pluginDir := filepath.Join(stateRoot, "plugins")

	originalResolver := resolveCredentialRevokerForCapabilitiesFn
	var resolvedPluginDir string
	resolveCredentialRevokerForCapabilitiesFn = func(_ context.Context, _, gotPluginDir string, _ *SecretsConfig, _ map[string]bool) (interfaces.ProviderCredentialRevoker, interface{ Close() error }, error) {
		resolvedPluginDir = gotPluginDir
		return nil, nil, nil
	}
	t.Cleanup(func() { resolveCredentialRevokerForCapabilitiesFn = originalResolver })

	originalBootstrap := bootstrapSecrets
	var issuerOptions credentialIssuerOptions
	bootstrapSecrets = func(ctx context.Context, _ secrets.Provider, _ *SecretsConfig, _ map[string]bool, _ ...interfaces.ProviderCredentialRevoker) (map[string]string, []RotationResult, error) {
		issuerOptions = credentialIssuerOptionsFromContext(ctx)
		return map[string]string{}, []RotationResult{{
			Key: "test-key", Source: "digitalocean.spaces", AccessKey: "AK_NEW", CreatedAt: "2026-05-08T11:00:00Z",
		}}, nil
	}
	t.Cleanup(func() { bootstrapSecrets = originalBootstrap })

	provider := &fakeProviderEnumerableDriver{outputs: []*interfaces.ResourceOutput{{
		Name: "test-key", Type: "infra.spaces_key", ProviderID: "AK_NEW",
		Outputs: map[string]any{"access_key": "AK_NEW", "name": "test-key"},
	}}}
	var output bytes.Buffer
	code := runInfraRotateAndPrune([]string{
		"--type", "infra.spaces_key", "--name", "test-key", "--config", cfgPath,
		"--plugin-dir", pluginDir, "--ack-single-writer", "--confirm", "--non-interactive", "--prune-first=false",
	}, provider, &output)
	if code != 0 {
		t.Fatalf("code=%d output=%s", code, output.String())
	}
	if resolvedPluginDir != pluginDir {
		t.Fatalf("revoker capability plugin directory=%q, want %q", resolvedPluginDir, pluginDir)
	}
	if !issuerOptions.Enabled || issuerOptions.PluginDir != pluginDir || !issuerOptions.AckSingleWriter || !issuerOptions.NonInteractive {
		t.Fatalf("issuer options=%+v", issuerOptions)
	}
	if issuerOptions.StateDir == "" || issuerOptions.LockDir == "" {
		t.Fatalf("issuer durable paths missing: %+v", issuerOptions)
	}
	wantPreparationKey := "infra.spaces_key\x00test-key\x00\x00false"
	if issuerOptions.PreparationKey != wantPreparationKey {
		t.Fatalf("issuer preparation key=%q, want %q", issuerOptions.PreparationKey, wantPreparationKey)
	}
}

func TestInfraRotateAndPrunePersistsCommittedRotationWhenFinalSetCancels(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	stateRoot := t.TempDir()
	t.Setenv("WFCTL_STATE_DIR", stateRoot)
	configPath := filepath.Join(stateRoot, "infra.yaml")
	configData := []byte(`secrets:
  provider: env
  generate:
    - key: EXAMPLE
      type: provider_credential
      source: example.source
      name: deploy-key
`)
	if err := os.WriteFile(configPath, configData, 0o600); err != nil {
		t.Fatal(err)
	}
	pluginRoot := filepath.Join(stateRoot, "plugins")
	pluginDir := filepath.Join(pluginRoot, "workflow-plugin-example")
	if err := os.MkdirAll(pluginDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"workflow-plugin-example","version":"1.2.3","author":"Workflow tests","description":"credential fixture","credentialSources":[{"source":"example.source","concurrencyMode":"provider_idempotent","outputs":[{"key":"id"},{"key":"secret"}],"identifierKey":"id"}]}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	issuerClient := &recordingCredentialIssuerClient{issueResponse: confirmedCredentialIssueResponse()}
	withCredentialIssuerResolver(t, issuerClient, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
	})
	ctx, cancel := context.WithCancel(context.Background())
	secretProvider := &cancelAfterFirstSecretMutationProvider{cancel: cancel, cancelAfter: 2}
	realBootstrap := bootstrapSecrets
	bootstrapSecrets = func(ctx context.Context, _ secrets.Provider, cfg *SecretsConfig, forceRotate map[string]bool, revoker ...interfaces.ProviderCredentialRevoker) (map[string]string, []RotationResult, error) {
		return realBootstrap(ctx, secretProvider, cfg, forceRotate, revoker...)
	}
	t.Cleanup(func() { bootstrapSecrets = realBootstrap })

	pruneProvider := &fakeProviderWithDelete{}
	var output bytes.Buffer
	code := runInfraRotateAndPruneWithContext(ctx, []string{
		"--type", "infra.example_credential", "--name", "deploy-key", "--config", configPath,
		"--plugin-dir", pluginRoot, "--confirm", "--non-interactive", "--prune-first=false",
	}, pruneProvider, &output)
	if code == 0 || !strings.Contains(output.String(), context.Canceled.Error()) {
		t.Fatalf("code=%d output=%s", code, output.String())
	}
	if got := strings.Join(secretProvider.setCalls, ","); got != "EXAMPLE_id,EXAMPLE_secret" {
		t.Fatalf("secret Set calls=%q", got)
	}
	if len(secretProvider.deleteCalls) != 0 || issuerClient.deleteCalls != 0 || len(pruneProvider.deleted) != 0 || pruneProvider.lastType != "" {
		t.Fatalf("secret deletes=%v issuer deletes=%d prune deletes=%v inventory type=%q", secretProvider.deleteCalls, issuerClient.deleteCalls, pruneProvider.deleted, pruneProvider.lastType)
	}
	recovery, err := readRecoveryFile()
	if err != nil {
		t.Fatalf("committed rotation recovery missing: %v", err)
	}
	if recovery.Type != "infra.example_credential" || recovery.AccessKey != "credential-123" {
		t.Fatalf("recovery=%+v", recovery)
	}
}

func TestInfraRotateAndPrunePersistsCommittedRotationOnBookkeepingFailure(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	stateRoot := t.TempDir()
	t.Setenv("WFCTL_STATE_DIR", stateRoot)
	configPath := writeMinimalRotationConfig(t, stateRoot)

	originalResolver := resolveCredentialRevokerForCapabilitiesFn
	resolveCredentialRevokerForCapabilitiesFn = func(context.Context, string, string, *SecretsConfig, map[string]bool) (interfaces.ProviderCredentialRevoker, interface{ Close() error }, error) {
		return nil, nil, nil
	}
	t.Cleanup(func() { resolveCredentialRevokerForCapabilitiesFn = originalResolver })
	originalBootstrap := bootstrapSecrets
	bootstrapSecrets = func(context.Context, secrets.Provider, *SecretsConfig, map[string]bool, ...interfaces.ProviderCredentialRevoker) (map[string]string, []RotationResult, error) {
		return nil, []RotationResult{{
			Key: "test-key", Source: "example.source", AccessKey: "committed-id", CreatedAt: "2026-05-08T11:00:00Z",
		}}, errors.New("simulated post-commit bookkeeping failure")
	}
	t.Cleanup(func() { bootstrapSecrets = originalBootstrap })

	provider := &fakeProviderWithDelete{}
	var output bytes.Buffer
	code := runInfraRotateAndPrune([]string{
		"--type", "infra.example_credential", "--name", "test-key", "--config", configPath,
		"--confirm", "--non-interactive", "--prune-first=false",
	}, provider, &output)
	if code == 0 || !strings.Contains(output.String(), "post-commit bookkeeping failure") || provider.lastType != "" || len(provider.deleted) != 0 {
		t.Fatalf("code=%d inventory type=%q deletes=%v output=%s", code, provider.lastType, provider.deleted, output.String())
	}
	recovery, err := readRecoveryFile()
	if err != nil || recovery.AccessKey != "committed-id" {
		t.Fatalf("recovery=%+v error=%v", recovery, err)
	}
}

func TestInfraRotateAndPruneSerializesGlobalRecoveryStateBeforeMutation(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	stateRoot := t.TempDir()
	t.Setenv("WFCTL_STATE_DIR", stateRoot)
	configPath := writeMinimalRotationConfig(t, stateRoot)
	lockDir, err := credentialOperationLockDir()
	if err != nil {
		t.Fatal(err)
	}
	release, err := acquireCredentialOperationLock(lockDir, "wfctl.rotate-and-prune-recovery", "global")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	originalBootstrap := bootstrapSecrets
	bootstrapCalls := 0
	bootstrapSecrets = func(context.Context, secrets.Provider, *SecretsConfig, map[string]bool, ...interfaces.ProviderCredentialRevoker) (map[string]string, []RotationResult, error) {
		bootstrapCalls++
		return nil, nil, nil
	}
	t.Cleanup(func() { bootstrapSecrets = originalBootstrap })
	var output bytes.Buffer
	code := runInfraRotateAndPrune([]string{
		"--type", "infra.example_credential", "--name", "test-key", "--config", configPath,
		"--confirm", "--non-interactive", "--prune-first=false",
	}, &fakeProviderEnumerableDriver{}, &output)
	if code == 0 || bootstrapCalls != 0 || !strings.Contains(output.String(), "locked") {
		t.Fatalf("code=%d bootstrap calls=%d output=%s", code, bootstrapCalls, output.String())
	}
}

func TestInfraRotateAndPruneRefusesToOverwriteRetainedRecoveryRecord(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	stateRoot := t.TempDir()
	t.Setenv("WFCTL_STATE_DIR", stateRoot)
	configPath := writeMinimalRotationConfig(t, stateRoot)
	if err := writeRecoveryRecord(recoveryRecord{
		Type: "infra.spaces_key", Name: "test-key", AccessKey: "retained-id", CreatedAt: "2026-05-08T11:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	recoveryPath := filepath.Join(stateRoot, "last-rotation.json")
	wantBytes, err := os.ReadFile(recoveryPath)
	if err != nil {
		t.Fatal(err)
	}

	originalBootstrap := bootstrapSecrets
	bootstrapCalls := 0
	bootstrapSecrets = func(context.Context, secrets.Provider, *SecretsConfig, map[string]bool, ...interfaces.ProviderCredentialRevoker) (map[string]string, []RotationResult, error) {
		bootstrapCalls++
		return nil, []RotationResult{{
			Key: "test-key", Source: "digitalocean.spaces", AccessKey: "replacement-id", CreatedAt: "2026-05-09T11:00:00Z",
		}}, nil
	}
	t.Cleanup(func() { bootstrapSecrets = originalBootstrap })

	var output bytes.Buffer
	code := runInfraRotateAndPrune([]string{
		"--type", "infra.spaces_key", "--name", "test-key", "--config", configPath,
		"--confirm", "--non-interactive", "--prune-first=false",
	}, &fakeProviderEnumerableDriver{}, &output)
	if code == 0 || bootstrapCalls != 0 || !strings.Contains(output.String(), "recovery") {
		t.Fatalf("code=%d bootstrap calls=%d output=%s", code, bootstrapCalls, output.String())
	}
	gotBytes, err := os.ReadFile(recoveryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotBytes, wantBytes) {
		t.Fatalf("retained recovery record changed:\nwant=%s\n got=%s", wantBytes, gotBytes)
	}
}

func TestInfraRotateAndPruneDefersPrePruneUntilBootstrapSafetyGates(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	stateRoot := t.TempDir()
	t.Setenv("WFCTL_STATE_DIR", stateRoot)
	cfgPath := writeMinimalRotationConfig(t, stateRoot)

	originalBootstrap := bootstrapSecrets
	bootstrapSecrets = func(context.Context, secrets.Provider, *SecretsConfig, map[string]bool, ...interfaces.ProviderCredentialRevoker) (map[string]string, []RotationResult, error) {
		return nil, nil, errors.New("simulated issuer safety gate failure")
	}
	t.Cleanup(func() { bootstrapSecrets = originalBootstrap })

	provider := &fakeProviderEnumerableDriver{outputs: []*interfaces.ResourceOutput{{
		Name: "orphan-key", Type: "infra.spaces_key", ProviderID: "AK_ORPHAN",
		Outputs: map[string]any{"created_at": "2026-05-01T00:00:00Z", "name": "orphan-key"},
	}}}
	var output bytes.Buffer
	code := runInfraRotateAndPrune([]string{
		"--type", "infra.spaces_key", "--name", "test-key", "--config", cfgPath,
		"--confirm", "--non-interactive",
	}, provider, &output)
	if code == 0 || !strings.Contains(output.String(), "simulated issuer safety gate failure") {
		t.Fatalf("code=%d output=%s", code, output.String())
	}
	if len(provider.deleted) != 0 {
		t.Fatalf("pre-prune mutated resources before issuer safety gates: %v", provider.deleted)
	}
}

func TestInfraRotateAndPruneRedactsSensitiveCredentialIdentifiers(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	stateRoot := t.TempDir()
	t.Setenv("WFCTL_STATE_DIR", stateRoot)
	cfgPath := writeMinimalRotationConfig(t, stateRoot)

	originalBootstrap := bootstrapSecrets
	bootstrapSecrets = func(context.Context, secrets.Provider, *SecretsConfig, map[string]bool, ...interfaces.ProviderCredentialRevoker) (map[string]string, []RotationResult, error) {
		return map[string]string{}, []RotationResult{{
			Key: "test-key", Source: "example.credential", AccessKey: "SENSITIVE_NEW", CreatedAt: "2026-05-08T11:00:00Z", IdentifierSensitive: true,
		}}, nil
	}
	t.Cleanup(func() { bootstrapSecrets = originalBootstrap })

	provider := &fakeProviderEnumerableDriver{outputs: []*interfaces.ResourceOutput{{
		Name: "old-key", Type: "infra.example_credential", ProviderID: "SENSITIVE_OLD",
		Outputs: map[string]any{"created_at": "2026-05-01T00:00:00Z", "name": "old-key"},
	}}}
	var output bytes.Buffer
	code := runInfraRotateAndPrune([]string{
		"--type", "infra.example_credential", "--name", "test-key", "--config", cfgPath,
		"--confirm", "--non-interactive", "--prune-first=false",
	}, provider, &output)
	if code != 0 {
		t.Fatalf("code=%d output=%s", code, output.String())
	}
	if strings.Contains(output.String(), "SENSITIVE_NEW") || strings.Contains(output.String(), "SENSITIVE_OLD") {
		t.Fatalf("sensitive identifier leaked in output: %s", output.String())
	}
	if !strings.Contains(output.String(), "[sensitive identifier redacted]") {
		t.Fatalf("redaction marker missing from output: %s", output.String())
	}
}

func TestPreRotationPruneRedactsSensitiveCredentialIdentifiers(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	provider := &fakeProviderEnumerableDriver{outputs: []*interfaces.ResourceOutput{{
		Name: "orphan-key", Type: "infra.example_credential", ProviderID: "SENSITIVE_ORPHAN",
		Outputs: map[string]any{"created_at": "2026-05-01T00:00:00Z", "name": "orphan-key"},
	}}}
	var output bytes.Buffer
	code := runPreRotationPrune(context.Background(), provider, "infra.example_credential", "canonical-key", "", true, true, &output)
	if code != 0 {
		t.Fatalf("code=%d output=%s", code, output.String())
	}
	if strings.Contains(output.String(), "SENSITIVE_ORPHAN") || !strings.Contains(output.String(), "[sensitive identifier redacted]") {
		t.Fatalf("sensitive pre-prune identifier output=%s", output.String())
	}
}

func TestPreRotationPruneSuppressesProviderErrorText(t *testing.T) {
	t.Setenv("WFCTL_CONFIRM_PRUNE", "1")
	for _, provider := range []*fakeProviderEnumerableDriver{
		{enumErr: errors.New("SENSITIVE_ENUMERATE")},
		{outputs: []*interfaces.ResourceOutput{{Name: "orphan", Type: "infra.example_credential", ProviderID: "orphan-id"}}, deleteErr: errors.New("SENSITIVE_DELETE")},
	} {
		var output bytes.Buffer
		if code := runPreRotationPrune(context.Background(), provider, "infra.example_credential", "canonical", "", true, true, &output); code == 0 {
			t.Fatalf("expected provider failure: %s", output.String())
		}
		if strings.Contains(output.String(), "SENSITIVE_") || !strings.Contains(output.String(), "provider error text suppressed") {
			t.Fatalf("provider error output=%s", output.String())
		}
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
	bootstrapSecrets = func(ctx context.Context, _ secrets.Provider, _ *SecretsConfig, _ map[string]bool, _ ...interfaces.ProviderCredentialRevoker) (map[string]string, []RotationResult, error) {
		if err := runStubbedCredentialPreparation(ctx); err != nil {
			return nil, nil, err
		}
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

func TestWriteRecoveryRecordHardensPreexistingStatePaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}
	stateDir := t.TempDir()
	t.Setenv("WFCTL_STATE_DIR", stateDir)
	if err := os.Chmod(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "unrelated")
	if err := os.WriteFile(target, []byte("unchanged"), 0o644); err != nil {
		t.Fatal(err)
	}
	recoveryPath := filepath.Join(stateDir, "last-rotation.json")
	if err := os.Symlink(target, recoveryPath); err != nil {
		t.Fatal(err)
	}
	if err := writeRecoveryRecord(recoveryRecord{
		Type: "infra.example_credential", Name: "deploy-key", AccessKey: "credential-1", CreatedAt: "2026-05-08T11:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(target); err != nil || string(content) != "unchanged" {
		t.Fatalf("recovery write followed symlink: content=%q error=%v", content, err)
	}
	info, err := os.Lstat(recoveryPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
		t.Fatalf("recovery file mode=%v", info.Mode())
	}
	dirInfo, err := os.Stat(stateDir)
	if err != nil || dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("state directory mode=%v error=%v", dirInfo.Mode(), err)
	}
	symlinkTargetDir := t.TempDir()
	symlinkStateDir := filepath.Join(t.TempDir(), "state-link")
	if err := os.Symlink(symlinkTargetDir, symlinkStateDir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WFCTL_STATE_DIR", symlinkStateDir)
	if err := writeRecoveryRecord(recoveryRecord{Type: "infra.example_credential", AccessKey: "credential-2"}); err == nil {
		t.Fatal("writer accepted symlinked recovery-state directory")
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
	bootstrapSecrets = func(ctx context.Context, _ secrets.Provider, _ *SecretsConfig, _ map[string]bool, _ ...interfaces.ProviderCredentialRevoker) (map[string]string, []RotationResult, error) {
		if err := runStubbedCredentialPreparation(ctx); err != nil {
			return nil, nil, err
		}
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
	bootstrapSecrets = func(ctx context.Context, _ secrets.Provider, _ *SecretsConfig, _ map[string]bool, _ ...interfaces.ProviderCredentialRevoker) (map[string]string, []RotationResult, error) {
		if err := runStubbedCredentialPreparation(ctx); err != nil {
			return nil, nil, err
		}
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
	bootstrapSecrets = func(ctx context.Context, _ secrets.Provider, _ *SecretsConfig, _ map[string]bool, _ ...interfaces.ProviderCredentialRevoker) (map[string]string, []RotationResult, error) {
		if err := runStubbedCredentialPreparation(ctx); err != nil {
			return nil, nil, err
		}
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
