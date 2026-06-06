package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureTransport is a test http.RoundTripper that:
//   - captures the Authorization header from each request (race-safe via mutex)
//   - rewrites the request host to a target test server so real network calls
//     are never made, even when the URL hostname is "github.com"
type captureTransport struct {
	mu     sync.Mutex
	header string
	target string // host:port of the httptest.Server
}

func (ct *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ct.mu.Lock()
	ct.header = req.Header.Get("Authorization")
	ct.mu.Unlock()
	// Clone and redirect to test server.
	r2 := req.Clone(req.Context())
	r2.URL.Scheme = "http"
	r2.URL.Host = ct.target
	return http.DefaultTransport.RoundTrip(r2)
}

func (ct *captureTransport) gotHeader() string {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.header
}

// TestPluginListAcceptsPluginDirFlag verifies that -plugin-dir is accepted by
// runPluginList and correctly used as the directory to scan.
func TestPluginListAcceptsPluginDirFlag(t *testing.T) {
	dir := t.TempDir()

	// Create a fake installed plugin directory with a minimal plugin.json.
	pluginDir := filepath.Join(dir, "myplugin")
	if err := os.MkdirAll(pluginDir, 0750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := `{"name":"myplugin","version":"1.0.0","author":"test","description":"test plugin"}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0640); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}

	// Should succeed using -plugin-dir.
	if err := runPluginList([]string{"-plugin-dir", dir}); err != nil {
		t.Errorf("-plugin-dir: runPluginList returned unexpected error: %v", err)
	}
}

// TestParseGitHubPluginRef verifies that parseGitHubRef correctly identifies GitHub refs.
func TestParseGitHubPluginRef(t *testing.T) {
	tests := []struct {
		input   string
		owner   string
		repo    string
		version string
		isGH    bool
	}{
		{"GoCodeAlone/workflow-plugin-authz@v0.3.1", "GoCodeAlone", "workflow-plugin-authz", "v0.3.1", true},
		{"GoCodeAlone/workflow-plugin-authz", "GoCodeAlone", "workflow-plugin-authz", "", true},
		{"authz", "", "", "", false},
		{"workflow-plugin-authz", "", "", "", false},
		{"owner/repo@v1.0.0", "owner", "repo", "v1.0.0", true},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			owner, repo, version, isGH := parseGitHubRef(tc.input)
			if owner != tc.owner || repo != tc.repo || version != tc.version || isGH != tc.isGH {
				t.Errorf("parseGitHubRef(%q) = (%q, %q, %q, %v), want (%q, %q, %q, %v)",
					tc.input, owner, repo, version, isGH,
					tc.owner, tc.repo, tc.version, tc.isGH)
			}
		})
	}
}

// TestPluginListAcceptsLegacyDataDirFlag verifies that the deprecated -data-dir flag
// still works as an alias for -plugin-dir.
func TestPluginListAcceptsLegacyDataDirFlag(t *testing.T) {
	dir := t.TempDir()

	// Should succeed using -data-dir (deprecated alias).
	if err := runPluginList([]string{"-data-dir", dir}); err != nil {
		t.Errorf("-data-dir: runPluginList returned unexpected error: %v", err)
	}
}

// installTestClient replaces http.DefaultClient with one using transport ct and
// restores the original on test cleanup.
func installTestClient(t *testing.T, ct *captureTransport) {
	t.Helper()
	orig := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: ct}
	t.Cleanup(func() { http.DefaultClient = orig })
}

func TestDownloadURL_DirectGetUsesBoundedRequestContext(t *testing.T) {
	orig := http.DefaultClient
	http.DefaultClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if _, ok := req.Context().Deadline(); !ok {
				return nil, fmt.Errorf("request has no deadline")
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("ok")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}
	t.Cleanup(func() { http.DefaultClient = orig })

	var got []byte
	_, err := captureStderr(t, func() error {
		var err error
		got, err = downloadURL("https://example.com/plugin.tar.gz")
		return err
	})
	if err != nil {
		t.Fatalf("downloadURL: %v", err)
	}
	if string(got) != "ok" {
		t.Fatalf("downloadURL body = %q, want ok", got)
	}
}

func TestDownloadURL_LargeDirectDownloadEmitsProgress(t *testing.T) {
	orig := http.DefaultClient
	http.DefaultClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				ContentLength: int64(len("fake tarball bytes")),
				Body:          io.NopCloser(strings.NewReader("fake tarball bytes")),
				Header:        make(http.Header),
				Request:       req,
			}, nil
		}),
	}
	t.Cleanup(func() { http.DefaultClient = orig })

	stderr, err := captureStderr(t, func() error {
		_, err := downloadURL("https://example.com/plugin.tar.gz")
		return err
	})
	if err != nil {
		t.Fatalf("downloadURL: %v", err)
	}
	if !strings.Contains(stderr, "Download progress") || !strings.Contains(stderr, "Download complete") {
		t.Fatalf("stderr = %q, want progress and completion indicators", stderr)
	}
}

// TestDownloadURL_GitHubAuthHeader verifies that downloadURL injects a Bearer
// Authorization header for non-release github.com URLs (direct-download path)
// using the first non-empty token env var (RELEASES_TOKEN > GH_TOKEN >
// GITHUB_TOKEN), and sends no header when none are set.
func TestDownloadURL_GitHubAuthHeader(t *testing.T) {
	const sentinel = "test-token-value"

	cases := []struct {
		name    string
		envKey  string
		wantHdr string
	}{
		{"RELEASES_TOKEN", "RELEASES_TOKEN", "Bearer " + sentinel},
		{"GH_TOKEN", "GH_TOKEN", "Bearer " + sentinel},
		{"GITHUB_TOKEN", "GITHUB_TOKEN", "Bearer " + sentinel},
		{"no token", "", ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			srvURL, _ := url.Parse(srv.URL)
			ct := &captureTransport{target: srvURL.Host}
			installTestClient(t, ct)

			// Clear all token env vars, then set the one under test.
			for _, k := range []string{"RELEASES_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
				t.Setenv(k, "")
			}
			if tc.envKey != "" {
				t.Setenv(tc.envKey, sentinel)
			}

			// Use a non-release github.com URL so we exercise the direct-download
			// path (not the two-step API flow). captureTransport redirects it to srv.
			testURL := "https://github.com/GoCodeAlone/plugin/archive/main.tar.gz"
			if _, err := downloadURL(testURL); err != nil {
				t.Fatalf("downloadURL: %v", err)
			}
			if got := ct.gotHeader(); got != tc.wantHdr {
				t.Errorf("Authorization header = %q, want %q", got, tc.wantHdr)
			}
		})
	}
}

// TestDownloadURL_NonGitHubNoAuthHeader verifies that downloadURL does NOT inject
// an Authorization header for non-github.com URLs, even when a token env var is set.
// Also verifies that a URL with "github.com" only in the path (not the hostname)
// does not trigger injection.
func TestDownloadURL_NonGitHubNoAuthHeader(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"plain non-github host", ""},   // filled in per-test using srv.URL
		{"github.com in path only", ""}, // filled in per-test
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	srvURL, _ := url.Parse(srv.URL)
	cases[0].url = srv.URL + "/some/asset.tar.gz"
	cases[1].url = srv.URL + "/path/github.com/owner/repo/asset.tar.gz"

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ct := &captureTransport{target: srvURL.Host}
			installTestClient(t, ct)
			t.Setenv("RELEASES_TOKEN", "should-not-appear")

			if _, err := downloadURL(tc.url); err != nil {
				t.Fatalf("downloadURL: %v", err)
			}
			if got := ct.gotHeader(); got != "" {
				t.Errorf("expected no Authorization header, got %q", got)
			}
		})
	}
}

// TestDownloadURL_TokenPriority verifies that RELEASES_TOKEN takes precedence over
// GH_TOKEN and GITHUB_TOKEN when multiple env vars are set (direct-download path).
func TestDownloadURL_TokenPriority(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	srvURL, _ := url.Parse(srv.URL)
	ct := &captureTransport{target: srvURL.Host}
	installTestClient(t, ct)

	t.Setenv("RELEASES_TOKEN", "releases-wins")
	t.Setenv("GH_TOKEN", "gh-loses")
	t.Setenv("GITHUB_TOKEN", "github-loses")

	// Non-release github.com URL exercises the direct-download path.
	testURL := "https://github.com/GoCodeAlone/plugin/archive/main.tar.gz"
	if _, err := downloadURL(testURL); err != nil {
		t.Fatalf("downloadURL: %v", err)
	}
	const want = "Bearer releases-wins"
	if got := ct.gotHeader(); got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
}

// TestParseGitHubReleaseDownloadURL verifies URL parsing for the GitHub release
// download pattern.
func TestParseGitHubReleaseDownloadURL(t *testing.T) {
	cases := []struct {
		rawURL   string
		owner    string
		repo     string
		tag      string
		filename string
		ok       bool
	}{
		{
			rawURL:   "https://github.com/GoCodeAlone/workflow-plugin-supply-chain/releases/download/v0.3.0/workflow-plugin-supply-chain-linux-amd64.tar.gz",
			owner:    "GoCodeAlone",
			repo:     "workflow-plugin-supply-chain",
			tag:      "v0.3.0",
			filename: "workflow-plugin-supply-chain-linux-amd64.tar.gz",
			ok:       true,
		},
		{
			// Non-release URL
			rawURL: "https://github.com/GoCodeAlone/plugin/archive/main.tar.gz",
			ok:     false,
		},
		{
			// Non-GitHub host
			rawURL: "https://example.com/owner/repo/releases/download/v1.0.0/file.tar.gz",
			ok:     false,
		},
		{
			// Suffix-matching but not github.com — must be rejected to prevent token leakage
			rawURL: "https://evilgithub.com/owner/repo/releases/download/v1.0.0/file.tar.gz",
			ok:     false,
		},
		{
			// http scheme — must be rejected (only https allowed)
			rawURL: "http://github.com/owner/repo/releases/download/v1.0.0/file.tar.gz",
			ok:     false,
		},
		{
			// Too few path segments
			rawURL: "https://github.com/owner/repo/releases/download/v1.0.0",
			ok:     false,
		},
		{
			// Extra path segments (len > 6) — must be rejected for exact match
			rawURL: "https://github.com/owner/repo/releases/download/v1.0.0/file.tar.gz/extra",
			ok:     false,
		},
		{
			// Userinfo present — rejected to prevent credential injection attacks
			rawURL: "https://user:pass@github.com/owner/repo/releases/download/v1.0.0/file.tar.gz",
			ok:     false,
		},
		{
			// Non-default port — u.Hostname() strips port before isGitHubHost check,
			// so explicit rejection via u.Port() != "" is required.
			rawURL: "https://github.com:8080/owner/repo/releases/download/v1.0.0/file.tar.gz",
			ok:     false,
		},
		{
			// Any explicit port is rejected — including the HTTPS default :443.
			// Explicit port = likely proxy or redirect; reject unconditionally.
			rawURL: "https://github.com:443/owner/repo/releases/download/v1.0.0/file.tar.gz",
			ok:     false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.rawURL, func(t *testing.T) {
			owner, repo, tag, filename, ok := parseGitHubReleaseDownloadURL(tc.rawURL)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !tc.ok {
				return
			}
			if owner != tc.owner || repo != tc.repo || tag != tc.tag || filename != tc.filename {
				t.Errorf("got (%q, %q, %q, %q), want (%q, %q, %q, %q)",
					owner, repo, tag, filename,
					tc.owner, tc.repo, tc.tag, tc.filename)
			}
		})
	}
}

// TestDownloadURL_PrivateReleaseAsset verifies the two-step GitHub API flow used
// to download assets from private repos. A mock server handles both the
// releases/tags and releases/assets endpoints.
func TestDownloadURL_PrivateReleaseAsset(t *testing.T) {
	const (
		wantAssetID  = int64(99)
		wantFilename = "plugin-linux-amd64.tar.gz"
		wantTag      = "v1.0.0"
		wantOwner    = "GoCodeAlone"
		wantRepo     = "test-plugin"
		wantToken    = "test-secret-token"
	)
	wantPayload := []byte("fake tarball bytes")

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/releases/tags/%s", wantOwner, wantRepo, wantTag),
		func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+wantToken {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if r.Header.Get("Accept") != "application/vnd.github+json" {
				http.Error(w, "bad accept", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"assets":[{"id":%d,"name":%q}]}`, wantAssetID, wantFilename)
		},
	)
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/releases/assets/%d", wantOwner, wantRepo, wantAssetID),
		func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+wantToken {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if r.Header.Get("Accept") != "application/octet-stream" {
				http.Error(w, "bad accept", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write(wantPayload) //nolint:errcheck
		},
	)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Override API base URL and client to point at the mock server.
	origAPIBase := gitHubAPIBaseURL
	origAPIClient := gitHubAPIClient
	gitHubAPIBaseURL = srv.URL
	gitHubAPIClient = srv.Client()
	t.Cleanup(func() {
		gitHubAPIBaseURL = origAPIBase
		gitHubAPIClient = origAPIClient
	})

	t.Setenv("RELEASES_TOKEN", wantToken)
	for _, k := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}

	rawURL := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s",
		wantOwner, wantRepo, wantTag, wantFilename)
	got, err := downloadURL(rawURL)
	if err != nil {
		t.Fatalf("downloadURL: %v", err)
	}
	if string(got) != string(wantPayload) {
		t.Errorf("payload = %q, want %q", got, wantPayload)
	}
}

