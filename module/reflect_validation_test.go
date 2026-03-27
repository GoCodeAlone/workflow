package module_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/schema"
)

// TestReflectValidation_DatabaseConfig verifies that editor tags on DatabaseConfig
// produce fields consistent with the hand-written database.workflow schema.
func TestReflectValidation_DatabaseConfig(t *testing.T) {
	fields := schema.GenerateConfigFields(module.DatabaseConfig{})

	// Only driver, dsn, maxOpenConns, maxIdleConns have editor tags.
	if len(fields) != 4 {
		t.Fatalf("expected 4 tagged fields, got %d", len(fields))
	}

	driver := fields[0]
	assertField(t, "driver", driver, schema.FieldTypeSelect, true, false)
	if len(driver.Options) != 3 {
		t.Errorf("driver: expected 3 options, got %d", len(driver.Options))
	}
	if driver.Label != "Driver" {
		t.Errorf("driver: expected label=Driver, got %q", driver.Label)
	}

	dsn := fields[1]
	assertField(t, "dsn", dsn, schema.FieldTypeString, true, true)
	if dsn.Label != "DSN" {
		t.Errorf("dsn: expected label=DSN, got %q", dsn.Label)
	}
	if dsn.Placeholder == "" {
		t.Error("dsn: expected non-empty placeholder")
	}

	maxOpen := fields[2]
	assertField(t, "maxOpenConns", maxOpen, schema.FieldTypeNumber, false, false)
	if maxOpen.DefaultValue != 25 {
		t.Errorf("maxOpenConns: expected defaultValue=25, got %v", maxOpen.DefaultValue)
	}
	if maxOpen.Label != "Max Open Connections" {
		t.Errorf("maxOpenConns: expected label='Max Open Connections', got %q", maxOpen.Label)
	}

	maxIdle := fields[3]
	assertField(t, "maxIdleConns", maxIdle, schema.FieldTypeNumber, false, false)
	if maxIdle.DefaultValue != 5 {
		t.Errorf("maxIdleConns: expected defaultValue=5, got %v", maxIdle.DefaultValue)
	}
	if maxIdle.Label != "Max Idle Connections" {
		t.Errorf("maxIdleConns: expected label='Max Idle Connections', got %q", maxIdle.Label)
	}

	// Compare against hand-written schema (first 4 fields).
	reg := schema.NewModuleSchemaRegistry()
	ms := reg.Get("database.workflow")
	if ms == nil {
		t.Fatal("database.workflow schema not found in registry")
	}
	assertSchemaMatchesGenerated(t, "database.workflow", ms.ConfigFields[:4], fields)
}

// TestReflectValidation_RedisNoSQLConfig verifies editor tags on RedisNoSQLConfig.
// Note: the struct json key is "addr" but the hand-written schema uses "address" —
// this is a known structural discrepancy captured here to prevent regression.
func TestReflectValidation_RedisNoSQLConfig(t *testing.T) {
	fields := schema.GenerateConfigFields(module.RedisNoSQLConfig{})

	if len(fields) != 3 {
		t.Fatalf("expected 3 tagged fields, got %d", len(fields))
	}

	addr := fields[0]
	if addr.Key != "addr" {
		t.Errorf("expected key=addr (json tag), got %q", addr.Key)
	}
	if addr.Type != schema.FieldTypeString {
		t.Errorf("addr: expected type=string, got %q", addr.Type)
	}
	if addr.Label != "Address" {
		t.Errorf("addr: expected label=Address, got %q", addr.Label)
	}
	if addr.DefaultValue != "localhost:6379" {
		t.Errorf("addr: expected default=localhost:6379, got %v", addr.DefaultValue)
	}

	password := fields[1]
	assertField(t, "password", password, schema.FieldTypeString, false, true)
	if password.Label != "Password" {
		t.Errorf("password: expected label=Password, got %q", password.Label)
	}

	db := fields[2]
	assertField(t, "db", db, schema.FieldTypeNumber, false, false)
	if db.Label != "Database" {
		t.Errorf("db: expected label=Database, got %q", db.Label)
	}
	if db.DefaultValue != 0 {
		t.Errorf("db: expected default=0, got %v", db.DefaultValue)
	}
}

