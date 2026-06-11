package main

import (
	"bytes"
	"encoding/json"
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

func TestRunCapabilityCheckWarnOnly(t *testing.T) {
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
	if !strings.Contains(out.String(), "WARN") || !strings.Contains(out.String(), "tenant-evidence-missing") {
		t.Fatalf("expected warning output, got %s", out.String())
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
