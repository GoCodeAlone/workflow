package module

import (
	"fmt"
	"strings"
)

// validateSQLIdentifier checks that s is safe to interpolate directly into SQL as an
// identifier (e.g. a table name). Only ASCII letters (A-Z, a-z), ASCII digits (0-9),
// underscores (_) and hyphens (-) are permitted. This strict allowlist prevents SQL
// injection when dynamic values are embedded in queries via allow_dynamic_sql.
func validateSQLIdentifier(s string) error {
	if s == "" {
		return fmt.Errorf("dynamic SQL identifier must not be empty")
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') &&
			(c < 'A' || c > 'Z') &&
			(c < '0' || c > '9') &&
			c != '_' && c != '-' {
			return fmt.Errorf("dynamic SQL identifier %q contains unsafe character %q (only ASCII letters, digits, underscores and hyphens are allowed)", s, string(c))
		}
	}
	return nil
}

// resolveDynamicSQL resolves every {{ }} template expression found in query against
// pc and validates that each resolved value is a safe SQL identifier. The validated
// values are substituted back into the query in left-to-right order and the final
// SQL string is returned.
//
// Each occurrence of a template expression is resolved independently, so
// non-deterministic functions like {{uuid}} or {{now}} produce a distinct value
// per occurrence.
//
// This is only called when allow_dynamic_sql is true (explicit opt-in). Callers
// are responsible for ensuring that the query has already passed template parsing.
func resolveDynamicSQL(tmpl *TemplateEngine, query string, pc *PipelineContext) (string, error) {
	if !strings.Contains(query, "{{") {
		return query, nil
	}

	// Process template expressions in left-to-right order. Each occurrence is
	// resolved and validated independently to preserve correct semantics for
	// non-deterministic template functions (e.g. {{uuid}}, {{now}}).
	var result strings.Builder
	rest := query
	for {
		openIdx := strings.Index(rest, "{{")
		if openIdx < 0 {
			result.WriteString(rest)
			break
		}
		closeIdx := strings.Index(rest[openIdx:], "}}")
		if closeIdx < 0 {
			return "", fmt.Errorf("dynamic SQL: unclosed template action in query (missing closing '}}')")
		}
		closeIdx += openIdx

		// Write the literal SQL text before this expression.
		result.WriteString(rest[:openIdx])

		expr := rest[openIdx : closeIdx+2]

		resolved, err := tmpl.Resolve(expr, pc)
		if err != nil {
			return "", fmt.Errorf("dynamic SQL: failed to resolve %q: %w", expr, err)
		}
		if err := validateSQLIdentifier(resolved); err != nil {
			return "", fmt.Errorf("dynamic SQL: %w", err)
		}
		result.WriteString(resolved)
		rest = rest[closeIdx+2:]
	}
	return result.String(), nil
}
