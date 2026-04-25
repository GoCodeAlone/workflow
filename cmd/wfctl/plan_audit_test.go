package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParsePlanDocFrontmatter(t *testing.T) {
	input := `---
status: implemented
area: wfctl
owner: workflow
implementation_refs:
  - repo: workflow
    commit: abc123
external_refs:
  - "#15"
verification:
  last_checked: 2026-04-25
  commands:
    - GOWORK=off go test ./cmd/wfctl
  result: pass
supersedes: []
superseded_by: []
---

# Example Design
`

	doc, findings := parsePlanDoc("docs/plans/example.md", []byte(input), planAuditNow(), 30*24*time.Hour)
	if len(findings) != 0 {
		t.Fatalf("findings = %v", findings)
	}
	if doc.Status != "implemented" || doc.Area != "wfctl" || doc.Title != "Example Design" {
		t.Fatalf("unexpected doc: %+v", doc)
	}
	if len(doc.ExternalRefs) != 1 || doc.ExternalRefs[0] != "#15" {
		t.Fatalf("external refs = %v", doc.ExternalRefs)
	}
	if !doc.HasFrontmatter {
		t.Fatal("expected frontmatter")
	}
}

func TestParsePlanDocLegacyWarning(t *testing.T) {
	doc, findings := parsePlanDoc("docs/plans/legacy.md", []byte("# Legacy Plan\n"), planAuditNow(), 30*24*time.Hour)
	if doc.Title != "Legacy Plan" {
		t.Fatalf("title = %q", doc.Title)
	}
	if !hasPlanFinding(findings, "WARN", "missing_frontmatter") {
		t.Fatalf("expected missing frontmatter warning, got %v", findings)
	}
}

func TestValidatePlanDocFindings(t *testing.T) {
	now := planAuditNow()
	input := `---
status: done
area: unknown
owner: workflow
implementation_refs: []
verification:
  last_checked: 2026-02-01
  commands: []
  result: pass
---

# Bad Plan
`

	_, findings := parsePlanDoc("docs/plans/bad.md", []byte(input), now, 30*24*time.Hour)
	for _, want := range []string{"invalid_status", "invalid_area", "stale_verification"} {
		if !hasPlanFindingCode(findings, want) {
			t.Fatalf("expected %s finding, got %v", want, findings)
		}
	}
}

func TestValidatePlanDocImplementedRequiresEvidence(t *testing.T) {
	input := `---
status: implemented
area: wfctl
owner: workflow
implementation_refs: []
verification:
  last_checked: 2026-04-25
  commands: []
  result: pass
---

# Missing Evidence
`

	_, findings := parsePlanDoc("docs/plans/missing.md", []byte(input), planAuditNow(), 30*24*time.Hour)
	for _, want := range []string{"implemented_without_refs", "implemented_without_verification"} {
		if !hasPlanFindingCode(findings, want) {
			t.Fatalf("expected %s finding, got %v", want, findings)
		}
	}
}

func TestValidatePlanDocsReferencesDuplicatesAndCommits(t *testing.T) {
	repo := initPlanAuditGitRepo(t)
	docA := planDoc{
		Path:           "docs/plans/a.md",
		Title:          "Duplicate",
		Status:         "approved",
		Area:           "wfctl",
		SupersededBy:   []string{"docs/plans/missing.md"},
		HasFrontmatter: true,
		ImplementationRefs: []planImplementationRef{
			{Repo: "workflow", Commit: "deadbeef"},
		},
	}
	docB := planDoc{
		Path:           "docs/plans/b.md",
		Title:          "Duplicate",
		Status:         "planned",
		Area:           "wfctl",
		HasFrontmatter: true,
	}

	findings := validatePlanDocs([]planDoc{docA, docB}, repo)
	for _, want := range []string{"broken_superseded_by", "duplicate_active_design", "missing_local_commit"} {
		if !hasPlanFindingCode(findings, want) {
			t.Fatalf("expected %s finding, got %v", want, findings)
		}
	}
}

func TestValidatePlanDocsAcceptsExistingCommitAndSupersession(t *testing.T) {
	repo := initPlanAuditGitRepo(t)
	commit := strings.TrimSpace(runPlanAuditGit(t, repo, "rev-parse", "HEAD"))
	docA := planDoc{
		Path:           "docs/plans/a.md",
		Title:          "Old",
		Status:         "superseded",
		Area:           "wfctl",
		SupersededBy:   []string{"docs/plans/b.md"},
		HasFrontmatter: true,
		ImplementationRefs: []planImplementationRef{
			{Repo: "workflow", Commit: commit},
		},
	}
	docB := planDoc{Path: "docs/plans/b.md", Title: "New", Status: "approved", Area: "wfctl", HasFrontmatter: true}

	findings := validatePlanDocs([]planDoc{docA, docB}, repo)
	if len(findings) != 0 {
		t.Fatalf("findings = %v", findings)
	}
}

func hasPlanFinding(findings []planFinding, level, code string) bool {
	for _, finding := range findings {
		if finding.Level == level && finding.Code == code {
			return true
		}
	}
	return false
}

func hasPlanFindingCode(findings []planFinding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func initPlanAuditGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runPlanAuditGit(t, dir, "init")
	runPlanAuditGit(t, dir, "config", "user.email", "test@example.com")
	runPlanAuditGit(t, dir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	runPlanAuditGit(t, dir, "add", "README.md")
	runPlanAuditGit(t, dir, "commit", "-m", "initial")
	return dir
}

func runPlanAuditGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
