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

func TestParsePlanDocAcceptsClosingFrontmatterAtEOF(t *testing.T) {
	input := "---\nstatus: approved\narea: wfctl\nowner: workflow\nimplementation_refs: []\nverification:\n  last_checked: 2026-04-25\n  commands: []\n  result: pass\n---"
	doc, findings := parsePlanDoc("docs/plans/eof.md", []byte(input), fixedPlanAuditNow(), 30*24*time.Hour)
	if len(findings) != 0 {
		t.Fatalf("findings = %v", findings)
	}
	if !doc.HasFrontmatter || doc.Status != "approved" {
		t.Fatalf("unexpected doc: %+v", doc)
	}
}

func TestParsePlanDocMissingClosingFrontmatterIsInvalid(t *testing.T) {
	input := "---\nstatus: approved\narea: wfctl\nowner: workflow\n# missing closing delimiter\n"
	_, findings := parsePlanDoc("docs/plans/malformed.md", []byte(input), fixedPlanAuditNow(), 30*24*time.Hour)
	if !hasPlanFinding(findings, "ERROR", "invalid_frontmatter") {
		t.Fatalf("expected invalid_frontmatter error, got %v", findings)
	}
	if hasPlanFinding(findings, "WARN", "missing_frontmatter") {
		t.Fatalf("malformed frontmatter should not be reported as missing: %v", findings)
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

func TestValidatePlanDocsRejectsInvalidImplementationCommit(t *testing.T) {
	repo := initPlanAuditGitRepo(t)
	doc := planDoc{
		Path:           "docs/plans/a.md",
		Title:          "Bad Commit",
		Status:         "implemented",
		Area:           "wfctl",
		HasFrontmatter: true,
		ImplementationRefs: []planImplementationRef{
			{Repo: "workflow-plugin-missing", Commit: "HEAD"},
		},
	}

	findings := validatePlanDocs([]planDoc{doc}, repo)
	if !hasPlanFindingCode(findings, "invalid_implementation_commit") {
		t.Fatalf("expected invalid_implementation_commit finding, got %v", findings)
	}
}

func TestValidatePlanDocsRejectsCommitRefsWithoutRepoRoot(t *testing.T) {
	doc := planDoc{
		Path:           "docs/plans/a.md",
		Title:          "No Root",
		Status:         "implemented",
		Area:           "wfctl",
		HasFrontmatter: true,
		ImplementationRefs: []planImplementationRef{
			{Repo: "workflow", Commit: "deadbeef"},
		},
	}

	findings := validatePlanDocs([]planDoc{doc}, "")
	if !hasPlanFindingCode(findings, "invalid_plan_repo_root") {
		t.Fatalf("expected invalid_plan_repo_root finding, got %v", findings)
	}
}

func TestValidatePlanDocsRejectsUnsafeImplementationRepo(t *testing.T) {
	repo := initPlanAuditGitRepo(t)
	tests := []struct {
		name string
		ref  planImplementationRef
	}{
		{name: "current directory", ref: planImplementationRef{Repo: ".", Commit: "deadbeef"}},
		{name: "parent directory", ref: planImplementationRef{Repo: "..", Commit: "deadbeef"}},
		{name: "hidden directory", ref: planImplementationRef{Repo: ".git", Commit: "deadbeef"}},
		{name: "trailing dot", ref: planImplementationRef{Repo: "workflow.", Commit: "deadbeef"}},
		{name: "parent traversal without commit", ref: planImplementationRef{Repo: "../workflow"}},
		{name: "parent traversal", ref: planImplementationRef{Repo: "../workflow", Commit: "deadbeef"}},
		{name: "nested path", ref: planImplementationRef{Repo: "team/workflow", Commit: "deadbeef"}},
		{name: "windows path", ref: planImplementationRef{Repo: `team\workflow`, Commit: "deadbeef"}},
		{name: "absolute path", ref: planImplementationRef{Repo: filepath.Join(string(filepath.Separator), "tmp", "workflow"), Commit: "deadbeef"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := planDoc{
				Path:           "docs/plans/a.md",
				Title:          "Unsafe Repo",
				Status:         "implemented",
				Area:           "wfctl",
				HasFrontmatter: true,
				ImplementationRefs: []planImplementationRef{
					tt.ref,
				},
			}

			findings := validatePlanDocs([]planDoc{doc}, repo)
			if !hasPlanFindingCode(findings, "invalid_implementation_repo") {
				t.Fatalf("expected invalid_implementation_repo finding, got %v", findings)
			}
			if hasPlanFindingCode(findings, "missing_local_commit") {
				t.Fatalf("unsafe repo should not be checked for missing commit: %v", findings)
			}
		})
	}
}

func TestValidatePlanDocsAcceptsExistingSiblingRepoCommit(t *testing.T) {
	repo := initPlanAuditGitRepo(t)
	sibling := initPlanAuditGitRepoAt(t, filepath.Join(filepath.Dir(repo), "workflow-plugin-auth"))
	commit := strings.TrimSpace(runPlanAuditGit(t, sibling, "rev-parse", "HEAD"))
	doc := planDoc{
		Path:           "docs/plans/a.md",
		Title:          "Sibling Repo",
		Status:         "implemented",
		Area:           "wfctl",
		HasFrontmatter: true,
		ImplementationRefs: []planImplementationRef{
			{Repo: "workflow-plugin-auth", Commit: commit},
		},
	}

	findings := validatePlanDocs([]planDoc{doc}, repo)
	if hasPlanFindingCode(findings, "invalid_implementation_repo") {
		t.Fatalf("unexpected invalid_implementation_repo finding: %v", findings)
	}
	if hasPlanFindingCode(findings, "missing_local_commit") {
		t.Fatalf("unexpected missing_local_commit finding: %v", findings)
	}
}

func TestValidatePlanDocsAcceptsSiblingRepoCommitFromLinkedWorktree(t *testing.T) {
	root := t.TempDir()
	mainRepo := initPlanAuditGitRepoAt(t, filepath.Join(root, "workflow"))
	worktreeRepo := filepath.Join(mainRepo, ".worktrees", "audit")
	runPlanAuditGit(t, mainRepo, "worktree", "add", "-b", "audit-test", worktreeRepo)
	sibling := initPlanAuditGitRepoAt(t, filepath.Join(root, "workflow-plugin-auth"))
	commit := strings.TrimSpace(runPlanAuditGit(t, sibling, "rev-parse", "HEAD"))
	doc := planDoc{
		Path:           "docs/plans/a.md",
		Title:          "Sibling Repo",
		Status:         "implemented",
		Area:           "wfctl",
		HasFrontmatter: true,
		ImplementationRefs: []planImplementationRef{
			{Repo: "workflow-plugin-auth", Commit: commit},
		},
	}

	findings := validatePlanDocs([]planDoc{doc}, worktreeRepo)
	if hasPlanFindingCode(findings, "invalid_implementation_repo") || hasPlanFindingCode(findings, "missing_local_commit") {
		t.Fatalf("unexpected sibling repo findings from linked worktree: %v", findings)
	}
}

func TestPlanCommitRepoPathFailsClosed(t *testing.T) {
	for _, repoRoot := range []string{"", "relative/workflow"} {
		if path, ok := planCommitRepoPath(repoRoot, planImplementationRef{Repo: "workflow-plugin-auth"}); ok {
			t.Fatalf("planCommitRepoPath(%q) = %q, true; want false", repoRoot, path)
		}
	}
	if path, ok := planCommitRepoPath(t.TempDir(), planImplementationRef{Repo: "workflow-plugin-auth"}); ok {
		t.Fatalf("planCommitRepoPath(non-git dir) = %q, true; want false", path)
	}
}

func TestValidatePlanDocsAcceptsSafeImplementationRepos(t *testing.T) {
	repo := initPlanAuditGitRepo(t)
	tests := []planImplementationRef{
		{Repo: "workflow-plugin-auth"},
		{Repo: "buymywishlist"},
		{Repo: "workflow.plugin_auth-2"},
		{Repo: "workflow", Commit: strings.TrimSpace(runPlanAuditGit(t, repo, "rev-parse", "HEAD"))},
		{Commit: strings.TrimSpace(runPlanAuditGit(t, repo, "rev-parse", "HEAD"))},
	}

	for _, ref := range tests {
		t.Run(ref.Repo, func(t *testing.T) {
			doc := planDoc{
				Path:           "docs/plans/a.md",
				Title:          "Safe Repo",
				Status:         "implemented",
				Area:           "wfctl",
				HasFrontmatter: true,
				ImplementationRefs: []planImplementationRef{
					ref,
				},
			}

			findings := validatePlanDocs([]planDoc{doc}, repo)
			if hasPlanFindingCode(findings, "invalid_implementation_repo") {
				t.Fatalf("unexpected invalid_implementation_repo finding: %v", findings)
			}
		})
	}
}

func TestRunAuditPlansReportsUnsafeImplementationRepo(t *testing.T) {
	withFixedPlanAuditNow(t)
	repo := initPlanAuditGitRepo(t)
	dir := filepath.Join(repo, "docs/plans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir plans: %v", err)
	}
	writePlanAuditDoc(t, dir, "unsafe.md", `---
status: approved
area: wfctl
owner: workflow
implementation_refs:
  - repo: ../workflow
verification:
  last_checked: 2026-04-25
  commands: []
  result: pass
---

# Unsafe Repo
`)

	var out strings.Builder
	err := runAuditWithOutput([]string{"plans", "--dir", dir}, &out)
	if err == nil {
		t.Fatal("expected audit error")
	}
	if !strings.Contains(out.String(), "invalid_implementation_repo") {
		t.Fatalf("missing invalid_implementation_repo in output:\n%s", out.String())
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
	return initPlanAuditGitRepoAt(t, dir)
}

func initPlanAuditGitRepoAt(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
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
