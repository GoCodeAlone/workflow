package main

import (
	"fmt"
	"os"
	"os/exec"
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

// buildFixtureBinaryForVerify builds the fixture scenario in-place and emits
// the binary to t.TempDir(). ldflag is the -X ...Version= value ("" = no flag,
// which makes ResolveBuildVersion fall back to "(devel) [@ sha]" for fixtures
// whose initial Version var is "dev").
func buildFixtureBinaryForVerify(t *testing.T, scenario, ldflagTag string) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "p")
	args := []string{"build", "-mod=readonly"}
	if ldflagTag != "" {
		// Fixture main.go is `package main` with `var Version` at fixture root,
		// so the linker symbol is `main.Version` (NOT `<module>/internal.Version`
		// as production plugins use). Empirically verified via `go tool nm`.
		args = append(args, "-ldflags",
			fmt.Sprintf("-X main.Version=%s", ldflagTag))
	}
	_ = scenario // retained for future scenario-specific build customization
	args = append(args, "-o", binPath, ".")
	cmd := exec.Command("go", args...)
	cmd.Dir = filepath.Join("testdata", "verify_capabilities", scenario)
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", scenario, err, out)
	}
	return binPath
}

func TestVerifyCapabilities_Good(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "good", "v0.1.0")
	if err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/good"}); err != nil {
		t.Fatalf("want PASS, got: %v", err)
	}
}

func TestVerifyCapabilities_ReleaseGood(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "release-good", "v1.2.3")
	if err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/release-good"}); err != nil {
		t.Fatalf("want PASS, got: %v", err)
	}
}

func TestVerifyCapabilities_MissingLdflag(t *testing.T) {
	// No ldflag → Version stays "dev" → ResolveBuildVersion("dev") → "(devel) [@ sha]"
	bin := buildFixtureBinaryForVerify(t, "missing-ldflag", "")
	err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/missing-ldflag"})
	if err == nil {
		t.Fatal("want FAIL, got nil")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("want mismatch error, got: %v", err)
	}
}

func TestVerifyCapabilities_VersionDrift(t *testing.T) {
	bin := buildFixtureBinaryForVerify(t, "version-drift", "v0.9.0")
	err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/version-drift"})
	if err == nil {
		t.Fatal("want FAIL, got nil")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("want mismatch error, got: %v", err)
	}
}

func TestVerifyCapabilities_NameDrift(t *testing.T) {
	// Build with non-sentinel ldflag tag so Version PASSes — matrix row that
	// fires: plugin.json="0.0.0" + binary="v0.0.0" → PASS via the
	// `declared == "0.0.0"` branch returning early (isSentinel("v0.0.0")==false
	// because the SDK sentinel set is {"", "dev", "0.0.0", "(devel)..."} — NOT
	// "v0.0.0"). This ISOLATES Name as the sole failure under test, so a
	// regression that breaks Name-diff while leaving Version-diff intact
	// doesn't silently pass through a lenient `Contains("mismatch")` check.
	bin := buildFixtureBinaryForVerify(t, "name-drift", "v0.0.0")
	err := runPluginVerifyCapabilities([]string{"--binary", bin, "testdata/verify_capabilities/name-drift"})
	if err == nil {
		t.Fatal("want FAIL, got nil")
	}
	// Tighter assertion: error must specifically mention "name:" prefix from the diff report.
	if !strings.Contains(err.Error(), "name:") && !strings.Contains(fmt.Sprintf("%v", err), "name:") {
		t.Errorf("want name-mismatch error, got: %v", err)
	}
}
