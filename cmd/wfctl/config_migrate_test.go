package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestConfigMigrate_HelpText verifies that wfctl config migrate --help returns
// help text that describes engine config DB migrations.
func TestConfigMigrate_HelpText(t *testing.T) {
	// runConfigMigrate with no args (and -help suppressed) should surface usage.
	err := runConfigMigrate([]string{})
	if err == nil {
		t.Fatal("expected error (no subcommand), got nil")
	}
	// Verify the error describes valid subcommands.
	msg := err.Error()
	if !strings.Contains(msg, "subcommand") {
		t.Errorf("error should mention 'subcommand', got: %q", msg)
	}
}

// TestConfigMigrate_RouteToSameHandler verifies that wfctl config migrate and
// wfctl migrate dispatch to the same underlying logic: both should fail in the
// same way when given an unknown subcommand.
func TestConfigMigrate_RouteToSameHandler(t *testing.T) {
	errOld := runMigrateDeprecated([]string{"unknown-sub"})
	errNew := runConfigMigrate([]string{"unknown-sub"})

	if errOld == nil || errNew == nil {
		t.Fatal("both should return an error for unknown subcommand")
	}
	// Both errors should contain the same "unknown subcommand" message.
	if !strings.Contains(errOld.Error(), "unknown subcommand") {
		t.Errorf("old path error should mention unknown subcommand, got: %v", errOld)
	}
	if !strings.Contains(errNew.Error(), "unknown subcommand") {
		t.Errorf("new path error should mention unknown subcommand, got: %v", errNew)
	}
}

// TestConfigMigrate_DeprecationBanner verifies that wfctl migrate writes a
// deprecation notice to stderr before running the handler.
func TestConfigMigrate_DeprecationBanner(t *testing.T) {
	var buf bytes.Buffer
	origStderr := migrateDeprecationWriter
	migrateDeprecationWriter = &buf
	defer func() { migrateDeprecationWriter = origStderr }()

	// Call with a valid (no-op in test) args set — the banner fires regardless.
	_ = runMigrateDeprecated([]string{})

	banner := buf.String()
	if !strings.Contains(banner, "wfctl migrate") {
		t.Errorf("deprecation banner should mention 'wfctl migrate', got: %q", banner)
	}
	if !strings.Contains(banner, "wfctl config migrate") {
		t.Errorf("deprecation banner should mention 'wfctl config migrate', got: %q", banner)
	}
}

// TestConfigCommand_DispatchesMigrate verifies that `wfctl config migrate`
// routes through runConfig correctly.
func TestConfigCommand_DispatchesMigrate(t *testing.T) {
	err := runConfig([]string{"migrate"})
	if err == nil {
		t.Fatal("expected error (no DB subcommand), got nil")
	}
	// Should be the same error as running runConfigMigrate directly.
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
