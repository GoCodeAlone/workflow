package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

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

// TestDownloadURL_GitHubAuthHeader verifies that downloadURL injects an
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
		{"RELEASES_TOKEN", "RELEASES_TOKEN", "token " + sentinel},
		{"GH_TOKEN", "GH_TOKEN", "token " + sentinel},
		{"GITHUB_TOKEN", "GITHUB_TOKEN", "token " + sentinel},
		{"no token", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Capture the Authorization header the client sends.
			var gotHdr string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotHdr = r.Header.Get("Authorization")
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			// Build a URL that contains "github.com" as a path segment so the
			// auth injection logic fires, while actually targeting the test server.
			testURL := srv.URL + "/github.com/GoCodeAlone/plugin/releases/download/v1.0.0/asset.tar.gz"

			// Set / clear the env var for this sub-test.
			for _, k := range []string{"RELEASES_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
				t.Setenv(k, "")
			}
			if tc.envKey != "" {
				t.Setenv(tc.envKey, sentinel)
			}

			if _, err := downloadURL(testURL); err != nil {
				t.Fatalf("downloadURL: %v", err)
			}
			if gotHdr != tc.wantHdr {
				t.Errorf("Authorization header = %q, want %q", gotHdr, tc.wantHdr)
			}
		})
	}
}

// TestDownloadURL_NonGitHubNoAuthHeader verifies that downloadURL does NOT inject
// an Authorization header for non-github.com URLs, even when a token env var is set.
func TestDownloadURL_NonGitHubNoAuthHeader(t *testing.T) {
	var gotHdr string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHdr = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("RELEASES_TOKEN", "should-not-appear")

	if _, err := downloadURL(srv.URL + "/some/asset.tar.gz"); err != nil {
		t.Fatalf("downloadURL: %v", err)
	}
	if gotHdr != "" {
		t.Errorf("expected no Authorization header for non-github URL, got %q", gotHdr)
	}
}

// TestDownloadURL_TokenPriority verifies that RELEASES_TOKEN takes precedence over
// GH_TOKEN and GITHUB_TOKEN when multiple env vars are set.
func TestDownloadURL_TokenPriority(t *testing.T) {
	var gotHdr string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHdr = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("RELEASES_TOKEN", "releases-wins")
	t.Setenv("GH_TOKEN", "gh-loses")
	t.Setenv("GITHUB_TOKEN", "github-loses")

	testURL := srv.URL + "/github.com/GoCodeAlone/plugin/releases/download/v1.0.0/asset.tar.gz"
	if _, err := downloadURL(testURL); err != nil {
		t.Fatalf("downloadURL: %v", err)
	}
	if gotHdr != "token releases-wins" {
		t.Errorf("Authorization header = %q, want %q", gotHdr, "token releases-wins")
	}
}
