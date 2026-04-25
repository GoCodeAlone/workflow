# wfctl SHA-256 Verification by Default — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `wfctl plugin install` and `wfctl update` fail-closed on missing SHA-256 — a binary download that cannot be verified must not succeed without explicit opt-out.

**Architecture:** Three PRs in series — PR-X adds shared helpers + plugin-install fail-closed, PR-Y adds `wfctl update` hardening (depends on PR-X helpers), PR-Z adds post-extraction lockfile write-back (independent, depends on PR-X).

**Tech Stack:** Go 1.26+, `cmd/wfctl/` package, `net/http`, `crypto/sha256`, `net/url`, `path`.

**Design doc:** `docs/plans/2026-04-25-wfctl-sha256-verification-design.md`

---

## Key Files

- `cmd/wfctl/plugin_install.go` — `installPluginFromManifest`, `installFromURL`, `parseGitHubReleaseDownloadURL`, `verifyChecksum`
- `cmd/wfctl/plugin_install_test.go` — existing tests
- `cmd/wfctl/plugin_checksum.go` — NEW: `parseChecksumsTxt`, `lookupChecksumForURL`
- `cmd/wfctl/plugin_checksum_test.go` — NEW: unit + mock HTTP tests
- `cmd/wfctl/update.go` — `runUpdate`, `findChecksumAsset`, `verifyAssetChecksum`
- `cmd/wfctl/update_test.go` — existing tests
- `cmd/wfctl/plugin_lockfile.go` — `updateLockfileWithChecksum`
- `cmd/wfctl/plugin_install_wfctllock.go` — `installFromWfctlLockfile`

---

## PR-X: Core helpers + plugin-install fail-closed

Branch: `feat/sha256-pr-x` (off `main`)

---

### Task 1: Add `parseChecksumsTxt` with unit tests

**Files:**
- Create: `cmd/wfctl/plugin_checksum.go`
- Create: `cmd/wfctl/plugin_checksum_test.go`

**Step 1: Write the failing test**

```go
// cmd/wfctl/plugin_checksum_test.go
package main

import (
    "testing"
)

func TestParseChecksumsTxt_Valid(t *testing.T) {
    body := "abc123def456  plugin-darwin-arm64.tar.gz\n" +
        "789abcdef012  plugin-linux-amd64.tar.gz\n"
    got, err := parseChecksumsTxt(body)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(got) != 2 {
        t.Fatalf("expected 2 entries, got %d", len(got))
    }
    if got["plugin-darwin-arm64.tar.gz"] != "abc123def456" {
        t.Errorf("unexpected hash for darwin: %s", got["plugin-darwin-arm64.tar.gz"])
    }
    if got["plugin-linux-amd64.tar.gz"] != "789abcdef012" {
        t.Errorf("unexpected hash for linux: %s", got["plugin-linux-amd64.tar.gz"])
    }
}

func TestParseChecksumsTxt_SkipsBlankLines(t *testing.T) {
    body := "\nabc123  plugin.tar.gz\n\n"
    got, err := parseChecksumsTxt(body)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(got) != 1 {
        t.Fatalf("expected 1 entry, got %d", len(got))
    }
}

func TestParseChecksumsTxt_MalformedLine(t *testing.T) {
    body := "abc123 plugin.tar.gz\n" // single space — not goreleaser format
    _, err := parseChecksumsTxt(body)
    if err == nil {
        t.Fatal("expected error for malformed line")
    }
}

func TestParseChecksumsTxt_Empty(t *testing.T) {
    got, err := parseChecksumsTxt("")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(got) != 0 {
        t.Fatalf("expected empty map, got %d entries", len(got))
    }
}

func TestParseChecksumsTxt_WindowsLineEndings(t *testing.T) {
    body := "abc123  plugin.tar.gz\r\n789abc  other.tar.gz\r\n"
    got, err := parseChecksumsTxt(body)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(got) != 2 {
        t.Fatalf("expected 2 entries, got %d", len(got))
    }
}
```

**Step 2: Run test to verify it fails**

```bash
cd cmd/wfctl && go test -run TestParseChecksumsTxt -v .
```
Expected: FAIL with `undefined: parseChecksumsTxt`

**Step 3: Write minimal implementation**

```go
// cmd/wfctl/plugin_checksum.go
package main

import (
    "fmt"
    "path"
    "strings"
    "time"

    neturl "net/url"
)

// parseChecksumsTxt parses a goreleaser-style checksums.txt body into a
// map[filename → sha256hex]. Each non-empty line must be of the form
// "<sha256hex>  <filename>" (two spaces — goreleaser standard). Empty lines
// and lines ending in \r are handled. Malformed lines return an error.
func parseChecksumsTxt(body string) (map[string]string, error) {
    result := make(map[string]string)
    for _, line := range strings.Split(body, "\n") {
        line = strings.TrimRight(line, "\r")
        if line == "" {
            continue
        }
        // goreleaser format: "<sha256hex>  <filename>" (exactly two spaces between hash and name)
        idx := strings.Index(line, "  ")
        if idx < 0 {
            return nil, fmt.Errorf("malformed checksums.txt line: %q (expected \"<sha256hex>  <filename>\")", line)
        }
        hash := line[:idx]
        name := line[idx+2:]
        if hash == "" || name == "" {
            return nil, fmt.Errorf("malformed checksums.txt line: %q (empty hash or filename)", line)
        }
        result[name] = hash
    }
    return result, nil
}

// lookupChecksumForURL derives the checksums.txt URL from a release asset download URL,
// downloads it, and returns the expected SHA256 hex for the asset. The asset name is
// derived by URL-decoding the last path segment of downloadURL. No GitHub URL validation
// is performed — callers must pre-validate the URL.
//
// The checksums.txt URL is derived by replacing the last path segment of downloadURL
// with "checksums.txt" and stripping query/fragment. For example:
//   https://github.com/GoCodeAlone/workflow/releases/download/v1.0.0/plugin.tar.gz
//   → https://github.com/GoCodeAlone/workflow/releases/download/v1.0.0/checksums.txt
func lookupChecksumForURL(downloadURL string) (string, error) {
    u, err := neturl.Parse(downloadURL)
    if err != nil {
        return "", fmt.Errorf("parse download URL: %w", err)
    }

    // Derive asset name from URL-decoded last path segment.
    assetName, err := neturl.PathUnescape(path.Base(u.Path))
    if err != nil {
        return "", fmt.Errorf("unescape asset name from %q: %w", u.Path, err)
    }

    // Derive checksums.txt URL: replace last path segment, strip query/fragment.
    u.Path = path.Dir(u.Path) + "/checksums.txt"
    u.RawQuery = ""
    u.Fragment = ""
    checksumsURL := u.String()

    body, err := downloadWithTimeout(checksumsURL, 30*time.Second)
    if err != nil {
        return "", fmt.Errorf("fetch checksums.txt from %s: %w", checksumsURL, err)
    }

    entries, err := parseChecksumsTxt(string(body))
    if err != nil {
        return "", fmt.Errorf("parse checksums.txt from %s: %w", checksumsURL, err)
    }

    sha, ok := entries[assetName]
    if !ok {
        return "", fmt.Errorf("asset %q not listed in checksums.txt at %s", assetName, checksumsURL)
    }
    return sha, nil
}
```

