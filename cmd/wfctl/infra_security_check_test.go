package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func loadFixturePlan(t *testing.T, name string) interfaces.IaCPlan {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "security", name))
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	var plan interfaces.IaCPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return plan
}

func defaultOpts() SecurityCheckOpts {
	return SecurityCheckOpts{MaxChanges: 20}
}

func findingsForRule(findings []SecurityFinding, ruleID string) []SecurityFinding {
	var out []SecurityFinding
	for _, f := range findings {
		if f.RuleID == ruleID {
			out = append(out, f)
		}
	}
	return out
}

func requireFinding(t *testing.T, findings []SecurityFinding, ruleID string, wantSev SecuritySeverity) {
	t.Helper()
	for _, f := range findings {
		if f.RuleID == ruleID && f.Severity == wantSev {
			return
		}
	}
	t.Errorf("expected %s %s finding, got: %+v", wantSev, ruleID, findings)
}

func requireNoFinding(t *testing.T, findings []SecurityFinding, ruleID string) {
	t.Helper()
	for _, f := range findings {
		if f.RuleID == ruleID {
			t.Errorf("unexpected %s finding: %+v", ruleID, f)
		}
	}
}

// ── R1: Firewall over-permissive sources ─────────────────────────────────────

func TestInfraSecurityCheck_R1_Trigger(t *testing.T) {
	plan := loadFixturePlan(t, "r1_trigger.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireFinding(t, findings, "R1", SeverityFail)
}

func TestInfraSecurityCheck_R1_Pass_Port80(t *testing.T) {
	plan := loadFixturePlan(t, "r1_pass.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireNoFinding(t, findings, "R1")
}

func TestInfraSecurityCheck_R1_IPv6Blocked(t *testing.T) {
	plan := interfaces.IaCPlan{
		ID: "ipv6-test",
		Actions: []interfaces.PlanAction{{
			Action: "create",
			Resource: interfaces.ResourceSpec{
				Name: "fw", Type: "infra.firewall",
				Config: map[string]any{
					"inbound_rules": []any{
						map[string]any{"ports": "3306", "sources": []any{"::/0"}},
					},
				},
			},
		}},
		CreatedAt: time.Now().UTC(),
	}
	findings := RunSecurityCheck(plan, defaultOpts())
	requireFinding(t, findings, "R1", SeverityFail)
}

// ── R2: DB without trusted_sources ───────────────────────────────────────────

func TestInfraSecurityCheck_R2_Trigger_NoTrustedSources(t *testing.T) {
	plan := loadFixturePlan(t, "r2_trigger.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireFinding(t, findings, "R2", SeverityFail)
}

func TestInfraSecurityCheck_R2_Trigger_OpenCIDR(t *testing.T) {
	plan := interfaces.IaCPlan{
		ID: "r2-open",
		Actions: []interfaces.PlanAction{{
			Action: "create",
			Resource: interfaces.ResourceSpec{
				Name: "app-db", Type: "infra.database",
				Config: map[string]any{
					"trusted_sources": []any{"0.0.0.0/0"},
				},
			},
		}},
		CreatedAt: time.Now().UTC(),
	}
	findings := RunSecurityCheck(plan, defaultOpts())
	requireFinding(t, findings, "R2", SeverityFail)
}

func TestInfraSecurityCheck_R2_Pass(t *testing.T) {
	plan := loadFixturePlan(t, "r2_pass.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireNoFinding(t, findings, "R2")
}

// ── R3: Internal naming publicly exposed ─────────────────────────────────────

func TestInfraSecurityCheck_R3_Trigger(t *testing.T) {
	plan := loadFixturePlan(t, "r3_trigger.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireFinding(t, findings, "R3", SeverityFail)
}

func TestInfraSecurityCheck_R3_Pass_NoHTTPPort(t *testing.T) {
	plan := loadFixturePlan(t, "r3_pass.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireNoFinding(t, findings, "R3")
}

func TestInfraSecurityCheck_R3_Pass_PublicName(t *testing.T) {
	plan := interfaces.IaCPlan{
		ID: "r3-public",
		Actions: []interfaces.PlanAction{{
			Action: "create",
			Resource: interfaces.ResourceSpec{
				Name: "web-api", Type: "infra.container_service",
				Config: map[string]any{"http_port": "8080"},
			},
		}},
		CreatedAt: time.Now().UTC(),
	}
	findings := RunSecurityCheck(plan, defaultOpts())
	requireNoFinding(t, findings, "R3")
}

// ── R4: Secret-shape literal in env_vars ─────────────────────────────────────

// TestInfraSecurityCheck_R4_Trigger_GenericSecret verifies that a 40+ character
// base64-like literal in an env_var whose key contains "SECRET" triggers R4.
// The fixture uses a non-service-specific pattern to keep this file clean of
// real or plausible credential shapes.
func TestInfraSecurityCheck_R4_Trigger_GenericSecret(t *testing.T) {
	plan := loadFixturePlan(t, "r4_trigger.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireFinding(t, findings, "R4", SeverityFail)
}

func TestInfraSecurityCheck_R4_Trigger_AWSKey(t *testing.T) {
	plan := interfaces.IaCPlan{
		ID: "r4-aws",
		Actions: []interfaces.PlanAction{{
			Action: "create",
			Resource: interfaces.ResourceSpec{
				Name: "worker", Type: "infra.container_service",
				Config: map[string]any{
					"env_vars": map[string]any{
						"AWS_ACCESS_KEY_ID": "AKIAIOSFODNN7EXAMPLE",
					},
				},
			},
		}},
		CreatedAt: time.Now().UTC(),
	}
	findings := RunSecurityCheck(plan, defaultOpts())
	requireFinding(t, findings, "R4", SeverityFail)
}

func TestInfraSecurityCheck_R4_Pass_EnvVarRef(t *testing.T) {
	plan := loadFixturePlan(t, "r4_pass.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireNoFinding(t, findings, "R4")
}

// ── R5: Replace on protected resource ────────────────────────────────────────

func TestInfraSecurityCheck_R5_Trigger(t *testing.T) {
	plan := loadFixturePlan(t, "r5_trigger.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireFinding(t, findings, "R5", SeverityFail)
}

func TestInfraSecurityCheck_R5_Pass_NotProtected(t *testing.T) {
	plan := loadFixturePlan(t, "r5_pass.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireNoFinding(t, findings, "R5")
}

// ── R6: DB backup/encryption regression ──────────────────────────────────────

func TestInfraSecurityCheck_R6_Trigger_BackupsOff(t *testing.T) {
	plan := loadFixturePlan(t, "r6_trigger.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireFinding(t, findings, "R6", SeverityFail)
}

func TestInfraSecurityCheck_R6_Trigger_EncryptionOff(t *testing.T) {
	plan := interfaces.IaCPlan{
		ID: "r6-enc",
		Actions: []interfaces.PlanAction{{
			Action: "update",
			Resource: interfaces.ResourceSpec{
				Name: "app-db", Type: "infra.database",
				Config: map[string]any{"at_rest_encryption": false},
			},
			Current: &interfaces.ResourceState{
				Name: "app-db", Type: "infra.database",
				AppliedConfig: map[string]any{"at_rest_encryption": true},
			},
		}},
		CreatedAt: time.Now().UTC(),
	}
	findings := RunSecurityCheck(plan, defaultOpts())
	requireFinding(t, findings, "R6", SeverityFail)
}

func TestInfraSecurityCheck_R6_Pass(t *testing.T) {
	plan := loadFixturePlan(t, "r6_pass.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireNoFinding(t, findings, "R6")
}

// ── R7: CIDR widening ─────────────────────────────────────────────────────────

func TestInfraSecurityCheck_R7_Trigger_Warn(t *testing.T) {
	plan := loadFixturePlan(t, "r7_trigger.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireFinding(t, findings, "R7", SeverityWarn)
	// Verify the finding targets the expected resource (uses findingsForRule).
	r7 := findingsForRule(findings, "R7")
	if len(r7) == 0 {
		t.Fatal("expected at least one R7 finding")
	}
	if r7[0].Resource == "" {
		t.Errorf("R7 finding has empty resource name")
	}
}

func TestInfraSecurityCheck_R7_StrictCIDR_Fail(t *testing.T) {
	plan := loadFixturePlan(t, "r7_trigger.json")
	opts := defaultOpts()
	opts.StrictCIDR = true
	findings := RunSecurityCheck(plan, opts)
	requireFinding(t, findings, "R7", SeverityFail)
}

func TestInfraSecurityCheck_R7_Pass(t *testing.T) {
	plan := loadFixturePlan(t, "r7_pass.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireNoFinding(t, findings, "R7")
}

// TestInfraSecurityCheck_R7_Trigger_PrefixBroadened verifies the principal
// attack vector: replacing 10.0.0.0/24 with 10.0.0.0/8 (same CIDR string count,
// but /8 is a supernet that allows 16× more IPs).
func TestInfraSecurityCheck_R7_Trigger_PrefixBroadened(t *testing.T) {
	plan := loadFixturePlan(t, "r7_trigger_broadened.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireFinding(t, findings, "R7", SeverityWarn)
}

// TestInfraSecurityCheck_R7_Pass_PrefixNarrowed is the false-positive guard:
// replacing 10.0.0.0/8 with 10.0.0.0/24 is a restriction, not a widening.
func TestInfraSecurityCheck_R7_Pass_PrefixNarrowed(t *testing.T) {
	plan := loadFixturePlan(t, "r7_pass_narrowed.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireNoFinding(t, findings, "R7")
}

// ── R8: State bucket public ACL ───────────────────────────────────────────────

func TestInfraSecurityCheck_R8_Trigger(t *testing.T) {
	plan := loadFixturePlan(t, "r8_trigger.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireFinding(t, findings, "R8", SeverityFail)
}

func TestInfraSecurityCheck_R8_Pass(t *testing.T) {
	plan := loadFixturePlan(t, "r8_pass.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireNoFinding(t, findings, "R8")
}

// ── R9: Registry retention disabled ──────────────────────────────────────────

func TestInfraSecurityCheck_R9_Trigger_Warn(t *testing.T) {
	plan := loadFixturePlan(t, "r9_trigger.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireFinding(t, findings, "R9", SeverityWarn)
}

func TestInfraSecurityCheck_R9_Pass(t *testing.T) {
	plan := loadFixturePlan(t, "r9_pass.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireNoFinding(t, findings, "R9")
}

// ── R10: Blast-radius cap ─────────────────────────────────────────────────────

func TestInfraSecurityCheck_R10_Trigger_TooManyChanges(t *testing.T) {
	plan := loadFixturePlan(t, "r10_trigger_max.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireFinding(t, findings, "R10", SeverityFail)
}

func TestInfraSecurityCheck_R10_Trigger_DeleteAndRecreate(t *testing.T) {
	plan := loadFixturePlan(t, "r10_trigger_replace_same.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireFinding(t, findings, "R10", SeverityFail)
}

func TestInfraSecurityCheck_R10_Pass(t *testing.T) {
	plan := loadFixturePlan(t, "r10_pass.json")
	findings := RunSecurityCheck(plan, defaultOpts())
	requireNoFinding(t, findings, "R10")
}

// ── Declarative YAML rules ────────────────────────────────────────────────────

func TestInfraSecurityCheck_DeclarativeRule_Fires(t *testing.T) {
	dir := t.TempDir()
	ruleYAML := `id: custom-db-check
description: No public databases
applies_to_action: create,update
applies_to_resource_type: infra.database
match: acl == "public"
severity: FAIL
message: Database must not have public ACL
`
	if err := os.WriteFile(filepath.Join(dir, "no-public-db.yaml"), []byte(ruleYAML), 0o600); err != nil {
		t.Fatalf("write rule: %v", err)
	}
	plan := interfaces.IaCPlan{
		ID: "decl-test",
		Actions: []interfaces.PlanAction{{
			Action: "create",
			Resource: interfaces.ResourceSpec{
				Name: "my-db", Type: "infra.database",
				Config: map[string]any{"acl": "public"},
			},
		}},
		CreatedAt: time.Now().UTC(),
	}
	opts := SecurityCheckOpts{MaxChanges: 20, ExtraRulesDir: dir}
	findings := RunSecurityCheck(plan, opts)
	requireFinding(t, findings, "custom-db-check", SeverityFail)
}

// ── runInfraSecurityCheck end-to-end ─────────────────────────────────────────

func TestRunInfraSecurityCheck_ExitOnFail(t *testing.T) {
	plan := interfaces.IaCPlan{
		ID: "e2e-fail",
		Actions: []interfaces.PlanAction{{
			Action: "create",
			Resource: interfaces.ResourceSpec{
				Name: "unsafe-fw", Type: "infra.firewall",
				Config: map[string]any{
					"inbound_rules": []any{
						map[string]any{"ports": "22", "sources": []any{"0.0.0.0/0"}},
					},
				},
			},
		}},
		CreatedAt: time.Now().UTC(),
	}
	data, _ := json.Marshal(plan)
	tmp := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	err := runInfraSecurityCheck([]string{"--plan", tmp})
	if err == nil {
		t.Fatal("expected error from FAIL finding, got nil")
	}
	if !strings.Contains(err.Error(), "security-check") {
		t.Errorf("error should mention security-check, got: %v", err)
	}
}

func TestRunInfraSecurityCheck_PassPlan(t *testing.T) {
	plan := interfaces.IaCPlan{
		ID: "e2e-pass",
		Actions: []interfaces.PlanAction{{
			Action: "create",
			Resource: interfaces.ResourceSpec{
				Name: "web", Type: "infra.container_service",
				Config: map[string]any{"image": "nginx:latest"},
			},
		}},
		CreatedAt: time.Now().UTC(),
	}
	data, _ := json.Marshal(plan)
	tmp := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	if err := runInfraSecurityCheck([]string{"--plan", tmp}); err != nil {
		t.Fatalf("expected no error for clean plan, got: %v", err)
	}
}