func TestDownloadURL_PrivateReleaseAssetUsesFreshAssetDownloadDeadline(t *testing.T) {
	const (
		wantAssetID  = int64(99)
		wantFilename = "plugin-linux-amd64.tar.gz"
		wantTag      = "v1.0.0"
		wantOwner    = "GoCodeAlone"
		wantRepo     = "test-plugin"
		wantToken    = "test-secret-token"
	)

	var metadataDeadline, assetDeadline time.Time
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		deadline, ok := req.Context().Deadline()
		if !ok {
			return nil, fmt.Errorf("%s has no request deadline", req.URL.Path)
		}

		switch req.URL.Path {
		case fmt.Sprintf("/repos/%s/%s/releases/tags/%s", wantOwner, wantRepo, wantTag):
			metadataDeadline = deadline
			time.Sleep(10 * time.Millisecond)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					fmt.Sprintf(`{"assets":[{"id":%d,"name":%q}]}`, wantAssetID, wantFilename),
				)),
				Header:  make(http.Header),
				Request: req,
			}, nil
		case fmt.Sprintf("/repos/%s/%s/releases/assets/%d", wantOwner, wantRepo, wantAssetID):
			assetDeadline = deadline
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("fake tarball bytes")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		default:
			return nil, fmt.Errorf("unexpected path %s", req.URL.Path)
		}
	})

	origAPIBase := gitHubAPIBaseURL
	origAPIClient := gitHubAPIClient
	gitHubAPIBaseURL = "https://api.github.test"
	gitHubAPIClient = &http.Client{Transport: rt}
	t.Cleanup(func() {
		gitHubAPIBaseURL = origAPIBase
		gitHubAPIClient = origAPIClient
	})

	t.Setenv("RELEASES_TOKEN", wantToken)
	for _, k := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}

	rawURL := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s",
		wantOwner, wantRepo, wantTag, wantFilename)
	if _, err := downloadURL(rawURL); err != nil {
		t.Fatalf("downloadURL: %v", err)
	}
	if metadataDeadline.IsZero() || assetDeadline.IsZero() {
		t.Fatalf("missing recorded deadlines: metadata=%v asset=%v", metadataDeadline, assetDeadline)
	}
	if !assetDeadline.After(metadataDeadline) {
		t.Fatalf("asset deadline = %v, want after metadata deadline %v", assetDeadline, metadataDeadline)
	}
}

