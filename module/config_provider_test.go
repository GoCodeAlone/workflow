package module

import (
	"os"
	"sort"
	"testing"
)

// --- ConfigRegistry tests ---

func TestConfigRegistrySetAndGet(t *testing.T) {
	r := NewConfigRegistry()
	if err := r.Set("key1", "value1", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := r.Get("key1")
	if !ok || v != "value1" {
		t.Fatalf("expected value1, got %q (ok=%v)", v, ok)
	}
}

func TestConfigRegistryGetMissing(t *testing.T) {
	r := NewConfigRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Fatal("expected key not found")
	}
}

func TestConfigRegistrySensitive(t *testing.T) {
	r := NewConfigRegistry()
	_ = r.Set("secret", "s3cr3t", true)
	_ = r.Set("plain", "hello", false)

	if !r.IsSensitive("secret") {
		t.Fatal("expected secret to be sensitive")
	}
	if r.IsSensitive("plain") {
		t.Fatal("expected plain to not be sensitive")
	}
}

func TestConfigRegistryRedactedValue(t *testing.T) {
	r := NewConfigRegistry()
	_ = r.Set("secret", "s3cr3t", true)
	_ = r.Set("plain", "hello", false)

	if r.RedactedValue("secret") != "********" {
		t.Fatalf("expected redacted, got %q", r.RedactedValue("secret"))
	}
	if r.RedactedValue("plain") != "hello" {
		t.Fatalf("expected hello, got %q", r.RedactedValue("plain"))
	}
}

func TestConfigRegistryFreeze(t *testing.T) {
	r := NewConfigRegistry()
	_ = r.Set("key1", "value1", false)
	r.Freeze()

	if err := r.Set("key2", "value2", false); err == nil {
		t.Fatal("expected error setting after freeze")
	}
	// Can still read
	v, ok := r.Get("key1")
	if !ok || v != "value1" {
		t.Fatal("expected to read frozen value")
	}
}

func TestConfigRegistryReset(t *testing.T) {
	r := NewConfigRegistry()
	_ = r.Set("key1", "value1", false)
	r.Freeze()
	r.Reset()

	// Should be writable again
	if err := r.Set("key2", "value2", false); err != nil {
		t.Fatalf("unexpected error after reset: %v", err)
	}
	_, ok := r.Get("key1")
	if ok {
		t.Fatal("expected key1 to be cleared after reset")
	}
}

func TestConfigRegistryKeys(t *testing.T) {
	r := NewConfigRegistry()
	_ = r.Set("b", "2", false)
	_ = r.Set("a", "1", false)
	_ = r.Set("c", "3", false)
	keys := r.Keys()
	sort.Strings(keys)
	if len(keys) != 3 || keys[0] != "a" || keys[1] != "b" || keys[2] != "c" {
		t.Fatalf("unexpected keys: %v", keys)
	}
}

// --- ExpandConfigTemplate tests ---

