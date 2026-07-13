package module_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

const godoModulePath = "github.com/digitalocean/godo"

func isGodoModulePath(modulePath string) bool {
	return modulePath == godoModulePath || strings.HasPrefix(modulePath, godoModulePath+"/")
}

func findGodoImports(repoRoot string) ([]string, error) {
	findings := []string{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(repoRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && path != repoRoot {
			switch entry.Name() {
			case ".git", ".worktrees", "_worktrees":
				return filepath.SkipDir
			}
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}

		parsed, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse Go imports in %s: %w", path, err)
		}
		for _, importSpec := range parsed.Imports {
			importPath, err := strconv.Unquote(importSpec.Path.Value)
			if err != nil {
				return fmt.Errorf("unquote Go import in %s: %w", path, err)
			}
			if !isGodoModulePath(importPath) {
				continue
			}
			relativePath, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return fmt.Errorf("relativize Go import path %s: %w", path, err)
			}
			findings = append(findings, fmt.Sprintf("%s imports %s", filepath.ToSlash(relativePath), importPath))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return findings, nil
}

func findGodoModuleDeclarations(repoRoot string, relativePaths []string) ([]string, error) {
	findings := []string{}
	for _, relativePath := range relativePaths {
		path := filepath.Join(repoRoot, filepath.FromSlash(relativePath))
		contents, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", relativePath, err)
		}
		parsed, err := modfile.Parse(relativePath, contents, nil)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", relativePath, err)
		}
		for _, requirement := range parsed.Require {
			if isGodoModulePath(requirement.Mod.Path) {
				findings = append(findings, fmt.Sprintf("%s requires %s", relativePath, requirement.Mod.Path))
			}
		}
		for _, replacement := range parsed.Replace {
			if isGodoModulePath(replacement.Old.Path) {
				findings = append(findings, fmt.Sprintf("%s replaces %s", relativePath, replacement.Old.Path))
			}
			if isGodoModulePath(replacement.New.Path) {
				findings = append(findings, fmt.Sprintf("%s uses replacement target %s", relativePath, replacement.New.Path))
			}
		}
		for _, exclusion := range parsed.Exclude {
			if isGodoModulePath(exclusion.Mod.Path) {
				findings = append(findings, fmt.Sprintf("%s excludes %s", relativePath, exclusion.Mod.Path))
			}
		}
	}
	return findings, nil
}

// TestNoDigitalOceanGodoReferences is the repository-wide regression gate for
// issue #617. DigitalOcean implementation belongs in workflow-plugin-digitalocean.
func TestNoDigitalOceanGodoReferences(t *testing.T) {
	repoRoot := filepath.Clean("..")
	importFindings, err := findGodoImports(repoRoot)
	if err != nil {
		t.Fatalf("scan repository Go imports: %v", err)
	}
	moduleFindings, err := findGodoModuleDeclarations(repoRoot, []string{"go.mod", "example/go.mod"})
	if err != nil {
		t.Fatalf("scan repository module declarations: %v", err)
	}
	for _, finding := range append(importFindings, moduleFindings...) {
		t.Errorf("forbidden DigitalOcean provider dependency: %s", finding)
	}
}