// TestDownloadURL_PublicReleaseNoToken verifies that when no token is set,
// downloadURL falls back to a plain GET for release download URLs (public repos).
func TestDownloadURL_PublicReleaseNoToken(t *testing.T) {
	wantPayload := []byte("public tarball bytes")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(wantPayload) //nolint:errcheck
	}))
	defer srv.Close()

	// No token — release URL goes through direct-download path.
	for _, k := range []string{"RELEASES_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}

	srvURL, _ := url.Parse(srv.URL)
	ct := &captureTransport{target: srvURL.Host}
	installTestClient(t, ct)

	rawURL := "https://github.com/GoCodeAlone/public-plugin/releases/download/v1.0.0/plugin.tar.gz"
	got, err := downloadURL(rawURL)
	if err != nil {
		t.Fatalf("downloadURL: %v", err)
	}
	if string(got) != string(wantPayload) {
		t.Errorf("payload = %q, want %q", got, wantPayload)
	}
	// No auth header should be sent when there is no token.
	if hdr := ct.gotHeader(); hdr != "" {
		t.Errorf("expected no Authorization header with no token, got %q", hdr)
	}
}

// ---- test helpers for archive-based install tests ----

// makeTestTarGz builds a minimal .tar.gz with a plugin binary + plugin.json so
// installPluginFromManifest / installFromURL can successfully extract and install it.
func makeTestTarGz(t *testing.T, pluginName string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	topDir := pluginName + "-" + runtime.GOOS + "-" + runtime.GOARCH
	pjContent := fmt.Sprintf(`{"name":%q,"version":"1.0.0","author":"test","description":"test plugin"}`, pluginName)
	addTestTarFile(t, tw, topDir+"/plugin.json", 0640, []byte(pjContent))
	addTestTarFile(t, tw, topDir+"/"+pluginName, 0750, []byte("#!/bin/sh\necho ok\n"))

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func addTestTarFile(t *testing.T, tw *tar.Writer, name string, mode int64, data []byte) {
	t.Helper()
	hdr := &tar.Header{Name: name, Mode: mode, Size: int64(len(data)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("write tar header for %s: %v", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatalf("write tar data for %s: %v", name, err)
	}
}

// makeTestManifest returns a RegistryManifest for the current GOOS/GOARCH.
func makeTestManifest(name, downloadURL, sha256sum string) *RegistryManifest {
	return &RegistryManifest{
		Name:        name,
		Version:     "1.0.0",
		Author:      "test",
		Description: "test plugin",
		Type:        "external",
		Tier:        "community",
		License:     "MIT",
		Downloads: []PluginDownload{
			{OS: runtime.GOOS, Arch: runtime.GOARCH, URL: downloadURL, SHA256: sha256sum},
		},
	}
}

// makeTestTarGzVersioned builds a minimal .tar.gz using the GoReleaser platform-
// suffix naming convention (e.g. myplugin-linux-amd64/myplugin-linux-amd64) so
// ensurePluginBinary must rename the binary. version and binaryContent allow
// callers to distinguish upgrades from fresh installs.
func makeTestTarGzVersioned(t *testing.T, pluginName, version string, binaryContent []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Use the GoReleaser platform-suffix convention so ensurePluginBinary must
	// rename the file (rather than finding it already correctly named).
	topDir := pluginName + "-" + runtime.GOOS + "-" + runtime.GOARCH
	binaryName := pluginName + "-" + runtime.GOOS + "-" + runtime.GOARCH
	pjContent := fmt.Sprintf(`{"name":%q,"version":%q,"author":"test","description":"test plugin"}`, pluginName, version)
	addTestTarFile(t, tw, topDir+"/plugin.json", 0640, []byte(pjContent))
	addTestTarFile(t, tw, topDir+"/"+binaryName, 0750, binaryContent)

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

// makeTestManifestVersioned is like makeTestManifest but accepts an explicit version.
func makeTestManifestVersioned(name, version, downloadURL, sha256sum string) *RegistryManifest {
	m := makeTestManifest(name, downloadURL, sha256sum)
	m.Version = version
	return m
}

// ---- stale-binary upgrade tests (issue: wfctl plugin install reports upgraded
// plugin but leaves stale executable) ----

// TestInstallPluginFromManifest_UpgradeReplacesStaleBinary verifies that
// upgrading a plugin via registry manifest atomically replaces the previous
// installation. The old binary (from a GoReleaser platform-suffix tarball) must
// be gone after the upgrade; only the new binary should exist.
func TestInstallPluginFromManifest_UpgradeReplacesStaleBinary(t *testing.T) {
	const pluginName = "myplugin"

	oldBinary := []byte("#!/bin/sh\necho v1.0.6\n")
	newBinary := []byte("#!/bin/sh\necho v1.0.8\n")

	oldTar := makeTestTarGzVersioned(t, pluginName, "1.0.6", oldBinary)
	newTar := makeTestTarGzVersioned(t, pluginName, "1.0.8", newBinary)

	var serveData []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(serveData)
	}))
	defer srv.Close()

	pluginDir := t.TempDir()

	// First install (v1.0.6).
	serveData = oldTar
	oldSum := sha256sum(oldTar)
	if err := installPluginFromManifest(pluginDir, pluginName,
		makeTestManifestVersioned(pluginName, "1.0.6", srv.URL+"/p.tar.gz", oldSum), nil, false); err != nil {
		t.Fatalf("first install: %v", err)
	}
	binaryPath := filepath.Join(pluginDir, pluginName, pluginName)
	gotOld, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read v1 binary: %v", err)
	}
	if !bytes.Equal(gotOld, oldBinary) {
		t.Fatalf("after v1 install, binary = %q, want %q", gotOld, oldBinary)
	}

	// Upgrade to v1.0.8 — the new tarball uses the GoReleaser suffix name.
	serveData = newTar
	newSum := sha256sum(newTar)
	if err := installPluginFromManifest(pluginDir, pluginName,
		makeTestManifestVersioned(pluginName, "1.0.8", srv.URL+"/p.tar.gz", newSum), nil, false); err != nil {
		t.Fatalf("upgrade install: %v", err)
	}

	gotNew, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read v2 binary: %v", err)
	}
	if bytes.Equal(gotNew, oldBinary) {
		t.Fatal("binary still contains old content after upgrade — stale binary bug")
	}
	if !bytes.Equal(gotNew, newBinary) {
		t.Fatalf("after upgrade, binary = %q, want %q", gotNew, newBinary)
	}

	// Installed version in plugin.json must match v1.0.8.
	if got := readInstalledVersion(filepath.Join(pluginDir, pluginName)); got != "1.0.8" {
		t.Errorf("installed version = %q, want 1.0.8", got)
	}

	// The old platform-suffix binary must NOT be present (no stale files).
	staleName := pluginName + "-" + runtime.GOOS + "-" + runtime.GOARCH
	if _, err := os.Stat(filepath.Join(pluginDir, pluginName, staleName)); !os.IsNotExist(err) {
		t.Errorf("stale GoReleaser-named binary %q still present after upgrade", staleName)
	}
}