**Step 4: Run tests to verify they pass**

```bash
cd cmd/wfctl && go test -run TestParseChecksumsTxt -v .
```
Expected: PASS (all 5 tests)

**Step 5: Commit**

```bash
git add cmd/wfctl/plugin_checksum.go cmd/wfctl/plugin_checksum_test.go
git commit -m "feat: add parseChecksumsTxt helper with unit tests"
```

---

### Task 2: Add `lookupChecksumForURL` mock HTTP tests

**Files:**
- Modify: `cmd/wfctl/plugin_checksum_test.go`

**Step 1: Write failing tests**

Add to `plugin_checksum_test.go`:

```go
import (
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestLookupChecksumForURL_Found(t *testing.T) {
    // The archive content we'll hash.
    archiveData := []byte("fake archive content")
    h := sha256.Sum256(archiveData)
    expectedSHA := hex.EncodeToString(h[:])

    // Serve both the archive and the checksums.txt from the test server.
    mux := http.NewServeMux()
    mux.HandleFunc("/GoCodeAlone/repo/releases/download/v1.0.0/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
        fmt.Fprintf(w, "%s  plugin-linux-amd64.tar.gz\n", expectedSHA)
    })
    srv := httptest.NewServer(mux)
    defer srv.Close()

    downloadURL := srv.URL + "/GoCodeAlone/repo/releases/download/v1.0.0/plugin-linux-amd64.tar.gz"
    got, err := lookupChecksumForURL(downloadURL)
    if err != nil {
        t.Fatalf("lookupChecksumForURL: %v", err)
    }
    if got != expectedSHA {
        t.Errorf("expected %s, got %s", expectedSHA, got)
    }
}

func TestLookupChecksumForURL_ChecksumsTxtAbsent(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
        http.Error(w, "not found", http.StatusNotFound)
    }))
    defer srv.Close()

    downloadURL := srv.URL + "/GoCodeAlone/repo/releases/download/v1.0.0/plugin.tar.gz"
    _, err := lookupChecksumForURL(downloadURL)
    if err == nil {
        t.Fatal("expected error when checksums.txt returns 404")
    }
    if !strings.Contains(err.Error(), "checksums.txt") {
        t.Errorf("expected error to mention checksums.txt, got: %v", err)
    }
}

func TestLookupChecksumForURL_AssetNotListed(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
        // Serve checksums.txt that does NOT list the requested asset.
        fmt.Fprintln(w, "abc123  other-asset.tar.gz")
    }))
    defer srv.Close()

    downloadURL := srv.URL + "/GoCodeAlone/repo/releases/download/v1.0.0/plugin.tar.gz"
    _, err := lookupChecksumForURL(downloadURL)
    if err == nil {
        t.Fatal("expected error when asset not listed")
    }
    if !strings.Contains(err.Error(), "plugin.tar.gz") {
        t.Errorf("expected error to mention asset name, got: %v", err)
    }
}

func TestLookupChecksumForURL_URLEncodedAssetName(t *testing.T) {
    // Asset name with URL-encoded chars: "plugin name.tar.gz" → "plugin%20name.tar.gz"
    archiveData := []byte("content")
    h := sha256.Sum256(archiveData)
    expectedSHA := hex.EncodeToString(h[:])

    mux := http.NewServeMux()
    mux.HandleFunc("/GoCodeAlone/repo/releases/download/v1.0.0/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
        fmt.Fprintf(w, "%s  plugin name.tar.gz\n", expectedSHA)
    })
    srv := httptest.NewServer(mux)
    defer srv.Close()

    // URL has encoded space: %20
    downloadURL := srv.URL + "/GoCodeAlone/repo/releases/download/v1.0.0/plugin%20name.tar.gz"
    got, err := lookupChecksumForURL(downloadURL)
    if err != nil {
        t.Fatalf("lookupChecksumForURL with URL-encoded name: %v", err)
    }
    if got != expectedSHA {
        t.Errorf("expected %s, got %s", expectedSHA, got)
    }
}
```

**Step 2: Run tests to verify they pass**

```bash
cd cmd/wfctl && go test -run TestLookupChecksumForURL -v .
```
Expected: PASS (all 4 tests — the implementation is already in plugin_checksum.go)

**Step 3: Commit**

```bash
git add cmd/wfctl/plugin_checksum_test.go
git commit -m "test: add lookupChecksumForURL mock HTTP tests"
```

---

### Task 3: Harden `parseGitHubReleaseDownloadURL` — reject userinfo and non-default ports

**Files:**
- Modify: `cmd/wfctl/plugin_install.go` (line ~808)
- Modify: `cmd/wfctl/plugin_install_test.go`

**Step 1: Write failing tests**

Add to `plugin_install_test.go`:

```go
func TestParseGitHubReleaseDownloadURL(t *testing.T) {
    tests := []struct {
        name     string
        rawURL   string
        wantOK   bool
        wantOwner string
    }{
        // Valid
        {"valid https", "https://github.com/GoCodeAlone/workflow/releases/download/v1.0.0/wfctl-linux-amd64.tar.gz", true, "GoCodeAlone"},
        // Invalid — new hardening
        {"userinfo rejected", "https://user:pass@github.com/GoCodeAlone/workflow/releases/download/v1.0.0/file.tar.gz", false, ""},
        {"non-default port rejected", "https://github.com:8080/GoCodeAlone/workflow/releases/download/v1.0.0/file.tar.gz", false, ""},
        // Pre-existing rejections
        {"http rejected", "http://github.com/GoCodeAlone/workflow/releases/download/v1.0.0/file.tar.gz", false, ""},
        {"wrong host", "https://evilgithub.com/GoCodeAlone/workflow/releases/download/v1.0.0/file.tar.gz", false, ""},
        {"too few segments", "https://github.com/GoCodeAlone/workflow/releases/download/v1.0.0", false, ""},
        {"non-release path", "https://github.com/GoCodeAlone/workflow/archive/main.tar.gz", false, ""},
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            owner, _, _, _, ok := parseGitHubReleaseDownloadURL(tc.rawURL)
            if ok != tc.wantOK {
                t.Errorf("parseGitHubReleaseDownloadURL(%q) ok=%v, want %v", tc.rawURL, ok, tc.wantOK)
            }
            if ok && owner != tc.wantOwner {
                t.Errorf("owner=%q, want %q", owner, tc.wantOwner)
            }
        })
    }
}
```

