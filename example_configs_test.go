package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

// TestExampleConfigsLoad dynamically discovers all YAML config files in example/
// and verifies they parse without error. This prevents regressions in example configs.
func TestExampleConfigsLoad(t *testing.T) {
	exampleDir := "example"

	// Verify example directory exists
	if _, err := os.Stat(exampleDir); os.IsNotExist(err) {
		t.Fatalf("example directory %q does not exist", exampleDir)
	}

	// Directories whose YAML files are NOT workflow configs
	skipDirs := map[string]bool{
		"configs":       true,
		"seed":          true,
		"observability": true,
		"spa":           true,
		"components":    true,
		"node_modules":  true,
		"e2e":           true,
	}

	// Files that are not workflow configs
	skipFiles := map[string]bool{
		"docker-compose.yml":  true,
		"docker-compose.yaml": true,
	}

	var configs []string

	err := filepath.WalkDir(exampleDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			// Skip hidden directories (e.g. .playwright-cli) and known non-config dirs
			if strings.HasPrefix(name, ".") || skipDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if ext == ".yaml" || ext == ".yml" {
			if !skipFiles[d.Name()] {
				configs = append(configs, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk example directory: %v", err)
	}

	if len(configs) == 0 {
		t.Fatal("no YAML config files found in example/")
	}

	t.Logf("discovered %d example config files", len(configs))

	for _, cfg := range configs {
		cfg := cfg // capture range variable
		t.Run(filepath.Base(cfg), func(t *testing.T) {
			t.Parallel()
			wfCfg, err := config.LoadFromFile(cfg)
			if err != nil {
				t.Errorf("failed to load config %s: %v", cfg, err)
				return
			}
			// Workflow configs should have modules; linking/reference configs may not.
			// Log a warning but don't fail for configs without modules.
			if len(wfCfg.Modules) == 0 {
				t.Logf("note: config %s has no modules (may be a linking/reference config)", cfg)
			}
		})
	}
}
