package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// writeLockfile creates a .wfctl.yaml in dir with the given YAML content.
// It returns the evaluated (symlink-resolved) path so comparisons with
// findLockfile (which resolves via os.Getwd) work on macOS where /tmp -> /private/tmp.
func writeLockfile(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, ".wfctl.yaml")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write lockfile: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("failed to resolve lockfile symlink: %v", err)
	}
	return resolved
}

// writeBinary creates a fake plugin binary at <pluginDir>/<pluginName>/<pluginName>
// and returns its SHA-256 hex digest.
func writeBinary(t *testing.T, pluginDir, pluginName string, data []byte) string {
	t.Helper()
	dir := filepath.Join(pluginDir, pluginName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to mkdir plugin binary dir: %v", err)
	}
	binPath := filepath.Join(dir, pluginName)
	if err := os.WriteFile(binPath, data, 0755); err != nil {
		t.Fatalf("failed to write fake binary: %v", err)
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// TestVerifyPluginIntegrity_UnreadableLockfile verifies that the function fails
// closed when the lockfile exists but cannot be read.
func TestVerifyPluginIntegrity_UnreadableLockfile(t *testing.T) {
	dir := t.TempDir()

	orig, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(orig) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// Create a lockfile with no read permission.
	p := filepath.Join(dir, ".wfctl.yaml")
	if err := os.WriteFile(p, []byte("plugins:\n  my-plugin:\n    sha256: abc\n"), 0000); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}

	// POSIX permission bits are not enforced on all platforms (e.g. Windows, or
	// when running as root). Verify the file is actually unreadable before
	// asserting fail-closed behaviour.
	if _, err := os.ReadFile(p); err == nil {
		t.Skip("lockfile is readable despite 0000 permissions (platform or root); skipping test")
	}

	err := VerifyPluginIntegrity(filepath.Join(dir, "plugins"), "my-plugin")
	if err == nil {
		t.Error("expected error when lockfile is unreadable, got nil (fail-open)")
	}
}

// TestVerifyPluginIntegrity_MalformedLockfile verifies that the function fails
// closed when the lockfile exists but contains invalid YAML.
func TestVerifyPluginIntegrity_MalformedLockfile(t *testing.T) {
	dir := t.TempDir()

	orig, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(orig) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	writeLockfile(t, dir, "{{{{not valid yaml")

	err := VerifyPluginIntegrity(filepath.Join(dir, "plugins"), "my-plugin")
	if err == nil {
		t.Error("expected error when lockfile contains invalid YAML, got nil (fail-open)")
	}
}

// TestVerifyPluginIntegrity_NoLockfile verifies that the function returns nil
// when no lockfile can be found in the directory hierarchy.
func TestVerifyPluginIntegrity_NoLockfile(t *testing.T) {
	// Use a fresh temp dir with no .wfctl.yaml anywhere.
	dir := t.TempDir()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { os.Chdir(orig) })

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	err = VerifyPluginIntegrity(filepath.Join(dir, "plugins"), "my-plugin")
	if err != nil {
		t.Errorf("expected nil when no lockfile exists, got: %v", err)
	}
}

// TestVerifyPluginIntegrity_NoEntryForPlugin verifies nil is returned when the
// lockfile exists but has no entry for the requested plugin.
func TestVerifyPluginIntegrity_NoEntryForPlugin(t *testing.T) {
	dir := t.TempDir()

	orig, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(orig) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	writeLockfile(t, dir, "plugins:\n  other-plugin:\n    sha256: abc123\n")

	err := VerifyPluginIntegrity(filepath.Join(dir, "plugins"), "my-plugin")
	if err != nil {
		t.Errorf("expected nil when lockfile has no entry for plugin, got: %v", err)
	}
}

// TestVerifyPluginIntegrity_NoSHA256InEntry verifies nil is returned when the
// lockfile entry exists but has no sha256 field.
func TestVerifyPluginIntegrity_NoSHA256InEntry(t *testing.T) {
	dir := t.TempDir()

	orig, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(orig) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	writeLockfile(t, dir, "plugins:\n  my-plugin:\n    sha256: \"\"\n")

	err := VerifyPluginIntegrity(filepath.Join(dir, "plugins"), "my-plugin")
	if err != nil {
		t.Errorf("expected nil when lockfile entry has empty sha256, got: %v", err)
	}
}

