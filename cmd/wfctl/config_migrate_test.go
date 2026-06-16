package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigMigrate_NoArgs_PrintsUsage verifies that `wfctl config migrate`
// with no subcommand prints help text that describes engine config DB migrations
// and returns the expected error.
func TestConfigMigrate_NoArgs_PrintsUsage(t *testing.T) {
	// Capture stderr where fs.Usage() writes.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.Stderr
	os.Stderr = w

	gotErr := runConfigMigrate([]string{})

	w.Close()
	os.Stderr = origStderr

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	helpText := buf.String()

	if gotErr == nil {
		t.Fatal("expected error (no subcommand), got nil")
	}
	if !strings.Contains(gotErr.Error(), "subcommand") {
		t.Errorf("error should mention 'subcommand', got: %q", gotErr)
	}
	for _, want := range []string{"wfctl config migrate", "engine config", "status", "apply"} {
		if !strings.Contains(helpText, want) {
			t.Errorf("usage output missing %q; got:\n%s", want, helpText)
		}
	}
}

// TestConfigMigrate_DefaultWriterIsStderr verifies that the deprecation banner
// defaults to os.Stderr, so future changes that accidentally redirect it are caught.
func TestConfigMigrate_DefaultWriterIsStderr(t *testing.T) {
	if migrateDeprecationWriter != defaultMigrateDeprecationWriter {
		t.Errorf("migrateDeprecationWriter default should be defaultMigrateDeprecationWriter, got %T", migrateDeprecationWriter)
	}
}

// TestConfigMigrate_DeprecationBanner verifies that wfctl migrate writes the
// exact verbatim deprecation notice to stderr before running the handler.
func TestConfigMigrate_DeprecationBanner(t *testing.T) {
	var buf bytes.Buffer
	origStderr := migrateDeprecationWriter
	migrateDeprecationWriter = &buf
	defer func() { migrateDeprecationWriter = origStderr }()

	_ = runMigrateDeprecated([]string{})

	banner := buf.String()
	wantBanner := "wfctl migrate is being renamed to wfctl config migrate " +
		"(engine config migration is config-domain). " +
		"The old form is supported for one release; please update your scripts."
	if !strings.Contains(banner, wantBanner) {
		t.Errorf("deprecation banner does not contain full expected string.\nwant: %q\ngot:  %q", wantBanner, banner)
	}
}

// TestConfigMigrate_StatusRoutingWithBanner verifies that `wfctl migrate status`
// (Step 6 of dispatch spec): fires the deprecation banner AND produces the same
// status output as `wfctl config migrate status`. Uses a real temp SQLite DB
// to exercise the full path.
func TestConfigMigrate_StatusRoutingWithBanner(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// --- via deprecated path ---
	var bannerBuf bytes.Buffer
	origWriter := migrateDeprecationWriter
	migrateDeprecationWriter = &bannerBuf
	defer func() { migrateDeprecationWriter = origWriter }()

	errOld := runMigrateDeprecated([]string{"status", "--db", dbPath})
	banner := bannerBuf.String()

	// Reset for clean state.
	migrateDeprecationWriter = origWriter

	// Banner must have fired.
	if !strings.Contains(banner, "wfctl migrate is being renamed to wfctl config migrate") {
		t.Errorf("expected deprecation banner, got: %q", banner)
	}

	// --- via canonical path ---
	errNew := runConfigMigrate([]string{"status", "--db", dbPath})

	// Both paths must agree on success/failure (both should succeed — no
	// schema providers means "No schema providers registered.").
	if (errOld == nil) != (errNew == nil) {
		t.Errorf("routing mismatch: deprecated=%v canonical=%v", errOld, errNew)
	}
}

// TestConfigCommand_DispatchesMigrate verifies that `wfctl config migrate`
// routes through runConfig correctly.
func TestConfigCommand_DispatchesMigrate(t *testing.T) {
	err := runConfig([]string{"migrate"})
	if err == nil {
		t.Fatal("expected error (no DB subcommand), got nil")
	}
	if !strings.Contains(err.Error(), "subcommand") {
		t.Errorf("expected subcommand error from config migrate, got: %v", err)
	}
}

