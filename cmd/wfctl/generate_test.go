package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	allplugins "github.com/GoCodeAlone/workflow/plugins/all"
	"golang.org/x/mod/modfile"
	"gopkg.in/yaml.v3"
)

// generateMinimalConfig is a basic API service config without UI or auth.
const generateMinimalConfig = `
modules:
  - name: test-server
    type: http.server
    config:
      address: ":8080"
  - name: test-router
    type: http.router
    dependsOn: [test-server]
  - name: test-api
    type: http.handler
    dependsOn: [test-router]

triggers:
  http:
    server: test-server
`

// configWithUI has a static.fileserver module indicating a UI.
const configWithUI = `
modules:
  - name: test-server
    type: http.server
    config:
      address: ":8080"
  - name: test-router
    type: http.router
    dependsOn: [test-server]
  - name: test-ui
    type: static.fileserver
    dependsOn: [test-router]
    config:
      root: "./ui/dist"
      spa: true

triggers:
  http:
    server: test-server
`

// configWithAuth has an auth module.
const configWithAuth = `
modules:
  - name: test-server
    type: http.server
    config:
      address: ":8080"
  - name: test-auth
    type: auth.jwt
    config:
      secret: "${JWT_SECRET}"

triggers:
  http:
    server: test-server
`

// configWithDatabase has a database module.
const configWithDatabase = `
modules:
  - name: test-server
    type: http.server
    config:
      address: ":8080"
  - name: test-db
    type: storage.sqlite
    config:
      path: data/test.db

triggers:
  http:
    server: test-server
`

func writeConfigFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(path, []byte(content), 0640); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	return path
}

func TestRunGenerateNoSubcommand(t *testing.T) {
	err := runGenerate([]string{})
	if err == nil {
		t.Fatal("expected error when no subcommand given")
	}
}

