package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/schema"
)

// TestMain forces WFCTL_DIFFCACHE=disabled across the cmd/wfctl test
// suite. The platform diffcache (consumed by computeInfraPlan via
// the var-seam introduced in W-3b T3.6c/f) lazily initializes its
// process-level planDiffCache from this env var on the first
// ComputePlan call. Without this override, tests that depend on
// driver.Diff actually firing would observe stale entries from a
// developer's local ~/.cache/wfctl/diff/ as false-positive cache
// hits. Disabled is the safe default for tests; cache-hit semantics
// are exercised by platform/differ_cache_test.go via its own
// in-memory swap.
func TestMain(m *testing.M) {
	if err := os.Setenv("WFCTL_DIFFCACHE", "disabled"); err != nil {
		panic("setenv WFCTL_DIFFCACHE: " + err.Error())
	}
	os.Exit(m.Run())
}

func TestHelpFlagDoesNotLeakEngineError(t *testing.T) {
	if !isHelpRequested(flag.ErrHelp) {
		t.Error("isHelpRequested should return true for flag.ErrHelp")
	}
	if isHelpRequested(nil) {
		t.Error("isHelpRequested should return false for nil")
	}
	if isHelpRequested(errors.New("some other error")) {
		t.Error("isHelpRequested should return false for unrelated errors")
	}
	// Wrapped error should also be detected
	wrapped := fmt.Errorf("pipeline failed: %w", flag.ErrHelp)
	if !isHelpRequested(wrapped) {
		t.Error("isHelpRequested should return true for wrapped flag.ErrHelp")
	}
}

func TestBuildVersionStripsDirtyMarker(t *testing.T) {
	// cleanBuildVersion must strip +dirty from both release tags and pseudo-versions.
	for _, tc := range []struct {
		in   string
		want string
	}{
		{
			in:   "v0.22.8-0.20260510180701-a851625d3bf0+dirty",
			want: "v0.22.8-0.20260510180701-a851625d3bf0",
		},
		{
			in:   "v0.51.2+dirty",
			want: "v0.51.2",
		},
		{
			in:   "v0.51.2",
			want: "v0.51.2",
		},
		{
			in:   "v0.22.8-0.20260510180701-a851625d3bf0",
			want: "v0.22.8-0.20260510180701-a851625d3bf0",
		},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got := cleanBuildVersion(tc.in)
			if got != tc.want {
				t.Fatalf("cleanBuildVersion(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	// buildVersion() itself must never return a value ending in +dirty.
	v := buildVersion()
	if strings.HasSuffix(v, "+dirty") {
		t.Fatalf("buildVersion() = %q, must not end in +dirty", v)
	}
}

func TestLinkedVersionOverridesBuildInfo(t *testing.T) {
	exeName := "wfctl"
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	exe := filepath.Join(t.TempDir(), exeName)
	build := exec.Command("go", "build", "-o", exe, "-ldflags", "-X main.version=v9.9.9", ".")
	build.Env = append(os.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build wfctl: %v\n%s", err, out)
	}

	run := exec.Command(exe, "--version")
	run.Env = append(os.Environ(), "WFCTL_NO_UPDATE_CHECK=1", "CI=true")
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("wfctl --version: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "v9.9.9" {
		t.Fatalf("linked version = %q, want v9.9.9", got)
	}
}

// TestSecretsSetupNonTTYDoesNotHang is the binary-level regression guard for
// the two findings in PR2 code review:
//
//  1. interactive valuer ErrNotInteractive must surface (so the non-interactive
//     fallback triggers), and
//  2. the non-TTY path must never block on stdin (Fscanln / open empty pipe).
//
// It builds the real wfctl binary and runs `secrets setup --config <tmp>` with
// EMPTY stdin piped (a non-TTY pipe that yields EOF), under a hard deadline.
// Asserts: exits non-zero, output names the missing secret, and the process
// returns well before the deadline (no hang).
func TestSecretsSetupNonTTYDoesNotHang(t *testing.T) {
	exeName := "wfctl"
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, exeName)
	build := exec.Command("go", "build", "-o", exe, ".")
	build.Env = append(os.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build wfctl: %v\n%s", err, out)
	}

	// Minimal config: one declared secret, a writable file store, and an
	// http.server entry point so the config is otherwise valid.
	storeDir := filepath.Join(dir, "store")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatalf("mkdir store: %v", err)
	}
	cfg := `modules:
  - name: http
    type: http.server
    config:
      address: ":0"
secrets:
  defaultStore: localfs
  entries:
    - name: NEEDS_VALUE
secretStores:
  localfs:
    provider: file
    config:
      path: ` + storeDir + `
`
	cfgPath := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write app.yaml: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// No --non-interactive flag: the binary must DETECT the non-TTY pipe and
	// route to the non-interactive path rather than blocking on a prompt.
	run := exec.CommandContext(ctx, exe, "secrets", "setup", "--config", cfgPath)
	run.Env = append(os.Environ(), "WFCTL_NO_UPDATE_CHECK=1", "CI=true")
	run.Stdin = strings.NewReader("") // empty, non-TTY stdin → immediate EOF
	var combined bytes.Buffer
	run.Stdout = &combined
	run.Stderr = &combined

	start := time.Now()
	err := run.Run()
	elapsed := time.Since(start)

	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("wfctl secrets setup HUNG (deadline exceeded after %s); output:\n%s", elapsed, combined.String())
	}
	if err == nil {
		t.Fatalf("expected non-zero exit (missing secret value), got success; output:\n%s", combined.String())
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T: %v\noutput:\n%s", err, err, combined.String())
	}
	if combined.Len() == 0 || !strings.Contains(combined.String(), "NEEDS_VALUE") {
		t.Fatalf("output should name the missing secret NEEDS_VALUE; got:\n%s", combined.String())
	}
	// Sanity: it must have returned promptly, not near the deadline.
	if elapsed > 15*time.Second {
		t.Fatalf("returned too slowly (%s) — possible partial hang; output:\n%s", elapsed, combined.String())
	}
}

