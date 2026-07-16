package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/secrets"
)

// writeOnlyProvider simulates a GitHub-style provider where Get is not
// supported but List returns known names.
type writeOnlyProvider struct {
	existing  []string
	stored    map[string]string
	getCalls  int
	listCalls int
	listOK    bool
	name      string
}

type cancelAfterFirstSecretMutationProvider struct {
	cancel      context.CancelFunc
	cancelAfter int
	setCalls    []string
	deleteCalls []string
}

func (*cancelAfterFirstSecretMutationProvider) Name() string { return "cancel-after-first" }
func (*cancelAfterFirstSecretMutationProvider) Get(context.Context, string) (string, error) {
	return "", secrets.ErrNotFound
}
func (p *cancelAfterFirstSecretMutationProvider) Set(_ context.Context, key, _ string) error {
	p.setCalls = append(p.setCalls, key)
	cancelAfter := p.cancelAfter
	if cancelAfter == 0 {
		cancelAfter = 1
	}
	if len(p.setCalls) == cancelAfter && p.cancel != nil {
		p.cancel()
	}
	return nil
}
func (p *cancelAfterFirstSecretMutationProvider) Delete(_ context.Context, key string) error {
	p.deleteCalls = append(p.deleteCalls, key)
	return nil
}
func (*cancelAfterFirstSecretMutationProvider) List(context.Context) ([]string, error) {
	return nil, nil
}

func (p *writeOnlyProvider) Name() string {
	if p.name != "" {
		return p.name
	}
	return "write-only-fake"
}

func (p *writeOnlyProvider) Get(_ context.Context, _ string) (string, error) {
	p.getCalls++
	return "", secrets.ErrUnsupported
}

func (p *writeOnlyProvider) Set(_ context.Context, key, value string) error {
	if p.stored == nil {
		p.stored = map[string]string{}
	}
	p.stored[key] = value
	return nil
}

func (p *writeOnlyProvider) Delete(_ context.Context, _ string) error {
	return nil
}

func (p *writeOnlyProvider) List(_ context.Context) ([]string, error) {
	p.listCalls++
	if !p.listOK {
		return nil, secrets.ErrUnsupported
	}
	return append([]string(nil), p.existing...), nil
}

// withStubGenerator swaps the package-level generateSecret for the duration
// of the test, so provider_credential paths don't reach out to cloud APIs.
func withStubGenerator(t *testing.T, fn func(ctx context.Context, genType string, cfg map[string]any) (string, error)) {
	t.Helper()
	prev := generateSecret
	generateSecret = fn
	t.Cleanup(func() { generateSecret = prev })
}

func TestBootstrapSecretsStopsBeforeNextGeneratorMutationAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	provider := &cancelAfterFirstSecretMutationProvider{cancel: cancel}
	withStubGenerator(t, func(context.Context, string, map[string]any) (string, error) { return "generated", nil })
	_, _, err := bootstrapSecrets(ctx, provider, &SecretsConfig{Generate: []SecretGen{
		{Key: "FIRST", Type: "random_hex"},
		{Key: "SECOND", Type: "random_hex"},
	}}, nil)
	if !errors.Is(err, context.Canceled) || len(provider.setCalls) != 1 || provider.setCalls[0] != "FIRST" {
		t.Fatalf("error=%v set calls=%v", err, provider.setCalls)
	}
}

func TestBootstrapSecretsLegacyPreparationCancellationStopsBeforeGenerator(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	generatorCalls := 0
	withStubGenerator(t, func(context.Context, string, map[string]any) (string, error) {
		generatorCalls++
		return `{"access_key":"new-id","secret_key":"new-secret"}`, nil
	})
	ctx = withCredentialIssuerOptions(ctx, credentialIssuerOptions{
		BeforeIssue: func(context.Context, bool) error {
			cancel()
			return nil
		},
	})
	provider := &transactionalSecretProvider{stored: map[string]string{
		"SPACES_access_key": "old-id", "SPACES_secret_key": "old-secret",
	}}
	_, _, err := bootstrapSecrets(ctx, provider, &SecretsConfig{Generate: []SecretGen{{
		Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces",
	}}}, map[string]bool{"SPACES": true}, nil)
	if !errors.Is(err, context.Canceled) || generatorCalls != 0 {
		t.Fatalf("error=%v generator calls=%d", err, generatorCalls)
	}
}

