package handler_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/admin/catalog"
	"github.com/GoCodeAlone/workflow/iac/admin/handler"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"gopkg.in/yaml.v3"
)

// TestGenerateConfig_HappyPath_VPC verifies the typical form
// submission for an infra.vpc resource produces a well-formed
// module YAML snippet. The YAML round-trips through yaml.Unmarshal
// to a generic map so the test asserts shape semantically rather
// than via string-equality (whitespace + key ordering vary).
func TestGenerateConfig_HappyPath_VPC(t *testing.T) {
	in := &adminpb.AdminGenerateConfigInput{
		ResourceType:   "infra.vpc",
		ResourceName:   "site-vpc",
		ProviderModule: "do-prod",
		FieldValues: map[string]string{
			"provider": "do-prod",
			"region":   "nyc3",
			"cidr":     "10.10.0.0/16",
		},
		Evidence: authzOK(),
	}
	out, err := handler.GenerateConfig(context.Background(), catalog.New(), in)
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	if out.Error != "" {
		t.Errorf("unexpected error: %q", out.Error)
	}
	if out.YamlSnippet == "" {
		t.Fatal("YamlSnippet empty")
	}

	// Parse the YAML to assert shape.
	var got map[string]any
	if err := yaml.Unmarshal([]byte(out.YamlSnippet), &got); err != nil {
		t.Fatalf("YAML output not parseable: %v\n%s", err, out.YamlSnippet)
	}
	if got["name"] != "site-vpc" {
		t.Errorf("name = %v, want site-vpc", got["name"])
	}
	if got["type"] != "infra.vpc" {
		t.Errorf("type = %v, want infra.vpc", got["type"])
	}
	cfg, _ := got["config"].(map[string]any)
	if cfg == nil {
		t.Fatalf("config missing or wrong shape: %v", got["config"])
	}
	if cfg["region"] != "nyc3" {
		t.Errorf("config.region = %v, want nyc3", cfg["region"])
	}
	if cfg["cidr"] != "10.10.0.0/16" {
		t.Errorf("config.cidr = %v, want 10.10.0.0/16", cfg["cidr"])
	}
}

// TestGenerateConfig_DefaultDeny pins the authz contract.
func TestGenerateConfig_DefaultDeny(t *testing.T) {
	in := &adminpb.AdminGenerateConfigInput{
		ResourceType: "infra.vpc",
		ResourceName: "site-vpc",
		FieldValues:  map[string]string{"region": "nyc3"},
	} // no Evidence
	out, _ := handler.GenerateConfig(context.Background(), catalog.New(), in)
	if out.Error == "" {
		t.Error("expected non-empty Error on missing evidence")
	}
	if out.YamlSnippet != "" {
		t.Errorf("YamlSnippet leaked on auth refusal: %q", out.YamlSnippet)
	}
}

// TestGenerateConfig_UnknownTypeReturnsValidationError pins the
// catalog-driven type guard. An unknown resource_type cannot be
// safely coerced; refuse with a clear message.
func TestGenerateConfig_UnknownTypeReturnsValidationError(t *testing.T) {
	in := &adminpb.AdminGenerateConfigInput{
		ResourceType:   "infra.not_real",
		ResourceName:   "x",
		ProviderModule: "do-prod",
		Evidence:       authzOK(),
	}
	out, _ := handler.GenerateConfig(context.Background(), catalog.New(), in)
	if out.Error == "" && len(out.ValidationErrors) == 0 {
		t.Error("expected error or validation_errors for unknown resource_type")
	}
}

