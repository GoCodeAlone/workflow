package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

// writeOnlyAuditProvider mimics GitHubSecretsProvider Get/List ErrUnsupported.
type writeOnlyAuditProvider struct {
	envTestProvider // embed for Set/Delete pass-through; values map shared
}

func (p *writeOnlyAuditProvider) Get(_ context.Context, _ string) (string, error) {
	return "", secrets.ErrUnsupported
}
func (p *writeOnlyAuditProvider) List(_ context.Context) ([]string, error) {
	return nil, secrets.ErrUnsupported
}

func TestAuditStateSecrets_NoFindings_ExitZero(t *testing.T) {
	store := &stubInfraStore{
		saved: []interfaces.ResourceState{
			{Name: "ok", Outputs: map[string]any{"bucket": "b", "region": "nyc3"}},
		},
	}
	prov := newEnvTestProvider()
	w := &bytes.Buffer{}
	rc := runAuditStateSecrets(context.Background(), w, store, prov)
	if rc != 0 {
		t.Errorf("rc = %d, want 0; output:\n%s", rc, w.String())
	}
	if !strings.Contains(w.String(), "no findings") {
		t.Errorf("expected 'no findings', got: %s", w.String())
	}
}

func TestAuditStateSecrets_OrphanInProvider(t *testing.T) {
	// Provider has a routed-secret "ghost_secret_key" but state has no ghost resource.
	store := &stubInfraStore{
		saved: []interfaces.ResourceState{{Name: "live", Outputs: map[string]any{"bucket": "b"}}},
	}
	prov := newEnvTestProvider()
	prov.values["ghost_secret_key"] = "ORPHAN"
	w := &bytes.Buffer{}
	rc := runAuditStateSecrets(context.Background(), w, store, prov)
	if rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}
	if !strings.Contains(w.String(), "orphan") || !strings.Contains(w.String(), "ghost_secret_key") {
		t.Errorf("expected orphan finding for ghost_secret_key; got:\n%s", w.String())
	}
}

func TestAuditStateSecrets_LegacyPlaintext(t *testing.T) {
	store := &stubInfraStore{
		saved: []interfaces.ResourceState{
			{Name: "legacy", Outputs: map[string]any{"secret_key": "PLAINTEXT-SECRET"}},
		},
	}
	prov := newEnvTestProvider()
	w := &bytes.Buffer{}
	rc := runAuditStateSecrets(context.Background(), w, store, prov)
	if rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}
	if !strings.Contains(w.String(), "legacy plaintext") || !strings.Contains(w.String(), "legacy") {
		t.Errorf("expected legacy plaintext finding; got:\n%s", w.String())
	}
}

func TestAuditStateSecrets_PlaceholderMissingValue(t *testing.T) {
	// State has a placeholder but provider doesn't have the secret.
	store := &stubInfraStore{
		saved: []interfaces.ResourceState{
			{Name: "broken", Outputs: map[string]any{"secret_key": "secret_ref://broken_secret_key"}},
		},
	}
	prov := newEnvTestProvider()
	w := &bytes.Buffer{}
	rc := runAuditStateSecrets(context.Background(), w, store, prov)
	if rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}
	if !strings.Contains(w.String(), "missing routed value") || !strings.Contains(w.String(), "broken_secret_key") {
		t.Errorf("expected missing-routed-value finding; got:\n%s", w.String())
	}
}

func TestAuditStateSecrets_MistakenSecretConfigRefInState(t *testing.T) {
	// State contains a "secret://" string (user-config syntax leaked into state).
	store := &stubInfraStore{
		saved: []interfaces.ResourceState{
			{Name: "weird", Outputs: map[string]any{"token": "secret://my_token"}},
		},
	}
	prov := newEnvTestProvider()
	w := &bytes.Buffer{}
	rc := runAuditStateSecrets(context.Background(), w, store, prov)
	if rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}
	if !strings.Contains(w.String(), "config-reference in state") {
		t.Errorf("expected config-reference-in-state finding; got:\n%s", w.String())
	}
}

func TestAuditStateSecrets_Prune_DeletesOrphans(t *testing.T) {
	store := &stubInfraStore{
		saved: []interfaces.ResourceState{{Name: "live", Outputs: map[string]any{"bucket": "b"}}},
	}
	prov := newEnvTestProvider()
	prov.values["ghost_secret_key"] = "ORPHAN"
	w := &bytes.Buffer{}
	rc := runAuditStateSecretsWithPrune(context.Background(), w, store, prov, true)
	if rc != 0 {
		t.Errorf("rc = %d, want 0 after prune; output:\n%s", rc, w.String())
	}
	if _, ok := prov.values["ghost_secret_key"]; ok {
		t.Errorf("orphan secret not pruned: %v", prov.values)
	}
	if !strings.Contains(w.String(), "pruned") {
		t.Errorf("expected 'pruned' line in output, got: %s", w.String())
	}
}

func TestAuditStateSecrets_ListUnsupported_ReportsAdvisory(t *testing.T) {
	store := &stubInfraStore{
		saved: []interfaces.ResourceState{
			{Name: "ok", Outputs: map[string]any{"secret_key": "secret_ref://ok_secret_key"}},
		},
	}
	prov := &writeOnlyAuditProvider{envTestProvider: envTestProvider{values: map[string]string{}}}
	w := &bytes.Buffer{}
	rc := runAuditStateSecrets(context.Background(), w, store, prov)
	if rc == 2 {
		t.Errorf("rc=2 reserved for hard audit errors; should not fire on write-only providers; output:\n%s", w.String())
	}
	if !strings.Contains(w.String(), "unsupported") {
		t.Errorf("expected write-only provider advisory; got:\n%s", w.String())
	}
}

func TestAuditStateSecrets_ValidPlaceholderWithMatchingProvider_NoFinding(t *testing.T) {
	store := &stubInfraStore{
		saved: []interfaces.ResourceState{
			{Name: "ok", Outputs: map[string]any{"secret_key": "secret_ref://ok_secret_key"}},
		},
	}
	prov := newEnvTestProvider()
	prov.values["ok_secret_key"] = "VALID"
	w := &bytes.Buffer{}
	rc := runAuditStateSecrets(context.Background(), w, store, prov)
	if rc != 0 {
		t.Errorf("rc = %d, want 0 (placeholder + matching provider value); output:\n%s", rc, w.String())
	}
}