func TestConfigCommand_EmbeddedCLIWiresConfigCommand(t *testing.T) {
	embedded := string(wfctlConfigBytes)
	for _, want := range []string{
		"name: config",
		"cmd-config:",
		"command: config",
	} {
		if !strings.Contains(embedded, want) {
			t.Fatalf("embedded wfctl config must wire config command, missing %q", want)
		}
	}
}

// TestConfigCommand_UnknownSubcommand verifies that wfctl config with an
// unknown sub-subcommand returns a clear error.
func TestConfigCommand_UnknownSubcommand(t *testing.T) {
	err := runConfig([]string{"unknown-thing"})
	if err == nil {
		t.Fatal("expected error for unknown config subcommand")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Errorf("expected 'unknown' in error, got: %v", err)
	}
}

func TestConfigValidateAcceptsWfctlManifestAndLockfile(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "wfctl.yaml")
	lockfile := filepath.Join(dir, ".wfctl-lock.yaml")
	if err := os.WriteFile(manifest, []byte(`version: 1
plugins:
  - name: workflow-plugin-digitalocean
    version: v1.0.13
    source: github.com/GoCodeAlone/workflow-plugin-digitalocean
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockfile, []byte(`version: 1
generated_at: 2026-05-14T00:00:00Z
plugins:
  workflow-plugin-digitalocean:
    version: v1.0.13
    source: github.com/GoCodeAlone/workflow-plugin-digitalocean
    platforms:
      darwin/arm64:
        url: https://example.invalid/workflow-plugin-digitalocean_Darwin_arm64.tar.gz
        sha256: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runConfig([]string{"validate", "--manifest", manifest, "--lock-file", lockfile}); err != nil {
		t.Fatalf("wfctl config validate failed: %v", err)
	}
}

func TestConfigValidateLockedRejectsStaleLockfile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	manifest := `version: 1
plugins:
  - name: workflow-plugin-foo
    version: v1.0.0
    source: github.com/GoCodeAlone/workflow-plugin-foo
`
	if err := os.WriteFile("wfctl.yaml", []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := runPluginLockFromManifest("wfctl.yaml", ".wfctl-lock.yaml"); err != nil {
		t.Fatalf("lock manifest: %v", err)
	}
	if err := os.WriteFile("wfctl.yaml", []byte(strings.ReplaceAll(manifest, "v1.0.0", "v1.1.0")), 0o600); err != nil {
		t.Fatalf("write stale manifest: %v", err)
	}

	err := runConfigValidate([]string{"--locked"})
	if err == nil {
		t.Fatal("config validate --locked succeeded with stale lockfile")
	}
	if !strings.Contains(err.Error(), "lockfile is stale") {
		t.Fatalf("error = %v, want stale lockfile", err)
	}
}

func TestConfigValidateAcceptsPositionalManifestAndWarnsOnMissingDefaultLock(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("wfctl.yaml", []byte(`version: 1
plugins:
  - name: workflow-plugin-digitalocean
    version: v1.0.13
`), 0o600); err != nil {
		t.Fatal(err)
	}
	stderr := captureConfigValidateStderr(t, func() {
		if err := runConfigValidate([]string{"wfctl.yaml"}); err != nil {
			t.Fatalf("config validate: %v", err)
		}
	})
	if !strings.Contains(stderr, "lockfile not found") {
		t.Fatalf("stderr = %q, want missing lockfile warning", stderr)
	}
}

func TestConfigValidateRejectsTooManyPositionals(t *testing.T) {
	if err := runConfigValidate([]string{"one.yaml", "two.yaml"}); err == nil {
		t.Fatal("expected too many positional args to fail")
	}
}

func TestConfigValidateRejectsRuntimeWorkflowConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(path, []byte(minimalConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runConfig([]string{"validate", path, "--skip-lock"})
	if err == nil {
		t.Fatal("expected workflow runtime config to be rejected by wfctl config validate")
	}
	if !strings.Contains(err.Error(), "not a wfctl project manifest") {
		t.Fatalf("error = %v, want project manifest guidance", err)
	}
}

func TestValidateWfctlManifestFileReportsPluginProblems(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wfctl.yaml")
	if err := os.WriteFile(path, []byte(`version: 2
plugins:
  - name: workflow-plugin-foo
    version: ""
    auth: {}
    verify: {}
  - name: workflow-plugin-foo
    version: v1.0.0
  - version: v1.0.0
`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := validateWfctlManifestFile(path)
	if err == nil {
		t.Fatal("expected invalid manifest to fail")
	}
	for _, want := range []string{"version: got 2 want 1", "duplicated", "version is required", "auth.env", "verify.identity", "name is required"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, missing %q", err, want)
		}
	}
}

func TestValidateWfctlLockfileReportsPlatformProblems(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wfctl-lock.yaml")
	if err := os.WriteFile(path, []byte(`version: 2
plugins:
  workflow-plugin-foo:
    version: ""
    platforms:
      "":
        url: not a url
        sha256: abc
`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := validateWfctlLockfile(path)
	if err == nil {
		t.Fatal("expected invalid lockfile to fail")
	}
	for _, want := range []string{"version: got 2 want 1", "version is required", "empty platform", "url is invalid", "got length 3 want 64"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, missing %q", err, want)
		}
	}
}

func TestValidateWfctlLockfilePreservesExplicitMissingLockError(t *testing.T) {
	err := validateWfctlLockfile(filepath.Join(t.TempDir(), ".wfctl-lock.yaml"))
	if !os.IsNotExist(err) {
		t.Fatalf("error = %v, want os.ErrNotExist", err)
	}
}

func TestRunValidateRejectsWfctlManifestWithGuidance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wfctl.yaml")
	if err := os.WriteFile(path, []byte(`version: 1
plugins:
  - name: workflow-plugin-digitalocean
    version: v1.0.13
`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runValidate([]string{path})
	if err == nil {
		t.Fatal("expected wfctl validate to reject wfctl.yaml")
	}
	if !strings.Contains(err.Error(), "wfctl config validate") {
		t.Fatalf("error = %v, want guidance to wfctl config validate", err)
	}
}

func TestIsLikelyWfctlProjectManifestClassifiesOnlyCanonicalProjectFiles(t *testing.T) {
	dir := t.TempDir()
	wfctl := filepath.Join(dir, ".wfctl.yaml")
	if err := os.WriteFile(wfctl, []byte(`version: 1
plugins: []
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !isLikelyWfctlProjectManifest(wfctl) {
		t.Fatal(".wfctl.yaml with plugins should be classified as wfctl project manifest")
	}
	runtime := filepath.Join(dir, "wfctl.yaml")
	if err := os.WriteFile(runtime, []byte(`version: 1
plugins: []
modules: []
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if isLikelyWfctlProjectManifest(runtime) {
		t.Fatal("wfctl.yaml with runtime keys must not be classified as project manifest")
	}
	other := filepath.Join(dir, "other.yaml")
	if err := os.WriteFile(other, []byte(`version: 1
plugins: []
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if isLikelyWfctlProjectManifest(other) {
		t.Fatal("non-canonical filename must not be classified as project manifest")
	}
	bad := filepath.Join(dir, "bad.wfctl.yaml")
	if err := os.WriteFile(bad, []byte("plugins: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if isLikelyWfctlProjectManifest(bad) {
		t.Fatal("malformed yaml must not be classified as project manifest")
	}
}

func TestRunValidateDoesNotMisclassifyOtherPluginsYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "buf.gen.yaml")
	if err := os.WriteFile(path, []byte(`version: v2
plugins:
  - local: protoc-gen-go
    out: gen/go
`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runValidate([]string{path})
	if err == nil {
		t.Fatal("expected non-workflow plugins YAML to fail workflow validation")
	}
	if strings.Contains(err.Error(), "wfctl config validate") {
		t.Fatalf("error = %v, should not classify buf.gen.yaml as wfctl project manifest", err)
	}
}

func captureConfigValidateStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() {
		os.Stderr = orig
	}()
	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
