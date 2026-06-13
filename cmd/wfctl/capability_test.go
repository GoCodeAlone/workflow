package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/capability/inventory"
	"github.com/GoCodeAlone/workflow/config"
)

func TestRunCapabilityUsage(t *testing.T) {
	var out bytes.Buffer
	err := runCapabilityWithOutput([]string{}, &out)
	if err == nil || !strings.Contains(out.String(), "Usage: wfctl capability") {
		t.Fatalf("expected usage, got err=%v out=%s", err, out.String())
	}
}

func TestRunCapabilityEcosystemJSON(t *testing.T) {
	var out bytes.Buffer
	err := runCapabilityWithOutput([]string{
		"ecosystem",
		"--registry", "testdata/capability/registry",
		"--repo-root", "testdata/capability/repos",
		"--taxonomy", "../../capability/inventory/testdata/taxonomy.yaml",
		"--format", "json",
	}, &out)
	if err != nil {
		t.Fatalf("runCapabilityWithOutput: %v", err)
	}
	var inv inventory.Inventory
	if err := json.Unmarshal(out.Bytes(), &inv); err != nil {
		t.Fatalf("json.Unmarshal: %v\n%s", err, out.String())
	}
	if len(inv.Capabilities) == 0 {
		t.Fatal("expected capabilities")
	}
	if inv.Metadata.Generator != "wfctl capability ecosystem" {
		t.Fatalf("generator = %q", inv.Metadata.Generator)
	}
}

func TestRunCapabilityEcosystemFiltersJSON(t *testing.T) {
	var out bytes.Buffer
	err := runCapabilityWithOutput([]string{
		"ecosystem",
		"--registry", "testdata/capability/registry",
		"--repo-root", "testdata/capability/repos",
		"--taxonomy", "../../capability/inventory/testdata/taxonomy.yaml",
		"--capability", "auth.authz",
		"--provider", "authz",
		"--format", "json",
	}, &out)
	if err != nil {
		t.Fatalf("runCapabilityWithOutput: %v", err)
	}
	var inv inventory.Inventory
	if err := json.Unmarshal(out.Bytes(), &inv); err != nil {
		t.Fatalf("json.Unmarshal: %v\n%s", err, out.String())
	}
	if len(inv.Capabilities) != 1 {
		t.Fatalf("expected one capability, got %#v", inv.Capabilities)
	}
	if inv.Capabilities[0].ID != "auth.authz" {
		t.Fatalf("capability = %q", inv.Capabilities[0].ID)
	}
	if inv.Metadata.Counts["capabilities"] != 1 {
		t.Fatalf("filtered capability count = %d", inv.Metadata.Counts["capabilities"])
	}
}

func TestRunCapabilityCatalogJSON(t *testing.T) {
	var out bytes.Buffer
	err := runCapabilityWithOutput([]string{
		"catalog",
		"--registry", "testdata/capability/registry",
		"--repo-root", "testdata/capability/repos",
		"--taxonomy", "../../capability/inventory/testdata/taxonomy.yaml",
		"--format", "json",
	}, &out)
	if err != nil {
		t.Fatalf("runCapabilityWithOutput: %v", err)
	}
	var catalog inventory.Catalog
	if err := json.Unmarshal(out.Bytes(), &catalog); err != nil {
		t.Fatalf("json.Unmarshal: %v\n%s", err, out.String())
	}
	if catalog.Metadata.Generator != "wfctl capability catalog" {
		t.Fatalf("generator = %q", catalog.Metadata.Generator)
	}
	if len(catalog.Capabilities) == 0 {
		t.Fatal("expected catalog capabilities")
	}
}

func TestRunCapabilityCatalogFiltersByCategory(t *testing.T) {
	var out bytes.Buffer
	err := runCapabilityWithOutput([]string{
		"catalog",
		"--registry", "testdata/capability/registry",
		"--repo-root", "testdata/capability/repos",
		"--taxonomy", "../../capability/inventory/testdata/taxonomy.yaml",
		"--category", "auth",
		"--format", "json",
	}, &out)
	if err != nil {
		t.Fatalf("runCapabilityWithOutput: %v", err)
	}
	var catalog inventory.Catalog
	if err := json.Unmarshal(out.Bytes(), &catalog); err != nil {
		t.Fatalf("json.Unmarshal: %v\n%s", err, out.String())
	}
	if len(catalog.Capabilities) == 0 {
		t.Fatal("expected filtered capabilities")
	}
	for _, cap := range catalog.Capabilities {
		if cap.Category != "auth" {
			t.Fatalf("unexpected category %q in %#v", cap.Category, catalog.Capabilities)
		}
	}
}

