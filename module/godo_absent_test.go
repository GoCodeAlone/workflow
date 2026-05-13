package module_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestGodoNotImported_InModulePackage asserts no file under module/ imports
// github.com/digitalocean/godo. This is the regression gate for issue #617.
func TestGodoNotImported_InModulePackage(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	fset := token.NewFileSet()
	for _, f := range files {
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, imp := range af.Imports {
			if strings.Trim(imp.Path.Value, `"`) == "github.com/digitalocean/godo" {
				t.Errorf("%s imports github.com/digitalocean/godo (issue #617 — moved to workflow-plugin-digitalocean)", f)
			}
		}
	}
}
