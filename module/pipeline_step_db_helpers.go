package module

import (
	"database/sql"
	"fmt"
)

// scanSQLRows iterates over rows and returns a slice of column→value maps.
// []byte values are converted to string for readability. Callers are
// responsible for closing rows after this function returns.
func scanSQLRows(rows *sql.Rows) ([]map[string]any, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	var results []map[string]any
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		row := make(map[string]any, len(columns))
		for i, col := range columns {
			val := values[i]
			// Convert []byte to string for readability
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return results, nil
}

// formatQueryOutput builds the standard step output map for query results.
// mode "single" returns {row, found}; any other mode returns {rows, count}.
func formatQueryOutput(results []map[string]any, mode string) map[string]any {
	output := make(map[string]any)
	if mode == "single" {
		if len(results) > 0 {
			output["row"] = results[0]
			output["found"] = true
		} else {
			output["row"] = map[string]any{}
			output["found"] = false
		}
	} else {
		if results == nil {
			results = []map[string]any{}
		}
		output["rows"] = results
		output["count"] = len(results)
	}
	return output
}
