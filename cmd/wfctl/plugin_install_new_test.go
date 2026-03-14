package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// ============================================================
// helpers shared by tests in this file
// ============================================================

// buildPluginTarGz builds an in-memory tar.gz whose layout matches a real
// GoReleaser plugin release: a single top-level directory containing the
// binary and plugin.json.
func buildPluginTarGz(t *testing.T, pluginName string, binaryContent []byte, pjContent []byte) []byte {
	t.Helper()
	topDir := pluginName + "-" + runtime.GOOS + "-" + runtime.GOARCH
	entries := map[string][]byte{
		topDir + "/" + pluginName:    binaryContent,
		topDir + "/plugin.json":      pjContent,
	}
	return buildTarGz(t, entries, 0755)
}

// minimalPluginJSON returns a valid, minimal plugin.json as bytes.
func minimalPluginJSON(name, version string) []byte {
	pj := installedPluginJSON{
		Name:        name,
		Version:     version,
		Author:      "tester",
		Description: "test plugin",
		Type:        "external",
		Tier:        "community",
		License:     "MIT",
	}
	data, _ := json.MarshalIndent(pj, "", "  ")
	return append(data, '\n')
}

// ============================================================
// Test 3: installFromURL
// ============================================================

// TestInstallFromURL sets up a local HTTP server serving a tar.gz archive
// with a valid plugin.json, calls installFromURL, and verifies:
//   - the plugin binary is extracted to <pluginDir>/<normalizedName>/<normalizedName>
//   - the plugin.json is written
//   - the lockfile (.wfctl.yaml in cwd) is updated with a checksum
func TestInstallFromURL(t *testing.T) {
	const pluginName = "url-test-plugin"
	binaryContent := []byte("#!/bin/sh\necho url-test\n")
	pjContent := minimalPluginJSON(pluginName, "1.2.3")

	tarball := buildPluginTarGz(t, pluginName, binaryContent, pjContent)
	tarChecksum := sha256Hex(tarball)

	// Serve tarball from a local httptest server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(tarball) //nolint:errcheck
	}))
	defer srv.Close()

	// Run inside a temp cwd so .wfctl.yaml ends up there, not the repo root.
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cwdDir := t.TempDir()
	if err := os.Chdir(cwdDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(origWD) }) //nolint:errcheck

	pluginsDir := t.TempDir()

	if err := installFromURL(srv.URL+"/"+pluginName+".tar.gz", pluginsDir); err != nil {
		t.Fatalf("installFromURL: %v", err)
	}

	// Binary should exist at <pluginsDir>/<pluginName>/<pluginName>.
	binaryPath := filepath.Join(pluginsDir, pluginName, pluginName)
	gotBinary, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if !bytes.Equal(gotBinary, binaryContent) {
		t.Errorf("binary content mismatch: got %q, want %q", gotBinary, binaryContent)
	}

	// plugin.json should be present.
	pjPath := filepath.Join(pluginsDir, pluginName, "plugin.json")
	if _, err := os.Stat(pjPath); err != nil {
		t.Errorf("plugin.json not found: %v", err)
	}

	// Lockfile should record the plugin with a checksum. It is written to
	// .wfctl.yaml in the cwd (cwdDir).
	lockfilePath := filepath.Join(cwdDir, ".wfctl.yaml")
	lf, loadErr := loadPluginLockfile(lockfilePath)
	if loadErr != nil {
		t.Fatalf("load lockfile: %v", loadErr)
	}
	entry, ok := lf.Plugins[pluginName]
	if !ok {
		t.Fatalf("lockfile missing entry for %q; entries: %v", pluginName, lf.Plugins)
	}
	if entry.SHA256 != tarChecksum {
		t.Errorf("lockfile checksum: got %q, want %q", entry.SHA256, tarChecksum)
	}
	if entry.Version != "1.2.3" {
		t.Errorf("lockfile version: got %q, want %q", entry.Version, "1.2.3")
	}
}

// TestInstallFromURL_404 verifies that a 404 from the server returns an error.
func TestInstallFromURL_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	err := installFromURL(srv.URL+"/missing.tar.gz", t.TempDir())
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

// TestInstallFromURL_MissingPluginJSON verifies that a tarball without
// plugin.json returns an error.
func TestInstallFromURL_MissingPluginJSON(t *testing.T) {
	// Build tarball with only a binary, no plugin.json.
	topDir := "plugin-linux-amd64"
	entries := map[string][]byte{
		topDir + "/binary": []byte("#!/bin/sh\n"),
	}
	tarball := buildTarGz(t, entries, 0755)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(tarball) //nolint:errcheck
	}))
	defer srv.Close()

	err := installFromURL(srv.URL+"/plugin.tar.gz", t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing plugin.json, got nil")
	}
}

