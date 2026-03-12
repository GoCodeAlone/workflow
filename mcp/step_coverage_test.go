package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/plugin"
	pluginall "github.com/GoCodeAlone/workflow/plugins/all"
	"github.com/GoCodeAlone/workflow/schema"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerBuiltinPluginTypesForTest loads all built-in plugins into the global
// schema registries (schema.KnownModuleTypes / schema.GetStepSchemaRegistry)
// so that MCP tools that rely on these registries reflect the full type set.
// This mirrors what happens at runtime when the workflow engine calls LoadPlugin
// for each built-in plugin.
func registerBuiltinPluginTypesForTest(t *testing.T) {
	t.Helper()
	capReg := capability.NewRegistry()
	schemaReg := schema.NewModuleSchemaRegistry()
	loader := plugin.NewPluginLoader(capReg, schemaReg)
	for _, p := range pluginall.DefaultPlugins() {
		if err := loader.LoadPlugin(p); err != nil {
			t.Fatalf("LoadPlugin(%q) failed: %v", p.Name(), err)
		}
		// Register module and step types into the global schema registry so
		// that schema.KnownModuleTypes() and handleListStepTypes see them.
		for typeName := range loader.ModuleFactories() {
			schema.RegisterModuleType(typeName)
		}
		for typeName := range loader.StepFactories() {
			schema.RegisterModuleType(typeName)
		}
		// Register rich step schemas (descriptions, config fields, outputs).
		for _, ss := range loader.StepSchemaRegistry().All() {
			schema.GetStepSchemaRegistry().Register(ss)
		}
	}
}

// TestListStepTypes_AllBuiltinsPresent validates that every step type registered
// by the built-in plugins (plugins/all) appears in the MCP list_step_types tool
// response. This is the MCP equivalent of TestDocumentationCoverage and ensures
// that wfctl's MCP server accurately reflects all available step types.
func TestListStepTypes_AllBuiltinsPresent(t *testing.T) {
	registerBuiltinPluginTypesForTest(t)

	srv := NewServer("")
	result, err := srv.handleListStepTypes(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handleListStepTypes error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	steps, ok := data["step_types"].([]any)
	if !ok {
		t.Fatal("step_types not found in result")
	}
	listed := make(map[string]bool, len(steps))
	for _, s := range steps {
		if entry, ok := s.(map[string]any); ok {
			if typeName, ok := entry["type"].(string); ok {
				listed[typeName] = true
			}
		}
	}

	// Collect all step types from the built-in plugins.
	capReg := capability.NewRegistry()
	schemaReg := schema.NewModuleSchemaRegistry()
	loader := plugin.NewPluginLoader(capReg, schemaReg)
	for _, p := range pluginall.DefaultPlugins() {
		if err := loader.LoadPlugin(p); err != nil {
			t.Fatalf("LoadPlugin(%q) failed: %v", p.Name(), err)
		}
	}

	var missing []string
	for typeName := range loader.StepFactories() {
		if !listed[typeName] {
			missing = append(missing, typeName)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("step types registered by built-in plugins but missing from list_step_types (%d missing):\n  %s\n\n"+
			"Add these step types to schema/schema.go coreModuleTypes slice "+
			"or register them via schema.RegisterModuleType so they appear in KnownModuleTypes.",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// TestListModuleTypes_AllBuiltinsPresent validates that every module type registered
// by the built-in plugins (plugins/all) appears in the MCP list_module_types tool
// response.
func TestListModuleTypes_AllBuiltinsPresent(t *testing.T) {
	registerBuiltinPluginTypesForTest(t)

	srv := NewServer("")
	result, err := srv.handleListModuleTypes(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handleListModuleTypes error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	rawTypes, ok := data["module_types"].([]any)
	if !ok {
		t.Fatal("module_types not found in result")
	}
	listed := make(map[string]bool, len(rawTypes))
	for _, mt := range rawTypes {
		if s, ok := mt.(string); ok {
			listed[s] = true
		}
	}

	// Collect all module types from the built-in plugins.
	capReg := capability.NewRegistry()
	schemaReg := schema.NewModuleSchemaRegistry()
	loader := plugin.NewPluginLoader(capReg, schemaReg)
	for _, p := range pluginall.DefaultPlugins() {
		if err := loader.LoadPlugin(p); err != nil {
			t.Fatalf("LoadPlugin(%q) failed: %v", p.Name(), err)
		}
	}

	var missing []string
	for typeName := range loader.ModuleFactories() {
		if !listed[typeName] {
			missing = append(missing, typeName)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("module types registered by built-in plugins but missing from list_module_types (%d missing):\n  %s\n\n"+
			"Add these module types to schema/schema.go coreModuleTypes slice "+
			"or register them via schema.RegisterModuleType so they appear in KnownModuleTypes.",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// TestDocsFullReference_Fallback verifies that the full-reference resource
// returns a usable fallback when DOCUMENTATION.md cannot be found.
func TestDocsFullReference_Fallback(t *testing.T) {
	// Use a server with a non-existent plugin dir so no file is found.
	srv := NewServer("/nonexistent/data/plugins")
	contents, err := srv.handleDocsFullReference(context.Background(), mcp.ReadResourceRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 resource content, got %d", len(contents))
	}
	text, ok := contents[0].(mcp.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}
	if text.URI != "workflow://docs/full-reference" {
		t.Errorf("unexpected URI: %q", text.URI)
	}
	if text.MIMEType != "text/markdown" {
		t.Errorf("unexpected MIME type: %q", text.MIMEType)
	}
	if !strings.Contains(text.Text, "GoCodeAlone/workflow") {
		t.Error("fallback text should mention 'GoCodeAlone/workflow'")
	}
}

// TestDocsFullReference_WithFile verifies that the full-reference resource
// serves the provided file content when WithDocumentationFile is used.
func TestDocsFullReference_WithFile(t *testing.T) {
	// Write a temporary DOCUMENTATION.md-like file.
	dir := t.TempDir()
	docPath := filepath.Join(dir, "DOCUMENTATION.md")
	content := "# Workflow Engine Documentation\n\nTest content.\n"
	if err := os.WriteFile(docPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	srv := NewServer("", WithDocumentationFile(docPath))
	contents, err := srv.handleDocsFullReference(context.Background(), mcp.ReadResourceRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 resource content, got %d", len(contents))
	}
	text, ok := contents[0].(mcp.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}
	if text.Text != content {
		t.Errorf("expected file content %q, got %q", content, text.Text)
	}
}

// TestDocsFullReference_RepoFile verifies that the full-reference resource
// serves the actual DOCUMENTATION.md when it exists next to the test.
func TestDocsFullReference_RepoFile(t *testing.T) {
	// Locate the repo root via the test file's path.
	_, testFilePath, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(testFilePath), "..")
	docPath := filepath.Join(repoRoot, "DOCUMENTATION.md")
	if _, err := os.Stat(docPath); err != nil {
		t.Skipf("DOCUMENTATION.md not found at %q: %v", docPath, err)
	}

	srv := NewServer("", WithDocumentationFile(docPath))
	contents, err := srv.handleDocsFullReference(context.Background(), mcp.ReadResourceRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text, ok := contents[0].(mcp.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}

	// Spot-check a few key strings that should be in DOCUMENTATION.md.
	for _, want := range []string{"openapi", "auth.m2m", "database.partitioned", "config.provider"} {
		if !strings.Contains(text.Text, want) {
			t.Errorf("DOCUMENTATION.md should contain %q", want)
		}
	}
}
