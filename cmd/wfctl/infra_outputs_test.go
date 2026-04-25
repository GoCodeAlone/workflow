package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

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

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	if err := infraOutputsEnv(entries); err != nil {
		t.Fatalf("infraOutputsEnv: %v", err)
	}

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	out := buf.String()

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

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	if err := infraOutputsJSON(entries); err != nil {
		t.Fatalf("infraOutputsJSON: %v", err)
	}

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)

	var result map[string]map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON output: %v\noutput was:\n%s", err, buf.String())
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

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	if err := infraOutputsYAML(entries); err != nil {
		t.Fatalf("infraOutputsYAML: %v", err)
	}

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	out := buf.String()

	if !strings.Contains(out, "mydb:") {
		t.Errorf("YAML output missing 'mydb:'\ngot:\n%s", out)
	}
	if !strings.Contains(out, "host: db.example.com") {
		t.Errorf("YAML output missing 'host: db.example.com'\ngot:\n%s", out)
	}
}

// ── runInfraOutputs integration ──────────────────────────────────────────────

func TestRunInfraOutputs_EmptyState(t *testing.T) {
	// Create a temp dir as a filesystem state store with no records.
	dir := t.TempDir()

	cfgFile := writeInfraOutputsTestConfig(t, dir)

	// Capture stderr (the "No outputs found" message goes there).
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	err := runInfraOutputs([]string{"--config", cfgFile})

	w.Close()
	os.Stderr = old

	if err != nil {
		t.Fatalf("runInfraOutputs: %v", err)
	}
	var buf bytes.Buffer
	buf.ReadFrom(r)
	if !strings.Contains(buf.String(), "No outputs") {
		t.Errorf("expected 'No outputs' on stderr, got: %s", buf.String())
	}
}

func TestRunInfraOutputs_WithState_YAML(t *testing.T) {
	dir := t.TempDir()
	writeInfraOutputsStateFile(t, dir, "mydb", map[string]any{"host": "localhost", "port": 5432})

	cfgFile := writeInfraOutputsTestConfig(t, dir)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runInfraOutputs([]string{"--config", cfgFile, "--format", "yaml"})

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("runInfraOutputs yaml: %v", err)
	}
	var buf bytes.Buffer
	buf.ReadFrom(r)
	out := buf.String()

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

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runInfraOutputs([]string{"--config", cfgFile, "--format", "json"})

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("runInfraOutputs json: %v", err)
	}
	var buf bytes.Buffer
	buf.ReadFrom(r)

	var result map[string]map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v\noutput:\n%s", err, buf.String())
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

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runInfraOutputs([]string{"--config", cfgFile, "--module", "db-a", "--format", "yaml"})

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("runInfraOutputs --module: %v", err)
	}
	var buf bytes.Buffer
	buf.ReadFrom(r)
	out := buf.String()

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
	// Write a state file with empty outputs.
	writeInfraOutputsStateFileRaw(t, dir, "no-outputs", nil)
	writeInfraOutputsStateFile(t, dir, "has-outputs", map[string]any{"key": "val"})

	cfgFile := writeInfraOutputsTestConfig(t, dir)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runInfraOutputs([]string{"--config", cfgFile, "--format", "yaml"})

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("runInfraOutputs: %v", err)
	}
	var buf bytes.Buffer
	buf.ReadFrom(r)
	out := buf.String()

	if strings.Contains(out, "no-outputs") {
		t.Errorf("resource with empty outputs should be skipped, got:\n%s", out)
	}
	if !strings.Contains(out, "has-outputs") {
		t.Errorf("resource with outputs should appear, got:\n%s", out)
	}
}

// ── test helpers ─────────────────────────────────────────────────────────────

// writeInfraOutputsTestConfig writes a minimal infra.yaml that points the
// filesystem state store at dir, then returns the file path.
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
	path := stateDir + "/" + name + ".json"
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write state file: %v", err)
	}
}
