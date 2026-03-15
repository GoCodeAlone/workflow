package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestDeepMergeConfigs_NilHandling(t *testing.T) {
	base := &WorkflowConfig{
		Modules: []ModuleConfig{{Name: "a", Type: "http.server"}},
	}

	if got := DeepMergeConfigs(nil, base); got != base {
		t.Error("expected nil base to return override")
	}
	if got := DeepMergeConfigs(base, nil); got != base {
		t.Error("expected nil override to return base")
	}
	if got := DeepMergeConfigs(nil, nil); got != nil {
		t.Error("expected nil,nil to return nil")
	}
}

func TestDeepMergeConfigs_ModuleNameMatching(t *testing.T) {
	base := &WorkflowConfig{
		Modules: []ModuleConfig{
			{
				Name:   "db",
				Type:   "postgres",
				Config: map[string]any{"host": "localhost", "port": 5432},
			},
		},
	}
	override := &WorkflowConfig{
		Modules: []ModuleConfig{
			{
				Name:   "db",
				Config: map[string]any{"host": "prod-db", "ssl": true},
			},
		},
	}

	result := DeepMergeConfigs(base, override)

	if len(result.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(result.Modules))
	}
	mod := result.Modules[0]
	if mod.Config["host"] != "prod-db" {
		t.Errorf("expected host=prod-db (override wins), got %v", mod.Config["host"])
	}
	if mod.Config["port"] != 5432 {
		t.Errorf("expected port=5432 (base preserved), got %v", mod.Config["port"])
	}
	if mod.Config["ssl"] != true {
		t.Errorf("expected ssl=true (new key from override), got %v", mod.Config["ssl"])
	}
	// type should be preserved from base when override type is empty
	if mod.Type != "postgres" {
		t.Errorf("expected type=postgres preserved, got %q", mod.Type)
	}
}

func TestDeepMergeConfigs_NewModulesAppended(t *testing.T) {
	base := &WorkflowConfig{
		Modules: []ModuleConfig{{Name: "a", Type: "typeA"}},
	}
	override := &WorkflowConfig{
		Modules: []ModuleConfig{
			{Name: "b", Type: "typeB"},
			{Name: "c", Type: "typeC"},
		},
	}

	result := DeepMergeConfigs(base, override)

	if len(result.Modules) != 3 {
		t.Fatalf("expected 3 modules, got %d", len(result.Modules))
	}
	names := make(map[string]bool)
	for _, m := range result.Modules {
		names[m.Name] = true
	}
	for _, n := range []string{"a", "b", "c"} {
		if !names[n] {
			t.Errorf("expected module %q in result", n)
		}
	}
}

func TestDeepMergeConfigs_WorkflowOverride(t *testing.T) {
	base := &WorkflowConfig{
		Workflows: map[string]any{
			"wf-a": map[string]any{"type": "http"},
			"wf-b": map[string]any{"type": "schedule"},
		},
	}
	override := &WorkflowConfig{
		Workflows: map[string]any{
			"wf-a": map[string]any{"type": "http", "timeout": "30s"},
		},
	}

	result := DeepMergeConfigs(base, override)

	wfA, ok := result.Workflows["wf-a"].(map[string]any)
	if !ok {
		t.Fatal("wf-a should be map")
	}
	if wfA["timeout"] != "30s" {
		t.Errorf("expected timeout=30s from override, got %v", wfA["timeout"])
	}
	if _, exists := result.Workflows["wf-b"]; !exists {
		t.Error("wf-b should be preserved from base")
	}
}

func TestDeepMergeConfigs_PipelineOverride(t *testing.T) {
	base := &WorkflowConfig{
		Pipelines: map[string]any{
			"pipe-1": map[string]any{"steps": []any{"a", "b"}},
		},
	}
	override := &WorkflowConfig{
		Pipelines: map[string]any{
			"pipe-1": map[string]any{"steps": []any{"x", "y", "z"}},
			"pipe-2": map[string]any{"steps": []any{"p"}},
		},
	}

	result := DeepMergeConfigs(base, override)

	if _, ok := result.Pipelines["pipe-2"]; !ok {
		t.Error("pipe-2 should be present from override")
	}
	p1, _ := result.Pipelines["pipe-1"].(map[string]any)
	steps, _ := p1["steps"].([]any)
	if len(steps) != 3 {
		t.Errorf("expected 3 steps from override, got %d", len(steps))
	}
}

