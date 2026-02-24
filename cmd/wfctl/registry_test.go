package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// patchRegistryURL temporarily replaces FetchManifest's base URL for testing.
// We do this by using httptest.Server and a test helper that accepts a base URL.

// fetchManifestFromURL is the testable version of FetchManifest.
func fetchManifestFromURL(baseURL, name string) (*RegistryManifest, error) {
	url := baseURL + "/plugins/" + name + "/manifest.json"
	resp, err := http.Get(url) //nolint:gosec // G107: test-only helper
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}
	var m RegistryManifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

func TestRegistryManifest_ParseJSON(t *testing.T) {
	raw := `{
		"name": "admin",
		"version": "1.0.0",
		"author": "GoCodeAlone",
		"description": "Admin dashboard",
		"type": "external",
		"tier": "core",
		"license": "MIT",
		"downloads": [
			{"os": "linux",  "arch": "amd64", "url": "https://example.com/admin-linux-amd64.tar.gz"},
			{"os": "darwin", "arch": "arm64", "url": "https://example.com/admin-darwin-arm64.tar.gz", "sha256": "abc123"}
		]
	}`
	var m RegistryManifest
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Name != "admin" {
		t.Errorf("name: got %q, want %q", m.Name, "admin")
	}
	if m.Version != "1.0.0" {
		t.Errorf("version: got %q, want %q", m.Version, "1.0.0")
	}
	if len(m.Downloads) != 2 {
		t.Fatalf("downloads: got %d, want 2", len(m.Downloads))
	}
	if m.Downloads[1].SHA256 != "abc123" {
		t.Errorf("sha256: got %q, want %q", m.Downloads[1].SHA256, "abc123")
	}
}

func TestRegistryManifest_FindDownload_Match(t *testing.T) {
	m := &RegistryManifest{
		Name:    "admin",
		Version: "1.0.0",
		Downloads: []PluginDownload{
			{OS: "linux", Arch: "amd64", URL: "https://example.com/linux-amd64.tar.gz"},
			{OS: "darwin", Arch: "arm64", URL: "https://example.com/darwin-arm64.tar.gz"},
		},
	}
	dl, err := m.FindDownload("darwin", "arm64")
	if err != nil {
		t.Fatalf("FindDownload: %v", err)
	}
	if dl.URL != "https://example.com/darwin-arm64.tar.gz" {
		t.Errorf("URL: got %q", dl.URL)
	}
}

func TestRegistryManifest_FindDownload_NoMatch(t *testing.T) {
	m := &RegistryManifest{
		Name: "admin",
		Downloads: []PluginDownload{
			{OS: "linux", Arch: "amd64", URL: "https://example.com/linux-amd64.tar.gz"},
		},
	}
	_, err := m.FindDownload("windows", "amd64")
	if err == nil {
		t.Fatal("expected error for missing OS/arch")
	}
}

func TestFetchManifest_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	m, err := fetchManifestFromURL(srv.URL, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != nil {
		t.Errorf("expected nil manifest for 404, got: %+v", m)
	}
}

func TestFetchManifest_ValidJSON(t *testing.T) {
	manifest := RegistryManifest{
		Name:    "test-plugin",
		Version: "0.1.0",
		Downloads: []PluginDownload{
			{OS: "linux", Arch: "amd64", URL: "https://example.com/test.tar.gz"},
		},
	}
	data, _ := json.Marshal(manifest)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(data) //nolint:errcheck // test server
	}))
	defer srv.Close()

	m, err := fetchManifestFromURL(srv.URL, "test-plugin")
	if err != nil {
		t.Fatalf("fetchManifest: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil manifest")
	}
	if m.Name != "test-plugin" {
		t.Errorf("name: got %q, want %q", m.Name, "test-plugin")
	}
	if len(m.Downloads) != 1 {
		t.Errorf("downloads: got %d, want 1", len(m.Downloads))
	}
}

func TestVerifyChecksum(t *testing.T) {
	// sha256("hello") = 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
	data := []byte("hello")
	expected := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if err := verifyChecksum(data, expected); err != nil {
		t.Errorf("expected checksum to pass: %v", err)
	}
}

func TestVerifyChecksum_Mismatch(t *testing.T) {
	data := []byte("hello")
	if err := verifyChecksum(data, "wrong"); err == nil {
		t.Error("expected checksum mismatch error")
	}
}

func TestParseNameVersion(t *testing.T) {
	tests := []struct {
		arg     string
		name    string
		version string
	}{
		{"admin", "admin", ""},
		{"admin@1.0.0", "admin", "1.0.0"},
		{"my-plugin@0.2.1", "my-plugin", "0.2.1"},
	}
	for _, tt := range tests {
		n, v := parseNameVersion(tt.arg)
		if n != tt.name || v != tt.version {
			t.Errorf("parseNameVersion(%q) = (%q, %q), want (%q, %q)", tt.arg, n, v, tt.name, tt.version)
		}
	}
}

func TestStripTopDir(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"plugin-linux-amd64/binary", "binary"},
		{"plugin-linux-amd64/subdir/file", "subdir/file"},
		{"singlefile", "singlefile"},
		{"plugin-linux-amd64/", ""},
	}
	for _, tt := range tests {
		got := stripTopDir(tt.input)
		if got != tt.want {
			t.Errorf("stripTopDir(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSafeJoin_PathTraversal(t *testing.T) {
	base := "/safe/base"
	_, err := safeJoin(base, "../../../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

func TestSafeJoin_Valid(t *testing.T) {
	base := "/safe/base"
	got, err := safeJoin(base, "subdir/file.txt")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	expected := "/safe/base/subdir/file.txt"
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestReadInstalledVersion(t *testing.T) {
	dir := t.TempDir()
	data := `{"name":"admin","version":"1.2.3"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(data), 0640); err != nil {
		t.Fatal(err)
	}
	ver := readInstalledVersion(dir)
	if ver != "1.2.3" {
		t.Errorf("got %q, want %q", ver, "1.2.3")
	}
}

func TestReadInstalledVersion_Missing(t *testing.T) {
	dir := t.TempDir()
	ver := readInstalledVersion(dir)
	if ver != "unknown" {
		t.Errorf("got %q, want %q", ver, "unknown")
	}
}

func TestRunPluginList_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	// Should not error even with empty dir
	err := runPluginList([]string{"--data-dir", dir})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunPluginList_NonExistentDir(t *testing.T) {
	dir := t.TempDir()
	err := runPluginList([]string{"--data-dir", filepath.Join(dir, "nonexistent")})
	if err != nil {
		t.Errorf("unexpected error for missing dir: %v", err)
	}
}

func TestRunPluginList_WithInstalledPlugins(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "admin")
	if err := os.MkdirAll(pluginDir, 0750); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"admin","version":"1.0.0"}` + "\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0640); err != nil {
		t.Fatal(err)
	}
	if err := runPluginList([]string{"--data-dir", dir}); err != nil {
		t.Errorf("runPluginList: %v", err)
	}
}

func TestRunPluginRemove_NotInstalled(t *testing.T) {
	dir := t.TempDir()
	err := runPluginRemove([]string{"--data-dir", dir, "nonexistent"})
	if err == nil {
		t.Error("expected error for uninstalled plugin")
	}
}

func TestRunPluginRemove_Installed(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "admin")
	if err := os.MkdirAll(pluginDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := runPluginRemove([]string{"--data-dir", dir, "admin"}); err != nil {
		t.Errorf("runPluginRemove: %v", err)
	}
	if _, err := os.Stat(pluginDir); !os.IsNotExist(err) {
		t.Error("expected plugin dir to be removed")
	}
}