func TestRunCapabilityCrossrefsJSON(t *testing.T) {
	var out bytes.Buffer
	err := runCapabilityWithOutput([]string{
		"crossrefs",
		"--registry", "testdata/capability/registry",
		"--repo-root", "testdata/capability/repos",
		"--taxonomy", "../../capability/inventory/testdata/taxonomy.yaml",
		"--format", "json",
	}, &out)
	if err != nil {
		t.Fatalf("runCapabilityWithOutput: %v", err)
	}
	var refs inventory.CapabilityCrossrefs
	if err := json.Unmarshal(out.Bytes(), &refs); err != nil {
		t.Fatalf("json.Unmarshal: %v\n%s", err, out.String())
	}
	if len(refs.Plugins) == 0 {
		t.Fatalf("expected plugin refs, got %#v", refs)
	}
}

func TestRunCapabilityCrossrefsFiltersByPluginAlias(t *testing.T) {
	var out bytes.Buffer
	err := runCapabilityWithOutput([]string{
		"crossrefs",
		"--registry", "testdata/capability/registry",
		"--repo-root", "testdata/capability/repos",
		"--taxonomy", "../../capability/inventory/testdata/taxonomy.yaml",
		"--plugin", "workflow-plugin-authz",
		"--format", "json",
	}, &out)
	if err != nil {
		t.Fatalf("runCapabilityWithOutput: %v", err)
	}
	var refs inventory.CapabilityCrossrefs
	if err := json.Unmarshal(out.Bytes(), &refs); err != nil {
		t.Fatalf("json.Unmarshal: %v\n%s", err, out.String())
	}
	if len(refs.Plugins) != 1 {
		t.Fatalf("expected one plugin ref, got %#v", refs.Plugins)
	}
	if _, ok := refs.Plugins["workflow-plugin-authz"]; !ok {
		t.Fatalf("expected workflow-plugin-authz ref, got %#v", refs.Plugins)
	}
}

func TestRunCapabilityCrossrefsHelpListsJSONOnly(t *testing.T) {
	var out bytes.Buffer
	err := runCapabilityWithOutput([]string{"crossrefs", "-h"}, &out)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	text := out.String()
	if !strings.Contains(text, "output format: json") {
		t.Fatalf("help missing json-only format text:\n%s", text)
	}
	if strings.Contains(text, "json or md") {
		t.Fatalf("help still advertises markdown format:\n%s", text)
	}
}

func TestRunCapabilityAppJSON(t *testing.T) {
	var out bytes.Buffer
	err := runCapabilityWithOutput([]string{
		"app",
		"--manifest", "testdata/capability/app/wfctl.yaml",
		"--workflow", "testdata/capability/app/workflow.yaml",
		"--plugin-dir", "testdata/capability/app/plugins",
		"--lock-file", "testdata/capability/app/.wfctl-lock.yaml",
		"--taxonomy", "../../capability/inventory/testdata/taxonomy.yaml",
		"--format", "json",
	}, &out)
	if err != nil {
		t.Fatalf("runCapabilityWithOutput: %v", err)
	}
	var profile inventory.AppProfile
	if err := json.Unmarshal(out.Bytes(), &profile); err != nil {
		t.Fatalf("json.Unmarshal: %v\n%s", err, out.String())
	}
	if len(profile.Usage) == 0 {
		t.Fatal("expected usage")
	}
}

func TestRunCapabilityAppFiltersByCapability(t *testing.T) {
	var out bytes.Buffer
	err := runCapabilityWithOutput([]string{
		"app",
		"--manifest", "testdata/capability/app/wfctl.yaml",
		"--workflow", "testdata/capability/app/workflow.yaml",
		"--plugin-dir", "testdata/capability/app/plugins",
		"--lock-file", "testdata/capability/app/.wfctl-lock.yaml",
		"--taxonomy", "../../capability/inventory/testdata/taxonomy.yaml",
		"--capability", "secrets.management",
		"--usage", "inferred",
		"--format", "json",
	}, &out)
	if err != nil {
		t.Fatalf("runCapabilityWithOutput: %v", err)
	}
	var profile inventory.AppProfile
	if err := json.Unmarshal(out.Bytes(), &profile); err != nil {
		t.Fatalf("json.Unmarshal: %v\n%s", err, out.String())
	}
	if len(profile.Usage) != 1 {
		t.Fatalf("expected one usage row, got %#v", profile.Usage)
	}
	if profile.Usage[0].CapabilityID != "secrets.management" {
		t.Fatalf("usage capability = %q", profile.Usage[0].CapabilityID)
	}
}

