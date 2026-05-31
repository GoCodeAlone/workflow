package cigen

import (
	"encoding/json"
	"testing"
)

func TestDeployPhase_ScopedSecretsJSON(t *testing.T) {
	p := DeployPhase{
		Name:       "prereq",
		ConfigPath: "deploy.prereq.yaml",
		Secrets:    []SecretRef{{Name: "DIGITALOCEAN_TOKEN"}},
		Scoped:     true,
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got DeployPhase
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.Scoped || len(got.Secrets) != 1 || got.Secrets[0].Name != "DIGITALOCEAN_TOKEN" {
		t.Fatalf("round-trip lost fields: %+v", got)
	}
	// Unscoped phase with no secrets must omit both in JSON (additive, back-compat).
	b2, _ := json.Marshal(DeployPhase{Name: "deploy", ConfigPath: "deploy.yaml"})
	if string(b2) != `{"name":"deploy","config_path":"deploy.yaml"}` {
		t.Fatalf("unexpected JSON for unscoped phase: %s", b2)
	}
}
