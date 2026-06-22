package assembler

import (
	"testing"

	"github.com/GoCodeAlone/workflow/schema"
)

func TestGenConfig_DefaultsPlusSensitiveEnv(t *testing.T) {
	// auth.jwt: secret (Required+Sensitive) -> ${JWT_SECRET}; tokenExpiry/issuer defaults present
	reg := schema.NewModuleSchemaRegistry() // has real auth.jwt registered
	cfg, ok := genConfig("auth.jwt", reg)
	if !ok {
		t.Fatal("expected auth.jwt in registry")
	}
	if cfg["secret"] != "${JWT_SECRET}" {
		t.Fatalf("secret=%v want ${JWT_SECRET}", cfg["secret"])
	}
	if cfg["tokenExpiry"] != "24h" {
		t.Fatalf("tokenExpiry=%v want 24h", cfg["tokenExpiry"])
	}
	if cfg["issuer"] != "workflow" {
		t.Fatalf("issuer=%v want workflow", cfg["issuer"])
	}
}

func TestGenConfig_UnknownTypeReturnsFalse(t *testing.T) {
	reg := schema.NewModuleSchemaRegistry()
	if _, ok := genConfig("no.such.type", reg); ok {
		t.Fatal("want ok=false for unknown type (existence gate, D6)")
	}
}

func TestGenConfig_DSNAsEnv(t *testing.T) {
	reg := schema.NewModuleSchemaRegistry() // database.workflow.dsn Required+Sensitive
	cfg, ok := genConfig("database.workflow", reg)
	if !ok {
		t.Fatal("expected database.workflow in registry")
	}
	if cfg["dsn"] != "${WORKFLOW_DSN}" {
		t.Fatalf("dsn=%v want ${WORKFLOW_DSN}", cfg["dsn"])
	} // namespaced (P2b)
}

func TestGenConfig_SelectRequiredGetsFirstOption(t *testing.T) {
	// database.workflow.driver: FieldTypeSelect, Required, no default -> first Option (P2)
	reg := schema.NewModuleSchemaRegistry()
	cfg, ok := genConfig("database.workflow", reg)
	if !ok {
		t.Fatal("expected database.workflow in registry")
	}
	if cfg["driver"] == "" {
		t.Fatalf("driver empty — want first select option (P2), cfg=%+v", cfg)
	}
}
