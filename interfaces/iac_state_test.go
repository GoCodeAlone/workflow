package interfaces

import (
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
