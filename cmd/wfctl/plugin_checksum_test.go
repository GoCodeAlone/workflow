package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
