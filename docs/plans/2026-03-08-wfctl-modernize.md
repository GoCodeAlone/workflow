# wfctl modernize Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `wfctl modernize` command that detects and auto-fixes known YAML config anti-patterns using AST-based transformations.

**Architecture:** Each fix is a `Rule` with `Check()` (detect) and `Fix()` (transform) methods operating on `yaml.Node` ASTs. Dry-run by default; `--apply` writes in-place. Follows existing wfctl command patterns (flag.FlagSet, reorderFlags, findYAMLFiles).

**Tech Stack:** Go, gopkg.in/yaml.v3 (`yaml.Node` for AST), existing `config` package for struct-level validation.

---

### Task 1: Register the command skeleton

**Files:**
- Create: `cmd/wfctl/modernize.go`
- Modify: `cmd/wfctl/main.go:34-59` (add to commands map)
- Modify: `cmd/wfctl/wfctl.yaml` (add command definition + pipeline)

**Step 1: Create modernize.go with minimal runModernize**

```go
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
		fixableCount := 0
		// Count fixable (approximate — we already printed per-file)
		for _, r := range rules {
			if r.Fix != nil {
				fixableCount++
			}
		}
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
```

**Step 2: Register in main.go**

In `cmd/wfctl/main.go`, add to the commands map (line ~58, before the closing `}`):

```go
"modernize": runModernize,
```

**Step 3: Register in wfctl.yaml**

Add to the `commands:` list (after the `mcp` entry, around line 53):

```yaml
      - name: modernize
        description: "Detect and fix known YAML config anti-patterns (dry-run by default)"
```

Add to the `pipelines:` section (at the end of the file):

```yaml
  cmd-modernize:
    trigger:
      type: cli
      config:
        command: modernize
    steps:
      - name: run
        type: step.cli_invoke
        config:
          command: modernize
```

**Step 4: Create empty rules placeholder**

Create `cmd/wfctl/modernize_rules.go`:

```go
package main

// allModernizeRules returns all registered modernize rules.
func allModernizeRules() []Rule {
	return []Rule{}
}
```

**Step 5: Verify it compiles**

Run: `cd /Users/jon/workspace/workflow && go build ./cmd/wfctl/`
Expected: Clean build, no errors.

**Step 6: Verify --list-rules works**

Run: `./wfctl modernize --list-rules`
Expected: "Available modernize rules:" with empty list.

**Step 7: Commit**

```bash
git add cmd/wfctl/modernize.go cmd/wfctl/modernize_rules.go cmd/wfctl/main.go cmd/wfctl/wfctl.yaml
git commit -m "feat(wfctl): add modernize command skeleton"
```

---

### Task 2: Implement rule — hyphen-steps

**Files:**
- Modify: `cmd/wfctl/modernize_rules.go`

This rule detects step names containing hyphens and renames them to underscores, updating all references in `step.conditional` field paths and template expressions.

**Step 1: Write the test**

Create `cmd/wfctl/modernize_test.go`:

```go
package main

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func parseTestYAML(t *testing.T, input string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(input), &doc); err != nil {
		t.Fatalf("parse YAML: %v", err)
	}
	return &doc
}

func TestHyphenStepsCheck(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: check-xss
        type: step.regex_match
      - name: route_xss
        type: step.conditional
        config:
          field: steps.check-xss.matched
`
	rules := allModernizeRules()
	var rule Rule
	for _, r := range rules {
		if r.ID == "hyphen-steps" {
			rule = r
			break
		}
	}
	if rule.ID == "" {
		t.Fatal("hyphen-steps rule not found")
	}

	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) == 0 {
		t.Fatal("expected findings for hyphenated step name")
	}
	found := false
	for _, f := range findings {
		if strings.Contains(f.Message, "check-xss") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected finding for check-xss, got: %v", findings)
	}
}

