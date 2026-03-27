package schema_test

import (
	"os"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/schema"
)

// TestNoNewJSONFields is a ratchet test that prevents the number of json-typed
// schema fields from growing. The maxAllowed constant should only decrease over
// time as fields are converted to proper typed schemas (Task 5 and successors).
//
// To run with strict mode (requires zero json fields):
//
//	STRICT_SCHEMA=true go test ./schema/ -run TestNoNewJSONFields -v
func TestNoNewJSONFields(t *testing.T) {
	moduleRegistry := schema.NewModuleSchemaRegistry()
	stepRegistry := schema.NewStepSchemaRegistry()

	var jsonFields []string

	for typeName, ms := range moduleRegistry.AllMap() {
		for _, field := range ms.ConfigFields {
			if field.Type == schema.FieldTypeJSON {
				jsonFields = append(jsonFields, "module:"+typeName+"."+field.Key)
			}
		}
	}
	for typeName, ss := range stepRegistry.AllMap() {
		for _, field := range ss.ConfigFields {
			if field.Type == schema.FieldTypeJSON {
				jsonFields = append(jsonFields, "step:"+typeName+"."+field.Key)
			}
		}
	}

	if os.Getenv("STRICT_SCHEMA") == "true" {
		if len(jsonFields) > 0 {
			t.Errorf("STRICT_SCHEMA: %d json fields remain (must be 0):\n%s",
				len(jsonFields), strings.Join(jsonFields, "\n"))
		}
		return
	}

	// Ratchet: count should only decrease over time.
	// Update this number downward as fields are converted to typed schemas.
	// Was 73 before Task 5; reduced to 51 after Task 5.
	const maxAllowed = 51

	if len(jsonFields) > maxAllowed {
		t.Errorf("JSON field count increased to %d (max allowed: %d). New json fields:\n%s",
			len(jsonFields), maxAllowed, strings.Join(jsonFields, "\n"))
	}

	t.Logf("Current JSON field count: %d / %d allowed", len(jsonFields), maxAllowed)
}