// TestVerifyPluginIntegrity_ChecksumMatches verifies nil is returned when the
// binary checksum matches the lockfile entry.
func TestVerifyPluginIntegrity_ChecksumMatches(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")

	orig, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(orig) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	binaryData := []byte("fake plugin binary content")
	digest := writeBinary(t, pluginDir, "my-plugin", binaryData)

	lockContent := "plugins:\n  my-plugin:\n    sha256: " + digest + "\n"
	writeLockfile(t, dir, lockContent)

	err := VerifyPluginIntegrity(pluginDir, "my-plugin")
	if err != nil {
		t.Errorf("expected nil when checksum matches, got: %v", err)
	}
}

// TestVerifyPluginIntegrity_ChecksumMismatch verifies an error is returned when
// the binary checksum does not match the lockfile entry.
func TestVerifyPluginIntegrity_ChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")

	orig, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(orig) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	writeBinary(t, pluginDir, "my-plugin", []byte("correct binary"))

	// Write a lockfile with a wrong SHA.
	writeLockfile(t, dir, "plugins:\n  my-plugin:\n    sha256: 0000000000000000000000000000000000000000000000000000000000000000\n")

	err := VerifyPluginIntegrity(pluginDir, "my-plugin")
	if err == nil {
		t.Error("expected error when checksum mismatches, got nil")
	}
}

// TestVerifyPluginIntegrity_ChecksumMismatch_CaseInsensitive verifies that the
// comparison is case-insensitive (hex digits may be upper or lower case).
func TestVerifyPluginIntegrity_ChecksumMismatch_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")

	orig, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(orig) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	binaryData := []byte("plugin binary")
	digest := writeBinary(t, pluginDir, "my-plugin", binaryData)

	// Write lockfile with uppercase hex digest.
	upper := ""
	for _, c := range digest {
		if c >= 'a' && c <= 'f' {
			upper += string(rune(c - 32))
		} else {
			upper += string(c)
		}
	}
	writeLockfile(t, dir, "plugins:\n  my-plugin:\n    sha256: "+upper+"\n")

	err := VerifyPluginIntegrity(pluginDir, "my-plugin")
	if err != nil {
		t.Errorf("expected nil for case-insensitive match, got: %v", err)
	}
}

// TestFindLockfile_WalksUpDirectories verifies that findLockfile finds a
// .wfctl.yaml placed in a parent directory when CWD is a subdirectory.
func TestFindLockfile_WalksUpDirectories(t *testing.T) {
	// Build a directory structure: root/.wfctl.yaml, root/sub/sub2/ (cwd)
	root := t.TempDir()
	subDir := filepath.Join(root, "sub", "sub2")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	lockPath := writeLockfile(t, root, "plugins: {}\n")

	orig, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(orig) })
	if err := os.Chdir(subDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	found := findLockfile()
	if found != lockPath {
		t.Errorf("findLockfile() = %q, want %q", found, lockPath)
	}
}

// TestFindLockfile_NotFound verifies that findLockfile returns "" when no
// .wfctl.yaml exists anywhere in the walked hierarchy.
func TestFindLockfile_NotFound(t *testing.T) {
	dir := t.TempDir()

	orig, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(orig) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	found := findLockfile()
	if found != "" {
		t.Errorf("expected empty string when no lockfile found, got: %q", found)
	}
}

// TestFindLockfile_CWDFirst verifies that findLockfile returns the lockfile in
// CWD when lockfiles exist in both CWD and a parent directory.
func TestFindLockfile_CWDFirst(t *testing.T) {
	root := t.TempDir()
	subDir := filepath.Join(root, "child")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Lockfile in parent.
	writeLockfile(t, root, "plugins: {}\n")
	// Lockfile in CWD (child).
	childLock := writeLockfile(t, subDir, "plugins: {}\n")

	orig, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(orig) })
	if err := os.Chdir(subDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	found := findLockfile()
	if found != childLock {
		t.Errorf("findLockfile() = %q, want CWD lockfile %q", found, childLock)
	}
}
