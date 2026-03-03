package module

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// pgPlaceholderRe matches PostgreSQL-style $N placeholders in SQL queries.
var pgPlaceholderRe = regexp.MustCompile(`\$(\d+)`)

// isPostgresDriver returns true for PostgreSQL-compatible driver names.
func isPostgresDriver(driver string) bool {
	switch driver {
	case "postgres", "pgx", "pgx/v5":
		return true
	}
	return false
}

// isSQLiteDriver returns true for SQLite driver names.
func isSQLiteDriver(driver string) bool {
	switch driver {
	case "sqlite3", "sqlite":
		return true
	}
	return false
}

// normalizePlaceholders converts SQL placeholder syntax between database drivers.
// Users write PostgreSQL-style $1, $2, $3 placeholders (the canonical format).
// When running against SQLite, these are converted to positional ? placeholders.
// When running against PostgreSQL, the query is returned unchanged.
//
// If the driver is unknown or the query already uses the correct format, the
// query is returned unchanged.
func normalizePlaceholders(query, driver string) string {
	if isPostgresDriver(driver) || driver == "" {
		// PostgreSQL or unknown driver: $N placeholders are native, pass through.
		return query
	}

	if !isSQLiteDriver(driver) {
		// Unknown non-SQLite driver: don't modify.
		return query
	}

	// SQLite: convert $N to ? placeholders.
	// We need to ensure the parameters are in order ($1, $2, $3 → ?, ?, ?).
	// First verify that placeholders are sequential starting from $1.
	matches := pgPlaceholderRe.FindAllStringSubmatchIndex(query, -1)
	if len(matches) == 0 {
		return query // No $N placeholders, might already use ? or have no params.
	}

	// Verify sequential ordering for safety.
	seen := make(map[int]bool)
	for _, m := range matches {
		numStr := query[m[2]:m[3]]
		n, err := strconv.Atoi(numStr)
		if err != nil || n < 1 {
			return query // Malformed, don't modify.
		}
		seen[n] = true
	}

	// Check that all numbers from 1..max are present.
	maxN := len(seen)
	for i := 1; i <= maxN; i++ {
		if !seen[i] {
			return query // Non-sequential (e.g. $1, $3 without $2), don't modify.
		}
	}

	// Replace each $N with ? (they may appear out of order or repeat in the query).
	// For SQLite, positional ? params correspond to the order they appear in the
	// param slice, so we need to reorder params. However, the standard use case
	// is $1..$N in order, matching the params slice. For simplicity we replace
	// $N with ? and trust the params order matches.
	result := pgPlaceholderRe.ReplaceAllString(query, "?")
	return result
}

// appendTenantFilter appends "AND <column> = $N" to a SQL query, where N is
// the next positional parameter index. The function handles queries with and
// without an existing WHERE clause.
func appendTenantFilter(query, column string, paramIndex int) string {
	query = strings.TrimRight(query, " \t\n\r;")
	upper := strings.ToUpper(query)
	if strings.Contains(upper, " WHERE ") {
		return fmt.Sprintf("%s AND %s = $%d", query, column, paramIndex)
	}
	return fmt.Sprintf("%s WHERE %s = $%d", query, column, paramIndex)
}

// placeholder count in the query. Returns an error if there's a mismatch.
func validatePlaceholderCount(query, driver string, paramCount int) error {
	if paramCount == 0 {
		return nil
	}

	var count int
	switch {
	case isPostgresDriver(driver) || driver == "":
		// Count $N placeholders
		matches := pgPlaceholderRe.FindAllString(query, -1)
		// Deduplicate — same $N can appear multiple times
		unique := make(map[string]bool)
		for _, m := range matches {
			unique[m] = true
		}
		count = len(unique)
	case isSQLiteDriver(driver):
		count = strings.Count(query, "?")
	default:
		return nil // Unknown driver, skip validation
	}

	if count != paramCount {
		return fmt.Errorf("query has %d placeholder(s) but %d param(s) were provided", count, paramCount)
	}
	return nil
}