// TestInstallFromURL_UpgradeReplacesStaleBinary verifies the same invariant for
// the --url install path.
func TestInstallFromURL_UpgradeReplacesStaleBinary(t *testing.T) {
	const pluginName = "urlplugin"

	// Change cwd to a temp dir so updateLockfileWithChecksum writes
	// .wfctl-lock.yaml there instead of the package checkout.
	t.Chdir(t.TempDir())

	oldBinary := []byte("#!/bin/sh\necho old-url\n")
	newBinary := []byte("#!/bin/sh\necho new-url\n")

	oldTar := makeTestTarGzVersioned(t, pluginName, "2.0.0", oldBinary)
	newTar := makeTestTarGzVersioned(t, pluginName, "3.0.0", newBinary)

	var serveData []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(serveData)
	}))
	defer srv.Close()

	pluginDir := t.TempDir()

	// First install.
	serveData = oldTar
	if err := installFromURL(srv.URL+"/p.tar.gz", pluginDir, sha256sum(oldTar), false); err != nil {
		t.Fatalf("first install: %v", err)
	}
	binaryPath := filepath.Join(pluginDir, pluginName, pluginName)
	gotOld, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read v1 binary: %v", err)
	}
	if !bytes.Equal(gotOld, oldBinary) {
		t.Fatalf("after first install, binary = %q, want %q", gotOld, oldBinary)
	}

	// Upgrade.
	serveData = newTar
	if err := installFromURL(srv.URL+"/p.tar.gz", pluginDir, sha256sum(newTar), false); err != nil {
		t.Fatalf("upgrade install: %v", err)
	}
	gotNew, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read v2 binary: %v", err)
	}
	if bytes.Equal(gotNew, oldBinary) {
		t.Fatal("binary still contains old content after URL upgrade — stale binary bug")
	}
	if !bytes.Equal(gotNew, newBinary) {
		t.Fatalf("after upgrade, binary = %q, want %q", gotNew, newBinary)
	}
}

