package schema

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

// ---------------------------------------------------------------------------
// GenerateWorkflowSchema tests
// ---------------------------------------------------------------------------

func TestGenerateWorkflowSchema_TopLevel(t *testing.T) {
	s := GenerateWorkflowSchema()

	if s.Schema != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("expected draft 2020-12, got %q", s.Schema)
	}
	if s.Type != "object" {
		t.Errorf("expected type object, got %q", s.Type)
	}
	if !slices.Contains(s.Required, "modules") {
		t.Error("modules should be required")
	}
	if s.Properties["modules"] == nil {
		t.Fatal("modules property missing")
	}
	if s.Properties["workflows"] == nil {
		t.Fatal("workflows property missing")
	}
	if s.Properties["triggers"] == nil {
		t.Fatal("triggers property missing")
	}
}

func TestGenerateWorkflowSchema_ModulesArray(t *testing.T) {
	s := GenerateWorkflowSchema()
	modules := s.Properties["modules"]
	if modules.Type != "array" {
		t.Errorf("modules should be array, got %q", modules.Type)
	}
	if modules.MinItems == nil || *modules.MinItems != 1 {
		t.Error("modules should have minItems=1")
	}
	items := modules.Items
	if items == nil {
		t.Fatal("modules items schema missing")
	}
	if items.Type != "object" {
		t.Errorf("module item should be object, got %q", items.Type)
	}
	if !slices.Contains(items.Required, "name") {
		t.Error("module item should require name")
	}
	if !slices.Contains(items.Required, "type") {
		t.Error("module item should require type")
	}
	typeSchema := items.Properties["type"]
	if typeSchema == nil {
		t.Fatal("type property missing from module item")
	}
	if len(typeSchema.Enum) == 0 {
		t.Error("type property should have enum values")
	}
	// Spot-check a few types
	for _, expected := range []string{"http.server", "http.router", "messaging.broker", "statemachine.engine"} {
		if !slices.Contains(typeSchema.Enum, expected) {
			t.Errorf("type enum missing %q", expected)
		}
	}
}

func TestGenerateWorkflowSchema_MarshalJSON(t *testing.T) {
	s := GenerateWorkflowSchema()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal schema: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("marshaled schema is empty")
	}
	// Round-trip: unmarshal and verify key fields survive
	var roundTrip map[string]any
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("failed to unmarshal schema JSON: %v", err)
	}
	if roundTrip["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Error("$schema lost in round-trip")
	}
	if roundTrip["title"] != "Workflow Configuration" {
		t.Error("title lost in round-trip")
	}
}

// ---------------------------------------------------------------------------
// KnownModuleTypes / KnownTriggerTypes / KnownWorkflowTypes tests
// ---------------------------------------------------------------------------

func TestKnownModuleTypes_Sorted(t *testing.T) {
	types := KnownModuleTypes()
	if len(types) == 0 {
		t.Fatal("no module types returned")
	}
	for i := 1; i < len(types); i++ {
		if types[i] < types[i-1] {
			t.Errorf("module types not sorted: %q comes before %q", types[i-1], types[i])
		}
	}
}

func TestKnownModuleTypes_NoDuplicates(t *testing.T) {
	seen := make(map[string]bool)
	for _, mt := range KnownModuleTypes() {
		if seen[mt] {
			t.Errorf("duplicate module type: %q", mt)
		}
		seen[mt] = true
	}
}

func TestKnownTriggerTypes(t *testing.T) {
	types := KnownTriggerTypes()
	expected := []string{"http", "schedule", "event", "eventbus"}
	for _, e := range expected {
		if !slices.Contains(types, e) {
			t.Errorf("missing trigger type %q", e)
		}
	}
}

func TestKnownWorkflowTypes(t *testing.T) {
	types := KnownWorkflowTypes()
	expected := []string{"http", "messaging", "statemachine", "scheduler", "integration"}
	for _, e := range expected {
		if !slices.Contains(types, e) {
			t.Errorf("missing workflow type %q", e)
		}
	}
}

// ---------------------------------------------------------------------------
// ValidateConfig tests
// ---------------------------------------------------------------------------

func validMinimalConfig() *config.WorkflowConfig {
	return &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "my-router", Type: "http.router"},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}
}

func TestValidateConfig_Minimal_Valid(t *testing.T) {
	cfg := validMinimalConfig()
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestValidateConfig_EmptyModules(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: nil,
	}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for empty modules")
	}
	assertContains(t, err.Error(), "at least one module is required")
}

func TestValidateConfig_MissingName(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "", Type: "http.router"},
		},
	}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	assertContains(t, err.Error(), "module name is required")
}

func TestValidateConfig_MissingType(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "foo", Type: ""},
		},
	}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing type")
	}
	assertContains(t, err.Error(), "module type is required")
}

