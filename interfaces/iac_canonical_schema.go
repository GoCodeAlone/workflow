package interfaces

import _ "embed"

// iacCanonicalSchemaJSON is the JSON Schema for the canonical IaC container service config.
// It is exported for use by wfctl validate and plugin authors.
//
//go:embed iac_canonical_schema.json
var iacCanonicalSchemaJSON []byte

// IaCCanonicalSchemaJSON returns the raw JSON Schema bytes.
func IaCCanonicalSchemaJSON() []byte {
	return iacCanonicalSchemaJSON
}
