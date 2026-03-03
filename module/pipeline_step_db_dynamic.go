package module

import (
	"fmt"
	"strings"
	"unicode"
)

// validateSQLIdentifier checks that s is safe to interpolate directly into SQL as an
// identifier (e.g. a table name). Only Unicode letters, ASCII digits, underscores and
// hyphens are permitted. This strict allowlist prevents SQL injection when dynamic
// values are embedded in queries via allow_dynamic_sql.
func validateSQLIdentifier(s string) error {
	if s == "" {
		return fmt.Errorf("dynamic SQL identifier must not be empty")
	}
	for _, c := range s {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '_' && c != '-' {
			return fmt.Errorf("dynamic SQL identifier %q contains unsafe character %q (only letters, digits, underscores and hyphens are allowed)", s, string(c))
		}
	}
	return nil
}

// resolveDynamicSQL resolves every {{ }} template expression found in query against
// pc and validates that each resolved value is a safe SQL identifier. The validated
// values are substituted back into the query in left-to-right order and the final
// SQL string is returned.
//
// This is only called when allow_dynamic_sql is true (explicit opt-in). Callers
// are responsible for ensuring that the query has already passed template parsing.
func resolveDynamicSQL(tmpl *TemplateEngine, query string, pc *PipelineContext) (string, error) {
	if !strings.Contains(query, "{{") {
		return query, nil
	}

	// Extract template expressions in left-to-right order, using the same
	// {{ ... }} scanning logic as preprocessTemplate.
	var exprs []string
	rest := query
	for {
		openIdx := strings.Index(rest, "{{")
		if openIdx < 0 {
			break
		}
		closeIdx := strings.Index(rest[openIdx:], "}}")
		if closeIdx < 0 {
			break
		}
		closeIdx += openIdx
		exprs = append(exprs, rest[openIdx:closeIdx+2])
		rest = rest[closeIdx+2:]
	}

	// Resolve and validate each distinct expression.
	resolvedVals := make(map[string]string, len(exprs))
	for _, expr := range exprs {
		if _, seen := resolvedVals[expr]; seen {
			continue
		}
		resolved, err := tmpl.Resolve(expr, pc)
		if err != nil {
			return "", fmt.Errorf("dynamic SQL: failed to resolve %q: %w", expr, err)
		}
		if err := validateSQLIdentifier(resolved); err != nil {
			return "", fmt.Errorf("dynamic SQL: %w", err)
		}
		resolvedVals[expr] = resolved
	}

	// Replace expressions in the original query one at a time (left-to-right),
	// so duplicate expressions are each substituted exactly once in order.
	result := query
	for _, expr := range exprs {
		result = strings.Replace(result, expr, resolvedVals[expr], 1)
	}
	return result, nil
}
