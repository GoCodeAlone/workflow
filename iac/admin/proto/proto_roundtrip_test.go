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

// --- T5 mutation message round-trips ---

// TestAdminPlanInput_Roundtrip pins the AdminPlanInput wire shape:
// app_context + resource_filter survive protojson; evidence nested
// with authz_checked/authz_allowed/subject/granted_permissions.
func TestAdminPlanInput_Roundtrip(t *testing.T) {
	in := &adminpb.AdminPlanInput{
		AppContext:     "myapp",
		ResourceFilter: "infra.vpc",
		Evidence: &adminpb.AdminAuthzEvidence{
			AuthzChecked:       true,
			AuthzAllowed:       true,
			Subject:            "user:bob",
			GrantedPermissions: []string{"infra:read"},
		},
	}
	b, err := protojson.Marshal(in)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	var out adminpb.AdminPlanInput
	if err := protojson.Unmarshal(b, &out); err != nil {
		t.Fatalf("protojson.Unmarshal: %v", err)
	}
	if out.AppContext != "myapp" {
		t.Errorf("app_context lost: got %q", out.AppContext)
	}
	if out.ResourceFilter != "infra.vpc" {
		t.Errorf("resource_filter lost: got %q", out.ResourceFilter)
	}
	if out.Evidence == nil || out.Evidence.Subject != "user:bob" {
		t.Errorf("evidence/subject lost: %+v", out.Evidence)
	}
}

// TestAdminPlanOutput_Roundtrip checks plan_id, desired_hash, actions,
// plan_json and that error is reachable at tag 100.
func TestAdminPlanOutput_Roundtrip(t *testing.T) {
	planJSON := []byte(`{"actions":[{"type":"create"}]}`)
	in := &adminpb.AdminPlanOutput{
		PlanId:      "plan-abc",
		DesiredHash: "abc123",
		Actions: []*adminpb.AdminPlanAction{
			{ActionType: "create", ResourceName: "site-vpc", Type: "infra.vpc", ChangeSummary: "+vpc"},
		},
		PlanJson: planJSON,
	}
	b, err := protojson.Marshal(in)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	var out adminpb.AdminPlanOutput
	if err := protojson.Unmarshal(b, &out); err != nil {
		t.Fatalf("protojson.Unmarshal: %v", err)
	}
	if out.PlanId != "plan-abc" {
		t.Errorf("plan_id lost: got %q", out.PlanId)
	}
	if out.DesiredHash != "abc123" {
		t.Errorf("desired_hash lost: got %q", out.DesiredHash)
	}
	if len(out.Actions) != 1 || out.Actions[0].ActionType != "create" {
		t.Errorf("actions lost: %+v", out.Actions)
	}
	if out.Actions[0].ChangeSummary != "+vpc" {
		t.Errorf("change_summary lost: got %q", out.Actions[0].ChangeSummary)
	}
	if string(out.PlanJson) != string(planJSON) {
		t.Errorf("plan_json mangled: got %q want %q", out.PlanJson, planJSON)
	}

	// tag-100 error discriminator
	errOut := &adminpb.AdminPlanOutput{Error: "authz denied"}
	eb, _ := protojson.Marshal(errOut)
	var errRt adminpb.AdminPlanOutput
	if err := protojson.Unmarshal(eb, &errRt); err != nil {
		t.Fatalf("error roundtrip Unmarshal: %v", err)
	}
	if errRt.Error != "authz denied" {
		t.Errorf("AdminPlanOutput error tag-100 lost: got %q", errRt.Error)
	}
}

// TestAdminApplyInput_Roundtrip checks plan_id, desired_hash,
// allow_replace, app_context, and evidence survive protojson.
func TestAdminApplyInput_Roundtrip(t *testing.T) {
	in := &adminpb.AdminApplyInput{
		PlanId:       "plan-abc",
		DesiredHash:  "abc123",
		AllowReplace: []string{"site-vpc"},
		AppContext:   "myapp",
		Evidence: &adminpb.AdminAuthzEvidence{
			AuthzChecked: true,
			AuthzAllowed: true,
			Subject:      "user:operator",
		},
	}
	b, err := protojson.Marshal(in)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	var out adminpb.AdminApplyInput
	if err := protojson.Unmarshal(b, &out); err != nil {
		t.Fatalf("protojson.Unmarshal: %v", err)
	}
	if out.PlanId != "plan-abc" {
		t.Errorf("plan_id lost: got %q", out.PlanId)
	}
	if out.DesiredHash != "abc123" {
		t.Errorf("desired_hash lost: got %q", out.DesiredHash)
	}
	if len(out.AllowReplace) != 1 || out.AllowReplace[0] != "site-vpc" {
		t.Errorf("allow_replace lost: got %v", out.AllowReplace)
	}
	if out.AppContext != "myapp" {
		t.Errorf("app_context lost: got %q", out.AppContext)
	}
	if out.Evidence == nil || out.Evidence.Subject != "user:operator" {
		t.Errorf("evidence/subject lost: %+v", out.Evidence)
	}
}

