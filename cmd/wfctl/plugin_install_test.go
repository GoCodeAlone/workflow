package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
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