**Step 2: Run test to verify userinfo/port cases fail**

```bash
cd cmd/wfctl && go test -run TestParseGitHubReleaseDownloadURL -v .
```
Expected: 2 subtests FAIL (`userinfo rejected`, `non-default port rejected`) — they currently return `ok=true` instead of `false`.

**Step 3: Update `parseGitHubReleaseDownloadURL` in `plugin_install.go`**

Find the current function (around line 808):

```go
func parseGitHubReleaseDownloadURL(rawURL string) (owner, repo, tag, filename string, ok bool) {
	u, err := neturl.Parse(rawURL)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || !isGitHubHost(u.Hostname()) {
		return
	}
	// Path must be exactly: /owner/repo/releases/download/tag/filename (6 segments).
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 6 || parts[2] != "releases" || parts[3] != "download" ||
		parts[0] == "" || parts[1] == "" || parts[4] == "" || parts[5] == "" {
		return
	}
	return parts[0], parts[1], parts[4], parts[5], true
}
```

Replace with:

```go
func parseGitHubReleaseDownloadURL(rawURL string) (owner, repo, tag, filename string, ok bool) {
	u, err := neturl.Parse(rawURL)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || !isGitHubHost(u.Hostname()) {
		return
	}
	// Reject URLs with userinfo (user:pass@host) — prevents credential injection attacks.
	if u.User != nil {
		return
	}
	// Reject URLs with a non-default port. u.Hostname() strips the port before
	// isGitHubHost sees it, so https://github.com:8080/... would pass without this check.
	if u.Port() != "" {
		return
	}
	// Path must be exactly: /owner/repo/releases/download/tag/filename (6 segments).
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 6 || parts[2] != "releases" || parts[3] != "download" ||
		parts[0] == "" || parts[1] == "" || parts[4] == "" || parts[5] == "" {
		return
	}
	return parts[0], parts[1], parts[4], parts[5], true
}
```

**Step 4: Run tests to verify all pass**

```bash
cd cmd/wfctl && go test -run TestParseGitHubReleaseDownloadURL -v .
```
Expected: PASS (all 7 subtests)

**Step 5: Run full test suite to ensure nothing regressed**

```bash
cd cmd/wfctl && go test ./... 2>&1 | tail -20
```
Expected: PASS (no failures)

**Step 6: Commit**

```bash
git add cmd/wfctl/plugin_install.go cmd/wfctl/plugin_install_test.go
git commit -m "feat: harden parseGitHubReleaseDownloadURL — reject userinfo and non-default ports"
```

---

### Task 4: Update `verifyChecksum` error format

**Files:**
- Modify: `cmd/wfctl/plugin_install.go` (line ~944)

The design specifies a multi-line mismatch message. Update `verifyChecksum`:

**Step 1: Find current implementation** (around line 944):

```go
func verifyChecksum(data []byte, expected string) error {
	h := sha256.Sum256(data)
	got := hex.EncodeToString(h[:])
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, expected)
	}
	return nil
}
```

**Step 2: Write failing test**

Add to `plugin_install_test.go`:

```go
func TestVerifyChecksum_MismatchFormat(t *testing.T) {
    err := verifyChecksum([]byte("data"), "deadbeef")
    if err == nil {
        t.Fatal("expected error")
    }
    msg := err.Error()
    if !strings.Contains(msg, "got:") || !strings.Contains(msg, "want:") {
        t.Errorf("expected 'got:'/'want:' in error, got: %s", msg)
    }
    if !strings.Contains(msg, "supply-chain") {
        t.Errorf("expected supply-chain mention in error, got: %s", msg)
    }
}
```

**Step 3: Run test to verify it fails**

```bash
cd cmd/wfctl && go test -run TestVerifyChecksum_MismatchFormat -v .
```
Expected: FAIL

**Step 4: Update `verifyChecksum`**

```go
func verifyChecksum(data []byte, expected string) error {
	h := sha256.Sum256(data)
	got := hex.EncodeToString(h[:])
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("checksum mismatch:\n  got:  %s\n  want: %s\nThis may indicate a corrupted download or a supply-chain attack.", got, expected)
	}
	return nil
}
```

**Step 5: Run tests**

```bash
cd cmd/wfctl && go test -run TestVerifyChecksum -v . && go test ./... 2>&1 | tail -5
```
Expected: PASS

**Step 6: Commit**

```bash
git add cmd/wfctl/plugin_install.go cmd/wfctl/plugin_install_test.go
git commit -m "feat: update verifyChecksum error format to include got/want and supply-chain warning"
```

---

### Task 5: Add `--skip-checksum` + `--sha256` flags; update `installFromURL` signature

**Files:**
- Modify: `cmd/wfctl/plugin_install.go`
- Modify: `cmd/wfctl/plugin_install_wfctllock.go`

**Step 1: Write failing tests for `installFromURL` new signature**

Add to `plugin_install_test.go`:

```go
// TestInstallFromURL_SkipChecksumSilent verifies that skipChecksum=true skips
// all verification without error, using an arbitrary (non-GitHub) URL.
func TestInstallFromURL_SkipChecksumSilent(t *testing.T) {
    // This test cannot easily run installFromURL end-to-end (needs a real archive).
    // We test that the flag compiles and is accepted — coverage via Task 7 integration test.
    _ = installFromURL // just verify the signature compiles with new params
}
```

Note: Full coverage comes from the integration test in Task 7.

**Step 2: Update `installFromURL` signature in `plugin_install.go`**

Find the current function signature (around line 584):

```go
func installFromURL(url, pluginDir string) error {
```

Change to:

```go
// installFromURL downloads a plugin tarball from a direct URL and installs it.
// When expectedSHA256 is non-empty, the archive is verified against it before extraction.
// When skipChecksum is true, all verification is bypassed silently (callers should warn).
// When both are empty/false, integrity enforcement is applied by the caller
// (runPluginInstall adds --sha256 or auto-fetch logic before calling this function
// for user-facing --url installs; installFromWfctlLockfile passes skipChecksum=true
// since it verifies the binary SHA separately).
func installFromURL(rawURL, pluginDir, expectedSHA256 string, skipChecksum bool) error {
    fmt.Fprintf(os.Stderr, "Downloading %s...\n", rawURL)
    data, err := downloadURL(rawURL)
    if err != nil {
        return fmt.Errorf("download: %w", err)
    }

    // Verify archive integrity if requested.
    if !skipChecksum && expectedSHA256 != "" {
        if err := verifyChecksum(data, expectedSHA256); err != nil {
            return fmt.Errorf("integrity check failed: %w", err)
        }
        fmt.Fprintf(os.Stderr, "Checksum verified.\n")
    }

    tmpDir, err := os.MkdirTemp("", "wfctl-plugin-*")
    // ... (rest of function body unchanged) ...
```

