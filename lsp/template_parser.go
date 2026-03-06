package lsp

import (
	"strings"
)

// TemplateExprPath represents the parsed cursor position within a template expression.
type TemplateExprPath struct {
	Namespace   string // "steps", "trigger", "meta", "body", or "" (top-level)
	StepName    string // if Namespace=="steps", the step name
	SubField    string // nested path within namespace (e.g. "path_params" for trigger, "address" for body)
	FieldPrefix string // partial field name being typed (for filtering)
	Raw         string // the raw expression text before cursor
}

// ParseTemplateExprAt extracts and parses the template expression
// from a line up to the cursor position. Returns nil if cursor is not
// in a template expression.
func ParseTemplateExprAt(line string, char int) *TemplateExprPath {
	// Clamp char to line length
	if char > len(line) {
		char = len(line)
	}
	truncated := line[:char]

	// Find the last {{ in the truncated string
	openIdx := strings.LastIndex(truncated, "{{")
	if openIdx == -1 {
		return nil
	}

	// Check there's no }} after the last {{
	afterOpen := truncated[openIdx:]
	if strings.Contains(afterOpen, "}}") {
		return nil
	}

	// Extract text between {{ and cursor
	expr := truncated[openIdx+2:]

	// Strip everything after the last pipe (|)
	if pipeIdx := strings.LastIndex(expr, "|"); pipeIdx != -1 {
		expr = expr[:pipeIdx]
	}

	// Trim whitespace
	expr = strings.TrimSpace(expr)

	raw := expr

	result := &TemplateExprPath{Raw: raw}

	// Pattern: just empty or whitespace → top-level
	if expr == "" || expr == "." {
		return result
	}

	// Pattern: index syntax — `index .steps "stepName" "field"`
	if strings.HasPrefix(expr, "index") {
		rest := strings.TrimSpace(expr[len("index"):])
		if strings.HasPrefix(rest, ".steps") {
			result.Namespace = "steps"
			rest = strings.TrimSpace(rest[len(".steps"):])
			quoted := extractQuotedStrings(rest)
			if len(quoted) > 0 {
				result.StepName = quoted[0]
			}
			if len(quoted) > 1 {
				result.FieldPrefix = quoted[1]
			}
		}
		return result
	}

	// Pattern: step function — `step "stepName" "field"`
	if strings.HasPrefix(expr, "step") && (len(expr) == 4 || expr[4] == ' ' || expr[4] == '\t' || expr[4] == '"') {
		result.Namespace = "steps"
		rest := strings.TrimSpace(expr[4:])
		quoted := extractQuotedStrings(rest)
		if len(quoted) > 0 {
			result.StepName = quoted[0]
		}
		if len(quoted) > 1 {
			result.FieldPrefix = quoted[1]
		}
		return result
	}

	// Pattern: dot-path starting with .
	if strings.HasPrefix(expr, ".") {
		return parseDotPath(expr[1:])
	}

	return result
}

// parseDotPath parses a dot-separated path (without leading dot).
func parseDotPath(path string) *TemplateExprPath {
	result := &TemplateExprPath{}

	if path == "" {
		// Just "." — top-level namespace completions
		return result
	}

	// Split on dots
	parts := strings.Split(path, ".")

	if len(parts) == 0 {
		return result
	}

	result.Namespace = parts[0]

	switch result.Namespace {
	case "steps":
		if len(parts) == 1 {
			// .steps. (trailing dot included in split as empty last element) or .steps
			// if path ends with ".", last part is ""
			if strings.HasSuffix(path, ".") {
				result.StepName = ""
			}
		} else if len(parts) == 2 {
			if strings.HasSuffix(path, ".") {
				// .steps.lookup. → listing step fields
				result.StepName = parts[1]
			} else {
				// .steps.lo → partial step name
				result.StepName = ""
				result.FieldPrefix = parts[1]
			}
		} else {
			// .steps.lookup.fieldOrPrefix
			result.StepName = parts[1]
			last := parts[len(parts)-1]
			if strings.HasSuffix(path, ".") {
				// ends with dot → last part is "", SubField is second to last
				if len(parts) > 3 {
					result.SubField = strings.Join(parts[2:len(parts)-1], ".")
				}
				result.FieldPrefix = ""
			} else {
				result.FieldPrefix = last
				if len(parts) > 3 {
					result.SubField = strings.Join(parts[2:len(parts)-1], ".")
				}
			}
		}

	case "body", "trigger", "meta":
		if len(parts) == 1 {
			// just .body or .trigger etc with no trailing dot
			if !strings.HasSuffix(path, ".") {
				result.FieldPrefix = ""
			}
		} else if len(parts) == 2 {
			if strings.HasSuffix(path, ".") {
				// .body.address. → SubField is address, listing nested fields
				result.SubField = parts[1]
				result.FieldPrefix = ""
			} else {
				// .body.em → FieldPrefix
				result.FieldPrefix = parts[1]
			}
		} else {
			// .trigger.path_params.key
			last := parts[len(parts)-1]
			if strings.HasSuffix(path, ".") {
				result.SubField = strings.Join(parts[1:len(parts)-1], ".")
				result.FieldPrefix = ""
			} else {
				result.SubField = strings.Join(parts[1:len(parts)-1], ".")
				result.FieldPrefix = last
			}
		}

	default:
		// Unknown namespace, treat last part as prefix
		if len(parts) > 1 {
			result.FieldPrefix = parts[len(parts)-1]
		}
	}

	return result
}

// extractQuotedStrings extracts all double-quoted strings from s.
func extractQuotedStrings(s string) []string {
	var results []string
	for {
		start := strings.Index(s, `"`)
		if start == -1 {
			break
		}
		s = s[start+1:]
		end := strings.Index(s, `"`)
		if end == -1 {
			// Unclosed quote — partial string being typed
			results = append(results, s)
			break
		}
		results = append(results, s[:end])
		s = s[end+1:]
	}
	return results
}