func TestRunGenerateUnknownSubcommand(t *testing.T) {
	err := runGenerate([]string{"unknown"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

func TestRunGenerateGithubActionsNoConfig(t *testing.T) {
	err := runGenerateGithubActions([]string{})
	if err == nil {
		t.Fatal("expected error when no config file given")
	}
}

func TestRunGenerateGithubActionsMinimal(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfigFile(t, dir, generateMinimalConfig)
	outDir := filepath.Join(dir, ".github", "workflows")

	err := runGenerateGithubActions([]string{"-output", outDir, cfgPath})
	if err != nil {
		t.Fatalf("generate github-actions failed: %v", err)
	}

	// CI and CD should be generated
	for _, f := range []string{"ci.yml", "cd.yml"} {
		path := filepath.Join(outDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected %s to be created", f)
		}
	}

	// release.yml should NOT be generated (no plugin.json)
	if _, err := os.Stat(filepath.Join(outDir, "release.yml")); err == nil {
		t.Error("release.yml should not be generated without plugin.json")
	}
}

func TestRunGenerateGithubActionsWithUI(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfigFile(t, dir, configWithUI)
	outDir := filepath.Join(dir, ".github", "workflows")

	err := runGenerateGithubActions([]string{"-output", outDir, cfgPath})
	if err != nil {
		t.Fatalf("generate github-actions (UI) failed: %v", err)
	}

	ciPath := filepath.Join(outDir, "ci.yml")
	data, err := os.ReadFile(ciPath)
	if err != nil {
		t.Fatalf("failed to read ci.yml: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "setup-node") {
		t.Error("ci.yml should include setup-node step for UI projects")
	}
	if !strings.Contains(content, "build-ui") {
		t.Error("ci.yml should include build-ui step for UI projects")
	}

	cdPath := filepath.Join(outDir, "cd.yml")
	cdData, err := os.ReadFile(cdPath)
	if err != nil {
		t.Fatalf("failed to read cd.yml: %v", err)
	}
	if !strings.Contains(string(cdData), "npm ci") {
		t.Error("cd.yml should include npm ci step for UI projects")
	}
}

func TestRunGenerateGithubActionsWithAuth(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfigFile(t, dir, configWithAuth)
	outDir := filepath.Join(dir, ".github", "workflows")

	err := runGenerateGithubActions([]string{"-output", outDir, cfgPath})
	if err != nil {
		t.Fatalf("generate github-actions (auth) failed: %v", err)
	}

	ciPath := filepath.Join(outDir, "ci.yml")
	data, err := os.ReadFile(ciPath)
	if err != nil {
		t.Fatalf("failed to read ci.yml: %v", err)
	}

	if !strings.Contains(string(data), "JWT_SECRET") {
		t.Error("ci.yml should include JWT_SECRET for auth projects")
	}
}

func TestRunGenerateGithubActionsWithDatabase(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfigFile(t, dir, configWithDatabase)
	outDir := filepath.Join(dir, ".github", "workflows")

	err := runGenerateGithubActions([]string{"-output", outDir, cfgPath})
	if err != nil {
		t.Fatalf("generate github-actions (db) failed: %v", err)
	}

	ciPath := filepath.Join(outDir, "ci.yml")
	data, err := os.ReadFile(ciPath)
	if err != nil {
		t.Fatalf("failed to read ci.yml: %v", err)
	}

	if !strings.Contains(string(data), "migrate") {
		t.Error("ci.yml should include migration step for database projects")
	}
}

func TestRunGenerateGithubActionsWithPlugin(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfigFile(t, dir, generateMinimalConfig)

	// Create plugin.json alongside the config
	pluginJSON := `{"name": "test-plugin", "version": "0.1.0"}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(pluginJSON), 0640); err != nil {
		t.Fatalf("failed to write plugin.json: %v", err)
	}

	outDir := filepath.Join(dir, ".github", "workflows")

	err := runGenerateGithubActions([]string{"-output", outDir, cfgPath})
	if err != nil {
		t.Fatalf("generate github-actions (plugin) failed: %v", err)
	}

	// release.yml should be generated when plugin.json exists
	relPath := filepath.Join(outDir, "release.yml")
	if _, err := os.Stat(relPath); os.IsNotExist(err) {
		t.Error("release.yml should be generated when plugin.json exists")
	}

	data, err := os.ReadFile(relPath)
	if err != nil {
		t.Fatalf("failed to read release.yml: %v", err)
	}
	content := string(data)
	for _, name := range []string{"ci.yml", "cd.yml", "release.yml"} {
		workflow, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Fatalf("read generated %s: %v", name, err)
		}
		t.Run(name, func(t *testing.T) {
			assertGeneratedWorkflowGoVersion(t, workflow)
		})
	}
	if strings.Contains(content, "softprops/action-gh-release") {
		t.Error("release.yml should not use softprops/action-gh-release")
	}
	if !strings.Contains(content, "gh release create") {
		t.Error("release.yml should publish releases with gh release create")
	}
	if !strings.Contains(content, "GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}") {
		t.Error("release.yml should authenticate gh with GH_TOKEN")
	}
}

func TestRunGenerateGithubActionsCIOnly(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfigFile(t, dir, generateMinimalConfig)
	outDir := filepath.Join(dir, ".github", "workflows")

	err := runGenerateGithubActions([]string{"-cd=false", "-output", outDir, cfgPath})
	if err != nil {
		t.Fatalf("generate github-actions CI-only failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outDir, "ci.yml")); os.IsNotExist(err) {
		t.Error("ci.yml should be generated")
	}
	if _, err := os.Stat(filepath.Join(outDir, "cd.yml")); err == nil {
		t.Error("cd.yml should not be generated when -cd=false")
	}
}

func TestRunGenerateGithubActionsCDOnly(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfigFile(t, dir, generateMinimalConfig)
	outDir := filepath.Join(dir, ".github", "workflows")

	err := runGenerateGithubActions([]string{"-ci=false", "-output", outDir, cfgPath})
	if err != nil {
		t.Fatalf("generate github-actions CD-only failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outDir, "ci.yml")); err == nil {
		t.Error("ci.yml should not be generated when -ci=false")
	}
	if _, err := os.Stat(filepath.Join(outDir, "cd.yml")); os.IsNotExist(err) {
		t.Error("cd.yml should be generated")
	}
}

func TestRunGenerateGithubActionsCustomRegistry(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfigFile(t, dir, generateMinimalConfig)
	outDir := filepath.Join(dir, ".github", "workflows")

	err := runGenerateGithubActions([]string{"-registry", "registry.mycompany.com", "-output", outDir, cfgPath})
	if err != nil {
		t.Fatalf("generate github-actions (custom registry) failed: %v", err)
	}

	cdPath := filepath.Join(outDir, "cd.yml")
	data, err := os.ReadFile(cdPath)
	if err != nil {
		t.Fatalf("failed to read cd.yml: %v", err)
	}
	if !strings.Contains(string(data), "registry.mycompany.com") {
		t.Error("cd.yml should use the custom registry")
	}
}

func TestDetectProjectFeaturesMinimal(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfigFile(t, dir, generateMinimalConfig)

	features, err := detectProjectFeatures(cfgPath)
	if err != nil {
		t.Fatalf("detectProjectFeatures failed: %v", err)
	}

	if features.hasUI {
		t.Error("expected hasUI=false for minimal config")
	}
	if features.hasAuth {
		t.Error("expected hasAuth=false for minimal config")
	}
	if features.hasDatabase {
		t.Error("expected hasDatabase=false for minimal config")
	}
	if features.hasPlugin {
		t.Error("expected hasPlugin=false without plugin.json")
	}
}

func TestDetectProjectFeaturesUI(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfigFile(t, dir, configWithUI)

	features, err := detectProjectFeatures(cfgPath)
	if err != nil {
		t.Fatalf("detectProjectFeatures (UI) failed: %v", err)
	}

	if !features.hasUI {
		t.Error("expected hasUI=true for config with static.fileserver")
	}
}

func TestDetectProjectFeaturesAuth(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfigFile(t, dir, configWithAuth)

	features, err := detectProjectFeatures(cfgPath)
	if err != nil {
		t.Fatalf("detectProjectFeatures (auth) failed: %v", err)
	}

	if !features.hasAuth {
		t.Error("expected hasAuth=true for config with auth.jwt")
	}
}

func TestDetectProjectFeaturesDatabase(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfigFile(t, dir, configWithDatabase)

	features, err := detectProjectFeatures(cfgPath)
	if err != nil {
		t.Fatalf("detectProjectFeatures (database) failed: %v", err)
	}

	if !features.hasDatabase {
		t.Error("expected hasDatabase=true for config with storage.sqlite")
	}
}

func TestDetectProjectFeaturesPlugin(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfigFile(t, dir, generateMinimalConfig)
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{}`), 0640); err != nil {
		t.Fatalf("failed to write plugin.json: %v", err)
	}

	features, err := detectProjectFeatures(cfgPath)
	if err != nil {
		t.Fatalf("detectProjectFeatures (plugin) failed: %v", err)
	}

	if !features.hasPlugin {
		t.Error("expected hasPlugin=true when plugin.json exists")
	}
}

func TestCIWorkflowContent(t *testing.T) {
	dir := t.TempDir()
	features := &projectFeatures{configFile: "workflow.yaml"}
	path := filepath.Join(dir, "ci.yml")

	if err := writeCIWorkflow(path, features); err != nil {
		t.Fatalf("writeCIWorkflow failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read ci.yml: %v", err)
	}
	content := string(data)
	assertGeneratedWorkflowGoVersion(t, data)

	if !strings.Contains(content, "actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3") {
		t.Error("ci.yml should use SHA-pinned actions/checkout v6.0.3")
	}
	if !strings.Contains(content, "actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0") {
		t.Error("ci.yml should use SHA-pinned actions/setup-go v6.4.0")
	}
	if !strings.Contains(content, "wfctl validate") {
		t.Error("ci.yml should include wfctl validate step")
	}
	if !strings.Contains(content, "wfctl inspect") {
		t.Error("ci.yml should include wfctl inspect step")
	}
}

func TestCDWorkflowContent(t *testing.T) {
	dir := t.TempDir()
	features := &projectFeatures{configFile: "workflow.yaml"}
	path := filepath.Join(dir, "cd.yml")

	if err := writeCDWorkflow(path, features, "ghcr.io", "linux/amd64,linux/arm64"); err != nil {
		t.Fatalf("writeCDWorkflow failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read cd.yml: %v", err)
	}
	content := string(data)
	assertGeneratedWorkflowGoVersion(t, data)

	if !strings.Contains(content, "tags: ['v*']") {
		t.Error("cd.yml should trigger on version tags")
	}
	if !strings.Contains(content, "ghcr.io") {
		t.Error("cd.yml should use the configured registry")
	}
	if !strings.Contains(content, "linux/amd64,linux/arm64") {
		t.Error("cd.yml should include the build platforms")
	}
	if !strings.Contains(content, "docker/build-push-action") {
		t.Error("cd.yml should use docker/build-push-action")
	}
}

func TestReleaseWorkflowContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "release.yml")

	if err := writeReleaseWorkflow(path); err != nil {
		t.Fatalf("writeReleaseWorkflow failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read release.yml: %v", err)
	}
	content := string(data)
	assertGeneratedWorkflowGoVersion(t, data)

	if strings.Contains(content, "softprops/action-gh-release") {
		t.Error("release.yml should not use softprops/action-gh-release")
	}
	if !strings.Contains(content, "gh release create") {
		t.Error("release.yml should publish releases with gh release create")
	}
	if !strings.Contains(content, "GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}") {
		t.Error("release.yml should authenticate gh with GH_TOKEN")
	}
	if !strings.Contains(content, "dist/*") {
		t.Error("release.yml should upload dist/* files")
	}
}