// TestInstallFromURL_NameNormalization verifies that a plugin named
// "workflow-plugin-foo" is normalized to "foo" in the destination path.
func TestInstallFromURL_NameNormalization(t *testing.T) {
	const fullName = "workflow-plugin-foo"
	const shortName = "foo"

	pjContent := minimalPluginJSON(fullName, "0.1.0")
	entries := map[string][]byte{
		"top/" + fullName:    []byte("#!/bin/sh\n"),
		"top/plugin.json":    pjContent,
	}
	tarball := buildTarGz(t, entries, 0755)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(tarball) //nolint:errcheck
	}))
	defer srv.Close()

	// Run in a temp cwd so .wfctl.yaml lockfile stays isolated.
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(origWD) }) //nolint:errcheck

	pluginsDir := t.TempDir()
	if err := installFromURL(srv.URL+"/plugin.tar.gz", pluginsDir); err != nil {
		t.Fatalf("installFromURL: %v", err)
	}

	// Destination should use the normalized short name.
	destDir := filepath.Join(pluginsDir, shortName)
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		t.Errorf("expected dest dir %s to exist (normalized from %q)", destDir, fullName)
	}
}

// ============================================================
// Test 4: installFromLocal
// ============================================================

// TestInstallFromLocal sets up a temp dir with a fake plugin.json and binary,
// calls installFromLocal, and verifies the files are copied correctly.
func TestInstallFromLocal(t *testing.T) {
	const pluginName = "local-plugin"
	binaryContent := []byte("#!/bin/sh\necho local\n")

	// Create a source directory with plugin.json and binary.
	srcDir := t.TempDir()
	pjContent := minimalPluginJSON(pluginName, "2.0.0")
	if err := os.WriteFile(filepath.Join(srcDir, "plugin.json"), pjContent, 0640); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	// Binary named to match plugin name.
	binaryPath := filepath.Join(srcDir, pluginName)
	if err := os.WriteFile(binaryPath, binaryContent, 0750); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	pluginsDir := t.TempDir()
	if err := installFromLocal(srcDir, pluginsDir); err != nil {
		t.Fatalf("installFromLocal: %v", err)
	}

	// Verify binary was copied.
	destBinary := filepath.Join(pluginsDir, pluginName, pluginName)
	gotContent, err := os.ReadFile(destBinary)
	if err != nil {
		t.Fatalf("read dest binary: %v", err)
	}
	if !bytes.Equal(gotContent, binaryContent) {
		t.Errorf("binary content mismatch: got %q, want %q", gotContent, binaryContent)
	}

	// Verify plugin.json was copied.
	destPJ := filepath.Join(pluginsDir, pluginName, "plugin.json")
	if _, err := os.Stat(destPJ); err != nil {
		t.Errorf("plugin.json not in dest: %v", err)
	}
}

// TestInstallFromLocal_NameNormalization verifies that a plugin named
// "workflow-plugin-bar" is normalized to "bar" in the destination path.
func TestInstallFromLocal_NameNormalization(t *testing.T) {
	const fullName = "workflow-plugin-bar"
	const shortName = "bar"

	srcDir := t.TempDir()
	pjContent := minimalPluginJSON(fullName, "0.1.0")
	if err := os.WriteFile(filepath.Join(srcDir, "plugin.json"), pjContent, 0640); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	// Binary named with full "workflow-plugin-" prefix (also accepted).
	binaryPath := filepath.Join(srcDir, shortName)
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\n"), 0750); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	pluginsDir := t.TempDir()
	if err := installFromLocal(srcDir, pluginsDir); err != nil {
		t.Fatalf("installFromLocal: %v", err)
	}

	destDir := filepath.Join(pluginsDir, shortName)
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		t.Errorf("expected dest dir %s (normalized from %q)", destDir, fullName)
	}
}

// TestInstallFromLocal_FallbackBinaryName verifies that installFromLocal
// falls back to looking for "workflow-plugin-<name>" when the short name
// binary is not found.
func TestInstallFromLocal_FallbackBinaryName(t *testing.T) {
	const pluginName = "baz"
	const fullBinaryName = "workflow-plugin-baz"

	srcDir := t.TempDir()
	pjContent := minimalPluginJSON(pluginName, "0.1.0")
	if err := os.WriteFile(filepath.Join(srcDir, "plugin.json"), pjContent, 0640); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	// Binary uses the full name.
	if err := os.WriteFile(filepath.Join(srcDir, fullBinaryName), []byte("#!/bin/sh\n"), 0750); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	pluginsDir := t.TempDir()
	if err := installFromLocal(srcDir, pluginsDir); err != nil {
		t.Fatalf("installFromLocal with fallback name: %v", err)
	}

	// The installed binary should be renamed to the short name.
	destBinary := filepath.Join(pluginsDir, pluginName, pluginName)
	if _, err := os.Stat(destBinary); err != nil {
		t.Errorf("expected binary at %s: %v", destBinary, err)
	}
}

// TestInstallFromLocal_MissingPluginJSON verifies that missing plugin.json returns an error.
func TestInstallFromLocal_MissingPluginJSON(t *testing.T) {
	srcDir := t.TempDir()
	err := installFromLocal(srcDir, t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing plugin.json, got nil")
	}
}

