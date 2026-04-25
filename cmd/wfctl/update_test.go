package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestFindReleaseAsset_Found(t *testing.T) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	assets := []githubAsset{
		{Name: "wfctl-" + goos + "-" + goarch, BrowserDownloadURL: "http://example.com/wfctl"},
		{Name: "wfctl-plan9-mips", BrowserDownloadURL: "http://example.com/other"},
	}

	asset, err := findReleaseAsset(assets)
	if err != nil {
		t.Fatalf("findReleaseAsset: %v", err)
	}
	if asset == nil {
		t.Fatal("expected asset, got nil")
	}
	if asset.Name != "wfctl-"+goos+"-"+goarch {
		t.Errorf("unexpected asset name: %s", asset.Name)
	}
}

func TestFindReleaseAsset_NotFound(t *testing.T) {
	assets := []githubAsset{
		{Name: "wfctl-plan9-mips", BrowserDownloadURL: "http://example.com/x"},
	}
	_, err := findReleaseAsset(assets)
	if err == nil {
		t.Fatal("expected error for unsupported platform")
	}
}

func TestReplaceBinary(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "wfctl-test")
	if err := os.WriteFile(target, []byte("old"), 0755); err != nil { //nolint:gosec
		t.Fatal(err)
	}
	if err := replaceBinary(target, []byte("new")); err != nil {
		t.Fatalf("replaceBinary: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("expected 'new', got %q", got)
	}
}

func TestRunUpdate_CheckOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rel := githubRelease{
			TagName: "v9.9.9",
			HTMLURL: "https://github.com/GoCodeAlone/workflow/releases/tag/v9.9.9",
			Assets:  []githubAsset{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rel)
	}))
	defer srv.Close()

	githubReleasesURLOverride = srv.URL
	defer func() { githubReleasesURLOverride = "" }()

	// Should not error even when no asset matches (--check only reports version).
	err := runUpdate([]string{"--check"})
	if err != nil {
		t.Fatalf("runUpdate --check: %v", err)
	}
}

func TestRunUpdate_AlreadyLatest(t *testing.T) {
	origVersion := version
	version = "1.2.3"
	defer func() { version = origVersion }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rel := githubRelease{
			TagName: "v1.2.3",
			HTMLURL: "https://example.com",
			Assets:  []githubAsset{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rel)
	}))
	defer srv.Close()

	githubReleasesURLOverride = srv.URL
	defer func() { githubReleasesURLOverride = "" }()

	if err := runUpdate([]string{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunUpdate_GitHubAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	githubReleasesURLOverride = srv.URL
	defer func() { githubReleasesURLOverride = "" }()

	err := runUpdate([]string{})
	if err == nil {
		t.Fatal("expected error on API failure")
	}
}

func TestCheckForUpdateNotice_SkipsDevBuild(t *testing.T) {
	origVersion := version
	version = "dev"
	defer func() { version = origVersion }()
	// Should close the done channel immediately without making any network requests.
	done := checkForUpdateNotice()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected done channel to be closed immediately for dev build")
	}
}

func TestCheckForUpdateNotice_RespectsEnvVar(t *testing.T) {
	t.Setenv(envNoUpdateCheck, "1")
	// Should close the done channel immediately without any network call.
	done := checkForUpdateNotice()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected done channel to be closed immediately when update check disabled")
	}
}

func TestFetchLatestRelease_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rel := githubRelease{
			TagName: "v1.0.0",
			HTMLURL: "https://github.com/GoCodeAlone/workflow/releases/tag/v1.0.0",
			Assets: []githubAsset{
				{Name: "wfctl-linux-amd64", BrowserDownloadURL: "http://example.com/wfctl"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rel)
	}))
	defer srv.Close()

	githubReleasesURLOverride = srv.URL
	defer func() { githubReleasesURLOverride = "" }()

	rel, err := fetchLatestRelease()
	if err != nil {
		t.Fatalf("fetchLatestRelease: %v", err)
	}
	if rel.TagName != "v1.0.0" {
		t.Errorf("expected v1.0.0, got %s", rel.TagName)
	}
	if len(rel.Assets) != 1 {
		t.Errorf("expected 1 asset, got %d", len(rel.Assets))
	}
}

func TestFetchLatestRelease_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	githubReleasesURLOverride = srv.URL
	defer func() { githubReleasesURLOverride = "" }()

	_, err := fetchLatestRelease()
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
}

func TestFindChecksumAsset(t *testing.T) {
	assets := []githubAsset{
		{Name: "wfctl-linux-amd64", BrowserDownloadURL: "http://example.com/bin"},
		{Name: "checksums.txt", BrowserDownloadURL: "http://example.com/checksums.txt"},
	}
	got := findChecksumAsset(assets)
	if got == nil {
		t.Fatal("expected to find checksums.txt asset")
	}
	if got.Name != "checksums.txt" {
		t.Errorf("unexpected name: %s", got.Name)
	}
}

func TestFindChecksumAsset_NotFound(t *testing.T) {
	assets := []githubAsset{
		{Name: "wfctl-linux-amd64", BrowserDownloadURL: "http://example.com/bin"},
	}
	if got := findChecksumAsset(assets); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestVerifyAssetChecksum_Valid(t *testing.T) {
	data := []byte("fake binary content")
	h := sha256.Sum256(data)
	hash := hex.EncodeToString(h[:])
	checksumsContent := fmt.Sprintf("%s  wfctl-linux-amd64\n", hash)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(checksumsContent))
	}))
	defer srv.Close()

	checksumAsset := &githubAsset{Name: "checksums.txt", BrowserDownloadURL: srv.URL}
	if err := verifyAssetChecksum(checksumAsset, "wfctl-linux-amd64", data); err != nil {
		t.Fatalf("verifyAssetChecksum: %v", err)
	}
}