// TestInitTemplatesIncludeGithubWorkflows checks that wfctl init now generates .github/workflows/.
func TestInitTemplatesIncludeGithubWorkflows(t *testing.T) {
	cases := []struct {
		template  string
		wfFile    string
		goBuilder string
	}{
		{"api-service", ".github/workflows/ci.yml", "builder"},
		{"event-processor", ".github/workflows/ci.yml", "builder"},
		{"full-stack", ".github/workflows/ci.yml", "go-builder"},
		{"plugin", ".github/workflows/release.yml", ""},
		{"ui-plugin", ".github/workflows/release.yml", ""},
	}

	for _, tc := range cases {
		t.Run(tc.template, func(t *testing.T) {
			dir := t.TempDir()
			outDir := filepath.Join(dir, "project")
			err := runInit([]string{"--template", tc.template, "--author", "myorg", "--output", outDir, "project"})
			if err != nil {
				t.Fatalf("init %s failed: %v", tc.template, err)
			}
			wfPath := filepath.Join(outDir, tc.wfFile)
			workflow, err := os.ReadFile(wfPath)
			if err != nil {
				t.Fatalf("read generated %s: %v", tc.wfFile, err)
			}
			assertGeneratedWorkflowGoVersion(t, workflow)

			modPath := filepath.Join(outDir, "go.mod")
			modData, err := os.ReadFile(modPath)
			if err != nil {
				t.Fatalf("read generated go.mod: %v", err)
			}
			mod, err := modfile.Parse(modPath, modData, nil)
			if err != nil {
				t.Fatalf("parse generated go.mod: %v", err)
			}
			if mod.Go == nil {
				t.Fatal("generated go.mod has no Go directive")
			}
			if mod.Go.Version != "1.27.2" {
				t.Errorf("generated go.mod Go version = %q, want 1.27.2", mod.Go.Version)
			}

			if tc.goBuilder != "" {
				assertGeneratedMainDefaultPlugins(t, filepath.Join(outDir, "main.go"))
				configData, err := os.ReadFile(filepath.Join(outDir, "workflow.yaml"))
				if err != nil {
					t.Fatalf("read generated Workflow config: %v", err)
				}
				var appConfig struct {
					Modules []struct {
						Type   string `yaml:"type"`
						Config struct {
							HealthPath string `yaml:"healthPath"`
						} `yaml:"config"`
					} `yaml:"modules"`
					Workflows struct {
						HTTP struct {
							Routes []struct {
								Path string `yaml:"path"`
							} `yaml:"routes"`
						} `yaml:"http"`
					} `yaml:"workflows"`
					Triggers map[string]any `yaml:"triggers"`
				}
				if err := yaml.Unmarshal(configData, &appConfig); err != nil {
					t.Fatalf("parse generated Workflow config: %v", err)
				}
				if len(appConfig.Modules) == 0 {
					t.Fatal("generated application has no modules")
				}
				knownTypes := make(map[string]bool)
				knownTriggers := make(map[string]bool)
				for _, p := range allplugins.DefaultPlugins() {
					for moduleType := range p.ModuleFactories() {
						knownTypes[moduleType] = true
					}
					for triggerType := range p.TriggerFactories() {
						knownTriggers[triggerType] = true
					}
				}
				for triggerType := range appConfig.Triggers {
					if !knownTriggers[triggerType] {
						t.Errorf("default generated app references unavailable trigger %q", triggerType)
					}
				}
				healthCheckers := 0
				for _, m := range appConfig.Modules {
					if !knownTypes[m.Type] {
						t.Errorf("default generated app references unavailable module %q", m.Type)
					}
					if m.Type == "health.checker" {
						healthCheckers++
						if m.Config.HealthPath != "/health" {
							t.Errorf("generated health checker path = %q, want /health", m.Config.HealthPath)
						}
					}
				}
				if healthCheckers != 1 {
					t.Errorf("generated health checkers = %d, want 1", healthCheckers)
				}
				for _, route := range appConfig.Workflows.HTTP.Routes {
					if route.Path == "/health" {
						t.Error("generated /health must use the health-checker wiring, not a manual HTTP route")
					}
				}
				dockerfile, err := readFileContents(filepath.Join(outDir, "Dockerfile"))
				if err != nil {
					t.Fatalf("read generated Dockerfile: %v", err)
				}
				want := "FROM golang:1.27.2-alpine AS " + tc.goBuilder
				if !strings.Contains(dockerfile, want) {
					t.Errorf("generated Dockerfile missing %q", want)
				}
				readme, err := readFileContents(filepath.Join(outDir, "README.md"))
				if err != nil {
					t.Fatalf("read generated README.md: %v", err)
				}
				if !strings.Contains(readme, "- Go 1.27.2+") {
					t.Error("generated README.md missing Go 1.27.2+ prerequisite")
				}
			}
		})
	}
}