// TestInstallFromLocal_UpgradeReplacesStaleBinary verifies the same invariant
// for the --local install path.
func TestInstallFromLocal_UpgradeReplacesStaleBinary(t *testing.T) {
	const pluginName = "localplugin"

	// Change cwd to a temp dir so updateLockfileWithChecksum writes
	// .wfctl-lock.yaml there instead of the package checkout.
	t.Chdir(t.TempDir())

	pluginDir := t.TempDir()

	makeLocalSrc := func(version string, binaryContent []byte) string {
		src := t.TempDir()
		pj := fmt.Sprintf(`{"name":%q,"version":%q,"author":"t","description":"t"}`, pluginName, version)
		if err := os.WriteFile(filepath.Join(src, "plugin.json"), []byte(pj), 0640); err != nil {
			t.Fatalf("write plugin.json: %v", err)
		}
		if err := os.WriteFile(filepath.Join(src, pluginName), binaryContent, 0750); err != nil {
			t.Fatalf("write binary: %v", err)
		}
		return src
	}

	oldBinary := []byte("#!/bin/sh\necho local-v1\n")
	newBinary := []byte("#!/bin/sh\necho local-v2\n")

	// First install.
	if err := installFromLocal(makeLocalSrc("1.0.0", oldBinary), pluginDir); err != nil {
		t.Fatalf("first install: %v", err)
	}
	binaryPath := filepath.Join(pluginDir, pluginName, pluginName)
	gotOld, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read v1 binary: %v", err)
	}
	if !bytes.Equal(gotOld, oldBinary) {
		t.Fatalf("after first install, binary = %q, want %q", gotOld, oldBinary)
	}

	// Upgrade.
	if err := installFromLocal(makeLocalSrc("2.0.0", newBinary), pluginDir); err != nil {
		t.Fatalf("upgrade install: %v", err)
	}
	gotNew, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read v2 binary: %v", err)
	}
	if bytes.Equal(gotNew, oldBinary) {
		t.Fatal("binary still contains old content after local upgrade — stale binary bug")
	}
	if !bytes.Equal(gotNew, newBinary) {
		t.Fatalf("after upgrade, binary = %q, want %q", gotNew, newBinary)
	}
}

// TestInstallPluginFromManifest_StagingCleanedUpOnFailure verifies that if the
// install fails (e.g. download error) the staging directory is removed and the
// original installation is left intact.
func TestInstallPluginFromManifest_StagingCleanedUpOnFailure(t *testing.T) {
	const pluginName = "myplugin"

	oldBinary := []byte("#!/bin/sh\necho stable\n")
	oldTar := makeTestTarGz(t, pluginName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/old.tar.gz":
			_, _ = w.Write(oldTar)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	pluginDir := t.TempDir()

	// Install stable version first.
	oldSum := sha256sum(oldTar)
	if err := installPluginFromManifest(pluginDir, pluginName,
		makeTestManifest(pluginName, srv.URL+"/old.tar.gz", oldSum), nil, false); err != nil {
		t.Fatalf("first install: %v", err)
	}
	// Overwrite binary with sentinel so we can verify it's preserved.
	binaryPath := filepath.Join(pluginDir, pluginName, pluginName)
	if err := os.WriteFile(binaryPath, oldBinary, 0750); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	// Attempt upgrade with a failing URL.
	badManifest := makeTestManifest(pluginName, srv.URL+"/nonexistent.tar.gz", strings.Repeat("0", 64))
	if err := installPluginFromManifest(pluginDir, pluginName, badManifest, nil, false); err == nil {
		t.Fatal("expected upgrade with non-existent URL to fail, but it succeeded")
	}

	// Staging directory must be cleaned up.
	stagingDir := filepath.Join(pluginDir, pluginName+".installing")
	if _, err := os.Stat(stagingDir); !os.IsNotExist(err) {
		t.Errorf("staging dir %q was not cleaned up after failed install", stagingDir)
	}

	// Original installation must be intact.
	gotBinary, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("original binary missing after failed upgrade: %v", err)
	}
	if !bytes.Equal(gotBinary, oldBinary) {
		t.Errorf("original binary modified by failed upgrade: got %q, want %q", gotBinary, oldBinary)
	}
}

// sha256sum is a test helper that returns the hex-encoded SHA256 of data.
func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func TestVerifyChecksum_MismatchFormat(t *testing.T) {
	err := verifyChecksum([]byte("data"), strings.Repeat("0", 64))
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "got:") || !strings.Contains(msg, "want:") {
		t.Errorf("expected 'got:'/'want:' in error, got: %s", msg)
	}
	if !strings.Contains(msg, "supply-chain") {
		t.Errorf("expected supply-chain mention in error, got: %s", msg)
	}
}