func TestRunCapabilityCheckSummarizesDetectedCapabilities(t *testing.T) {
	var out bytes.Buffer
	err := runCapabilityWithOutput([]string{
		"check",
		"--manifest", "testdata/capability/healthy/wfctl.yaml",
		"--workflow", "testdata/capability/healthy/workflow.yaml",
		"--plugin-dir", "testdata/capability/healthy/plugins",
		"--lock-file", "testdata/capability/healthy/.wfctl-lock.yaml",
		"--taxonomy", "../../capability/inventory/testdata/taxonomy.yaml",
	}, &out)
	if err != nil {
		t.Fatalf("check should be warning-only, got %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "Capabilities") || !strings.Contains(text, "auth.authz") {
		t.Fatalf("expected capability summary, got %s", text)
	}
	if !strings.Contains(text, "OK no capability findings") {
		t.Fatalf("expected no-finding status, got %s", text)
	}
}

func TestRunCapabilityCheckFiltersFindings(t *testing.T) {
	var out bytes.Buffer
	err := runCapabilityWithOutput([]string{
		"check",
		"--findings-only",
		"--manifest", "testdata/capability/app/wfctl.yaml",
		"--workflow", "testdata/capability/app/workflow.yaml",
		"--plugin-dir", "testdata/capability/app/plugins",
		"--lock-file", "testdata/capability/app/.wfctl-lock.yaml",
		"--taxonomy", "../../capability/inventory/testdata/taxonomy.yaml",
		"--finding", "tenant-evidence-missing",
	}, &out)
	if err != nil {
		t.Fatalf("check should be warning-only, got %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "tenant-evidence-missing") {
		t.Fatalf("expected filtered finding, got %s", text)
	}
	if strings.Contains(text, "missing-provider") {
		t.Fatalf("unexpected unfiltered finding, got %s", text)
	}
}

func TestRunCapabilityCheckIncludesFindingsAfterSummary(t *testing.T) {
	var out bytes.Buffer
	err := runCapabilityWithOutput([]string{
		"check",
		"--manifest", "testdata/capability/app/wfctl.yaml",
		"--workflow", "testdata/capability/app/workflow.yaml",
		"--plugin-dir", "testdata/capability/app/plugins",
		"--lock-file", "testdata/capability/app/.wfctl-lock.yaml",
		"--taxonomy", "../../capability/inventory/testdata/taxonomy.yaml",
	}, &out)
	if err != nil {
		t.Fatalf("check should be warning-only, got %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "Capabilities") || !strings.Contains(text, "auth.authz") {
		t.Fatalf("expected capability summary, got %s", text)
	}
	if !strings.Contains(text, "WARN") || !strings.Contains(text, "tenant-evidence-missing") {
		t.Fatalf("expected warning output, got %s", text)
	}
}

func TestRunCapabilityCheckFindingsOnlyPreservesWarningOnlyOutput(t *testing.T) {
	var out bytes.Buffer
	err := runCapabilityWithOutput([]string{
		"check",
		"--findings-only",
		"--manifest", "testdata/capability/app/wfctl.yaml",
		"--workflow", "testdata/capability/app/workflow.yaml",
		"--plugin-dir", "testdata/capability/app/plugins",
		"--lock-file", "testdata/capability/app/.wfctl-lock.yaml",
		"--taxonomy", "../../capability/inventory/testdata/taxonomy.yaml",
	}, &out)
	if err != nil {
		t.Fatalf("check should be warning-only, got %v", err)
	}
	text := out.String()
	if strings.Contains(text, "Capabilities") {
		t.Fatalf("findings-only should not print summary, got %s", text)
	}
	if !strings.Contains(text, "WARN") || !strings.Contains(text, "tenant-evidence-missing") {
		t.Fatalf("expected warning output, got %s", text)
	}
}

func TestEmbeddedCLIRegistersCapability(t *testing.T) {
	if _, ok := commands["capability"]; !ok {
		t.Fatal("commands does not register capability")
	}
	cfg, err := config.LoadFromBytes(wfctlConfigBytes)
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	workflow, ok := cfg.Workflows["cli"].(map[string]any)
	if !ok {
		t.Fatal("cli workflow missing")
	}
	commands, ok := workflow["commands"].([]any)
	if !ok {
		t.Fatalf("commands has type %T", workflow["commands"])
	}
	for _, command := range commands {
		entry, ok := command.(map[string]any)
		if ok && entry["name"] == "capability" {
			return
		}
	}
	t.Fatal("embedded CLI config does not list capability")
}

func TestCapabilityGeneratedArtifacts(t *testing.T) {
	data, err := os.ReadFile("../../docs/generated/capabilities/ecosystem.json")
	if err != nil {
		t.Fatalf("read ecosystem.json: %v", err)
	}
	var inv inventory.Inventory
	if err := json.Unmarshal(data, &inv); err != nil {
		t.Fatalf("json.Unmarshal ecosystem.json: %v", err)
	}
	if inv.Metadata.TaxonomyDigest == "" {
		t.Fatal("expected taxonomy digest")
	}
	if inv.Metadata.WorkflowVersion == "" {
		t.Fatal("expected workflow version")
	}
	if inv.Metadata.Counts["capabilities"] == 0 {
		t.Fatalf("expected capabilities count, got %#v", inv.Metadata.Counts)
	}

	md, err := os.ReadFile("../../docs/generated/capabilities/ecosystem.md")
	if err != nil {
		t.Fatalf("read ecosystem.md: %v", err)
	}
	text := string(md)
	if !strings.Contains(text, "# Workflow Capability Matrix") {
		t.Fatalf("markdown missing title: %s", text)
	}
	if !strings.Contains(text, "auth") {
		t.Fatalf("markdown missing known category: %s", text)
	}
}
