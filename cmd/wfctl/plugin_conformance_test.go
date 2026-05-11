package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPluginConformanceRequiresExactlyOneSource(t *testing.T) {
	if err := runPluginConformance([]string{"--mode", "typed-iac"}); err == nil {
		t.Fatal("expected missing source error")
	}
	if err := runPluginConformance([]string{"--mode", "typed-iac", "--artifact", "x.tar.gz", "cmd/wfctl/testdata/conformance/iac-pass"}); err == nil {
		t.Fatal("expected mutually exclusive source error")
	}
}

func TestPluginConformanceHelpListsFlags(t *testing.T) {
	output, err := captureStderr(t, func() error {
		return runPluginConformance([]string{"--help"})
	})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("runPluginConformance --help error = %v, want flag.ErrHelp", err)
	}
	for _, want := range []string{"--artifact", "--build-package", "--mode", "--engine-version", "--timeout", "executes plugin code"} {
		if !strings.Contains(output, want) {
			t.Fatalf("help output missing %q:\n%s", want, output)
		}
	}
}

func TestPluginConformanceLocalJSONPass(t *testing.T) {
	fixture := prepareIACPassFixture(t)
	out := filepath.Join(t.TempDir(), "evidence.json")
	t.Setenv("WFCTL_CONFORMANCE_SECRET", "secret-value-that-must-not-leak")
	stdout, err := captureStdout(t, func() error {
		return runPluginConformance([]string{
			"--mode", "typed-iac",
			"--engine-version", "v0.51.2",
			"--format", "json",
			"--output", out,
			fixture,
		})
	})
	if err != nil {
		t.Fatalf("runPluginConformance: %v", err)
	}
	if strings.Contains(stdout, "{") {
		t.Fatalf("stdout should stay concise when --output is used, got %q", stdout)
	}
	if strings.Contains(stdout, "iac-pass stderr marker") {
		t.Fatalf("plugin output leaked to wfctl stdout: %q", stdout)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read evidence: %v", err)
	}
	if bytes.Contains(raw, []byte("secret-value-that-must-not-leak")) {
		t.Fatalf("evidence JSON leaked environment data: %s", raw)
	}
	ev := readEvidence(t, out)
	if ev.Status != PluginCompatibilityStatusPass {
		t.Fatalf("status = %q, want pass", ev.Status)
	}
	if ev.Mode != PluginCompatibilityModeTypedIaC {
		t.Fatalf("mode = %q", ev.Mode)
	}
	if ev.Plugin != "iac-pass" || ev.Version != "v0.1.0" || ev.EngineVersion != "v0.51.2" {
		t.Fatalf("unexpected evidence identity: %#v", ev)
	}
	if ev.WfctlVersion != buildVersion() {
		t.Fatalf("wfctlVersion = %q, want actual build version %q", ev.WfctlVersion, buildVersion())
	}
	if ev.BinarySHA256 == "" || ev.PluginManifestSHA256 == "" || ev.EvidenceDigest == "" {
		t.Fatalf("missing hashes/digest: %#v", ev)
	}
	if ev.ArchiveSHA256 != "" {
		t.Fatalf("local-dir evidence should not include archiveSHA256: %#v", ev)
	}
	if !strings.Contains(ev.StderrTail, "iac-pass stderr marker") {
		t.Fatalf("stderr tail missing plugin output: %#v", ev)
	}
}

func TestPluginConformanceBuildsRequestedPackage(t *testing.T) {
	fixture := prepareIACPassFixture(t)
	cmdDir := filepath.Join(fixture, "cmd", "plugin")
	if err := os.MkdirAll(cmdDir, 0o750); err != nil {
		t.Fatalf("mkdir cmd/plugin: %v", err)
	}
	if err := os.Rename(filepath.Join(fixture, "main.go"), filepath.Join(cmdDir, "main.go")); err != nil {
		t.Fatalf("move main.go: %v", err)
	}
	out := filepath.Join(t.TempDir(), "evidence.json")
	if err := runPluginConformance([]string{
		"--mode", "typed-iac",
		"--engine-version", "v0.51.2",
		"--build-package", "./cmd/plugin",
		"--format", "json",
		"--output", out,
		fixture,
	}); err != nil {
		t.Fatalf("runPluginConformance with build package: %v", err)
	}
	ev := readEvidence(t, out)
	if ev.Status != PluginCompatibilityStatusPass {
		t.Fatalf("status = %q, want pass", ev.Status)
	}
}