func TestVerifyChecksum_Match(t *testing.T) {
	data := []byte("hello")
	h := sha256.Sum256(data)
	if err := verifyChecksum(data, hex.EncodeToString(h[:])); err != nil {
		t.Fatalf("expected no error for correct checksum: %v", err)
	}
}

// ---- installPluginFromManifest fail-closed tests ----

func TestInstallPluginFromManifest_FailsNonGitHubNoSHA(t *testing.T) {
	archiveData := makeTestTarGz(t, "myplugin")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archiveData)
	}))
	defer srv.Close()

	dir := t.TempDir()
	manifest := makeTestManifest("myplugin", srv.URL+"/myplugin.tar.gz", "")
	err := installPluginFromManifest(dir, "myplugin", manifest, nil, false)
	if err == nil {
		t.Fatal("expected error: non-GitHub URL with no SHA should fail closed")
	}
	if !strings.Contains(err.Error(), "cannot verify integrity") {
		t.Errorf("expected 'cannot verify integrity' in error, got: %v", err)
	}
}

func TestInstallPluginFromManifest_SkipChecksumBypasses(t *testing.T) {
	archiveData := makeTestTarGz(t, "myplugin")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archiveData)
	}))
	defer srv.Close()

	dir := t.TempDir()
	manifest := makeTestManifest("myplugin", srv.URL+"/myplugin.tar.gz", "")
	if err := installPluginFromManifest(dir, "myplugin", manifest, nil, true); err != nil {
		t.Fatalf("expected success with skipChecksum=true, got: %v", err)
	}
}

// TestInstallPluginFromManifest_SkipChecksumBypassesManifestSHA verifies that
// skipChecksum=true is a full bypass: even when the manifest provides a SHA256,
// verification is skipped (the wrong hash must not cause a failure).
func TestInstallPluginFromManifest_SkipChecksumBypassesManifestSHA(t *testing.T) {
	archiveData := makeTestTarGz(t, "myplugin")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archiveData)
	}))
	defer srv.Close()

	// Wrong SHA — would fail without skipChecksum, must succeed with it.
	dir := t.TempDir()
	manifest := makeTestManifest("myplugin", srv.URL+"/myplugin.tar.gz", strings.Repeat("0", 64))
	if err := installPluginFromManifest(dir, "myplugin", manifest, nil, true); err != nil {
		t.Fatalf("skipChecksum=true should bypass manifest SHA verification, got: %v", err)
	}
}

func TestInstallPluginFromManifest_ManifestSHAVerified(t *testing.T) {
	archiveData := makeTestTarGz(t, "myplugin")
	h := sha256.Sum256(archiveData)
	goodSHA := hex.EncodeToString(h[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archiveData)
	}))
	defer srv.Close()

	// Correct SHA: must succeed.
	dir1 := t.TempDir()
	if err := installPluginFromManifest(dir1, "myplugin", makeTestManifest("myplugin", srv.URL+"/myplugin.tar.gz", goodSHA), nil, false); err != nil {
		t.Fatalf("expected success with correct SHA, got: %v", err)
	}

	// Wrong SHA: must fail with got/want format.
	dir2 := t.TempDir()
	err := installPluginFromManifest(dir2, "myplugin", makeTestManifest("myplugin", srv.URL+"/myplugin.tar.gz", strings.Repeat("0", 64)), nil, false)
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if !strings.Contains(err.Error(), "got:") {
		t.Errorf("expected 'got:' in mismatch error, got: %v", err)
	}
}

// ---- installFromURL fail-closed tests ----

func TestInstallFromURL_NonGitHubNoSHAFails(t *testing.T) {
	archiveData := makeTestTarGz(t, "myplugin")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archiveData)
	}))
	defer srv.Close()

	dir := t.TempDir()
	err := installFromURL(srv.URL+"/myplugin.tar.gz", dir, "", false)
	if err == nil {
		t.Fatal("expected error: non-GitHub URL with no SHA should fail closed")
	}
	if !strings.Contains(err.Error(), "cannot verify integrity") {
		t.Errorf("expected 'cannot verify integrity' in error, got: %v", err)
	}
}

func TestInstallFromURL_WithExpectedSHA256_Correct(t *testing.T) {
	// Change cwd to a temp dir so updateLockfileWithChecksum writes
	// .wfctl-lock.yaml there instead of the package checkout.
	t.Chdir(t.TempDir())

	archiveData := makeTestTarGz(t, "myplugin")
	h := sha256.Sum256(archiveData)
	sha := hex.EncodeToString(h[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archiveData)
	}))
	defer srv.Close()

	dir := t.TempDir()
	if err := installFromURL(srv.URL+"/myplugin.tar.gz", dir, sha, false); err != nil {
		t.Fatalf("expected success with correct SHA, got: %v", err)
	}
}

func TestInstallFromURL_WithExpectedSHA256_Wrong(t *testing.T) {
	archiveData := makeTestTarGz(t, "myplugin")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archiveData)
	}))
	defer srv.Close()

	dir := t.TempDir()
	err := installFromURL(srv.URL+"/myplugin.tar.gz", dir, strings.Repeat("0", 64), false)
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if !strings.Contains(err.Error(), "supply-chain") {
		t.Errorf("expected supply-chain mention in error, got: %v", err)
	}
}

func TestInstallFromURL_SkipChecksum_NonGitHub(t *testing.T) {
	// Change cwd to a temp dir so updateLockfileWithChecksum writes
	// .wfctl-lock.yaml there instead of the package checkout.
	t.Chdir(t.TempDir())

	archiveData := makeTestTarGz(t, "myplugin")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archiveData)
	}))
	defer srv.Close()

	dir := t.TempDir()
	if err := installFromURL(srv.URL+"/myplugin.tar.gz", dir, "", true); err != nil {
		t.Fatalf("expected success with skipChecksum=true, got: %v", err)
	}
}

// ---- flag validation tests (Finding 1) ----