// TestAdminApplyOutput_Roundtrip checks applied summaries, action errors,
// and that error tag-100 survives.
func TestAdminApplyOutput_Roundtrip(t *testing.T) {
	in := &adminpb.AdminApplyOutput{
		Applied: []*adminpb.AdminResourceSummary{
			{Name: "site-vpc", Type: "infra.vpc", Status: "active"},
		},
		Errors: []*adminpb.AdminActionError{
			{Resource: "db-main", Action: "create", Error: "timeout"},
		},
	}
	b, err := protojson.Marshal(in)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	var out adminpb.AdminApplyOutput
	if err := protojson.Unmarshal(b, &out); err != nil {
		t.Fatalf("protojson.Unmarshal: %v", err)
	}
	if len(out.Applied) != 1 || out.Applied[0].Name != "site-vpc" {
		t.Errorf("applied lost: %+v", out.Applied)
	}
	if len(out.Errors) != 1 || out.Errors[0].Resource != "db-main" {
		t.Errorf("errors lost: %+v", out.Errors)
	}
	if out.Errors[0].Error != "timeout" {
		t.Errorf("error field lost: got %q", out.Errors[0].Error)
	}

	// tag-100 discriminator
	errOut := &adminpb.AdminApplyOutput{Error: "stale plan"}
	eb, _ := protojson.Marshal(errOut)
	var errRt adminpb.AdminApplyOutput
	if err := protojson.Unmarshal(eb, &errRt); err != nil {
		t.Fatalf("error roundtrip: %v", err)
	}
	if errRt.Error != "stale plan" {
		t.Errorf("AdminApplyOutput error tag-100 lost: got %q", errRt.Error)
	}
}

// TestAdminDestroyInput_Roundtrip checks refs (AdminResourceRef),
// confirm_hash, and evidence survive protojson.
func TestAdminDestroyInput_Roundtrip(t *testing.T) {
	in := &adminpb.AdminDestroyInput{
		Refs: []*adminpb.AdminResourceRef{
			{Name: "old-vpc", Type: "infra.vpc"},
		},
		ConfirmHash: "hash-xyz",
		Evidence: &adminpb.AdminAuthzEvidence{
			AuthzChecked: true,
			AuthzAllowed: true,
			Subject:      "user:admin",
		},
	}
	b, err := protojson.Marshal(in)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	var out adminpb.AdminDestroyInput
	if err := protojson.Unmarshal(b, &out); err != nil {
		t.Fatalf("protojson.Unmarshal: %v", err)
	}
	if len(out.Refs) != 1 || out.Refs[0].Name != "old-vpc" {
		t.Errorf("refs lost: %+v", out.Refs)
	}
	if out.Refs[0].Type != "infra.vpc" {
		t.Errorf("ref.type lost: got %q", out.Refs[0].Type)
	}
	if out.ConfirmHash != "hash-xyz" {
		t.Errorf("confirm_hash lost: got %q", out.ConfirmHash)
	}
}

// TestAdminDestroyOutput_Roundtrip checks destroyed list, action errors,
// and error tag-100.
func TestAdminDestroyOutput_Roundtrip(t *testing.T) {
	in := &adminpb.AdminDestroyOutput{
		Destroyed: []string{"old-vpc"},
		Errors:    []*adminpb.AdminActionError{{Resource: "lb-main", Action: "destroy", Error: "in use"}},
	}
	b, err := protojson.Marshal(in)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	var out adminpb.AdminDestroyOutput
	if err := protojson.Unmarshal(b, &out); err != nil {
		t.Fatalf("protojson.Unmarshal: %v", err)
	}
	if len(out.Destroyed) != 1 || out.Destroyed[0] != "old-vpc" {
		t.Errorf("destroyed lost: %v", out.Destroyed)
	}
	if len(out.Errors) != 1 || out.Errors[0].Action != "destroy" {
		t.Errorf("errors lost: %+v", out.Errors)
	}

	errOut := &adminpb.AdminDestroyOutput{Error: "confirm_hash mismatch"}
	eb, _ := protojson.Marshal(errOut)
	var errRt adminpb.AdminDestroyOutput
	if err := protojson.Unmarshal(eb, &errRt); err != nil {
		t.Fatalf("error roundtrip: %v", err)
	}
	if errRt.Error != "confirm_hash mismatch" {
		t.Errorf("AdminDestroyOutput error tag-100 lost: got %q", errRt.Error)
	}
}