func TestPluginConformanceSourceBuildPackageIgnoresRootBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell executable fixture is Unix-specific")
	}
	fixture := prepareIACPassFixture(t)
	cmdDir := filepath.Join(fixture, "cmd", "plugin")
	if err := os.MkdirAll(cmdDir, 0o750); err != nil {
		t.Fatalf("mkdir cmd/plugin: %v", err)
	}
	if err := os.Rename(filepath.Join(fixture, "main.go"), filepath.Join(cmdDir, "main.go")); err != nil {
		t.Fatalf("move main.go: %v", err)
	}
	staleBinary := filepath.Join(fixture, "iac-pass")
	if err := os.WriteFile(staleBinary, []byte("#!/bin/sh\necho stale-root-binary >&2\nexit 9\n"), 0o755); err != nil {
		t.Fatalf("write stale root binary: %v", err)
	}
	out := filepath.Join(t.TempDir(), "evidence.json")
	if err := runPluginConformance([]string{
		"--mode", "typed-iac",
		"--engine-version", "v0.51.2",
		"--build-package", "./cmd/plugin",
		"--format", "json",
		"--output", out,
		fixture,
	}); err != nil {
		t.Fatalf("runPluginConformance with stale root binary: %v", err)
	}
	ev := readEvidence(t, out)
	if strings.Contains(ev.StderrTail, "stale-root-binary") {
		t.Fatalf("source conformance used stale root binary: %#v", ev)
	}
}

func TestPluginConformanceRejectsUnsafeBuildPackage(t *testing.T) {
	fixture := prepareIACPassFixture(t)
	for _, buildPackage := range []string{
		"-toolexec=/tmp/evil",
		"example.com/other/plugin",
		"../other",
		"./../other",
		"/tmp/plugin",
		"./...",
		"./cmd/...",
	} {
		t.Run(buildPackage, func(t *testing.T) {
			err := runPluginConformance([]string{
				"--mode", "typed-iac",
				"--engine-version", "v0.51.2",
				"--build-package", buildPackage,
				fixture,
			})
			if err == nil {
				t.Fatal("expected unsafe build package to be rejected")
			}
			if !strings.Contains(err.Error(), "build-package") {
				t.Fatalf("error = %v, want build-package context", err)
			}
		})
	}
}

func TestPluginConformanceRejectsExplicitBuildPackageWithArtifact(t *testing.T) {
	fixture := prepareIACPassFixture(t)
	archive := filepath.Join(t.TempDir(), "iac-pass.tar.gz")
	writeTarGzFromDir(t, fixture, archive)
	err := runPluginConformance([]string{
		"--mode", "typed-iac",
		"--artifact", archive,
		"--build-package", ".",
	})
	if err == nil {
		t.Fatal("expected explicit build package with artifact to fail")
	}
	if !strings.Contains(err.Error(), "--build-package") {
		t.Fatalf("error = %v, want --build-package context", err)
	}
}

func TestNormalizeConformanceBuildPackage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "root", in: ".", want: "."},
		{name: "trim", in: " ./cmd/plugin ", want: "./cmd/plugin"},
		{name: "clean", in: "./cmd/../plugin", want: "./plugin"},
		{name: "empty", in: "", wantErr: true},
		{name: "slash root", in: "./", wantErr: true},
		{name: "backslash", in: `.\cmd\plugin`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeConformanceBuildPackage(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizeConformanceBuildPackage(%q) succeeded, want error", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeConformanceBuildPackage(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeConformanceBuildPackage(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestPluginConformanceDefaultEngineVersionIsStrictSemver(t *testing.T) {
	t.Setenv("WFCTL_ENGINE_VERSION", "")
	got := resolveConformanceEngineVersion()
	if _, err := CanonicalEngineVersion(got); err != nil {
		t.Fatalf("default engine version %q is not strict semver: %v", got, err)
	}
	t.Setenv("WFCTL_ENGINE_VERSION", "v0.51.2")
	if got := resolveConformanceEngineVersion(); got != "v0.51.2" {
		t.Fatalf("env engine version = %q, want v0.51.2", got)
	}
}

func TestPluginConformancePluginEnvIsScrubbed(t *testing.T) {
	t.Setenv("COMPUTE_API_TOKEN", "must-not-reach-plugin")
	t.Setenv("DIGITALOCEAN_TOKEN", "must-not-reach-plugin")
	for _, kv := range conformancePluginEnv() {
		if strings.HasPrefix(kv, "COMPUTE_API_TOKEN=") || strings.HasPrefix(kv, "DIGITALOCEAN_TOKEN=") {
			t.Fatalf("sensitive env leaked to conformance plugin: %q", kv)
		}
	}
}

func TestPluginConformanceSourceCopySkipsSensitiveFilesAndSymlinks(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, ".env"), []byte("SECRET=value\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside-secret")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(src, "linked-secret")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "stage")
	if err := copyConformanceSourceDir(src, dst); err != nil {
		t.Fatalf("copyConformanceSourceDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "main.go")); err != nil {
		t.Fatalf("expected normal file copied: %v", err)
	}
	for _, forbidden := range []string{".env", "linked-secret"} {
		if _, err := os.Stat(filepath.Join(dst, forbidden)); !os.IsNotExist(err) {
			t.Fatalf("%s should not be staged, stat err=%v", forbidden, err)
		}
	}
}

