package module_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGodoFixture(t *testing.T, root, relativePath, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", relativePath, err)
	}
}

func TestGodoImportScannerUnderstandsGoSyntax(t *testing.T) {
	denied := map[string]string{
		"interpreted direct": "package fixture\nimport \"github.com/digitalocean/godo\"\n",
		"alias":              "package fixture\nimport client \"github.com/digitalocean/godo\"\n",
		"blank":              "package fixture\nimport _ \"github.com/digitalocean/godo\"\n",
		"dot":                "package fixture\nimport . \"github.com/digitalocean/godo\"\n",
		"grouped":            "package fixture\nimport (\n\t\"github.com/digitalocean/godo\"\n)\n",
		"subpackage":         "package fixture\nimport \"github.com/digitalocean/godo/metrics\"\n",
		"raw string":         "package fixture\nimport `github.com/digitalocean/godo`\n",
		"commented":          "package fixture\nimport /* reviewed */ \"github.com/digitalocean/godo\"\n",
		"one-line grouped":   "package fixture\nimport (\"github.com/digitalocean/godo\")\n",
	}
	for name, source := range denied {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeGodoFixture(t, root, "fixture.go", source)
			findings, err := findGodoImports(root)
			if err != nil {
				t.Fatalf("scan imports: %v", err)
			}
			if len(findings) != 1 {
				t.Fatalf("findings = %v, want one denied import", findings)
			}
		})
	}

	root := t.TempDir()
	benignSource := "package fixture\n\n" +
		"// import \"github.com/digitalocean/godo\"\n" +
		"/*\nimport \"github.com/digitalocean/godo\"\n*/\n" +
		"const interpreted = \"github.com/digitalocean/godo\"\n" +
		"const raw = `github.com/digitalocean/godo`\n" +
		"const multiline = `before\ngithub.com/digitalocean/godo\nafter`\n" +
		"var policyData = []string{\n\t\"github.com/digitalocean/godo\",\n}\n"
	writeGodoFixture(t, root, "benign.go", benignSource)
	findings, err := findGodoImports(root)
	if err != nil {
		t.Fatalf("scan benign Go source: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("non-import Go text was rejected: %v", findings)
	}
}

func TestGodoImportScannerSkipsOnlyRepositoryMetadata(t *testing.T) {
	root := t.TempDir()
	deniedSource := "package fixture\nimport \"github.com/digitalocean/godo\"\n"
	for _, relativePath := range []string{
		".git/ignored.go",
		".worktrees/ignored.go",
		"_worktrees/ignored.go",
	} {
		writeGodoFixture(t, root, relativePath, deniedSource)
	}
	writeGodoFixture(t, root, "ordinary/forbidden.go", deniedSource)

	findings, err := findGodoImports(root)
	if err != nil {
		t.Fatalf("scan repository imports: %v", err)
	}
	if len(findings) != 1 || !strings.Contains(findings[0], "ordinary/forbidden.go") {
		t.Fatalf("metadata confinement findings = %v, want only ordinary source", findings)
	}
}

func TestGodoModuleScannerUnderstandsGoModSyntax(t *testing.T) {
	denied := map[string]string{
		"direct require": `module example.com/fixture
go 1.26
require github.com/digitalocean/godo v1.0.0
`,
		"indirect require": `module example.com/fixture
go 1.26
require github.com/digitalocean/godo v1.0.0 // indirect
`,
		"replace source": `module example.com/fixture
go 1.26
replace github.com/digitalocean/godo => example.com/fork/godo v1.0.0
`,
		"replace target": `module example.com/fixture
go 1.26
replace example.com/consumer v1.0.0 => github.com/digitalocean/godo v1.0.0
`,
		"exclude": `module example.com/fixture
go 1.26
exclude github.com/digitalocean/godo v1.0.0
`,
		"module subpath": `module example.com/fixture
go 1.26
require github.com/digitalocean/godo/v2 v2.0.0
`,
	}
	for name, goMod := range denied {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeGodoFixture(t, root, "go.mod", goMod)
			findings, err := findGodoModuleDeclarations(root, []string{"go.mod"})
			if err != nil {
				t.Fatalf("scan go.mod: %v", err)
			}
			if len(findings) == 0 {
				t.Fatal("actual godo module declaration was accepted")
			}
		})
	}

	root := t.TempDir()
	writeGodoFixture(t, root, "go.mod", `module example.com/fixture
go 1.26
// require github.com/digitalocean/godo v1.0.0
// replace github.com/digitalocean/godo => example.com/fork/godo v1.0.0
// exclude github.com/digitalocean/godo v1.0.0
`)
	findings, err := findGodoModuleDeclarations(root, []string{"go.mod"})
	if err != nil {
		t.Fatalf("scan benign go.mod comments: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("go.mod comments were rejected: %v", findings)
	}
}