// TestAdminDriftInput_Roundtrip checks refs and evidence survive protojson.
func TestAdminDriftInput_Roundtrip(t *testing.T) {
	in := &adminpb.AdminDriftInput{
		Refs: []*adminpb.AdminResourceRef{
			{Name: "site-vpc", Type: "infra.vpc"},
		},
		Evidence: &adminpb.AdminAuthzEvidence{
			AuthzChecked: true,
			AuthzAllowed: true,
			Subject:      "user:viewer",
		},
	}
	b, err := protojson.Marshal(in)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	var out adminpb.AdminDriftInput
	if err := protojson.Unmarshal(b, &out); err != nil {
		t.Fatalf("protojson.Unmarshal: %v", err)
	}
	if len(out.Refs) != 1 || out.Refs[0].Name != "site-vpc" {
		t.Errorf("refs lost: %+v", out.Refs)
	}
	if out.Evidence == nil || out.Evidence.Subject != "user:viewer" {
		t.Errorf("evidence/subject lost: %+v", out.Evidence)
	}
}

// TestAdminDriftOutput_Roundtrip checks AdminDriftResult fields and
// error tag-100 survive protojson.
func TestAdminDriftOutput_Roundtrip(t *testing.T) {
	in := &adminpb.AdminDriftOutput{
		Drift: []*adminpb.AdminDriftResult{
			{
				ResourceName: "site-vpc",
				Type:         "infra.vpc",
				Drifted:      true,
				Class:        "config",
				Fields:       []string{"region", "ip_range"},
			},
		},
	}
	b, err := protojson.Marshal(in)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	var out adminpb.AdminDriftOutput
	if err := protojson.Unmarshal(b, &out); err != nil {
		t.Fatalf("protojson.Unmarshal: %v", err)
	}
	if len(out.Drift) != 1 || out.Drift[0].ResourceName != "site-vpc" {
		t.Errorf("drift lost: %+v", out.Drift)
	}
	if !out.Drift[0].Drifted {
		t.Errorf("drifted bool lost")
	}
	if out.Drift[0].Class != "config" {
		t.Errorf("class lost: got %q", out.Drift[0].Class)
	}
	if len(out.Drift[0].Fields) != 2 || out.Drift[0].Fields[0] != "region" {
		t.Errorf("fields lost: %v", out.Drift[0].Fields)
	}

	errOut := &adminpb.AdminDriftOutput{Error: "provider unavailable"}
	eb, _ := protojson.Marshal(errOut)
	var errRt adminpb.AdminDriftOutput
	if err := protojson.Unmarshal(eb, &errRt); err != nil {
		t.Fatalf("error roundtrip: %v", err)
	}
	if errRt.Error != "provider unavailable" {
		t.Errorf("AdminDriftOutput error tag-100 lost: got %q", errRt.Error)
	}
}

// TestMutationOutputs_DiscardUnknown verifies that all mutation Output
// messages accept payloads with extra (future) fields when parsed with
// DiscardUnknown:true — the server uses this option (unmarshalOpts in
// module/infra_admin.go) for forward compatibility.
func TestMutationOutputs_DiscardUnknown(t *testing.T) {
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	extraPayload := []byte(`{"unknownField":"ignored","plan_id":"p1","desired_hash":"h1"}`)
	var planOut adminpb.AdminPlanOutput
	if err := opts.Unmarshal(extraPayload, &planOut); err != nil {
		t.Errorf("AdminPlanOutput DiscardUnknown: %v", err)
	}
	if planOut.PlanId != "p1" {
		t.Errorf("plan_id not read through DiscardUnknown: got %q", planOut.PlanId)
	}

	applyPayload := []byte(`{"unknownField":"ignored","error":"denied"}`)
	var applyOut adminpb.AdminApplyOutput
	if err := opts.Unmarshal(applyPayload, &applyOut); err != nil {
		t.Errorf("AdminApplyOutput DiscardUnknown: %v", err)
	}
	if applyOut.Error != "denied" {
		t.Errorf("error not read through DiscardUnknown: got %q", applyOut.Error)
	}

	destroyPayload := []byte(`{"unknownField":"ignored","destroyed":["r1"]}`)
	var destroyOut adminpb.AdminDestroyOutput
	if err := opts.Unmarshal(destroyPayload, &destroyOut); err != nil {
		t.Errorf("AdminDestroyOutput DiscardUnknown: %v", err)
	}
	if len(destroyOut.Destroyed) != 1 {
		t.Errorf("destroyed not read through DiscardUnknown: %v", destroyOut.Destroyed)
	}

	driftPayload := []byte(`{"unknownField":"ignored","error":"timeout"}`)
	var driftOut adminpb.AdminDriftOutput
	if err := opts.Unmarshal(driftPayload, &driftOut); err != nil {
		t.Errorf("AdminDriftOutput DiscardUnknown: %v", err)
	}
	if driftOut.Error != "timeout" {
		t.Errorf("error not read through DiscardUnknown: got %q", driftOut.Error)
	}
}