// TestReflectValidation_HealthCheckerConfig verifies editor tags on HealthCheckerConfig.
func TestReflectValidation_HealthCheckerConfig(t *testing.T) {
	fields := schema.GenerateConfigFields(module.HealthCheckerConfig{})

	if len(fields) != 5 {
		t.Fatalf("expected 5 tagged fields, got %d", len(fields))
	}

	healthPath := fields[0]
	assertField(t, "healthPath", healthPath, schema.FieldTypeString, false, false)
	if healthPath.DefaultValue != "/healthz" {
		t.Errorf("healthPath: expected default=/healthz, got %v", healthPath.DefaultValue)
	}

	checkTimeout := fields[3]
	if checkTimeout.Key != "checkTimeout" {
		t.Errorf("expected key=checkTimeout, got %q", checkTimeout.Key)
	}
	if checkTimeout.Type != schema.FieldTypeDuration {
		t.Errorf("checkTimeout: expected type=duration, got %q", checkTimeout.Type)
	}
	if checkTimeout.DefaultValue != "5s" {
		t.Errorf("checkTimeout: expected default=5s, got %v", checkTimeout.DefaultValue)
	}

	autoDiscover := fields[4]
	assertField(t, "autoDiscover", autoDiscover, schema.FieldTypeBool, false, false)
	if autoDiscover.Label != "Auto-Discover" {
		t.Errorf("autoDiscover: expected label='Auto-Discover', got %q", autoDiscover.Label)
	}
	if autoDiscover.DefaultValue != true {
		t.Errorf("autoDiscover: expected default=true, got %v", autoDiscover.DefaultValue)
	}

	// Compare against hand-written schema.
	reg := schema.NewModuleSchemaRegistry()
	ms := reg.Get("health.checker")
	if ms == nil {
		t.Fatal("health.checker schema not found in registry")
	}
	assertSchemaMatchesGenerated(t, "health.checker", ms.ConfigFields, fields)
}

// TestReflectValidation_MetricsCollectorConfig verifies editor tags on MetricsCollectorConfig.
func TestReflectValidation_MetricsCollectorConfig(t *testing.T) {
	fields := schema.GenerateConfigFields(module.MetricsCollectorConfig{})

	if len(fields) != 4 {
		t.Fatalf("expected 4 tagged fields, got %d", len(fields))
	}

	namespace := fields[0]
	assertField(t, "namespace", namespace, schema.FieldTypeString, false, false)
	if namespace.DefaultValue != "workflow" {
		t.Errorf("namespace: expected default=workflow, got %v", namespace.DefaultValue)
	}

	metricsPath := fields[2]
	if metricsPath.Key != "metricsPath" {
		t.Errorf("expected key=metricsPath, got %q", metricsPath.Key)
	}
	if metricsPath.DefaultValue != "/metrics" {
		t.Errorf("metricsPath: expected default=/metrics, got %v", metricsPath.DefaultValue)
	}

	enabledMetrics := fields[3]
	assertField(t, "enabledMetrics", enabledMetrics, schema.FieldTypeArray, false, false)
	if enabledMetrics.ArrayItemType != "string" {
		t.Errorf("enabledMetrics: expected arrayItemType=string, got %q", enabledMetrics.ArrayItemType)
	}

	// Compare against hand-written schema.
	reg := schema.NewModuleSchemaRegistry()
	ms := reg.Get("metrics.collector")
	if ms == nil {
		t.Fatal("metrics.collector schema not found in registry")
	}
	assertSchemaMatchesGenerated(t, "metrics.collector", ms.ConfigFields, fields)
}

