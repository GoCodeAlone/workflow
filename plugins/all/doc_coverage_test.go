package all

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// TestDocumentationCoverage verifies that every registered module type and
// step type appears in DOCUMENTATION.md at least once (as a backtick-quoted
// string, e.g. `my.module`).  This test is intended to catch drift between
// the plugin registrations and the public-facing documentation.
//
// If a new module or step type is added but the documentation is not updated,
// this test will fail with a list of the missing entries so they can be added
// to DOCUMENTATION.md.
func TestDocumentationCoverage(t *testing.T) {
	// Locate DOCUMENTATION.md relative to this test file.
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	docPath := filepath.Join(filepath.Dir(filename), "..", "..", "DOCUMENTATION.md")

	raw, err := os.ReadFile(docPath) //nolint:gosec // path constructed from known repo structure
	if err != nil {
		t.Fatalf("read DOCUMENTATION.md: %v", err)
	}
	docContent := string(raw)

	// Load all built-in plugins into a throwaway loader.
	capReg := capability.NewRegistry()
	schemaReg := schema.NewModuleSchemaRegistry()
	loader := plugin.NewPluginLoader(capReg, schemaReg)
	for _, p := range DefaultPlugins() {
		if err := loader.LoadPlugin(p); err != nil {
			t.Fatalf("LoadPlugin(%q) error: %v", p.Name(), err)
		}
	}

	// Collect module types missing from docs.
	var missingModules []string
	for typeName := range loader.ModuleFactories() {
		if !strings.Contains(docContent, "`"+typeName+"`") {
			missingModules = append(missingModules, typeName)
		}
	}

	// Collect step types missing from docs.
	var missingSteps []string
	for typeName := range loader.StepFactories() {
		if !strings.Contains(docContent, "`"+typeName+"`") {
			missingSteps = append(missingSteps, typeName)
		}
	}

	if len(missingModules) > 0 {
		sort.Strings(missingModules)
		t.Errorf("module types registered but not documented in DOCUMENTATION.md (%d missing):\n  %s\n\nAdd a row for each type to the appropriate section of DOCUMENTATION.md.",
			len(missingModules), strings.Join(missingModules, "\n  "))
	}

	if len(missingSteps) > 0 {
		sort.Strings(missingSteps)
		t.Errorf("step types registered but not documented in DOCUMENTATION.md (%d missing):\n  %s\n\nAdd a row for each type to the appropriate section of DOCUMENTATION.md.",
			len(missingSteps), strings.Join(missingSteps, "\n  "))
	}
}