func writeTestConfig(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
	return path
}

const validConfig = `
modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
  - name: router
    type: http.router
workflows:
  http:
    routes: []
triggers:
  http:
    port: 8080
`

const minimalConfig = `
modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
`

const invalidConfig = `
modules:
  - name: ""
    type: ""
`

func TestRunValidateValid(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, "valid.yaml", minimalConfig)
	if err := runValidate([]string{path}); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestRunValidateInvalid(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, "invalid.yaml", invalidConfig)
	err := runValidate([]string{path})
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
	if !strings.Contains(err.Error(), "failed validation") {
		t.Errorf("expected validation error, got: %v", err)
	}
}

func TestRunValidateStrictByDefault(t *testing.T) {
	dir := t.TempDir()
	emptyConfig := "modules: []\n"
	path := writeTestConfig(t, dir, "empty.yaml", emptyConfig)
	err := runValidate([]string{path})
	if err == nil {
		t.Fatal("expected error by default with empty modules")
	}
}

func TestRunValidateLooseAllowsEmptyModules(t *testing.T) {
	dir := t.TempDir()
	emptyConfig := "modules: []\n"
	path := writeTestConfig(t, dir, "empty.yaml", emptyConfig)
	if err := runValidate([]string{"--loose", path}); err != nil {
		t.Fatalf("expected --loose to allow empty modules, got: %v", err)
	}
	if err := runValidate([]string{"--non-strict", path}); err != nil {
		t.Fatalf("expected --non-strict to allow empty modules, got: %v", err)
	}
}

func TestRunValidateCatchesDBQueryCachedRowWrapperByDefault(t *testing.T) {
	dir := t.TempDir()
	cfg := `
modules:
  - name: router
    type: http.router
pipelines:
  payment-create-intent:
    trigger:
      type: http
      config:
        path: /api/v1/payments/intents
        method: POST
    steps:
      - name: check_mock_mode
        type: step.db_query_cached
        config:
          database: db
          query: "SELECT COALESCE((SELECT settings->>'mock_payments' FROM tenants WHERE id = $1), 'false') AS mock_payments"
          mode: single
          cache_key: tenant:test:mock_payments
      - name: set_mock_flag
        type: step.set
        config:
          values:
            is_mock: '{{ index .steps "check_mock_mode" "row" "mock_payments" | default "false" }}'
`
	path := writeTestConfig(t, dir, "payment.yaml", cfg)
	err := runValidate([]string{path})
	if err == nil {
		t.Fatal("expected validate to fail on stale db_query_cached row wrapper")
	}
	if !strings.Contains(err.Error(), "pipeline-refs warning") || !strings.Contains(err.Error(), "check_mock_mode.row") {
		t.Fatalf("validate error should mention pipeline refs and check_mock_mode.row, got: %v", err)
	}
	if err := runValidate([]string{"--loose", path}); err != nil {
		t.Fatalf("--loose should allow transitional pipeline reference warnings, got: %v", err)
	}
}