func TestHyphenStepsFix(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: check-xss
        type: step.regex_match
      - name: route-result
        type: step.conditional
        config:
          field: steps.check-xss.matched
      - name: respond
        type: step.json_response
        config:
          body:
            value: "{{ .steps.check-xss.result }}"
`
	rules := allModernizeRules()
	var rule Rule
	for _, r := range rules {
		if r.ID == "hyphen-steps" {
			rule = r
			break
		}
	}

	doc := parseTestYAML(t, input)
	changes := rule.Fix(doc)
	if len(changes) == 0 {
		t.Fatal("expected changes from fix")
	}

	out, _ := yaml.Marshal(doc)
	result := string(out)

	if strings.Contains(result, "check-xss") {
		t.Errorf("expected hyphens to be replaced, got:\n%s", result)
	}
	if !strings.Contains(result, "check_xss") {
		t.Errorf("expected underscored name, got:\n%s", result)
	}
	if strings.Contains(result, "route-result") {
		t.Errorf("expected route-result to be renamed, got:\n%s", result)
	}
	if !strings.Contains(result, "route_result") {
		t.Errorf("expected route_result, got:\n%s", result)
	}
	// Check that references in field paths and templates are updated
	if strings.Contains(result, "steps.check-xss") {
		t.Errorf("expected field reference to be updated, got:\n%s", result)
	}
}
```

**Step 2: Run the test to verify it fails**

Run: `cd /Users/jon/workspace/workflow && go test ./cmd/wfctl/ -run TestHyphenSteps -v`
Expected: FAIL — "hyphen-steps rule not found"

**Step 3: Implement the rule**

In `cmd/wfctl/modernize_rules.go`, replace with:

```go
package main

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// allModernizeRules returns all registered modernize rules.
func allModernizeRules() []Rule {
	return []Rule{
		hyphenStepsRule(),
	}
}

// --- yaml.Node helpers ---

// walkNodes calls fn for every node in the tree (depth-first).
func walkNodes(node *yaml.Node, fn func(n *yaml.Node)) {
	if node == nil {
		return
	}
	fn(node)
	for _, child := range node.Content {
		walkNodes(child, fn)
	}
}

// findMapValue returns the value node for a given key in a mapping node.
func findMapValue(node *yaml.Node, key string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// collectStepNames walks pipelines and collects all step names.
func collectStepNames(root *yaml.Node) []string {
	var names []string
	// root is DocumentNode → first child is the mapping
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	pipelines := findMapValue(root, "pipelines")
	if pipelines == nil || pipelines.Kind != yaml.MappingNode {
		return names
	}
	// Iterate pipeline values
	for i := 1; i < len(pipelines.Content); i += 2 {
		pipelineVal := pipelines.Content[i]
		steps := findMapValue(pipelineVal, "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode {
			continue
		}
		for _, step := range steps.Content {
			nameNode := findMapValue(step, "name")
			if nameNode != nil && nameNode.Kind == yaml.ScalarNode {
				names = append(names, nameNode.Value)
			}
		}
	}
	return names
}

// hyphenStepNameRegex matches step names containing hyphens.
var hyphenStepNameRegex = regexp.MustCompile(`[a-zA-Z0-9]+-[a-zA-Z0-9]+`)

func hyphenStepsRule() Rule {
	return Rule{
		ID:          "hyphen-steps",
		Description: "Rename hyphenated step names to underscores (hyphens break Go templates)",
		Severity:    "error",
		Check: func(root *yaml.Node, raw []byte) []Finding {
			var findings []Finding
			names := collectStepNames(root)
			for _, name := range names {
				if strings.Contains(name, "-") {
					findings = append(findings, Finding{
						RuleID:  "hyphen-steps",
						Message: fmt.Sprintf("Step %q uses hyphens (causes Go template parse errors)", name),
						Fixable: true,
					})
				}
			}
			return findings
		},
		Fix: func(root *yaml.Node) []Change {
			names := collectStepNames(root)
			// Build rename map: old -> new
			renames := make(map[string]string)
			for _, name := range names {
				if strings.Contains(name, "-") {
					renames[name] = strings.ReplaceAll(name, "-", "_")
				}
			}
			if len(renames) == 0 {
				return nil
			}

			var changes []Change

			// Walk all scalar nodes and replace references
			walkNodes(root, func(n *yaml.Node) {
				if n.Kind != yaml.ScalarNode {
					return
				}
				for oldName, newName := range renames {
					if n.Value == oldName {
						n.Value = newName
						changes = append(changes, Change{
							RuleID:      "hyphen-steps",
							Line:        n.Line,
							Description: fmt.Sprintf("Renamed step %q -> %q", oldName, newName),
						})
						return
					}
					// Update references in field paths (steps.old-name.field)
					if strings.Contains(n.Value, oldName) {
						updated := strings.ReplaceAll(n.Value, oldName, newName)
						if updated != n.Value {
							n.Value = updated
							changes = append(changes, Change{
								RuleID:      "hyphen-steps",
								Line:        n.Line,
								Description: fmt.Sprintf("Updated reference %q in value", oldName),
							})
						}
					}
				}
			})

			return changes
		},
	}
}
```

**Step 4: Run the test**

Run: `cd /Users/jon/workspace/workflow && go test ./cmd/wfctl/ -run TestHyphenSteps -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/wfctl/modernize_rules.go cmd/wfctl/modernize_test.go
git commit -m "feat(wfctl): add hyphen-steps modernize rule"
```

---

### Task 3: Implement rule — conditional-field

**Files:**
- Modify: `cmd/wfctl/modernize_rules.go`
- Modify: `cmd/wfctl/modernize_test.go`

Detects `step.conditional` steps where the `field` value contains `{{ }}` template syntax (should be a dot-path like `steps.X.Y`).

**Step 1: Write the test**

Add to `cmd/wfctl/modernize_test.go`:

```go
func TestConditionalFieldCheck(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: route
        type: step.conditional
        config:
          field: "{{ .steps.check_xss.matched }}"
          routes:
            "true": deny
          default: allow
`
	rule := findRule("conditional-field")
	if rule == nil {
		t.Fatal("conditional-field rule not found")
	}

	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) == 0 {
		t.Fatal("expected findings for template in conditional field")
	}
}