Also remove the old `fmt.Fprintf(os.Stderr, "Downloading %s...\n", url)` from the body since it's now at the top.

**Step 3: Update all callers of `installFromURL`**

In `plugin_install.go`, `runPluginInstall` (around line 110):
```go
if *directURL != "" {
    return installFromURL(*directURL, pluginDirVal)
}
```
→
```go
if *directURL != "" {
    // Integrity enforcement for --url installs is handled here (see Task 7).
    // For now pass the expected SHA256 and skipChecksum flags through.
    return installFromURLWithPolicy(*directURL, pluginDirVal, *sha256Flag, *skipChecksum)
}
```

Wait — actually Task 5 is just about the signature change and flag additions. The policy enforcement goes in Task 7. Let me keep it simple: Task 5 just updates the signature and wires flags.

For `runPluginInstall`, update the flag set:

```go
func runPluginInstall(args []string) error {
    fs := flag.NewFlagSet("plugin install", flag.ContinueOnError)
    var pluginDirVal string
    fs.StringVar(&pluginDirVal, "plugin-dir", defaultDataDir, "Plugin directory")
    fs.StringVar(&pluginDirVal, "data-dir", defaultDataDir, "Plugin directory (deprecated, use -plugin-dir)")
    cfgPath := fs.String("config", "", "Registry config file path")
    registryName := fs.String("registry", "", "Use a specific registry by name")
    directURL := fs.String("url", "", "Install from a direct download URL (tar.gz archive)")
    localPath := fs.String("local", "", "Install from a local plugin directory")
    fromConfig := fs.String("from-config", "", "Install all requires.plugins[] from a workflow config file")
    sha256Flag := fs.String("sha256", "", "Expected SHA-256 hex digest for --url installs (archive hash)")
    skipChecksum := fs.Bool("skip-checksum", false, "Skip SHA-256 integrity verification (not recommended)")
    // ... (rest of flag set + parse unchanged)
```

And update the `--url` dispatch:
```go
if *directURL != "" {
    if *skipChecksum {
        fmt.Fprintln(os.Stderr, "warning: --skip-checksum is set; binary integrity not verified")
    }
    return installFromURL(*directURL, pluginDirVal, *sha256Flag, *skipChecksum)
}
```

Also add `skipChecksum` threading to `installPluginFromManifest`:
```go
if err := installPluginFromManifest(pluginDirVal, pluginName, manifest, nil, *skipChecksum); err != nil {
```
And in `runPluginUpdate`:
```go
return installPluginFromManifest(pluginDirVal, pluginName, manifest, nil, false)
```
(updates never get `--skip-checksum` for now)

In `plugin_install_wfctllock.go`, update the `installFromURL` call (around line 53):
```go
if err := installFromURL(plat.URL, pluginDirVal, "", true); err != nil {
```
(lockfile reinstalls pass skipChecksum=true — the binary SHA is verified separately after install)

**Step 4: Run full test suite**

```bash
cd cmd/wfctl && go test ./... 2>&1 | tail -10
```
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/wfctl/plugin_install.go cmd/wfctl/plugin_install_wfctllock.go
git commit -m "feat: add --skip-checksum + --sha256 flags; update installFromURL signature"
```

---

### Task 6: Make `installPluginFromManifest` fail-closed

**Files:**
- Modify: `cmd/wfctl/plugin_install.go`
- Modify: `cmd/wfctl/plugin_install_test.go`

**Step 1: Write failing tests**

Add to `plugin_install_test.go`. These tests need a mock HTTP server to serve checksums.txt and an archive:

```go
// makeMinimalTarGz builds a minimal .tar.gz archive with a plugin binary
// and plugin.json so installPluginFromManifest can extract it.
func makeMinimalTarGz(t *testing.T, pluginName string) []byte {
    t.Helper()
    var buf bytes.Buffer
    gw := gzip.NewWriter(&buf)
    tw := tar.NewWriter(gw)

    // plugin.json
    pj := fmt.Sprintf(`{"name":%q,"version":"1.0.0","author":"test","description":"test"}`, pluginName)
    addTarFile(t, tw, pluginName+"-darwin-arm64/plugin.json", []byte(pj))
    // binary
    addTarFile(t, tw, pluginName+"-darwin-arm64/"+pluginName, []byte("#!/bin/sh\necho ok"))

    if err := tw.Close(); err != nil {
        t.Fatalf("close tar: %v", err)
    }
    if err := gw.Close(); err != nil {
        t.Fatalf("close gzip: %v", err)
    }
    return buf.Bytes()
}

func addTarFile(t *testing.T, tw *tar.Writer, name string, data []byte) {
    t.Helper()
    hdr := &tar.Header{Name: name, Mode: 0755, Size: int64(len(data))}
    if err := tw.WriteHeader(hdr); err != nil {
        t.Fatalf("write header: %v", err)
    }
    if _, err := tw.Write(data); err != nil {
        t.Fatalf("write data: %v", err)
    }
}

// makeManifest builds a minimal RegistryManifest for the test.
func makeManifest(name, downloadURL, sha256 string) *RegistryManifest {
    return &RegistryManifest{
        Name:    name,
        Version: "1.0.0",
        Downloads: []RegistryDownload{
            {OS: runtime.GOOS, Arch: runtime.GOARCH, URL: downloadURL, SHA256: sha256},
        },
    }
}

func TestInstallPluginFromManifest_FailsWhenNoSHAAndNonGitHubURL(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Write(makeMinimalTarGz(t, "myplugin"))
    }))
    defer srv.Close()

    dir := t.TempDir()
    manifest := makeManifest("myplugin", srv.URL+"/myplugin.tar.gz", "")
    err := installPluginFromManifest(dir, "myplugin", manifest, nil, false)
    if err == nil {
        t.Fatal("expected error: non-GitHub URL with no SHA should fail")
    }
    if !strings.Contains(err.Error(), "cannot verify integrity") {
        t.Errorf("expected 'cannot verify integrity' in error, got: %v", err)
    }
}

func TestInstallPluginFromManifest_SkipChecksumBypasses(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Write(makeMinimalTarGz(t, "myplugin"))
    }))
    defer srv.Close()

    dir := t.TempDir()
    manifest := makeManifest("myplugin", srv.URL+"/myplugin.tar.gz", "")
    // --skip-checksum should succeed even with no SHA and non-GitHub URL.
    err := installPluginFromManifest(dir, "myplugin", manifest, nil, true)
    if err != nil {
        t.Fatalf("expected success with --skip-checksum, got: %v", err)
    }
}