func assertGeneratedMainDefaultPlugins(t *testing.T, path string) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse generated main.go: %v", err)
	}
	imported := false
	for _, spec := range file.Imports {
		if spec.Path.Value == `"github.com/GoCodeAlone/workflow/plugins/all"` && spec.Name != nil && spec.Name.Name == "allplugins" {
			imported = true
		}
	}
	if !imported {
		t.Error("generated main.go must import plugins/all as allplugins")
	}
	registered := false
	ast.Inspect(file, func(node ast.Node) bool {
		build, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		buildMethod, ok := build.Fun.(*ast.SelectorExpr)
		if !ok || buildMethod.Sel.Name != "BuildFromConfig" {
			return true
		}
		plugins, ok := buildMethod.X.(*ast.CallExpr)
		if !ok || len(plugins.Args) != 1 || !plugins.Ellipsis.IsValid() {
			return true
		}
		pluginsMethod, ok := plugins.Fun.(*ast.SelectorExpr)
		if !ok || pluginsMethod.Sel.Name != "WithPlugins" {
			return true
		}
		defaults, ok := pluginsMethod.X.(*ast.CallExpr)
		if !ok || len(defaults.Args) != 0 {
			return true
		}
		defaultsMethod, ok := defaults.Fun.(*ast.SelectorExpr)
		if !ok || defaultsMethod.Sel.Name != "WithAllDefaults" {
			return true
		}
		defaultPlugins, ok := plugins.Args[0].(*ast.CallExpr)
		if !ok || len(defaultPlugins.Args) != 0 {
			return true
		}
		defaultPluginsMethod, ok := defaultPlugins.Fun.(*ast.SelectorExpr)
		if !ok || defaultPluginsMethod.Sel.Name != "DefaultPlugins" {
			return true
		}
		alias, ok := defaultPluginsMethod.X.(*ast.Ident)
		registered = ok && alias.Name == "allplugins"
		return true
	})
	if !registered {
		t.Error("generated main.go must call WithAllDefaults().WithPlugins(allplugins.DefaultPlugins()...).BuildFromConfig")
	}
}

func assertGeneratedWorkflowGoVersion(t *testing.T, data []byte) {
	t.Helper()
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Uses string            `yaml:"uses"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("parse generated workflow: %v", err)
	}
	found := false
	for name, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if strings.HasPrefix(step.Uses, "actions/setup-go@") {
				found = true
				if got := step.With["go-version"]; got != "1.27.2" {
					t.Errorf("job %s setup-go version = %q, want 1.27.2", name, got)
				}
			}
		}
	}
	if !found {
		t.Error("generated workflow has no setup-go step")
	}
}