func TestConditionalFieldFix(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: route
        type: step.conditional
        config:
          field: "{{ .steps.check_xss.matched }}"
          routes:
            "true": deny
          default: allow
`
	rule := findRule("conditional-field")
	doc := parseTestYAML(t, input)
	changes := rule.Fix(doc)
	if len(changes) == 0 {
		t.Fatal("expected changes from fix")
	}

	out, _ := yaml.Marshal(doc)
	result := string(out)

	if strings.Contains(result, "{{") {
		t.Errorf("expected template syntax to be removed, got:\n%s", result)
	}
	if !strings.Contains(result, "steps.check_xss.matched") {
		t.Errorf("expected dot-path field value, got:\n%s", result)
	}
}

// findRule is a test helper that looks up a rule by ID.
func findRule(id string) *Rule {
	for _, r := range allModernizeRules() {
		if r.ID == id {
			return &r
		}
	}
	return nil
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/jon/workspace/workflow && go test ./cmd/wfctl/ -run TestConditionalField -v`
Expected: FAIL

**Step 3: Implement the rule**

Add to `allModernizeRules()` return slice:

```go
conditionalFieldRule(),
```

Add the rule function:

```go
// conditionalFieldTemplateRegex matches {{ .some.path }} in a field value.
var conditionalFieldTemplateRegex = regexp.MustCompile(`^\{\{\s*\.?([\w.]+)\s*\}\}$`)

func conditionalFieldRule() Rule {
	return Rule{
		ID:          "conditional-field",
		Description: "Convert template syntax in step.conditional field to dot-path",
		Severity:    "error",
		Check: func(root *yaml.Node, raw []byte) []Finding {
			var findings []Finding
			forEachStepOfType(root, "step.conditional", func(step *yaml.Node) {
				cfg := findMapValue(step, "config")
				if cfg == nil {
					return
				}
				field := findMapValue(cfg, "field")
				if field == nil || field.Kind != yaml.ScalarNode {
					return
				}
				if strings.Contains(field.Value, "{{") {
					findings = append(findings, Finding{
						RuleID:  "conditional-field",
						Line:    field.Line,
						Message: fmt.Sprintf("step.conditional field uses template syntax %q (should be dot-path)", field.Value),
						Fixable: true,
					})
				}
			})
			return findings
		},
		Fix: func(root *yaml.Node) []Change {
			var changes []Change
			forEachStepOfType(root, "step.conditional", func(step *yaml.Node) {
				cfg := findMapValue(step, "config")
				if cfg == nil {
					return
				}
				field := findMapValue(cfg, "field")
				if field == nil || field.Kind != yaml.ScalarNode {
					return
				}
				if m := conditionalFieldTemplateRegex.FindStringSubmatch(field.Value); m != nil {
					oldVal := field.Value
					field.Value = m[1]
					field.Style = 0 // remove quotes
					changes = append(changes, Change{
						RuleID:      "conditional-field",
						Line:        field.Line,
						Description: fmt.Sprintf("Converted field %q -> %q", oldVal, field.Value),
					})
				}
			})
			return changes
		},
	}
}

// forEachStepOfType calls fn for each step node of the given type across all pipelines.
func forEachStepOfType(root *yaml.Node, stepType string, fn func(step *yaml.Node)) {
	docRoot := root
	if docRoot.Kind == yaml.DocumentNode && len(docRoot.Content) > 0 {
		docRoot = docRoot.Content[0]
	}
	pipelines := findMapValue(docRoot, "pipelines")
	if pipelines == nil || pipelines.Kind != yaml.MappingNode {
		return
	}
	for i := 1; i < len(pipelines.Content); i += 2 {
		pipelineVal := pipelines.Content[i]
		steps := findMapValue(pipelineVal, "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode {
			continue
		}
		for _, step := range steps.Content {
			typeNode := findMapValue(step, "type")
			if typeNode != nil && typeNode.Value == stepType {
				fn(step)
			}
		}
	}
}
```

**Step 4: Run test**

Run: `cd /Users/jon/workspace/workflow && go test ./cmd/wfctl/ -run TestConditionalField -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/wfctl/modernize_rules.go cmd/wfctl/modernize_test.go
git commit -m "feat(wfctl): add conditional-field modernize rule"
```

---

### Task 4: Implement rule — db-query-mode

**Files:**
- Modify: `cmd/wfctl/modernize_rules.go`
- Modify: `cmd/wfctl/modernize_test.go`

Detects `step.db_query` steps missing `mode: single` when downstream templates reference `.row` or `.found` (which are only available in single mode).

**Step 1: Write the test**

Add to `cmd/wfctl/modernize_test.go`:

```go
func TestDbQueryModeCheck(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: fetch_user
        type: step.db_query
        config:
          database: my-db
          query: "SELECT * FROM users WHERE id = ?"
      - name: respond
        type: step.json_response
        config:
          body:
            name: '{{ index .steps "fetch_user" "row" "name" }}'
`
	rule := findRule("db-query-mode")
	if rule == nil {
		t.Fatal("db-query-mode rule not found")
	}

	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) == 0 {
		t.Fatal("expected findings for missing mode:single")
	}
}