// TestInstallFromLocal_MissingBinary verifies that missing binary returns an error.
func TestInstallFromLocal_MissingBinary(t *testing.T) {
	srcDir := t.TempDir()
	pjContent := minimalPluginJSON("nobinary", "0.1.0")
	if err := os.WriteFile(filepath.Join(srcDir, "plugin.json"), pjContent, 0640); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	// No binary file.
	err := installFromLocal(srcDir, t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
}

// ============================================================
// Test 5: verifyInstalledChecksum
// ============================================================

// TestVerifyInstalledChecksum verifies that verifyInstalledChecksum:
//   - succeeds when the checksum matches the binary content
//   - fails when the checksum does not match
func TestVerifyInstalledChecksum_Match(t *testing.T) {
	content := []byte("plugin binary content for checksum test")
	h := sha256.Sum256(content)
	expectedSHA := hex.EncodeToString(h[:])

	pluginDir := t.TempDir()
	const pluginName = "checksum-plugin"
	binaryPath := filepath.Join(pluginDir, pluginName)
	if err := os.WriteFile(binaryPath, content, 0750); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	if err := verifyInstalledChecksum(pluginDir, pluginName, expectedSHA); err != nil {
		t.Errorf("expected checksum match, got error: %v", err)
	}
}

// TestVerifyInstalledChecksum_Mismatch verifies that a wrong checksum is rejected.
func TestVerifyInstalledChecksum_Mismatch(t *testing.T) {
	content := []byte("plugin binary content")
	pluginDir := t.TempDir()
	const pluginName = "checksum-plugin"
	binaryPath := filepath.Join(pluginDir, pluginName)
	if err := os.WriteFile(binaryPath, content, 0750); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	wrongSHA := "0000000000000000000000000000000000000000000000000000000000000000"
	if err := verifyInstalledChecksum(pluginDir, pluginName, wrongSHA); err == nil {
		t.Error("expected error for checksum mismatch, got nil")
	}
}

// TestVerifyInstalledChecksum_MissingBinary verifies that a missing binary returns an error.
func TestVerifyInstalledChecksum_MissingBinary(t *testing.T) {
	pluginDir := t.TempDir()
	err := verifyInstalledChecksum(pluginDir, "nonexistent-plugin", "abc123")
	if err == nil {
		t.Error("expected error for missing binary, got nil")
	}
}

// TestVerifyInstalledChecksum_CaseInsensitive verifies that checksum comparison
// is case-insensitive (uppercase hex is accepted).
func TestVerifyInstalledChecksum_CaseInsensitive(t *testing.T) {
	content := []byte("case insensitive checksum test")
	h := sha256.Sum256(content)
	lowerSHA := hex.EncodeToString(h[:])
	upperSHA := hex.EncodeToString(h[:])
	for i := range upperSHA {
		if upperSHA[i] >= 'a' && upperSHA[i] <= 'f' {
			upperSHA = upperSHA[:i] + string(rune(upperSHA[i]-32)) + upperSHA[i+1:]
		}
	}

	pluginDir := t.TempDir()
	const pluginName = "case-plugin"
	if err := os.WriteFile(filepath.Join(pluginDir, pluginName), content, 0750); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	// Both lower and upper should succeed.
	if err := verifyInstalledChecksum(pluginDir, pluginName, lowerSHA); err != nil {
		t.Errorf("lowercase checksum failed: %v", err)
	}
	if err := verifyInstalledChecksum(pluginDir, pluginName, upperSHA); err != nil {
		t.Errorf("uppercase checksum failed: %v", err)
	}
}

// ============================================================
// Test 8: copyFile helper
// ============================================================

// TestCopyFile verifies that copyFile copies content and sets the correct mode.
func TestCopyFile(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	content := []byte("copy me please")
	srcPath := filepath.Join(srcDir, "source.txt")
	if err := os.WriteFile(srcPath, content, 0640); err != nil {
		t.Fatalf("write source: %v", err)
	}

	dstPath := filepath.Join(dstDir, "dest.txt")
	const wantMode = os.FileMode(0750)
	if err := copyFile(srcPath, dstPath, wantMode); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	got, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}

	info, err := os.Stat(dstPath)
	if err != nil {
		t.Fatalf("stat dest: %v", err)
	}
	if info.Mode() != wantMode {
		t.Errorf("mode: got %v, want %v", info.Mode(), wantMode)
	}
}

// TestCopyFile_MissingSource verifies that a missing source returns an error.
func TestCopyFile_MissingSource(t *testing.T) {
	err := copyFile("/nonexistent/source.txt", filepath.Join(t.TempDir(), "dest.txt"), 0640)
	if err == nil {
		t.Fatal("expected error for missing source, got nil")
	}
}

// TestCopyFile_NonWritableDest verifies that an unwritable destination returns an error.
func TestCopyFile_OverwritesExisting(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src := filepath.Join(srcDir, "new.txt")
	dst := filepath.Join(dstDir, "old.txt")

	// Write initial dest content.
	if err := os.WriteFile(dst, []byte("old content"), 0640); err != nil {
		t.Fatalf("write initial dest: %v", err)
	}
	// Write new source content.
	if err := os.WriteFile(src, []byte("new content"), 0640); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if err := copyFile(src, dst, 0640); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != "new content" {
		t.Errorf("overwrite failed: got %q, want %q", got, "new content")
	}
}
