package module

import (
	"bytes"
	"encoding/json"
)

// parseJSONBytesOrString attempts to unmarshal b as JSON. If successful the
// parsed Go value is returned (map[string]any, []any, string, float64, bool,
// or nil). This transparently handles PostgreSQL json/jsonb columns, which the
// pgx driver delivers as raw JSON bytes rather than pre-typed Go values.
//
// A cheap leading-byte pre-check is applied first so that binary blobs (e.g.
// PostgreSQL bytea) skip the full JSON parser entirely and fall back to
// string conversion without incurring unnecessary CPU overhead.
//
// If b is not valid JSON (e.g. PostgreSQL bytea binary data), string(b) is
// returned so that the existing string-fallback behaviour is preserved.
func parseJSONBytesOrString(b []byte) any {
	if len(b) == 0 {
		return string(b)
	}
	// Quick check: JSON must start with one of these characters (after optional
	// whitespace). Anything else is definitely not JSON and we avoid calling the
	// full decoder on large binary blobs.
	trimmed := bytes.TrimLeft(b, " \t\r\n")
	if len(trimmed) == 0 {
		return string(b)
	}
	first := trimmed[0]
	if first != '{' && first != '[' && first != '"' &&
		first != 't' && first != 'f' && first != 'n' &&
		first != '-' && (first < '0' || first > '9') {
		return string(b)
	}
	var v any
	if err := json.Unmarshal(b, &v); err == nil {
		return v
	}
	return string(b)
}