func TestBootstrapSecretsStopsBeforeNextTypedOutputAfterCancellation(t *testing.T) {
	pluginDir := t.TempDir()
	installedDir := filepath.Join(pluginDir, "workflow-plugin-example")
	if err := os.MkdirAll(installedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"workflow-plugin-example","version":"1.2.3","author":"Workflow tests","description":"provider capability fixture","credentialSources":[{"source":"example.source","concurrencyMode":"provider_idempotent","outputs":[{"key":"id"},{"key":"secret"}],"identifierKey":"id"}]}`
	if err := os.WriteFile(filepath.Join(installedDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &recordingCredentialIssuerClient{issueResponse: confirmedCredentialIssueResponse()}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
	})
	ctx, cancel := context.WithCancel(context.Background())
	provider := &cancelAfterFirstSecretMutationProvider{cancel: cancel}
	ctx = withCredentialIssuerOptions(ctx, credentialIssuerOptions{
		Enabled: true, PluginDir: pluginDir, StateDir: t.TempDir(), NonInteractive: true,
	})
	_, _, err := bootstrapSecrets(ctx, provider, &SecretsConfig{Generate: []SecretGen{{
		Key: "EXAMPLE", Type: "provider_credential", Source: "example.source", Name: "deploy-key",
	}}}, nil)
	if !errors.Is(err, context.Canceled) || len(provider.setCalls) != 1 || len(provider.deleteCalls) != 0 || client.deleteCalls != 0 {
		t.Fatalf("error=%v set calls=%v delete calls=%v issuer deletes=%d", err, provider.setCalls, provider.deleteCalls, client.deleteCalls)
	}
}

func TestBootstrapSecretsUsesInstalledTypedCredentialIssuer(t *testing.T) {
	pluginDir := t.TempDir()
	installedDir := filepath.Join(pluginDir, "workflow-plugin-example")
	if err := os.MkdirAll(installedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"workflow-plugin-example","version":"1.2.3","author":"Workflow tests","description":"provider capability fixture","credentialSources":[{"source":"example.source","concurrencyMode":"provider_idempotent","outputs":[{"key":"id"},{"key":"secret"}],"identifierKey":"id"}]}`
	if err := os.WriteFile(filepath.Join(installedDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &recordingCredentialIssuerClient{issueResponse: confirmedCredentialIssueResponse()}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
	})
	issuerOptions := credentialIssuerOptions{Enabled: true, PluginDir: pluginDir, StateDir: t.TempDir(), NonInteractive: true}
	withStubGenerator(t, func(context.Context, string, map[string]any) (string, error) {
		t.Fatal("legacy generator called despite installed typed credential source")
		return "", nil
	})

	provider := &writeOnlyProvider{listOK: true}
	ctx := withCredentialIssuerOptions(context.Background(), issuerOptions)
	generated, _, err := bootstrapSecrets(ctx, provider, &SecretsConfig{Generate: []SecretGen{{
		Key: "EXAMPLE", Type: "provider_credential", Source: "example.source", Name: "deploy-key",
	}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if generated["EXAMPLE_id"] != "credential-123" || generated["EXAMPLE_secret"] != "sensitive-value" {
		t.Fatalf("generated=%v", generated)
	}
	state, err := loadCredentialOperationState(issuerOptions.StateDir, "example.source", "deploy-key")
	if err != nil || state.Status != credentialOperationStored {
		t.Fatalf("operation state=%+v error=%v", state, err)
	}
}

func TestBootstrapSecretsPreservesTypedCredentialOutputBytes(t *testing.T) {
	pluginDir := t.TempDir()
	installedDir := filepath.Join(pluginDir, "workflow-plugin-example")
	if err := os.MkdirAll(installedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"workflow-plugin-example","version":"1.2.3","author":"Workflow tests","description":"provider capability fixture","credentialSources":[{"source":"example.source","concurrencyMode":"provider_idempotent","outputs":[{"key":"id"},{"key":"secret"}],"identifierKey":"id"}]}`
	if err := os.WriteFile(filepath.Join(installedDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &recordingCredentialIssuerClient{issueResponse: &pb.CredentialIssueResponse{
		Outputs: []*pb.CredentialOutput{
			{Key: "id", Value: []byte("credential-123"), Sensitive: true},
			{Key: "secret", Value: []byte{0xff, 0x00, 0x7f}, Sensitive: true},
		},
		Identifier:          "credential-123",
		IdentifierSensitive: true,
		ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
	}}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
	})
	provider := &transactionalSecretProvider{}
	ctx := withCredentialIssuerOptions(context.Background(), credentialIssuerOptions{
		Enabled: true, PluginDir: pluginDir, StateDir: t.TempDir(), NonInteractive: true,
	})
	_, _, err := bootstrapSecrets(ctx, provider, &SecretsConfig{Generate: []SecretGen{{
		Key: "EXAMPLE", Type: "provider_credential", Source: "example.source", Name: "deploy-key",
	}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := []byte(provider.stored["EXAMPLE_secret"]); !slices.Equal(got, []byte{0xff, 0x00, 0x7f}) {
		t.Fatalf("stored typed bytes=%x, want ff007f", got)
	}
}

func TestCredentialIdentifierForLogRedactsSensitiveValues(t *testing.T) {
	if got := credentialIdentifierForLog("credential-123", true); strings.Contains(got, "credential-123") {
		t.Fatalf("sensitive identifier leaked: %q", got)
	}
	if got := credentialIdentifierForLog("metadata-id", false); got != "metadata-id" {
		t.Fatalf("non-sensitive identifier=%q", got)
	}
}

func TestBootstrapSecretsTypedIssuerRollsBackUpstreamAndPartialStore(t *testing.T) {
	pluginDir := t.TempDir()
	installedDir := filepath.Join(pluginDir, "workflow-plugin-example")
	if err := os.MkdirAll(installedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"workflow-plugin-example","version":"1.2.3","author":"Workflow tests","description":"provider capability fixture","credentialSources":[{"source":"example.source","concurrencyMode":"provider_idempotent","outputs":[{"key":"id"},{"key":"secret"}],"identifierKey":"id"}]}`
	if err := os.WriteFile(filepath.Join(installedDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &recordingCredentialIssuerClient{
		issueResponse: confirmedCredentialIssueResponse(),
		deleteResponse: &pb.CredentialDeleteResponse{
			Identifier: "credential-123", IdentifierSensitive: true,
			ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
		},
	}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
	})
	issuerOptions := credentialIssuerOptions{Enabled: true, PluginDir: pluginDir, StateDir: t.TempDir(), NonInteractive: true}
	ctx := withCredentialIssuerOptions(context.Background(), issuerOptions)
	provider := &failOnSetProvider{failKey: "EXAMPLE_secret"}
	_, _, err := bootstrapSecrets(ctx, provider, &SecretsConfig{Generate: []SecretGen{{
		Key: "EXAMPLE", Type: "provider_credential", Source: "example.source", Name: "deploy-key",
	}}}, nil)
	if err == nil {
		t.Fatal("want secret-store failure")
	}
	if client.deleteCalls != 1 || client.deleteIdentifier != "credential-123" {
		t.Fatalf("typed rollback calls=%d identifier=%q", client.deleteCalls, client.deleteIdentifier)
	}
	sort.Strings(provider.deleted)
	if got := strings.Join(provider.deleted, ","); got != "EXAMPLE_id,EXAMPLE_secret" {
		t.Fatalf("partial store cleanup=%q", got)
	}
	state, loadErr := loadCredentialOperationState(issuerOptions.StateDir, "example.source", "deploy-key")
	if loadErr != nil || state.Status != credentialOperationRolledBack {
		t.Fatalf("state=%+v error=%v", state, loadErr)
	}
}

func TestBootstrapSecretsTypedIssuerRejectsOpaqueSecretStoreBeforeIssue(t *testing.T) {
	pluginDir := t.TempDir()
	installedDir := filepath.Join(pluginDir, "workflow-plugin-example")
	if err := os.MkdirAll(installedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"workflow-plugin-example","version":"1.2.3","author":"Workflow tests","description":"provider capability fixture","credentialSources":[{"source":"example.source","concurrencyMode":"provider_idempotent","outputs":[{"key":"id"},{"key":"secret"}],"identifierKey":"id"}]}`
	if err := os.WriteFile(filepath.Join(installedDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &recordingCredentialIssuerClient{issueResponse: confirmedCredentialIssueResponse()}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
	})
	provider := &writeOnlyProvider{
		existing: []string{"EXAMPLE_id"},
		listOK:   false,
	}
	ctx := withCredentialIssuerOptions(context.Background(), credentialIssuerOptions{
		Enabled: true, PluginDir: pluginDir, StateDir: t.TempDir(), NonInteractive: true,
	})
	_, _, err := bootstrapSecrets(ctx, provider, &SecretsConfig{Generate: []SecretGen{{
		Key: "EXAMPLE", Type: "provider_credential", Source: "example.source", Name: "deploy-key",
	}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot safely") {
		t.Fatalf("error=%v, want fail-closed opaque-store diagnostic", err)
	}
	if client.issueCalls != 0 || len(provider.stored) != 0 {
		t.Fatalf("opaque store mutated: Issue=%d stored=%v", client.issueCalls, provider.stored)
	}
}

func TestBootstrapSecretsTypedForceRotateRevokesOldCredentialThroughSelectedIssuer(t *testing.T) {
	pluginDir := t.TempDir()
	installedDir := filepath.Join(pluginDir, "workflow-plugin-example")
	if err := os.MkdirAll(installedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"workflow-plugin-example","version":"1.2.3","author":"Workflow tests","description":"provider capability fixture","credentialSources":[{"source":"example.source","concurrencyMode":"provider_idempotent","outputs":[{"key":"id"},{"key":"secret"},{"key":"created_at"}],"identifierKey":"id"}]}`
	if err := os.WriteFile(filepath.Join(installedDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &recordingCredentialIssuerClient{
		issueResponse: confirmedCredentialIssueResponse(),
		deleteResponse: &pb.CredentialDeleteResponse{
			Identifier: "old-credential", IdentifierSensitive: true,
			ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
		},
	}
	client.issueResponse.Outputs = append(client.issueResponse.Outputs, &pb.CredentialOutput{
		Key: "created_at", Value: []byte("provider-owned-non-rfc3339-value"), Sensitive: true,
	})
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}, {Key: "created_at"}}, IdentifierKey: "id",
	})
	issuerOptions := credentialIssuerOptions{Enabled: true, PluginDir: pluginDir, StateDir: t.TempDir(), NonInteractive: true}
	provider := &transactionalSecretProvider{stored: map[string]string{
		"EXAMPLE_id": "old-credential", "EXAMPLE_secret": "old-secret",
	}}
	legacyRevoker := &recordingRevoker{}
	ctx := withCredentialIssuerOptions(context.Background(), issuerOptions)
	_, rotations, err := bootstrapSecrets(ctx, provider, &SecretsConfig{Generate: []SecretGen{{
		Key: "EXAMPLE", Type: "provider_credential", Source: "example.source", Name: "deploy-key",
	}}}, map[string]bool{"EXAMPLE": true}, legacyRevoker)
	if err != nil {
		t.Fatal(err)
	}
	if client.deleteCalls != 1 || client.deleteIdentifier != "old-credential" || client.deleteOperationID != client.operationID+"-delete-previous" {
		t.Fatalf("selected issuer Delete calls=%d operation=%q identifier=%q", client.deleteCalls, client.deleteOperationID, client.deleteIdentifier)
	}
	if len(legacyRevoker.calls) != 0 {
		t.Fatalf("typed rotation crossed into legacy revoker: %+v", legacyRevoker.calls)
	}
	if provider.stored["EXAMPLE_id"] != "credential-123" || provider.stored["EXAMPLE_secret"] != "sensitive-value" {
		t.Fatalf("stored values=%v", provider.stored)
	}
	if len(rotations) != 1 {
		t.Fatalf("rotations=%+v", rotations)
	}
	if !rotations[0].CutoffFromInventory || rotations[0].CreatedAt != "" {
		t.Fatalf("typed rotation cutoff metadata=%+v", rotations[0])
	}
}

func TestBootstrapSecretsTypedForceRotateRestoresStoredOutputsAfterEverySetFailure(t *testing.T) {
	for _, failKey := range []string{"EXAMPLE_id", "EXAMPLE_secret"} {
		t.Run(failKey, func(t *testing.T) {
			pluginDir := t.TempDir()
			installedDir := filepath.Join(pluginDir, "workflow-plugin-example")
			if err := os.MkdirAll(installedDir, 0o700); err != nil {
				t.Fatal(err)
			}
			manifest := `{"name":"workflow-plugin-example","version":"1.2.3","author":"Workflow tests","description":"provider capability fixture","credentialSources":[{"source":"example.source","concurrencyMode":"provider_idempotent","outputs":[{"key":"id"},{"key":"secret"}],"identifierKey":"id"}]}`
			if err := os.WriteFile(filepath.Join(installedDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
				t.Fatal(err)
			}
			client := &recordingCredentialIssuerClient{
				issueResponse: confirmedCredentialIssueResponse(),
				deleteResponse: &pb.CredentialDeleteResponse{
					Identifier: "credential-123", IdentifierSensitive: true,
					ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
				},
			}
			withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
				Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
				Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
			})
			provider := &transactionalSecretProvider{
				stored: map[string]string{
					"EXAMPLE_id": "old-credential", "EXAMPLE_secret": "old-secret",
				},
				failKey: failKey,
			}
			if failKey == "EXAMPLE_id" {
				provider.failValue = "credential-123"
			} else {
				provider.failValue = "sensitive-value"
			}
			legacyRevoker := &recordingRevoker{}
			ctx := withCredentialIssuerOptions(context.Background(), credentialIssuerOptions{
				Enabled: true, PluginDir: pluginDir, StateDir: t.TempDir(), NonInteractive: true,
			})
			_, _, err := bootstrapSecrets(ctx, provider, &SecretsConfig{Generate: []SecretGen{{
				Key: "EXAMPLE", Type: "provider_credential", Source: "example.source", Name: "deploy-key",
			}}}, map[string]bool{"EXAMPLE": true}, legacyRevoker)
			if err == nil {
				t.Fatal("want secret-store failure")
			}
			if provider.stored["EXAMPLE_id"] != "old-credential" || provider.stored["EXAMPLE_secret"] != "old-secret" {
				t.Fatalf("failed rotation destroyed working values: %v", provider.stored)
			}
			if client.deleteCalls != 1 || client.deleteIdentifier != "credential-123" || client.deleteOperationID != client.operationID+"-rollback" {
				t.Fatalf("new credential rollback calls=%d operation=%q identifier=%q", client.deleteCalls, client.deleteOperationID, client.deleteIdentifier)
			}
			if len(legacyRevoker.calls) != 0 {
				t.Fatalf("typed rollback crossed into legacy revoker: %+v", legacyRevoker.calls)
			}
		})
	}
}

