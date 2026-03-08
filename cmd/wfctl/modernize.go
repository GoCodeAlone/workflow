package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Finding represents a single issue detected by a modernize rule.
type Finding struct {
	RuleID  string
	Line    int
	Message string
	Fixable bool
}

// Change represents a modification applied by a rule's Fix function.
type Change struct {
	RuleID      string
	Line        int
	Description string
}

// Rule defines a modernize transformation rule.
type Rule struct {
	ID          string
	Description string
	Severity    string // "error" or "warning"
	Check       func(root *yaml.Node, raw []byte) []Finding
	Fix         func(root *yaml.Node) []Change
}

func runModernize(args []string) error {
	fs := flag.NewFlagSet("modernize", flag.ContinueOnError)
	apply := fs.Bool("apply", false, "Apply fixes in-place (default: dry-run)")
	listRules := fs.Bool("list-rules", false, "List all available modernize rules")
	rulesFlag := fs.String("rules", "", "Comma-separated list of rule IDs to run (default: all)")
	excludeFlag := fs.String("exclude-rules", "", "Comma-separated list of rule IDs to skip")
	format := fs.String("format", "text", "Output format: text or json")
	dir := fs.String("dir", "", "Scan all YAML files in a directory (recursive)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl modernize [options] <config.yaml> [config2.yaml ...]

Detect and fix known YAML config anti-patterns.

By default runs in dry-run mode (report only). Use --apply to write fixes.

Examples:
  wfctl modernize config/app.yaml
  wfctl modernize --apply config/app.yaml
  wfctl modernize --dir ./config/
  wfctl modernize --rules hyphen-steps,conditional-field config.yaml
  wfctl modernize --list-rules

Options:
`)
		fs.PrintDefaults()
	}
	args = reorderFlags(args)
	if err := fs.Parse(args); err != nil {
		return err
	}

	rules := allModernizeRules()

	if *listRules {
		fmt.Println("Available modernize rules:")
		fmt.Println()
		for _, r := range rules {
			fixable := "fixable"
			if r.Fix == nil {
				fixable = "detect-only"
			}
			fmt.Printf("  %-24s [%-7s] [%-11s] %s\n", r.ID, r.Severity, fixable, r.Description)
		}
		return nil
	}

	// Filter rules
	rules = filterRules(rules, *rulesFlag, *excludeFlag)

	// Collect files
	var files []string
	if *dir != "" {
		found, err := findYAMLFiles(*dir)
		if err != nil {
			return fmt.Errorf("scan directory %s: %w", *dir, err)
		}
		files = append(files, found...)
	}
	files = append(files, fs.Args()...)
	if len(files) == 0 {
		fs.Usage()
		return fmt.Errorf("at least one config file or --dir is required")
	}

	totalFindings := 0
	totalFixes := 0

	for _, f := range files {
		findings, fixes, err := modernizeFile(f, rules, *apply)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  SKIP %s: %v\n", f, err)
			continue
		}
		totalFindings += len(findings)
		totalFixes += fixes

		if len(findings) == 0 {
			continue
		}

		switch *format {
		case "json":
			// JSON output handled after all files
		default:
			fmt.Printf("%s:\n", f)
			for _, finding := range findings {
				fixable := ""
				if finding.Fixable {
					fixable = " (fixable)"
				}
				fmt.Printf("  line %d: [%s] %s%s\n", finding.Line, finding.RuleID, finding.Message, fixable)
			}
			fmt.Println()
		}
	}

	// Summary
	if totalFindings == 0 {
		fmt.Println("No issues found.")
		return nil
	}

	if *apply {
		fmt.Printf("%d fix(es) applied across %d finding(s).\n", totalFixes, totalFindings)
	} else {
		fmt.Printf("%d issue(s) found. Run with --apply to fix.\n", totalFindings)
	}

	return nil
}

// modernizeFile checks (and optionally fixes) a single YAML file.
func modernizeFile(path string, rules []Rule, apply bool) ([]Finding, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, 0, fmt.Errorf("parse YAML: %w", err)
	}

	// Check phase
	var allFindings []Finding
	for _, r := range rules {
		findings := r.Check(&doc, data)
		allFindings = append(allFindings, findings...)
	}

	if !apply || len(allFindings) == 0 {
		return allFindings, 0, nil
	}

	// Fix phase
	fixCount := 0
	for _, r := range rules {
		if r.Fix == nil {
			continue
		}
		changes := r.Fix(&doc)
		fixCount += len(changes)
	}

	if fixCount > 0 {
		out, err := yaml.Marshal(&doc)
		if err != nil {
			return allFindings, 0, fmt.Errorf("marshal fixed YAML: %w", err)
		}
		if err := os.WriteFile(path, out, 0644); err != nil {
			return allFindings, 0, fmt.Errorf("write fixed file: %w", err)
		}
	}

	return allFindings, fixCount, nil
}

// filterRules filters the rule list based on include/exclude flags.
func filterRules(rules []Rule, include, exclude string) []Rule {
	if include == "" && exclude == "" {
		return rules
	}

	includeSet := make(map[string]bool)
	if include != "" {
		for _, id := range strings.Split(include, ",") {
			includeSet[strings.TrimSpace(id)] = true
		}
	}

	excludeSet := make(map[string]bool)
	if exclude != "" {
		for _, id := range strings.Split(exclude, ",") {
			excludeSet[strings.TrimSpace(id)] = true
		}
	}

	var filtered []Rule
	for _, r := range rules {
		if len(includeSet) > 0 && !includeSet[r.ID] {
			continue
		}
		if excludeSet[r.ID] {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}