func TestRunPluginInstall_SHA256WithoutURL_Errors(t *testing.T) {
	err := runPluginInstall([]string{"--sha256", strings.Repeat("a", 64)})
	if err == nil {
		t.Fatal("expected error: --sha256 without --url should be rejected")
	}
	if !strings.Contains(err.Error(), "--sha256") || !strings.Contains(err.Error(), "--url") {
		t.Errorf("expected error to mention --sha256 and --url, got: %v", err)
	}
}

func TestRunPluginInstall_SkipChecksumAndSHA256Contradiction_Errors(t *testing.T) {
	archiveData := makeTestTarGz(t, "myplugin")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archiveData)
	}))
	defer srv.Close()

	err := runPluginInstall([]string{
		"--url", srv.URL + "/myplugin.tar.gz",
		"--sha256", strings.Repeat("a", 64),
		"--skip-checksum",
	})
	if err == nil {
		t.Fatal("expected error: --skip-checksum and --sha256 are contradictory")
	}
	if !strings.Contains(err.Error(), "--skip-checksum") || !strings.Contains(err.Error(), "--sha256") {
		t.Errorf("expected error to mention both flags, got: %v", err)
	}
}

func TestRunPluginInstallCompatSkipsNewerKnownFail(t *testing.T) {
	reg := newCompatInstallRegistry(t, "test", "v0.2.0", []compatInstallVersion{
		{Version: "v0.1.0", Status: PluginCompatibilityStatusPass},
		{Version: "v0.2.0", Status: PluginCompatibilityStatusFail},
	})
	pluginDir := t.TempDir()
	if err := runPluginInstall([]string{
		"--config", reg.ConfigPath,
		"--plugin-dir", pluginDir,
		"--engine-version", "v0.51.2",
		"test",
	}); err != nil {
		t.Fatalf("runPluginInstall: %v", err)
	}
	if got := readInstalledVersion(filepath.Join(pluginDir, "test")); got != "v0.1.0" {
		t.Fatalf("installed version = %q, want v0.1.0", got)
	}
}

func TestRunPluginInstallHonorsTrailingPluginDirFlag(t *testing.T) {
	reg := newCompatInstallRegistry(t, "test", "v0.2.0", []compatInstallVersion{
		{Version: "v0.2.0", Status: PluginCompatibilityStatusPass},
	})
	pluginDir := t.TempDir()
	if err := runPluginInstall([]string{
		"test",
		"--config", reg.ConfigPath,
		"--plugin-dir", pluginDir,
		"--engine-version", "v0.51.2",
	}); err != nil {
		t.Fatalf("runPluginInstall: %v", err)
	}
	if got := readInstalledVersion(filepath.Join(pluginDir, "test")); got != "v0.2.0" {
		t.Fatalf("installed version in trailing --plugin-dir = %q, want v0.2.0", got)
	}
}

func TestRunPluginInstallTrailingFlagMissingValueErrors(t *testing.T) {
	err := runPluginInstall([]string{"test", "--config"})
	if err == nil {
		t.Fatal("expected missing trailing --config value to error")
	}
	if !strings.Contains(err.Error(), "flag needs an argument") || !strings.Contains(err.Error(), "config") {
		t.Fatalf("error = %v, want missing --config value", err)
	}
}