func TestRunValidateRejectsConditionalRoutesWithNonStringKeys(t *testing.T) {
	dir := t.TempDir()
	cfg := `
modules:
  - name: router
    type: http.router
pipelines:
  authz:
    trigger:
      type: mock
    steps:
      - name: route-by-authz
        type: step.conditional
        config:
          field: authz.allowed
          routes:
            true: allow
            false: deny
      - name: allow
        type: step.log
        config:
          message: allow
      - name: deny
        type: step.log
        config:
          message: deny
`
	path := writeTestConfig(t, dir, "conditional.yaml", cfg)

	err := runValidate([]string{"--skip-unknown-types", "--allow-no-entry-points", path})
	if err == nil {
		t.Fatal("expected validate to fail on non-string conditional route keys")
	}
	if !strings.Contains(err.Error(), "step.conditional") ||
		!strings.Contains(err.Error(), "routes") ||
		!strings.Contains(err.Error(), "'true'") {
		t.Fatalf("expected actionable conditional route key error, got: %v", err)
	}
}

func TestRunValidateRejectsImportedConditionalRoutesWithNonStringKeys(t *testing.T) {
	dir := t.TempDir()
	imported := `
pipelines:
  imported:
    steps:
      - name: route-by-authz
        type: step.conditional
        config:
          field: authz.allowed
          routes:
            true: allow
            false: deny
`
	writeTestConfig(t, dir, "imported.yaml", imported)
	cfg := `
imports:
  - imported.yaml
modules:
  - name: router
    type: http.router
pipelines: {}
`
	path := writeTestConfig(t, dir, "main.yaml", cfg)

	err := runValidate([]string{"--skip-unknown-types", "--allow-no-entry-points", path})
	if err == nil {
		t.Fatal("expected validate to fail on imported non-string conditional route keys")
	}
	if !strings.Contains(err.Error(), "imported.yaml") ||
		!strings.Contains(err.Error(), "step.conditional") ||
		!strings.Contains(err.Error(), "'true'") {
		t.Fatalf("expected actionable imported conditional route key error, got: %v", err)
	}
}

func TestRunValidateRejectsImportedConditionalRoutesWhenRootHasNoPipelines(t *testing.T) {
	dir := t.TempDir()
	imported := `
pipelines:
  imported:
    steps:
      - name: route-by-authz
        type: step.conditional
        config:
          field: authz.allowed
          routes:
            true: allow
            false: deny
`
	writeTestConfig(t, dir, "imported.yaml", imported)
	cfg := `
imports:
  - imported.yaml
modules:
  - name: router
    type: http.router
`
	path := writeTestConfig(t, dir, "main.yaml", cfg)

	err := runValidate([]string{"--skip-unknown-types", "--allow-no-entry-points", path})
	if err == nil {
		t.Fatal("expected validate to fail on imported non-string conditional route keys")
	}
	if !strings.Contains(err.Error(), "imported.yaml") ||
		!strings.Contains(err.Error(), "step.conditional") ||
		!strings.Contains(err.Error(), "'true'") {
		t.Fatalf("expected actionable imported conditional route key error, got: %v", err)
	}
}