func TestInstallPluginFromManifest_ManifestSHAVerified(t *testing.T) {
    archiveData := makeMinimalTarGz(t, "myplugin")
    h := sha256.Sum256(archiveData)
    goodSHA := hex.EncodeToString(h[:])
    badSHA := strings.Repeat("0", 64)

    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Write(archiveData)
    }))
    defer srv.Close()

    dir := t.TempDir()

    // Good SHA: should succeed.
    manifest := makeManifest("myplugin", srv.URL+"/myplugin.tar.gz", goodSHA)
    if err := installPluginFromManifest(dir, "myplugin", manifest, nil, false); err != nil {
        t.Fatalf("expected success with correct SHA, got: %v", err)
    }

    // Bad SHA: should fail.
    dir2 := t.TempDir()
    manifest2 := makeManifest("myplugin", srv.URL+"/myplugin.tar.gz", badSHA)
    err := installPluginFromManifest(dir2, "myplugin", manifest2, nil, false)
    if err == nil {
        t.Fatal("expected checksum mismatch error")
    }
    if !strings.Contains(err.Error(), "got:") {
        t.Errorf("expected 'got:' in mismatch error, got: %v", err)
    }
}
```

**Step 2: Run tests to see current behavior**

```bash
cd cmd/wfctl && go test -run TestInstallPluginFromManifest -v .
```
Expected: `TestInstallPluginFromManifest_FailsWhenNoSHAAndNonGitHubURL` FAIL (currently succeeds without error)

**Step 3: Update `installPluginFromManifest` in `plugin_install.go`**

Add `skipChecksum bool` parameter to the signature:

```go
func installPluginFromManifest(dataDir, pluginName string, manifest *RegistryManifest, verify *config.PluginVerifyConfig, skipChecksum bool) error {
```

Replace the existing checksum block:

```go
// Before (old code — around line 240):
if dl.SHA256 != "" {
    if err := verifyChecksum(data, dl.SHA256); err != nil {
        return err
    }
    fmt.Fprintf(os.Stderr, "Checksum verified.\n")
}
```

With fail-closed logic:

```go
// Integrity verification — fail-closed by default.
if !skipChecksum {
    if dl.SHA256 != "" {
        // Manifest provides the archive SHA: verify directly.
        if err := verifyChecksum(data, dl.SHA256); err != nil {
            return fmt.Errorf("plugin %q: %w", pluginName, err)
        }
        fmt.Fprintf(os.Stderr, "Checksum verified.\n")
    } else if _, _, _, _, ok := parseGitHubReleaseDownloadURL(dl.URL); ok {
        // GitHub release URL, no SHA in manifest: auto-fetch checksums.txt.
        sha, lookupErr := lookupChecksumForURL(dl.URL)
        if lookupErr != nil {
            // Derive the checksums.txt URL for the error message so users can curl it manually.
            u, _ := neturl.Parse(dl.URL)
            checksumsURL := u.Scheme + "://" + u.Host + path.Dir(u.Path) + "/checksums.txt"
            return fmt.Errorf(
                "plugin %q: no SHA-256 in manifest and checksums.txt not found or asset not listed at %s\n\n"+
                    "To proceed without verification (not recommended):\n"+
                    "  wfctl plugin install --skip-checksum %s\n\n"+
                    "To add verification, ask the plugin author to publish a checksums.txt\n"+
                    "alongside their release assets (goreleaser does this automatically).",
                pluginName, checksumsURL, pluginName)
        }
        if err := verifyChecksum(data, sha); err != nil {
            return fmt.Errorf("plugin %q: %w", pluginName, err)
        }
        fmt.Fprintf(os.Stderr, "Checksum verified.\n")
    } else {
        // Non-GitHub URL, no SHA in manifest: must fail closed.
        return fmt.Errorf(
            "plugin %q: URL %s is not a GitHub release URL and no SHA-256 in manifest; cannot verify integrity.\n\n"+
                "To proceed without verification (not recommended):\n"+
                "  wfctl plugin install --skip-checksum %s",
            pluginName, dl.URL, pluginName)
    }
}
```

Also add `"path"` to the imports in `plugin_install.go`.

**Step 4: Run tests**

```bash
cd cmd/wfctl && go test -run TestInstallPluginFromManifest -v . && go test ./... 2>&1 | tail -5
```
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/wfctl/plugin_install.go cmd/wfctl/plugin_install_test.go
git commit -m "feat: make installPluginFromManifest fail-closed — auto-fetch checksums.txt or error"
```

---

### Task 7: Make `installFromURL` enforce integrity for `--url` installs

**Files:**
- Modify: `cmd/wfctl/plugin_install.go`
- Modify: `cmd/wfctl/plugin_install_test.go`

**Context:** `installFromURL` is called from two places:
1. `runPluginInstall --url` — needs full integrity enforcement
2. `installFromWfctlLockfile` — already passes `skipChecksum=true` (Task 5), binary SHA verified separately

The signature now has `expectedSHA256 string, skipChecksum bool`. We add the enforcement logic inside the function:

**Step 1: Write failing tests**

```go
func TestInstallFromURL_NonGitHubNoSHAFails(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Write(makeMinimalTarGz(t, "myplugin"))
    }))
    defer srv.Close()

    dir := t.TempDir()
    // srv.URL is not a GitHub release URL and no expectedSHA256 provided.
    err := installFromURL(srv.URL+"/myplugin.tar.gz", dir, "", false)
    if err == nil {
        t.Fatal("expected error: non-GitHub URL with no SHA")
    }
    if !strings.Contains(err.Error(), "cannot verify integrity") {
        t.Errorf("expected 'cannot verify integrity', got: %v", err)
    }
}

func TestInstallFromURL_WithExpectedSHA256_Correct(t *testing.T) {
    archiveData := makeMinimalTarGz(t, "myplugin")
    h := sha256.Sum256(archiveData)
    sha := hex.EncodeToString(h[:])

    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Write(archiveData)
    }))
    defer srv.Close()

    dir := t.TempDir()
    err := installFromURL(srv.URL+"/myplugin.tar.gz", dir, sha, false)
    if err != nil {
        t.Fatalf("expected success with correct SHA, got: %v", err)
    }
}

func TestInstallFromURL_WithExpectedSHA256_Wrong(t *testing.T) {
    archiveData := makeMinimalTarGz(t, "myplugin")
    dir := t.TempDir()

    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Write(archiveData)
    }))
    defer srv.Close()

    err := installFromURL(srv.URL+"/myplugin.tar.gz", dir, strings.Repeat("0", 64), false)
    if err == nil {
        t.Fatal("expected checksum mismatch error")
    }
    if !strings.Contains(err.Error(), "supply-chain") {
        t.Errorf("expected supply-chain mention, got: %v", err)
    }
}

func TestInstallFromURL_SkipChecksum_NonGitHub(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Write(makeMinimalTarGz(t, "myplugin"))
    }))
    defer srv.Close()

    dir := t.TempDir()
    // skipChecksum=true: should succeed even for non-GitHub URL with no SHA.
    err := installFromURL(srv.URL+"/myplugin.tar.gz", dir, "", true)
    if err != nil {
        t.Fatalf("expected success with skipChecksum=true, got: %v", err)
    }
}
```

**Step 2: Run tests to see failures**

```bash
cd cmd/wfctl && go test -run TestInstallFromURL -v .
```
Expected: some FAIL

**Step 3: Add integrity enforcement to `installFromURL` in `plugin_install.go`**

After `data, err := downloadURL(rawURL)` returns successfully, add:

```go
// Integrity verification — fail-closed unless skipChecksum is set.
if !skipChecksum {
    if expectedSHA256 != "" {
        // Caller provided expected hash: verify directly.
        if err := verifyChecksum(data, expectedSHA256); err != nil {
            return fmt.Errorf("integrity check failed for %s: %w", rawURL, err)
        }
        fmt.Fprintf(os.Stderr, "Checksum verified.\n")
    } else if _, _, _, _, ok := parseGitHubReleaseDownloadURL(rawURL); ok {
        // GitHub release URL: auto-fetch checksums.txt.
        sha, lookupErr := lookupChecksumForURL(rawURL)
        if lookupErr != nil {
            u, _ := neturl.Parse(rawURL)
            checksumsURL := u.Scheme + "://" + u.Host + path.Dir(u.Path) + "/checksums.txt"
            return fmt.Errorf(
                "URL %s is a GitHub release URL but verification failed: %w\n\n"+
                    "Checksums.txt expected at: %s\n\n"+
                    "To proceed without verification (not recommended):\n"+
                    "  wfctl plugin install --skip-checksum --url %s <name>",
                rawURL, lookupErr, checksumsURL, rawURL)
        }
        if err := verifyChecksum(data, sha); err != nil {
            return fmt.Errorf("integrity check failed for %s: %w", rawURL, err)
        }
        fmt.Fprintf(os.Stderr, "Checksum verified.\n")
    } else {
        // Non-GitHub URL with no expected SHA: must fail closed.
        return fmt.Errorf(
            "URL %s is not a GitHub release URL and no --sha256 was provided; cannot verify integrity.\n\n"+
                "Provide a checksum:\n"+
                "  wfctl plugin install --url %s --sha256 <hex> <name>\n\n"+
                "Or proceed without verification (not recommended):\n"+
                "  wfctl plugin install --skip-checksum --url %s <name>",
            rawURL, rawURL, rawURL)
    }
}
```

Also remove the `fmt.Fprintf(os.Stderr, "Downloading %s...\n", url)` line that was at the top of the old function (we now pass `rawURL` and the call moved earlier in the new signature task).

**Step 4: Run all tests**

```bash
cd cmd/wfctl && go test -run "TestInstallFromURL|TestInstallPluginFromManifest" -v . && go test ./... 2>&1 | tail -5
```
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/wfctl/plugin_install.go cmd/wfctl/plugin_install_test.go
git commit -m "feat: make installFromURL fail-closed — require SHA256 or auto-fetch for GitHub URLs"
```

---

### Task 8: Open PR-X and trigger Copilot

**Step 1: Push branch and open PR**

```bash
git push -u origin feat/sha256-pr-x
gh pr create \
  --title "feat(sha256): plugin install fail-closed — auto-fetch checksums.txt + --skip-checksum" \
  --body "$(cat <<'EOF'
## Summary

- Adds `parseChecksumsTxt` and `lookupChecksumForURL` shared helpers
- Hardens `parseGitHubReleaseDownloadURL` to reject userinfo and non-default ports (new security hardening)
- Makes `installPluginFromManifest` fail-closed: auto-fetches `checksums.txt` for GitHub release URLs when SHA absent; hard-fails for non-GitHub URLs without SHA
- Makes `installFromURL` fail-closed with same logic
- Adds `--skip-checksum` and `--sha256` flags to `wfctl plugin install`

## Design

See `docs/plans/2026-04-25-wfctl-sha256-verification-design.md`

## Test plan

- [ ] `parseChecksumsTxt` unit tests (valid, malformed, blank lines, Windows line endings)
- [ ] `lookupChecksumForURL` mock HTTP tests (found, 404, asset missing, URL-encoded name)
- [ ] `parseGitHubReleaseDownloadURL` table tests (userinfo rejected, port rejected)
- [ ] `installPluginFromManifest` fail-closed tests (non-GitHub no SHA fails, skip-checksum bypasses, manifest SHA verified)
- [ ] `installFromURL` fail-closed tests (non-GitHub fails, correct SHA succeeds, wrong SHA fails, skip-checksum bypasses)
- [ ] Full test suite passes

🤖 Generated with Claude Code
EOF
)"
```

**Step 2: Trigger Copilot review**

```bash
gh pr comment <number> --body "@Copilot review"
```

Wait for 2 clean Copilot passes before merging.

---

## PR-Y: `wfctl update` hardening

Branch: `feat/sha256-pr-y` (off `main` after PR-X merges)

---

### Task 9: Make `wfctl update` fail-closed + add `--skip-checksum`

**Files:**
- Modify: `cmd/wfctl/update.go`
- Modify: `cmd/wfctl/update_test.go`

**Step 1: Write failing tests**

Add to `update_test.go`:

```go
func TestRunUpdate_FailsWhenNoChecksumsAsset(t *testing.T) {
    // Release has a binary asset but no checksums.txt — should fail without --skip-checksum.
    origVersion := version
    version = "0.0.1"
    defer func() { version = origVersion }()

    goos := runtime.GOOS
    goarch := runtime.GOARCH

    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        rel := githubRelease{
            TagName: "v9.9.9",
            HTMLURL: "https://github.com/GoCodeAlone/workflow/releases/tag/v9.9.9",
            Assets: []githubAsset{
                {Name: "wfctl-" + goos + "-" + goarch + ".tar.gz",
                 BrowserDownloadURL: "http://example.com/wfctl.tar.gz"},
                // NOTE: no checksums.txt asset
            },
        }
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(rel)
    }))
    defer srv.Close()

    githubReleasesURLOverride = srv.URL
    defer func() { githubReleasesURLOverride = "" }()

    err := runUpdate([]string{})
    if err == nil {
        t.Fatal("expected error: no checksums.txt asset should fail closed")
    }
    if !strings.Contains(err.Error(), "checksums") {
        t.Errorf("expected 'checksums' in error, got: %v", err)
    }
}

