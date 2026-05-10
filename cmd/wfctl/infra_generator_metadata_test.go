package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestCollectInstalledIaCPluginVersions_EmptyDir verifies that an absent or
// empty plugin directory returns a nil (not empty) slice without error.
func TestCollectInstalledIaCPluginVersions_EmptyDir(t *testing.T) {
	t.Setenv("WFCTL_PLUGIN_DIR", t.TempDir())
	infos := collectInstalledIaCPluginVersions()
	if len(infos) != 0 {
		t.Errorf("expected no plugins, got %v", infos)
	}
}

// TestCollectInstalledIaCPluginVersions_NoIaCProvider verifies that plugins
// without an iacProvider capability are excluded from the result.
func TestCollectInstalledIaCPluginVersions_NoIaCProvider(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WFCTL_PLUGIN_DIR", dir)

	pluginDir := filepath.Join(dir, "workflow-plugin-auth")
	if err := os.MkdirAll(pluginDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := `{"name":"workflow-plugin-auth","version":"1.2.0","capabilities":{}}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	infos := collectInstalledIaCPluginVersions()
	if len(infos) != 0 {
		t.Errorf("expected no IaC plugins, got %v", infos)
	}
}

// TestCollectInstalledIaCPluginVersions_WithIaCProvider verifies that a plugin
// declaring an iacProvider capability is included in the result with its
// correct name and version.
func TestCollectInstalledIaCPluginVersions_WithIaCProvider(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WFCTL_PLUGIN_DIR", dir)

	pluginDir := filepath.Join(dir, "workflow-plugin-aws")
	if err := os.MkdirAll(pluginDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := `{
		"name": "workflow-plugin-aws",
		"version": "2.3.1",
		"capabilities": {
			"iacProvider": {"name": "aws"}
		}
	}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	infos := collectInstalledIaCPluginVersions()
	if len(infos) != 1 {
		t.Fatalf("expected 1 plugin, got %d: %v", len(infos), infos)
	}
	if infos[0].Name != "workflow-plugin-aws" {
		t.Errorf("unexpected name %q", infos[0].Name)
	}
	if infos[0].Version != "2.3.1" {
		t.Errorf("unexpected version %q", infos[0].Version)
	}
}

// TestCollectInstalledIaCPluginVersions_MixedPlugins verifies that only
// IaC-provider plugins are included when multiple plugin types are installed.
func TestCollectInstalledIaCPluginVersions_MixedPlugins(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WFCTL_PLUGIN_DIR", dir)

	writePlugin := func(subdir, name, version, iacProviderName string) {
		t.Helper()
		pd := filepath.Join(dir, subdir)
		if err := os.MkdirAll(pd, 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", subdir, err)
		}
		iacCap := ""
		if iacProviderName != "" {
			iacCap = `"iacProvider": {"name": "` + iacProviderName + `"}`
		}
		m := `{"name":"` + name + `","version":"` + version + `","capabilities":{` + iacCap + `}}`
		if err := os.WriteFile(filepath.Join(pd, "plugin.json"), []byte(m), 0o600); err != nil {
			t.Fatalf("write %s manifest: %v", subdir, err)
		}
	}

	writePlugin("plugin-aws", "workflow-plugin-aws", "1.0.0", "aws")
	writePlugin("plugin-auth", "workflow-plugin-auth", "0.5.0", "")
	writePlugin("plugin-gcp", "workflow-plugin-gcp", "3.1.0", "gcp")

	infos := collectInstalledIaCPluginVersions()
	if len(infos) != 2 {
		t.Fatalf("expected 2 IaC plugins, got %d: %v", len(infos), infos)
	}
	names := map[string]string{}
	for _, p := range infos {
		names[p.Name] = p.Version
	}
	if names["workflow-plugin-aws"] != "1.0.0" {
		t.Errorf("aws version mismatch: %q", names["workflow-plugin-aws"])
	}
	if names["workflow-plugin-gcp"] != "3.1.0" {
		t.Errorf("gcp version mismatch: %q", names["workflow-plugin-gcp"])
	}
	if _, ok := names["workflow-plugin-auth"]; ok {
		t.Error("non-IaC plugin should not be included")
	}
}

// TestBuildGeneratorMetadata_WfctlVersion verifies that buildGeneratorMetadata
// always populates WfctlVersion with the binary's version string.
func TestBuildGeneratorMetadata_WfctlVersion(t *testing.T) {
	// Point to an empty plugin dir so plugin scanning is deterministic.
	t.Setenv("WFCTL_PLUGIN_DIR", t.TempDir())
	meta := buildGeneratorMetadata()
	if meta.WfctlVersion == "" {
		t.Error("WfctlVersion must not be empty")
	}
}

// TestFsWfctlStateStore_SaveAndReadMetadata verifies that SaveMetadata writes
// a metadata.json file wrapped under "generator_metadata" and that
// ListResources does not mistake it for a resource state record.
func TestFsWfctlStateStore_SaveAndReadMetadata(t *testing.T) {
	dir := t.TempDir()
	store := &fsWfctlStateStore{dir: dir}

	meta := interfaces.GeneratorMetadata{
		WfctlVersion: "v1.2.3",
		Plugins: []interfaces.PluginVersionInfo{
			{Name: "workflow-plugin-aws", Version: "2.0.0"},
		},
	}
	if err := store.SaveMetadata(context.Background(), meta); err != nil {
		t.Fatalf("SaveMetadata: %v", err)
	}

	// Verify the file was written with the "generator_metadata" wrapper.
	data, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		t.Fatalf("read metadata.json: %v", err)
	}
	var wrapper struct {
		GeneratorMetadata interfaces.GeneratorMetadata `json:"generator_metadata"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		t.Fatalf("unmarshal metadata.json: %v", err)
	}
	got := wrapper.GeneratorMetadata
	if got.WfctlVersion != "v1.2.3" {
		t.Errorf("WfctlVersion: got %q, want %q", got.WfctlVersion, "v1.2.3")
	}
	if len(got.Plugins) != 1 || got.Plugins[0].Name != "workflow-plugin-aws" {
		t.Errorf("unexpected Plugins: %v", got.Plugins)
	}

	// The "generator_metadata" key must be present at the top level.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	if _, ok := raw["generator_metadata"]; !ok {
		t.Error("metadata.json must have a top-level generator_metadata key")
	}

	// ListResources must not return the metadata file as a resource.
	states, err := store.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(states) != 0 {
		t.Errorf("expected no resources, got %d (metadata.json must be skipped)", len(states))
	}
}

// TestFsWfctlStateStore_MetadataOverwritten verifies that calling SaveMetadata
// twice overwrites the previous file (the file reflects the most-recent run).
func TestFsWfctlStateStore_MetadataOverwritten(t *testing.T) {
	dir := t.TempDir()
	store := &fsWfctlStateStore{dir: dir}

	first := interfaces.GeneratorMetadata{WfctlVersion: "v1.0.0"}
	if err := store.SaveMetadata(context.Background(), first); err != nil {
		t.Fatalf("SaveMetadata (first): %v", err)
	}
	second := interfaces.GeneratorMetadata{WfctlVersion: "v2.0.0"}
	if err := store.SaveMetadata(context.Background(), second); err != nil {
		t.Fatalf("SaveMetadata (second): %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		t.Fatalf("read metadata.json: %v", err)
	}
	var wrapper struct {
		GeneratorMetadata interfaces.GeneratorMetadata `json:"generator_metadata"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if wrapper.GeneratorMetadata.WfctlVersion != "v2.0.0" {
		t.Errorf("expected v2.0.0 (most-recent), got %q", wrapper.GeneratorMetadata.WfctlVersion)
	}
}

// TestIaCPlanGeneratorMetadata_RoundTrip verifies that GeneratorMetadata
// is preserved across JSON marshal/unmarshal of an IaCPlan (plan.json format).
func TestIaCPlanGeneratorMetadata_RoundTrip(t *testing.T) {
	meta := &interfaces.GeneratorMetadata{
		WfctlVersion: "v0.42.1",
		Plugins: []interfaces.PluginVersionInfo{
			{Name: "workflow-plugin-aws", Version: "3.1.0"},
			{Name: "workflow-plugin-gcp", Version: "1.0.5"},
		},
	}
	plan := interfaces.IaCPlan{
		ID:                "plan-123",
		GeneratorMetadata: meta,
	}

	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got interfaces.IaCPlan
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.GeneratorMetadata == nil {
		t.Fatal("GeneratorMetadata was nil after round-trip")
	}
	if got.GeneratorMetadata.WfctlVersion != "v0.42.1" {
		t.Errorf("WfctlVersion: got %q", got.GeneratorMetadata.WfctlVersion)
	}
	if len(got.GeneratorMetadata.Plugins) != 2 {
		t.Errorf("Plugins len: got %d", len(got.GeneratorMetadata.Plugins))
	}
}

// TestIaCPlanGeneratorMetadata_OmitEmpty verifies that when GeneratorMetadata
// is nil (e.g., a plan loaded from an older wfctl version), the JSON output
// does not include the "generator_metadata" key.
func TestIaCPlanGeneratorMetadata_OmitEmpty(t *testing.T) {
	plan := interfaces.IaCPlan{ID: "plan-456"}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) == "" {
		t.Fatal("expected non-empty JSON")
	}
	// The key must be absent, not null, when GeneratorMetadata is nil.
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	if _, ok := m["generator_metadata"]; ok {
		t.Error("generator_metadata must be absent when nil (omitempty)")
	}
}