func TestDbQueryModeNoFalsePositive(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: fetch_user
        type: step.db_query
        config:
          database: my-db
          query: "SELECT * FROM users WHERE id = ?"
          mode: single
      - name: respond
        type: step.json_response
        config:
          body:
            name: '{{ index .steps "fetch_user" "row" "name" }}'
`
	rule := findRule("db-query-mode")
	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) != 0 {
		t.Errorf("expected no findings when mode:single is set, got: %v", findings)
	}
}

func TestDbQueryModeFix(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: fetch_user
        type: step.db_query
        config:
          database: my-db
          query: "SELECT * FROM users WHERE id = ?"
      - name: respond
        type: step.json_response
        config:
          body:
            found: "{{ .steps.fetch_user.found }}"
`
	rule := findRule("db-query-mode")
	doc := parseTestYAML(t, input)
	changes := rule.Fix(doc)
	if len(changes) == 0 {
		t.Fatal("expected changes from fix")
	}

	out, _ := yaml.Marshal(doc)
	result := string(out)

	if !strings.Contains(result, "mode: single") {
		t.Errorf("expected mode: single to be added, got:\n%s", result)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/jon/workspace/workflow && go test ./cmd/wfctl/ -run TestDbQueryMode -v`
Expected: FAIL

**Step 3: Implement the rule**

Add `dbQueryModeRule()` to `allModernizeRules()`.

```go
func dbQueryModeRule() Rule {
	return Rule{
		ID:          "db-query-mode",
		Description: "Add mode:single to step.db_query when downstream uses .row or .found",
		Severity:    "warning",
		Check: func(root *yaml.Node, raw []byte) []Finding {
			var findings []Finding
			rawStr := string(raw)
			forEachStepOfType(root, "step.db_query", func(step *yaml.Node) {
				cfg := findMapValue(step, "config")
				if cfg == nil {
					return
				}
				mode := findMapValue(cfg, "mode")
				if mode != nil {
					return // already has mode set
				}
				nameNode := findMapValue(step, "name")
				if nameNode == nil {
					return
				}
				stepName := nameNode.Value
				// Check if raw YAML references .row or .found for this step
				if strings.Contains(rawStr, stepName+`" "row"`) ||
					strings.Contains(rawStr, stepName+".row") ||
					strings.Contains(rawStr, stepName+".found") {
					findings = append(findings, Finding{
						RuleID:  "db-query-mode",
						Line:    step.Line,
						Message: fmt.Sprintf("step.db_query %q missing mode:single (downstream uses .row/.found)", stepName),
						Fixable: true,
					})
				}
			})
			return findings
		},
		Fix: func(root *yaml.Node) []Change {
			var changes []Change
			// We need the raw text for reference checking — marshal current state
			rawBytes, _ := yaml.Marshal(root)
			rawStr := string(rawBytes)

			forEachStepOfType(root, "step.db_query", func(step *yaml.Node) {
				cfg := findMapValue(step, "config")
				if cfg == nil {
					return
				}
				mode := findMapValue(cfg, "mode")
				if mode != nil {
					return
				}
				nameNode := findMapValue(step, "name")
				if nameNode == nil {
					return
				}
				stepName := nameNode.Value
				if strings.Contains(rawStr, stepName+`" "row"`) ||
					strings.Contains(rawStr, stepName+".row") ||
					strings.Contains(rawStr, stepName+".found") {
					// Add mode: single to config mapping
					cfg.Content = append(cfg.Content,
						&yaml.Node{Kind: yaml.ScalarNode, Value: "mode"},
						&yaml.Node{Kind: yaml.ScalarNode, Value: "single"},
					)
					changes = append(changes, Change{
						RuleID:      "db-query-mode",
						Line:        step.Line,
						Description: fmt.Sprintf("Added mode: single to step.db_query %q", stepName),
					})
				}
			})
			return changes
		},
	}
}
```

**Step 4: Run test**

Run: `cd /Users/jon/workspace/workflow && go test ./cmd/wfctl/ -run TestDbQueryMode -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/wfctl/modernize_rules.go cmd/wfctl/modernize_test.go
git commit -m "feat(wfctl): add db-query-mode modernize rule"
```

---

### Task 5: Implement rule — db-query-index

**Files:**
- Modify: `cmd/wfctl/modernize_rules.go`
- Modify: `cmd/wfctl/modernize_test.go`

Detects `.steps.X.row.Y` dot-access patterns in templates (which cause nil pointer errors) and converts to `index .steps "X" "row" "Y"` syntax.

**Step 1: Write the test**

Add to `cmd/wfctl/modernize_test.go`:

```go
func TestDbQueryIndexCheck(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: respond
        type: step.json_response
        config:
          body:
            name: "{{ .steps.fetch_user.row.name }}"
`
	rule := findRule("db-query-index")
	if rule == nil {
		t.Fatal("db-query-index rule not found")
	}

	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) == 0 {
		t.Fatal("expected findings for .row. dot-access")
	}
}

func TestDbQueryIndexFix(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: respond
        type: step.json_response
        config:
          body:
            name: "{{ .steps.fetch_user.row.name }}"
            email: "{{ .steps.fetch_user.row.email }}"
`
	rule := findRule("db-query-index")
	doc := parseTestYAML(t, input)
	changes := rule.Fix(doc)
	if len(changes) == 0 {
		t.Fatal("expected changes from fix")
	}

	out, _ := yaml.Marshal(doc)
	result := string(out)

	if strings.Contains(result, ".steps.fetch_user.row.name") {
		t.Errorf("expected dot-access to be replaced, got:\n%s", result)
	}
	if !strings.Contains(result, `index .steps "fetch_user" "row" "name"`) {
		t.Errorf("expected index syntax, got:\n%s", result)
	}
}
```

**Step 2: Run test, verify fail**

Run: `cd /Users/jon/workspace/workflow && go test ./cmd/wfctl/ -run TestDbQueryIndex -v`
Expected: FAIL

**Step 3: Implement the rule**

Add `dbQueryIndexRule()` to `allModernizeRules()`.

```go
// dotRowAccessRegex matches patterns like .steps.stepname.row.column inside {{ }}.
var dotRowAccessRegex = regexp.MustCompile(`\.steps\.(\w+)\.row\.(\w+)`)

func dbQueryIndexRule() Rule {
	return Rule{
		ID:          "db-query-index",
		Description: "Convert .steps.X.row.Y dot-access to index syntax (dot-access causes nil pointer)",
		Severity:    "error",
		Check: func(root *yaml.Node, raw []byte) []Finding {
			var findings []Finding
			walkNodes(root, func(n *yaml.Node) {
				if n.Kind != yaml.ScalarNode {
					return
				}
				if matches := dotRowAccessRegex.FindAllString(n.Value, -1); len(matches) > 0 {
					for _, m := range matches {
						findings = append(findings, Finding{
							RuleID:  "db-query-index",
							Line:    n.Line,
							Message: fmt.Sprintf("Dot-access %q will cause nil pointer (use index syntax)", m),
							Fixable: true,
						})
					}
				}
			})
			return findings
		},
		Fix: func(root *yaml.Node) []Change {
			var changes []Change
			walkNodes(root, func(n *yaml.Node) {
				if n.Kind != yaml.ScalarNode {
					return
				}
				if !dotRowAccessRegex.MatchString(n.Value) {
					return
				}
				oldVal := n.Value
				n.Value = dotRowAccessRegex.ReplaceAllStringFunc(n.Value, func(match string) string {
					parts := dotRowAccessRegex.FindStringSubmatch(match)
					// parts[1] = step name, parts[2] = column name
					return fmt.Sprintf(`index .steps "%s" "row" "%s"`, parts[1], parts[2])
				})
				if n.Value != oldVal {
					changes = append(changes, Change{
						RuleID:      "db-query-index",
						Line:        n.Line,
						Description: fmt.Sprintf("Converted dot-access to index syntax"),
					})
				}
			})
			return changes
		},
	}
}
```

**Step 4: Run test**

Run: `cd /Users/jon/workspace/workflow && go test ./cmd/wfctl/ -run TestDbQueryIndex -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/wfctl/modernize_rules.go cmd/wfctl/modernize_test.go
git commit -m "feat(wfctl): add db-query-index modernize rule"
```

---

### Task 6: Implement rule — database-to-sqlite

**Files:**
- Modify: `cmd/wfctl/modernize_rules.go`
- Modify: `cmd/wfctl/modernize_test.go`

Detects `database.workflow` module type and converts to `storage.sqlite` with `dbPath` config.

**Step 1: Write the test**

Add to `cmd/wfctl/modernize_test.go`:

```go
func TestDatabaseToSqliteCheck(t *testing.T) {
	input := `
modules:
  - name: my-db
    type: database.workflow
    config:
      driver: sqlite
      dsn: "file:data.db"
`
	rule := findRule("database-to-sqlite")
	if rule == nil {
		t.Fatal("database-to-sqlite rule not found")
	}

	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) == 0 {
		t.Fatal("expected findings for database.workflow")
	}
}

func TestDatabaseToSqliteFix(t *testing.T) {
	input := `
modules:
  - name: my-db
    type: database.workflow
    config:
      driver: sqlite
      dsn: "file:data.db"
`
	rule := findRule("database-to-sqlite")
	doc := parseTestYAML(t, input)
	changes := rule.Fix(doc)
	if len(changes) == 0 {
		t.Fatal("expected changes from fix")
	}

	out, _ := yaml.Marshal(doc)
	result := string(out)

	if strings.Contains(result, "database.workflow") {
		t.Errorf("expected type to be changed, got:\n%s", result)
	}
	if !strings.Contains(result, "storage.sqlite") {
		t.Errorf("expected storage.sqlite type, got:\n%s", result)
	}
	if !strings.Contains(result, "dbPath") {
		t.Errorf("expected dbPath in config, got:\n%s", result)
	}
}
```

**Step 2: Run test, verify fail**

Run: `cd /Users/jon/workspace/workflow && go test ./cmd/wfctl/ -run TestDatabaseToSqlite -v`

**Step 3: Implement the rule**

Add `databaseToSqliteRule()` to `allModernizeRules()`.

```go
func databaseToSqliteRule() Rule {
	return Rule{
		ID:          "database-to-sqlite",
		Description: "Convert database.workflow modules to storage.sqlite",
		Severity:    "warning",
		Check: func(root *yaml.Node, raw []byte) []Finding {
			var findings []Finding
			forEachModule(root, func(mod *yaml.Node) {
				typeNode := findMapValue(mod, "type")
				if typeNode != nil && typeNode.Value == "database.workflow" {
					nameNode := findMapValue(mod, "name")
					name := ""
					if nameNode != nil {
						name = nameNode.Value
					}
					findings = append(findings, Finding{
						RuleID:  "database-to-sqlite",
						Line:    typeNode.Line,
						Message: fmt.Sprintf("Module %q uses database.workflow (use storage.sqlite instead)", name),
						Fixable: true,
					})
				}
			})
			return findings
		},
		Fix: func(root *yaml.Node) []Change {
			var changes []Change
			forEachModule(root, func(mod *yaml.Node) {
				typeNode := findMapValue(mod, "type")
				if typeNode == nil || typeNode.Value != "database.workflow" {
					return
				}
				nameNode := findMapValue(mod, "name")
				name := "unknown"
				if nameNode != nil {
					name = nameNode.Value
				}

				// Change type
				typeNode.Value = "storage.sqlite"

				// Rebuild config: extract DSN to derive dbPath
				cfg := findMapValue(mod, "config")
				if cfg == nil || cfg.Kind != yaml.MappingNode {
					// Create config with default dbPath
					cfg = &yaml.Node{Kind: yaml.MappingNode}
					mod.Content = append(mod.Content,
						&yaml.Node{Kind: yaml.ScalarNode, Value: "config"},
						cfg,
					)
				}

				// Try to extract filename from DSN
				dbPath := name + ".db"
				dsnNode := findMapValue(cfg, "dsn")
				if dsnNode != nil {
					dsn := dsnNode.Value
					dsn = strings.TrimPrefix(dsn, "file:")
					dsn = strings.Split(dsn, "?")[0]
					if dsn != "" {
						dbPath = dsn
					}
				}

				// Replace config contents with storage.sqlite config
				cfg.Content = []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "dbPath"},
					{Kind: yaml.ScalarNode, Value: dbPath},
					{Kind: yaml.ScalarNode, Value: "maxConnections"},
					{Kind: yaml.ScalarNode, Value: "5"},
					{Kind: yaml.ScalarNode, Value: "walMode"},
					{Kind: yaml.ScalarNode, Value: "true", Tag: "!!bool"},
				}

				// Remove driver key if present (not valid for storage.sqlite)
				changes = append(changes, Change{
					RuleID:      "database-to-sqlite",
					Line:        typeNode.Line,
					Description: fmt.Sprintf("Converted module %q from database.workflow to storage.sqlite (dbPath: %s)", name, dbPath),
				})
			})
			return changes
		},
	}
}

