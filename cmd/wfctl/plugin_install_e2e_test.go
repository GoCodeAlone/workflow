package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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

	"github.com/GoCodeAlone/workflow/plugin/external"
)

// buildTarGz creates an in-memory tar.gz archive.
// entries is a map of path → content (as []byte). mode is applied to all files.
func buildTarGz(t *testing.T, entries map[string][]byte, mode os.FileMode) []byte {
	t.Helper()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	for name, content := range entries {
		hdr := &tar.Header{
			Name:     name,
			Typeflag: tar.TypeReg,
			Mode:     int64(mode),
			Size:     int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar write header %q: %v", name, err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatalf("tar write content %q: %v", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// sha256Hex returns the hex-encoded SHA256 of data.
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// TestPluginInstallE2E exercises the full install pipeline:
// registry → fetch manifest → download tarball → verify checksum → extract → discover.
func TestPluginInstallE2E(t *testing.T) {
	const pluginName = "test-plugin"
	binaryContent := []byte("#!/bin/sh\necho hello\n")

	// Build in-memory tar.gz with the binary nested under a top-level directory,
	// as real plugin releases do (e.g. test-plugin-darwin-arm64/test-plugin).
	topDir := fmt.Sprintf("%s-%s-%s", pluginName, runtime.GOOS, runtime.GOARCH)
	tarEntries := map[string][]byte{
		topDir + "/" + pluginName: binaryContent,
	}
	tarball := buildTarGz(t, tarEntries, 0755)
	checksum := sha256Hex(tarball)

	// httptest server that serves the tarball.
	tarSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(tarball) //nolint:errcheck
	}))
	defer tarSrv.Close()

	tarURL := tarSrv.URL + "/" + pluginName + ".tar.gz"

	// Build a RegistryManifest pointing at the local tar server.
	manifest := &RegistryManifest{
		Name:        pluginName,
		Version:     "1.0.0",
		Author:      "tester",
		Description: "e2e test plugin",
		Type:        "external",
		Tier:        "community",
		License:     "MIT",
		Downloads: []PluginDownload{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				URL:    tarURL,
				SHA256: checksum,
			},
		},
	}

	// Wire up a mock registry source (uses the shared mockRegistrySource from multi_registry_test.go).
	src := &mockRegistrySource{
		name: "test-registry",
		manifests: map[string]*RegistryManifest{
			pluginName: manifest,
		},
	}
	mr := NewMultiRegistryFromSources(src)

	// --- Step 1: Fetch manifest via registry ---
	gotManifest, sourceName, err := mr.FetchManifest(pluginName)
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if sourceName != "test-registry" {
		t.Errorf("source name: got %q, want %q", sourceName, "test-registry")
	}
	if gotManifest.Name != pluginName {
		t.Errorf("manifest name: got %q, want %q", gotManifest.Name, pluginName)
	}

	// --- Step 2: Find platform download ---
	dl, err := gotManifest.FindDownload(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("FindDownload(%s/%s): %v", runtime.GOOS, runtime.GOARCH, err)
	}

	// --- Step 3: Download tarball ---
	data, err := downloadURL(dl.URL)
	if err != nil {
		t.Fatalf("downloadURL: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("downloaded empty tarball")
	}

	// --- Step 4: Verify checksum ---
	if err := verifyChecksum(data, dl.SHA256); err != nil {
		t.Fatalf("verifyChecksum: %v", err)
	}

	// --- Step 5: Extract to temp dir ---
	pluginsDir := t.TempDir()
	destDir := filepath.Join(pluginsDir, pluginName)
	if err := os.MkdirAll(destDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := extractTarGz(data, destDir); err != nil {
		t.Fatalf("extractTarGz: %v", err)
	}

	// Verify the binary was extracted with correct content.
	binaryPath := filepath.Join(destDir, pluginName)
	gotContent, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read extracted binary: %v", err)
	}
	if !bytes.Equal(gotContent, binaryContent) {
		t.Errorf("binary content mismatch: got %q, want %q", gotContent, binaryContent)
	}

	// --- Step 6: Write plugin.json ---
	pluginJSONPath := filepath.Join(destDir, "plugin.json")
	if err := writeInstalledManifest(pluginJSONPath, gotManifest); err != nil {
		t.Fatalf("writeInstalledManifest: %v", err)
	}

	// Verify plugin.json content.
	raw, err := os.ReadFile(pluginJSONPath)
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}
	var pj installedPluginJSON
	if err := json.Unmarshal(raw, &pj); err != nil {
		t.Fatalf("unmarshal plugin.json: %v", err)
	}
	if pj.Name != pluginName {
		t.Errorf("plugin.json name: got %q, want %q", pj.Name, pluginName)
	}
	if pj.Version != "1.0.0" {
		t.Errorf("plugin.json version: got %q, want %q", pj.Version, "1.0.0")
	}

	// --- Step 7: ExternalPluginManager.DiscoverPlugins ---
	mgr := external.NewExternalPluginManager(pluginsDir, nil)
	discovered, err := mgr.DiscoverPlugins()
	if err != nil {
		t.Fatalf("DiscoverPlugins: %v", err)
	}
	if len(discovered) != 1 {
		t.Fatalf("expected 1 discovered plugin, got %d: %v", len(discovered), discovered)
	}
	if discovered[0] != pluginName {
		t.Errorf("discovered plugin: got %q, want %q", discovered[0], pluginName)
	}
}

