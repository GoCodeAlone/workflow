package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---- parseChecksumsTxt ----

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
	// Single space — not goreleaser two-space format.
	body := "abc123 plugin.tar.gz\n"
	_, err := parseChecksumsTxt(body)
	if err == nil {
		t.Fatal("expected error for malformed line")
	}
	if !strings.Contains(err.Error(), "malformed") {
		t.Errorf("expected 'malformed' in error, got: %v", err)
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

func TestParseChecksumsTxt_EmptyHashOrName(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"empty hash", "  filename.tar.gz"},
		{"empty name", "abc123  "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseChecksumsTxt(tc.body)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParseChecksumsTxt_ThreeSpacesRejected(t *testing.T) {
	// Three spaces between hash and filename: the name would start with a space,
	// causing silent lookup failures if not caught.
	body := "abc123   filename.tar.gz\n"
	_, err := parseChecksumsTxt(body)
	if err == nil {
		t.Fatal("expected error for 3-space separator")
	}
	if !strings.Contains(err.Error(), "exactly two spaces") {
		t.Errorf("expected 'exactly two spaces' in error, got: %v", err)
	}
}

// ---- lookupChecksumForURL ----

func TestLookupChecksumForURL_Found(t *testing.T) {
	archiveData := []byte("fake archive content")
	h := sha256.Sum256(archiveData)
	expectedSHA := hex.EncodeToString(h[:])

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
	archiveData := []byte("content")
	h := sha256.Sum256(archiveData)
	expectedSHA := hex.EncodeToString(h[:])

	mux := http.NewServeMux()
	mux.HandleFunc("/GoCodeAlone/repo/releases/download/v1.0.0/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		// checksums.txt stores the decoded filename.
		fmt.Fprintf(w, "%s  plugin name.tar.gz\n", expectedSHA)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// URL has encoded space: %20 — function must URL-decode before lookup.
	downloadURL := srv.URL + "/GoCodeAlone/repo/releases/download/v1.0.0/plugin%20name.tar.gz"
	got, err := lookupChecksumForURL(downloadURL)
	if err != nil {
		t.Fatalf("lookupChecksumForURL with URL-encoded name: %v", err)
	}
	if got != expectedSHA {
		t.Errorf("expected %s, got %s", expectedSHA, got)
	}
}

// checksumAuthTransport stays synchronous: assertions run on the test goroutine.
type checksumAuthTransport func(*http.Request) (*http.Response, error)

func (f checksumAuthTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func checksumAuthResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}
}
func checksumAuthSetup(t *testing.T, f checksumAuthTransport) {
	t.Helper()
	for _, key := range []string{"RELEASES_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(key, "")
	}
	oldBase, oldAPI, oldDefault, oldTransport := gitHubAPIBaseURL, gitHubAPIClient, http.DefaultClient, http.DefaultTransport
	gitHubAPIBaseURL = "https://api.github.com"
	gitHubAPIClient = &http.Client{Transport: f}
	http.DefaultClient = &http.Client{Transport: f}
	http.DefaultTransport = f
	t.Cleanup(func() {
		gitHubAPIBaseURL, gitHubAPIClient, http.DefaultClient, http.DefaultTransport = oldBase, oldAPI, oldDefault, oldTransport
	})
}

const checksumAuthURL = "https://github.com/GoCodeAlone/private/releases/download/v1.0.12/plugin.tar.gz"
const checksumAuthMetadata = `{"assets":[{"id":123,"name":"checksums.txt"},{"id":124,"name":"plugin.tar.gz"}]}`

func TestChecksumAuthPrivateAndPrecedence(t *testing.T) {
	for _, tc := range []struct{ name, releases, gh, github, want string }{
		{"GH", "", "synthetic-gh", "", "synthetic-gh"},
		{"GITHUB", "", "", "synthetic-github", "synthetic-github"},
		{"RELEASES", "synthetic-release", "synthetic-gh", "synthetic-github", "synthetic-release"},
		{"GHBeforeGITHUB", "", "synthetic-gh", "synthetic-github", "synthetic-gh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			var metadataCtx context.Context
			checksumAuthSetup(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "api.github.com" {
					return checksumAuthResponse(r, 404, "unauthenticated fallback"), nil
				}
				if got := r.Header.Get("Authorization"); got != "Bearer "+tc.want {
					t.Fatalf("authorization missing or wrong on request %d", calls)
				}
				deadline, ok := r.Context().Deadline()
				if !ok {
					t.Fatal("request has no deadline")
				}
				if calls == 1 {
					metadataCtx = r.Context()
					if r.Header.Get("Accept") != "application/vnd.github+json" {
						t.Fatal("metadata accept")
					}
					if d := time.Until(deadline); d <= 0 || d > gitHubReleaseMetadataTimeout {
						t.Fatalf("metadata deadline %v", d)
					}
					return checksumAuthResponse(r, 200, checksumAuthMetadata), nil
				}
				if r.URL.Path != "/repos/GoCodeAlone/private/releases/assets/123" {
					t.Fatal("wrong checksum asset path")
				}
				if r.Header.Get("Accept") != "application/octet-stream" {
					t.Fatal("asset accept")
				}
				if metadataCtx.Err() != context.Canceled || r.Context().Err() != nil {
					t.Fatal("asset did not get a fresh context")
				}
				if d := time.Until(deadline); d < 29*time.Second || d > 30*time.Second {
					t.Fatalf("checksum deadline %v", d)
				}
				return checksumAuthResponse(r, 200, "abc123  plugin.tar.gz\n"), nil
			})
			t.Setenv("RELEASES_TOKEN", tc.releases)
			t.Setenv("GH_TOKEN", tc.gh)
			t.Setenv("GITHUB_TOKEN", tc.github)
			got, err := lookupChecksumForURL(checksumAuthURL)
			if err != nil || got != "abc123" || calls != 2 {
				t.Fatalf("got=%q err=%v calls=%d", got, err, calls)
			}
		})
	}
}
func TestChecksumAuthFailuresNoFallback(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		metadataStatus, assetStatus int
		metadata, body              string
		wantCalls                   int
	}{
		{"metadata401", 401, 200, "", "", 1}, {"metadata403", 403, 200, "", "", 1}, {"metadata404", 404, 200, "", "", 1},
		{"asset401", 200, 401, checksumAuthMetadata, "", 2}, {"asset403", 200, 403, checksumAuthMetadata, "", 2}, {"asset404", 200, 404, checksumAuthMetadata, "", 2},
		{"malformedMetadata", 200, 200, "{", "", 1}, {"missingAsset", 200, 200, `{"assets":[]}`, "", 1},
		{"malformedChecksum", 200, 200, checksumAuthMetadata, "bad line", 2}, {"missingEntry", 200, 200, checksumAuthMetadata, "abc  other.tar.gz\n", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			checksumAuthSetup(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "api.github.com" {
					t.Fatal("unauthenticated fallback")
				}
				if r.Header.Get("Authorization") != "Bearer synthetic" {
					t.Fatal("missing auth")
				}
				if calls == 1 {
					return checksumAuthResponse(r, tc.metadataStatus, tc.metadata), nil
				}
				return checksumAuthResponse(r, tc.assetStatus, tc.body), nil
			})
			t.Setenv("GH_TOKEN", "synthetic")
			if _, err := lookupChecksumForURL(checksumAuthURL); err == nil {
				t.Fatal("expected failure")
			}
			if calls != tc.wantCalls {
				t.Fatalf("calls=%d want=%d", calls, tc.wantCalls)
			}
		})
	}
}
func TestChecksumAuthPublicAndNonGitHub(t *testing.T) {
	for _, tc := range []struct{ name, url, token string }{{"public", checksumAuthURL, ""}, {"nonGitHub", "https://plugins.example/release/plugin.tar.gz", "synthetic"}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			checksumAuthSetup(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Authorization") != "" {
					t.Fatal("unexpected credential")
				}
				if r.URL.Host == "api.github.com" {
					t.Fatal("unexpected API")
				}
				return checksumAuthResponse(r, 200, "abc  plugin.tar.gz\n"), nil
			})
			t.Setenv("GH_TOKEN", tc.token)
			got, err := lookupChecksumForURL(tc.url)
			if err != nil || got != "abc" || calls != 1 {
				t.Fatalf("got=%q err=%v calls=%d", got, err, calls)
			}
		})
	}
}
func TestChecksumAuthRedirects(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(fmt.Sprint(reject), func(t *testing.T) {
			calls := 0
			checksumAuthSetup(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return checksumAuthResponse(r, 200, checksumAuthMetadata), nil
				}
				if calls == 2 {
					resp := checksumAuthResponse(r, 302, "")
					resp.Header.Set("Location", "https://signed-assets.example/checksums.txt?signature=synthetic")
					return resp, nil
				}
				if r.URL.Host != "signed-assets.example" || r.Header.Get("Authorization") != "" {
					t.Fatal("credential forwarded to unrelated host")
				}
				return checksumAuthResponse(r, 200, "abc  plugin.tar.gz\n"), nil
			})
			t.Setenv("GH_TOKEN", "synthetic")
			if reject {
				gitHubAPIClient.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("synthetic redirect rejection") }
			}
			got, err := lookupChecksumForURL(checksumAuthURL)
			if reject {
				if err == nil || calls != 2 {
					t.Fatalf("err=%v calls=%d", err, calls)
				}
			} else if err != nil || got != "abc" || calls != 3 {
				t.Fatalf("got=%q err=%v calls=%d", got, err, calls)
			}
		})
	}
}
func TestChecksumAuthTransportFailureAndArchiveDeadline(t *testing.T) {
	for _, stage := range []int{1, 2} {
		t.Run(fmt.Sprint(stage), func(t *testing.T) {
			calls := 0
			checksumAuthSetup(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == stage {
					return nil, context.DeadlineExceeded
				}
				return checksumAuthResponse(r, 200, checksumAuthMetadata), nil
			})
			t.Setenv("GH_TOKEN", "synthetic")
			if _, err := lookupChecksumForURL(checksumAuthURL); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("err=%v", err)
			}
			if calls != stage {
				t.Fatal("fallback after timeout")
			}
		})
	}
	t.Run("archive", func(t *testing.T) {
		calls := 0
		checksumAuthSetup(t, func(r *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return checksumAuthResponse(r, 200, checksumAuthMetadata), nil
			}
			d, ok := r.Context().Deadline()
			if !ok || time.Until(d) > downloadTimeout || time.Until(d) < downloadTimeout-time.Second {
				t.Fatal("archive timeout changed")
			}
			return checksumAuthResponse(r, 200, "archive"), nil
		})
		got, err := downloadGitHubReleaseAsset("GoCodeAlone", "private", "v1.0.12", "plugin.tar.gz", "synthetic")
		if err != nil || string(got) != "archive" {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestChecksumAuthFailedUpgradePreservesInstall(t *testing.T) {
	for _, body := range []string{"", "invalid", "abc  another.tar.gz\n", strings.Repeat("0", 64) + "  plugin.tar.gz\n"} {
		t.Run(fmt.Sprintf("body-%d", len(body)), func(t *testing.T) {
			t.Chdir(t.TempDir())
			pluginDir := t.TempDir()
			dest := filepath.Join(pluginDir, "myplugin")
			if err := os.MkdirAll(dest, 0750); err != nil {
				t.Fatal(err)
			}
			sentinel := []byte("existing stable binary")
			binaryPath := filepath.Join(dest, "myplugin")
			if err := os.WriteFile(binaryPath, sentinel, 0750); err != nil {
				t.Fatal(err)
			}
			tarball := makeTestTarGz(t, "myplugin")
			calls := 0
			checksumAuthSetup(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "api.github.com" || r.Header.Get("Authorization") != "Bearer synthetic" {
					t.Fatal("unexpected unauthenticated request")
				}
				if strings.Contains(r.URL.Path, "/releases/tags/") {
					return checksumAuthResponse(r, 200, checksumAuthMetadata), nil
				}
				if strings.HasSuffix(r.URL.Path, "/124") {
					return checksumAuthResponse(r, 200, string(tarball)), nil
				}
				if strings.HasSuffix(r.URL.Path, "/123") {
					return checksumAuthResponse(r, 200, body), nil
				}
				t.Fatal("unexpected asset")
				return nil, errors.New("unexpected asset")
			})
			t.Setenv("GH_TOKEN", "synthetic")
			if err := installPluginFromManifest(pluginDir, "myplugin", makeTestManifest("myplugin", checksumAuthURL, ""), nil, false); err == nil {
				t.Fatal("expected checksum failure")
			}
			if calls != 4 {
				t.Fatalf("requests=%d, want archive and checksum metadata/assets", calls)
			}
			got, err := os.ReadFile(binaryPath)
			if err != nil || string(got) != string(sentinel) {
				t.Fatalf("existing binary changed: %q %v", got, err)
			}
			if _, err := os.Stat(dest + ".installing"); !os.IsNotExist(err) {
				t.Fatalf("staging not cleaned: %v", err)
			}
		})
	}
}
