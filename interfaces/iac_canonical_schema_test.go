package interfaces_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// loadCanonicalSchema compiles the embedded canonical IaC JSON schema.
func loadCanonicalSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	schemaDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(interfaces.IaCCanonicalSchemaJSON()))
	if err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("canonical.json", schemaDoc); err != nil {
		t.Fatalf("add schema resource: %v", err)
	}
	s, err := c.Compile("canonical.json")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return s
}

// validateConfig marshals config to JSON and validates it against the schema.
func validateConfig(t *testing.T, s *jsonschema.Schema, config map[string]any) error {
	t.Helper()
	b, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("unmarshal config for validation: %v", err)
	}
	return s.Validate(doc)
}

func TestIaCCanonicalSchema_ValidMinimal(t *testing.T) {
	s := loadCanonicalSchema(t)
	config := map[string]any{
		"name": "api-server",
	}
	if err := validateConfig(t, s, config); err != nil {
		t.Errorf("valid minimal config should pass: %v", err)
	}
}

func TestIaCCanonicalSchema_ValidFull(t *testing.T) {
	s := loadCanonicalSchema(t)
	config := map[string]any{
		"name":           "api-server",
		"region":         "nyc3",
		"image":          "registry.digitalocean.com/myapp/api:latest",
		"http_port":      8080,
		"instance_count": 2,
		"size":           "s",
		"env_vars": map[string]any{
			"ENV": "production",
		},
		"autoscaling": map[string]any{
			"min":         1,
			"max":         10,
			"cpu_percent": 70,
		},
		"health_check": map[string]any{
			"path": "/healthz",
		},
		"domains": []any{"api.example.com"},
		"jobs": []any{
			map[string]any{
				"name":        "migrate",
				"kind":        "pre_deploy",
				"run_command": "workflow-migrate up",
			},
		},
		"provider_specific": map[string]any{
			"digitalocean": map[string]any{"app_id": "abc123"},
		},
	}
	if err := validateConfig(t, s, config); err != nil {
		t.Errorf("valid full config should pass: %v", err)
	}
}

func TestIaCCanonicalSchema_InvalidUnknownKey(t *testing.T) {
	s := loadCanonicalSchema(t)
	config := map[string]any{
		"name":            "api-server",
		"unknown_field_x": "this should fail",
	}
	if err := validateConfig(t, s, config); err == nil {
		t.Error("config with unknown key should fail validation")
	}
}

func TestIaCCanonicalSchema_InvalidSizeEnum(t *testing.T) {
	s := loadCanonicalSchema(t)
	config := map[string]any{
		"name": "api-server",
		"size": "huge", // not in xs|s|m|l|xl
	}
	if err := validateConfig(t, s, config); err == nil {
		t.Error("invalid size value should fail validation")
	}
}

func TestIaCCanonicalSchema_ValidJSON(t *testing.T) {
	// Verify the schema file itself is valid JSON.
	var schema map[string]any
	if err := json.Unmarshal(interfaces.IaCCanonicalSchemaJSON(), &schema); err != nil {
		t.Errorf("schema file is not valid JSON: %v", err)
	}
}
