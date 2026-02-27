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
