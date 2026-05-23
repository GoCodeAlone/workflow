package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/schema"
)

// Issue #756: wfctl validate must recognize module/step/trigger types declared
// in local plugin.json manifests when the workflow config references those
// types via requires.plugins[].
//
// Two surfaces are tested:
//   1. --plugin-manifest <path>: explicit path to a plugin.json (or a directory
//      containing one). Operator-driven override.
//   2. Auto-resolution of requires.plugins[] against conventional sibling and
//      ancestor locations of the config file. Convention over configuration.

const issue756ConfigBody = `requires:
  plugins:
    - name: workflow-plugin-issue756
modules:
  - name: ext
    type: issue756.module
  - name: ext_step_owner
    type: issue756.other_module
`

const issue756ManifestBody = `{
  "name": "workflow-plugin-issue756",
  "version": "0.1.0",
  "capabilities": {
    "moduleTypes": ["issue756.module", "issue756.other_module"]
  }
}`

func unregisterIssue756Types(t *testing.T) {
	t.Helper()
	schema.UnregisterModuleType("issue756.module")
	schema.UnregisterModuleType("issue756.other_module")
}

// Baseline: without any manifest resolution, validate fails because the
// referenced module types are not registered. This pins the bug.
func TestValidate_UnknownTypes_WithoutManifestResolution(t *testing.T) {
	unregisterIssue756Types(t)
	t.Cleanup(func() { unregisterIssue756Types(t) })

	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "workflow.yaml", issue756ConfigBody)

	err := runValidate([]string{"--allow-no-entry-points", cfgPath})
	if err == nil {
		t.Fatal("expected validation to fail when manifest is not discoverable")
	}
}

// --plugin-manifest pointing at the manifest file directly resolves types.
func TestValidate_PluginManifestFlag_PointingAtFile(t *testing.T) {
	unregisterIssue756Types(t)
	t.Cleanup(func() { unregisterIssue756Types(t) })

	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "workflow.yaml", issue756ConfigBody)
	manifestPath := filepath.Join(dir, "plugin.json")
	if err := os.WriteFile(manifestPath, []byte(issue756ManifestBody), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := runValidate([]string{"--allow-no-entry-points", "--plugin-manifest", manifestPath, cfgPath}); err != nil {
		t.Fatalf("validate with --plugin-manifest <file>: %v", err)
	}
}

// --plugin-manifest pointing at a directory containing plugin.json resolves types.
func TestValidate_PluginManifestFlag_PointingAtDir(t *testing.T) {
	unregisterIssue756Types(t)
	t.Cleanup(func() { unregisterIssue756Types(t) })

	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "workflow.yaml", issue756ConfigBody)
	pluginDir := filepath.Join(dir, "workflow-plugin-issue756")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("mkdir plugin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(issue756ManifestBody), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := runValidate([]string{"--allow-no-entry-points", "--plugin-manifest", pluginDir, cfgPath}); err != nil {
		t.Fatalf("validate with --plugin-manifest <dir>: %v", err)
	}
}

// --plugin-manifest is repeatable.
func TestValidate_PluginManifestFlag_Repeatable(t *testing.T) {
	unregisterIssue756Types(t)
	t.Cleanup(func() { unregisterIssue756Types(t) })

	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "workflow.yaml", issue756ConfigBody)

	mA := filepath.Join(dir, "a.json")
	mB := filepath.Join(dir, "b.json")
	manifestA := `{"name":"a","capabilities":{"moduleTypes":["issue756.module"]}}`
	manifestB := `{"name":"b","capabilities":{"moduleTypes":["issue756.other_module"]}}`
	if err := os.WriteFile(mA, []byte(manifestA), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mB, []byte(manifestB), 0644); err != nil {
		t.Fatal(err)
	}

	if err := runValidate([]string{"--allow-no-entry-points", "--plugin-manifest", mA, "--plugin-manifest", mB, cfgPath}); err != nil {
		t.Fatalf("validate with two --plugin-manifest flags: %v", err)
	}
}

// Auto-resolution: plugin.json found at <cfgDir>/<name>/plugin.json.
func TestValidate_AutoResolve_SiblingPluginDir(t *testing.T) {
	unregisterIssue756Types(t)
	t.Cleanup(func() { unregisterIssue756Types(t) })

	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "workflow.yaml", issue756ConfigBody)
	pluginDir := filepath.Join(dir, "workflow-plugin-issue756")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(issue756ManifestBody), 0644); err != nil {
		t.Fatal(err)
	}

	if err := runValidate([]string{"--allow-no-entry-points", cfgPath}); err != nil {
		t.Fatalf("validate with auto-resolved sibling plugin dir: %v", err)
	}
}

// Auto-resolution: plugin.json at <cfgDir>/providers/<name>/plugin.json.
// Mirrors workflow-compute-scenarios layout where scenario-local plugins live
// under a providers/ subtree.
func TestValidate_AutoResolve_ProvidersSubdir(t *testing.T) {
	unregisterIssue756Types(t)
	t.Cleanup(func() { unregisterIssue756Types(t) })

	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "workflow.yaml", issue756ConfigBody)
	pluginDir := filepath.Join(dir, "providers", "workflow-plugin-issue756")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(issue756ManifestBody), 0644); err != nil {
		t.Fatal(err)
	}

	if err := runValidate([]string{"--allow-no-entry-points", cfgPath}); err != nil {
		t.Fatalf("validate with auto-resolved providers/ subdir: %v", err)
	}
}

// Auto-resolution: plugin.json at workspace-sibling layout.
//
//	workspace/myapp/workflow.yaml
//	workspace/workflow-plugin-issue756/plugin.json
func TestValidate_AutoResolve_WorkspaceSibling(t *testing.T) {
	unregisterIssue756Types(t)
	t.Cleanup(func() { unregisterIssue756Types(t) })

	workspace := t.TempDir()
	cfgDir := filepath.Join(workspace, "myapp", "apps", "edge")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(issue756ConfigBody), 0644); err != nil {
		t.Fatal(err)
	}

	pluginDir := filepath.Join(workspace, "workflow-plugin-issue756")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(issue756ManifestBody), 0644); err != nil {
		t.Fatal(err)
	}

	if err := runValidate([]string{"--allow-no-entry-points", cfgPath}); err != nil {
		t.Fatalf("validate with workspace-sibling layout: %v", err)
	}
}

// --plugin-manifest pointing at a path that does not exist must error so the
// operator notices a typo or missing file rather than silently validating with
// no extra types.
func TestValidate_PluginManifestFlag_MissingPathErrors(t *testing.T) {
	unregisterIssue756Types(t)
	t.Cleanup(func() { unregisterIssue756Types(t) })

	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "workflow.yaml", issue756ConfigBody)

	err := runValidate([]string{"--allow-no-entry-points", "--plugin-manifest", filepath.Join(dir, "no-such.json"), cfgPath})
	if err == nil {
		t.Fatal("expected --plugin-manifest pointing at missing path to error")
	}
}
