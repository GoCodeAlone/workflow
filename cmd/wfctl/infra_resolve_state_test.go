package main

import (
	"reflect"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestResolveSpecsAgainstState_ResolvesModuleFieldRefs(t *testing.T) {
	specs := []interfaces.ResourceSpec{{
		Name:   "pg",
		Type:   "infra.droplet",
		Config: map[string]any{"vpc_uuid": "${vpc.id}"},
	}}
	current := []interfaces.ResourceState{{
		Name: "vpc", Type: "infra.vpc", ProviderID: "14badc41-1234",
		Outputs: map[string]any{"id": "14badc41-1234"},
	}}
	cfg := &config.WorkflowConfig{}

	out, diags, err := resolveSpecsAgainstState(specs, current, cfg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out[0].Config["vpc_uuid"]; got != "14badc41-1234" {
		t.Errorf("vpc_uuid: got %v, want resolved", got)
	}
	if len(diags) != 0 {
		t.Errorf("unexpected diagnostics: %v", diags)
	}
}

func TestResolveSpecsAgainstState_ResolvesInfraOutputSecrets(t *testing.T) {
	specs := []interfaces.ResourceSpec{{
		Name:   "pg",
		Type:   "infra.droplet",
		Config: map[string]any{"vpc_uuid": "${STAGING_VPC_UUID}"},
	}}
	current := []interfaces.ResourceState{{
		Name: "core-dump-vpc", Type: "infra.vpc", ProviderID: "14badc41-1234",
		Outputs: map[string]any{"id": "14badc41-1234"},
	}}
	cfg := &config.WorkflowConfig{
		Secrets: &config.SecretsConfig{
			Generate: []config.SecretGen{
				{Key: "STAGING_VPC_UUID", Type: "infra_output", Source: "core-dump-vpc.id"},
			},
		},
	}

	out, _, err := resolveSpecsAgainstState(specs, current, cfg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out[0].Config["vpc_uuid"]; got != "14badc41-1234" {
		t.Errorf("vpc_uuid: got %v, want resolved through infra_output secret", got)
	}
}

func TestResolveSpecsAgainstState_LeavesUnresolvedVerbatim(t *testing.T) {
	specs := []interfaces.ResourceSpec{{
		Name:   "pg",
		Type:   "infra.droplet",
		Config: map[string]any{"vpc_uuid": "${BRAND_NEW.id}"},
	}}
	out, diags, err := resolveSpecsAgainstState(specs, nil, &config.WorkflowConfig{}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out[0].Config["vpc_uuid"]; got != "${BRAND_NEW.id}" {
		t.Errorf("got %q, want preserved template", got)
	}
	if len(diags) != 1 || diags[0].Ref != "BRAND_NEW.id" {
		t.Errorf("expected one diag for BRAND_NEW.id, got %+v", diags)
	}
}

func TestResolveSpecsAgainstState_DesiredHashStable(t *testing.T) {
	specs := []interfaces.ResourceSpec{{
		Name: "pg", Type: "infra.droplet",
		Config: map[string]any{"vpc_uuid": "${vpc.id}"},
	}}
	current := []interfaces.ResourceState{{
		Name: "vpc", Type: "infra.vpc",
		Outputs: map[string]any{"id": "14badc41"},
	}}
	out1, _, _ := resolveSpecsAgainstState(specs, current, &config.WorkflowConfig{}, "")
	out2, _, _ := resolveSpecsAgainstState(specs, current, &config.WorkflowConfig{}, "")
	if !reflect.DeepEqual(out1, out2) {
		t.Errorf("resolution must be deterministic across runs")
	}
}

func TestResolveSpecsAgainstState_HashByteStable(t *testing.T) {
	specs := []interfaces.ResourceSpec{{
		Name: "pg", Type: "infra.droplet",
		Config: map[string]any{
			"vpc_uuid": "${vpc.id}",
			"tags":     []any{"a", "b"},
			"size":     "s-1vcpu-2gb",
		},
	}}
	current := []interfaces.ResourceState{{
		Name: "vpc", Type: "infra.vpc",
		Outputs: map[string]any{"id": "14badc41"},
	}}
	cfg := &config.WorkflowConfig{}

	var hashes []string
	for i := 0; i < 5; i++ {
		out, _, err := resolveSpecsAgainstState(specs, current, cfg, "")
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		h := desiredStateHash(out)
		hashes = append(hashes, h)
	}
	for i := 1; i < len(hashes); i++ {
		if hashes[i] != hashes[0] {
			t.Errorf("hash drift at iter %d: %q vs %q", i, hashes[i], hashes[0])
		}
	}
}

func TestResolveSpecsAgainstState_NilCfg(t *testing.T) {
	specs := []interfaces.ResourceSpec{{
		Name: "x", Type: "infra.droplet",
		Config: map[string]any{"k": "literal"},
	}}
	out, diags, err := resolveSpecsAgainstState(specs, nil, nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out[0].Config["k"]; got != "literal" {
		t.Errorf("k: got %q, want literal", got)
	}
	if len(diags) != 0 {
		t.Errorf("unexpected diags: %v", diags)
	}
}