// forEachModule calls fn for each module mapping node.
func forEachModule(root *yaml.Node, fn func(mod *yaml.Node)) {
	docRoot := root
	if docRoot.Kind == yaml.DocumentNode && len(docRoot.Content) > 0 {
		docRoot = docRoot.Content[0]
	}
	modules := findMapValue(docRoot, "modules")
	if modules == nil || modules.Kind != yaml.SequenceNode {
		return
	}
	for _, mod := range modules.Content {
		if mod.Kind == yaml.MappingNode {
			fn(mod)
		}
	}
}
```

**Step 4: Run test**

Run: `cd /Users/jon/workspace/workflow && go test ./cmd/wfctl/ -run TestDatabaseToSqlite -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/wfctl/modernize_rules.go cmd/wfctl/modernize_test.go
git commit -m "feat(wfctl): add database-to-sqlite modernize rule"
```

---

### Task 7: Implement detection-only rules (absolute-dbpath, empty-routes, camelcase-config)

**Files:**
- Modify: `cmd/wfctl/modernize_rules.go`
- Modify: `cmd/wfctl/modernize_test.go`

These three rules are detect-only (no `Fix` function).

**Step 1: Write tests**

Add to `cmd/wfctl/modernize_test.go`:

```go
func TestAbsoluteDbPathCheck(t *testing.T) {
	input := `
modules:
  - name: my-db
    type: storage.sqlite
    config:
      dbPath: /data/myapp.db
`
	rule := findRule("absolute-dbpath")
	if rule == nil {
		t.Fatal("absolute-dbpath rule not found")
	}
	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) == 0 {
		t.Fatal("expected warning for absolute dbPath")
	}
}