func TestInterspersedPluginInstallArgsReordersSupportedForms(t *testing.T) {
	fs := flag.NewFlagSet("plugin install", flag.ContinueOnError)
	fs.String("config", "", "")
	fs.String("plugin-dir", "", "")
	fs.Bool("skip-checksum", false, "")

	got, err := interspersedPluginInstallArgs(fs, []string{
		"test",
		"--config=registry.yaml",
		"--skip-checksum",
		"--plugin-dir", "plugins",
		"--",
		"--not-a-flag",
	})
	if err != nil {
		t.Fatalf("interspersedPluginInstallArgs: %v", err)
	}
	want := []string{
		"--config=registry.yaml",
		"--skip-checksum",
		"--plugin-dir", "plugins",
		"test",
		"--",
		"--not-a-flag",
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestRunPluginInstallCompatRequestedFailErrorsAndWarnPermits(t *testing.T) {
	reg := newCompatInstallRegistry(t, "test", "v0.2.0", []compatInstallVersion{
		{Version: "v0.2.0", Status: PluginCompatibilityStatusFail},
	})
	err := runPluginInstall([]string{
		"--config", reg.ConfigPath,
		"--plugin-dir", t.TempDir(),
		"--engine-version", "v0.51.2",
		"test@v0.2.0",
	})
	if err == nil {
		t.Fatal("expected requested known-fail error")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Fatalf("error = %v, want failed context", err)
	}

	pluginDir := t.TempDir()
	if err := runPluginInstall([]string{
		"--config", reg.ConfigPath,
		"--plugin-dir", pluginDir,
		"--engine-version", "v0.51.2",
		"--compat-mode", "warn",
		"test@v0.2.0",
	}); err != nil {
		t.Fatalf("runPluginInstall warn: %v", err)
	}
	if got := readInstalledVersion(filepath.Join(pluginDir, "test")); got != "v0.2.0" {
		t.Fatalf("installed version = %q, want v0.2.0", got)
	}
}

func TestRunPluginUpdateCompatUsesOlderPassingVersion(t *testing.T) {
	reg := newCompatInstallRegistry(t, "test", "v0.2.0", []compatInstallVersion{
		{Version: "v0.1.0", Status: PluginCompatibilityStatusPass},
		{Version: "v0.2.0", Status: PluginCompatibilityStatusFail},
	})
	pluginDir := t.TempDir()
	installed := filepath.Join(pluginDir, "test")
	if err := os.MkdirAll(installed, 0o750); err != nil {
		t.Fatalf("mkdir installed plugin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installed, "plugin.json"), []byte(`{"name":"test","version":"v0.0.1","author":"test","description":"old"}`), 0o600); err != nil {
		t.Fatalf("write installed plugin.json: %v", err)
	}
	if err := runPluginUpdate([]string{
		"--config", reg.ConfigPath,
		"--plugin-dir", pluginDir,
		"--engine-version", "v0.51.2",
		"test",
	}); err != nil {
		t.Fatalf("runPluginUpdate: %v", err)
	}
	if got := readInstalledVersion(installed); got != "v0.1.0" {
		t.Fatalf("installed version = %q, want v0.1.0", got)
	}
}

type compatInstallRegistry struct {
	ConfigPath string
}

type compatInstallVersion struct {
	Version string
	Status  string
}

func newCompatInstallRegistry(t *testing.T, plugin, manifestVersion string, versions []compatInstallVersion) compatInstallRegistry {
	t.Helper()
	archiveData := makeTestTarGz(t, plugin)
	sum := sha256.Sum256(archiveData)
	archiveSHA := hex.EncodeToString(sum[:])
	var serverURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/downloads/"):
			_, _ = w.Write(archiveData)
		case r.URL.Path == "/plugins/"+plugin+"/manifest.json":
			writeCompatRegistryManifest(t, w, plugin, manifestVersion, serverURL, archiveSHA)
		case r.URL.Path == "/compatibility/"+plugin+"/index.json":
			writeCompatRegistryIndex(t, w, plugin, serverURL, archiveSHA, versions)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	serverURL = srv.URL
	cfgPath := filepath.Join(t.TempDir(), "wfctl-registry.yaml")
	cfg := "registries:\n" +
		"  - name: local\n" +
		"    type: static\n" +
		"    url: " + srv.URL + "\n" +
		"    compatibilityEvidence:\n" +
		"      trust: first_party\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write registry config: %v", err)
	}
	return compatInstallRegistry{ConfigPath: cfgPath}
}

func writeCompatRegistryManifest(t *testing.T, w http.ResponseWriter, plugin, version, baseURL, archiveSHA string) {
	t.Helper()
	manifest := makeTestManifest(plugin, baseURL+"/downloads/"+plugin+"-"+version+".tar.gz", archiveSHA)
	manifest.Version = version
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func writeCompatRegistryIndex(t *testing.T, w http.ResponseWriter, plugin, baseURL, archiveSHA string, versions []compatInstallVersion) {
	t.Helper()
	idx := PluginVersionIndex{
		Plugin: plugin,
		EvidencePolicy: CompatibilityEvidencePolicy{
			RequiredFromEngine: "v0.51.0",
		},
	}
	for _, v := range versions {
		ev := resolverEvidence(v.Version, "v0.51.2", v.Status)
		ev.Plugin = plugin
		ev.OS = runtime.GOOS
		ev.Arch = runtime.GOARCH
		ev.ArchiveSHA256 = archiveSHA
		ev, err := ValidateCompatibilityEvidence(ev)
		if err != nil {
			t.Fatalf("validate evidence: %v", err)
		}
		idx.Versions = append(idx.Versions, PluginVersionRecord{
			Version:          v.Version,
			MinEngineVersion: "v0.50.0",
			Downloads: []PluginDownload{{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				URL:    baseURL + "/downloads/" + plugin + "-" + v.Version + ".tar.gz",
				SHA256: archiveSHA,
			}},
			Compatibility: []PluginCompatibilityEvidence{ev},
		})
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func TestWriteInstalledManifest_PreservesRequiredSecrets(t *testing.T) {
	// G4 regression: writeInstalledManifest used to drop the
	// required_secrets[] block from the registry manifest, so the
	// on-disk plugin.json had no required_secrets even when upstream
	// declared them. `wfctl secrets setup --plugin <name>` then
	// reported "declares no required_secrets[]" no-op.
	m := &RegistryManifest{
		Name: "workflow-plugin-hover", Version: "v0.2.0",
		Author: "GoCodeAlone", Description: "test",
		RequiredSecrets: []PluginRequiredSecret{
			{Name: "HOVER_USERNAME", Sensitive: false, Description: "Hover username", Prompt: "Hover username"},
			{Name: "HOVER_PASSWORD", Sensitive: true, Description: "Hover password"},
		},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.json")
	if err := writeInstalledManifest(path, m); err != nil {
		t.Fatalf("writeInstalledManifest: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var pj installedPluginJSON
	if err := json.Unmarshal(raw, &pj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(pj.RequiredSecrets) != 2 {
		t.Fatalf("required_secrets len = %d, want 2", len(pj.RequiredSecrets))
	}
	if pj.RequiredSecrets[0].Name != "HOVER_USERNAME" {
		t.Errorf("required_secrets[0].name = %q, want HOVER_USERNAME", pj.RequiredSecrets[0].Name)
	}
	if !pj.RequiredSecrets[1].Sensitive {
		t.Errorf("required_secrets[1].sensitive = false; want true (HOVER_PASSWORD)")
	}
	// Also assert the raw JSON contains the field — guards against a
	// future installedPluginJSON refactor that adds Sensitive omitempty
	// or otherwise hides the field at marshal time.
	if !strings.Contains(string(raw), "\"required_secrets\":") {
		t.Errorf("installed plugin.json missing required_secrets[] key:\n%s", string(raw))
	}
}

func TestRegistryManifest_UnmarshalPreservesRequiredSecrets(t *testing.T) {
	// G4 regression: RegistryManifest dropped required_secrets[] at
	// json.Unmarshal time because the struct didn't carry the field.
	src := `{
		"name": "workflow-plugin-hover",
		"version": "0.2.0",
		"author": "GoCodeAlone",
		"description": "test",
		"type": "external",
		"tier": "community",
		"license": "MIT",
		"required_secrets": [
			{"name": "X", "sensitive": false, "description": "d", "prompt": "p"},
			{"name": "Y", "sensitive": true}
		]
	}`
	var m RegistryManifest
	if err := json.Unmarshal([]byte(src), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(m.RequiredSecrets) != 2 {
		t.Fatalf("required_secrets len = %d, want 2", len(m.RequiredSecrets))
	}
	if m.RequiredSecrets[1].Name != "Y" || !m.RequiredSecrets[1].Sensitive {
		t.Errorf("unexpected required_secrets[1]: %+v", m.RequiredSecrets[1])
	}
}