func TestRunValidateRejectsAliasedConditionalRoutesWithNonStringKeys(t *testing.T) {
	dir := t.TempDir()
	cfg := `
shared:
  routes: &routes
    true: allow
    false: deny
  config: &condition
    field: authz.allowed
    routes: *routes
modules:
  - name: router
    type: http.router
pipelines:
  authz:
    steps:
      - name: route-by-authz
        type: step.conditional
        config: *condition
`
	path := writeTestConfig(t, dir, "conditional-alias.yaml", cfg)

	err := runValidate([]string{"--skip-unknown-types", "--allow-no-entry-points", path})
	if err == nil {
		t.Fatal("expected validate to fail on aliased non-string conditional route keys")
	}
	if !strings.Contains(err.Error(), "step.conditional") ||
		!strings.Contains(err.Error(), "routes") ||
		!strings.Contains(err.Error(), "'true'") {
		t.Fatalf("expected actionable aliased conditional route key error, got: %v", err)
	}
}

func TestRunValidateMissingArg(t *testing.T) {
	err := runValidate([]string{})
	if err == nil {
		t.Fatal("expected error when no config file given")
	}
}

func TestRunInspect(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, "config.yaml", validConfig)
	if err := runInspect([]string{path}); err != nil {
		t.Fatalf("inspect failed: %v", err)
	}
}

func TestRunInspectWithDeps(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, "config.yaml", validConfig)
	if err := runInspect([]string{"-deps", path}); err != nil {
		t.Fatalf("inspect with deps failed: %v", err)
	}
}

func TestRunInspectMissingArg(t *testing.T) {
	err := runInspect([]string{})
	if err == nil {
		t.Fatal("expected error when no config file given")
	}
}

func TestRunSchema(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "schema.json")
	if err := runSchema([]string{"-output", outPath}); err != nil {
		t.Fatalf("schema generation failed: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read schema output: %v", err)
	}

	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if schema["title"] == nil {
		t.Error("expected title in schema")
	}
}

func TestRunSchemaStdout(t *testing.T) {
	if err := runSchema([]string{}); err != nil {
		t.Fatalf("schema to stdout failed: %v", err)
	}
}

func TestRunValidateMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	path1 := writeTestConfig(t, dir, "a.yaml", minimalConfig)
	path2 := writeTestConfig(t, dir, "b.yaml", minimalConfig)
	if err := runValidate([]string{path1, path2}); err != nil {
		t.Fatalf("expected both valid, got error: %v", err)
	}
}

func TestRunValidateDir(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "a.yaml", minimalConfig)
	writeTestConfig(t, dir, "b.yaml", minimalConfig)
	if err := runValidate([]string{"--dir", dir}); err != nil {
		t.Fatalf("expected all valid, got error: %v", err)
	}
}

func TestRunValidateBatchWithFailure(t *testing.T) {
	dir := t.TempDir()
	path1 := writeTestConfig(t, dir, "good.yaml", minimalConfig)
	path2 := writeTestConfig(t, dir, "bad.yaml", invalidConfig)
	err := runValidate([]string{path1, path2})
	if err == nil {
		t.Fatal("expected error when one config is invalid")
	}
	if !strings.Contains(err.Error(), "1 config(s) failed") {
		t.Errorf("expected batch failure message, got: %v", err)
	}
}

func TestRunValidateSkipUnknownTypes(t *testing.T) {
	dir := t.TempDir()
	unknownTypeConfig := `
modules:
  - name: custom-thing
    type: custom.unknown.type
    config: {}
`
	path := writeTestConfig(t, dir, "custom.yaml", unknownTypeConfig)
	// Should fail without the flag
	err := runValidate([]string{"--allow-no-entry-points", path})
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
	// Should pass with the flag
	if err := runValidate([]string{"--skip-unknown-types", "--allow-no-entry-points", path}); err != nil {
		t.Fatalf("expected pass with --skip-unknown-types, got: %v", err)
	}
}

func TestRunValidateSnakeCaseConfig(t *testing.T) {
	dir := t.TempDir()
	// "content_type" is the snake_case form of the known camelCase field "contentType"
	snakeCaseConfig := `
modules:
  - name: handler
    type: http.handler
    config:
      content_type: "application/json"
triggers:
  http:
    port: 8080
`
	path := writeTestConfig(t, dir, "snake.yaml", snakeCaseConfig)
	// validateFile returns the detailed error; runValidate returns a summary
	err := validateFile(path, false, false, false, false)
	if err == nil {
		t.Fatal("expected error for snake_case config field")
	}
	if !strings.Contains(err.Error(), "content_type") {
		t.Errorf("expected error to mention snake_case field 'content_type', got: %v", err)
	}
	if !strings.Contains(err.Error(), "contentType") {
		t.Errorf("expected error to suggest camelCase 'contentType', got: %v", err)
	}
}

