package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── stdout/stderr capture helpers ────────────────────────────────────────────

// captureStdout redirects os.Stdout to a pipe for the duration of fn, drains
// the pipe in a goroutine to prevent deadlocks when fn writes more than the
// OS pipe buffer (~64 KB), and returns the captured output alongside any error
// fn returned. os.Stdout is restored via t.Cleanup.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("captureStdout: create pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() { defer r.Close(); io.Copy(&buf, r); close(done) }()

	fnErr := fn()

	w.Close()
	os.Stdout = orig // restore immediately so later writes in the test are not captured
	<-done
	return buf.String(), fnErr
}

// captureStderr is the stderr equivalent of captureStdout.
func captureStderr(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("captureStderr: create pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() { defer r.Close(); io.Copy(&buf, r); close(done) }()

	fnErr := fn()

	w.Close()
	os.Stderr = orig // restore immediately so later writes in the test are not captured
	<-done
	return buf.String(), fnErr
}

// ── infraEnvVarName ──────────────────────────────────────────────────────────

func TestInfraEnvVarName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"bmw-staging-db", "BMW_STAGING_DB"},
		{"uri", "URI"},
		{"host.name", "HOST_NAME"},
		{"MY_MODULE", "MY_MODULE"},
		// Characters beyond hyphen/dot must also be sanitised.
		{"module/v2:resource", "MODULE_V2_RESOURCE"},
		{"a\\b", "A_B"},
		{"a:b:c", "A_B_C"},
		// Leading digit: POSIX env var names must start with letter or underscore.
		{"1st-module", "_1ST_MODULE"},
		{"42", "_42"},
	}
	for _, tc := range cases {
		got := infraEnvVarName(tc.in)
		if got != tc.want {
			t.Errorf("infraEnvVarName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ── shellQuote ───────────────────────────────────────────────────────────────

func TestShellQuote(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"simple", "'simple'"},
		{"with space", "'with space'"},
		{"has'quote", `'has'\''quote'`},
		{"dollar $VAR", "'dollar $VAR'"},
		{42, "'42'"},
		{true, "'true'"},
		// Non-scalar: map serialised as compact JSON then single-quoted.
		{map[string]any{"k": "v"}, `'{"k":"v"}'`},
	}
	for _, tc := range cases {
		got := shellQuote(tc.in)
		if got != tc.want {
			t.Errorf("shellQuote(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ── infraOutputsEnv ──────────────────────────────────────────────────────────

func TestInfraOutputsEnv(t *testing.T) {
	entries := []infraOutputEntry{
		{module: "bmw-staging-db", outputs: map[string]any{"host": "db.example.com", "port": 5432}},
		{module: "bmw-app", outputs: map[string]any{"url": "https://app.example.com"}},
	}

	out, err := captureStdout(t, func() error { return infraOutputsEnv(entries) })
	if err != nil {
		t.Fatalf("infraOutputsEnv: %v", err)
	}

	wantLines := []string{
		"BMW_STAGING_DB_HOST='db.example.com'",
		"BMW_STAGING_DB_PORT='5432'",
		"BMW_APP_URL='https://app.example.com'",
	}
	for _, line := range wantLines {
		if !strings.Contains(out, line) {
			t.Errorf("env output missing %q\ngot:\n%s", line, out)
		}
	}
}

// ── infraOutputsJSON ─────────────────────────────────────────────────────────

func TestInfraOutputsJSON(t *testing.T) {
	entries := []infraOutputEntry{
		{module: "mydb", outputs: map[string]any{"host": "db.example.com", "port": 5432}},
	}

	out, err := captureStdout(t, func() error { return infraOutputsJSON(entries) })
	if err != nil {
		t.Fatalf("infraOutputsJSON: %v", err)
	}

	var result map[string]map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("parse JSON output: %v\noutput was:\n%s", err, out)
	}
	if _, ok := result["mydb"]; !ok {
		t.Error("JSON output missing 'mydb' key")
	}
	if result["mydb"]["host"] != "db.example.com" {
		t.Errorf("JSON host = %v, want db.example.com", result["mydb"]["host"])
	}
}

// ── infraOutputsYAML ─────────────────────────────────────────────────────────

func TestInfraOutputsYAML(t *testing.T) {
	entries := []infraOutputEntry{
		{module: "mydb", outputs: map[string]any{"host": "db.example.com"}},
	}

	out, err := captureStdout(t, func() error { return infraOutputsYAML(entries) })
	if err != nil {
		t.Fatalf("infraOutputsYAML: %v", err)
	}

	if !strings.Contains(out, "mydb:") {
		t.Errorf("YAML output missing 'mydb:'\ngot:\n%s", out)
	}
	if !strings.Contains(out, "host: db.example.com") {
		t.Errorf("YAML output missing 'host: db.example.com'\ngot:\n%s", out)
	}
}

// ── runInfraOutputs integration ──────────────────────────────────────────────

func TestRunInfraOutputs_EmptyState(t *testing.T) {
	dir := t.TempDir()
	cfgFile := writeInfraOutputsTestConfig(t, dir)

	// "No outputs found" message goes to stderr.
	errOut, err := captureStderr(t, func() error {
		return runInfraOutputs([]string{"--config", cfgFile})
	})
	if err != nil {
		t.Fatalf("runInfraOutputs: %v", err)
	}
	if !strings.Contains(errOut, "No outputs") {
		t.Errorf("expected 'No outputs' on stderr, got: %s", errOut)
	}
}

func TestRunInfraOutputs_WithState_YAML(t *testing.T) {
	dir := t.TempDir()
	writeInfraOutputsStateFile(t, dir, "mydb", map[string]any{"host": "localhost", "port": 5432})
	cfgFile := writeInfraOutputsTestConfig(t, dir)

	out, err := captureStdout(t, func() error {
		return runInfraOutputs([]string{"--config", cfgFile, "--format", "yaml"})
	})
	if err != nil {
		t.Fatalf("runInfraOutputs yaml: %v", err)
	}

	if !strings.Contains(out, "mydb:") {
		t.Errorf("YAML missing 'mydb:', got:\n%s", out)
	}
	if !strings.Contains(out, "host: localhost") {
		t.Errorf("YAML missing 'host: localhost', got:\n%s", out)
	}
}

func TestRunInfraOutputs_WithState_JSON(t *testing.T) {
	dir := t.TempDir()
	writeInfraOutputsStateFile(t, dir, "mydb", map[string]any{"uri": "postgresql://localhost/db"})
	cfgFile := writeInfraOutputsTestConfig(t, dir)

	out, err := captureStdout(t, func() error {
		return runInfraOutputs([]string{"--config", cfgFile, "--format", "json"})
	})
	if err != nil {
		t.Fatalf("runInfraOutputs json: %v", err)
	}

	var result map[string]map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("parse JSON: %v\noutput:\n%s", err, out)
	}
	if result["mydb"]["uri"] != "postgresql://localhost/db" {
		t.Errorf("uri = %v, want postgresql://localhost/db", result["mydb"]["uri"])
	}
}

func TestRunInfraOutputs_ModuleFilter(t *testing.T) {
	dir := t.TempDir()
	writeInfraOutputsStateFile(t, dir, "db-a", map[string]any{"host": "a.example.com"})
	writeInfraOutputsStateFile(t, dir, "db-b", map[string]any{"host": "b.example.com"})
	cfgFile := writeInfraOutputsTestConfig(t, dir)

	out, err := captureStdout(t, func() error {
		return runInfraOutputs([]string{"--config", cfgFile, "--module", "db-a", "--format", "yaml"})
	})
	if err != nil {
		t.Fatalf("runInfraOutputs --module: %v", err)
	}

	if !strings.Contains(out, "db-a:") {
		t.Errorf("expected db-a in output, got:\n%s", out)
	}
	if strings.Contains(out, "db-b:") {
		t.Errorf("db-b should be filtered out, got:\n%s", out)
	}
}

func TestRunInfraOutputs_UnknownFormat(t *testing.T) {
	dir := t.TempDir()
	writeInfraOutputsStateFile(t, dir, "mydb", map[string]any{"host": "localhost"})
	cfgFile := writeInfraOutputsTestConfig(t, dir)

	err := runInfraOutputs([]string{"--config", cfgFile, "--format", "table"})
	if err == nil {
		t.Fatal("expected error for unknown format, got nil")
	}
	if !strings.Contains(err.Error(), "unknown format") {
		t.Errorf("error = %v, want 'unknown format'", err)
	}
}

func TestRunInfraOutputs_SkipsEmptyOutputs(t *testing.T) {
	dir := t.TempDir()
	writeInfraOutputsStateFileRaw(t, dir, "no-outputs", nil)
	writeInfraOutputsStateFile(t, dir, "has-outputs", map[string]any{"key": "val"})
	cfgFile := writeInfraOutputsTestConfig(t, dir)

	out, err := captureStdout(t, func() error {
		return runInfraOutputs([]string{"--config", cfgFile, "--format", "yaml"})
	})
	if err != nil {
		t.Fatalf("runInfraOutputs: %v", err)
	}

	if strings.Contains(out, "no-outputs") {
		t.Errorf("resource with empty outputs should be skipped, got:\n%s", out)
	}
	if !strings.Contains(out, "has-outputs") {
		t.Errorf("resource with outputs should appear, got:\n%s", out)
	}
}

// TestRunInfraOutputs_EnvModuleFilter verifies that --module accepts the base
// config name and correctly matches the env-resolved name in state. When
// --env staging is used, the module "bmw-database" is stored in state as
// "bmw-staging-db" (per-env name override). The filter must resolve the base
// name before comparing to state records.
func TestRunInfraOutputs_EnvModuleFilter(t *testing.T) {
	dir := t.TempDir()
	// State holds the resolved name "bmw-staging-db".
	writeInfraOutputsStateFile(t, dir, "bmw-staging-db", map[string]any{"uri": "postgresql://staging/db"})
	writeInfraOutputsStateFile(t, dir, "other-module", map[string]any{"key": "val"})

	// Config has "bmw-database" as the base name; per-env staging it resolves to "bmw-staging-db".
	cfgFile := writeInfraOutputsEnvTestConfig(t, dir)

	out, err := captureStdout(t, func() error {
		return runInfraOutputs([]string{
			"--config", cfgFile,
			"--env", "staging",
			"--module", "bmw-database", // base config name
			"--format", "yaml",
		})
	})
	if err != nil {
		t.Fatalf("runInfraOutputs --env --module: %v", err)
	}

	if !strings.Contains(out, "bmw-staging-db:") {
		t.Errorf("expected 'bmw-staging-db:' in output (env-resolved name), got:\n%s", out)
	}
	if strings.Contains(out, "other-module") {
		t.Errorf("other-module should be filtered out, got:\n%s", out)
	}
}

// ── test helpers ─────────────────────────────────────────────────────────────

// writeInfraOutputsTestConfig writes a minimal infra.yaml (no env overrides)
// that points the filesystem state store at dir, then returns the file path.
func writeInfraOutputsTestConfig(t *testing.T, stateDir string) string {
	t.Helper()
	content := `modules:
  - name: iac-state
    type: iac.state
    config:
      backend: filesystem
      directory: ` + stateDir + "\n"
	f, err := os.CreateTemp(t.TempDir(), "infra-*.yaml")
	if err != nil {
		t.Fatalf("create config: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write config: %v", err)
	}
	f.Close()
	return f.Name()
}

// writeInfraOutputsEnvTestConfig writes a config that has a "bmw-database"
// module whose per-env staging name resolves to "bmw-staging-db", used by
// TestRunInfraOutputs_EnvModuleFilter.
func writeInfraOutputsEnvTestConfig(t *testing.T, stateDir string) string {
	t.Helper()
	content := `modules:
  - name: iac-state
    type: iac.state
    config:
      backend: filesystem
      directory: ` + stateDir + `
  - name: bmw-database
    type: infra.database
    config:
      engine: pg
    environments:
      staging:
        config:
          name: bmw-staging-db
` + "\n"
	f, err := os.CreateTemp(t.TempDir(), "infra-env-*.yaml")
	if err != nil {
		t.Fatalf("create env config: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write env config: %v", err)
	}
	f.Close()
	return f.Name()
}

// writeInfraOutputsStateFile writes a JSON state record to stateDir.
func writeInfraOutputsStateFile(t *testing.T, stateDir, name string, outputs map[string]any) {
	t.Helper()
	writeInfraOutputsStateFileRaw(t, stateDir, name, outputs)
}

func writeInfraOutputsStateFileRaw(t *testing.T, stateDir, name string, outputs map[string]any) {
	t.Helper()
	record := iacStateRecord{
		ResourceID:   name,
		ResourceType: "infra.database",
		Provider:     "digitalocean",
		Status:       "active",
		Outputs:      outputs,
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatalf("marshal state record: %v", err)
	}
	path := filepath.Join(stateDir, name+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write state file: %v", err)
	}
}
