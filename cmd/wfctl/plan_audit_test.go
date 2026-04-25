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

	doc, findings := parsePlanDoc("docs/plans/example.md", []byte(input), fixedPlanAuditNow(), 30*24*time.Hour)
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
	doc, findings := parsePlanDoc("docs/plans/legacy.md", []byte("# Legacy Plan\n"), fixedPlanAuditNow(), 30*24*time.Hour)
	if doc.Title != "Legacy Plan" {
		t.Fatalf("title = %q", doc.Title)
	}
	if !hasPlanFinding(findings, "WARN", "missing_frontmatter") {
		t.Fatalf("expected missing frontmatter warning, got %v", findings)
	}
}

func TestParsePlanDocCRLFFrontmatter(t *testing.T) {
	input := "---\r\nstatus: approved\r\narea: wfctl\r\nowner: workflow\r\nimplementation_refs: []\r\nverification:\r\n  last_checked: 2026-04-25\r\n  commands: []\r\n  result: pass\r\n---\r\n\r\n# CRLF Plan\r\n"
	doc, findings := parsePlanDoc("docs/plans/crlf.md", []byte(input), fixedPlanAuditNow(), 30*24*time.Hour)
	if len(findings) != 0 {
		t.Fatalf("findings = %v", findings)
	}
	if !doc.HasFrontmatter || doc.Title != "CRLF Plan" || doc.Status != "approved" {
		t.Fatalf("unexpected doc: %+v", doc)
	}
}

func TestParsePlanDocAcceptsScenariosArea(t *testing.T) {
	input := `---
status: approved
area: scenarios
owner: workflow
implementation_refs: []
verification:
  last_checked: 2026-04-25
  commands: []
  result: pass
---

# Scenario Plan
`

	doc, findings := parsePlanDoc("docs/plans/scenario.md", []byte(input), fixedPlanAuditNow(), 30*24*time.Hour)
	if len(findings) != 0 {
		t.Fatalf("findings = %v", findings)
	}
	if doc.Area != "scenarios" {
		t.Fatalf("area = %q", doc.Area)
	}
}

func TestValidatePlanDocFindings(t *testing.T) {
	now := fixedPlanAuditNow()
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

	_, findings := parsePlanDoc("docs/plans/missing.md", []byte(input), fixedPlanAuditNow(), 30*24*time.Hour)
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

func TestValidatePlanDocsSkipsUnavailableSiblingRepoCommit(t *testing.T) {
	repo := initPlanAuditGitRepo(t)
	doc := planDoc{
		Path:           "docs/plans/a.md",
		Title:          "External",
		Status:         "implemented",
		Area:           "wfctl",
		HasFrontmatter: true,
		ImplementationRefs: []planImplementationRef{
			{Repo: "workflow-plugin-missing", Commit: "deadbeef"},
		},
	}

	findings := validatePlanDocs([]planDoc{doc}, repo)
	if hasPlanFindingCode(findings, "missing_local_commit") {
		t.Fatalf("unexpected missing commit for unavailable sibling repo: %v", findings)
	}
}

func TestRenderPlanIndex(t *testing.T) {
	docs := []planDoc{
		{
			Path:         "docs/plans/a.md",
			Title:        "A",
			Status:       "implemented",
			Area:         "wfctl",
			Owner:        "workflow",
			ExternalRefs: []string{"#15"},
			Verification: planVerification{Result: "pass", LastChecked: "2026-04-25"},
		},
		{
			Path:         "docs/plans/b.md",
			Title:        "B",
			Status:       "approved",
			Area:         "plugins",
			Owner:        "workflow",
			ExternalRefs: []string{"#76"},
			Verification: planVerification{Result: "partial", LastChecked: "2026-04-25"},
		},
	}

	got := renderPlanIndex(docs)
	for _, want := range []string{"# Plans Index", "| A |", "| implemented |", "| plugins |", "#76"} {
		if !strings.Contains(got, want) {
			t.Fatalf("index missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "](docs/plans/") {
		t.Fatalf("index contains non-relative plan link:\n%s", got)
	}
	if !strings.Contains(got, "[a.md](a.md)") {
		t.Fatalf("index missing basename link:\n%s", got)
	}
}

func TestWritePlanIndexFixture(t *testing.T) {
	if os.Getenv("WFCTL_WRITE_PLAN_INDEX") != "1" {
		t.Skip("set WFCTL_WRITE_PLAN_INDEX=1 to regenerate docs/plans/INDEX.md")
	}
	root := discoverPlanAuditRepoRoot(".")
	if root == "" {
		t.Fatal("could not discover repo root")
	}
	plansDir := filepath.Join(root, "docs/plans")
	docs, findings, err := collectPlanDocs(plansDir, fixedPlanAuditNow(), 30*24*time.Hour)
	if err != nil {
		t.Fatalf("collect plans: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("no plan docs collected")
	}
	_ = findings
	if err := os.WriteFile(filepath.Join(plansDir, "INDEX.md"), []byte(renderPlanIndex(docs)), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
}

func fixedPlanAuditNow() time.Time {
	return time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC)
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