func TestDeepMergeConfigs_NestedMapRecursion(t *testing.T) {
	base := &WorkflowConfig{
		Workflows: map[string]any{
			"wf": map[string]any{
				"config": map[string]any{
					"nested": map[string]any{
						"a": "base-a",
						"b": "base-b",
					},
				},
			},
		},
	}
	override := &WorkflowConfig{
		Workflows: map[string]any{
			"wf": map[string]any{
				"config": map[string]any{
					"nested": map[string]any{
						"a": "override-a",
						"c": "new-c",
					},
				},
			},
		},
	}

	result := DeepMergeConfigs(base, override)

	wf, _ := result.Workflows["wf"].(map[string]any)
	cfg, _ := wf["config"].(map[string]any)
	nested, _ := cfg["nested"].(map[string]any)

	if nested["a"] != "override-a" {
		t.Errorf("expected a=override-a, got %v", nested["a"])
	}
	if nested["b"] != "base-b" {
		t.Errorf("expected b=base-b preserved, got %v", nested["b"])
	}
	if nested["c"] != "new-c" {
		t.Errorf("expected c=new-c from override, got %v", nested["c"])
	}
}

func TestDeepMergeMap(t *testing.T) {
	base := map[string]any{
		"x": 1,
		"y": map[string]any{"inner": "base"},
	}
	override := map[string]any{
		"x": 2,
		"y": map[string]any{"inner": "override", "extra": true},
		"z": "new",
	}

	result := deepMergeMap(base, override)

	if result["x"] != 2 {
		t.Errorf("expected x=2 (override wins), got %v", result["x"])
	}
	if result["z"] != "new" {
		t.Errorf("expected z=new, got %v", result["z"])
	}
	inner, _ := result["y"].(map[string]any)
	if inner["inner"] != "override" {
		t.Errorf("expected inner=override, got %v", inner["inner"])
	}
	if inner["extra"] != true {
		t.Errorf("expected extra=true, got %v", inner["extra"])
	}
}

func TestDeepMergeMap_NilBothReturnsNil(t *testing.T) {
	result := deepMergeMap(nil, nil)
	if result != nil {
		t.Error("expected nil result for nil,nil inputs")
	}
}

func TestDeepMergeMap_NilBase(t *testing.T) {
	override := map[string]any{"a": 1}
	result := deepMergeMap(nil, override)
	if !reflect.DeepEqual(result, override) {
		t.Errorf("expected result to equal override, got %v", result)
	}
}

func TestDeepMergeMap_NilOverride(t *testing.T) {
	base := map[string]any{"a": 1}
	result := deepMergeMap(base, nil)
	if !reflect.DeepEqual(result, base) {
		t.Errorf("expected result to equal base, got %v", result)
	}
}

func TestDeepMergeConfigs_ConfigDirOverride(t *testing.T) {
	base := &WorkflowConfig{ConfigDir: "/base/dir"}
	override := &WorkflowConfig{ConfigDir: "/override/dir"}

	result := DeepMergeConfigs(base, override)
	if result.ConfigDir != "/override/dir" {
		t.Errorf("expected ConfigDir=/override/dir, got %q", result.ConfigDir)
	}
}

func TestDeepMergeConfigs_ConfigDirBasePreservedWhenOverrideEmpty(t *testing.T) {
	base := &WorkflowConfig{ConfigDir: "/base/dir"}
	override := &WorkflowConfig{}

	result := DeepMergeConfigs(base, override)
	if result.ConfigDir != "/base/dir" {
		t.Errorf("expected ConfigDir=/base/dir preserved, got %q", result.ConfigDir)
	}
}

func TestDeepMergeConfigs_RequiresOverride(t *testing.T) {
	base := &WorkflowConfig{
		Requires: &RequiresConfig{Capabilities: []string{"base-cap"}},
	}
	override := &WorkflowConfig{
		Requires: &RequiresConfig{Capabilities: []string{"override-cap"}},
	}

	result := DeepMergeConfigs(base, override)
	if len(result.Requires.Capabilities) != 1 || result.Requires.Capabilities[0] != "override-cap" {
		t.Errorf("expected override Requires, got %v", result.Requires)
	}
}