func TestBootstrapSecretsTypedPartialRegenerationRestoresStoredOutputsAfterEverySetFailure(t *testing.T) {
	for _, failKey := range []string{"EXAMPLE_id", "EXAMPLE_secret"} {
		t.Run(failKey, func(t *testing.T) {
			pluginDir := t.TempDir()
			installedDir := filepath.Join(pluginDir, "workflow-plugin-example")
			if err := os.MkdirAll(installedDir, 0o700); err != nil {
				t.Fatal(err)
			}
			manifest := `{"name":"workflow-plugin-example","version":"1.2.3","author":"Workflow tests","description":"provider capability fixture","credentialSources":[{"source":"example.source","concurrencyMode":"provider_idempotent","outputs":[{"key":"id"},{"key":"secret"}],"identifierKey":"id"}]}`
			if err := os.WriteFile(filepath.Join(installedDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
				t.Fatal(err)
			}
			client := &recordingCredentialIssuerClient{
				issueResponse: confirmedCredentialIssueResponse(),
				deleteResponse: &pb.CredentialDeleteResponse{
					Identifier: "credential-123", IdentifierSensitive: true,
					ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
				},
			}
			withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
				Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
				Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
			})
			provider := &transactionalSecretProvider{
				stored:    map[string]string{"EXAMPLE_id": "old-credential"},
				failKey:   failKey,
				failValue: map[string]string{"EXAMPLE_id": "credential-123", "EXAMPLE_secret": "sensitive-value"}[failKey],
			}
			ctx := withCredentialIssuerOptions(context.Background(), credentialIssuerOptions{
				Enabled: true, PluginDir: pluginDir, StateDir: t.TempDir(), NonInteractive: true,
			})
			_, _, err := bootstrapSecrets(ctx, provider, &SecretsConfig{Generate: []SecretGen{{
				Key: "EXAMPLE", Type: "provider_credential", Source: "example.source", Name: "deploy-key",
			}}}, nil)
			if err == nil {
				t.Fatal("want secret-store failure")
			}
			if got := provider.stored["EXAMPLE_id"]; got != "old-credential" {
				t.Fatalf("partial regeneration destroyed existing identifier: stored=%v", provider.stored)
			}
			if _, exists := provider.stored["EXAMPLE_secret"]; exists {
				t.Fatalf("partial regeneration retained newly introduced secret: stored=%v", provider.stored)
			}
			if client.deleteCalls != 1 || client.deleteIdentifier != "credential-123" || client.deleteOperationID != client.operationID+"-rollback" {
				t.Fatalf("new credential rollback calls=%d operation=%q identifier=%q", client.deleteCalls, client.deleteOperationID, client.deleteIdentifier)
			}
		})
	}
}

