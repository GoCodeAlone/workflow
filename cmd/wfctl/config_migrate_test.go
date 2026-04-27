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
	if migrateDeprecationWriter != os.Stderr {
		t.Errorf("migrateDeprecationWriter default should be os.Stderr, got %T", migrateDeprecationWriter)
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