func TestVerifyAssetChecksum_Mismatch(t *testing.T) {
	data := []byte("fake binary content")
	checksumsContent := "deadbeef  wfctl-linux-amd64\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(checksumsContent))
	}))
	defer srv.Close()

	checksumAsset := &githubAsset{Name: "checksums.txt", BrowserDownloadURL: srv.URL}
	err := verifyAssetChecksum(checksumAsset, "wfctl-linux-amd64", data)
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
}

func TestVerifyAssetChecksum_Missing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("abc123  other-asset\n"))
	}))
	defer srv.Close()

	checksumAsset := &githubAsset{Name: "checksums.txt", BrowserDownloadURL: srv.URL}
	err := verifyAssetChecksum(checksumAsset, "wfctl-linux-amd64", []byte("data"))
	if err == nil {
		t.Fatal("expected error when asset not in checksums.txt")
	}
}

// TestVerifyAssetChecksum_SingleSpaceSeparatorRejected verifies that verifyAssetChecksum
// uses the strict goreleaser two-space format (via parseChecksumsTxt). A checksums.txt
// with a single-space separator should be rejected as malformed — not silently accepted
// by a whitespace-splitting parser.
func TestVerifyAssetChecksum_SingleSpaceSeparatorRejected(t *testing.T) {
	data := []byte("fake binary content")
	h := sha256.Sum256(data)
	hash := hex.EncodeToString(h[:])
	// Single space between hash and filename — NOT goreleaser format.
	checksumsContent := fmt.Sprintf("%s wfctl-linux-amd64\n", hash)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(checksumsContent))
	}))
	defer srv.Close()

	checksumAsset := &githubAsset{Name: "checksums.txt", BrowserDownloadURL: srv.URL}
	err := verifyAssetChecksum(checksumAsset, "wfctl-linux-amd64", data)
	if err == nil {
		t.Fatal("expected error: single-space separator should be rejected as malformed")
	}
}

func TestDownloadWithTimeout_Success(t *testing.T) {
	body := []byte("hello world")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	got, err := downloadWithTimeout(srv.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("downloadWithTimeout: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("expected %q, got %q", body, got)
	}
}

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		latest  string
		current string
		want    bool
	}{
		// Newer available
		{"v0.3.43", "v0.3.42", true},
		{"0.3.43", "0.3.42", true},
		{"v1.0.0", "v0.9.9", true},
		// Same version
		{"v0.3.42", "v0.3.42", false},
		// Older version reported as "latest" (the bug scenario)
		{"v0.3.41", "v0.3.42", false},
		{"v0.2.0", "v1.0.0", false},
		// Invalid semver
		{"not-a-version", "v1.0.0", false},
		{"v1.0.0", "not-a-version", false},
		{"", "v1.0.0", false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("latest=%s current=%s", tt.latest, tt.current), func(t *testing.T) {
			got := isNewerVersion(tt.latest, tt.current)
			if got != tt.want {
				t.Errorf("isNewerVersion(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
			}
		})
	}
}

func TestCheckForUpdateNotice_OlderReleaseSuppressed(t *testing.T) {
	// Regression test: when running a newer version than the latest GitHub release,
	// no update notice should be printed.
	origVersion := version
	version = "v0.3.42"
	defer func() { version = origVersion }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rel := githubRelease{
			TagName: "v0.3.41", // older than current
			HTMLURL: "https://github.com/GoCodeAlone/workflow/releases/tag/v0.3.41",
			Assets:  []githubAsset{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rel)
	}))
	defer srv.Close()

	githubReleasesURLOverride = srv.URL
	defer func() { githubReleasesURLOverride = "" }()

	// Capture stderr to ensure no update notice is printed.
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = origStderr
		r.Close()
	})

	done := checkForUpdateNotice()
	<-done

	w.Close()
	var buf [512]byte
	n, _ := r.Read(buf[:])

	output := string(buf[:n])
	if output != "" {
		t.Errorf("expected no update notice for older release, got: %q", output)
	}
}

func TestRunUpdate_CheckOnly_OlderRelease(t *testing.T) {
	// When current version is newer than the GitHub release, --check should
	// report "up to date" rather than showing a spurious update notice.
	origVersion := version
	version = "v0.3.42"
	defer func() { version = origVersion }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rel := githubRelease{
			TagName: "v0.3.41",
			HTMLURL: "https://example.com",
			Assets:  []githubAsset{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rel)
	}))
	defer srv.Close()

	githubReleasesURLOverride = srv.URL
	defer func() { githubReleasesURLOverride = "" }()

	if err := runUpdate([]string{"--check"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunUpdate_OlderRelease_NoDownload(t *testing.T) {
	// When the current version is newer, runUpdate should not attempt to download.
	origVersion := version
	version = "v0.3.42"
	defer func() { version = origVersion }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rel := githubRelease{
			TagName: "v0.3.41",
			HTMLURL: "https://example.com",
			Assets:  []githubAsset{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rel)
	}))
	defer srv.Close()

	githubReleasesURLOverride = srv.URL
	defer func() { githubReleasesURLOverride = "" }()

	if err := runUpdate([]string{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDownloadWithTimeout_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusGone)
	}))
	defer srv.Close()

	_, err := downloadWithTimeout(srv.URL, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
}

func TestReplaceBinary_PreservesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits not meaningful on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "wfctl-test")
	// Write with a distinct mode.
	if err := os.WriteFile(target, []byte("old"), 0750); err != nil { //nolint:gosec
		t.Fatal(err)
	}
	if err := replaceBinary(target, []byte("new")); err != nil {
		t.Fatalf("replaceBinary: %v", err)
	}
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0750 {
		t.Errorf("expected mode 0750, got %o", fi.Mode().Perm())
	}
}