func TestBootstrapSecretsTypedPartialRegenerationRevokesReplacedCredentialAfterStore(t *testing.T) {
	pluginDir := t.TempDir()
	installedDir := filepath.Join(pluginDir, "workflow-plugin-example")
	if err := os.MkdirAll(installedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"workflow-plugin-example","version":"1.2.3","author":"Workflow tests","description":"provider capability fixture","credentialSources":[{"source":"example.source","concurrencyMode":"provider_idempotent","outputs":[{"key":"id"},{"key":"secret"}],"identifierKey":"id"}]}`
	if err := os.WriteFile(filepath.Join(installedDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &recordingCredentialIssuerClient{
		issueResponse: confirmedCredentialIssueResponse(),
		deleteResponse: &pb.CredentialDeleteResponse{
			Identifier:          "old-credential",
			ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
		},
	}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
	})
	provider := &transactionalSecretProvider{stored: map[string]string{"EXAMPLE_id": "old-credential"}}
	ctx := withCredentialIssuerOptions(context.Background(), credentialIssuerOptions{
		Enabled: true, PluginDir: pluginDir, StateDir: t.TempDir(), NonInteractive: true,
	})
	_, _, err := bootstrapSecrets(ctx, provider, &SecretsConfig{Generate: []SecretGen{{
		Key: "EXAMPLE", Type: "provider_credential", Source: "example.source", Name: "deploy-key",
	}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if provider.stored["EXAMPLE_id"] != "credential-123" || provider.stored["EXAMPLE_secret"] != "sensitive-value" {
		t.Fatalf("stored values=%v", provider.stored)
	}
	if client.deleteCalls != 1 || client.deleteIdentifier != "old-credential" || client.deleteOperationID != client.operationID+"-delete-previous" {
		t.Fatalf("replaced credential cleanup calls=%d operation=%q identifier=%q", client.deleteCalls, client.deleteOperationID, client.deleteIdentifier)
	}
}

func TestBootstrapSecretsTypedPartialRegenerationRejectsWriteOnlyStoreBeforeIssue(t *testing.T) {
	pluginDir := t.TempDir()
	installedDir := filepath.Join(pluginDir, "workflow-plugin-example")
	if err := os.MkdirAll(installedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"workflow-plugin-example","version":"1.2.3","author":"Workflow tests","description":"provider capability fixture","credentialSources":[{"source":"example.source","concurrencyMode":"provider_idempotent","outputs":[{"key":"id"},{"key":"secret"}],"identifierKey":"id"}]}`
	if err := os.WriteFile(filepath.Join(installedDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &recordingCredentialIssuerClient{issueResponse: confirmedCredentialIssueResponse()}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
	})
	provider := &writeOnlyProvider{existing: []string{"EXAMPLE_id"}, listOK: true}
	ctx := withCredentialIssuerOptions(context.Background(), credentialIssuerOptions{
		Enabled: true, PluginDir: pluginDir, StateDir: t.TempDir(), NonInteractive: true,
	})
	_, _, err := bootstrapSecrets(ctx, provider, &SecretsConfig{Generate: []SecretGen{{
		Key: "EXAMPLE", Type: "provider_credential", Source: "example.source", Name: "deploy-key",
	}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "write-only") {
		t.Fatalf("error=%v, want safe write-only partial-regeneration rejection", err)
	}
	if client.issueCalls != 0 || len(provider.stored) != 0 {
		t.Fatalf("unsafe partial regeneration mutated state: Issue=%d stored=%v", client.issueCalls, provider.stored)
	}
}

func TestBootstrapSecretsTypedForceRotateRejectsWriteOnlyStoreBeforeIssue(t *testing.T) {
	pluginDir := t.TempDir()
	installedDir := filepath.Join(pluginDir, "workflow-plugin-example")
	if err := os.MkdirAll(installedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"workflow-plugin-example","version":"1.2.3","author":"Workflow tests","description":"provider capability fixture","credentialSources":[{"source":"example.source","concurrencyMode":"provider_idempotent","outputs":[{"key":"id"},{"key":"secret"}],"identifierKey":"id"}]}`
	if err := os.WriteFile(filepath.Join(installedDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &recordingCredentialIssuerClient{issueResponse: confirmedCredentialIssueResponse()}
	withCredentialIssuerResolver(t, client, config.CredentialSourceDecl{
		Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
		Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
	})
	provider := &writeOnlyProvider{existing: []string{"EXAMPLE_id", "EXAMPLE_secret"}, listOK: true}
	ctx := withCredentialIssuerOptions(context.Background(), credentialIssuerOptions{
		Enabled: true, PluginDir: pluginDir, StateDir: t.TempDir(), NonInteractive: true,
	})
	_, _, err := bootstrapSecrets(ctx, provider, &SecretsConfig{Generate: []SecretGen{{
		Key: "EXAMPLE", Type: "provider_credential", Source: "example.source", Name: "deploy-key",
	}}}, map[string]bool{"EXAMPLE": true})
	if err == nil || !strings.Contains(err.Error(), "write-only") {
		t.Fatalf("error=%v, want safe write-only rotation rejection", err)
	}
	if client.issueCalls != 0 || len(provider.stored) != 0 {
		t.Fatalf("unsafe rotation mutated state: Issue=%d stored=%v", client.issueCalls, provider.stored)
	}
}

func TestBootstrapSecretsTypedForceRotatePreflightFailurePreservesStoredOutputs(t *testing.T) {
	tests := []struct {
		name        string
		mode        config.CredentialConcurrencyMode
		resolveErr  error
		acknowledge bool
	}{
		{name: "single writer acknowledgement", mode: config.CredentialConcurrencySingleWriter},
		{name: "runtime parity", mode: config.CredentialConcurrencyProviderIdempotent, resolveErr: errFakeStoreUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pluginDir := t.TempDir()
			installedDir := filepath.Join(pluginDir, "workflow-plugin-example")
			if err := os.MkdirAll(installedDir, 0o700); err != nil {
				t.Fatal(err)
			}
			manifest := `{"name":"workflow-plugin-example","version":"1.2.3","author":"Workflow tests","description":"provider capability fixture","credentialSources":[{"source":"example.source","concurrencyMode":"` + string(test.mode) + `","outputs":[{"key":"id"},{"key":"secret"}],"identifierKey":"id"}]}`
			if err := os.WriteFile(filepath.Join(installedDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
				t.Fatal(err)
			}
			client := &recordingCredentialIssuerClient{issueResponse: confirmedCredentialIssueResponse()}
			oldResolve := resolveCredentialIssuerCapability
			resolveCredentialIssuerCapability = func(context.Context, string, string) (pb.CredentialIssuerClient, func(), config.CredentialSourceDecl, string, string, bool, error) {
				declaration := config.CredentialSourceDecl{
					Source: "example.source", ConcurrencyMode: test.mode,
					Outputs: []config.CredentialOutputDecl{{Key: "id"}, {Key: "secret"}}, IdentifierKey: "id",
				}
				if test.resolveErr != nil {
					return nil, nil, config.CredentialSourceDecl{}, "", "", false, test.resolveErr
				}
				return client, func() {}, declaration, "workflow-plugin-example", "1.2.3", true, nil
			}
			t.Cleanup(func() { resolveCredentialIssuerCapability = oldResolve })

			provider := &transactionalSecretProvider{stored: map[string]string{
				"EXAMPLE_id": "old-credential", "EXAMPLE_secret": "old-secret",
			}}
			ctx := withCredentialIssuerOptions(context.Background(), credentialIssuerOptions{
				Enabled: true, PluginDir: pluginDir, StateDir: t.TempDir(),
				NonInteractive: true, AckSingleWriter: test.acknowledge,
			})
			_, _, err := bootstrapSecrets(ctx, provider, &SecretsConfig{Generate: []SecretGen{{
				Key: "EXAMPLE", Type: "provider_credential", Source: "example.source", Name: "deploy-key",
			}}}, map[string]bool{"EXAMPLE": true})
			if err == nil {
				t.Fatal("want issuer preflight failure")
			}
			if client.issueCalls != 0 || len(provider.deleted) != 0 || len(provider.setCalls) != 0 {
				t.Fatalf("preflight failure mutated state: Issue=%d Delete=%v Set=%v", client.issueCalls, provider.deleted, provider.setCalls)
			}
			if provider.stored["EXAMPLE_id"] != "old-credential" || provider.stored["EXAMPLE_secret"] != "old-secret" {
				t.Fatalf("preflight failure changed stored outputs: %v", provider.stored)
			}
		})
	}
}

// TestBootstrapSecrets_WriteOnlyProviderSkipsExisting verifies that when the
// provider is write-only (GitHub Actions), bootstrapSecrets consults List()
// and skips regeneration if the secret name already exists. Without this,
// every bootstrap run regenerates, and for provider_credential that orphans
// upstream credentials (e.g. DO Spaces access keys).
func TestBootstrapSecrets_WriteOnlyProviderSkipsExisting(t *testing.T) {
	p := &writeOnlyProvider{
		existing: []string{"JWT_SECRET", "SPACES_access_key", "SPACES_secret_key"},
		listOK:   true,
	}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "JWT_SECRET", Type: "random_hex", Length: 32},
			{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces"},
		},
	}
	if _, _, err := bootstrapSecrets(context.Background(), p, cfg, nil); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}
	if len(p.stored) != 0 {
		t.Fatalf("stored = %v, want empty (all secrets already exist)", p.stored)
	}
	if p.listCalls != 1 {
		t.Fatalf("List called %d times, want 1 (should be cached)", p.listCalls)
	}
}

// TestBootstrapSecrets_GitHubProviderCredentialMatchesUppercaseList verifies
// GitHub's write-only secret list can satisfy mixed-case generated key probes.
// GitHub Actions secret names are case-insensitive, and the API reports common
// subkey names as uppercase (SPACES_ACCESS_KEY / SPACES_SECRET_KEY). Without
// this, auto-bootstrap attempts to recreate an existing upstream provider
// credential and DigitalOcean refuses the duplicate name.
func TestBootstrapSecrets_GitHubProviderCredentialMatchesUppercaseList(t *testing.T) {
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		t.Fatal("generator must not be called when GitHub-listed sub-keys already exist")
		return "", nil
	})
	p := &writeOnlyProvider{
		name:     "github",
		existing: []string{"SPACES_ACCESS_KEY", "SPACES_SECRET_KEY"},
		listOK:   true,
	}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces"},
		},
	}
	if _, _, err := bootstrapSecrets(context.Background(), p, cfg, nil); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}
	if len(p.stored) != 0 {
		t.Fatalf("stored = %v, want empty (GitHub-listed secrets already exist)", p.stored)
	}
}

// TestBootstrapSecrets_WriteOnlyProviderGeneratesWhenMissing verifies the
// fallback still generates when List shows the name is absent.
func TestBootstrapSecrets_WriteOnlyProviderGeneratesWhenMissing(t *testing.T) {
	p := &writeOnlyProvider{
		existing: []string{"UNRELATED"},
		listOK:   true,
	}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "JWT_SECRET", Type: "random_hex", Length: 8},
		},
	}
	if _, _, err := bootstrapSecrets(context.Background(), p, cfg, nil); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}
	if _, ok := p.stored["JWT_SECRET"]; !ok {
		t.Fatalf("JWT_SECRET was not stored; stored=%v", p.stored)
	}
}

// TestBootstrapSecrets_WriteOnlyProviderListUnsupported verifies that when
// both Get and List return ErrUnsupported, bootstrap regenerates (preserves
// prior behaviour for providers with no introspection at all).
func TestBootstrapSecrets_WriteOnlyProviderListUnsupported(t *testing.T) {
	p := &writeOnlyProvider{listOK: false}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "JWT_SECRET", Type: "random_hex", Length: 8},
		},
	}
	if _, _, err := bootstrapSecrets(context.Background(), p, cfg, nil); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}
	if len(p.stored) != 1 {
		t.Fatalf("stored = %v, want 1 entry (List unsupported → regenerate)", p.stored)
	}
}

// TestBootstrapSecrets_ProviderCredentialAllSubKeysPresent verifies the
// provider_credential skip path: both access_key and secret_key sub-keys
// must exist before the generator is skipped.
func TestBootstrapSecrets_ProviderCredentialAllSubKeysPresent(t *testing.T) {
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		t.Fatal("generator must not be called when both sub-keys already exist")
		return "", nil
	})
	p := &writeOnlyProvider{
		existing: []string{"SPACES_access_key", "SPACES_secret_key"},
		listOK:   true,
	}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces"},
		},
	}
	if _, _, err := bootstrapSecrets(context.Background(), p, cfg, nil); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}
	if len(p.stored) != 0 {
		t.Fatalf("stored = %v, want empty", p.stored)
	}
}

// TestBootstrapSecrets_ProviderCredentialPartialRegenerates verifies that a
// partial prior write (one sub-key missing) triggers regeneration.
func TestBootstrapSecrets_ProviderCredentialPartialRegenerates(t *testing.T) {
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		out, _ := json.Marshal(map[string]string{
			"access_key": "new-access",
			"secret_key": "new-secret",
		})
		return string(out), nil
	})
	// Only the access_key is present — the secret_key is missing, so the
	// stored credential is unusable and bootstrap must regenerate.
	p := &writeOnlyProvider{
		existing: []string{"SPACES_access_key"},
		listOK:   true,
	}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces"},
		},
	}
	if _, _, err := bootstrapSecrets(context.Background(), p, cfg, nil); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}
	if got := p.stored["SPACES_access_key"]; got != "new-access" {
		t.Errorf("SPACES_access_key = %q, want %q", got, "new-access")
	}
	if got := p.stored["SPACES_secret_key"]; got != "new-secret" {
		t.Errorf("SPACES_secret_key = %q, want %q", got, "new-secret")
	}
}

// TestBootstrapSecrets_StorageFilter_OnlyPersistsSubKeys verifies that
// provider_credential JSON is filtered to the canonical sub-keys defined in
// providerCredentialSubKeys before being persisted as GH Secrets. Without
// this filter, sidecar metadata that the generator now emits alongside the
// canonical creds (e.g. created_at after Task 8) would leak into the GH
// Secrets store as phantom keys like SPACES_created_at — breaking the
// audit-keys/prune contract that "every GH Secret matches an upstream key"
// (ADR 0020 same-commit constraint with Task 8).
//
// This is the failing test for Task 9 of the spaces-key-iac-resource plan.
// Until Task 10 implements the sub-key allow-list filter in bootstrapSecrets,
// this test fails at the SPACES_created_at assertion.
func TestBootstrapSecrets_StorageFilter_OnlyPersistsSubKeys(t *testing.T) {
	// Stub generateSecret to mimic the post-Task-8 generateDOSpacesKey shape:
	// access_key + secret_key (canonical) plus created_at (sidecar metadata).
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		out, _ := json.Marshal(map[string]string{
			"access_key": "AK",
			"secret_key": "SK",
			"created_at": "2026-05-08T10:00:00Z",
		})
		return string(out), nil
	})

	// Empty existing → bootstrap will generate.
	p := &writeOnlyProvider{
		existing: nil,
		listOK:   true,
	}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{
				Key:    "SPACES",
				Type:   "provider_credential",
				Source: "digitalocean.spaces",
				Name:   "test-key",
			},
		},
	}

	if _, _, err := bootstrapSecrets(context.Background(), p, cfg, nil); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}

	// Storage MUST contain the two canonical sub-keys.
	if _, ok := p.stored["SPACES_access_key"]; !ok {
		t.Errorf("SPACES_access_key should be stored; stored=%v", p.stored)
	}
	if _, ok := p.stored["SPACES_secret_key"]; !ok {
		t.Errorf("SPACES_secret_key should be stored; stored=%v", p.stored)
	}

	// Storage MUST NOT contain sidecar metadata fields like created_at:
	// these are not real GH Secrets and would pollute audit-keys/prune output.
	if _, ok := p.stored["SPACES_created_at"]; ok {
		t.Errorf("SPACES_created_at MUST NOT be stored as a GH Secret (storage-filter regression); stored=%v", p.stored)
	}
}

// TestBootstrapSecrets_ProviderCredentialProbeIgnoresBareKey verifies that a
// plain secret named the same as the provider_credential key (without the
// _access_key / _secret_key suffixes) does not cause a false skip.
func TestBootstrapSecrets_ProviderCredentialProbeIgnoresBareKey(t *testing.T) {
	generateCalls := 0
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		generateCalls++
		out, _ := json.Marshal(map[string]string{
			"access_key": "a",
			"secret_key": "b",
		})
		return string(out), nil
	})
	// "SPACES" is present, but the real sub-keys are not — must regenerate.
	p := &writeOnlyProvider{
		existing: []string{"SPACES"},
		listOK:   true,
	}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces"},
		},
	}
	if _, _, err := bootstrapSecrets(context.Background(), p, cfg, nil); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}
	if generateCalls != 1 {
		t.Fatalf("generator called %d times, want 1", generateCalls)
	}
}

// failOnSetProvider triggers a Set() failure on the first matching key
// — used to exercise the transactional rollback path when a
// provider_credential creation succeeds upstream but the store-write
// half fails (e.g. GH PAT lost permissions mid-bootstrap).
type failOnSetProvider struct {
	failKey  string
	setCalls []string
	deleted  []string
}

type transactionalSecretProvider struct {
	stored    map[string]string
	failKey   string
	failValue string
	setCalls  []string
	deleted   []string
}

func (p *transactionalSecretProvider) Name() string { return "transactional-fake" }
func (p *transactionalSecretProvider) Get(_ context.Context, key string) (string, error) {
	value, ok := p.stored[key]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return value, nil
}
func (p *transactionalSecretProvider) Set(_ context.Context, key, value string) error {
	p.setCalls = append(p.setCalls, key)
	if key == p.failKey && value == p.failValue {
		return errFakeStoreUnavailable
	}
	if p.stored == nil {
		p.stored = make(map[string]string)
	}
	p.stored[key] = value
	return nil
}
func (p *transactionalSecretProvider) Delete(_ context.Context, key string) error {
	p.deleted = append(p.deleted, key)
	delete(p.stored, key)
	return nil
}
func (p *transactionalSecretProvider) List(_ context.Context) ([]string, error) {
	keys := make([]string, 0, len(p.stored))
	for key := range p.stored {
		keys = append(keys, key)
	}
	return keys, nil
}

func (p *failOnSetProvider) Name() string { return "fail-on-set" }
func (p *failOnSetProvider) Get(_ context.Context, _ string) (string, error) {
	return "", secrets.ErrUnsupported
}
func (p *failOnSetProvider) Set(_ context.Context, key, _ string) error {
	p.setCalls = append(p.setCalls, key)
	if key == p.failKey {
		return errFakeStoreUnavailable
	}
	return nil
}
func (p *failOnSetProvider) Delete(_ context.Context, key string) error {
	p.deleted = append(p.deleted, key)
	return nil
}
func (p *failOnSetProvider) List(_ context.Context) ([]string, error) {
	return nil, nil
}

// recordingRevoker captures RevokeProviderCredential calls so the test
// can assert rollback occurred. Implements interfaces.ProviderCredentialRevoker.
type recordingRevoker struct {
	calls []revokeCall
}
type revokeCall struct {
	source       string
	credentialID string
}

func (r *recordingRevoker) RevokeProviderCredential(_ context.Context, source, credentialID string) error {
	r.calls = append(r.calls, revokeCall{source: source, credentialID: credentialID})
	return nil
}

var errFakeStoreUnavailable = errFakeStore("store unavailable (simulated)")

type errFakeStore string

func (e errFakeStore) Error() string { return string(e) }

// TestBootstrapSecrets_ProviderCredential_RollbackOnSetFailure is the
// regression test for the orphan-key bug: when generateSecret returns a
// fresh DO Spaces key but provider.Set fails to persist it, bootstrap
// MUST revoke the just-minted upstream credential.
//
// Pre-fix behaviour: Set fails → return error → DO key remains in the
// account with an unrecoverable secret_key. Every subsequent run mints
// another orphan with the same name.
//
// Post-fix: Set failure triggers credRevoker.RevokeProviderCredential
// with the access_key from the just-generated subKeyMap.
func TestBootstrapSecrets_ProviderCredential_RollbackOnSetFailure(t *testing.T) {
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		out, _ := json.Marshal(map[string]string{
			"access_key": "AK_ORPHAN",
			"secret_key": "SK_DOOMED",
		})
		return string(out), nil
	})

	p := &failOnSetProvider{failKey: "SPACES_secret_key"}
	rev := &recordingRevoker{}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces"},
		},
	}

	_, _, err := bootstrapSecrets(context.Background(), p, cfg, nil, rev)
	if err == nil {
		t.Fatal("expected Set failure to surface as error")
	}

	if len(rev.calls) != 1 {
		t.Fatalf("expected 1 rollback-revoke call; got %d", len(rev.calls))
	}
	if rev.calls[0].credentialID != "AK_ORPHAN" {
		t.Errorf("rollback called with credentialID=%q want AK_ORPHAN", rev.calls[0].credentialID)
	}
	if rev.calls[0].source != "digitalocean.spaces" {
		t.Errorf("rollback source=%q want digitalocean.spaces", rev.calls[0].source)
	}
}

// TestBootstrapSecrets_ProviderCredential_RollbackOnFirstSetFailure
// guards the most insidious shape of the bug: the very first Set call
// fails (e.g. access_key write). The pre-fix code extracted
// newAccessKey *during* the for-range loop, so a first-iteration
// failure left newAccessKey empty even though the upstream key exists.
// The fix extracts access_key BEFORE the loop.
func TestBootstrapSecrets_ProviderCredential_RollbackOnFirstSetFailure(t *testing.T) {
	withStubGenerator(t, func(_ context.Context, _ string, _ map[string]any) (string, error) {
		out, _ := json.Marshal(map[string]string{
			"access_key": "AK_FIRST",
			"secret_key": "SK_FIRST",
		})
		return string(out), nil
	})

	p := &failOnSetProvider{failKey: "SPACES_access_key"}
	rev := &recordingRevoker{}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces"},
		},
	}

	_, _, err := bootstrapSecrets(context.Background(), p, cfg, nil, rev)
	if err == nil {
		t.Fatal("expected Set failure")
	}
	if len(rev.calls) != 1 || rev.calls[0].credentialID != "AK_FIRST" {
		t.Errorf("rollback calls = %v; want one revoke of AK_FIRST", rev.calls)
	}
}

// TestBootstrapSecrets_ForwardsGenName is the regression test for the
// breakage caught during the gocodealone-multisite deploy: SecretGen
// has a `name:` field but bootstrapSecrets didn't propagate it into
// genConfig. provider_credential generators requiring a non-empty
// name (e.g. digitalocean.spaces post-v0.60.4) then failed every run
// because the config they received was missing the field.
func TestBootstrapSecrets_ForwardsGenName(t *testing.T) {
	var capturedConfig map[string]any
	withStubGenerator(t, func(_ context.Context, _ string, cfg map[string]any) (string, error) {
		capturedConfig = cfg
		out, _ := json.Marshal(map[string]string{
			"access_key": "AK",
			"secret_key": "SK",
		})
		return string(out), nil
	})

	p := &writeOnlyProvider{listOK: true}
	cfg := &SecretsConfig{
		Generate: []SecretGen{
			{
				Key:    "SPACES",
				Type:   "provider_credential",
				Source: "digitalocean.spaces",
				Name:   "multisite-deploy-key",
			},
		},
	}
	if _, _, err := bootstrapSecrets(context.Background(), p, cfg, nil); err != nil {
		t.Fatalf("bootstrapSecrets: %v", err)
	}
	if got := capturedConfig["name"]; got != "multisite-deploy-key" {
		t.Errorf("generator received name=%v want multisite-deploy-key (full config: %v)", got, capturedConfig)
	}
	if got := capturedConfig["source"]; got != "digitalocean.spaces" {
		t.Errorf("generator received source=%v want digitalocean.spaces", got)
	}
}