func TestAbsoluteDbPathNoFalsePositive(t *testing.T) {
	input := `
modules:
  - name: my-db
    type: storage.sqlite
    config:
      dbPath: data.db
`
	rule := findRule("absolute-dbpath")
	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) != 0 {
		t.Errorf("expected no findings for relative dbPath, got: %v", findings)
	}
}

func TestEmptyRoutesCheck(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: route
        type: step.conditional
        config:
          field: steps.check.matched
          routes: {}
          default: next
`
	rule := findRule("empty-routes")
	if rule == nil {
		t.Fatal("empty-routes rule not found")
	}
	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) == 0 {
		t.Fatal("expected findings for empty routes")
	}
}

func TestCamelCaseConfigCheck(t *testing.T) {
	input := `
modules:
  - name: my-server
    type: http.server
    config:
      max_connections: 10
      listen_address: ":8080"
`
	rule := findRule("camelcase-config")
	if rule == nil {
		t.Fatal("camelcase-config rule not found")
	}
	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) == 0 {
		t.Fatal("expected findings for snake_case config keys")
	}
}
```

**Step 2: Run tests, verify fail**

Run: `cd /Users/jon/workspace/workflow && go test ./cmd/wfctl/ -run "TestAbsoluteDbPath|TestEmptyRoutes|TestCamelCaseConfig" -v`

**Step 3: Implement the three rules**

Add all three to `allModernizeRules()`. Implementations:

```go
func absoluteDbPathRule() Rule {
	return Rule{
		ID:          "absolute-dbpath",
		Description: "Warn on absolute dbPath in storage.sqlite (should be relative to config dir)",
		Severity:    "warning",
		Check: func(root *yaml.Node, raw []byte) []Finding {
			var findings []Finding
			forEachModule(root, func(mod *yaml.Node) {
				typeNode := findMapValue(mod, "type")
				if typeNode == nil || typeNode.Value != "storage.sqlite" {
					return
				}
				cfg := findMapValue(mod, "config")
				if cfg == nil {
					return
				}
				dbPath := findMapValue(cfg, "dbPath")
				if dbPath != nil && strings.HasPrefix(dbPath.Value, "/") {
					nameNode := findMapValue(mod, "name")
					name := ""
					if nameNode != nil {
						name = nameNode.Value
					}
					findings = append(findings, Finding{
						RuleID:  "absolute-dbpath",
						Line:    dbPath.Line,
						Message: fmt.Sprintf("Module %q has absolute dbPath %q (use relative path)", name, dbPath.Value),
						Fixable: false,
					})
				}
			})
			return findings
		},
	}
}

func emptyRoutesRule() Rule {
	return Rule{
		ID:          "empty-routes",
		Description: "Detect empty routes map in step.conditional (engine requires at least one route)",
		Severity:    "error",
		Check: func(root *yaml.Node, raw []byte) []Finding {
			var findings []Finding
			forEachStepOfType(root, "step.conditional", func(step *yaml.Node) {
				cfg := findMapValue(step, "config")
				if cfg == nil {
					return
				}
				routes := findMapValue(cfg, "routes")
				if routes == nil {
					nameNode := findMapValue(step, "name")
					name := ""
					if nameNode != nil {
						name = nameNode.Value
					}
					findings = append(findings, Finding{
						RuleID:  "empty-routes",
						Line:    step.Line,
						Message: fmt.Sprintf("step.conditional %q missing routes map", name),
						Fixable: false,
					})
					return
				}
				if routes.Kind == yaml.MappingNode && len(routes.Content) == 0 {
					nameNode := findMapValue(step, "name")
					name := ""
					if nameNode != nil {
						name = nameNode.Value
					}
					findings = append(findings, Finding{
						RuleID:  "empty-routes",
						Line:    routes.Line,
						Message: fmt.Sprintf("step.conditional %q has empty routes (at least one route required)", name),
						Fixable: false,
					})
				}
			})
			return findings
		},
	}
}

// snakeCaseKeyRegex matches keys with underscores (snake_case).
var snakeCaseKeyRegex = regexp.MustCompile(`^[a-z]+(_[a-z0-9]+)+$`)

func camelCaseConfigRule() Rule {
	return Rule{
		ID:          "camelcase-config",
		Description: "Detect snake_case config field names (engine requires camelCase)",
		Severity:    "warning",
		Check: func(root *yaml.Node, raw []byte) []Finding {
			var findings []Finding
			forEachModule(root, func(mod *yaml.Node) {
				cfg := findMapValue(mod, "config")
				if cfg == nil || cfg.Kind != yaml.MappingNode {
					return
				}
				nameNode := findMapValue(mod, "name")
				modName := ""
				if nameNode != nil {
					modName = nameNode.Value
				}
				for i := 0; i+1 < len(cfg.Content); i += 2 {
					key := cfg.Content[i]
					if key.Kind == yaml.ScalarNode && snakeCaseKeyRegex.MatchString(key.Value) {
						findings = append(findings, Finding{
							RuleID:  "camelcase-config",
							Line:    key.Line,
							Message: fmt.Sprintf("Module %q config key %q is snake_case (use camelCase)", modName, key.Value),
							Fixable: false,
						})
					}
				}
			})
			return findings
		},
	}
}
```

**Step 4: Run tests**

Run: `cd /Users/jon/workspace/workflow && go test ./cmd/wfctl/ -run "TestAbsoluteDbPath|TestEmptyRoutes|TestCamelCaseConfig" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/wfctl/modernize_rules.go cmd/wfctl/modernize_test.go
git commit -m "feat(wfctl): add absolute-dbpath, empty-routes, camelcase-config rules"
```

---

### Task 8: Integration test and documentation

**Files:**
- Modify: `cmd/wfctl/modernize_test.go`
- Modify: `docs/WFCTL.md` (if it exists; add modernize section)

**Step 1: Write integration test**

Add to `cmd/wfctl/modernize_test.go`:

```go
func TestModernizeAllRulesRegistered(t *testing.T) {
	rules := allModernizeRules()
	expectedIDs := []string{
		"hyphen-steps",
		"conditional-field",
		"db-query-mode",
		"db-query-index",
		"database-to-sqlite",
		"absolute-dbpath",
		"empty-routes",
		"camelcase-config",
	}
	if len(rules) != len(expectedIDs) {
		t.Errorf("expected %d rules, got %d", len(expectedIDs), len(rules))
	}
	ruleMap := make(map[string]bool)
	for _, r := range rules {
		ruleMap[r.ID] = true
	}
	for _, id := range expectedIDs {
		if !ruleMap[id] {
			t.Errorf("missing rule: %s", id)
		}
	}
}

func TestFilterRules(t *testing.T) {
	rules := allModernizeRules()

	// Include filter
	filtered := filterRules(rules, "hyphen-steps,empty-routes", "")
	if len(filtered) != 2 {
		t.Errorf("expected 2 rules with include filter, got %d", len(filtered))
	}

	// Exclude filter
	filtered = filterRules(rules, "", "camelcase-config")
	if len(filtered) != len(rules)-1 {
		t.Errorf("expected %d rules with exclude filter, got %d", len(rules)-1, len(filtered))
	}
}

func TestModernizeFullPipeline(t *testing.T) {
	// A config with multiple issues
	input := `
name: test-app
modules:
  - name: my-db
    type: database.workflow
    config:
      driver: sqlite
      dsn: "file:app.db"
pipelines:
  check:
    steps:
      - name: check-input
        type: step.regex_match
        config:
          pattern: "test"
          input: "{{ .body.data }}"
      - name: route-check
        type: step.conditional
        config:
          field: "{{ .steps.check-input.matched }}"
          routes:
            "true": block
          default: allow
      - name: fetch
        type: step.db_query
        config:
          database: my-db
          query: "SELECT name FROM users WHERE id = ?"
      - name: respond
        type: step.json_response
        config:
          body:
            name: "{{ .steps.fetch.row.name }}"
`
	rules := allModernizeRules()
	doc := parseTestYAML(t, input)

	// Check phase
	var allFindings []Finding
	for _, r := range rules {
		allFindings = append(allFindings, r.Check(doc, []byte(input))...)
	}
	if len(allFindings) < 4 {
		t.Errorf("expected at least 4 findings, got %d: %v", len(allFindings), allFindings)
	}

	// Fix phase
	totalChanges := 0
	for _, r := range rules {
		if r.Fix == nil {
			continue
		}
		changes := r.Fix(doc)
		totalChanges += len(changes)
	}
	if totalChanges == 0 {
		t.Fatal("expected changes from fixes")
	}

	out, _ := yaml.Marshal(doc)
	result := string(out)

	// Verify fixes applied
	if strings.Contains(result, "check-input") {
		t.Error("hyphen-steps: check-input not renamed")
	}
	if strings.Contains(result, "database.workflow") {
		t.Error("database-to-sqlite: type not changed")
	}
	if strings.Contains(result, "{{ .steps") && strings.Contains(result, "field:") {
		// Check specifically the conditional field
		if strings.Contains(result, `field: "{{ .steps`) {
			t.Error("conditional-field: template not converted")
		}
	}
}
```

**Step 2: Run all tests**

Run: `cd /Users/jon/workspace/workflow && go test ./cmd/wfctl/ -run TestModernize -v`
Expected: PASS

**Step 3: Run full test suite to check for regressions**

Run: `cd /Users/jon/workspace/workflow && go test ./cmd/wfctl/ -v -count=1`
Expected: All existing tests still pass.

**Step 4: Update WFCTL.md if it exists**

Check if `docs/WFCTL.md` exists. If so, add a `## modernize` section:

