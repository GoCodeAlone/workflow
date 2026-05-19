package interfaces

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestIaCPlan_SchemaVersionField(t *testing.T) {
	p := IaCPlan{SchemaVersion: 2}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var got IaCPlan
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 2 {
		t.Errorf("SchemaVersion roundtrip: got %d want 2", got.SchemaVersion)
	}
}

func TestIaCPlan_InputSnapshotField(t *testing.T) {
	p := IaCPlan{InputSnapshot: map[string]string{"FOO": "deadbeefcafebabe"}}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var got IaCPlan
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.InputSnapshot["FOO"] != "deadbeefcafebabe" {
		t.Errorf("InputSnapshot roundtrip failed: %v", got.InputSnapshot)
	}
}

func TestIaCPlan_IncludeField(t *testing.T) {
	p := IaCPlan{Include: []string{"bmw-dns"}}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var got IaCPlan
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Include) != 1 || got.Include[0] != "bmw-dns" {
		t.Errorf("Include roundtrip failed: %v", got.Include)
	}
}

func TestPlanAction_ResolvedConfigHashField(t *testing.T) {
	// platform.ConfigHash returns a lower-case hex sha256 digest with no
	// "sha256:" prefix; use a realistic 64-hex value so the test's expected
	// shape matches the on-disk format and won't mislead a future validator.
	const realisticHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	a := PlanAction{Action: "create", ResolvedConfigHash: realisticHash}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var got PlanAction
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.ResolvedConfigHash != realisticHash {
		t.Errorf("ResolvedConfigHash: got %q want %q", got.ResolvedConfigHash, realisticHash)
	}
}

// TestApplyResult_InputDriftReport_RoundTrip verifies the InputDriftReport
// field declared in T3.0.4. Field is populated by the deferred postcondition
// in wfctlhelpers.ApplyPlan (T3.1.5).
func TestApplyResult_InputDriftReport_RoundTrip(t *testing.T) {
	r := ApplyResult{InputDriftReport: []DriftEntry{
		{Name: "STAGING_PG_PASSWORD", PlanFingerprint: "abc", ApplyFingerprint: "def"},
	}}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var got ApplyResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.InputDriftReport) != 1 || got.InputDriftReport[0].Name != "STAGING_PG_PASSWORD" {
		t.Errorf("InputDriftReport roundtrip failed: %+v", got)
	}
}

// TestApplyResult_InitialInputSnapshot_RoundTrip verifies the
// InitialInputSnapshot field declared in T3.0.4. Field is populated at apply
// entry by wfctlhelpers.ApplyPlan (T3.1) and consumed by the deferred
// postcondition (T3.1.5) when computing drift.
func TestApplyResult_InitialInputSnapshot_RoundTrip(t *testing.T) {
	r := ApplyResult{InitialInputSnapshot: map[string]string{"FOO": "fp1234"}}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var got ApplyResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.InitialInputSnapshot["FOO"] != "fp1234" {
		t.Errorf("InitialInputSnapshot roundtrip failed: %+v", got)
	}
}

// TestApplyResult_ReplaceIDMap_RoundTrip verifies the ReplaceIDMap field
// declared in T3.0.4. Field is populated by doReplace (T3.4) and consumed
// by JIT substitution in W-5 (T5.2/T5.3).
func TestApplyResult_ReplaceIDMap_RoundTrip(t *testing.T) {
	r := ApplyResult{ReplaceIDMap: map[string]string{"vpc": "new-uuid"}}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var got ApplyResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.ReplaceIDMap["vpc"] != "new-uuid" {
		t.Errorf("ReplaceIDMap roundtrip failed: %+v", got)
	}
}

// TestApplyResult_OmitEmptyContract locks in the omitempty JSON tag
// behavior on the three T3.0.4 fields. Both nil and empty-but-non-nil
// values must drop from the encoded form so plan/result transcripts stay
// lean and downstream consumers can treat "absent key" and "empty value"
// identically — matching the behavior already documented for
// IaCPlan.InputSnapshot and PlanAction.ResolvedConfigHash.
func TestApplyResult_OmitEmptyContract(t *testing.T) {
	cases := map[string]ApplyResult{
		"nil-fields": {},
		"empty-non-nil-fields": {
			InitialInputSnapshot: map[string]string{},
			InputDriftReport:     []DriftEntry{},
			ReplaceIDMap:         map[string]string{},
		},
	}
	for name, r := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			s := string(data)
			for _, key := range []string{"initial_input_snapshot", "input_drift_report", "replace_id_map"} {
				if containsString(s, key) {
					t.Errorf("expected %q to be omitted from %s; got %s", key, name, s)
				}
			}
		})
	}
}

// containsString is a tiny, dependency-free substring helper local to this
// test file so the omitempty test does not pull in strings just for one
// check (the file's other tests use only encoding/json + testing).
func containsString(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestResourceState_AppliedConfigSourceJSONRoundtrip(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"apply provenance", "apply"},
		{"adoption provenance", "adoption"},
		{"empty (legacy state)", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := ResourceState{Name: "x", Type: "infra.foo", AppliedConfigSource: tc.src}
			data, err := json.Marshal(in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var out ResourceState
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if out.AppliedConfigSource != tc.src {
				t.Errorf("source: got %q, want %q", out.AppliedConfigSource, tc.src)
			}
		})
	}
}

func TestResourceState_AppliedConfigSourceOmitemptyWhenLegacy(t *testing.T) {
	// Legacy state: AppliedConfigSource not set. JSON output must omit
	// the field so old state-store readers don't trip on unknown keys.
	in := ResourceState{Name: "x", Type: "infra.foo"}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(data, []byte("applied_config_source")) {
		t.Errorf("legacy state should omit applied_config_source; got %s", data)
	}
}

