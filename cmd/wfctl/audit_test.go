package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAuditPlansReportsFindings(t *testing.T) {
	dir := t.TempDir()
	writePlanAuditDoc(t, dir, "legacy.md", "# Legacy\n")

	var out bytes.Buffer
	err := runAuditWithOutput([]string{"plans", "--dir", dir}, &out)
	if err != nil {
		t.Fatalf("run audit plans: %v", err)
	}
	if !strings.Contains(out.String(), "WARN") || !strings.Contains(out.String(), "missing_frontmatter") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func TestRunAuditPlansReportsInitialDesignFindings(t *testing.T) {
	repo := initPlanAuditGitRepo(t)
	dir := filepath.Join(repo, "docs/plans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir plans: %v", err)
	}
	writePlanAuditDoc(t, dir, "a.md", `---
status: done
area: unknown
owner: workflow
implementation_refs:
  - repo: workflow
    commit: deadbeef
verification:
  last_checked: 2026-02-01
  commands: []
  result: pass
superseded_by:
  - docs/plans/missing.md
---

# Duplicate
`)
	writePlanAuditDoc(t, dir, "b.md", `---
status: planned
area: unknown
owner: workflow
implementation_refs: []
verification:
  last_checked: 2026-04-25
  commands: []
  result: pass
---

# Duplicate
`)
	writePlanAuditDoc(t, dir, "d.md", `---
status: approved
area: unknown
owner: workflow
implementation_refs: []
verification:
  last_checked: 2026-04-25
  commands: []
  result: pass
---

# Duplicate
`)
	writePlanAuditDoc(t, dir, "c.md", `---
status: implemented
area: wfctl
owner: workflow
implementation_refs:
  - repo: workflow
    commit: deadbeef
verification:
  last_checked: 2026-04-25
  commands: []
  result: pass
---

# Missing Evidence
`)

	var out bytes.Buffer
	err := runAuditWithOutput([]string{"plans", "--dir", dir}, &out)
	if err == nil {
		t.Fatal("expected audit errors")
	}
	for _, want := range []string{
		"invalid_status",
		"invalid_area",
		"implemented_without_verification",
		"stale_verification",
		"broken_superseded_by",
		"duplicate_active_design",
		"missing_local_commit",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %s in output:\n%s", want, out.String())
		}
	}
}

func TestRunAuditPlansJSON(t *testing.T) {
	dir := t.TempDir()
	writePlanAuditDoc(t, dir, "legacy.md", "# Legacy\n")

	var out bytes.Buffer
	err := runAuditWithOutput([]string{"plans", "--dir", dir, "--json"}, &out)
	if err != nil {
		t.Fatalf("run audit plans json: %v", err)
	}
	var report planAuditReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if len(report.Findings) != 1 || report.Findings[0].Code != "missing_frontmatter" {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestRunAuditPlansFixIndex(t *testing.T) {
	dir := t.TempDir()
	writePlanAuditDoc(t, dir, "tracked.md", `---
status: approved
area: wfctl
owner: workflow
implementation_refs: []
verification:
  last_checked: 2026-04-25
  commands:
    - GOWORK=off go test ./cmd/wfctl
  result: pass
---

# Tracked
`)

	var out bytes.Buffer
	err := runAuditWithOutput([]string{"plans", "--dir", dir, "--fix-index"}, &out)
	if err != nil {
		t.Fatalf("run audit plans fix-index: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "INDEX.md"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if !strings.Contains(string(data), "| Tracked |") {
		t.Fatalf("unexpected index:\n%s", data)
	}
}

func TestAuditCommandRouting(t *testing.T) {
	if _, ok := commands["audit"]; !ok {
		t.Fatal("audit command not registered")
	}
	var out bytes.Buffer
	err := runAuditWithOutput([]string{"unknown"}, &out)
	if err == nil || !strings.Contains(err.Error(), "unknown audit subcommand") {
		t.Fatalf("expected unknown subcommand error, got %v", err)
	}
}

func TestRunAuditPlugins(t *testing.T) {
	root := t.TempDir()
	writePluginAuditRepoAt(t, root, "workflow-plugin-good", `{
  "name": "workflow-plugin-good",
  "version": "0.1.0",
  "capabilities": {}
}`)
	writePluginAuditRepoAt(t, root, "workflow-plugin-legacy", `{
  "name": "workflow-plugin-legacy",
  "version": "0.1.0",
  "moduleTypes": ["legacy.module"]
}`)
	missing := filepath.Join(root, "workflow-plugin-missing")
	if err := os.MkdirAll(missing, 0o755); err != nil {
		t.Fatalf("mkdir missing plugin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(missing, "go.mod"), []byte("module example.com/workflow-plugin-missing\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	var out bytes.Buffer
	err := runAuditWithOutput([]string{"plugins", "--repo-root", root}, &out)
	if err != nil {
		t.Fatalf("audit plugins: %v", err)
	}
	for _, want := range []string{"canonical", "legacy", "missing", "missing_plugin_manifest"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in output:\n%s", want, out.String())
		}
	}
}

func TestRunAuditPluginsJSON(t *testing.T) {
	root := t.TempDir()
	writePluginAuditRepoAt(t, root, "workflow-plugin-good", `{
  "name": "workflow-plugin-good",
  "version": "0.1.0",
  "capabilities": {}
}`)

	var out bytes.Buffer
	err := runAuditWithOutput([]string{"plugins", "--repo-root", root, "--json"}, &out)
	if err != nil {
		t.Fatalf("audit plugins json: %v", err)
	}
	var report pluginAuditReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if report.Summary.Total != 1 || report.Summary.Canonical != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestRunAuditPluginsStrictFailsOnWarnings(t *testing.T) {
	root := t.TempDir()
	writePluginAuditRepoAt(t, root, "workflow-plugin-legacy", `{
  "name": "workflow-plugin-legacy",
  "version": "0.1.0",
  "moduleTypes": ["legacy.module"]
}`)

	var out bytes.Buffer
	err := runAuditWithOutput([]string{"plugins", "--repo-root", root, "--strict"}, &out)
	if err == nil {
		t.Fatal("expected strict audit failure")
	}
	if !strings.Contains(err.Error(), "plugin audit finding") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writePlanAuditDoc(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write plan doc: %v", err)
	}
}
