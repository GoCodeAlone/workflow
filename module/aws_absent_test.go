package module_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestAWSServicePackagesAbsent verifies that the freed AWS SDK service packages
// are not imported anywhere in the module/ directory (issue #653).
// Uses filepath.WalkDir (recursive) — NOT filepath.Glob — per #617 retro.
func TestAWSServicePackagesAbsent(t *testing.T) {
	freed := []string{
		"aws-sdk-go-v2/service/apigatewayv2",
		"aws-sdk-go-v2/service/applicationautoscaling",
		"aws-sdk-go-v2/service/route53",
		"aws-sdk-go-v2/service/codebuild",
		"aws-sdk-go-v2/service/eks",
	}

	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "aws_absent_test.go") {
			return nil // skip self
		}
		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if f == nil {
			if parseErr != nil {
				t.Logf("skipping unparseable file %s: %v", path, parseErr)
			}
			return nil
		}
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			for _, pkg := range freed {
				if strings.Contains(importPath, pkg) {
					t.Errorf("%s imports freed package %q", path, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
}