func TestValidateConfig_UnknownType(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "foo", Type: "nonexistent.module"},
		},
	}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
	assertContains(t, err.Error(), `unknown module type "nonexistent.module"`)
}

func TestValidateConfig_DuplicateNames(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "mymod", Type: "http.router"},
			{Name: "mymod", Type: "http.handler"},
		},
	}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for duplicate names")
	}
	assertContains(t, err.Error(), `duplicate module name "mymod"`)
}

func TestValidateConfig_DependsOnUndefined(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "router", Type: "http.router", DependsOn: []string{"nonexistent"}},
		},
	}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for undefined dependency")
	}
	assertContains(t, err.Error(), `depends on undefined module "nonexistent"`)
}

func TestValidateConfig_DependsOnEmpty(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "router", Type: "http.router", DependsOn: []string{""}},
		},
	}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for empty dependency")
	}
	assertContains(t, err.Error(), "dependency name must not be empty")
}

func TestValidateConfig_DependsOnValid(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "server", Type: "http.server", Config: map[string]any{"address": ":8080"}},
			{Name: "router", Type: "http.router", DependsOn: []string{"server"}},
		},
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestValidateConfig_UnknownWorkflowType(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "r", Type: "http.router"},
		},
		Workflows: map[string]any{
			"unknown_workflow": map[string]any{},
		},
	}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for unknown workflow type")
	}
	assertContains(t, err.Error(), `unknown workflow type "unknown_workflow"`)
}

func TestValidateConfig_UnknownTriggerType(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "r", Type: "http.router"},
		},
		Triggers: map[string]any{
			"bad_trigger": map[string]any{},
		},
	}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for unknown trigger type")
	}
	assertContains(t, err.Error(), `unknown trigger type "bad_trigger"`)
}

func TestValidateConfig_ValidWorkflowAndTrigger(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "r", Type: "http.router"},
		},
		Workflows: map[string]any{
			"http": map[string]any{},
		},
		Triggers: map[string]any{
			"http": map[string]any{},
		},
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Module-specific config validation tests
// ---------------------------------------------------------------------------

func TestValidateConfig_HTTPServer_MissingAddress(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "srv", Type: "http.server", Config: map[string]any{}},
		},
	}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing address")
	}
	assertContains(t, err.Error(), `required config field "address" is missing`)
}

func TestValidateConfig_HTTPServer_NilConfig(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "srv", Type: "http.server"},
		},
	}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
	assertContains(t, err.Error(), "address")
}

func TestValidateConfig_HTTPServer_ValidAddress(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "srv", Type: "http.server", Config: map[string]any{"address": ":9090"}},
		},
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestValidateConfig_StaticFileserver_MissingRoot(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "fs", Type: "static.fileserver", Config: map[string]any{}},
		},
	}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing root")
	}
	assertContains(t, err.Error(), `required config field "root" is missing`)
}

func TestValidateConfig_DatabaseWorkflow_MissingFields(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "db", Type: "database.workflow", Config: map[string]any{}},
		},
	}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing driver/dsn")
	}
	assertContains(t, err.Error(), "driver")
	assertContains(t, err.Error(), "dsn")
}

func TestValidateConfig_AuthJWT_MissingSecret(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "auth", Type: "auth.jwt", Config: map[string]any{}},
		},
	}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
	assertContains(t, err.Error(), "secret")
}

func TestValidateConfig_SimpleProxy_InvalidTarget(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "proxy", Type: "http.simple_proxy", Config: map[string]any{
				"targets": map[string]any{
					"/api": 12345, // should be string
				},
			}},
		},
	}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for non-string target")
	}
	assertContains(t, err.Error(), "proxy target must be a string URL")
}

// ---------------------------------------------------------------------------
// Multiple errors accumulation test
// ---------------------------------------------------------------------------

func TestValidateConfig_MultipleErrors(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "", Type: ""},
			{Name: "dup", Type: "http.router"},
			{Name: "dup", Type: "http.handler"},
		},
		Workflows: map[string]any{
			"bad_wf": nil,
		},
		Triggers: map[string]any{
			"bad_trig": nil,
		},
	}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected multiple errors")
	}
	ve, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors type, got %T", err)
	}
	// We should have at least 5 errors: missing name, missing type, duplicate name,
	// unknown workflow, unknown trigger
	if len(ve) < 5 {
		t.Errorf("expected at least 5 errors, got %d: %v", len(ve), err)
	}
}

// ---------------------------------------------------------------------------
// ValidationError formatting tests
// ---------------------------------------------------------------------------

func TestValidationError_Format(t *testing.T) {
	e := &ValidationError{Path: "modules[0].type", Message: "unknown type"}
	got := e.Error()
	if got != "modules[0].type: unknown type" {
		t.Errorf("unexpected format: %q", got)
	}
}