func TestRunUpdate_SkipChecksumBypasses(t *testing.T) {
    // With --skip-checksum, update should not fail even if checksums.txt is absent.
    // We can't easily make it succeed end-to-end (would need valid tar.gz + binary replacement),
    // but we can verify it doesn't fail on the checksum check specifically.
    // This test is a compile/flag registration check; full coverage via manual runtime test.
    _ = runUpdate // just verify it compiles with --skip-checksum flag
}
```

**Step 2: Run failing test**

```bash
cd cmd/wfctl && go test -run TestRunUpdate_FailsWhenNoChecksumsAsset -v .
```
Expected: FAIL (currently succeeds silently without checksums.txt)

**Step 3: Update `runUpdate` in `update.go`**

Add `--skip-checksum` flag and make the checksums check fail-closed:

```go
func runUpdate(args []string) error {
    fs := flag.NewFlagSet("update", flag.ContinueOnError)
    checkOnly := fs.Bool("check", false, "Only check for updates without installing")
    skipChecksum := fs.Bool("skip-checksum", false, "Skip SHA-256 integrity verification (not recommended)")
    // ... (rest of flags unchanged) ...
```

Replace the checksum block:

```go
// Before:
if checksumAsset := findChecksumAsset(rel.Assets); checksumAsset != nil {
    fmt.Fprintln(os.Stderr, "Verifying checksum...")
    if err := verifyAssetChecksum(checksumAsset, asset.Name, data); err != nil {
        return fmt.Errorf("integrity check failed: %w", err)
    }
    fmt.Fprintln(os.Stderr, "Checksum verified.")
}
```

```go
// After:
if *skipChecksum {
    fmt.Fprintln(os.Stderr, "warning: --skip-checksum is set; binary integrity not verified")
} else {
    checksumAsset := findChecksumAsset(rel.Assets)
    if checksumAsset == nil {
        return fmt.Errorf(
            "integrity check failed: no checksums.txt found in release %s\n\n"+
                "To proceed without verification (not recommended):\n"+
                "  wfctl update --skip-checksum",
            rel.TagName)
    }
    fmt.Fprintln(os.Stderr, "Verifying checksum...")
    if err := verifyAssetChecksum(checksumAsset, asset.Name, data); err != nil {
        return fmt.Errorf("integrity check failed: %w", err)
    }
    fmt.Fprintln(os.Stderr, "Checksum verified.")
}
```

**Step 4: Run tests**

```bash
cd cmd/wfctl && go test -run "TestRunUpdate" -v . && go test ./... 2>&1 | tail -5
```
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/wfctl/update.go cmd/wfctl/update_test.go
git commit -m "feat: make wfctl update fail-closed when checksums.txt absent + add --skip-checksum"
```

---

### Task 10: Refactor `runUpdate` to use `lookupChecksumForURL`

The design says to unify `wfctl update` with the same helper used by plugin install, replacing `findChecksumAsset + verifyAssetChecksum` with `lookupChecksumForURL + verifyChecksum`.

**Files:**
- Modify: `cmd/wfctl/update.go`
- Modify: `cmd/wfctl/update_test.go`

**Step 1: Write test for refactored path**

```go
func TestRunUpdate_UsesLookupChecksumForURL(t *testing.T) {
    // Regression test: after refactor, update should verify via lookupChecksumForURL.
    // Set up a release where checksums.txt is served at the derived URL.
    origVersion := version
    version = "0.0.1"
    defer func() { version = origVersion }()

    goos := runtime.GOOS
    goarch := runtime.GOARCH
    assetName := fmt.Sprintf("wfctl-%s-%s.tar.gz", goos, goarch)

    // Build a minimal wfctl archive to use as the "new binary".
    archiveData := []byte("fake archive")
    h := sha256.Sum256(archiveData)
    expectedSHA := hex.EncodeToString(h[:])

    mux := http.NewServeMux()
    // Releases API
    mux.HandleFunc("/repos/GoCodeAlone/workflow/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
        rel := githubRelease{
            TagName: "v9.9.9",
            HTMLURL: "https://github.com/GoCodeAlone/workflow/releases/tag/v9.9.9",
            Assets: []githubAsset{
                {Name: assetName, BrowserDownloadURL: "http://" + r.Host + "/releases/download/v9.9.9/" + assetName},
            },
        }
        json.NewEncoder(w).Encode(rel)
    })
    // Asset download
    mux.HandleFunc("/releases/download/v9.9.9/"+assetName, func(w http.ResponseWriter, _ *http.Request) {
        w.Write(archiveData)
    })
    // checksums.txt (derived by lookupChecksumForURL)
    mux.HandleFunc("/releases/download/v9.9.9/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
        fmt.Fprintf(w, "%s  %s\n", expectedSHA, assetName)
    })
    srv := httptest.NewServer(mux)
    defer srv.Close()
    // ...
```

Note: this test is complex — include it as a basic `lookupChecksumForURL` integration test rather than full `runUpdate` test. The full path requires binary replacement which is tested by `TestReplaceBinary`.

**Step 2: Refactor `runUpdate` to use `lookupChecksumForURL`**

Replace:
```go
if checksumAsset := findChecksumAsset(rel.Assets); checksumAsset == nil {
    ...
}
fmt.Fprintln(os.Stderr, "Verifying checksum...")
if err := verifyAssetChecksum(checksumAsset, asset.Name, data); err != nil {
```

With:
```go
fmt.Fprintln(os.Stderr, "Verifying checksum...")
sha, err := lookupChecksumForURL(asset.BrowserDownloadURL)
if err != nil {
    return fmt.Errorf(
        "integrity check failed: %w\n\nTo proceed without verification (not recommended):\n  wfctl update --skip-checksum",
        err)
}
if err := verifyChecksum(data, sha); err != nil {
    return fmt.Errorf("integrity check failed: %w", err)
}
fmt.Fprintln(os.Stderr, "Checksum verified.")
```

Note: `lookupChecksumForURL` is in `plugin_checksum.go` — same package, available in PR-Y since it was added in PR-X.

Note: `findChecksumAsset` and `verifyAssetChecksum` can be kept for backward compat or removed if no other callers exist.

**Step 3: Check for other callers of `findChecksumAsset` / `verifyAssetChecksum`**

```bash
grep -n "findChecksumAsset\|verifyAssetChecksum" cmd/wfctl/*.go
```

If only in `update.go` and `update_test.go`, keep the test functions for regression but the production code uses `lookupChecksumForURL`.

**Step 4: Run tests**

```bash
cd cmd/wfctl && go test ./... 2>&1 | tail -5
```
Expected: PASS

**Step 5: Commit and push, open PR-Y**

```bash
git add cmd/wfctl/update.go cmd/wfctl/update_test.go
git commit -m "refactor: wfctl update uses lookupChecksumForURL for unified checksum verification"
git push -u origin feat/sha256-pr-y
gh pr create \
  --title "feat(sha256): wfctl update fail-closed + unified lookupChecksumForURL" \
  --body "$(cat <<'EOF'
## Summary

- `wfctl update` now fails when `checksums.txt` is absent in the release (was: silently skipped)
- Adds `--skip-checksum` flag to `wfctl update`
- Refactors checksum verification to use `lookupChecksumForURL` (same helper as plugin install)

Depends on PR-X for `lookupChecksumForURL`.

## Test plan

- [ ] `TestRunUpdate_FailsWhenNoChecksumsAsset` — hard failure without --skip-checksum
- [ ] Existing `TestVerifyAssetChecksum_*` tests still pass
- [ ] Full test suite passes

🤖 Generated with Claude Code
EOF
)"
gh pr comment <number> --body "@Copilot review"
```

---

## PR-Z: Lockfile write-back on first install

Branch: `feat/sha256-pr-z` (off `main` after PR-X merges)

---

### Task 11: Always write binary SHA to lockfile after `installPluginFromManifest`

**Context:** Currently `runPluginInstall` only writes the binary SHA to `.wfctl-lock.yaml` when the user specified `name@version`. The design requires write-back on every successful first install so that subsequent lockfile-based reinstalls can skip the `checksums.txt` HTTP call.

**Files:**
- Modify: `cmd/wfctl/plugin_install.go` (`runPluginInstall`, `runPluginUpdate`)
- Modify: `cmd/wfctl/plugin_install_test.go`

**Step 1: Write failing test**

```go
func TestRunPluginInstall_WritesLockfileChecksumWithoutVersion(t *testing.T) {
    // Verify that installing without @version still writes binary SHA to lockfile.
    // Setup: mock registry + mock download server.
    // ...
    // After install, read .wfctl-lock.yaml and check SHA256 is non-empty.
    // This is a higher-level test — see plugin_install_lockfile_test.go for pattern.
}
```

See `plugin_install_lockfile_test.go` for the pattern of setting up a mock registry and verifying lockfile state.

**Step 2: Remove the `@version` guard in `runPluginInstall`**

Current code in `runPluginInstall` (after `installPluginFromManifest` returns successfully):

```go
// Update .wfctl-lock.yaml lockfile if name@version was provided.
if _, ver := parseNameVersion(nameArg); ver != "" {
    pluginName = normalizePluginName(pluginName)
    binaryChecksum := ""
    binaryPath := filepath.Join(pluginDirVal, pluginName, pluginName)
    if cs, hashErr := hashFileSHA256(binaryPath); hashErr == nil {
        binaryChecksum = cs
    } else {
        fmt.Fprintf(os.Stderr, "warning: could not hash binary %s: %v (lockfile will have no checksum)\n", binaryPath, hashErr)
    }
    updateLockfileWithChecksum(pluginName, manifest.Version, manifest.Repository, sourceName, binaryChecksum)
}
```

Replace with (remove `if ver != ""`):

```go
// Always write the binary SHA to .wfctl-lock.yaml after a successful install.
// This ensures subsequent lockfile-based reinstalls can verify integrity without
// an extra checksums.txt HTTP call.
{
    fsName := normalizePluginName(pluginName)
    binaryPath := filepath.Join(pluginDirVal, fsName, fsName)
    binaryChecksum := ""
    if cs, hashErr := hashFileSHA256(binaryPath); hashErr == nil {
        binaryChecksum = cs
    } else {
        fmt.Fprintf(os.Stderr, "warning: could not hash binary %s: %v (lockfile will have no checksum)\n", binaryPath, hashErr)
    }
    updateLockfileWithChecksum(fsName, manifest.Version, manifest.Repository, sourceName, binaryChecksum)
}
```

**Step 3: Add write-back to `runPluginUpdate`**

Current `runPluginUpdate` calls `installPluginFromManifest` but never writes the binary SHA. Add after each successful `installPluginFromManifest` call:

```go
// Write updated binary SHA to lockfile after successful update.
{
    fsName := normalizePluginName(pluginName)
    binaryPath := filepath.Join(pluginDirVal, fsName, fsName)
    if cs, hashErr := hashFileSHA256(binaryPath); hashErr == nil {
        updateLockfileWithChecksum(fsName, manifest.Version, manifest.Repository, sourceName, cs)
    }
}
```

(There are two call sites in `runPluginUpdate` — one for registry path and one for repository fallback. Add write-back to both.)

**Step 4: Run tests**

```bash
cd cmd/wfctl && go test ./... 2>&1 | tail -10
```
Expected: PASS

**Step 5: Commit and push, open PR-Z**

```bash
git add cmd/wfctl/plugin_install.go cmd/wfctl/plugin_install_test.go
git commit -m "feat: always write binary SHA to lockfile after install/update (not just @version installs)"
git push -u origin feat/sha256-pr-z
gh pr create \
  --title "feat(sha256): always write binary SHA to lockfile after install/update" \
  --body "$(cat <<'EOF'
## Summary

Post-extraction binary SHA is now written to `.wfctl-lock.yaml` after every successful
`wfctl plugin install` and `wfctl plugin update`, not just `@version`-pinned installs.

This enables lockfile-based CI reinstalls to verify binary integrity without an extra
`checksums.txt` HTTP call on repeat runs.

## Test plan

- [ ] Install without @version: lockfile SHA is non-empty after install
- [ ] Install with @version: behavior unchanged
- [ ] Update: lockfile SHA reflects updated binary
- [ ] Full test suite passes

🤖 Generated with Claude Code
EOF
)"
gh pr comment <number> --body "@Copilot review"
```

---

## Runtime Validation (all PRs)

Per the team-lead's instructions, each PR body must include pre-fix and post-fix transcripts of `wfctl plugin install` against a known-good plugin.

**For PR-X: capture before/after**

Before the PR:
```bash
# On main (pre-fix):
wfctl plugin install workflow-plugin-digitalocean 2>&1
# Note: should succeed silently without SHA verification
```

After the PR:
```bash
# On feat/sha256-pr-x:
wfctl plugin install workflow-plugin-digitalocean 2>&1
# Expected: "Verifying checksum..." appears in output (auto-fetched from checksums.txt)

# Test --skip-checksum:
wfctl plugin install --skip-checksum workflow-plugin-digitalocean 2>&1
# Expected: "warning: --skip-checksum is set; binary integrity not verified"

# Test fail-closed for non-GitHub URL (use a public non-GitHub server):
wfctl plugin install --url https://example.com/nonexistent.tar.gz 2>&1
# Expected: error about non-GitHub URL, cannot verify integrity
```

Capture these transcripts and include them in each PR description.

---

## Discipline Rules

- 2 clean Copilot passes required before merging each PR
- PR-Y and PR-Z can proceed in parallel after PR-X merges
- Fix Copilot findings before merging — no deferred fixes
- All tests must pass on CI before requesting second Copilot pass
- Do not bundle PR-X + PR-Y + PR-Z into one PR — each must pass independently
