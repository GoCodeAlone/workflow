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
	a := PlanAction{Action: "create", ResolvedConfigHash: "sha256:abc"}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var got PlanAction
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.ResolvedConfigHash != "sha256:abc" {
		t.Errorf("ResolvedConfigHash: got %q", got.ResolvedConfigHash)
	}
}
