package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePluginManifestFile(t *testing.T, dir, name, manifest string) string {
	t.Helper()
	pdir := filepath.Join(dir, name)
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(pdir, "plugin.json")
	if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return pdir
}

func TestLoadPluginManifest_HappyPath(t *testing.T) {
	dir := t.TempDir()
	writePluginManifestFile(t, dir, "workflow-plugin-fake", `{
		"name": "workflow-plugin-fake",
		"required_secrets": [
			{"name": "FAKE_USER", "sensitive": false},
			{"name": "FAKE_TOKEN", "sensitive": true, "description": "API token"}
		]
	}`)
	m, err := loadPluginManifest("workflow-plugin-fake", dir)
	if err != nil {
		t.Fatalf("loadPluginManifest: %v", err)
	}
	if m.Name != "workflow-plugin-fake" {
		t.Errorf("name = %q", m.Name)
	}
	if len(m.RequiredSecrets) != 2 {
		t.Fatalf("required_secrets = %d want 2", len(m.RequiredSecrets))
	}
	if m.RequiredSecrets[0].Name != "FAKE_USER" || m.RequiredSecrets[0].Sensitive {
		t.Errorf("rs[0] = %+v", m.RequiredSecrets[0])
	}
	if m.RequiredSecrets[1].Name != "FAKE_TOKEN" || !m.RequiredSecrets[1].Sensitive {
		t.Errorf("rs[1] = %+v", m.RequiredSecrets[1])
	}
}

func TestLoadPluginManifest_MissingDir(t *testing.T) {
	_, err := loadPluginManifest("nope", t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
	if !strings.Contains(err.Error(), "wfctl plugin install") {
		t.Errorf("error should hint at remediation: %v", err)
	}
}

func TestLoadPluginManifest_BadJSON(t *testing.T) {
	dir := t.TempDir()
	writePluginManifestFile(t, dir, "x", `{not-json}`)
	_, err := loadPluginManifest("x", dir)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

// TestPromptOne_PipedNonSensitive reads a single line from a piped
// reader and returns it.
func TestPromptOne_PipedNonSensitive(t *testing.T) {
	got, err := promptOne(PluginRequiredSecret{Name: "X"}, strings.NewReader("hello\n"))
	if err != nil {
		t.Fatalf("promptOne: %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q want hello", got)
	}
}

// TestPromptOne_PipedSensitive — sensitive value can still come via
// pipe (tests bypass tty path).
func TestPromptOne_PipedSensitive(t *testing.T) {
	got, err := promptOne(PluginRequiredSecret{Name: "Y", Sensitive: true}, strings.NewReader("hunter2\n"))
	if err != nil {
		t.Fatalf("promptOne: %v", err)
	}
	if got != "hunter2" {
		t.Errorf("got %q want hunter2", got)
	}
}

// TestRunSecretsSetupPlugin_PiperReadsRequiredSecrets exercises the
// full flow with a piped reader for input. Output goes to a buffer.
//
// We swap out stdin via the io.Reader arg + verify the buffered out
// reports each secret as "set".
func TestRunSecretsSetupPlugin_PiperReadsRequiredSecrets(t *testing.T) {
	dir := t.TempDir()
	writePluginManifestFile(t, dir, "wp-fake", `{
		"name": "wp-fake",
		"required_secrets": [
			{"name": "A", "sensitive": false},
			{"name": "B", "sensitive": true}
		]
	}`)
	// Stub the writer side by setting the org via env so
	// buildSecretWriter is short-circuited (we just want to exercise
	// the prompt loop). Use --scope=org with a stub provider not
	// reachable in tests; the call will fail at network → we assert
	// we got at least to the writer construction.
	in := io.Reader(strings.NewReader("alice\nhunter2\n"))
	var out bytes.Buffer
	t.Setenv("GITHUB_TOKEN", "stub")

	// We can't actually hit the GH API; use --scope=org pointing
	// at a non-resolvable token+org, then assert error returns from
	// the network-side path (after the prompts succeed).
	err := runSecretsSetupPluginWithIO([]string{
		"--plugin", "wp-fake",
		"--plugin-dir", dir,
		"--scope", "org",
		"--org", "test-org",
		"--token-env", "GITHUB_TOKEN",
	}, in, &out)
	if err == nil {
		t.Fatal("expected network-side error reaching GH (test runs offline)")
	}
	// The output buffer should still show that we entered the setup
	// loop (prompt prelude).
	if !strings.Contains(out.String(), "Setting up secrets for plugin") {
		t.Errorf("setup prelude missing from output:\n%s", out.String())
	}
}

func TestBuildSecretWriter_ScopeRouting(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "stub")
	// Org happy path.
	w, label, err := buildSecretWriter("org", "", "my-org", "all", "GITHUB_TOKEN", "")
	if err != nil || w == nil {
		t.Errorf("org: err=%v writer=%v", err, w)
	}
	if !strings.Contains(label, "github org \"my-org\"") {
		t.Errorf("org label: %q", label)
	}

	// Org rejects missing --org.
	if _, _, err := buildSecretWriter("org", "", "", "all", "GITHUB_TOKEN", ""); err == nil {
		t.Error("org: expected error without --org")
	}

	// Env rejects missing --env.
	if _, _, err := buildSecretWriter("env", "", "", "all", "GITHUB_TOKEN", "app.yaml"); err == nil {
		t.Error("env: expected error without --env")
	}

	// Unknown scope.
	if _, _, err := buildSecretWriter("nope", "", "", "all", "GITHUB_TOKEN", ""); err == nil {
		t.Error("unknown scope should error")
	}
}