func TestPluginConformanceNoTypedIaCServiceFails(t *testing.T) {
	fixture := prepareNoIACFixture(t)
	out := filepath.Join(t.TempDir(), "failure.json")
	err := runPluginConformance([]string{
		"--mode", "typed-iac",
		"--engine-version", "v0.51.2",
		"--format", "json",
		"--output", out,
		fixture,
	})
	if err == nil {
		t.Fatal("expected no typed-IaC service error")
	}
	if !strings.Contains(err.Error(), "typed") && !strings.Contains(err.Error(), "IaC") && !strings.Contains(err.Error(), "legacy") {
		t.Fatalf("error = %v, want typed-IaC context", err)
	}
	ev := readEvidence(t, out)
	if ev.Status != PluginCompatibilityStatusFail {
		t.Fatalf("status = %q, want fail", ev.Status)
	}
	if ev.EvidenceDigest == "" {
		t.Fatalf("failure evidence missing digest: %#v", ev)
	}
}

func TestPluginConformanceTextFormat(t *testing.T) {
	fixture := prepareIACPassFixture(t)
	if err := runPluginConformance([]string{
		"--mode", "typed-iac",
		"--engine-version", "v0.51.2",
		"--format", "text",
		fixture,
	}); err != nil {
		t.Fatalf("runPluginConformance text: %v", err)
	}
}

func TestPluginConformanceArchiveIncludesArchiveHash(t *testing.T) {
	dir := t.TempDir()
	fixture := prepareIACPassFixture(t)
	archive := filepath.Join(dir, "iac-pass.tar.gz")
	writeTarGzFromDir(t, fixture, archive)
	out := filepath.Join(dir, "evidence.json")
	if err := runPluginConformance([]string{
		"--mode", "typed-iac",
		"--artifact", archive,
		"--engine-version", "v0.51.2",
		"--format", "json",
		"--output", out,
	}); err != nil {
		t.Fatalf("runPluginConformance archive: %v", err)
	}
	ev := readEvidence(t, out)
	if ev.ArchiveSHA256 == "" {
		t.Fatalf("archive evidence missing archiveSHA256: %#v", ev)
	}
	want, err := hashFileSHA256(archive)
	if err != nil {
		t.Fatalf("hash archive: %v", err)
	}
	if ev.ArchiveSHA256 != want {
		t.Fatalf("archiveSHA256 = %q, want %q", ev.ArchiveSHA256, want)
	}
}

func TestPluginConformanceTimeoutKillsPlugin(t *testing.T) {
	err := runPluginConformance([]string{
		"--mode", "typed-iac",
		"--engine-version", "v0.51.2",
		"--timeout", "200ms",
		"testdata/conformance/iac-hang",
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("error = %v, want timeout", err)
	}
}

func readEvidence(t *testing.T, path string) PluginCompatibilityEvidence {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read evidence: %v", err)
	}
	var ev PluginCompatibilityEvidence
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("parse evidence: %v", err)
	}
	return ev
}

func prepareIACPassFixture(t *testing.T) string {
	t.Helper()
	return prepareConformanceFixture(t, "iac-pass")
}

func prepareNoIACFixture(t *testing.T) string {
	t.Helper()
	return prepareConformanceFixture(t, "no-iac")
}

func prepareConformanceFixture(t *testing.T, name string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), name)
	if err := copyDir(filepath.Join("testdata/conformance", name), dst); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	goMod := "module example.com/" + name + "\n\ngo 1.26.0\n\nrequire github.com/GoCodeAlone/workflow v0.0.0\n\nreplace github.com/GoCodeAlone/workflow => " + filepath.ToSlash(root) + "\n"
	if err := os.WriteFile(filepath.Join(dst, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatalf("write fixture go.mod: %v", err)
	}
	if err := copyFile(filepath.Join(root, "go.sum"), filepath.Join(dst, "go.sum"), 0o600); err != nil {
		t.Fatalf("copy fixture go.sum: %v", err)
	}
	return dst
}

func writeTarGzFromDir(t *testing.T, srcDir, dest string) {
	t.Helper()
	f, err := os.Create(dest)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer f.Close()
	gw := gzip.NewWriter(f)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()
	if err := filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(filepath.Join("iac-pass", rel))
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(tw, in)
		return err
	}); err != nil {
		t.Fatalf("write archive: %v", err)
	}
}
