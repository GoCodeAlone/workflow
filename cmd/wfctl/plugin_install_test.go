package main

import (
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
// Authorization header for github.com URLs using the first non-empty token env
// var (RELEASES_TOKEN > GH_TOKEN > GITHUB_TOKEN), and sends no header when none
// are set.
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

			// Use a real github.com URL — captureTransport redirects it to srv.
			testURL := "https://github.com/GoCodeAlone/plugin/releases/download/v1.0.0/asset.tar.gz"
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
// GH_TOKEN and GITHUB_TOKEN when multiple env vars are set.
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

	testURL := "https://github.com/GoCodeAlone/plugin/releases/download/v1.0.0/asset.tar.gz"
	if _, err := downloadURL(testURL); err != nil {
		t.Fatalf("downloadURL: %v", err)
	}
	const want = "Bearer releases-wins"
	if got := ct.gotHeader(); got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
}
