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
