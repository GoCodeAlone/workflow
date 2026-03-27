package schema_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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

// TestNoJSONFieldsInPlugins ensures plugin schema files contain no FieldTypeJSON usages.
// This is a strict (not ratcheted) check — plugins must always use typed fields.
func TestNoJSONFieldsInPlugins(t *testing.T) {
	pluginDir := "../plugins"
	var violations []string

	err := filepath.WalkDir(pluginDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for i, line := range strings.Split(string(content), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.Contains(trimmed, "FieldTypeJSON") && !strings.HasPrefix(trimmed, "//") {
				violations = append(violations, fmt.Sprintf("%s:%d: %s", path, i+1, trimmed))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk plugins dir: %v", err)
	}

	if len(violations) > 0 {
		t.Errorf("%d FieldTypeJSON usage(s) found in plugin files:\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}
