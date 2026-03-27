package schema_test

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/schema"
)

// TestNoNewJSONFields enforces zero tolerance for json-typed schema fields.
// All module and step config fields must use a typed schema (string, number,
// boolean, select, array, map, duration, filepath, sql). Any new json field
// will cause this test to fail.
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

	if len(jsonFields) > 0 {
		t.Errorf("%d json-typed fields remain (must be 0):\n%s",
			len(jsonFields), strings.Join(jsonFields, "\n"))
	}
}
