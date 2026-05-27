package handler_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/iac/admin/handler"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// seedDetailFixture returns a store with one resource carrying
// sensitive output keys + applied config so the GetResource test can
// exercise the redaction path.
func seedDetailFixture() *fakeStateStore {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	return &fakeStateStore{
		resources: []interfaces.ResourceState{{
			ID:            "db-prod",
			Name:          "db-prod",
			Type:          "infra.database",
			Provider:      "digitalocean",
			ProviderRef:   "do-prod",
			ProviderID:    "db-001",
			ConfigHash:    "sha256:cafef00d",
			AppliedConfig: map[string]any{"engine": "postgres", "size": "m"},
			Outputs: map[string]any{
				"id":          "db-001",
				"endpoint":    "db.internal:5432",
				"password":    "super-secret-pw",
				"access_key":  "AKIA...",
				"private_key": "-----BEGIN...",
				"plain_field": "ok-to-show",
			},
			Dependencies:   []string{"vpc-prod"},
			UpdatedAt:      now,
			LastDriftCheck: now.Add(-time.Hour),
		}},
	}
}

func TestGetResource_HappyPath(t *testing.T) {
	store := seedDetailFixture()
	in := &adminpb.AdminGetResourceInput{Name: "db-prod", Evidence: authzOK()}
	out, err := handler.GetResource(context.Background(), store, in)
	if err != nil {
		t.Fatalf("GetResource: %v", err)
	}
	if out == nil || out.Resource == nil {
		t.Fatal("nil output or detail with nil error")
	}
	d := out.Resource
	if d.Summary == nil || d.Summary.Name != "db-prod" {
		t.Errorf("summary.name = %v, want db-prod", d.Summary)
	}
	if d.ConfigHash != "sha256:cafef00d" {
		t.Errorf("config_hash = %q, want sha256:cafef00d", d.ConfigHash)
	}
	if len(d.AppliedConfigJson) == 0 {
		t.Error("applied_config_json empty; expected JSON-encoded AppliedConfig")
	}
	if len(d.OutputsJson) == 0 {
		t.Error("outputs_json empty; expected JSON-encoded redacted outputs")
	}
}

func TestGetResource_RedactsSensitiveOutputs(t *testing.T) {
	store := seedDetailFixture()
	in := &adminpb.AdminGetResourceInput{Name: "db-prod", Evidence: authzOK()}
	out, err := handler.GetResource(context.Background(), store, in)
	if err != nil {
		t.Fatalf("GetResource: %v", err)
	}

	var outputs map[string]any
	if err := json.Unmarshal(out.Resource.OutputsJson, &outputs); err != nil {
		t.Fatalf("outputs_json not valid JSON: %v", err)
	}

	// Sensitive keys must be masked.
	sensitiveKeys := []string{"password", "access_key", "private_key"}
	for _, k := range sensitiveKeys {
		v, ok := outputs[k]
		if !ok {
			t.Errorf("output key %q dropped entirely; expected mask, not removal", k)
			continue
		}
		if vs, _ := v.(string); strings.Contains(vs, "super-secret") || strings.Contains(vs, "AKIA") || strings.Contains(vs, "BEGIN") {
			t.Errorf("output %q value not masked: %v", k, v)
		}
	}

	// Non-sensitive keys must pass through unchanged.
	if outputs["id"] != "db-001" {
		t.Errorf("id leaked or mangled: %v", outputs["id"])
	}
	if outputs["endpoint"] != "db.internal:5432" {
		t.Errorf("endpoint leaked or mangled: %v", outputs["endpoint"])
	}
	if outputs["plain_field"] != "ok-to-show" {
		t.Errorf("plain_field leaked or mangled: %v", outputs["plain_field"])
	}

	// sensitive_outputs_redacted must list each redacted key.
	got := map[string]bool{}
	for _, k := range out.Resource.SensitiveOutputsRedacted {
		got[k] = true
	}
	for _, k := range sensitiveKeys {
		if !got[k] {
			t.Errorf("sensitive_outputs_redacted missing %q (got %v)", k, out.Resource.SensitiveOutputsRedacted)
		}
	}
	// Should NOT list non-sensitive keys.
	for _, k := range []string{"id", "endpoint", "plain_field"} {
		if got[k] {
			t.Errorf("sensitive_outputs_redacted leaked non-sensitive key %q", k)
		}
	}
}

func TestGetResource_NotFound(t *testing.T) {
	store := seedDetailFixture()
	in := &adminpb.AdminGetResourceInput{Name: "does-not-exist", Evidence: authzOK()}
	out, err := handler.GetResource(context.Background(), store, in)
	if err != nil {
		t.Fatalf("GetResource should not error on not-found — surfaces via Output.error: %v", err)
	}
	if out.Resource != nil {
		t.Errorf("expected nil Resource on not-found, got %+v", out.Resource)
	}
	if out.Error == "" {
		t.Error("expected non-empty Error on not-found")
	}
}

func TestGetResource_DefaultDenyOnMissingEvidence(t *testing.T) {
	store := seedDetailFixture()
	in := &adminpb.AdminGetResourceInput{Name: "db-prod"} // no Evidence
	out, _ := handler.GetResource(context.Background(), store, in)
	if out.Error == "" {
		t.Error("expected non-empty Error on missing evidence (default-deny)")
	}
	if out.Resource != nil {
		t.Errorf("expected nil Resource on auth refusal, got %+v", out.Resource)
	}
}

func TestGetResource_DefaultDenyOnAuthzNotChecked(t *testing.T) {
	store := seedDetailFixture()
	in := &adminpb.AdminGetResourceInput{
		Name:     "db-prod",
		Evidence: &adminpb.AdminAuthzEvidence{AuthzChecked: false, AuthzAllowed: true},
	}
	out, _ := handler.GetResource(context.Background(), store, in)
	if out.Error == "" {
		t.Error("expected non-empty Error when authz_checked=false")
	}
}

func TestGetResource_DefaultDenyOnAuthzDenied(t *testing.T) {
	store := seedDetailFixture()
	in := &adminpb.AdminGetResourceInput{
		Name:     "db-prod",
		Evidence: &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: false},
	}
	out, _ := handler.GetResource(context.Background(), store, in)
	if out.Error == "" {
		t.Error("expected non-empty Error when authz_allowed=false")
	}
}

func TestGetResource_PopulatesSummaryFields(t *testing.T) {
	store := seedDetailFixture()
	in := &adminpb.AdminGetResourceInput{Name: "db-prod", Evidence: authzOK()}
	out, _ := handler.GetResource(context.Background(), store, in)
	s := out.Resource.Summary
	if s.ProviderType != "digitalocean" {
		t.Errorf("provider_type = %q, want digitalocean", s.ProviderType)
	}
	if s.ProviderModule != "do-prod" {
		t.Errorf("provider_module = %q, want do-prod", s.ProviderModule)
	}
	if s.ProviderId != "db-001" {
		t.Errorf("provider_id = %q, want db-001", s.ProviderId)
	}
	if len(s.Dependencies) != 1 || s.Dependencies[0] != "vpc-prod" {
		t.Errorf("dependencies = %v, want [vpc-prod]", s.Dependencies)
	}
}