func TestExpandConfigTemplate(t *testing.T) {
	r := NewConfigRegistry()
	_ = r.Set("db_dsn", "postgres://localhost/db", false)
	_ = r.Set("api_port", "8080", false)

	tests := []struct {
		input    string
		expected string
	}{
		{`{{config "db_dsn"}}`, "postgres://localhost/db"},
		{`{{ config "api_port" }}`, "8080"},
		{`host:{{config "api_port"}}`, "host:8080"},
		{`no-template-here`, "no-template-here"},
		{`{{config "missing"}}`, `{{config "missing"}}`}, // unresolved stays
		{`dsn={{config "db_dsn"}}&port={{config "api_port"}}`, "dsn=postgres://localhost/db&port=8080"},
		// single-quoted variants
		{`{{config 'db_dsn'}}`, "postgres://localhost/db"},
		{`{{ config 'api_port' }}`, "8080"},
	}
	for _, tt := range tests {
		result := r.ExpandConfigTemplate(tt.input)
		if result != tt.expected {
			t.Errorf("ExpandConfigTemplate(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// --- ParseSchema tests ---

func TestParseSchema(t *testing.T) {
	raw := map[string]any{
		"db_dsn": map[string]any{
			"env":       "DB_DSN",
			"required":  true,
			"sensitive": true,
			"desc":      "Database connection string",
		},
		"api_port": map[string]any{
			"env":     "API_PORT",
			"default": "8080",
			"desc":    "HTTP listen port",
		},
	}
	schema, err := ParseSchema(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(schema) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(schema))
	}
	db := schema["db_dsn"]
	if db.Env != "DB_DSN" || !db.Required || !db.Sensitive || db.Desc != "Database connection string" {
		t.Fatalf("unexpected db_dsn entry: %+v", db)
	}
	port := schema["api_port"]
	if port.Env != "API_PORT" || port.Default != "8080" || port.Required {
		t.Fatalf("unexpected api_port entry: %+v", port)
	}
}

func TestParseSchemaInvalid(t *testing.T) {
	raw := map[string]any{
		"bad_entry": "not-a-map",
	}
	_, err := ParseSchema(raw)
	if err == nil {
		t.Fatal("expected error for non-map entry")
	}
}

// --- LoadConfigSources tests ---

func TestLoadConfigSourcesDefaults(t *testing.T) {
	r := NewConfigRegistry()
	schema := map[string]SchemaEntry{
		"port":   {Default: "8080"},
		"region": {Default: "us-east-1"},
		"secret": {Default: "default-secret", Sensitive: true},
	}
	sources := []map[string]any{
		{"type": "defaults"},
	}
	if err := LoadConfigSources(r, sources, schema); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := r.Get("port")
	if !ok || v != "8080" {
		t.Fatalf("expected 8080, got %q", v)
	}
	if !r.IsSensitive("secret") {
		t.Fatal("expected secret to be sensitive")
	}
}

func TestLoadConfigSourcesEnv(t *testing.T) {
	r := NewConfigRegistry()
	schema := map[string]SchemaEntry{
		"port": {Env: "TEST_CFG_PORT", Default: "8080"},
	}

	t.Setenv("TEST_CFG_PORT", "9090")

	sources := []map[string]any{
		{"type": "defaults"},
		{"type": "env"},
	}
	if err := LoadConfigSources(r, sources, schema); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, _ := r.Get("port")
	if v != "9090" {
		t.Fatalf("expected env override 9090, got %q", v)
	}
}

func TestLoadConfigSourcesEnvPrefix(t *testing.T) {
	r := NewConfigRegistry()
	schema := map[string]SchemaEntry{
		"port": {Env: "PORT"},
	}

	t.Setenv("APP_PORT", "3000")

	sources := []map[string]any{
		{"type": "env", "prefix": "APP_"},
	}
	if err := LoadConfigSources(r, sources, schema); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, _ := r.Get("port")
	if v != "3000" {
		t.Fatalf("expected 3000, got %q", v)
	}
}

func TestLoadConfigSourcesUnknownType(t *testing.T) {
	r := NewConfigRegistry()
	sources := []map[string]any{
		{"type": "unknown"},
	}
	err := LoadConfigSources(r, sources, nil)
	if err == nil {
		t.Fatal("expected error for unknown source type")
	}
}

// --- ValidateRequired tests ---

func TestValidateRequiredAllPresent(t *testing.T) {
	r := NewConfigRegistry()
	_ = r.Set("db_dsn", "postgres://...", false)
	_ = r.Set("token", "abc", false)
	schema := map[string]SchemaEntry{
		"db_dsn": {Required: true},
		"token":  {Required: true},
		"port":   {Required: false},
	}
	if err := ValidateRequired(r, schema); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRequiredMissing(t *testing.T) {
	r := NewConfigRegistry()
	_ = r.Set("token", "abc", false)
	schema := map[string]SchemaEntry{
		"db_dsn": {Required: true},
		"token":  {Required: true},
	}
	err := ValidateRequired(r, schema)
	if err == nil {
		t.Fatal("expected error for missing required key")
	}
	if got := err.Error(); got != "missing required config keys: db_dsn" {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestValidateRequiredMissingSorted(t *testing.T) {
	r := NewConfigRegistry()
	schema := map[string]SchemaEntry{
		"z_key": {Required: true},
		"a_key": {Required: true},
		"m_key": {Required: true},
	}
	err := ValidateRequired(r, schema)
	if err == nil {
		t.Fatal("expected error for missing required keys")
	}
	// Keys must appear in sorted order regardless of map iteration order
	const want = "missing required config keys: a_key, m_key, z_key"
	if got := err.Error(); got != want {
		t.Fatalf("expected sorted error:\nwant: %q\ngot:  %q", want, got)
	}
}

// --- ExpandConfigRefsMap tests ---

func TestExpandConfigRefsMap(t *testing.T) {
	r := NewConfigRegistry()
	_ = r.Set("db_dsn", "postgres://localhost/db", false)
	_ = r.Set("port", "8080", false)

	cfg := map[string]any{
		"dsn":     `{{config "db_dsn"}}`,
		"address": `0.0.0.0:{{config "port"}}`,
		"nested": map[string]any{
			"value": `{{config "db_dsn"}}`,
		},
		"list": []any{
			`item-{{config "port"}}`,
			map[string]any{
				"inner": `{{config "db_dsn"}}`,
			},
		},
		"unchanged": "no-template",
	}

	ExpandConfigRefsMap(r, cfg)

	if cfg["dsn"] != "postgres://localhost/db" {
		t.Fatalf("dsn: expected expanded, got %q", cfg["dsn"])
	}
	if cfg["address"] != "0.0.0.0:8080" {
		t.Fatalf("address: expected expanded, got %q", cfg["address"])
	}
	nested := cfg["nested"].(map[string]any)
	if nested["value"] != "postgres://localhost/db" {
		t.Fatalf("nested.value: expected expanded, got %q", nested["value"])
	}
	list := cfg["list"].([]any)
	if list[0] != "item-8080" {
		t.Fatalf("list[0]: expected expanded, got %q", list[0])
	}
	innerMap := list[1].(map[string]any)
	if innerMap["inner"] != "postgres://localhost/db" {
		t.Fatalf("list[1].inner: expected expanded, got %q", innerMap["inner"])
	}
	if cfg["unchanged"] != "no-template" {
		t.Fatalf("unchanged: expected no change, got %q", cfg["unchanged"])
	}
}

func TestExpandConfigRefsMapNilSafe(t *testing.T) {
	// Should not panic
	ExpandConfigRefsMap(nil, nil)
	ExpandConfigRefsMap(NewConfigRegistry(), nil)
	ExpandConfigRefsMap(nil, map[string]any{"a": "b"})
}

// --- ConfigProviderModule tests ---

func TestConfigProviderModuleName(t *testing.T) {
	m := NewConfigProviderModule("my-config", nil)
	if m.Name() != "my-config" {
		t.Fatalf("expected my-config, got %q", m.Name())
	}
}

func TestConfigProviderModuleDependencies(t *testing.T) {
	m := NewConfigProviderModule("my-config", nil)
	if deps := m.Dependencies(); deps != nil {
		t.Fatalf("expected nil dependencies, got %v", deps)
	}
}

func TestConfigProviderModuleRegistry(t *testing.T) {
	m := NewConfigProviderModule("my-config", nil)
	if m.Registry() == nil {
		t.Fatal("expected non-nil registry")
	}
}

// --- Integration: end-to-end schema + sources + expand ---

func TestEndToEndConfigProvider(t *testing.T) {
	// Simulate what the ConfigTransformHook does

	t.Setenv("E2E_DB_DSN", "postgres://prod/mydb")
	// Don't set E2E_API_PORT — should use default

	r := NewConfigRegistry()

	schemaRaw := map[string]any{
		"db_dsn": map[string]any{
			"env":       "E2E_DB_DSN",
			"required":  true,
			"sensitive": true,
			"desc":      "Database connection string",
		},
		"api_port": map[string]any{
			"env":     "E2E_API_PORT",
			"default": "8080",
			"desc":    "HTTP listen port",
		},
		"region": map[string]any{
			"env":     "E2E_REGION",
			"default": "us-east-1",
		},
	}

	schema, err := ParseSchema(schemaRaw)
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}

	sources := []map[string]any{
		{"type": "defaults"},
		{"type": "env"},
	}
	if err := LoadConfigSources(r, sources, schema); err != nil {
		t.Fatalf("LoadConfigSources: %v", err)
	}
	if err := ValidateRequired(r, schema); err != nil {
		t.Fatalf("ValidateRequired: %v", err)
	}
	r.Freeze()

	// Expand references in module config
	moduleCfg := map[string]any{
		"driver":  "postgres",
		"dsn":     `{{config "db_dsn"}}`,
		"address": `0.0.0.0:{{config "api_port"}}`,
		"region":  `{{config "region"}}`,
	}
	ExpandConfigRefsMap(r, moduleCfg)

	if moduleCfg["dsn"] != "postgres://prod/mydb" {
		t.Fatalf("dsn: expected env value, got %q", moduleCfg["dsn"])
	}
	if moduleCfg["address"] != "0.0.0.0:8080" {
		t.Fatalf("address: expected default, got %q", moduleCfg["address"])
	}
	if moduleCfg["region"] != "us-east-1" {
		t.Fatalf("region: expected default, got %q", moduleCfg["region"])
	}

	// Verify sensitive redaction
	if r.RedactedValue("db_dsn") != "********" {
		t.Fatal("expected db_dsn to be redacted")
	}
	if r.RedactedValue("api_port") != "8080" {
		t.Fatal("expected api_port to not be redacted")
	}
}

func TestEndToEndRequiredMissing(t *testing.T) {
	// Unset env to trigger missing required
	os.Unsetenv("E2E_MISSING_KEY")

	r := NewConfigRegistry()
	schema := map[string]SchemaEntry{
		"missing_key": {Env: "E2E_MISSING_KEY", Required: true},
	}
	sources := []map[string]any{
		{"type": "defaults"},
		{"type": "env"},
	}
	if err := LoadConfigSources(r, sources, schema); err != nil {
		t.Fatalf("LoadConfigSources: %v", err)
	}
	err := ValidateRequired(r, schema)
	if err == nil {
		t.Fatal("expected validation error for missing required key")
	}
}

func TestEnvOverridesDefault(t *testing.T) {
	r := NewConfigRegistry()
	schema := map[string]SchemaEntry{
		"port": {Env: "TEST_OVERRIDE_PORT", Default: "8080"},
	}
	t.Setenv("TEST_OVERRIDE_PORT", "9999")

	sources := []map[string]any{
		{"type": "defaults"},
		{"type": "env"},
	}
	if err := LoadConfigSources(r, sources, schema); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, _ := r.Get("port")
	if v != "9999" {
		t.Fatalf("expected env override 9999, got %q", v)
	}
}

// --- Global registry tests ---

func TestGlobalConfigRegistry(t *testing.T) {
	reg := GetConfigRegistry()
	if reg == nil {
		t.Fatal("expected non-nil global registry")
	}
	// Reset to clean state for other tests
	reg.Reset()
}