func TestMergeWorkflowSection_NilDestinationList(t *testing.T) {
	// When dst[k] is nil (e.g. `routes:` with no value in YAML), the src list
	// should be used rather than being silently dropped.
	dst := map[string]any{
		"routes": nil,
	}
	src := map[string]any{
		"routes": []any{
			map[string]any{"method": "GET", "path": "/v2/resource"},
		},
	}
	mergeWorkflowSection(dst, src)

	routes, ok := dst["routes"].([]any)
	if !ok {
		t.Fatal("expected dst[routes] to be []any after merge")
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	route, _ := routes[0].(map[string]any)
	if route["path"] != "/v2/resource" {
		t.Errorf("expected path=/v2/resource, got %v", route["path"])
	}
}

func TestMergeWorkflowSection_NonSliceDestinationReplaced(t *testing.T) {
	// When dst[k] exists but holds a non-slice value (unexpected YAML shape),
	// the src list should replace it so items are not lost.
	dst := map[string]any{
		"routes": "not-a-list",
	}
	src := map[string]any{
		"routes": []any{
			map[string]any{"method": "POST", "path": "/v2/submit"},
		},
	}
	mergeWorkflowSection(dst, src)

	routes, ok := dst["routes"].([]any)
	if !ok {
		t.Fatal("expected dst[routes] to be []any after merge")
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
}

func TestMergeWorkflowSection_AppendsToExistingList(t *testing.T) {
	dst := map[string]any{
		"routes": []any{
			map[string]any{"method": "GET", "path": "/v1/resource"},
		},
	}
	src := map[string]any{
		"routes": []any{
			map[string]any{"method": "GET", "path": "/v2/resource"},
		},
	}
	mergeWorkflowSection(dst, src)

	routes, ok := dst["routes"].([]any)
	if !ok {
		t.Fatal("expected dst[routes] to be []any after merge")
	}
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}
}

func TestDeepMergeConfigs_ModuleTypeOverride(t *testing.T) {
	base := &WorkflowConfig{
		Modules: []ModuleConfig{{Name: "svc", Type: "old-type"}},
	}
	override := &WorkflowConfig{
		Modules: []ModuleConfig{{Name: "svc", Type: "new-type"}},
	}

	result := DeepMergeConfigs(base, override)
	if result.Modules[0].Type != "new-type" {
		t.Errorf("expected type=new-type from override, got %q", result.Modules[0].Type)
	}
}

func TestMergeApplicationConfig_NilWorkflowKeyReplacedByLaterFile(t *testing.T) {
	// If the first workflow file defines a workflow key with a null body (e.g.
	// `http:` with no content), the second file's non-null value should be used
	// rather than being silently dropped by the "first-definition-wins" logic.
	dir := t.TempDir()

	const file1 = `
modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
workflows:
  http:
triggers:
  http:
    server: server
`
	const file2 = `
modules:
  - name: ping-handler
    type: http.handler
workflows:
  http:
    routes:
      - method: GET
        path: /ping
        handler: ping-handler
`
	writeFile := func(name, content string) string {
		path := dir + "/" + name
		if err := writeFileContent(path, content); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return path
	}
	file1Path := writeFile("base.yaml", file1)
	file2Path := writeFile("routes.yaml", file2)
	_ = file1Path

	appCfg := &ApplicationConfig{
		ConfigDir: dir,
		Application: ApplicationInfo{
			Name: "null-key-test",
			Workflows: []WorkflowRef{
				{File: "base.yaml", Name: "base"},
				{File: "routes.yaml", Name: "routes"},
			},
		},
	}
	_ = file2Path

	combined, err := MergeApplicationConfig(appCfg)
	if err != nil {
		t.Fatalf("MergeApplicationConfig failed: %v", err)
	}

	httpSection, ok := combined.Workflows["http"]
	if !ok {
		t.Fatal("expected 'http' key in merged Workflows")
	}
	if httpSection == nil {
		t.Fatal("expected 'http' to be non-nil after merge with null first file")
	}
	httpMap, ok := httpSection.(map[string]any)
	if !ok {
		t.Fatalf("expected 'http' to be map[string]any, got %T", httpSection)
	}
	routes, ok := httpMap["routes"].([]any)
	if !ok || len(routes) == 0 {
		t.Error("expected routes from second file to be present after null-key merge")
	}
}

func TestMergeApplicationConfig_DuplicateModuleNameReturnsError(t *testing.T) {
	dir := t.TempDir()

	const file1 = `
modules:
  - name: shared-module
    type: http.server
`
	const file2 = `
modules:
  - name: shared-module
    type: http.handler
`
	if err := writeFileContent(dir+"/svc1.yaml", file1); err != nil {
		t.Fatal(err)
	}
	if err := writeFileContent(dir+"/svc2.yaml", file2); err != nil {
		t.Fatal(err)
	}

	appCfg := &ApplicationConfig{
		ConfigDir: dir,
		Application: ApplicationInfo{
			Name: "conflict-test",
			Workflows: []WorkflowRef{
				{File: "svc1.yaml", Name: "svc1"},
				{File: "svc2.yaml", Name: "svc2"},
			},
		},
	}

	_, err := MergeApplicationConfig(appCfg)
	if err == nil {
		t.Fatal("expected error for duplicate module name, got nil")
	}
	if !contains(err.Error(), "shared-module") {
		t.Errorf("error should mention conflicting module name, got: %v", err)
	}
}

func TestMergeApplicationConfig_DuplicatePipelineNameReturnsError(t *testing.T) {
	dir := t.TempDir()

	const file1 = `
modules:
  - name: handler-a
    type: http.handler
pipelines:
  my-pipeline:
    steps:
      - name: step1
        type: step.log
`
	const file2 = `
modules:
  - name: handler-b
    type: http.handler
pipelines:
  my-pipeline:
    steps:
      - name: step1
        type: step.log
`
	if err := writeFileContent(dir+"/svc1.yaml", file1); err != nil {
		t.Fatal(err)
	}
	if err := writeFileContent(dir+"/svc2.yaml", file2); err != nil {
		t.Fatal(err)
	}

	appCfg := &ApplicationConfig{
		ConfigDir: dir,
		Application: ApplicationInfo{
			Name: "pipeline-conflict",
			Workflows: []WorkflowRef{
				{File: "svc1.yaml", Name: "svc1"},
				{File: "svc2.yaml", Name: "svc2"},
			},
		},
	}

	_, err := MergeApplicationConfig(appCfg)
	if err == nil {
		t.Fatal("expected error for duplicate pipeline name, got nil")
	}
	if !contains(err.Error(), "my-pipeline") {
		t.Errorf("error should mention conflicting pipeline name, got: %v", err)
	}
}

// writeFileContent writes content to path (helper for MergeApplicationConfig tests).
func writeFileContent(path, content string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}

// contains is a helper wrapping strings.Contains for use in this test file.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestMergeApplicationConfig_PluginDedup(t *testing.T) {
	dir := t.TempDir()

	// Workflow A declares plugin "foo"
	wfA := dir + "/a.yaml"
	writeFileContent(wfA, `
plugins:
  external:
    - name: foo
      version: "1.0"
      repository: "https://example.com/foo"
modules: []
`)

	// Workflow B declares plugin "foo" (duplicate) and "bar"
	wfB := dir + "/b.yaml"
	writeFileContent(wfB, `
plugins:
  external:
    - name: foo
      version: "2.0"
      repository: "https://example.com/foo-v2"
    - name: bar
      version: "1.0"
      repository: "https://example.com/bar"
modules: []
`)

	appCfg := &ApplicationConfig{
		Application: ApplicationInfo{
			Workflows: []WorkflowRef{
				{File: wfA},
				{File: wfB},
			},
		},
	}

	cfg, err := MergeApplicationConfig(appCfg)
	if err != nil {
		t.Fatalf("MergeApplicationConfig: %v", err)
	}

	if cfg.Plugins == nil {
		t.Fatal("expected Plugins to be non-nil after merge")
	}
	if len(cfg.Plugins.External) != 2 {
		t.Fatalf("expected 2 plugins (foo + bar), got %d", len(cfg.Plugins.External))
	}

	// First definition wins — foo should have version "1.0" from workflow A
	var fooVer string
	for _, p := range cfg.Plugins.External {
		if p.Name == "foo" {
			fooVer = p.Version
		}
	}
	if fooVer != "1.0" {
		t.Errorf("expected foo version 1.0 (first definition wins), got %s", fooVer)
	}
}