func TestRunPluginMissingSubcommand(t *testing.T) {
	err := runPlugin([]string{})
	if err == nil {
		t.Fatal("expected error when no plugin subcommand given")
	}
}

func TestRunPluginInitMissingName(t *testing.T) {
	err := runPluginInit([]string{"-author", "test"})
	if err == nil {
		t.Fatal("expected error when no plugin name given")
	}
}

func TestRunPluginInitMissingAuthor(t *testing.T) {
	err := runPluginInit([]string{"my-plugin"})
	if err == nil {
		t.Fatal("expected error when no author given")
	}
}

func TestRunPluginInit(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "test-plugin")
	err := runPluginInit([]string{
		"-author", "tester",
		"-description", "A test plugin",
		"-output", outDir,
		"test-plugin",
	})
	if err != nil {
		t.Fatalf("plugin init failed: %v", err)
	}

	// Check manifest was created
	if _, err := os.Stat(filepath.Join(outDir, "plugin.json")); os.IsNotExist(err) {
		t.Error("expected plugin.json to be created")
	}
	// Check source file was created
	if _, err := os.Stat(filepath.Join(outDir, "test-plugin.go")); os.IsNotExist(err) {
		t.Error("expected test-plugin.go to be created")
	}
}

func TestRunPluginDocs(t *testing.T) {
	// First scaffold a plugin
	dir := t.TempDir()
	outDir := filepath.Join(dir, "doc-plugin")
	err := runPluginInit([]string{
		"-author", "tester",
		"-description", "A doc test plugin",
		"-output", outDir,
		"doc-plugin",
	})
	if err != nil {
		t.Fatalf("plugin init failed: %v", err)
	}

	// Then generate docs
	if err := runPluginDocs([]string{outDir}); err != nil {
		t.Fatalf("plugin docs failed: %v", err)
	}
}

func TestRunPluginDocsMissingDir(t *testing.T) {
	err := runPluginDocs([]string{})
	if err == nil {
		t.Fatal("expected error when no directory given")
	}
}