// TestGenerateConfig_BoolCoercion verifies `bool`-kind fields parse
// the form's string-encoded value ("true" / "false") to a proper YAML
// bool — the form-builder doesn't carry a typed map, so coercion
// lives in the handler.
func TestGenerateConfig_BoolCoercion(t *testing.T) {
	in := &adminpb.AdminGenerateConfigInput{
		ResourceType:   "infra.database",
		ResourceName:   "db",
		ProviderModule: "do-prod",
		FieldValues: map[string]string{
			"provider": "do-prod",
			"region":   "nyc3",
			"engine":   "postgres",
			"size":     "m",
			"multi_az": "true",
		},
		Evidence: authzOK(),
	}
	out, err := handler.GenerateConfig(context.Background(), catalog.New(), in)
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	var got map[string]any
	if err := yaml.Unmarshal([]byte(out.YamlSnippet), &got); err != nil {
		t.Fatalf("YAML output not parseable: %v\n%s", err, out.YamlSnippet)
	}
	cfg, _ := got["config"].(map[string]any)
	v, ok := cfg["multi_az"].(bool)
	if !ok || !v {
		t.Errorf("multi_az = %v (type %T), want bool true (catalog kind=bool coercion failed)", cfg["multi_az"], cfg["multi_az"])
	}
}

// TestGenerateConfig_NumberCoercion verifies `number`-kind fields
// coerce to numeric YAML values.
func TestGenerateConfig_NumberCoercion(t *testing.T) {
	in := &adminpb.AdminGenerateConfigInput{
		ResourceType:   "infra.database",
		ResourceName:   "db",
		ProviderModule: "do-prod",
		FieldValues: map[string]string{
			"provider":   "do-prod",
			"region":     "nyc3",
			"engine":     "postgres",
			"size":       "m",
			"storage_gb": "100",
		},
		Evidence: authzOK(),
	}
	out, _ := handler.GenerateConfig(context.Background(), catalog.New(), in)
	var got map[string]any
	if err := yaml.Unmarshal([]byte(out.YamlSnippet), &got); err != nil {
		t.Fatalf("YAML not parseable: %v", err)
	}
	cfg, _ := got["config"].(map[string]any)
	// yaml.Unmarshal returns numeric values as int.
	if v, ok := cfg["storage_gb"].(int); !ok || v != 100 {
		t.Errorf("storage_gb = %v (type %T), want int 100", cfg["storage_gb"], cfg["storage_gb"])
	}
}

// TestGenerateConfig_ArrayValuesJSONDecoded honors the cross-task
// contract locked in 2026-05-27: array_string field_values arrive
// JSON-encoded (e.g. `field_values["ingress"] =
// "[\"rule a\", \"rule b, c\"]"`) so values containing commas
// survive the wire. The handler decodes via json.Unmarshal.
func TestGenerateConfig_ArrayValuesJSONDecoded(t *testing.T) {
	// FirewallConfig.ingress is array_string (FREEFORM_OK per design
	// line 429). A rule DSL value can contain commas — pin the
	// round-trip via JSON encoding.
	rulesJSON, _ := json.Marshal([]string{
		"allow tcp 80",
		"allow tcp 443,80", // contains comma — CSV would split this incorrectly
	})
	in := &adminpb.AdminGenerateConfigInput{
		ResourceType:   "infra.firewall",
		ResourceName:   "fw",
		ProviderModule: "do-prod",
		FieldValues: map[string]string{
			"provider": "do-prod",
			"region":   "nyc3",
			"ingress":  string(rulesJSON),
		},
		Evidence: authzOK(),
	}
	out, err := handler.GenerateConfig(context.Background(), catalog.New(), in)
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	if out.Error != "" {
		t.Fatalf("unexpected error: %q", out.Error)
	}
	var got map[string]any
	if err := yaml.Unmarshal([]byte(out.YamlSnippet), &got); err != nil {
		t.Fatalf("YAML not parseable: %v\n%s", err, out.YamlSnippet)
	}
	cfg, _ := got["config"].(map[string]any)
	ingress, ok := cfg["ingress"].([]any)
	if !ok {
		t.Fatalf("ingress not a list: %v (type %T)", cfg["ingress"], cfg["ingress"])
	}
	if len(ingress) != 2 {
		t.Fatalf("ingress count = %d, want 2", len(ingress))
	}
	if ingress[1] != "allow tcp 443,80" {
		t.Errorf("ingress[1] = %v, want 'allow tcp 443,80' (comma-in-value lossless via JSON encoding)", ingress[1])
	}
}