// TestResourceState_OldReaderTolerates_NewWriter pins state-store roundtrip
// compat: state JSON written by NEW code (with applied_config_source field)
// MUST still decode without error in OLD code (which has no AppliedConfigSource
// field). Go's encoding/json silently ignores unknown fields by default; this
// test pins that contract.
func TestResourceState_OldReaderTolerates_NewWriter(t *testing.T) {
	in := ResourceState{Name: "x", Type: "infra.foo", AppliedConfigSource: "apply"}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// "Old" reader = a struct with NO AppliedConfigSource field.
	type oldResourceState struct {
		Name          string         `json:"name"`
		Type          string         `json:"type"`
		AppliedConfig map[string]any `json:"applied_config"`
	}
	var old oldResourceState
	if err := json.Unmarshal(data, &old); err != nil {
		t.Fatalf("old reader rejected new state: %v", err)
	}
	if old.Name != "x" {
		t.Errorf("old reader: Name corrupted; got %q", old.Name)
	}
}

// TestResourceState_NewReaderTolerates_OldWriter: legacy state with NO
// applied_config_source key must decode cleanly in new code with the field
// defaulted to empty string (which downstream code treats as "adoption" per
// ADR 0010, conservative default).
func TestResourceState_NewReaderTolerates_OldWriter(t *testing.T) {
	legacyJSON := []byte(`{"name":"x","type":"infra.foo","applied_config":{"k":"v"}}`)
	var out ResourceState
	if err := json.Unmarshal(legacyJSON, &out); err != nil {
		t.Fatalf("new reader rejected legacy state: %v", err)
	}
	if out.AppliedConfigSource != "" {
		t.Errorf("AppliedConfigSource on legacy state: got %q, want empty", out.AppliedConfigSource)
	}
}

// TestActionStatus_ZeroValueIsUnspecified pins the zero-value semantics of
// ActionStatus: an uninitialized status MUST be ActionStatusUnspecified so
// the engine-side populate path catches forgotten populates. Per
// workflow#640 Phase 2 + ADR 0040 invariant 2. (The proto-side
// applyResultFromPB decode path was deleted per workflow#699.)
func TestActionStatus_ZeroValueIsUnspecified(t *testing.T) {
	var s ActionStatus
	if s != ActionStatusUnspecified {
		t.Fatalf("zero-value ActionStatus: got %d, want ActionStatusUnspecified (0)", s)
	}
}

// TestActionStatus_ConstantValues pins the wire tags 0/1/2/3 to the four
// declared constants. Mirrors pb.ActionStatus values; drift would cause
// the engine-side populate to mis-categorize outcomes. (The proto-side
// applyResultFromPB decode path was deleted per workflow#699.)
func TestActionStatus_ConstantValues(t *testing.T) {
	cases := []struct {
		name string
		got  ActionStatus
		want uint8
	}{
		{"Unspecified", ActionStatusUnspecified, 0},
		{"Success", ActionStatusSuccess, 1},
		{"Error", ActionStatusError, 2},
		{"DeleteFailed", ActionStatusDeleteFailed, 3},
	}
	for _, c := range cases {
		if uint8(c.got) != c.want {
			t.Errorf("ActionStatus%s = %d, want %d", c.name, uint8(c.got), c.want)
		}
	}
}

// TestActionOutcome_ZeroValueAndConstructor verifies ActionOutcome has the
// three documented fields with the expected types and zero values. A bare
// ActionOutcome{} has ActionIndex=0, Status=ActionStatusUnspecified, Error="".
func TestActionOutcome_ZeroValueAndConstructor(t *testing.T) {
	var o ActionOutcome
	if o.ActionIndex != 0 || o.Status != ActionStatusUnspecified || o.Error != "" {
		t.Fatalf("zero-value ActionOutcome: got %+v, want all zero", o)
	}
	o2 := ActionOutcome{ActionIndex: 7, Status: ActionStatusDeleteFailed, Error: "in-use"}
	if o2.ActionIndex != 7 || o2.Status != ActionStatusDeleteFailed || o2.Error != "in-use" {
		t.Fatalf("constructed ActionOutcome: got %+v", o2)
	}
}

// TestApplyResult_Actions_RoundTrip verifies the new Actions field on
// ApplyResult survives JSON marshal/unmarshal — wfctl persists ApplyResult
// JSON in apply-state files (iac/applystate), so wire-format drift here
// breaks resume / replay.
func TestApplyResult_Actions_RoundTrip(t *testing.T) {
	r := ApplyResult{Actions: []ActionOutcome{
		{ActionIndex: 0, Status: ActionStatusSuccess},
		{ActionIndex: 1, Status: ActionStatusError, Error: "boom"},
	}}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var got ApplyResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Actions) != 2 {
		t.Fatalf("Actions len: got %d, want 2", len(got.Actions))
	}
	if got.Actions[0].Status != ActionStatusSuccess || got.Actions[1].Error != "boom" {
		t.Errorf("Actions round-trip mismatch: %+v", got.Actions)
	}
}

// TestApplyResult_Actions_OmitemptyWhenNil ensures a nil Actions slice
// is absent from the JSON output, matching the omitempty convention used
// by Errors / InputDriftReport / ReplaceIDMap. Plugins on v1 capability
// shim emit no actions; the JSON must not carry an empty array.
func TestApplyResult_Actions_OmitemptyWhenNil(t *testing.T) {
	r := ApplyResult{PlanID: "p"}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(`"actions"`)) {
		t.Errorf("nil Actions emitted in JSON: %s", data)
	}
}