// TestExtractTarGz verifies that tar.gz extraction produces correct files with preserved modes.
func TestExtractTarGz(t *testing.T) {
	entries := map[string][]byte{
		"top/file.txt":        []byte("hello"),
		"top/subdir/deep.txt": []byte("world"),
		"top/script.sh":       []byte("#!/bin/sh\necho hi"),
	}
	tarball := buildTarGz(t, entries, 0755)

	destDir := t.TempDir()
	if err := extractTarGz(tarball, destDir); err != nil {
		t.Fatalf("extractTarGz: %v", err)
	}

	// Verify each file (strip the top dir — extractTarGz does this internally).
	checks := map[string][]byte{
		"file.txt":        []byte("hello"),
		"subdir/deep.txt": []byte("world"),
		"script.sh":       []byte("#!/bin/sh\necho hi"),
	}
	for rel, want := range checks {
		path := filepath.Join(destDir, rel)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %q: %v", rel, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("content of %q: got %q, want %q", rel, got, want)
		}
	}

	// Verify mode is preserved for script.sh (executable).
	info, err := os.Stat(filepath.Join(destDir, "script.sh"))
	if err != nil {
		t.Fatalf("stat script.sh: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Errorf("expected executable bit set on script.sh, got mode %v", info.Mode())
	}
}

// TestExtractTarGzPathTraversal verifies that path traversal entries are rejected.
func TestExtractTarGzPathTraversal(t *testing.T) {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	// Write a malicious entry that tries to escape the destination directory.
	content := []byte("malicious")
	hdr := &tar.Header{
		Name:     "top/../../etc/passwd",
		Typeflag: tar.TypeReg,
		Mode:     0644,
		Size:     int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("write content: %v", err)
	}
	tw.Close()  //nolint:errcheck
	gzw.Close() //nolint:errcheck

	destDir := t.TempDir()
	err := extractTarGz(buf.Bytes(), destDir)
	if err == nil {
		t.Fatal("expected error for path traversal entry, got nil")
	}
}

// TestSafeJoin is a table-driven test for the safeJoin helper.
func TestSafeJoin(t *testing.T) {
	base := "/safe/base"
	tests := []struct {
		name    string
		input   string
		wantErr bool
		want    string
	}{
		{
			name:    "normal file",
			input:   "file.txt",
			wantErr: false,
			want:    "/safe/base/file.txt",
		},
		{
			name:    "nested path",
			input:   "subdir/file.txt",
			wantErr: false,
			want:    "/safe/base/subdir/file.txt",
		},
		{
			name:    "path traversal dotdot",
			input:   "../etc/passwd",
			wantErr: true,
		},
		{
			name:    "absolute path traversal",
			input:   "../../etc/passwd",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := safeJoin(base, tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q, got path %q", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDownloadURL tests the downloadURL helper using an httptest server.
func TestDownloadURL(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		body := []byte("binary content here")
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write(body) //nolint:errcheck
		}))
		defer srv.Close()

		got, err := downloadURL(srv.URL)
		if err != nil {
			t.Fatalf("downloadURL: %v", err)
		}
		if !bytes.Equal(got, body) {
			t.Errorf("content mismatch: got %q, want %q", got, body)
		}
	})

	t.Run("404 returns error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer srv.Close()

		_, err := downloadURL(srv.URL)
		if err == nil {
			t.Fatal("expected error for 404, got nil")
		}
	})

	t.Run("500 returns error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}))
		defer srv.Close()

		_, err := downloadURL(srv.URL)
		if err == nil {
			t.Fatal("expected error for 500, got nil")
		}
	})
}
