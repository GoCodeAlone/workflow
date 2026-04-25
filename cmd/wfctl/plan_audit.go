package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type planDoc struct {
	Path               string
	Title              string
	Status             string
	Area               string
	Owner              string
	ImplementationRefs []planImplementationRef
	ExternalRefs       []string
	Verification       planVerification
	Supersedes         []string
	SupersededBy       []string
	HasFrontmatter     bool
}

type planImplementationRef struct {
	Repo   string `yaml:"repo" json:"repo"`
	PR     string `yaml:"pr" json:"pr"`
	Commit string `yaml:"commit" json:"commit"`
}

type planVerification struct {
	LastChecked string   `yaml:"last_checked" json:"last_checked"`
	Commands    []string `yaml:"commands" json:"commands"`
	Result      string   `yaml:"result" json:"result"`
}

type planFinding struct {
	Path    string `json:"path"`
	Level   string `json:"level"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type planFrontmatter struct {
	Status             string                  `yaml:"status"`
	Area               string                  `yaml:"area"`
	Owner              string                  `yaml:"owner"`
	ImplementationRefs []planImplementationRef `yaml:"implementation_refs"`
	ExternalRefs       []string                `yaml:"external_refs"`
	Verification       planVerification        `yaml:"verification"`
	Supersedes         []string                `yaml:"supersedes"`
	SupersededBy       []string                `yaml:"superseded_by"`
}

var validPlanStatuses = map[string]bool{
	"proposed":    true,
	"approved":    true,
	"planned":     true,
	"in_progress": true,
	"implemented": true,
	"superseded":  true,
	"abandoned":   true,
}

var validPlanAreas = map[string]bool{
	"ecosystem": true,
	"wfctl":     true,
	"plugins":   true,
	"editor":    true,
	"cloud":     true,
	"ide":       true,
	"core":      true,
	"runtime":   true,
	"workflow":  true,
	"bmw":       true,
}

func planAuditNow() time.Time {
	return time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC)
}

func parsePlanDoc(path string, data []byte, now time.Time, staleAfter time.Duration) (planDoc, []planFinding) {
	doc := planDoc{Path: path, Title: firstMarkdownTitle(data)}
	var findings []planFinding

	frontmatter, body, ok := splitPlanFrontmatter(data)
	if !ok {
		findings = append(findings, planFinding{
			Path:    path,
			Level:   "WARN",
			Code:    "missing_frontmatter",
			Message: "document has no YAML frontmatter",
		})
		if doc.Title == "" {
			doc.Title = firstMarkdownTitle(body)
		}
		return doc, findings
	}

	doc.HasFrontmatter = true
	if title := firstMarkdownTitle(body); title != "" {
		doc.Title = title
	}

	var meta planFrontmatter
	if err := yaml.Unmarshal(frontmatter, &meta); err != nil {
		findings = append(findings, planFinding{
			Path:    path,
			Level:   "ERROR",
			Code:    "invalid_frontmatter",
			Message: fmt.Sprintf("parse frontmatter: %v", err),
		})
		return doc, findings
	}

	doc.Status = meta.Status
	doc.Area = meta.Area
	doc.Owner = meta.Owner
	doc.ImplementationRefs = meta.ImplementationRefs
	doc.ExternalRefs = meta.ExternalRefs
	doc.Verification = meta.Verification
	doc.Supersedes = meta.Supersedes
	doc.SupersededBy = meta.SupersededBy

	findings = append(findings, validatePlanDoc(doc, now, staleAfter)...)
	return doc, findings
}

func validatePlanDoc(doc planDoc, now time.Time, staleAfter time.Duration) []planFinding {
	var findings []planFinding
	if doc.Status != "" && !validPlanStatuses[doc.Status] {
		findings = append(findings, planFinding{
			Path:    doc.Path,
			Level:   "ERROR",
			Code:    "invalid_status",
			Message: fmt.Sprintf("invalid status %q", doc.Status),
		})
	}
	if doc.Area != "" && !validPlanAreas[doc.Area] {
		findings = append(findings, planFinding{
			Path:    doc.Path,
			Level:   "ERROR",
			Code:    "invalid_area",
			Message: fmt.Sprintf("invalid area %q", doc.Area),
		})
	}
	if doc.Status == "implemented" {
		if len(doc.ImplementationRefs) == 0 {
			findings = append(findings, planFinding{
				Path:    doc.Path,
				Level:   "ERROR",
				Code:    "implemented_without_refs",
				Message: "implemented document has no implementation refs",
			})
		}
		if len(doc.Verification.Commands) == 0 {
			findings = append(findings, planFinding{
				Path:    doc.Path,
				Level:   "ERROR",
				Code:    "implemented_without_verification",
				Message: "implemented document has no verification commands",
			})
		}
	}
	if doc.Verification.LastChecked != "" && staleAfter > 0 {
		checked, err := time.Parse("2006-01-02", doc.Verification.LastChecked)
		if err != nil {
			findings = append(findings, planFinding{
				Path:    doc.Path,
				Level:   "ERROR",
				Code:    "invalid_verification_date",
				Message: fmt.Sprintf("invalid verification date %q", doc.Verification.LastChecked),
			})
		} else if now.Sub(checked) > staleAfter {
			findings = append(findings, planFinding{
				Path:    doc.Path,
				Level:   "WARN",
				Code:    "stale_verification",
				Message: fmt.Sprintf("verification last checked on %s", doc.Verification.LastChecked),
			})
		}
	}
	return findings
}

func validatePlanDocs(docs []planDoc, repoRoot string) []planFinding {
	var findings []planFinding
	paths := make(map[string]bool, len(docs))
	active := make(map[string]string)

	for _, doc := range docs {
		paths[doc.Path] = true
		paths[filepath.Base(doc.Path)] = true
	}

	for _, doc := range docs {
		for _, target := range doc.SupersededBy {
			if !paths[target] {
				findings = append(findings, planFinding{
					Path:    doc.Path,
					Level:   "ERROR",
					Code:    "broken_superseded_by",
					Message: fmt.Sprintf("superseded_by target %q was not found", target),
				})
			}
		}

		if isActivePlanStatus(doc.Status) {
			key := strings.ToLower(strings.TrimSpace(doc.Area + "/" + doc.Title))
			if prev, ok := active[key]; ok {
				findings = append(findings, planFinding{
					Path:    doc.Path,
					Level:   "WARN",
					Code:    "duplicate_active_design",
					Message: fmt.Sprintf("duplicates active design %s", prev),
				})
			} else {
				active[key] = doc.Path
			}
		}

		for _, ref := range doc.ImplementationRefs {
			if ref.Commit == "" || repoRoot == "" {
				continue
			}
			if !planCommitExists(repoRoot, ref) {
				findings = append(findings, planFinding{
					Path:    doc.Path,
					Level:   "ERROR",
					Code:    "missing_local_commit",
					Message: fmt.Sprintf("commit %s not found for repo %q", ref.Commit, ref.Repo),
				})
			}
		}
	}

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Code < findings[j].Code
	})
	return findings
}

func collectPlanDocs(dir string, now time.Time, staleAfter time.Duration) ([]planDoc, []planFinding, error) {
	var docs []planDoc
	var findings []planFinding
	repoRoot := discoverPlanAuditRepoRoot(dir)
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" || filepath.Base(path) == "INDEX.md" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		docPath := path
		if repoRoot != "" {
			if rel, err := filepath.Rel(repoRoot, path); err == nil {
				docPath = rel
			}
		}
		doc, docFindings := parsePlanDoc(filepath.ToSlash(docPath), data, now, staleAfter)
		docs = append(docs, doc)
		findings = append(findings, docFindings...)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	findings = append(findings, validatePlanDocs(docs, repoRoot)...)
	sortPlanDocs(docs)
	return docs, findings, nil
}

func renderPlanIndex(docs []planDoc) string {
	docs = append([]planDoc(nil), docs...)
	sortPlanDocs(docs)

	var b strings.Builder
	b.WriteString("# Plans Index\n\n")
	b.WriteString("Generated by `wfctl audit plans --fix-index`. Do not edit by hand.\n\n")
	b.WriteString("| Title | Filename | Area | Status | Owner | External Refs | Implementation Refs | Last Checked | Verification | Supersedes | Superseded By |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, doc := range docs {
		b.WriteString("| ")
		b.WriteString(markdownCell(doc.Title))
		b.WriteString(" | ")
		b.WriteString(markdownCell(markdownLink(filepath.Base(doc.Path), doc.Path)))
		b.WriteString(" | ")
		b.WriteString(markdownCell(doc.Area))
		b.WriteString(" | ")
		b.WriteString(markdownCell(doc.Status))
		b.WriteString(" | ")
		b.WriteString(markdownCell(doc.Owner))
		b.WriteString(" | ")
		b.WriteString(markdownCell(strings.Join(doc.ExternalRefs, ", ")))
		b.WriteString(" | ")
		b.WriteString(markdownCell(formatPlanImplementationRefs(doc.ImplementationRefs)))
		b.WriteString(" | ")
		b.WriteString(markdownCell(doc.Verification.LastChecked))
		b.WriteString(" | ")
		b.WriteString(markdownCell(doc.Verification.Result))
		b.WriteString(" | ")
		b.WriteString(markdownCell(strings.Join(doc.Supersedes, ", ")))
		b.WriteString(" | ")
		b.WriteString(markdownCell(strings.Join(doc.SupersededBy, ", ")))
		b.WriteString(" |\n")
	}
	return b.String()
}

func splitPlanFrontmatter(data []byte) ([]byte, []byte, bool) {
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return nil, data, false
	}
	rest := data[len("---\n"):]
	idx := bytes.Index(rest, []byte("\n---\n"))
	if idx < 0 {
		return nil, data, false
	}
	return rest[:idx], rest[idx+len("\n---\n"):], true
}

func firstMarkdownTitle(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func isActivePlanStatus(status string) bool {
	switch status {
	case "approved", "planned", "in_progress", "implemented":
		return true
	default:
		return false
	}
}

func planCommitExists(repoRoot string, ref planImplementationRef) bool {
	repoPath := repoRoot
	if ref.Repo != "" && ref.Repo != "workflow" {
		repoPath = filepath.Join(filepath.Dir(repoRoot), ref.Repo)
	}
	cmd := exec.Command("git", "cat-file", "-e", ref.Commit+"^{commit}")
	cmd.Dir = repoPath
	return cmd.Run() == nil
}

func sortPlanDocs(docs []planDoc) {
	sort.SliceStable(docs, func(i, j int) bool {
		if docs[i].Area != docs[j].Area {
			return docs[i].Area < docs[j].Area
		}
		if docs[i].Status != docs[j].Status {
			return docs[i].Status < docs[j].Status
		}
		if docs[i].Title != docs[j].Title {
			return docs[i].Title < docs[j].Title
		}
		return docs[i].Path < docs[j].Path
	})
}

func markdownLink(title, path string) string {
	if title == "" {
		title = filepath.Base(path)
	}
	return fmt.Sprintf("[%s](%s)", title, path)
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "|", "\\|")
	value = asciiMarkdown(value)
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func asciiMarkdown(value string) string {
	replacer := strings.NewReplacer(
		"\u2014", "-",
		"\u2013", "-",
		"\u2194", "<->",
		"\u2192", "->",
		"\u2705", "PASS",
		"\u274c", "FAIL",
	)
	return replacer.Replace(value)
}

func formatPlanImplementationRefs(refs []planImplementationRef) string {
	if len(refs) == 0 {
		return ""
	}
	values := make([]string, 0, len(refs))
	for _, ref := range refs {
		parts := make([]string, 0, 3)
		if ref.Repo != "" {
			parts = append(parts, ref.Repo)
		}
		if ref.PR != "" {
			parts = append(parts, "PR "+ref.PR)
		}
		if ref.Commit != "" {
			parts = append(parts, ref.Commit)
		}
		values = append(values, strings.Join(parts, " "))
	}
	return strings.Join(values, ", ")
}

func discoverPlanAuditRepoRoot(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
