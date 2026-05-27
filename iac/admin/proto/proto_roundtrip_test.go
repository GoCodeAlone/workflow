package adminpb_test

import (
	"testing"

	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"google.golang.org/protobuf/encoding/protojson"
)

// TestAdminListResourcesInput_Roundtrip is the plan §Task 4 Step 3
// smoke test: protojson.Marshal + Unmarshal round-trips the typed
// input without losing scalar fields or the nested authz evidence.
// Wire format is protojson per design §Strict Proto Contracts.
func TestAdminListResourcesInput_Roundtrip(t *testing.T) {
	in := &adminpb.AdminListResourcesInput{
		TypeFilter:     "infra.vpc",
		ProviderFilter: "do-provider",
		EnvName:        "staging",
		Evidence: &adminpb.AdminAuthzEvidence{
			AuthzChecked:       true,
			AuthzAllowed:       true,
			Subject:            "user:alice",
			GrantedPermissions: []string{"infra:read"},
		},
	}
	bytes, err := protojson.Marshal(in)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	var out adminpb.AdminListResourcesInput
	if err := protojson.Unmarshal(bytes, &out); err != nil {
		t.Fatalf("protojson.Unmarshal: %v", err)
	}
	if out.TypeFilter != "infra.vpc" {
		t.Errorf("type_filter lost: got %q", out.TypeFilter)
	}
	if out.ProviderFilter != "do-provider" {
		t.Errorf("provider_filter lost: got %q", out.ProviderFilter)
	}
	if out.EnvName != "staging" {
		t.Errorf("env_name lost: got %q", out.EnvName)
	}
	if out.Evidence == nil {
		t.Fatal("evidence dropped from round-trip")
	}
	if !out.Evidence.AuthzChecked || !out.Evidence.AuthzAllowed {
		t.Errorf("evidence booleans lost: checked=%v allowed=%v", out.Evidence.AuthzChecked, out.Evidence.AuthzAllowed)
	}
	if out.Evidence.Subject != "user:alice" {
		t.Errorf("subject lost: got %q", out.Evidence.Subject)
	}
	if len(out.Evidence.GrantedPermissions) != 1 || out.Evidence.GrantedPermissions[0] != "infra:read" {
		t.Errorf("granted_permissions lost: got %v", out.Evidence.GrantedPermissions)
	}
}

// TestAdminResourceDetail_Roundtrip exercises the bytes-shaped
// applied_config_json + outputs_json fields. The handler library
// JSON-encodes the free-form per-resource payloads into these bytes;
// the test pins that protojson preserves the byte sequence without
// re-encoding as base64-then-misinterpreting on Unmarshal.
func TestAdminResourceDetail_Roundtrip(t *testing.T) {
	applied := []byte(`{"region":"nyc3","name":"site-vpc"}`)
	outputs := []byte(`{"id":"vpc-abc123"}`)
	in := &adminpb.AdminResourceDetail{
		Summary: &adminpb.AdminResourceSummary{
			Name:           "site-vpc",
			Type:           "infra.vpc",
			ProviderModule: "do-provider",
			ProviderType:   "digitalocean",
			ProviderId:     "vpc-abc123",
			Status:         "active",
		},
		AppliedConfigJson:        applied,
		OutputsJson:              outputs,
		ConfigHash:               "sha256:deadbeef",
		LastDriftCheckUnix:       1716800000,
		SensitiveOutputsRedacted: []string{"private_key"},
	}
	bytes, err := protojson.Marshal(in)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	var out adminpb.AdminResourceDetail
	if err := protojson.Unmarshal(bytes, &out); err != nil {
		t.Fatalf("protojson.Unmarshal: %v", err)
	}
	if string(out.AppliedConfigJson) != string(applied) {
		t.Errorf("applied_config_json mangled: got %q want %q", out.AppliedConfigJson, applied)
	}
	if string(out.OutputsJson) != string(outputs) {
		t.Errorf("outputs_json mangled: got %q want %q", out.OutputsJson, outputs)
	}
	if out.Summary == nil || out.Summary.Name != "site-vpc" {
		t.Errorf("summary lost: %+v", out.Summary)
	}
	if len(out.SensitiveOutputsRedacted) != 1 || out.SensitiveOutputsRedacted[0] != "private_key" {
		t.Errorf("sensitive_outputs_redacted lost: %v", out.SensitiveOutputsRedacted)
	}
	if out.LastDriftCheckUnix != 1716800000 {
		t.Errorf("last_drift_check_unix lost: got %d", out.LastDriftCheckUnix)
	}
}

// TestAdminGenerateConfigInput_FieldValuesMap pins protojson's
// map<string, string> handling. The form-builder submission is
// keyed by AdminFieldSpec.name; lost keys or value-type coercion
// would silently break catalog-driven config generation.
func TestAdminGenerateConfigInput_FieldValuesMap(t *testing.T) {
	in := &adminpb.AdminGenerateConfigInput{
		ResourceType:   "infra.vpc",
		ResourceName:   "site-vpc",
		ProviderModule: "do-provider",
		FieldValues: map[string]string{
			"region":   "nyc3",
			"name":     "site-vpc",
			"ip_range": "10.10.0.0/16",
		},
		Evidence: &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: true},
	}
	bytes, err := protojson.Marshal(in)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	var out adminpb.AdminGenerateConfigInput
	if err := protojson.Unmarshal(bytes, &out); err != nil {
		t.Fatalf("protojson.Unmarshal: %v", err)
	}
	if len(out.FieldValues) != 3 {
		t.Errorf("field_values size lost: got %d, want 3", len(out.FieldValues))
	}
	for k, want := range map[string]string{"region": "nyc3", "name": "site-vpc", "ip_range": "10.10.0.0/16"} {
		if got := out.FieldValues[k]; got != want {
			t.Errorf("field_values[%q] = %q, want %q", k, got, want)
		}
	}
}

// TestAdminListResourcesOutput_ErrorField pins the discriminator
// tag-100 convention: outputs carry a `error` field at tag 100 so
// generic decoders can sniff for a non-empty error before consuming
// the typed payload.
func TestAdminListResourcesOutput_ErrorField(t *testing.T) {
	in := &adminpb.AdminListResourcesOutput{Error: "authz denied"}
	bytes, err := protojson.Marshal(in)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	var out adminpb.AdminListResourcesOutput
	if err := protojson.Unmarshal(bytes, &out); err != nil {
		t.Fatalf("protojson.Unmarshal: %v", err)
	}
	if out.Error != "authz denied" {
		t.Errorf("error lost: got %q", out.Error)
	}
	if len(out.Resources) != 0 {
		t.Errorf("resources should be empty on error response: got %v", out.Resources)
	}
}
