package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyCapabilitiesUsage(t *testing.T) {
	err := runPluginVerifyCapabilities([]string{})
	if err == nil {
		t.Fatal("want error for missing args")
	}
	if !strings.Contains(err.Error(), "--binary") {
		t.Errorf("error %q should mention --binary", err.Error())
	}
}

func TestVerifyCapabilitiesRequiresBinary(t *testing.T) {
	err := runPluginVerifyCapabilities([]string{"."})
	if err == nil {
		t.Fatal("want error when --binary missing")
	}
	if !strings.Contains(err.Error(), "--binary") {
		t.Errorf("error %q should mention --binary", err.Error())
	}
}

func TestPreflightBinaryEmpty(t *testing.T) {
	if err := preflightBinary(""); err == nil || !strings.Contains(err.Error(), "binary path") {
		t.Errorf("want empty-path error, got %v", err)
	}
}

func TestPreflightBinaryNull(t *testing.T) {
	if err := preflightBinary("null"); err == nil || !strings.Contains(err.Error(), "binary path") {
		t.Errorf("want null-path error (jq fallback), got %v", err)
	}
}

func TestPreflightBinaryMissing(t *testing.T) {
	if err := preflightBinary("/nonexistent/missing-xyz"); err == nil || !strings.Contains(err.Error(), "stat") {
		t.Errorf("want stat error, got %v", err)
	}
}

func TestPreflightBinaryDirectory(t *testing.T) {
	if err := preflightBinary(t.TempDir()); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Errorf("want directory error, got %v", err)
	}
}

func TestPreflightBinaryNonExecutable(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "p")
	if err := os.WriteFile(f, []byte("not-exec"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := preflightBinary(f); err == nil || !strings.Contains(err.Error(), "executable") {
		t.Errorf("want non-executable error, got %v", err)
	}
}

func TestPreflightBinaryOK(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "p")
	if err := os.WriteFile(f, []byte("#!/bin/sh\necho ok"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := preflightBinary(f); err != nil {
		t.Errorf("want PASS, got %v", err)
	}
}

func TestIsSentinel(t *testing.T) {
	cases := map[string]bool{
		"":                          true,
		"dev":                       true,
		"0.0.0":                     true,
		"(devel)":                   true,
		"(devel) [@ a1b2c3d]":       true,
		"(devel) [@ a1b2c3d.dirty]": true,
		"v1.2.3":                    false,
		"1.2.3":                     false,
		"v0.0.1":                    false,
	}
	for v, want := range cases {
		if got := isSentinel(v); got != want {
			t.Errorf("isSentinel(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestDiffVersion(t *testing.T) {
	cases := []struct {
		declared, runtime string
		wantPass          bool
		wantReason        string
	}{
		// 0.0.0 + non-sentinel -> PASS (CI artifact)
		{"0.0.0", "v1.2.3", true, ""},
		{"0.0.0", "0.1.0", true, ""},
		// 0.0.0 + sentinel -> FAIL (ldflag missing)
		{"0.0.0", "", false, "ldflag"},
		{"0.0.0", "(devel)", false, "ldflag"},
		{"0.0.0", "(devel) [@ abc1234]", false, "ldflag"},
		{"0.0.0", "dev", false, "ldflag"},
		{"0.0.0", "0.0.0", false, "ldflag"},
		// X.Y.Z + vX.Y.Z or X.Y.Z -> PASS (normalize leading v)
		{"1.2.3", "v1.2.3", true, ""},
		{"1.2.3", "1.2.3", true, ""},
		// X.Y.Z + sentinel -> FAIL
		{"1.2.3", "", false, "ldflag"},
		{"1.2.3", "(devel)", false, "ldflag"},
		{"1.2.3", "(devel) [@ deadbee]", false, "ldflag"},
		// X.Y.Z + drift -> FAIL
		{"1.2.3", "v0.9.0", false, "drift"},
		{"1.2.3", "v2.0.0", false, "drift"},
	}
	for _, c := range cases {
		pass, reason := diffVersion(c.declared, c.runtime)
		if pass != c.wantPass {
			t.Errorf("diffVersion(%q, %q) pass=%v want=%v reason=%q",
				c.declared, c.runtime, pass, c.wantPass, reason)
			continue
		}
		if !pass && !strings.Contains(reason, c.wantReason) {
			t.Errorf("diffVersion(%q, %q) reason=%q want substring %q",
				c.declared, c.runtime, reason, c.wantReason)
		}
	}
}
