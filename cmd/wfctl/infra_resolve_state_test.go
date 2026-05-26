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

// TestBuildRuntimeOnlySecretKeys verifies that buildRuntimeOnlySecretKeys
// includes only non-infra_output secret keys.
func TestBuildRuntimeOnlySecretKeys(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Secrets: &config.SecretsConfig{
			Generate: []config.SecretGen{
				{Key: "STAGING_VPC_UUID", Type: "infra_output", Source: "core-dump-vpc.id"},
				{Key: "NATS_AUTH_TOKEN", Type: "random_hex", Length: 32},
				{Key: "SESSION_SECRET", Type: "random_base64", Length: 48},
				{Key: "SPACES_access_key", Type: "provider_credential", Source: "digitalocean.spaces"},
			},
			Entries: []config.SecretEntry{
				{Name: "STRIPE_KEY"},
			},
		},
		Modules: []config.ModuleConfig{
			{
				Name: "required-secrets",
				Type: "secrets.requires",
				Config: map[string]any{
					"requires": []any{
						map[string]any{"key": "WFCOMPUTE_VALIDATION_TOKEN"},
					},
				},
			},
			{
				Name: "generated-secrets",
				Type: "secrets.generate",
				Config: map[string]any{
					"generate": []any{
						map[string]any{"key": "MODULE_RANDOM", "type": "random_hex"},
						map[string]any{"key": "MODULE_INFRA_OUTPUT", "type": "infra_output"},
					},
				},
			},
		},
	}
	keys := buildRuntimeOnlySecretKeys(cfg)
	// infra_output must NOT be in the blocklist.
	if _, ok := keys["STAGING_VPC_UUID"]; ok {
		t.Errorf("STAGING_VPC_UUID (infra_output) must NOT be in runtime-only keys")
	}
	if _, ok := keys["MODULE_INFRA_OUTPUT"]; ok {
		t.Errorf("MODULE_INFRA_OUTPUT (infra_output) must NOT be in runtime-only keys")
	}
	// Non-infra_output types must be in the blocklist.
	for _, wantKey := range []string{"NATS_AUTH_TOKEN", "SESSION_SECRET", "SPACES_access_key", "STRIPE_KEY", "WFCOMPUTE_VALIDATION_TOKEN", "MODULE_RANDOM"} {
		if _, ok := keys[wantKey]; !ok {
			t.Errorf("%s must be in runtime-only keys", wantKey)
		}
	}
}

// TestPlanTimeEnvLookup_BlocksRuntimeOnlyKeys verifies that planTimeEnvLookup
// returns not-found for keys in runtimeOnlyKeys, even when they are present in
// the resolvedSecrets map (resolvedSecrets should not contain non-infra_output
// keys, but defense-in-depth: the blocklist wins). This is the ADR-0014 contract.
func TestPlanTimeEnvLookup_BlocksRuntimeOnlyKeys(t *testing.T) {
	resolvedSecrets := map[string]string{
		"STAGING_VPC_UUID": "14badc41-1234", // infra_output — resolves
	}
	runtimeOnlyKeys := map[string]struct{}{
		"NATS_AUTH_TOKEN": {},
	}
	lookup := planTimeEnvLookup(resolvedSecrets, runtimeOnlyKeys)

	// infra_output key should resolve.
	if val, ok := lookup("STAGING_VPC_UUID"); !ok || val != "14badc41-1234" {
		t.Errorf("STAGING_VPC_UUID: got (%q, %v), want (14badc41-1234, true)", val, ok)
	}
	// runtime-only key should be blocked (return not-found).
	if _, ok := lookup("NATS_AUTH_TOKEN"); ok {
		t.Errorf("NATS_AUTH_TOKEN (runtime-only) must not resolve at plan time")
	}
}

// TestResolveSpecsAgainstState_RuntimeOnlySecretNotSubstituted verifies the
// end-to-end ADR-0014 contract: a ${NATS_AUTH_TOKEN} reference in env_vars
// is left untouched (not resolved via os.LookupEnv) when NATS_AUTH_TOKEN is
// declared as a random_hex secret in secrets.generate. This prevents
// security-check R4 from flagging the literal value in the plan.
func TestResolveSpecsAgainstState_RuntimeOnlySecretNotSubstituted(t *testing.T) {
	specs := []interfaces.ResourceSpec{{
		Name: "coredump-nats-staging",
		Type: "infra.container_service",
		Config: map[string]any{
			"env_vars": map[string]any{
				"NATS_AUTH_TOKEN": "${NATS_AUTH_TOKEN}",
			},
		},
	}}
	cfg := &config.WorkflowConfig{
		Secrets: &config.SecretsConfig{
			Generate: []config.SecretGen{
				{Key: "NATS_AUTH_TOKEN", Type: "random_hex", Length: 32},
			},
		},
	}
	// Even with no state, the template must be preserved (not resolved).
	out, diags, err := resolveSpecsAgainstState(specs, nil, cfg, "staging")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	envVars, _ := out[0].Config["env_vars"].(map[string]any)
	if val := envVars["NATS_AUTH_TOKEN"]; val != "${NATS_AUTH_TOKEN}" {
		t.Errorf("NATS_AUTH_TOKEN: got %q, want preserved template — random_hex must not resolve at plan time (ADR-0014)", val)
	}
	// The unresolved ref should appear in diagnostics.
	if len(diags) != 1 || diags[0].Ref != "NATS_AUTH_TOKEN" {
		t.Errorf("diags: got %+v, want one entry for NATS_AUTH_TOKEN", diags)
	}
}

func TestResolveSpecsAgainstState_RequiredSecretNotSubstituted(t *testing.T) {
	t.Setenv("WFCOMPUTE_VALIDATION_TOKEN", "literal-token-that-must-not-enter-plan")
	specs := []interfaces.ResourceSpec{{
		Name: "bmw-staging",
		Type: "infra.container_service",
		Config: map[string]any{
			"env_vars": map[string]any{
				"WFCOMPUTE_VALIDATION_TOKEN": "${WFCOMPUTE_VALIDATION_TOKEN}",
			},
		},
	}}
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{{
			Name: "bmw-required-secrets",
			Type: "secrets.requires",
			Config: map[string]any{
				"requires": []any{
					map[string]any{"key": "WFCOMPUTE_VALIDATION_TOKEN"},
				},
			},
		}},
	}

	out, diags, err := resolveSpecsAgainstState(specs, nil, cfg, "staging")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	envVars, _ := out[0].Config["env_vars"].(map[string]any)
	if val := envVars["WFCOMPUTE_VALIDATION_TOKEN"]; val != "${WFCOMPUTE_VALIDATION_TOKEN}" {
		t.Errorf("WFCOMPUTE_VALIDATION_TOKEN: got %q, want preserved template", val)
	}
	if len(diags) != 1 || diags[0].Ref != "WFCOMPUTE_VALIDATION_TOKEN" {
		t.Errorf("diags: got %+v, want one entry for WFCOMPUTE_VALIDATION_TOKEN", diags)
	}
}