func TestValidationError_NoPath(t *testing.T) {
	e := &ValidationError{Message: "something wrong"}
	if e.Error() != "something wrong" {
		t.Errorf("unexpected format: %q", e.Error())
	}
}

func TestValidationErrors_Format(t *testing.T) {
	errs := ValidationErrors{
		{Path: "a", Message: "err1"},
		{Path: "b", Message: "err2"},
	}
	got := errs.Error()
	if !strings.Contains(got, "2 error(s)") {
		t.Errorf("expected error count, got: %q", got)
	}
	if !strings.Contains(got, "a: err1") {
		t.Error("missing first error")
	}
	if !strings.Contains(got, "b: err2") {
		t.Error("missing second error")
	}
}

// ---------------------------------------------------------------------------
// Integration: validate a realistic config
// ---------------------------------------------------------------------------

func TestValidateConfig_RealisticPipeline(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "server", Type: "http.server", Config: map[string]any{"address": ":8080"}},
			{Name: "router", Type: "http.router", DependsOn: []string{"server"}},
			{Name: "handler", Type: "http.handler", DependsOn: []string{"router"}, Config: map[string]any{"contentType": "application/json"}},
			{Name: "transformer", Type: "data.transformer", DependsOn: []string{"handler"}},
			{Name: "state-engine", Type: "statemachine.engine"},
			{Name: "state-tracker", Type: "state.tracker"},
			{Name: "broker", Type: "messaging.broker"},
			{Name: "msg-handler", Type: "messaging.handler"},
			{Name: "metrics", Type: "metrics.collector"},
			{Name: "health", Type: "health.checker"},
		},
		Workflows: map[string]any{
			"http":         map[string]any{},
			"statemachine": map[string]any{},
			"messaging":    map[string]any{},
		},
		Triggers: map[string]any{
			"http": map[string]any{},
		},
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("expected valid pipeline config, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ValidationOption tests
// ---------------------------------------------------------------------------

func TestValidateConfig_WithExtraModuleTypes(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "custom", Type: "my.custom.module"},
		},
	}
	// Without option, should fail
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("expected error for custom type without option")
	}
	// With option, should pass
	if err := ValidateConfig(cfg, WithExtraModuleTypes("my.custom.module")); err != nil {
		t.Errorf("expected valid with extra type, got: %v", err)
	}
}

func TestValidateConfig_WithExtraWorkflowTypes(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "r", Type: "http.router"},
		},
		Workflows: map[string]any{
			"custom_workflow": map[string]any{},
		},
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("expected error for custom workflow type")
	}
	if err := ValidateConfig(cfg, WithExtraWorkflowTypes("custom_workflow")); err != nil {
		t.Errorf("expected valid with extra workflow type, got: %v", err)
	}
}

func TestValidateConfig_WithExtraTriggerTypes(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "r", Type: "http.router"},
		},
		Triggers: map[string]any{
			"custom_trigger": map[string]any{},
		},
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("expected error for custom trigger type")
	}
	if err := ValidateConfig(cfg, WithExtraTriggerTypes("custom_trigger")); err != nil {
		t.Errorf("expected valid with extra trigger type, got: %v", err)
	}
}

func TestValidateConfig_WithAllowEmptyModules(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: nil,
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("expected error for empty modules without option")
	}
	if err := ValidateConfig(cfg, WithAllowEmptyModules()); err != nil {
		t.Errorf("expected valid with allow empty, got: %v", err)
	}
}

func TestValidateConfig_WithSkipWorkflowTypeCheck(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "r", Type: "http.router"},
		},
		Workflows: map[string]any{
			"totally-custom": map[string]any{},
		},
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("expected error without skip option")
	}
	if err := ValidateConfig(cfg, WithSkipWorkflowTypeCheck()); err != nil {
		t.Errorf("expected valid with skip, got: %v", err)
	}
}

func TestValidateConfig_WithSkipTriggerTypeCheck(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "r", Type: "http.router"},
		},
		Triggers: map[string]any{
			"totally-custom": map[string]any{},
		},
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("expected error without skip option")
	}
	if err := ValidateConfig(cfg, WithSkipTriggerTypeCheck()); err != nil {
		t.Errorf("expected valid with skip, got: %v", err)
	}
}

func TestValidateConfig_CombinedOptions(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "custom", Type: "my.type"},
		},
		Workflows: map[string]any{
			"custom-wf": map[string]any{},
		},
		Triggers: map[string]any{
			"custom-trig": map[string]any{},
		},
	}
	err := ValidateConfig(cfg,
		WithExtraModuleTypes("my.type"),
		WithSkipWorkflowTypeCheck(),
		WithSkipTriggerTypeCheck(),
	)
	if err != nil {
		t.Errorf("expected valid with combined options, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("expected %q to contain %q", s, substr)
	}
}
