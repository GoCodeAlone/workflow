package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// repoAuditReport holds all findings from a repo audit.
type repoAuditReport struct {
	Findings []repoAuditFinding `json:"findings"`
	Summary  repoAuditSummary   `json:"summary"`
}

// repoAuditFinding represents a single quality gate finding.
type repoAuditFinding struct {
	Path    string `json:"path"`
	Level   string `json:"level"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// repoAuditSummary summarises audit results.
type repoAuditSummary struct {
	Status   string `json:"status"`
	Files    int    `json:"files"`
	Checks   int    `json:"checks"`
	Warnings int    `json:"warnings"`
	Errors   int    `json:"errors"`
}

// repoAuditConfig controls which checks are enabled and which paths to ignore.
type repoAuditConfig struct {
	Checks  repoAuditChecksConfig `yaml:"checks"`
	Ignores []string              `yaml:"ignores"`
}

type repoAuditChecksConfig struct {
	PortablePaths  *bool `yaml:"portable_paths"`
	IndexDrift     *bool `yaml:"index_drift"`
	DocFrontmatter *bool `yaml:"doc_frontmatter"`
}

func (c *repoAuditChecksConfig) isEnabled(name string) bool {
	switch name {
	case "portable_paths":
		return c.PortablePaths == nil || *c.PortablePaths
	case "index_drift":
		return c.IndexDrift == nil || *c.IndexDrift
	case "doc_frontmatter":
		return c.DocFrontmatter == nil || *c.DocFrontmatter
	}
	return true
}

func runAuditRepoWithOutput(args []string, out io.Writer) error {
	fset := flag.NewFlagSet("audit repo", flag.ContinueOnError)
	fset.SetOutput(out)
	dir := fset.String("dir", ".", "Repository root directory to audit")
	jsonOut := fset.Bool("json", false, "Write JSON output")
	strict := fset.Bool("strict", false, "Treat warnings as errors")
	configFile := fset.String("config", "", "Path to audit config; defaults to <dir>/.wfctl.yaml")
	if err := fset.Parse(args); err != nil {
		return err
	}

	cfg, err := loadRepoAuditConfig(*configFile, *dir)
	if err != nil {
		return fmt.Errorf("audit repo: load config: %w", err)
	}

	report := runRepoAuditChecks(*dir, cfg)

	if *jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else {
		renderRepoAuditReport(out, report)
	}

	if report.Summary.Errors > 0 {
		return fmt.Errorf("%d repo audit error(s) found", report.Summary.Errors)
	}
	if *strict && report.Summary.Warnings > 0 {
		return fmt.Errorf("%d repo audit warning(s) found (strict mode)", report.Summary.Warnings)
	}
	return nil
}

func runRepoAuditChecks(dir string, cfg repoAuditConfig) repoAuditReport {
	var findings []repoAuditFinding
	var fileCount int
	var checkCount int

	_ = filepath.WalkDir(dir, func(fpath string, d fs.DirEntry, err error) error {
		if err != nil {
			findings = append(findings, repoAuditFinding{
				Path:    fpath,
				Level:   "ERROR",
				Code:    "walk_error",
				Message: fmt.Sprintf("cannot access path: %v", err),
			})
			return nil //nolint:nilerr // intentionally continue walking after per-entry errors
		}
		// Skip common noise directories
		base := d.Name()
		if d.IsDir() {
			if base == ".git" || base == "node_modules" || base == "vendor" || base == ".next" {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, _ := filepath.Rel(dir, fpath)
		if relPath == "" {
			relPath = fpath
		}
		// Use forward slashes for consistent cross-platform matching
		relPath = filepath.ToSlash(relPath)

		if isIgnored(relPath, cfg.Ignores) {
			return nil
		}

		fileCount++

		if cfg.Checks.isEnabled("portable_paths") {
			checkCount++
			if f := checkPortablePath(relPath); f != nil {
				findings = append(findings, *f)
			}
		}

		if cfg.Checks.isEnabled("doc_frontmatter") && isDocFile(relPath) && d.Type().IsRegular() {
			checkCount++
			if f := checkDocFrontmatter(fpath, relPath); f != nil {
				findings = append(findings, *f)
			}
		}

		return nil
	})

	if cfg.Checks.isEnabled("index_drift") {
		checkCount++
		findings = append(findings, checkIndexDrift(dir)...)
	}

	summary := repoAuditSummary{
		Status: "PASS",
		Files:  fileCount,
		Checks: checkCount,
	}
	for _, f := range findings {
		switch f.Level {
		case "ERROR":
			summary.Errors++
		case "WARN":
			summary.Warnings++
		}
	}
	if summary.Errors > 0 {
		summary.Status = "FAIL"
	} else if summary.Warnings > 0 {
		summary.Status = "WARN"
	}

	return repoAuditReport{
		Findings: findings,
		Summary:  summary,
	}
}

// checkPortablePath checks for non-portable characters in file paths.
func checkPortablePath(relPath string) *repoAuditFinding {
	for _, r := range relPath {
		if r > 127 || (unicode.IsControl(r) && r != '\t') {
			return &repoAuditFinding{
				Path:    relPath,
				Level:   "WARN",
				Code:    "non_portable_path",
				Message: fmt.Sprintf("path contains non-ASCII or control character %U", r),
			}
		}
	}
	// Check for characters that are problematic on Windows
	if strings.ContainsAny(relPath, ":*?\"<>|") {
		return &repoAuditFinding{
			Path:    relPath,
			Level:   "WARN",
			Code:    "non_portable_path",
			Message: "path contains characters not allowed on Windows",
		}
	}
	// Check for trailing spaces or dots in path segments (Windows issue)
	for _, part := range strings.Split(relPath, "/") {
		if strings.HasSuffix(part, " ") || strings.HasSuffix(part, ".") {
			return &repoAuditFinding{
				Path:    relPath,
				Level:   "WARN",
				Code:    "non_portable_path",
				Message: "path segment ends with space or dot (problematic on Windows)",
			}
		}
	}
	return nil
}

// unsafePathRe matches path traversal sequences.
var unsafePathRe = regexp.MustCompile(`(^|/)\.\.(/|$)`)

// checkUnsafePath reports path traversal and absolute path patterns. It is
// intended for validating user-supplied path strings (e.g. from config files
// or manifests), not for paths returned by filepath.WalkDir, which never
// produces traversal or absolute entries.
func checkUnsafePath(relPath string) *repoAuditFinding {
	if unsafePathRe.MatchString(relPath) {
		return &repoAuditFinding{
			Path:    relPath,
			Level:   "ERROR",
			Code:    "unsafe_path_traversal",
			Message: "path contains directory traversal (../)",
		}
	}
	if filepath.IsAbs(relPath) {
		return &repoAuditFinding{
			Path:    relPath,
			Level:   "ERROR",
			Code:    "unsafe_absolute_path",
			Message: "path is absolute",
		}
	}
	return nil
}

// isDocFile returns true if the file is a markdown doc inside a documentation directory.
func isDocFile(relPath string) bool {
	ext := strings.ToLower(filepath.Ext(relPath))
	if ext != ".md" && ext != ".mdx" {
		return false
	}
	// Only audit docs in known documentation directories
	return strings.HasPrefix(relPath, "docs/") ||
		strings.HasPrefix(relPath, "doc/") ||
		strings.HasPrefix(relPath, "documentation/")
}

// checkDocFrontmatter checks that regular markdown files in docs directories have valid frontmatter.
func checkDocFrontmatter(absPath, relPath string) *repoAuditFinding {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}
	if len(data) == 0 {
		return nil
	}
	// Only warn on docs that look like they should have frontmatter
	// (have multiple headings but no frontmatter)
	content := string(data)
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		if hasStructuredContent(content) {
			return &repoAuditFinding{
				Path:    relPath,
				Level:   "WARN",
				Code:    "missing_doc_frontmatter",
				Message: "structured documentation file lacks YAML frontmatter",
			}
		}
		return nil
	}
	// Has frontmatter opening — check it closes
	rest := content[4:]
	closeIdx := strings.Index(rest, "\n---")
	if closeIdx == -1 {
		closeIdx = strings.Index(rest, "\r\n---")
	}
	if closeIdx == -1 {
		return &repoAuditFinding{
			Path:    relPath,
			Level:   "ERROR",
			Code:    "malformed_frontmatter",
			Message: "frontmatter opening delimiter has no closing delimiter",
		}
	}
	return nil
}

// hasStructuredContent heuristic: file has multiple headings.
func hasStructuredContent(content string) bool {
	headingCount := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			headingCount++
			if headingCount >= 2 {
				return true
			}
		}
	}
	return false
}

// checkIndexDrift checks if docs/plans/INDEX.md is stale relative to plan docs.
func checkIndexDrift(dir string) []repoAuditFinding {
	plansDir := filepath.Join(dir, "docs", "plans")
	indexPath := filepath.Join(plansDir, "INDEX.md")

	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		// No index file — not an error (optional)
		return nil
	}

	// Collect .md files in plans dir
	entries, err := os.ReadDir(plansDir)
	if err != nil {
		return nil
	}

	var planFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".md") && name != "INDEX.md" {
			planFiles = append(planFiles, name)
		}
	}

	// Check each plan file is referenced in the index
	var findings []repoAuditFinding
	indexContent := string(indexData)
	for _, pf := range planFiles {
		if !strings.Contains(indexContent, pf) {
			findings = append(findings, repoAuditFinding{
				Path:    "docs/plans/INDEX.md",
				Level:   "WARN",
				Code:    "index_drift",
				Message: fmt.Sprintf("plan file %q not referenced in INDEX.md", pf),
			})
		}
	}
	return findings
}

func renderRepoAuditReport(out io.Writer, report repoAuditReport) {
	fmt.Fprintf(out, "%s repo audit: %d file(s) scanned, %d warning(s), %d error(s)\n",
		report.Summary.Status, report.Summary.Files, report.Summary.Warnings, report.Summary.Errors)
	if len(report.Findings) == 0 {
		return
	}

	fmt.Fprintln(out, "")
	for _, f := range report.Findings {
		fmt.Fprintf(out, "  [%s] %s: %s (%s)\n", f.Level, f.Path, f.Message, f.Code)
	}
}

// loadRepoAuditConfig loads audit configuration from .wfctl.yaml or a specified config file.
// It returns an error only when the config file exists but cannot be parsed; a missing default
// config is silently ignored.
func loadRepoAuditConfig(configPath, dir string) (repoAuditConfig, error) {
	var cfg repoAuditConfig

	explicitPath := configPath != ""
	if !explicitPath {
		configPath = filepath.Join(dir, ".wfctl.yaml")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if !explicitPath && os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read audit config %s: %w", configPath, err)
	}

	var raw struct {
		Audit repoAuditConfig `yaml:"audit"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return cfg, fmt.Errorf("parse audit config %s: %w", configPath, err)
	}
	return raw.Audit, nil
}

// isIgnored checks if a path matches any ignore pattern using slash-based glob matching.
func isIgnored(relPath string, patterns []string) bool {
	for _, pattern := range patterns {
		// Use path.Match (slash-based) since relPath is already normalised with forward slashes.
		matched, _ := path.Match(pattern, relPath)
		if matched {
			return true
		}
		// Also check if the pattern matches just the basename
		matched, _ = path.Match(pattern, path.Base(relPath))
		if matched {
			return true
		}
		// Support "dir/*" prefix patterns
		if strings.HasSuffix(pattern, "/*") {
			prefix := strings.TrimSuffix(pattern, "/*")
			if strings.HasPrefix(relPath, prefix+"/") {
				return true
			}
		}
	}
	return false
}
