package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
)

func runExprMigrate(args []string) error {
	fs := flag.NewFlagSet("expr-migrate", flag.ContinueOnError)
	configFlag := fs.String("config", "", "Path to workflow YAML config file (required)")
	outputFlag := fs.String("output", "", "Write converted output to this file (default: stdout)")
	inplace := fs.Bool("inplace", false, "Rewrite the input file in-place (overrides -output)")
	dryRun := fs.Bool("dry-run", false, "Show conversions without writing any file")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl expr-migrate [options]

Convert Go template expressions ({{ }}) to expr syntax (${ }) in a workflow config.

Simple patterns are converted automatically. Complex templates that cannot be
safely auto-converted receive a "# TODO: migrate" comment.

Conversions applied:
  {{ .field }}                  → ${ field }
  {{ .body.name }}              → ${ body.name }
  {{ .steps.name.field }}       → ${ steps["name"]["field"] }
  {{ eq .status "active" }}     → ${ status == "active" }
  {{ ne .x "val" }}             → ${ x != "val" }
  {{ gt .x 5 }}                 → ${ x > 5 }
  {{ lt .x 5 }}                 → ${ x < 5 }
  {{ index .steps "n" "k" }}    → ${ steps["n"]["k"] }
  {{ upper .name }}             → ${ upper(name) }
  {{ and (eq .x "a") ... }}     → ${ x == "a" && ... }

Examples:
  wfctl expr-migrate --config app.yaml --dry-run
  wfctl expr-migrate --config app.yaml --output app-new.yaml
  wfctl expr-migrate --config app.yaml --inplace

Options:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *configFlag == "" {
		fs.Usage()
		return fmt.Errorf("--config is required")
	}

	src, err := os.ReadFile(*configFlag)
	if err != nil {
		return fmt.Errorf("read %s: %w", *configFlag, err)
	}

	converted, stats := migrateExpressions(string(src))

	if *dryRun {
		fmt.Printf("Dry-run: %d expression(s) converted, %d marked as TODO\n",
			stats.converted, stats.todo)
		fmt.Println(converted)
		return nil
	}

	dest := *outputFlag
	if *inplace {
		dest = *configFlag
	}

	if dest == "" {
		fmt.Printf("# expr-migrate: %d expression(s) converted, %d marked as TODO\n",
			stats.converted, stats.todo)
		fmt.Print(converted)
		return nil
	}

	if err := os.WriteFile(dest, []byte(converted), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	fmt.Printf("Wrote %s: %d expression(s) converted, %d marked as TODO\n",
		dest, stats.converted, stats.todo)
	return nil
}

type migrateStats struct {
	converted int
	todo      int
}

// goTemplateRe matches {{ ... }} blocks (non-greedy, single-line).
var goTemplateRe = regexp.MustCompile(`\{\{([^}]|\}[^}])*\}\}`)

// migrateExpressions scans content for {{ }} blocks and converts them to ${ }
// where possible. Returns the converted content and conversion statistics.
func migrateExpressions(content string) (string, migrateStats) {
	var stats migrateStats
	result := goTemplateRe.ReplaceAllStringFunc(content, func(match string) string {
		inner := strings.TrimSpace(match[2 : len(match)-2])

		// Skip control blocks: {{ if }}, {{ else }}, {{ end }}, {{ range }}, etc.
		if isControlBlock(inner) {
			return match
		}

		// Skip whitespace-trim variants {{ - ... - }}.
		if strings.HasPrefix(inner, "-") || strings.HasSuffix(inner, "-") {
			stats.todo++
			return match + " # TODO: migrate"
		}

		converted, ok := convertGoTemplateExpr(inner)
		if !ok {
			stats.todo++
			return match + " # TODO: migrate"
		}
		stats.converted++
		return "${ " + converted + " }"
	})
	return result, stats
}

// controlKeywords are Go template action keywords that should not be converted.
var controlKeywords = []string{
	"if ", "else", "end", "range ", "with ", "define ", "template ",
	"block ", "break", "continue", "return", "/*", "- ",
}

// isControlBlock returns true if the template inner content is a control action.
func isControlBlock(inner string) bool {
	trimmed := strings.TrimSpace(inner)
	for _, kw := range controlKeywords {
		if strings.HasPrefix(trimmed, kw) || trimmed == strings.TrimSpace(kw) {
			return true
		}
	}
	return false
}

// dotPathRe matches a simple dot-path like .field or .a.b.c (no hyphens, no index).
var dotPathRe = regexp.MustCompile(`^\.\s*([a-zA-Z_][a-zA-Z0-9_]*)(\.[a-zA-Z_][a-zA-Z0-9_.]*)*$`)

// stepsDotRe matches .steps.stepName.field patterns.
var stepsDotRe = regexp.MustCompile(`^\.steps\.([a-zA-Z0-9_-]+)\.([a-zA-Z0-9_.]+)$`)

// indexStepsRe matches `index .steps "name" "field"` patterns.
var indexStepsRe = regexp.MustCompile(`^index\s+\.steps\s+"([^"]+)"\s+"([^"]+)"$`)

// eqRe matches `eq .field "value"` or `eq .field value` patterns.
var eqRe = regexp.MustCompile(`^eq\s+(\S+)\s+(.+)$`)

// neRe matches `ne .field "value"`.
var neRe = regexp.MustCompile(`^ne\s+(\S+)\s+(.+)$`)

// gtRe matches `gt .field value`.
var gtRe = regexp.MustCompile(`^gt\s+(\S+)\s+(.+)$`)

// ltRe matches `lt .field value`.
var ltRe = regexp.MustCompile(`^lt\s+(\S+)\s+(.+)$`)

// gteRe matches `ge .field value`.
var gteRe = regexp.MustCompile(`^ge\s+(\S+)\s+(.+)$`)

// lteRe matches `le .field value`.
var lteRe = regexp.MustCompile(`^le\s+(\S+)\s+(.+)$`)

// funcCallRe matches `funcName .field` style (single-arg function calls).
var funcCallRe = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9_]*)\s+(\.[a-zA-Z_][a-zA-Z0-9_.]*)$`)

// andRe matches `and (...) (...)` — two sub-expressions in parens.
var andRe = regexp.MustCompile(`^and\s+\(([^)]+)\)\s+\(([^)]+)\)$`)

// orRe matches `or (...) (...)`.
var orRe = regexp.MustCompile(`^or\s+\(([^)]+)\)\s+\(([^)]+)\)$`)

// convertGoTemplateExpr tries to convert a Go template expression (inner content,
// without {{ }}) to expr syntax. Returns (expr, true) on success or ("", false)
// when the expression is too complex to auto-convert.
func convertGoTemplateExpr(inner string) (string, bool) {
	inner = strings.TrimSpace(inner)

	// Simple field or dot-path: .field or .a.b.c
	if dotPathRe.MatchString(inner) {
		// Strip leading dot
		return inner[1:], true
	}

	// .steps.stepName.field → steps["stepName"]["field"]
	if m := stepsDotRe.FindStringSubmatch(inner); m != nil {
		return fmt.Sprintf(`steps["%s"]["%s"]`, m[1], m[2]), true
	}

	// index .steps "name" "field" → steps["name"]["field"]
	if m := indexStepsRe.FindStringSubmatch(inner); m != nil {
		return fmt.Sprintf(`steps["%s"]["%s"]`, m[1], m[2]), true
	}

	// eq .field "value" → field == "value"
	if m := eqRe.FindStringSubmatch(inner); m != nil {
		lhs := stripDot(m[1])
		return lhs + " == " + m[2], true
	}

	// ne .field "value" → field != "value"
	if m := neRe.FindStringSubmatch(inner); m != nil {
		lhs := stripDot(m[1])
		return lhs + " != " + m[2], true
	}

	// gt .field value → field > value
	if m := gtRe.FindStringSubmatch(inner); m != nil {
		lhs := stripDot(m[1])
		return lhs + " > " + m[2], true
	}

	// lt .field value → field < value
	if m := ltRe.FindStringSubmatch(inner); m != nil {
		lhs := stripDot(m[1])
		return lhs + " < " + m[2], true
	}

	// ge .field value → field >= value
	if m := gteRe.FindStringSubmatch(inner); m != nil {
		lhs := stripDot(m[1])
		return lhs + " >= " + m[2], true
	}

	// le .field value → field <= value
	if m := lteRe.FindStringSubmatch(inner); m != nil {
		lhs := stripDot(m[1])
		return lhs + " <= " + m[2], true
	}

	// and (sub1) (sub2) → sub1 && sub2
	if m := andRe.FindStringSubmatch(inner); m != nil {
		left, okL := convertGoTemplateExpr(m[1])
		right, okR := convertGoTemplateExpr(m[2])
		if okL && okR {
			return left + " && " + right, true
		}
	}

	// or (sub1) (sub2) → sub1 || sub2
	if m := orRe.FindStringSubmatch(inner); m != nil {
		left, okL := convertGoTemplateExpr(m[1])
		right, okR := convertGoTemplateExpr(m[2])
		if okL && okR {
			return left + " || " + right, true
		}
	}

	// Single-arg function: funcName .field → funcName(field)
	if m := funcCallRe.FindStringSubmatch(inner); m != nil {
		arg := stripDot(m[2])
		return fmt.Sprintf("%s(%s)", m[1], arg), true
	}

	return "", false
}

// stripDot removes a leading dot from a field reference like ".name" → "name".
func stripDot(s string) string {
	if strings.HasPrefix(s, ".") {
		return s[1:]
	}
	return s
}