// TestGenerateConfig_PlainStringNotJSONDecoded verifies that when
// field_values carries a literal string for an array_string field
// (operator typed CSV manually, or a single-value submission), the
// handler accepts it gracefully — defensively wrap a non-JSON string
// into a one-element array.
//
// The JSON-encoded shape is canonical; the literal-string fallback is
// for safety so a malformed UI submission doesn't crash the server.
func TestGenerateConfig_PlainStringNotJSONDecoded(t *testing.T) {
	in := &adminpb.AdminGenerateConfigInput{
		ResourceType:   "infra.firewall",
		ResourceName:   "fw",
		ProviderModule: "do-prod",
		FieldValues: map[string]string{
			"provider": "do-prod",
			"region":   "nyc3",
			"ingress":  "allow tcp 80", // not JSON; defensive parse
		},
		Evidence: authzOK(),
	}
	out, err := handler.GenerateConfig(context.Background(), catalog.New(), in)
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	if out.Error != "" {
		t.Errorf("unexpected error: %q", out.Error)
	}
	var got map[string]any
	yaml.Unmarshal([]byte(out.YamlSnippet), &got)
	cfg, _ := got["config"].(map[string]any)
	ingress, _ := cfg["ingress"].([]any)
	if len(ingress) != 1 || ingress[0] != "allow tcp 80" {
		t.Errorf("ingress = %v, want one-element array [allow tcp 80] (defensive wrap)", ingress)
	}
}

// TestGenerateConfig_NoFmtSprintfUserInput is the strict-contract
// guard from plan §Task 6: GenerateConfig MUST NOT use fmt.Sprintf
// on user input to construct YAML. Verified by submitting a value
// that would mangle YAML if string-interpolated (line breaks, YAML
// reserved chars) and asserting the output still parses + the value
// round-trips intact.
func TestGenerateConfig_NoFmtSprintfUserInput(t *testing.T) {
	maliciousName := "x: y\n  injected: true"
	in := &adminpb.AdminGenerateConfigInput{
		ResourceType:   "infra.storage",
		ResourceName:   "store",
		ProviderModule: "do-prod",
		FieldValues: map[string]string{
			"provider": "do-prod",
			"region":   "nyc3",
			"name":     maliciousName,
			"class":    "standard",
		},
		Evidence: authzOK(),
	}
	out, err := handler.GenerateConfig(context.Background(), catalog.New(), in)
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}
	if out.Error != "" {
		t.Errorf("unexpected error: %q", out.Error)
	}
	var got map[string]any
	if err := yaml.Unmarshal([]byte(out.YamlSnippet), &got); err != nil {
		t.Fatalf("YAML not parseable — possible Sprintf injection: %v\n%s", err, out.YamlSnippet)
	}
	cfg, _ := got["config"].(map[string]any)
	if cfg["name"] != maliciousName {
		t.Errorf("name not round-tripped intact (Sprintf injection?): got %v, want %v", cfg["name"], maliciousName)
	}
	if _, leaked := cfg["injected"]; leaked {
		t.Error("'injected' key leaked into config — yaml.Marshal not used (Sprintf injection succeeded)")
	}
}

// TestGenerateConfig_OutputIsAMapModuleEntry verifies the YAML
// produced is a single module-entry shape (with name + type +
// config), NOT wrapped in `modules: [...]`. The form-builder's
// "copy" button + the docs expect the user to paste under their
// existing `modules:` block.
func TestGenerateConfig_OutputIsAMapModuleEntry(t *testing.T) {
	in := &adminpb.AdminGenerateConfigInput{
		ResourceType:   "infra.vpc",
		ResourceName:   "vpc1",
		ProviderModule: "do-prod",
		FieldValues:    map[string]string{"provider": "do-prod", "region": "nyc3"},
		Evidence:       authzOK(),
	}
	out, _ := handler.GenerateConfig(context.Background(), catalog.New(), in)
	if strings.HasPrefix(strings.TrimSpace(out.YamlSnippet), "modules:") {
		t.Errorf("YAML wraps in modules: — should be a bare module entry. Got:\n%s", out.YamlSnippet)
	}
	if !strings.Contains(out.YamlSnippet, "name: vpc1") {
		t.Errorf("YAML missing name: vpc1\n%s", out.YamlSnippet)
	}
}