```markdown
## modernize

Detect and fix known YAML config anti-patterns.

```
wfctl modernize [options] <config.yaml|directory>
```

**Options:**
- `--apply` — Write fixes in-place (default: dry-run)
- `--list-rules` — List all available rules
- `--rules <ids>` — Comma-separated rule IDs to run
- `--exclude-rules <ids>` — Comma-separated rule IDs to skip
- `--dir <path>` — Scan all YAML files recursively
- `--format text|json` — Output format

**Rules:**

| ID | Severity | Fixable | Description |
|----|----------|---------|-------------|
| hyphen-steps | error | yes | Rename hyphenated step names to underscores |
| conditional-field | error | yes | Convert template syntax in conditional field to dot-path |
| db-query-mode | warning | yes | Add mode:single when downstream uses .row/.found |
| db-query-index | error | yes | Convert .steps.X.row.Y to index syntax |
| database-to-sqlite | warning | yes | Convert database.workflow to storage.sqlite |
| absolute-dbpath | warning | no | Warn on absolute dbPath values |
| empty-routes | error | no | Detect empty routes in step.conditional |
| camelcase-config | warning | no | Detect snake_case config keys |
```

**Step 5: Commit**

```bash
git add cmd/wfctl/modernize_test.go docs/WFCTL.md
git commit -m "feat(wfctl): add modernize integration tests and docs"
```

---

### Task 9: Final verification

**Step 1: Full build**

Run: `cd /Users/jon/workspace/workflow && go build ./cmd/wfctl/`
Expected: Clean build.

**Step 2: go vet**

Run: `cd /Users/jon/workspace/workflow && go vet ./cmd/wfctl/`
Expected: No issues.

**Step 3: All tests pass**

Run: `cd /Users/jon/workspace/workflow && go test ./cmd/wfctl/ -count=1`
Expected: All pass.

**Step 4: Manual smoke test**

Run against a known config with issues:

```bash
./wfctl modernize --list-rules
./wfctl modernize /Users/jon/workspace/workflow/example/api-server-config.yaml
```

Expected: Lists rules, scans config, reports any findings.

**Step 5: Final commit (if any fixups needed)**

```bash
git add -A
git commit -m "fix(wfctl): modernize command fixups"
```