func TestRunValidatePluginDir(t *testing.T) {
	// Create a fake external plugin directory with a plugin.json declaring a custom module type.
	pluginsDir := t.TempDir()
	pluginSubdir := filepath.Join(pluginsDir, "my-ext-plugin")
	if err := os.MkdirAll(pluginSubdir, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"moduleTypes": ["custom.ext.validate.testonly"]}`
	if err := os.WriteFile(filepath.Join(pluginSubdir, "plugin.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	// Config using the external plugin module type
	dir := t.TempDir()
	configContent := `
modules:
  - name: ext-mod
    type: custom.ext.validate.testonly
`
	path := writeTestConfig(t, dir, "workflow.yaml", configContent)

	// Without --plugin-dir: should fail (unknown type)
	if err := runValidate([]string{"--allow-no-entry-points", path}); err == nil {
		t.Fatal("expected error for unknown external module type without --plugin-dir")
	}

	// With --plugin-dir: should pass
	if err := runValidate([]string{"--plugin-dir", pluginsDir, "--allow-no-entry-points", path}); err != nil {
		t.Errorf("expected valid config with --plugin-dir, got: %v", err)
	}
	t.Cleanup(func() {
		schema.UnregisterModuleType("custom.ext.validate.testonly")
	})
}

func TestRunValidatePluginDirCapabilities(t *testing.T) {
	// Create a fake external plugin directory with a plugin.json using the
	// v0.3.0+ nested "capabilities" object format (as used by registry manifests and older installers).
	pluginsDir := t.TempDir()
	pluginSubdir := filepath.Join(pluginsDir, "my-ext-plugin-caps")
	if err := os.MkdirAll(pluginSubdir, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"my-ext-plugin-caps","version":"1.0.0","type":"external","capabilities":{"configProvider":false,"moduleTypes":["custom.caps.validate.testonly"],"stepTypes":["step.caps_validate_testonly"],"triggerTypes":[]}}`
	if err := os.WriteFile(filepath.Join(pluginSubdir, "plugin.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	// Config using the module type declared in capabilities
	dir := t.TempDir()
	configContent := "modules:\n  - name: caps-mod\n    type: custom.caps.validate.testonly\n"
	path := writeTestConfig(t, dir, "workflow.yaml", configContent)

	// Without --plugin-dir: should fail (unknown type)
	if err := runValidate([]string{"--allow-no-entry-points", path}); err == nil {
		t.Fatal("expected error for unknown external module type without --plugin-dir")
	}

	// With --plugin-dir: should pass (types from capabilities object are recognized)
	if err := runValidate([]string{"--plugin-dir", pluginsDir, "--allow-no-entry-points", path}); err != nil {
		t.Errorf("expected valid config with --plugin-dir (capabilities format), got: %v", err)
	}
	t.Cleanup(func() {
		schema.UnregisterModuleType("custom.caps.validate.testonly")
		schema.UnregisterModuleType("step.caps_validate_testonly")
	})
}

func TestRunValidatePluginDirLoadsStepSchemas(t *testing.T) {
	pluginsDir := t.TempDir()
	pluginSubdir := filepath.Join(pluginsDir, "my-ext-plugin-step-schema")
	if err := os.MkdirAll(pluginSubdir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
		"name": "my-ext-plugin-step-schema",
		"version": "1.0.0",
		"moduleTypes": ["custom.step_schema_validate_testonly"],
		"stepSchemas": [
			{
				"type": "step.schema_validate_testonly",
				"description": "test-only plugin step schema",
				"configFields": [
					{"key": "target", "type": "string", "required": true}
				]
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(pluginSubdir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := schema.GetStepSchemaRegistry()
	if reg.Get("step.schema_validate_testonly") != nil {
		t.Fatal("step.schema_validate_testonly should not exist before loading")
	}

	dir := t.TempDir()
	path := writeTestConfig(t, dir, "workflow.yaml", `
modules:
  - name: ext-mod
    type: custom.step_schema_validate_testonly
`)
	if err := runValidate([]string{"--plugin-dir", pluginsDir, "--allow-no-entry-points", path}); err != nil {
		t.Fatalf("expected valid config with --plugin-dir, got: %v", err)
	}
	if got := reg.Get("step.schema_validate_testonly"); got == nil {
		t.Fatal("expected runValidate --plugin-dir to load plugin step schema")
	}
	t.Cleanup(func() {
		schema.UnregisterModuleType("custom.step_schema_validate_testonly")
		reg.Unregister("step.schema_validate_testonly")
	})
}

func TestRunValidatePluginDirUsesStepSchemasForPipelineRefs(t *testing.T) {
	pluginsDir := t.TempDir()
	pluginSubdir := filepath.Join(pluginsDir, "my-ext-plugin-output-schema")
	if err := os.MkdirAll(pluginSubdir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
		"name": "my-ext-plugin-output-schema",
		"version": "1.0.0",
		"stepTypes": ["step.output_schema_validate_testonly"],
		"stepSchemas": [
			{
				"type": "step.output_schema_validate_testonly",
				"description": "test-only plugin step output schema",
				"outputs": [
					{"key": "known_output", "type": "string"}
				]
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(pluginSubdir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := schema.GetStepSchemaRegistry()
	t.Cleanup(func() {
		schema.UnregisterModuleType("step.output_schema_validate_testonly")
		reg.Unregister("step.output_schema_validate_testonly")
	})

	dir := t.TempDir()
	path := writeTestConfig(t, dir, "workflow.yaml", `
modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
pipelines:
  test:
    steps:
      - name: plugin-step
        type: step.output_schema_validate_testonly
      - name: consume
        type: step.set
        config:
          values:
            result: '{{ step "plugin-step" "missing_output" }}'
`)

	err := runValidate([]string{"--plugin-dir", pluginsDir, "--allow-no-entry-points", path})
	if err == nil {
		t.Fatal("expected strict validation to reject plugin step output field not declared by plugin schema")
	}
	if !strings.Contains(err.Error(), "missing_output") {
		t.Fatalf("expected error to mention missing plugin output field, got: %v", err)
	}
}