// TestReflectValidation_HTTPServerConfig verifies editor tags on HTTPServerConfig.
func TestReflectValidation_HTTPServerConfig(t *testing.T) {
	fields := schema.GenerateConfigFields(module.HTTPServerConfig{})

	if len(fields) != 3 {
		t.Fatalf("expected 3 tagged fields, got %d", len(fields))
	}

	address := fields[0]
	assertField(t, "address", address, schema.FieldTypeString, true, false)
	if address.DefaultValue != ":8080" {
		t.Errorf("address: expected default=:8080, got %v", address.DefaultValue)
	}

	readTimeout := fields[1]
	if readTimeout.Key != "readTimeout" {
		t.Errorf("expected key=readTimeout, got %q", readTimeout.Key)
	}
	if readTimeout.Type != schema.FieldTypeDuration {
		t.Errorf("readTimeout: expected type=duration, got %q", readTimeout.Type)
	}

	writeTimeout := fields[2]
	if writeTimeout.Key != "writeTimeout" {
		t.Errorf("expected key=writeTimeout, got %q", writeTimeout.Key)
	}
	if writeTimeout.Type != schema.FieldTypeDuration {
		t.Errorf("writeTimeout: expected type=duration, got %q", writeTimeout.Type)
	}

	// Verify address aligns with the hand-written http.server schema.
	reg := schema.NewModuleSchemaRegistry()
	ms := reg.Get("http.server")
	if ms == nil {
		t.Fatal("http.server schema not found in registry")
	}
	if len(ms.ConfigFields) < 1 {
		t.Fatal("http.server schema has no config fields")
	}
	schemaAddr := ms.ConfigFields[0]
	if schemaAddr.Type != address.Type {
		t.Errorf("address type: schema=%q generated=%q", schemaAddr.Type, address.Type)
	}
	if schemaAddr.Required != address.Required {
		t.Errorf("address required: schema=%v generated=%v", schemaAddr.Required, address.Required)
	}
}

// assertField checks key, type, required, and sensitive on a ConfigFieldDef.
func assertField(t *testing.T, key string, f schema.ConfigFieldDef, wantType schema.ConfigFieldType, wantRequired, wantSensitive bool) {
	t.Helper()
	if f.Key != key {
		t.Errorf("expected key=%q, got %q", key, f.Key)
	}
	if f.Type != wantType {
		t.Errorf("key=%q: expected type=%q, got %q", key, wantType, f.Type)
	}
	if f.Required != wantRequired {
		t.Errorf("key=%q: expected required=%v, got %v", key, wantRequired, f.Required)
	}
	if f.Sensitive != wantSensitive {
		t.Errorf("key=%q: expected sensitive=%v, got %v", key, wantSensitive, f.Sensitive)
	}
}

// assertSchemaMatchesGenerated compares manual schema fields to generated fields
// for fields present in both lists (matched by key).
func assertSchemaMatchesGenerated(t *testing.T, moduleType string, manual []schema.ConfigFieldDef, generated []schema.ConfigFieldDef) {
	t.Helper()
	genByKey := make(map[string]schema.ConfigFieldDef, len(generated))
	for _, f := range generated {
		genByKey[f.Key] = f
	}

	for _, m := range manual {
		g, ok := genByKey[m.Key]
		if !ok {
			// Key not present in generated output — skip (e.g. addr vs address).
			continue
		}
		if m.Type != g.Type {
			t.Errorf("%s field %q: schema type=%q, generated type=%q", moduleType, m.Key, m.Type, g.Type)
		}
		if m.Required != g.Required {
			t.Errorf("%s field %q: schema required=%v, generated required=%v", moduleType, m.Key, m.Required, g.Required)
		}
		if m.Sensitive != g.Sensitive {
			t.Errorf("%s field %q: schema sensitive=%v, generated sensitive=%v", moduleType, m.Key, m.Sensitive, g.Sensitive)
		}
	}
}
