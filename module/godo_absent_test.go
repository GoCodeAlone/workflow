package module_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestGodoNotImported_InModulePackage asserts no file under module/ (including
// subdirectories) imports github.com/digitalocean/godo. This is the regression
// gate for issue #617.
func TestGodoNotImported_InModulePackage(t *testing.T) {
	var files []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
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
